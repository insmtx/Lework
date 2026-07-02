package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/insmtx/Leros/backend/agent/runtime/externalcli"
	"github.com/insmtx/Leros/backend/agent/runtime/provider"
	"github.com/ygpkg/yg-go/logs"
)

// ============================================================================
// ServerInvoker — opencode serve 模式的调用器
// ============================================================================
// ServerInvoker 通过 opcode serve HTTP API 执行提示。
type ServerInvoker struct {
	binary  string
	baseEnv []string
}

// NewServerInvoker 创建新的 ServerInvoker。
func NewServerInvoker(binary string, extraEnv map[string]string) *ServerInvoker {
	return &ServerInvoker{
		binary:  binary,
		baseEnv: provider.BuildBaseEnv(extraEnv),
	}
}

// Run 启动 opcode serve，创建会话并执行提示。
func (inv *ServerInvoker) Invoke(ctx context.Context, req externalcli.InvocationRequest) (*externalcli.Invocation, error) {
	workDir := strings.TrimSpace(req.WorkDir)
	// 1. 启动 OpenCode 服务（healthCheckTimeout=0 使用默认 30s）
	srv, err := startOpenCodeServer(ctx, inv.binary, workDir, inv.baseEnv, req.Model, req.MCPServers, 0)
	if err != nil {
		return nil, fmt.Errorf("start opencode server for %s: %w", workDir, err)
	}
	evtChan := make(chan agent.Event, 64)
	st := &runState{
		srv:               srv,
		evtChan:           evtChan,
		workDir:           workDir,
		filteredToolCalls: make(map[string]string),
		sseDone:           make(chan struct{}),
		msgDone:           make(chan struct{}),
		sseTerminal:       make(chan struct{}),
	}
	// 2. 会话管理
	logs.Infof("OpenCode creating/resuming session...")
	sessionID, err := st.ensureSession(ctx, req)
	if err != nil {
		_ = srv.Stop()
		close(evtChan)
		return nil, err
	}
	st.sessionID = sessionID
	logs.Infof("OpenCode session ready: id=%s", sessionID)
	// 3. 启动 SSE 事件流（在发送消息之前启动，避免丢失事件）
	logs.Infof("OpenCode connecting SSE stream...")
	sseCtx, cancelSSE := context.WithCancel(ctx)
	sseCh, err := srv.ConnectSSE(sseCtx, workDir)
	if err != nil {
		cancelSSE()
		_ = srv.Stop()
		close(evtChan)
		return nil, fmt.Errorf("connect SSE: %w", err)
	}
	logs.Infof("OpenCode SSE stream connected")
	go st.processSSEStream(sseCtx, sseCh)
	// 4. 发送消息并等待同步响应
	logs.Infof("OpenCode sending message...")
	messageCtx, cancelMessage := context.WithCancel(ctx)
	go st.sendAndProcessMessage(messageCtx, req)
	// 5. 后台等待完成并清理
	go st.waitCompletion(ctx, cancelMessage, cancelSSE)
	return st.buildHandle(req)
}

// ============================================================================
// runState — 单次 Run 的上下文
// ============================================================================
type runState struct {
	srv               *OpenCodeServer
	evtChan           chan agent.Event
	mu                sync.Mutex
	sessionID         string
	messageID         string
	lastTextEnded     string
	tokenUsage        *agent.Usage
	workDir           string
	session           *sessionResponse
	filteredToolCalls map[string]string
	reasoningParts   map[string]struct{} // reasoning partID 集合，用于 message.part.delta 过滤
	sseDone           chan struct{}
	msgDone           chan struct{}

	// 本次调用期间从 SSE 失败事件（session.error / step-finish error part）提取的错误文本。
	// 优先生效先到达的错误；后续错误不影响。
	runErr string
	// sseTerminal 仅在 SSE 流收到 session.error 后关闭。
	sseTerminal chan struct{}
}

func (st *runState) buildHandle(_ externalcli.InvocationRequest) (*externalcli.Invocation, error) {
	return &externalcli.Invocation{
		Process:   st.srv,
		Events:    st.evtChan,
		Responder: &serverResponder{srv: st.srv},
		Questions: &questionResponder{srv: st.srv},
	}, nil
}

// ============================================================================
// 会话管理
// ============================================================================
func (st *runState) ensureSession(ctx context.Context, req externalcli.InvocationRequest) (string, error) {
	// Resume 模式：复用已有 sessionID
	if req.Resume && strings.TrimSpace(req.SessionID) != "" {
		sessionID := strings.TrimSpace(req.SessionID)
		session, err := st.srv.GetSession(ctx, sessionID)
		if err != nil {
			logs.WarnContextf(ctx, "OpenCode get resumed session metadata failed: %v", err)
		} else {
			st.session = session
		}
		sendEventTo(st.evtChan, events.EventProviderSessionStarted, sessionID)
		logs.Infof("OpenCode resuming session: %s", sessionID)
		return sessionID, nil
	}
	// 新会话
	title := req.ExecutionID
	if title == "" {
		title = "Leros Task"
	}
	session, err := st.srv.CreateSession(ctx, title, providerID, req.Model.Model, req.SystemPrompt)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	sendEventTo(st.evtChan, events.EventProviderSessionStarted, session.ID)
	st.sessionID = session.ID
	st.session = session
	return session.ID, nil
}

