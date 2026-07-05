package codex

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
	runtimeprocess "github.com/insmtx/Leros/backend/agent/runtime/internal/process"
	"github.com/ygpkg/yg-go/logs"
)

// ============================================================================
// Invoker
// ============================================================================

type AppServerInvoker struct {
	binary  string
	baseEnv []string
}

func NewAppServerInvoker(binary string, extraEnv map[string]string) *AppServerInvoker {
	return &AppServerInvoker{
		binary:  binary,
		baseEnv: runtimeprocess.BuildBaseEnv(extraEnv),
	}
}

func (inv *AppServerInvoker) Invoke(ctx context.Context, req cli.InvocationRequest) (*cli.Invocation, error) {
	workDir := strings.TrimSpace(req.WorkDir)

	srv, err := startAppServer(ctx, inv.binary, workDir, inv.baseEnv, req.Model, req.MCPServers, req.TaskDir)
	if err != nil {
		return nil, fmt.Errorf("start app-server for %s: %w", workDir, err)
	}

	evtChan := make(chan agent.NodeEvent, 64)
	resultChan := make(chan cli.InvocationResult, 1)
	srv.SetEventChannel(evtChan)

	st := &runState{
		srv:        srv,
		evtChan:    evtChan,
		resultChan: resultChan,
	}
	st.turnDone = make(chan turnResult, 1)

	srv.onNotification = st.handleNotification
	srv.onServerRequest = st.handleServerRequest

	// -- 会话管理 --
	threadID, err := st.ensureThread(ctx, req)
	if err != nil {
		_ = srv.Close()
		close(evtChan)
		return nil, err
	}

	// -- 开始 turn --
	tid, err := srv.StartTurn(ctx, threadID, req.Prompt)
	if err != nil {
		_ = srv.Close()
		close(evtChan)
		return nil, fmt.Errorf("start turn: %w", err)
	}
	_ = tid

	// -- 后台等待 turn 完成 --
	go st.waitTurnDone(ctx)

	return st.buildHandle(req)
}

func (st *runState) buildHandle(req cli.InvocationRequest) (*cli.Invocation, error) {
	responder := &appServerResponder{srv: st.srv}
	return &cli.Invocation{
		Process:   st.srv,
		Events:    st.evtChan,
		Result:    st.resultChan,
		Responder: responder,
	}, nil
}

// ============================================================================
// runState — 单次 Run 的上下文
// ============================================================================

type runState struct {
	srv        *AppServer
	evtChan    chan agent.NodeEvent
	resultChan chan cli.InvocationResult

	mu                sync.Mutex
	turnID            string
	msgCount          int // agentMessage 类型的 item/started 计数，用于生成跨轮唯一的消息 ID
	assistantText     strings.Builder
	currentDiff       strings.Builder
	messageID         string
	tokenUsage        *agent.Usage
	providerSessionID string
	turnDone          chan turnResult
}

type turnResult struct {
	completed  bool
	failed     bool
	errorMsg   string
	message    string
	diff       string
	tokenUsage any
}

// makeMessageID 组合 msgCount 和 itemID 生成跨轮唯一的 provider message ID。
func (st *runState) makeMessageID(itemID string) string {
	return fmt.Sprintf("%d_%s", st.msgCount, itemID)
}

// ============================================================================
// 通知处理
// ============================================================================

func (st *runState) handleNotification(method string, params sonic.NoCopyRawMessage) {
	logs.Infof("Codex notification: method=%s params=%s", method, string(params))
	st.mu.Lock()
	defer st.mu.Unlock()

	switch method {
	case "thread/started":
		st.onThreadStarted(params)
	case "turn/started":
		st.onTurnStarted(params)
	case "item/started":
		st.onItemStarted(params)
	case "item/completed":
		st.onItemCompleted(params)
	case "item/agentMessage/delta":
		st.onAgentDelta(params)
	case "turn/diff":
		st.onTurnDiff(params)
	case "turn/completed":
		st.onTurnCompleted(params)
	case "hook/started", "hook/completed":
		st.onHook(method, params)
	case "turn/plan/updated":
		st.onPlanUpdated(params)
	case "thread/tokenUsage/updated":
		st.onTokenUsage(params)
	case "thread/status/changed":
		logs.Debugf("Thread status changed: %s", string(params))
	}
}

