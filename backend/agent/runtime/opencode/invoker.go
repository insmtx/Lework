package opencode

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
	runtimeprocess "github.com/insmtx/Leros/backend/agent/runtime/internal/process"
	"github.com/ygpkg/yg-go/logs"
)

const defaultProgressIdleTimeout = 10 * time.Minute

// ============================================================================
// ServerInvoker — opencode serve 模式的调用器
// ============================================================================
// ServerInvoker 通过 opcode serve HTTP API 执行提示。
type ServerInvoker struct {
	binary  string
	baseEnv []string
	dataDir string
}

// NewServerInvoker 创建新的 ServerInvoker。
func NewServerInvoker(binary string, extraEnv map[string]string, dataDir string) *ServerInvoker {
	return &ServerInvoker{
		binary:  binary,
		baseEnv: runtimeprocess.BuildBaseEnv(extraEnv),
		dataDir: dataDir,
	}
}

// Run 启动 opcode serve，创建会话并执行提示。
func (inv *ServerInvoker) Invoke(ctx context.Context, req cli.InvocationRequest) (*cli.Invocation, error) {
	workDir := strings.TrimSpace(req.WorkDir)
	runEnv := buildOpenCodeInvocationEnv(inv.baseEnv, req.ExtraEnv, inv.dataDir)
	startedAt := time.Now()
	logs.InfoContextf(ctx,
		"OpenCode invocation starting: execution_id=%s trace_id=%s mode=%s model=%s resume=%v provider_session_id=%s work_dir=%s progress_timeout=%s",
		req.ExecutionID, req.TraceID, req.ExecutionMode, req.Model.Model, req.Resume, req.SessionID, workDir, defaultProgressIdleTimeout,
	)
	// 1. 启动 OpenCode 服务（healthCheckTimeout=0 使用默认 15s/次）
	srv, err := startOpenCodeServer(
		ctx,
		inv.binary,
		workDir,
		runEnv,
		req.Model,
		req.MCPServers,
		0,
		inv.dataDir,
		req.SkillDir,
	)
	if err != nil {
		logs.WarnContextf(ctx, "OpenCode invocation failed during server start: execution_id=%s elapsed=%s err=%v",
			req.ExecutionID, time.Since(startedAt).Truncate(time.Millisecond), err)
		return nil, fmt.Errorf("start opencode server for %s: %w", workDir, err)
	}
	evtChan := make(chan agent.NodeEvent, 64)
	resultChan := make(chan cli.InvocationResult, 1)
	st := &runState{
		srv:               srv,
		evtChan:           evtChan,
		resultChan:        resultChan,
		executionID:       req.ExecutionID,
		traceID:           req.TraceID,
		mode:              req.ExecutionMode,
		model:             req.Model.Model,
		startedAt:         startedAt,
		workDir:           workDir,
		filteredToolCalls: make(map[string]string),
		activeToolCalls:   make(map[string]string),
		sseDone:           make(chan struct{}),
		msgDone:           make(chan struct{}),
		sseTerminal:       make(chan struct{}),
		progressCh:        make(chan struct{}, 1),
		progressTimeout:   defaultProgressIdleTimeout,
	}
	// 2. 会话管理
	logs.InfoContextf(ctx, "OpenCode session phase starting: execution_id=%s resume=%v provider_session_id=%s", req.ExecutionID, req.Resume, req.SessionID)
	sessionID, err := st.ensureSession(ctx, req)
	if err != nil {
		_ = srv.Stop()
		close(evtChan)
		logs.WarnContextf(ctx, "OpenCode invocation failed during session phase: execution_id=%s elapsed=%s err=%v",
			req.ExecutionID, time.Since(startedAt).Truncate(time.Millisecond), err)
		return nil, err
	}
	st.sessionID = sessionID
	logs.InfoContextf(ctx, "OpenCode session phase complete: execution_id=%s session_id=%s elapsed=%s",
		req.ExecutionID, sessionID, time.Since(startedAt).Truncate(time.Millisecond))
	// 3. 启动 SSE 事件流（在发送消息之前启动，避免丢失事件）
	logs.InfoContextf(ctx, "OpenCode SSE phase starting: execution_id=%s session_id=%s work_dir=%s", req.ExecutionID, sessionID, workDir)
	sseCtx, cancelSSE := context.WithCancel(ctx)
	sseCh, err := srv.ConnectSSE(sseCtx, workDir)
	if err != nil {
		cancelSSE()
		_ = srv.Stop()
		close(evtChan)
		logs.WarnContextf(ctx, "OpenCode invocation failed during SSE connect: execution_id=%s session_id=%s elapsed=%s err=%v",
			req.ExecutionID, sessionID, time.Since(startedAt).Truncate(time.Millisecond), err)
		return nil, fmt.Errorf("connect SSE: %w", err)
	}
	logs.InfoContextf(ctx, "OpenCode SSE phase complete: execution_id=%s session_id=%s elapsed=%s",
		req.ExecutionID, sessionID, time.Since(startedAt).Truncate(time.Millisecond))
	go st.processSSEStream(sseCtx, sseCh)
	// 4. 发送消息并等待同步响应
	logs.InfoContextf(ctx, "OpenCode message phase starting: execution_id=%s session_id=%s agent=%s model=%s prompt_len=%d system_prompt_len=%d",
		req.ExecutionID, sessionID, openCodeAgent(req.ExecutionMode), req.Model.Model, len(req.Prompt), len(strings.TrimSpace(req.SystemPrompt)))
	messageCtx, cancelMessage := context.WithCancel(ctx)
	go st.sendAndProcessMessage(messageCtx, req)
	// 5. 后台等待完成并清理
	go st.waitCompletion(ctx, cancelMessage, cancelSSE)
	return st.buildHandle(req)
}

