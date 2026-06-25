package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
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
		baseEnv: engines.BuildBaseEnv(extraEnv),
	}
}

// Run 启动 opcode serve，创建会话并执行提示。
func (inv *ServerInvoker) Run(ctx context.Context, req engines.RunRequest) (*engines.RunHandle, error) {
	workDir := strings.TrimSpace(req.WorkDir)
	// 1. 启动 OpenCode 服务
	srv, err := startOpenCodeServer(ctx, inv.binary, workDir, inv.baseEnv, req.Model, req.MCPServers)
	if err != nil {
		return nil, fmt.Errorf("start opencode server for %s: %w", workDir, err)
	}
	evtChan := make(chan events.Event, 64)
	st := &runState{
		srv:     srv,
		evtChan: evtChan,
		sseDone: make(chan struct{}),
		msgDone: make(chan struct{}),
	}
	// 2. 会话管理
	sessionID, err := st.ensureSession(ctx, req)
	if err != nil {
		_ = srv.Stop()
		close(evtChan)
		return nil, err
	}
	st.sessionID = sessionID
	// 3. 启动 SSE 事件流（在发送消息之前启动，避免丢失事件）
	sseCtx, cancelSSE := context.WithCancel(ctx)
	sseCh, err := srv.ConnectSSE(sseCtx, workDir)
	if err != nil {
		cancelSSE()
		_ = srv.Stop()
		close(evtChan)
		return nil, fmt.Errorf("connect SSE: %w", err)
	}
	go st.processSSEStream(sseCtx, sseCh)
	// 4. 发送消息并等待同步响应
	go st.sendAndProcessMessage(ctx, req)
	// 5. 后台等待完成并清理
	go st.waitCompletion(ctx, cancelSSE)
	return st.buildHandle(req)
}

// ============================================================================
// runState — 单次 Run 的上下文
// ============================================================================
type runState struct {
	srv               *OpenCodeServer
	evtChan           chan events.Event
	mu                sync.Mutex
	sessionID         string
	messageID         string
	lastTextEnded     string
	tokenUsage        *events.UsagePayload
	filteredToolCalls map[string]struct{}
	sseDone           chan struct{}
	msgDone           chan struct{}
}

func (st *runState) buildHandle(_ engines.RunRequest) (*engines.RunHandle, error) {
	return &engines.RunHandle{
		Process:   st.srv,
		Events:    (<-chan events.Event)(st.evtChan),
		Responder: &serverResponder{srv: st.srv},
		Questions: &questionResponder{srv: st.srv},
	}, nil
}

// ============================================================================
// 会话管理
// ============================================================================
func (st *runState) ensureSession(ctx context.Context, req engines.RunRequest) (string, error) {
	// Resume 模式：复用已有 sessionID
	if req.Resume && strings.TrimSpace(req.SessionID) != "" {
		sessionID := strings.TrimSpace(req.SessionID)
		sendEventTo(st.evtChan, engines.EventProviderSessionStarted, sessionID)
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
	sendEventTo(st.evtChan, engines.EventProviderSessionStarted, session.ID)
	st.sessionID = session.ID
	return session.ID, nil
}

// ============================================================================
// 消息发送
// ============================================================================
func (st *runState) sendAndProcessMessage(ctx context.Context, req engines.RunRequest) {
	defer close(st.msgDone)
	msgReq := messageRequest{
		Model: &sessionModelRef{
			ProviderID: providerID,
			ModelID:    req.Model.Model,
		},
		System: req.SystemPrompt,
		Parts: []messagePart{
			{Type: "text", Text: req.Prompt},
		},
	}
	msgResp, err := st.srv.SendMessage(ctx, st.sessionID, msgReq)
	if err != nil {
		// 检查是否是 context 取消导致的错误
		if ctx.Err() != nil {
			logs.WarnContextf(ctx, "OpenCode send message cancelled: %v", ctx.Err())
			sendEventTo(st.evtChan, events.EventCancelled, ctx.Err().Error())
		} else {
			logs.Errorf("OpenCode send message failed: %v", err)
			sendEventTo(st.evtChan, events.EventFailed, err.Error())
		}
		return
	}
	st.mu.Lock()
	st.messageID = msgResp.Info.ID
	st.mu.Unlock()
	// 响应事件由 SSE 流式路径处理，同步响应体中的 parts 不再处理
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
			st.handleSSEEvent(event)
		}
	}
}

// ============================================================================
// 完成等待和清理
// ============================================================================
func (st *runState) waitCompletion(ctx context.Context, cancelSSE context.CancelFunc) {
	defer close(st.evtChan)
	defer func() {
		_ = st.srv.Stop()
	}()
	// 等待消息响应完成
	select {
	case <-ctx.Done():
		// Context 取消：尝试 abort 会话
		logs.Errorf("OpenCode run cancelled: %v", ctx.Err())
		cancelSSE()
		_ = st.srv.Abort(context.Background(), st.sessionID)
		sendEventTo(st.evtChan, events.EventCancelled, ctx.Err().Error())
	case <-st.msgDone:
		// 消息响应完成，取消 SSE 流
		cancelSSE()
		// 等待 SSE 流完全关闭（最多 5 秒，防止某些情况下 SSE 不释放）
		select {
		case <-st.sseDone:
		case <-time.After(5 * time.Second):
			logs.Warnf("OpenCode SSE stream did not close within 5s after cancel, proceeding anyway")
		}
		// 发送最终结果
		finalText := st.lastTextEnded
		if finalText != "" {
			sendEventTo(st.evtChan, events.EventResult, finalText)
			sendEventPayloadTo(st.evtChan, events.EventResult, events.MessageResultPayload{
				Message: finalText,
				Usage:   st.tokenUsage,
			})
		}
		sendEventTo(st.evtChan, events.EventCompleted, finalText)
	}
}

// ============================================================================
// 辅助函数
// ============================================================================
// emitMessageDelta 发送消息增量事件到通道。
func emitMessageDelta(ch chan<- events.Event, messageID, content string) {
	if ch == nil || content == "" {
		return
	}
	payload, _ := json.Marshal(events.MessageDeltaPayload{MessageID: messageID, Content: content})
	select {
	case ch <- events.Event{
		Type:    events.EventMessageDelta,
		Content: content,
		Payload: payload,
	}:
	default:
	}
}

// sendEventTo 发送简单事件到通道。
func sendEventTo(ch chan<- events.Event, eventType events.EventType, content string) {
	if ch == nil {
		return
	}
	select {
	case ch <- events.Event{Type: eventType, Content: content}:
	default:
	}
}

// sendEventPayloadTo 发送带 payload 的事件到通道。
func sendEventPayloadTo(ch chan<- events.Event, eventType events.EventType, payload any) {
	if ch == nil {
		return
	}
	evt := events.Event{Type: eventType}
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
func sendEventDirect(ch chan<- events.Event, evt *events.Event) {
	if ch == nil || evt == nil {
		return
	}
	select {
	case ch <- *evt:
	default:
	}
}