func (st *runState) onThreadStarted(params sonic.NoCopyRawMessage) {
	var payload struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := sonic.Unmarshal(params, &payload); err == nil && payload.Thread.ID != "" {
		st.srv.SetThreadID(payload.Thread.ID)
		st.providerSessionID = payload.Thread.ID
		sendNodeEventTo(st.evtChan, agent.NewAgentStartEvent(payload.Thread.ID))
	}
}

func (st *runState) onTurnStarted(params sonic.NoCopyRawMessage) {
	var payload struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := sonic.Unmarshal(params, &payload); err == nil {
		st.turnID = payload.Turn.ID
		st.srv.SetTurnID(st.turnID)
	}
}

func (st *runState) onItemStarted(params sonic.NoCopyRawMessage) {
	var payload struct {
		Item struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Command string `json:"command"`
			CWD     string `json:"cwd"`
		} `json:"item"`
	}
	if err := sonic.Unmarshal(params, &payload); err != nil {
		return
	}
	switch payload.Item.Type {
	case "agentMessage":
		if payload.Item.ID != "" {
			st.msgCount++
			st.messageID = st.makeMessageID(payload.Item.ID)
		}
	case "commandExecution", "fileChange":
		sendEventPayloadTo(st.evtChan, agent.NodeEventToolExecutionStart,
			&agent.ToolExecutionStartPayload{
				ToolCallID: st.makeMessageID(payload.Item.ID),
				Name:       "Command",
				Arguments:  agent.MarshalRawJSON(map[string]string{"command": payload.Item.Command, "cwd": payload.Item.CWD}),
			})
	}
}

func (st *runState) onItemCompleted(params sonic.NoCopyRawMessage) {
	var payload struct {
		Item struct {
			ID               string `json:"id"`
			Type             string `json:"type"`
			Output           string `json:"output"`
			AggregatedOutput string `json:"aggregatedOutput"`
			Text             string `json:"text"`
			ExitCode         *int   `json:"exitCode"`
			DurationMs       *int64 `json:"durationMs"`
		} `json:"item"`
	}
	if err := sonic.Unmarshal(params, &payload); err != nil {
		return
	}
	switch payload.Item.Type {
	case "agentMessage":
		if payload.Item.Text != "" {
			st.assistantText.Reset()
			st.assistantText.WriteString(payload.Item.Text)
		}
	case "commandExecution", "fileChange":
		st.emitToolResult(st.makeMessageID(payload.Item.ID), payload.Item.AggregatedOutput, payload.Item.Output, payload.Item.ExitCode, payload.Item.DurationMs)
	}
}

func (st *runState) emitToolResult(id, aggregated, output string, exitCode *int, durationMs *int64) {
	out := firstNonEmpty(aggregated, output)
	var elapsed int64
	if durationMs != nil {
		elapsed = *durationMs
	}
	if exitCode != nil && *exitCode != 0 {
		sendEventPayloadTo(st.evtChan, agent.NodeEventToolExecutionEnd,
			&agent.ToolExecutionEndPayload{ToolCallID: id, IsError: true, Error: out, ElapsedMS: elapsed})
	} else {
		sendEventPayloadTo(st.evtChan, agent.NodeEventToolExecutionEnd,
			&agent.ToolExecutionEndPayload{ToolCallID: id, IsError: false, Result: agent.MarshalRawJSON(out), ElapsedMS: elapsed})
	}
}

func (st *runState) onAgentDelta(params sonic.NoCopyRawMessage) {
	var payload struct {
		ItemID string `json:"itemId"`
		Delta  string `json:"delta"`
	}
	if err := sonic.Unmarshal(params, &payload); err != nil || payload.Delta == "" {
		return
	}
	if payload.ItemID != "" {
		st.messageID = st.makeMessageID(payload.ItemID)
	}
	st.assistantText.WriteString(payload.Delta)
	emitMessageDelta(st.evtChan, st.messageID, payload.Delta)
}

func (st *runState) onTurnDiff(params sonic.NoCopyRawMessage) {
	var payload struct {
		Diff string `json:"diff"`
	}
	if err := sonic.Unmarshal(params, &payload); err == nil && payload.Diff != "" {
		st.currentDiff.WriteString(payload.Diff)
		emitMessageDelta(st.evtChan, st.messageID, payload.Diff)
	}
}

func (st *runState) onTurnCompleted(params sonic.NoCopyRawMessage) {
	var payload struct {
		Turn struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
	}
	if err := sonic.Unmarshal(params, &payload); err == nil {
		if payload.Turn.Error != nil && payload.Turn.Error.Message != "" {
			st.turnDone <- turnResult{failed: true, errorMsg: payload.Turn.Error.Message, message: st.assistantText.String()}
		} else {
			st.turnDone <- turnResult{completed: true, message: st.assistantText.String(), diff: st.currentDiff.String()}
		}
	} else {
		st.turnDone <- turnResult{completed: true, message: st.assistantText.String()}
	}
}