func buildOpenCodeInvocationEnv(baseEnv, extraEnv []string, dataDir string) []string {
	entries := append([]string(nil), extraEnv...)
	if configDir := strings.TrimSpace(dataDir); configDir != "" {
		entries = append(entries, "OPENCODE_CONFIG_DIR="+configDir)
	}
	entries = append(entries, "OPENCODE_DISABLE_PROJECT_CONFIG=1", "OPENCODE_DISABLE_CLAUDE_CODE=1")
	return runtimeprocess.BuildRunEnv(baseEnv, entries, nil)
}

// ============================================================================
// runState — 单次 Run 的上下文
// ============================================================================
type runState struct {
	srv               *OpenCodeServer
	evtChan           chan agent.NodeEvent
	resultChan        chan cli.InvocationResult
	mu                sync.Mutex
	executionID       string
	traceID           string
	mode              agent.ExecutionMode
	model             string
	startedAt         time.Time
	sessionID         string
	messageID         string
	lastTextEnded     string
	tokenUsage        *agent.Usage
	workDir           string
	session           *sessionResponse
	filteredToolCalls map[string]string
	activeToolCalls   map[string]string
	reasoningParts    map[string]struct{} // reasoning partID 集合，用于 message.part.delta 过滤
	sseDone           chan struct{}
	msgDone           chan struct{}
	progressCh        chan struct{}
	progressTimeout   time.Duration

	// 本次调用期间从 SSE 失败事件（session.error / step-finish error part）提取的错误文本。
	// 优先生效先到达的错误；后续错误不影响。
	runErr string
	// sseTerminal 仅在 SSE 流收到 session.error 后关闭。
	sseTerminal chan struct{}
}

func (st *runState) buildHandle(_ cli.InvocationRequest) (*cli.Invocation, error) {
	return &cli.Invocation{
		Process:   st.srv,
		Events:    st.evtChan,
		Result:    st.resultChan,
		Responder: &serverResponder{srv: st.srv},
		Questions: &questionResponder{srv: st.srv},
	}, nil
}