// ============================================================================
// 消息发送
// ============================================================================
func (st *runState) sendAndProcessMessage(ctx context.Context, req externalcli.InvocationRequest) {
	defer close(st.msgDone)
	msgReq := messageRequest{
		Model: &sessionModelRef{
			ProviderID: providerID,
			ModelID:    req.Model.Model,
		},
		System: req.SystemPrompt,
		Agent:  openCodeAgent(req.ExecutionMode),
		Parts: []messagePart{
			{Type: "text", Text: req.Prompt},
		},
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
			logs.Errorf("OpenCode send message failed: %v", err)
		} else {
			logs.WarnContextf(ctx, "OpenCode send message cancelled: %v", ctx.Err())
		}
		return
	}
	st.mu.Lock()
	st.messageID = msgResp.Info.ID
	st.mu.Unlock()
	// 响应事件由 SSE 流式路径处理，同步响应体中的 parts 不再处理
}

func openCodeAgent(mode agent.ExecutionMode) string {
	if mode == agent.ExecutionModePlan {
		return "plan"
	}
	return "build"
}

// ============================================================================
// SSE 事件流处理
// ============================================================================
func (st *runState) processSSEStream(ctx context.Context, ch <-chan sseEvent) {
	defer close(st.sseDone)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			st.handleSSEEvent(ctx, event)
		}
	}
}

// ============================================================================
// 完成等待和清理
// ============================================================================
func (st *runState) waitCompletion(ctx context.Context, cancelMessage, cancelSSE context.CancelFunc) {
	defer close(st.evtChan)
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
	select {
	case <-ctx.Done():
		// 外部取消：立即终止一切
		logs.Errorf("OpenCode run cancelled: %v", ctx.Err())
		cancelMessage()
		cancelSSE()
		_ = st.srv.Abort(context.Background(), st.sessionID)
		sendEventTo(st.evtChan, events.EventInvocationCancelled, ctx.Err().Error())
		return

	case <-st.sseTerminal:
		// session.error：立即取消消息请求
		cancelMessage()

	case <-st.msgDone:
		// 正常完成路径：给 SSE 流一个短窗口收集 trailing error
		st.mu.Lock()
		hasRunErr := st.runErr != ""
		st.mu.Unlock()
		if !hasRunErr {
			select {
			case <-st.sseTerminal:
			case <-st.sseDone:
			case <-ctx.Done():
				sendEventTo(st.evtChan, events.EventInvocationCancelled, ctx.Err().Error())
				return
			case <-time.After(3 * time.Second):
			}
		}
	}

	// cleanup: 取消 SSE 并等待 goroutine 退出
	cancelSSE()
	select {
	case <-st.sseDone:
	case <-time.After(5 * time.Second):
		logs.Warnf("OpenCode SSE stream did not close within 5s after cancel, proceeding anyway")
	}

	st.mu.Lock()
	hasRunErr := st.runErr != ""
	runErr := st.runErr
	finalText := st.lastTextEnded
	usage := st.tokenUsage
	st.mu.Unlock()

	if finalText != "" {
		sendEventTo(st.evtChan, events.EventResult, finalText)
		sendEventPayloadTo(st.evtChan, events.EventResult, events.MessageResultPayload{
			Message: finalText,
			Usage:   usage,
		})
	}
	if hasRunErr {
		sendEventTo(st.evtChan, events.EventInvocationFailed, runErr)
		return
	}
	sendEventTo(st.evtChan, events.EventInvocationCompleted, finalText)
}

// ============================================================================
// 辅助函数
// ============================================================================
// emitMessageDelta 发送消息增量事件到通道。
func emitMessageDelta(ch chan<- agent.Event, messageID, content string) {
	if ch == nil || content == "" {
		return
	}
	payload, _ := json.Marshal(events.MessageDeltaPayload{MessageID: messageID, Content: content})
	select {
	case ch <- agent.Event{
		Type:    events.EventMessageDelta,
		Content: content,
		Payload: payload,
	}:
	default:
	}
}

// sendEventTo 发送简单事件到通道。
func sendEventTo(ch chan<- agent.Event, eventType agent.EventType, content string) {
	if ch == nil {
		return
	}
	select {
	case ch <- agent.Event{Type: eventType, Content: content}:
	default:
	}
}

// sendEventPayloadTo 发送带 payload 的事件到通道。
func sendEventPayloadTo(ch chan<- agent.Event, eventType agent.EventType, payload any) {
	if ch == nil {
		return
	}
	evt := agent.Event{Type: eventType}
	if payload != nil {
		if encoded, err := json.Marshal(payload); err == nil {
			evt.Payload = encoded
		}
	}
	select {
	case ch <- evt:
	default:
	}
}

// sendEventDirect 直接发送已有的事件指针到通道。
func sendEventDirect(ch chan<- agent.Event, evt *agent.Event) {
	if ch == nil || evt == nil {
		return
	}
	select {
	case ch <- *evt:
	default:
	}
}