func (st *runState) onHook(method string, params sonic.NoCopyRawMessage) {
	var payload struct {
		Run struct {
			EventName string `json:"eventName"`
		} `json:"run"`
	}
	if err := sonic.Unmarshal(params, &payload); err == nil && payload.Run.EventName != "" {
		emitMessageDelta(st.evtChan, st.messageID, fmt.Sprintf("[hook] %s: %s", method, payload.Run.EventName))
	}
}

func (st *runState) onPlanUpdated(params sonic.NoCopyRawMessage) {
	var payload struct {
		Plan []struct {
			Step   string `json:"step"`
			Status string `json:"status"`
		} `json:"plan"`
	}
	if err := sonic.Unmarshal(params, &payload); err != nil || len(payload.Plan) == 0 {
		return
	}
	items := make([]agent.RuntimeTodoItem, 0, len(payload.Plan))
	for i, p := range payload.Plan {
		items = append(items, agent.RuntimeTodoItem{
			ID:     fmt.Sprintf("plan_%d", i+1),
			Title:  p.Step,
			Status: planStatus(p.Status),
		})
	}
	sendEventPayloadTo(st.evtChan, agent.NodeEventTodoSnapshot, &agent.TodoSnapshotPayload{Items: items})
}

func planStatus(s string) string {
	switch strings.ToLower(s) {
	case "inprogress":
		return "in_progress"
	case "completed":
		return "completed"
	default:
		return "pending"
	}
}

func (st *runState) onTokenUsage(params sonic.NoCopyRawMessage) {
	var payload struct {
		TokenUsage struct {
			Total struct {
				InputTokens  int `json:"inputTokens"`
				OutputTokens int `json:"outputTokens"`
			} `json:"total"`
		} `json:"tokenUsage"`
	}
	if err := sonic.Unmarshal(params, &payload); err == nil {
		st.tokenUsage = agent.EnsureUsage(&agent.Usage{
			InputTokens:  payload.TokenUsage.Total.InputTokens,
			OutputTokens: payload.TokenUsage.Total.OutputTokens,
		})
	}
}

// ============================================================================
// 服务器请求处理（审批）
// ============================================================================

func (st *runState) handleServerRequest(req ServerRequest) {
	logs.Infof("Codex server request: method=%s id=%s params=%s", req.Method, string(req.ID), string(req.Params))
	st.srv.SetPendingApproval(&req)

	switch req.Method {
	case "item/commandExecution/requestApproval":
		var params struct {
			ItemID  string `json:"itemId"`
			Command string `json:"command"`
			Reason  string `json:"reason"`
		}
		if err := sonic.Unmarshal(req.Params, &params); err == nil {
			reqID := params.ItemID
			if reqID == "" {
				reqID = string(req.ID)
			}
			sendEventPayloadTo(st.evtChan, agent.NodeEventApprovalRequested, &agent.ApprovalRequestedPayload{
				RequestID:   reqID,
				ToolName:    "Command",
				ToolCallID:  params.ItemID,
				Description: firstNonEmpty(params.Reason, params.Command),
				Arguments:   agent.MarshalRawJSON(map[string]string{"command": params.Command}),
				Metadata:    map[string]string{"engine": "codex", "action_type": "command_execution"},
			})
		}

	case "item/fileChange/requestApproval":
		var params struct {
			ItemID    string `json:"itemId"`
			GrantRoot string `json:"grantRoot"`
			Reason    string `json:"reason"`
		}
		if err := sonic.Unmarshal(req.Params, &params); err == nil {
			reqID := params.ItemID
			if reqID == "" {
				reqID = string(req.ID)
			}
			sendEventPayloadTo(st.evtChan, agent.NodeEventApprovalRequested, &agent.ApprovalRequestedPayload{
				RequestID:   reqID,
				ToolName:    "Write",
				ToolCallID:  params.ItemID,
				Description: firstNonEmpty(params.Reason, params.GrantRoot),
				Arguments:   agent.MarshalRawJSON(map[string]string{"path": params.GrantRoot}),
				Metadata:    map[string]string{"engine": "codex", "action_type": "file_change"},
			})
		}

	case "item/permissions/requestApproval":
		sendEventPayloadTo(st.evtChan, agent.NodeEventApprovalRequested, &agent.ApprovalRequestedPayload{
			RequestID:   string(req.ID),
			ToolName:    "Permissions",
			Description: "Permission approval request",
			Metadata:    map[string]string{"engine": "codex", "action_type": "permissions"},
		})

	default:
		logs.Debugf("Unhandled server request: method=%s id=%s", req.Method, string(req.ID))
	}
}