// ============================================================================
// 会话管理
// ============================================================================
func (st *runState) ensureSession(ctx context.Context, req cli.InvocationRequest) (string, error) {
	// Resume 模式：复用已有 sessionID
	if req.Resume && strings.TrimSpace(req.SessionID) != "" {
		sessionID := strings.TrimSpace(req.SessionID)
		logs.Debugf("OpenCode resume session lookup: execution_id=%s session_id=%s", req.ExecutionID, sessionID)
		session, err := st.srv.GetSession(ctx, sessionID)
		if err != nil {
			logs.WarnContextf(ctx, "OpenCode get resumed session metadata failed: %v", err)
		} else {
			st.session = session
		}
		sendEventDirect(st.evtChan, agent.NewAgentStartEvent(sessionID))
		logs.InfoContextf(ctx, "OpenCode resuming session: execution_id=%s session_id=%s", req.ExecutionID, sessionID)
		return sessionID, nil
	}
	// 新会话
	title := req.ExecutionID
	if title == "" {
		title = "Leros Task"
	}
	logs.Debugf("OpenCode create session request: execution_id=%s title=%s model=%s", req.ExecutionID, title, req.Model.Model)
	session, err := st.srv.CreateSession(ctx, title, providerID, req.Model.Model, req.SystemPrompt)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	sendEventDirect(st.evtChan, agent.NewAgentStartEvent(session.ID))
	st.sessionID = session.ID
	st.session = session
	return session.ID, nil
}

// ============================================================================
// 消息发送
// ============================================================================
func (st *runState) sendAndProcessMessage(ctx context.Context, req cli.InvocationRequest) {
	startedAt := time.Now()
	defer close(st.msgDone)
	msgReq := messageRequest{
		Model: &sessionModelRef{
			ProviderID: providerID,
			ModelID:    req.Model.Model,
		},
		System: req.SystemPrompt,
		Agent:  openCodeAgent(req.ExecutionMode),
		Parts:  buildMessageParts(req.Prompt, req.UploadRelDir, req.Attachments),
	}
	msgResp, err := st.srv.SendMessage(ctx, st.sessionID, msgReq)
	if err != nil {
		// 终态事件或上层取消会主动取消请求，不应覆盖真实的运行错误。
		if ctx.Err() == nil {
			msg := err.Error()
			st.mu.Lock()
			if st.runErr == "" {
				st.runErr = msg
			}
			st.mu.Unlock()
			logs.Errorf("OpenCode send message failed: execution_id=%s session_id=%s elapsed=%s err=%v",
				req.ExecutionID, st.sessionID, time.Since(startedAt).Truncate(time.Millisecond), err)
		} else {
			logs.WarnContextf(ctx, "OpenCode send message cancelled: execution_id=%s session_id=%s elapsed=%s err=%v",
				req.ExecutionID, st.sessionID, time.Since(startedAt).Truncate(time.Millisecond), ctx.Err())
		}
		return
	}
	st.mu.Lock()
	st.messageID = msgResp.Info.ID
	// 关键修复：从同步响应体的 parts 中提取 assistant 的完整文本作为最终兜底。
	// 此前依赖 SSE 的 message.part.updated(text) 事件回填 lastTextEnded，但该事件
	// 与 SendMessage 同步返回（msgDone）之间存在竞态——msgDone 触发后 cancelSSE
	// 可能丢弃尚未消费的 assistant text part 事件，导致 lastTextEnded 停留在用户
	// 输入（回声误判）或为空。同步响应体里已携带有完整 assistant 文本，直接采用，
	// 从根本上规避 SSE 竞态。
	if text := assistantTextFromParts(msgResp.Parts); text != "" {
		st.lastTextEnded = text
		logs.Infof("[opencode][forensic] lastTextEnded from sync response: execution_id=%s session_id=%s message_id=%s text_len=%d",
			req.ExecutionID, st.sessionID, msgResp.Info.ID, len(text))
	}
	st.mu.Unlock()
	// FORENSIC: 同步 message 响应返回的事件时序
	logs.Infof("[opencode][forensic] SendMessage sync returned: execution_id=%s session_id=%s message_id=%s role=%s elapsed=%s",
		req.ExecutionID, st.sessionID, msgResp.Info.ID, msgResp.Info.Role, time.Since(startedAt).Truncate(time.Millisecond))
	logs.InfoContextf(ctx, "OpenCode send message completed: execution_id=%s session_id=%s message_id=%s elapsed=%s",
		req.ExecutionID, st.sessionID, msgResp.Info.ID, time.Since(startedAt).Truncate(time.Millisecond))
	// 响应事件由 SSE 流式路径处理，同步响应体中的 parts 不再处理
}

func openCodeAgent(mode agent.ExecutionMode) string {
	if mode == agent.ExecutionModePlan {
		return "plan"
	}
	return "build"
}

