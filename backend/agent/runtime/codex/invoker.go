package codex

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/insmtx/Leros/backend/agent/runtime/externalcli"
	"github.com/insmtx/Leros/backend/agent/runtime/provider"
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
		baseEnv: provider.BuildBaseEnv(extraEnv),
	}
}

func (inv *AppServerInvoker) Invoke(ctx context.Context, req externalcli.InvocationRequest) (*externalcli.Invocation, error) {
	workDir := strings.TrimSpace(req.WorkDir)

	srv, err := startAppServer(ctx, inv.binary, workDir, inv.baseEnv, req.Model, req.MCPServers, req.TaskDir)
	if err != nil {
		return nil, fmt.Errorf("start app-server for %s: %w", workDir, err)
	}

	evtChan := make(chan agent.Event, 64)
	srv.SetEventChannel(evtChan)

	st := &runState{
		srv:     srv,
		evtChan: evtChan,
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

	sendEventTo(evtChan, events.EventInvocationStarted, "")

	// -- 后台等待 turn 完成 --
	go st.waitTurnDone(ctx)

	return st.buildHandle(req)
}

func (st *runState) buildHandle(req externalcli.InvocationRequest) (*externalcli.Invocation, error) {
	responder := &appServerResponder{srv: st.srv}
	return &externalcli.Invocation{
		Process:   st.srv,
		Events:    st.evtChan,
		Responder: responder,
	}, nil
}

// ============================================================================
// runState — 单次 Run 的上下文
// ============================================================================

type runState struct {
	srv     *AppServer
	evtChan chan agent.Event

	mu            sync.Mutex
	turnID        string
	msgCount      int // agentMessage 类型的 item/started 计数，用于生成跨轮唯一的消息 ID
	assistantText strings.Builder
	currentDiff   strings.Builder
	messageID     string
	tokenUsage    *agent.Usage
	turnDone      chan turnResult
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
		sendEventTo(st.evtChan, events.EventProviderSessionStarted, payload.Thread.ID)
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
		sendEventPayloadTo(st.evtChan, events.EventToolCallStarted,
			events.ToolCallPayload{
				ToolCallID: st.makeMessageID(payload.Item.ID),
				Name:       "Command",
				Arguments:  events.MarshalRaw(map[string]string{"command": payload.Item.Command, "cwd": payload.Item.CWD}),
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
		sendEventPayloadTo(st.evtChan, events.EventToolCallFailed,
			events.ToolCallResultPayload{ToolCallID: id, Error: out, ElapsedMS: elapsed})
	} else {
		sendEventPayloadTo(st.evtChan, events.EventToolCallCompleted,
			events.ToolCallResultPayload{ToolCallID: id, Result: events.MarshalRaw(out), ElapsedMS: elapsed})
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
	items := make([]events.RuntimeTodoItem, 0, len(payload.Plan))
	for i, p := range payload.Plan {
		items = append(items, events.RuntimeTodoItem{
			ID:     fmt.Sprintf("plan_%d", i+1),
			Title:  p.Step,
			Status: planStatus(p.Status),
		})
	}
	sendEventPayloadTo(st.evtChan, events.EventTodoSnapshot, items)
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
		st.tokenUsage = &agent.Usage{
			InputTokens:  payload.TokenUsage.Total.InputTokens,
			OutputTokens: payload.TokenUsage.Total.OutputTokens,
			TotalTokens:  payload.TokenUsage.Total.InputTokens + payload.TokenUsage.Total.OutputTokens,
		}
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
			sendEventPayloadTo(st.evtChan, events.EventApprovalRequested, events.ApprovalRequestPayload{
				RequestID:   reqID,
				ToolName:    "Command",
				ToolCallID:  params.ItemID,
				Description: firstNonEmpty(params.Reason, params.Command),
				Arguments:   events.MarshalRaw(map[string]string{"command": params.Command}),
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
			sendEventPayloadTo(st.evtChan, events.EventApprovalRequested, events.ApprovalRequestPayload{
				RequestID:   reqID,
				ToolName:    "Write",
				ToolCallID:  params.ItemID,
				Description: firstNonEmpty(params.Reason, params.GrantRoot),
				Arguments:   events.MarshalRaw(map[string]string{"path": params.GrantRoot}),
				Metadata:    map[string]string{"engine": "codex", "action_type": "file_change"},
			})
		}

	case "item/permissions/requestApproval":
		sendEventPayloadTo(st.evtChan, events.EventApprovalRequested, events.ApprovalRequestPayload{
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

func (st *runState) ensureThread(ctx context.Context, req externalcli.InvocationRequest) (string, error) {
	resume := req.Resume && strings.TrimSpace(req.SessionID) != ""
	if resume {
		threadID := strings.TrimSpace(req.SessionID)
		if st.srv.ThreadID() != threadID {
			if err := st.srv.ResumeThread(ctx, threadID, req.Model, req.SystemPrompt); err != nil {
				return "", fmt.Errorf("resume thread %s: %w", threadID, err)
			}
		}
		sendEventTo(st.evtChan, events.EventProviderSessionStarted, threadID)
		return threadID, nil
	}
	tid, err := st.srv.StartThread(ctx, req.Model, req.SystemPrompt)
	if err != nil {
		return "", fmt.Errorf("start thread: %w", err)
	}
	return tid, nil
}

func (st *runState) waitTurnDone(ctx context.Context) {
	defer close(st.evtChan)
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
		sendEventTo(st.evtChan, events.EventInvocationFailed, ctx.Err().Error())

	case result := <-st.turnDone:
		if result.failed {
			sendEventTo(st.evtChan, events.EventInvocationFailed, result.errorMsg)
		} else if result.completed {
			finalMsg := firstNonEmpty(result.message, result.diff)
			if finalMsg != "" {
				sendEventTo(st.evtChan, events.EventResult, finalMsg)
				sendEventPayloadTo(st.evtChan, events.EventResult, events.MessageResultPayload{Message: finalMsg, Usage: st.tokenUsage})
			}
			sendEventTo(st.evtChan, events.EventInvocationCompleted, finalMsg)
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
	if action == provider.ApprovalActionApprove || action == provider.ApprovalActionAlways {
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

func emitMessageDelta(ch chan<- agent.Event, messageID, content string) {
	if ch == nil || content == "" {
		return
	}
	payload, _ := sonic.Marshal(events.MessageDeltaPayload{MessageID: messageID, Content: content})
	select {
	case ch <- agent.Event{
		Type:    events.EventMessageDelta,
		Content: content,
		Payload: payload,
	}:
	default:
	}
}

func sendEventTo(ch chan<- agent.Event, eventType agent.EventType, content string) {
	if ch == nil {
		return
	}
	select {
	case ch <- agent.Event{Type: eventType, Content: content}:
	default:
	}
}

func sendEventPayloadTo(ch chan<- agent.Event, eventType agent.EventType, payload any) {
	if ch == nil {
		return
	}
	event := agent.Event{Type: eventType}
	if payload != nil {
		if encoded, err := sonic.Marshal(payload); err == nil {
			event.Payload = encoded
		}
	}
	select {
	case ch <- event:
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