// ============================================================================
// 会话 & turn 生命周期
// ============================================================================

func (st *runState) ensureThread(ctx context.Context, req cli.InvocationRequest) (string, error) {
	resume := req.Resume && strings.TrimSpace(req.SessionID) != ""
	if resume {
		threadID := strings.TrimSpace(req.SessionID)
		if st.srv.ThreadID() != threadID {
			if err := st.srv.ResumeThread(ctx, threadID, req.Model, req.SystemPrompt); err != nil {
				return "", fmt.Errorf("resume thread %s: %w", threadID, err)
			}
		}
		st.providerSessionID = threadID
		sendNodeEventTo(st.evtChan, agent.NewAgentStartEvent(threadID))
		return threadID, nil
	}
	tid, err := st.srv.StartThread(ctx, req.Model, req.SystemPrompt)
	if err != nil {
		return "", fmt.Errorf("start thread: %w", err)
	}
	st.providerSessionID = tid
	return tid, nil
}

func (st *runState) waitTurnDone(ctx context.Context) {
	defer close(st.evtChan)
	defer close(st.resultChan)
	defer func() {
		st.srv.onNotification = nil
		st.srv.onServerRequest = nil
		st.srv.SetEventChannel(nil)
		st.srv.SetPendingApproval(nil)
		_ = st.srv.Close()
	}()

	select {
	case <-ctx.Done():
		logs.WarnContextf(ctx, "Turn context done: %v", ctx.Err())
		st.resultChan <- cli.InvocationResult{
			Message:           st.assistantText.String(),
			Usage:             st.tokenUsage,
			ProviderSessionID: st.providerSessionID,
			Err:               ctx.Err(),
		}

	case result := <-st.turnDone:
		if result.failed {
			st.resultChan <- cli.InvocationResult{
				Message:           result.message,
				Usage:             st.tokenUsage,
				ProviderSessionID: st.providerSessionID,
				Err:               fmt.Errorf("%s", result.errorMsg),
			}
		} else if result.completed {
			finalMsg := firstNonEmpty(result.message, result.diff)
			if finalMsg != "" {
				sendNodeEventTo(st.evtChan, agent.NewMessageEndEvent(finalMsg, st.tokenUsage))
			}
			st.resultChan <- cli.InvocationResult{
				Message:           finalMsg,
				Usage:             st.tokenUsage,
				ProviderSessionID: st.providerSessionID,
			}
		}
	}
}

// ============================================================================
// 审批 Responder
// ============================================================================

type appServerResponder struct {
	srv *AppServer
}

func (r *appServerResponder) WriteDecision(requestID string, action string) error {
	decision := "cancel"
	if action == agent.ApprovalActionApprove || action == agent.ApprovalActionAlways {
		decision = "accept"
	}

	pending := r.srv.PendingApproval()
	if pending == nil {
		return fmt.Errorf("no pending approval request")
	}
	if err := r.srv.RespondApproval(context.Background(), pending.ID, decision); err != nil {
		return fmt.Errorf("respond approval: %w", err)
	}
	r.srv.SetPendingApproval(nil)
	return nil
}

// ============================================================================
// 辅助
// ============================================================================

func resolveThread(sessionID string, resume bool) (string, bool) {
	if !resume {
		return "", false
	}
	threadID := strings.TrimSpace(sessionID)
	return threadID, threadID != ""
}

func emitMessageDelta(ch chan<- agent.NodeEvent, messageID, content string) {
	if ch == nil || content == "" {
		return
	}
	select {
	case ch <- agent.NewMessageUpdateEvent(messageID, content):
	default:
	}
}

func sendNodeEventTo(ch chan<- agent.NodeEvent, event agent.NodeEvent) {
	if ch == nil {
		return
	}
	select {
	case ch <- event:
	default:
	}
}

func sendEventPayloadTo(ch chan<- agent.NodeEvent, eventType agent.NodeEventType, payload agent.NodeEventPayload) {
	if ch == nil {
		return
	}
	select {
	case ch <- agent.NodeEvent{Type: eventType, Payload: payload}:
	default:
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