// assistantTextFromParts 从消息响应的 parts 中提取首个非空 text part 的完整文本。
// opencode /session/:id/message 同步响应体里包含最后一轮 assistant 的完整文本；
// 此处作为 lastTextEnded 的可靠来源，规避 SSE 事件竞态丢失的风险。
func assistantTextFromParts(parts []messagePartResp) string {
	for _, p := range parts {
		if p.Type != "text" {
			continue
		}
		if text := strings.TrimSpace(p.Text); text != "" {
			return p.Text
		}
	}
	return ""
}

// buildMessageParts 组装用户消息的 parts。纯文本始终作为第一个 text part，
// 随后按顺序追加多模态（当前仅图片）附件为 file part（data: base64 内联）。
// PDF/音视频已在 preparer 层退出多模态管线，不会带 Data 进入本函数。
// file part 的 Filename 优先使用 uploadRelDir 与附件 Name 拼接的工作区相对路径
// （与落盘一致，例如 uploads/头像.jpeg），以保证 opencode read 等工具能按该路径
// 定位到真实文件；uploadRelDir 为空时回退为仅用 Name。仅有 Name 而无 Data 的
// 大文件不在此内联，由上层以文本提示其路径。
func buildMessageParts(prompt string, uploadRelDir string, attachments []agent.Attachment) []messagePart {
	parts := []messagePart{{Type: "text", Text: prompt}}
	for _, att := range attachments {
		if len(att.Data) == 0 {
			continue
		}
		name := strings.TrimSpace(att.Name)
		relDir := strings.TrimSpace(uploadRelDir)
		filename := name
		if relDir != "" && name != "" {
			filename = relDir + "/" + name
		}
		parts = append(parts, messagePart{
			Type:     "file",
			MIME:     strings.TrimSpace(att.MIME),
			Filename: filename,
			URL:      "data:" + strings.TrimSpace(att.MIME) + ";base64," + base64.StdEncoding.EncodeToString(att.Data),
		})
		logs.Infof("[forensic][msgparts] appended file part: name=%q filename=%q mime=%q data_len=%d", name, filename, strings.TrimSpace(att.MIME), len(att.Data))
	}
	return parts
}

// ============================================================================
// SSE 事件流处理
// ============================================================================
func (st *runState) processSSEStream(ctx context.Context, ch <-chan sseEvent) {
	logs.Debugf("OpenCode SSE processor started: execution_id=%s session_id=%s", st.executionID, st.sessionID)
	defer close(st.sseDone)
	defer logs.Debugf("OpenCode SSE processor stopped: execution_id=%s session_id=%s", st.executionID, st.sessionID)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			st.recordProgress(event)
			st.handleSSEEvent(ctx, event)
		}
	}
}

func (st *runState) recordProgress(event sseEvent) {
	if st == nil || st.progressCh == nil || !isProgressEvent(event.Type) {
		return
	}
	select {
	case st.progressCh <- struct{}{}:
	default:
	}
}

func isProgressEvent(eventType string) bool {
	switch eventType {
	case "", "server.heartbeat", "server.connected", "session.idle":
		return false
	default:
		return true
	}
}

