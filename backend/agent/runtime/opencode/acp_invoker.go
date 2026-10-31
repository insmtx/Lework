package opencode

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/insmtx/Lework/backend/agent"
	"github.com/insmtx/Lework/backend/agent/runtime/internal/acp"
	"github.com/insmtx/Lework/backend/agent/runtime/internal/cli"
	runtimeprocess "github.com/insmtx/Lework/backend/agent/runtime/internal/process"
	"github.com/ygpkg/yg-go/logs"
)

const (
	// acpEventBuffer 与 acpUpdateBuffer 是两级队列：SDK 通知线程只写 update 队列，
	// 翻译 goroutine 再写事件通道，避免阻塞水位推进（设计文档 §3.2.1）。
	acpEventBuffer  = 1024
	acpUpdateBuffer = 4096
	// fallbackReply 与 v1 保持一致：空正文/回声都按失败处理。
	fallbackReply = "抱歉，本次未生成有效回复，请稍后重试或换个提问方式。"
	// rejectedReply 专用于「用户拒绝工具调用」：v2 侧表现为 cancelled+空正文，
	// 若沿用 fallbackReply，用户拒绝后看到的是"未生成回复"，容易被误读为模型故障。
	rejectedReply = "已拒绝本次工具调用，本轮已停止，未执行后续修改。"
)

// ACPInvoker 通过 opencode v2 的 ACP（stdio JSON-RPC）执行一次提示。
type ACPInvoker struct {
	binary              string
	baseEnv             []string
	dataDir             string
	progressIdleTimeout time.Duration
}

// NewACPInvoker 创建 ACP 调用器。
func NewACPInvoker(binary string, extraEnv map[string]string, dataDir string, progressIdleTimeout time.Duration) *ACPInvoker {
	if strings.TrimSpace(binary) == "" {
		binary = "opencode"
	}
	// 与 v1 一致：宿主未配置进度空闲超时时回退 10 分钟，避免完全关闭看门狗。
	if progressIdleTimeout <= 0 {
		progressIdleTimeout = 10 * time.Minute
	}
	return &ACPInvoker{
		binary:              binary,
		baseEnv:             runtimeprocess.BuildBaseEnv(extraEnv),
		dataDir:             dataDir,
		progressIdleTimeout: progressIdleTimeout,
	}
}

// Prepare 目前不需要额外工作区准备（配置随每次 Invoke 注入）。
func (inv *ACPInvoker) Prepare(_ context.Context, _ string) error { return nil }