// ============================================================================
// 完成等待和清理
// ============================================================================
func (st *runState) waitCompletion(ctx context.Context, cancelMessage, cancelSSE context.CancelFunc) {
	logs.Debugf("OpenCode completion watcher started: execution_id=%s session_id=%s progress_timeout=%s",
		st.executionID, st.sessionID, st.progressTimeout)
	defer close(st.evtChan)
	defer close(st.resultChan)
	defer cancelMessage()
	defer cancelSSE()
	defer func() {
		if st.srv != nil {
			_ = st.srv.Stop()
		}
	}()

	// 正常完成以同步 message 请求返回（msgDone）为准；
	// session.error 可以立即终止仍在进行的消息请求；
	// session.idle 不再作为终态信号（question 流程中模型暂停提问时也会触发 idle）。
	progressTimeout := st.progressTimeout
	if progressTimeout <= 0 {
		progressTimeout = defaultProgressIdleTimeout
	}
	progressTimer := time.NewTimer(progressTimeout)
	defer progressTimer.Stop()
	for {
		select {
		case <-ctx.Done():
			// 外部取消：立即终止一切
			logs.Errorf("OpenCode run cancelled: execution_id=%s session_id=%s elapsed=%s err=%v",
				st.executionID, st.sessionID, time.Since(st.startedAt).Truncate(time.Millisecond), ctx.Err())
			cancelMessage()
			cancelSSE()
			st.abortSession()
			st.resultChan <- cli.InvocationResult{
				ProviderSessionID: st.sessionID,
				Err:               ctx.Err(),
			}
			return

		case <-st.sseTerminal:
			// session.error：立即取消消息请求
			logs.Warnf("OpenCode completion watcher received SSE terminal: execution_id=%s session_id=%s elapsed=%s",
				st.executionID, st.sessionID, time.Since(st.startedAt).Truncate(time.Millisecond))
			cancelMessage()
			goto complete

		case <-st.msgDone:
			logs.Debugf("OpenCode completion watcher received message completion: execution_id=%s session_id=%s elapsed=%s",
				st.executionID, st.sessionID, time.Since(st.startedAt).Truncate(time.Millisecond))
			// FORENSIC: msgDone 触发时 lastTextEnded 的当前值与时序
			st.mu.Lock()
			lt := st.lastTextEnded
			ltHead := lt
			if len(ltHead) > 60 {
				ltHead = ltHead[:60]
			}
			forensicRunErr := st.runErr
			logs.Infof("[opencode][forensic] msgDone fired: execution_id=%s session_id=%s message_id=%s lastTextEnded_len=%d head=%q begins_with_user=%v runErr=%q",
				st.executionID, st.sessionID, st.messageID, len(lt), ltHead, strings.HasPrefix(lt, "【用户问题】"), st.runErr)
			_ = forensicRunErr
			st.mu.Unlock()
			// 正常完成路径：给 SSE 流一个短窗口收集 trailing error
			st.mu.Lock()
			hasRunErr := st.runErr != ""
			st.mu.Unlock()
			if !hasRunErr {
				select {
				case <-st.sseTerminal:
				case <-st.sseDone:
				case <-ctx.Done():
					st.resultChan <- cli.InvocationResult{
						ProviderSessionID: st.sessionID,
						Err:               ctx.Err(),
					}
					return
				case <-time.After(3 * time.Second):
				}
			}
			goto complete
		case <-progressTimer.C:
			err := fmt.Errorf("长时间未操作，任务已暂停，如需继续请输入\"继续\"。")
			logs.Warnf("%v: execution_id=%s session_id=%s elapsed=%s", err, st.executionID, st.sessionID, time.Since(st.startedAt).Truncate(time.Millisecond))
			cancelMessage()
			cancelSSE()
			st.abortSession()
			st.resultChan <- cli.InvocationResult{
				ProviderSessionID: st.sessionID,
				Err:               err,
			}
			return
		case <-st.progressCh:
			if !progressTimer.Stop() {
				select {
				case <-progressTimer.C:
				default:
				}
			}
			progressTimer.Reset(progressTimeout)
		}
	}

	// cleanup: 取消 SSE 并等待 goroutine 退出
complete:
	cancelSSE()
	select {
	case <-st.sseDone:
	case <-time.After(5 * time.Second):
		logs.Warnf("OpenCode SSE stream did not close within 5s after cancel, proceeding anyway")
	}
	st.emitMissingToolCompletions()

	st.mu.Lock()
	hasRunErr := st.runErr != ""
	runErr := st.runErr
	finalText := st.lastTextEnded
	usage := st.tokenUsage
	st.mu.Unlock()

	// FORENSIC: 最终将作为 Result.Message 发布的文本
	ftHead := finalText
	if len(ftHead) > 60 {
		ftHead = ftHead[:60]
	}
	logs.Infof("[opencode][forensic] final text captured: execution_id=%s session_id=%s message_id=%s run_err=%v lastTextEnded_len=%d head=%q begins_with_user=%v",
		st.executionID, st.sessionID, st.messageID, hasRunErr, len(finalText), ftHead, strings.HasPrefix(finalText, "【用户问题】"))

	if hasRunErr {
		logs.Warnf("OpenCode invocation completed with error: execution_id=%s session_id=%s elapsed=%s err=%s",
			st.executionID, st.sessionID, time.Since(st.startedAt).Truncate(time.Millisecond), runErr)
		st.resultChan <- cli.InvocationResult{
			ProviderSessionID: st.sessionID,
			Err:               fmt.Errorf("%s", runErr),
		}
		return
	}
	// 回声兜底：opencode 在部分多模态路径下（如大图被内部重编码时）不通过 SSE
	// 上报生成的文本，导致 lastTextEnded 停留在用户输入 part。此时若最终文本
	// 是用户输入（prompt）的逐字回声，而非真实回复，视为本次调用失败以便上层
	// 触发重试，避免把无意义的回声当作 assistant 回复落库。
	if strings.HasPrefix(strings.TrimSpace(finalText), "【用户问题】") {
		// 温和的中文用户提示，不透露底层引擎(opencode)与内部技术细节。
		// 日志仍保留英文技术描述便于排障。
		errMsg := "抱歉，本次未生成有效回复，请稍后重试或换个提问方式。"
		logs.Warnf("[opencode][echo-guard] blocked echo reply (opencode returned the user prompt as its answer, no assistant text streamed): execution_id=%s session_id=%s message_id=%s output_len=%d",
			st.executionID, st.sessionID, st.messageID, len(finalText))
		st.resultChan <- cli.InvocationResult{
			ProviderSessionID: st.sessionID,
			Err:               fmt.Errorf("%s", errMsg),
		}
		return
	}
	if finalText != "" {
		sendEventDirect(st.evtChan, agent.NewMessageEndEvent(finalText, usage))
	}
	logs.Infof("OpenCode invocation completed successfully: execution_id=%s session_id=%s message_id=%s output_len=%d elapsed=%s",
		st.executionID, st.sessionID, st.messageID, len(finalText), time.Since(st.startedAt).Truncate(time.Millisecond))
	st.resultChan <- cli.InvocationResult{
		Message:           finalText,
		Usage:             usage,
		ProviderSessionID: st.sessionID,
	}
}

func (st *runState) abortSession() {
	if st == nil || st.srv == nil || strings.TrimSpace(st.sessionID) == "" {
		return
	}
	if err := st.srv.Abort(context.Background(), st.sessionID); err != nil {
		logs.Warnf("OpenCode abort session failed: session_id=%s err=%v", st.sessionID, err)
	}
}

// emitMissingToolCompletions closes tool calls whose completed SSE event was
// lost after OpenCode already returned the final assistant message. The
// message request is synchronous, so at this point OpenCode has finished its
// tool loop; the synthetic result only repairs observability and explicitly
// says that the original tool output was unavailable.
func (st *runState) emitMissingToolCompletions() {
	st.mu.Lock()
	missing := make(map[string]string, len(st.activeToolCalls))
	for callID, name := range st.activeToolCalls {
		missing[callID] = name
	}
	st.activeToolCalls = make(map[string]string)
	st.mu.Unlock()

	for callID, name := range missing {
		sendEventPayloadTo(st.evtChan, agent.NodeEventToolExecutionEnd, &agent.ToolExecutionEndPayload{
			ToolCallID: callID,
			Name:       name,
			IsError:    false,
			Result: agent.MarshalRawJSON(map[string]string{
				"status":  "completed",
				"message": "OpenCode 未返回工具原始输出，已根据最终消息结束本次调用",
			}),
		})
		logs.Warnf("[opencode] synthesized missing tool completion: execution_id=%s session_id=%s tool=%s call_id=%s",
			st.executionID, st.sessionID, name, callID)
	}
}

// ============================================================================
// 辅助函数
// ============================================================================
// emitMessageDelta 发送消息增量事件到通道。
func emitMessageDelta(ch chan<- agent.NodeEvent, messageID, content string) {
	if ch == nil || content == "" {
		return
	}
	select {
	case ch <- agent.NewMessageUpdateEvent(messageID, content):
	default:
	}
}

// sendEventPayloadTo 发送带 payload 的事件到通道。
func sendEventPayloadTo(ch chan<- agent.NodeEvent, eventType agent.NodeEventType, payload agent.NodeEventPayload) {
	if ch == nil {
		return
	}
	select {
	case ch <- agent.NodeEvent{Type: eventType, Payload: payload}:
	default:
	}
}

// sendEventDirect 直接发送已有的事件到通道。
func sendEventDirect(ch chan<- agent.NodeEvent, evt agent.NodeEvent) {
	if ch == nil {
		return
	}
	select {
	case ch <- evt:
	default:
	}
}