// Invoke 启动 `opencode acp`、建立会话并把事件/结果通道交给 cli.Runner。
func (inv *ACPInvoker) Invoke(ctx context.Context, req cli.InvocationRequest) (*cli.Invocation, error) {
	workDir := strings.TrimSpace(req.WorkDir)
	if workDir == "" {
		return nil, fmt.Errorf("opencode acp invocation requires a work dir")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("opencode acp invocation requires a prompt")
	}

	options := make([]configOption, 0, 1)
	if req.ExecutionMode == agent.ExecutionModePlan {
		options = append(options, withPlanMode())
	}
	configContent, err := buildConfigContent(req.Model, req.MCPServers, req.SkillDir, options...)
	if err != nil {
		return nil, err
	}
	// 只记录脱敏后的配置：provider apiKey 已删除、请求头值已替换为 ***。
	logs.DebugContextf(ctx, "opencode acp config: %s", sanitizeConfigContent(configContent))
	databasePath, err := ensureOpenCodeDBPath(inv.dataDir)
	if err != nil {
		return nil, err
	}

	env := append([]string{}, inv.baseEnv...)
	env = append(env, req.ExtraEnv...)
	env = append(env, "OPENCODE_CONFIG_CONTENT="+configContent)
	env = append(env, "OPENCODE_DISABLE_PROJECT_CONFIG=1", "OPENCODE_DISABLE_CLAUDE_CODE=1", "OPENCODE_PURE=1", "OPENCODE_DISABLE_AUTOUPDATE=1")
	if databasePath != "" {
		env = append(env, "OPENCODE_DB="+databasePath)
	}
	if strings.TrimSpace(inv.dataDir) != "" {
		env = append(env, "OPENCODE_CONFIG_DIR="+inv.dataDir)
	}

	conn, err := acp.Start(ctx, acp.Config{
		Binary: inv.binary,
		Dir:    workDir,
		Env:    env,
	})
	if err != nil {
		return nil, err
	}

	resumeID := ""
	if req.Resume {
		resumeID = strings.TrimSpace(req.SessionID)
	}
	sess, resumed, err := conn.ResumeOrNewSession(ctx, resumeID, workDir, acpMCPServers(req.MCPServers))
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if req.Resume && resumeID != "" && !resumed {
		logs.Warnf("opencode acp resume_fallback: execution_id=%s requested_session=%s new_session=%s",
			req.ExecutionID, resumeID, sess.ID)
	}
	wantModel := ""
	if strings.TrimSpace(req.Model.Model) != "" {
		wantModel = providerID + "/" + strings.TrimSpace(req.Model.Model)
	}
	if err := sess.RequireModel(wantModel); err != nil {
		_ = conn.Close()
		return nil, err
	}
	logs.InfoContextf(ctx, "opencode acp invocation starting: execution_id=%s session_id=%s mode=%s model=%s resume=%v",
		req.ExecutionID, sess.ID, req.ExecutionMode, wantModel, req.Resume)

	events := make(chan agent.NodeEvent, acpEventBuffer)
	results := make(chan cli.InvocationResult, 1)
	updates := make(chan acpsdk.SessionNotification, acpUpdateBuffer)

	translator := acp.NewTranslator(conn.Stats())
	translator.SetWorkDir(workDir)
	translatorDone := make(chan struct{})
	go func() {
		defer close(translatorDone)
		for notification := range updates {
			for _, event := range translator.Handle(notification) {
				select {
				case events <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	var dropWarnOnce sync.Once
	conn.Client().SetUpdateSink(func(notification acpsdk.SessionNotification) {
		select {
		case updates <- notification:
		default:
			// 超限丢弃并计数，绝不阻塞 SDK 通知线程；首次丢弃打一次 warn。
			conn.Stats().DroppedEvents.Add(1)
			dropWarnOnce.Do(func() {
				logs.Warnf("opencode acp update queue full; dropping session updates (execution_id=%s)", req.ExecutionID)
			})
		}
	})

	emit := func(event agent.NodeEvent) error {
		select {
		case events <- event:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	permissionBridge := acp.NewPermissionBridge(string(sess.ID), emit)
	// planConfirmed 记录本轮是否真的产出了计划确认表单：emitPlanAware 在
	// 表单确认成立时置位，供一次性的兜底追问判断（见 plan.go 的契约说明）。
	var planConfirmed atomic.Bool
	formBridge := acp.NewFormBridge(string(sess.ID), conn.Stats(), func(event agent.NodeEvent) error {
		confirmed, err := emitPlanAware(emit, translator, workDir, string(sess.ID), req.ExecutionMode, event)
		if confirmed {
			planConfirmed.Store(true)
		}
		return err
	})
	conn.Client().SetPermissionBridge(permissionBridge)
	conn.Client().SetFormBridge(formBridge)

	// agent.start 必须先于任何消息事件；ProviderSessionID 是多轮续跑的关键。
	emit(agent.NewAgentStartEvent(string(sess.ID)))

	go func() {
		defer close(results)
		defer conn.Close()
		resp, promptErr := sess.PromptBlocks(ctx, promptBlocks(req), acp.PromptOptions{
			IdleTimeout: inv.progressIdleTimeout,
		})

		// 计划模式兜底：模型写完计划却没按契约问 Yes/No 时，在同一 ACP session
		// 内补一次追问。追问本身失败只记日志，不影响首轮已经拿到的结果。
		var retryResp acpsdk.PromptResponse
		retried := false
		if _, _, hasPlan := translator.PlanArtifact(); ShouldRetryPlanConfirmation(
			req.ExecutionMode, planConfirmed.Load(), hasPlan, promptErr, ctx.Err(),
		) {
			logs.WarnContextf(ctx, "plan_contract_miss: retrying confirmation question: execution_id=%s session_id=%s",
				req.ExecutionID, sess.ID)
			var retryErr error
			retryResp, retryErr = sess.PromptBlocks(ctx,
				[]acpsdk.ContentBlock{acpsdk.TextBlock(PlanRetryPrompt)},
				acp.PromptOptions{IdleTimeout: inv.progressIdleTimeout},
			)
			if retryErr != nil {
				logs.WarnContextf(ctx, "plan confirmation retry failed: execution_id=%s err=%v",
					req.ExecutionID, retryErr)
			} else {
				retried = true
				if !planConfirmed.Load() {
					logs.WarnContextf(ctx, "plan_contract_miss: confirmation still missing after retry: execution_id=%s session_id=%s",
						req.ExecutionID, sess.ID)
				}
			}
		}

		close(updates)
		<-translatorDone

		finalText := strings.TrimSpace(translator.FinalText())
		usage := usageFromACP(resp.Usage)
		if retried {
			addUsage(usage, usageFromACP(retryResp.Usage))
		}
		stopReason := resp.StopReason

		result := cli.InvocationResult{
			Message:           finalText,
			Usage:             usage,
			ProviderSessionID: string(sess.ID),
		}
		if result.Err = invocationError(ctx.Err(), promptErr, stopReason, finalText); result.Err == nil {
			emit(agent.NewMessageEndEvent(finalText, usage))
		}
		if result.Err != nil {
			logs.WarnContextf(ctx, "opencode acp invocation failed: execution_id=%s session_id=%s stop_reason=%s err=%v",
				req.ExecutionID, sess.ID, stopReason, result.Err)
		} else {
			logs.InfoContextf(ctx, "opencode acp invocation completed: execution_id=%s session_id=%s stop_reason=%s output_len=%d",
				req.ExecutionID, sess.ID, stopReason, len(finalText))
		}
		close(events)
		results <- result
	}()

	return &cli.Invocation{
		Process:   &acpProcess{conn: conn},
		Events:    events,
		Result:    results,
		Responder: permissionBridge,
		Questions: formBridge,
	}, nil
}

// promptBlocks 把文本与图片附件组装成 ACP 内容块。
// 仅有路径无字节的附件不内联，退化为一行文本说明（设计文档 §5.1）。
func promptBlocks(req cli.InvocationRequest) []acpsdk.ContentBlock {
	blocks := make([]acpsdk.ContentBlock, 0, len(req.Attachments)+1)
	blocks = append(blocks, acpsdk.TextBlock(req.Prompt))
	for _, attachment := range req.Attachments {
		if len(attachment.Data) == 0 {
			name := strings.TrimSpace(attachment.Name)
			if name == "" {
				name = "attachment"
			}
			blocks = append(blocks, acpsdk.TextBlock(fmt.Sprintf("\n[附件 %s 未内联，仅提供文件名]", name)))
			continue
		}
		mimeType := strings.TrimSpace(attachment.MIME)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		blocks = append(blocks, acpsdk.ImageBlock(base64.StdEncoding.EncodeToString(attachment.Data), mimeType))
	}
	return blocks
}

// emitPlanAware 在计划模式下把 formBridge 的 question 事件包装成
// 「先 plan.ready、再 interaction_type=plan_confirmation 的 question」。
// 返回 confirmed 表示本轮确实产出了计划确认表单（供一次性兜底追问判断）。
func emitPlanAware(
	emit func(agent.NodeEvent) error,
	translator *acp.Translator,
	workDir, sessionID string,
	mode agent.ExecutionMode,
	event agent.NodeEvent,
) (bool, error) {
	if mode != agent.ExecutionModePlan {
		return false, emit(event)
	}
	payload, ok := event.Payload.(*agent.QuestionAskedPayload)
	if !ok {
		return false, emit(event)
	}
	planPath, _, hasPlan := translator.PlanArtifact()
	readyEvents, marked, confirmed := PlanConfirmationBundle(workDir, planPath, sessionID, mode, *payload)
	if !confirmed {
		logs.Warnf("plan_contract_miss: execution session=%s questions=%d has_plan=%v",
			sessionID, len(payload.Questions), hasPlan)
		return false, emit(event)
	}
	for _, ready := range readyEvents {
		if err := emit(ready); err != nil {
			return false, err
		}
	}
	event.Payload = &marked
	return true, emit(event)
}

// acpProcess 把 Conn 适配成 cli.Invocation 需要的最小进程句柄。
type acpProcess struct {
	conn *acp.Conn
}

func (p *acpProcess) PID() int { return p.conn.PID() }

func (p *acpProcess) Stop() error { return p.conn.Close() }

// acpMCPServers 把 Lework 的 MCP 配置转换为 session/new 的 mcpServers。
func acpMCPServers(configs []agent.MCPServerConfig) []acpsdk.McpServer {
	servers := make([]acpsdk.McpServer, 0, len(configs))
	for _, config := range configs {
		name := strings.TrimSpace(config.Name)
		if name == "" {
			continue
		}
		switch {
		case runtimeprocess.NeedsSSEBridge(config):
			// opencode v2.1.4 的 mcpCapabilities.sse=false，不能直接下发 SSE；
			// 沿用 v1 的 mcp-remote stdio 桥，请求头仍走环境变量避免凭据落盘。
			headerArgs, headerEnv := runtimeprocess.BuildMCPRemoteHeaderArgs(config)
			args := []string{"-y", "mcp-remote", config.URL, "--transport", "sse-only"}
			args = append(args, headerArgs...)
			environment := make([]acpsdk.EnvVariable, 0, len(headerEnv))
			for key, value := range headerEnv {
				environment = append(environment, acpsdk.EnvVariable{Name: key, Value: value})
			}
			servers = append(servers, acpsdk.McpServer{Stdio: &acpsdk.McpServerStdio{
				Name:    name,
				Command: "npx",
				Args:    args,
				Env:     environment,
			}})
		case strings.TrimSpace(config.URL) != "":
			headers := make([]acpsdk.HttpHeader, 0, len(config.Headers)+1)
			for key, value := range config.Headers {
				headers = append(headers, acpsdk.HttpHeader{Name: key, Value: value})
			}
			if token := strings.TrimSpace(config.BearerToken); token != "" {
				headers = append(headers, acpsdk.HttpHeader{Name: "Authorization", Value: "Bearer " + token})
			}
			servers = append(servers, acpsdk.McpServer{Http: &acpsdk.McpServerHttpInline{
				Name:    name,
				Type:    "http",
				Url:     config.URL,
				Headers: headers,
			}})
		case strings.TrimSpace(config.Command) != "":
			environment := make([]acpsdk.EnvVariable, 0, len(config.Env))
			for key, value := range config.Env {
				environment = append(environment, acpsdk.EnvVariable{Name: key, Value: value})
			}
			servers = append(servers, acpsdk.McpServer{Stdio: &acpsdk.McpServerStdio{
				Name:    name,
				Command: config.Command,
				Args:    config.Args,
				Env:     environment,
			}})
		}
	}
	return servers
}

var _ cli.Invoker = (*ACPInvoker)(nil)

// invocationError 是终态判定：取消/协议错误/拒答/拒绝工具/空正文/回声都映射为可读错误。
// 单独抽成纯函数，便于对 execution.failed 契约做回放/单测。
//
// 拒绝与用户主动停止的区分靠 ctxErr：用户点停止会让 ctx 取消（agentrun 据此落
// RunStatusCancelled），而拒绝工具时 ctx 仍然存活，仅 stopReason=cancelled。
func invocationError(ctxErr, promptErr error, stopReason acpsdk.StopReason, finalText string) error {
	switch {
	case promptErr != nil:
		return promptErr
	case ctxErr != nil:
		return ctxErr
	case stopReason == acpsdk.StopReasonRefusal:
		return fmt.Errorf("模型拒绝回答本次请求，请调整提问后重试。")
	case stopReason == acpsdk.StopReasonCancelled && strings.TrimSpace(finalText) == "":
		return fmt.Errorf("%s", rejectedReply)
	case strings.TrimSpace(finalText) == "":
		return fmt.Errorf("%s", fallbackReply)
	case strings.HasPrefix(strings.TrimSpace(finalText), "【用户问题】"):
		return fmt.Errorf("%s", fallbackReply)
	default:
		return nil
	}
}

func usageFromACP(usage *acpsdk.Usage) *agent.Usage {
	if usage == nil {
		logs.Warnf("opencode acp returned nil usage; falling back to zero usage")
		return &agent.Usage{}
	}
	result := &agent.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.InputTokens + usage.OutputTokens,
	}
	if usage.CachedReadTokens != nil {
		result.CacheInputTokens = *usage.CachedReadTokens
	}
	if usage.CachedWriteTokens != nil {
		result.CacheOutputTokens = *usage.CachedWriteTokens
	}
	return result
}

// addUsage 把 extra 累加到 base，用于把兜底追问那一轮的 token 计费算进同一次执行。
func addUsage(base, extra *agent.Usage) {
	if base == nil || extra == nil {
		return
	}
	base.InputTokens += extra.InputTokens
	base.OutputTokens += extra.OutputTokens
	base.TotalTokens += extra.TotalTokens
	base.CacheInputTokens += extra.CacheInputTokens
	base.CacheOutputTokens += extra.CacheOutputTokens
}
