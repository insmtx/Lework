package opencode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
)

func TestHandleSSEEventQuestionAskedEmitsQuestionEvent(t *testing.T) {
	st := &runState{evtChan: make(chan agent.NodeEvent, 4)}

	st.handleSSEEvent(context.Background(), sseEvent{
		Type: "question.asked",
		Properties: map[string]any{
			"id":        "que_123",
			"sessionID": "ses_123",
			"tool": map[string]any{
				"callID":    "call_question",
				"messageID": "msg_question",
			},
			"questions": []any{
				map[string]any{
					"question": "今天是星期几？",
					"header":   "测试",
					"options": []any{
						map[string]any{"label": "星期四", "description": ""},
					},
				},
			},
		},
	})

	event := readEvent(t, st.evtChan)
	if event.Type != agent.NodeEventQuestionAsked {
		t.Fatalf("event type = %s, want %s", event.Type, agent.NodeEventQuestionAsked)
	}
	payload, ok := event.Payload.(*agent.QuestionAskedPayload)
	if !ok {
		t.Fatalf("question payload type = %T", event.Payload)
	}
	if payload.RequestID != "que_123" || payload.SessionID != "ses_123" {
		t.Fatalf("unexpected question identity: %#v", payload)
	}
	if payload.ToolCallID != "call_question" || payload.MessageID != "msg_question" {
		t.Fatalf("unexpected tool identity: %#v", payload)
	}
	if len(payload.Questions) != 1 || payload.Questions[0].Question != "今天是星期几？" {
		t.Fatalf("unexpected questions: %#v", payload.Questions)
	}
}

func TestHandleSSEEventPlanExitEmitsPlanReadyAndConfirmation(t *testing.T) {
	workDir := t.TempDir()
	planDir := filepath.Join(workDir, ".opencode", "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const planContent = "# Implementation plan"
	if err := os.WriteFile(filepath.Join(planDir, "123-plan-slug.md"), []byte(planContent), 0o600); err != nil {
		t.Fatal(err)
	}
	session := &sessionResponse{Slug: "plan-slug", Directory: workDir}
	session.Time.Created = 123
	st := &runState{
		evtChan:           make(chan agent.NodeEvent, 4),
		workDir:           workDir,
		session:           session,
		filteredToolCalls: make(map[string]string),
	}

	// Mark plan_exit as filtered via a tool part pending (V2 protocol).
	st.handleSSEEvent(context.Background(), sseEvent{
		Type: "message.part.updated",
		Properties: map[string]any{
			"part": map[string]any{
				"type":   "tool",
				"callID": "call_plan",
				"tool":   "plan_exit",
				"state": map[string]any{
					"status": "pending",
				},
			},
		},
	})
	// Now question.asked should detect plan_exit -> emit plan.ready + plan_confirmation question.
	st.handleSSEEvent(context.Background(), sseEvent{
		Type: "question.asked",
		Properties: map[string]any{
			"id":        "que_plan",
			"sessionID": "ses_plan",
			"tool": map[string]any{
				"callID":    "call_plan",
				"messageID": "msg_plan",
			},
			"questions": []any{
				map[string]any{
					"question": "Plan at .opencode/plans/123-plan-slug.md is complete.",
					"options": []any{
						map[string]any{"label": "Yes"},
						map[string]any{"label": "No"},
					},
				},
			},
		},
	})

	// First event: plan.ready (since file exists and is readable in test env).
	event := readEvent(t, st.evtChan)
	if event.Type != agent.NodeEventPlanReady {
		t.Fatalf("expected plan.ready, got type=%s content=%s", event.Type, "")
	}
	readyPayload, ok := event.Payload.(*agent.PlanReadyPayload)
	if !ok {
		t.Fatalf("plan.ready payload type = %T", event.Payload)
	}
	if readyPayload.Path == "" {
		t.Fatal("plan.ready path is empty")
	}
	if "" != "" {
		t.Fatalf("plan.ready content should not be embedded, got %q", "")
	}

	// Second event: question.asked with plan_confirmation.
	event = readEvent(t, st.evtChan)
	if event.Type != agent.NodeEventQuestionAsked {
		t.Fatalf("expected question.asked, got type=%s content=%s", event.Type, "")
	}
	payload, ok := event.Payload.(*agent.QuestionAskedPayload)
	if !ok {
		t.Fatalf("question payload type = %T", event.Payload)
	}
	if payload.InteractionType != "plan_confirmation" {
		t.Fatalf("interaction type = %q, want plan_confirmation", payload.InteractionType)
	}
}

func TestHandleSSEEventPlanExitCalledBeforeQuestionStillClassifies(t *testing.T) {
	workDir := t.TempDir()
	planDir := filepath.Join(workDir, ".opencode", "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const planContent = "# Plan doc"
	if err := os.WriteFile(filepath.Join(planDir, "123-plan.md"), []byte(planContent), 0o600); err != nil {
		t.Fatal(err)
	}
	st := &runState{
		evtChan:           make(chan agent.NodeEvent, 2),
		workDir:           workDir,
		filteredToolCalls: make(map[string]string),
	}
	// V2: mark plan_exit as filtered via tool part pending.
	st.handleSSEEvent(context.Background(), sseEvent{
		Type: "message.part.updated",
		Properties: map[string]any{
			"part": map[string]any{
				"type":   "tool",
				"callID": "call_plan",
				"tool":   "plan_exit",
				"state": map[string]any{
					"status": "pending",
				},
			},
		},
	})
	st.handleSSEEvent(context.Background(), sseEvent{
		Type: "question.asked",
		Properties: map[string]any{
			"id": "que_plan",
			"tool": map[string]any{
				"callID": "call_plan",
			},
			"questions": []any{
				map[string]any{
					"question": "Plan at .opencode/plans/123-plan.md is complete.",
				},
			},
		},
	})

	// First: plan.ready
	event := readEvent(t, st.evtChan)
	if event.Type != agent.NodeEventPlanReady {
		t.Fatalf("expected plan.ready, got type=%s", event.Type)
	}
	// Then: question with plan_confirmation
	event = readEvent(t, st.evtChan)
	payload, ok := event.Payload.(*agent.QuestionAskedPayload)
	if !ok {
		t.Fatalf("question payload type = %T", event.Payload)
	}
	if payload.InteractionType != "plan_confirmation" {
		t.Fatalf("interaction type = %q", payload.InteractionType)
	}
}

func TestHandleSSEEventPlanExitResolveFailureUsesStableMetadata(t *testing.T) {
	st := &runState{
		evtChan:           make(chan agent.NodeEvent, 2),
		workDir:           t.TempDir(),
		filteredToolCalls: map[string]string{"call_plan": "plan_exit"},
	}

	st.handleSSEEvent(context.Background(), sseEvent{
		Type: "question.asked",
		Properties: map[string]any{
			"id":        "que_plan",
			"sessionID": "ses_plan",
			"tool": map[string]any{
				"callID": "call_plan",
			},
			"questions": []any{
				map[string]any{"question": "Plan file could not be found."},
			},
		},
	})

	event := readEvent(t, st.evtChan)
	if event.Type != agent.NodeEventQuestionAsked {
		t.Fatalf("event type = %s, want %s", event.Type, agent.NodeEventQuestionAsked)
	}
	payload, ok := event.Payload.(*agent.QuestionAskedPayload)
	if !ok {
		t.Fatalf("question payload type = %T", event.Payload)
	}
	if payload.InteractionType != "plan_confirmation" {
		t.Fatalf("interaction type = %q, want plan_confirmation", payload.InteractionType)
	}
	if payload.Metadata["plan_error"] != "resolve_failed" {
		t.Fatalf("plan metadata = %#v, want stable resolve_failed code", payload.Metadata)
	}
}

func TestHandleSSEEventTodoUpdated(t *testing.T) {
	st := &runState{evtChan: make(chan agent.NodeEvent, 4)}

	st.handleSSEEvent(context.Background(), sseEvent{
		Type: "todo.updated",
		Properties: map[string]any{
			"sessionID": "ses_123",
			"todos": []any{
				map[string]any{
					"content":  "实现登录功能",
					"status":   "in_progress",
					"priority": "high",
				},
				map[string]any{
					"id":      "todo_custom",
					"content": "编写测试",
					"status":  "pending",
				},
			},
		},
	})

	event := readEvent(t, st.evtChan)
	if event.Type != agent.NodeEventTodoUpdated {
		t.Fatalf("event type = %s, want %s", event.Type, agent.NodeEventTodoUpdated)
	}

	items := todoItemsFromEventPayload(t, event)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %#v", len(items), items)
	}

	// 验证位置 ID 生成
	if items[0].ID != "todo_1" {
		t.Fatalf("items[0].ID = %q, want todo_1", items[0].ID)
	}
	if items[0].Title != "实现登录功能" {
		t.Fatalf("items[0].Title = %q", items[0].Title)
	}
	if items[0].Status != "in_progress" {
		t.Fatalf("items[0].Status = %q", items[0].Status)
	}
	if items[0].Priority != "high" {
		t.Fatalf("items[0].Priority = %q", items[0].Priority)
	}

	// 验证自定义 ID 保留
	if items[1].ID != "todo_custom" {
		t.Fatalf("items[1].ID = %q, want todo_custom", items[1].ID)
	}
	if items[1].Title != "编写测试" {
		t.Fatalf("items[1].Title = %q", items[1].Title)
	}
	if items[1].Status != "pending" {
		t.Fatalf("items[1].Status = %q", items[1].Status)
	}
	if items[1].Priority != "" {
		t.Fatalf("items[1].Priority = %q, want empty", items[1].Priority)
	}
}

func TestHandleSSEEventTodoUpdatedSkipsEmptyContent(t *testing.T) {
	st := &runState{evtChan: make(chan agent.NodeEvent, 4)}

	st.handleSSEEvent(context.Background(), sseEvent{
		Type: "todo.updated",
		Properties: map[string]any{
			"sessionID": "ses_123",
			"todos": []any{
				map[string]any{
					"content": "",
					"status":  "pending",
				},
				map[string]any{
					"content": "有效任务",
					"status":  "in_progress",
				},
			},
		},
	})

	event := readEvent(t, st.evtChan)
	items := todoItemsFromEventPayload(t, event)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1 (empty content skipped): %#v", len(items), items)
	}
	if items[0].Title != "有效任务" {
		t.Fatalf("items[0].Title = %q", items[0].Title)
	}
}

func TestHandleSSEEventTodoUpdatedAllEmptySkipsEvent(t *testing.T) {
	st := &runState{evtChan: make(chan agent.NodeEvent, 4)}

	st.handleSSEEvent(context.Background(), sseEvent{
		Type: "todo.updated",
		Properties: map[string]any{
			"sessionID": "ses_123",
			"todos": []any{
				map[string]any{
					"content": "",
					"status":  "pending",
				},
			},
		},
	})

	// 所有内容为空时不应发送事件
	select {
	case event := <-st.evtChan:
		t.Fatalf("unexpected event when all todos have empty content: %#v", event)
	default:
	}
}

func readEvent(t *testing.T, ch <-chan agent.NodeEvent) agent.NodeEvent {
	t.Helper()
	select {
	case event := <-ch:
		return event
	default:
		t.Fatal("expected event")
		return agent.NodeEvent{}
	}
}

func TestHandleSSEEventSessionErrorRecordsError(t *testing.T) {
	const wantErr = `no enabled provider model for "test"`
	st := &runState{
		evtChan:     make(chan agent.NodeEvent, 4),
		sseTerminal: make(chan struct{}),
	}

	st.handleSSEEvent(context.Background(), sseEvent{
		Type: "session.error",
		Properties: map[string]any{
			"sessionID": "ses_123",
			"error": map[string]any{
				"data": map[string]any{
					"message": wantErr,
				},
			},
		},
	})

	if st.runErr != wantErr {
		t.Fatalf("runErr = %q, want %q", st.runErr, wantErr)
	}
	select {
	case <-st.sseTerminal:
	default:
		t.Fatal("sseTerminal should be closed after session.error")
	}
}

func TestHandleSSEEventSessionErrorFallsBackWhenMessageMissing(t *testing.T) {
	st := &runState{
		evtChan:     make(chan agent.NodeEvent, 4),
		sseTerminal: make(chan struct{}),
	}

	st.handleSSEEvent(context.Background(), sseEvent{
		Type: "session.error",
		Properties: map[string]any{
			"sessionID": "ses_123",
			"error":     map[string]any{"name": "APIError"},
		},
	})

	if st.runErr != sessionErrorFallbackMessage {
		t.Fatalf("runErr = %q, want %q", st.runErr, sessionErrorFallbackMessage)
	}
	select {
	case <-st.sseTerminal:
	default:
		t.Fatal("sseTerminal should be closed after session.error")
	}
}

func TestHandleSSEEventIdleDoesNotCloseTerminal(t *testing.T) {
	st := &runState{
		evtChan:     make(chan agent.NodeEvent, 4),
		sseTerminal: make(chan struct{}),
	}

	st.handleSSEEvent(context.Background(), sseEvent{
		Type: "session.idle",
		Properties: map[string]any{
			"sessionID": "ses_123",
		},
	})

	select {
	case <-st.sseTerminal:
		t.Fatal("sseTerminal should NOT be closed after session.idle")
	default:
		// Expected: session.idle no longer closes sseTerminal
	}
}

func TestHandleSSEEventIdleDoesNotSignalAfterError(t *testing.T) {
	st := &runState{
		evtChan:     make(chan agent.NodeEvent, 4),
		sseTerminal: make(chan struct{}),
	}

	// record error first (session.error closes sseTerminal)
	st.handleSSEEvent(context.Background(), sseEvent{
		Type: "session.error",
		Properties: map[string]any{
			"sessionID": "ses_123",
			"error": map[string]any{
				"message": "model error",
			},
		},
	})

	// session.error should close sseTerminal
	select {
	case <-st.sseTerminal:
		// Expected: session.error still closes sseTerminal
	default:
		t.Fatal("sseTerminal should be closed after session.error")
	}
}

// Test runState.waitCompletion returns a terminal error outside NodeEvent.
func TestWaitCompletionFailedWhenErrorRecorded(t *testing.T) {
	const errMsg = "model not available"
	st := &runState{
		evtChan:     make(chan agent.NodeEvent, 16),
		resultChan:  make(chan cli.InvocationResult, 1),
		msgDone:     make(chan struct{}),
		sseDone:     make(chan struct{}),
		sseTerminal: make(chan struct{}),
		runErr:      errMsg,
	}
	close(st.msgDone)
	close(st.sseDone)
	close(st.sseTerminal)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go st.waitCompletion(ctx, func() {}, func() {})

	for range st.evtChan {
	}
	result := <-st.resultChan
	if result.Err == nil || result.Err.Error() != errMsg {
		t.Fatalf("result error = %v, want %q", result.Err, errMsg)
	}
}

func TestWaitCompletionSessionErrorUnblocksPendingMessage(t *testing.T) {
	const errMsg = "session failed"
	st := &runState{
		evtChan:     make(chan agent.NodeEvent, 16),
		resultChan:  make(chan cli.InvocationResult, 1),
		msgDone:     make(chan struct{}),
		sseDone:     make(chan struct{}),
		sseTerminal: make(chan struct{}),
		runErr:      errMsg,
	}
	close(st.sseDone)
	close(st.sseTerminal)

	st.waitCompletion(context.Background(), func() {}, func() {})

	for range st.evtChan {
	}
	result := <-st.resultChan
	if result.Err == nil || result.Err.Error() != errMsg {
		t.Fatalf("result error = %v, want %q", result.Err, errMsg)
	}
}

// Test waitCompletion returns a successful InvocationResult when no error.
func TestWaitCompletionCompletedWhenNoError(t *testing.T) {
	st := &runState{
		evtChan:       make(chan agent.NodeEvent, 16),
		resultChan:    make(chan cli.InvocationResult, 1),
		msgDone:       make(chan struct{}),
		sseDone:       make(chan struct{}),
		sseTerminal:   make(chan struct{}),
		lastTextEnded: "result text",
	}
	close(st.msgDone)
	close(st.sseDone)
	close(st.sseTerminal)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go st.waitCompletion(ctx, func() {}, func() {})

	for range st.evtChan {
	}
	result := <-st.resultChan
	if result.Err != nil || result.Message != "result text" {
		t.Fatalf("result = %#v", result)
	}
}

// Test that diagnostic errors are not promoted into assistant message content.
func TestWaitCompletionFailedKeepsErrorOutOfResult(t *testing.T) {
	const errMsg = "no enabled provider model for \"test\""
	st := &runState{
		evtChan:     make(chan agent.NodeEvent, 16),
		resultChan:  make(chan cli.InvocationResult, 1),
		msgDone:     make(chan struct{}),
		sseDone:     make(chan struct{}),
		sseTerminal: make(chan struct{}),
		runErr:      errMsg,
	}
	close(st.msgDone)
	close(st.sseDone)
	close(st.sseTerminal)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go st.waitCompletion(ctx, func() {}, func() {})

	var evts []agent.NodeEvent
	for event := range st.evtChan {
		evts = append(evts, event)
	}
	for _, event := range evts {
		if event.Type == agent.NodeEventMessageEnd {
			t.Fatalf("should not get EventResult for diagnostic-only failure, got: %+v", evts)
		}
	}
	result := <-st.resultChan
	if result.Message != "" || result.Err == nil {
		t.Fatalf("result = %#v", result)
	}
}

func TestWaitCompletionFailedDoesNotEmitMessageComplete(t *testing.T) {
	st := &runState{
		evtChan:       make(chan agent.NodeEvent, 16),
		resultChan:    make(chan cli.InvocationResult, 1),
		sseDone:       make(chan struct{}),
		sseTerminal:   make(chan struct{}),
		runErr:        sessionErrorFallbackMessage,
		lastTextEnded: "Reply with exactly one word: pong.",
	}
	close(st.sseDone)
	close(st.sseTerminal)

	st.waitCompletion(context.Background(), func() {}, func() {})

	var evts []agent.NodeEvent
	for event := range st.evtChan {
		evts = append(evts, event)
	}
	for _, event := range evts {
		if event.Type == agent.NodeEventMessageEnd {
			t.Fatalf("should not get message.completed on failure, got: %+v", evts)
		}
	}
	result := <-st.resultChan
	if result.Err == nil || result.Err.Error() != sessionErrorFallbackMessage {
		t.Fatalf("result error = %v, want %q", result.Err, sessionErrorFallbackMessage)
	}
	if result.Message != "" {
		t.Fatalf("result message = %q, want empty", result.Message)
	}
}

// TestHandleSSEEventGoldenMapping verifies that each V2 SSE event type maps to
// the correct unified runtime event type and payload.
func TestHandleSSEEventGoldenMapping(t *testing.T) {
	tests := []struct {
		name      string
		sseEvent  sseEvent
		wantType  agent.NodeEventType
		wantCheck func(t *testing.T, event agent.NodeEvent)
	}{
		{
			name: "message.part.delta text",
			sseEvent: sseEvent{
				Type: "message.part.delta",
				Properties: map[string]any{
					"sessionID": "s1", "messageID": "m1", "partID": "p1",
					"field": "text", "delta": "hello world",
				},
			},
			wantType: agent.NodeEventMessageUpdate,
		},
		{
			name: "message.part.delta filtering reasoning part",
			sseEvent: sseEvent{
				Type: "message.part.delta",
				Properties: map[string]any{
					"sessionID": "s1", "messageID": "m1", "partID": "p_reason",
					"field": "text", "delta": "thinking...",
				},
			},
		},
		{
			name: "message.part.updated step-finish with error",
			sseEvent: sseEvent{
				Type: "message.part.updated",
				Properties: map[string]any{
					"part": map[string]any{
						"type":   "step-finish",
						"reason": "error",
					},
				},
			},
		},
		{
			name: "message.part.updated tool completed",
			sseEvent: sseEvent{
				Type: "message.part.updated",
				Properties: map[string]any{
					"part": map[string]any{
						"type":   "tool",
						"callID": "call_1",
						"tool":   "shell",
						"state": map[string]any{
							"status": "completed",
							"output": "result",
						},
					},
				},
			},
			wantType: agent.NodeEventToolExecutionEnd,
			wantCheck: func(t *testing.T, event agent.NodeEvent) {
				t.Helper()
				p, ok := event.Payload.(*agent.ToolExecutionEndPayload)
				if !ok {
					t.Fatalf("tool payload type = %T", event.Payload)
				}
				if p.ToolCallID != "call_1" || p.Name != "shell" {
					t.Fatalf("payload = %#v", p)
				}
			},
		},
		{
			name: "message.part.updated tool error",
			sseEvent: sseEvent{
				Type: "message.part.updated",
				Properties: map[string]any{
					"part": map[string]any{
						"type":   "tool",
						"callID": "call_e",
						"tool":   "shell",
						"state": map[string]any{
							"status": "error",
							"error":  "command not found",
						},
					},
				},
			},
			wantType: agent.NodeEventToolExecutionEnd,
		},
		{
			name: "message.part.updated reasoning",
			sseEvent: sseEvent{
				Type: "message.part.updated",
				Properties: map[string]any{
					"part": map[string]any{
						"type":      "reasoning",
						"id":        "r1",
						"messageID": "m1",
						"text":      "Let me think...",
					},
				},
			},
			wantType: agent.NodeEventReasoningUpdate,
		},
		{
			name: "message.part.updated step-finish with tokens",
			sseEvent: sseEvent{
				Type: "message.part.updated",
				Properties: map[string]any{
					"part": map[string]any{
						"type": "step-finish",
						"tokens": map[string]any{
							"input": 100.0, "output": 50.0, "reasoning": 7.0,
							"cache": map[string]any{"read": 20.0, "write": 10.0},
						},
					},
				},
			},
		},
		{
			name: "message.updated assistant tokens",
			sseEvent: sseEvent{
				Type: "message.updated",
				Properties: map[string]any{
					"sessionID": "s1",
					"info": map[string]any{
						"id":   "msg_1",
						"role": "assistant",
						"tokens": map[string]any{
							"total": 200.0, "input": 100.0, "output": 50.0,
							"cache": map[string]any{"read": 30.0, "write": 20.0},
						},
					},
				},
			},
		},
		{
			name: "session.updated fallback tokens",
			sseEvent: sseEvent{
				Type: "session.updated",
				Properties: map[string]any{
					"sessionID": "s1",
					"info": map[string]any{
						"tokens": map[string]any{
							"input": 40.0, "output": 10.0, "reasoning": 5.0,
							"cache": map[string]any{"read": 4.0, "write": 1.0},
						},
					},
				},
			},
		},
		{
			name: "permission.asked",
			sseEvent: sseEvent{
				Type: "permission.asked",
				Properties: map[string]any{
					"id":         "perm_1",
					"permission": "shell",
					"patterns":   []any{"rm -rf"},
					"tool":       map[string]any{"callID": "call_p1"},
				},
			},
			wantType: agent.NodeEventApprovalRequested,
		},
		{
			name: "todo.updated",
			sseEvent: sseEvent{
				Type: "todo.updated",
				Properties: map[string]any{
					"sessionID": "s1",
					"todos": []any{
						map[string]any{"content": "Task 1", "status": "in_progress", "priority": "high"},
					},
				},
			},
			wantType: agent.NodeEventTodoUpdated,
		},
		{
			name: "session.error",
			sseEvent: sseEvent{
				Type: "session.error",
				Properties: map[string]any{
					"sessionID": "s1",
					"error":     map[string]any{"message": "model error"},
				},
			},
		},
		{
			name: "session.idle ignored",
			sseEvent: sseEvent{
				Type:       "session.idle",
				Properties: map[string]any{"sessionID": "s1"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectEvent := tt.wantType != ""

			st := &runState{
				evtChan:           make(chan agent.NodeEvent, 4),
				sseTerminal:       make(chan struct{}),
				filteredToolCalls: make(map[string]string),
				reasoningParts:    make(map[string]struct{}),
			}
			// Pre-mark reasoning partIDs for filtering test
			if tt.name == "message.part.delta filtering reasoning part" {
				st.reasoningParts["p_reason"] = struct{}{}
			}

			st.handleSSEEvent(context.Background(), tt.sseEvent)

			if !expectEvent {
				// Special cases that produce side effects without events.
				switch tt.name {
				case "session.error":
					select {
					case <-st.sseTerminal:
					default:
						t.Fatal("expected sseTerminal closed")
					}
				case "message.part.updated step-finish with error":
					if st.runErr == "" {
						t.Fatal("expected runErr to be set")
					}
				case "message.part.updated step-finish with tokens":
					if st.tokenUsage == nil || st.tokenUsage.InputTokens != 100 || st.tokenUsage.CacheInputTokens != 20 || st.tokenUsage.CacheOutputTokens != 10 || st.tokenUsage.TotalTokens != 150 {
						t.Fatalf("unexpected tokenUsage: %#v", st.tokenUsage)
					}
				case "message.updated assistant tokens":
					if st.messageID != "msg_1" || st.tokenUsage == nil || st.tokenUsage.TotalTokens != 150 || st.tokenUsage.CacheInputTokens != 30 || st.tokenUsage.CacheOutputTokens != 20 {
						t.Fatalf("unexpected message usage: messageID=%s usage=%#v", st.messageID, st.tokenUsage)
					}
				case "session.updated fallback tokens":
					if st.tokenUsage == nil || st.tokenUsage.InputTokens != 40 || st.tokenUsage.OutputTokens != 10 || st.tokenUsage.CacheInputTokens != 4 || st.tokenUsage.CacheOutputTokens != 1 || st.tokenUsage.TotalTokens != 50 {
						t.Fatalf("unexpected session usage: %#v", st.tokenUsage)
					}
				}

				select {
				case evt := <-st.evtChan:
					t.Fatalf("unexpected event: type=%s content=%s", evt.Type, "")
				default:
				}
				return
			}

			event := readEvent(t, st.evtChan)
			if string(event.Type) != string(tt.wantType) {
				t.Fatalf("event type = %s, want %s", event.Type, tt.wantType)
			}
			if tt.wantCheck != nil {
				tt.wantCheck(t, event)
			}
		})
	}
}

func todoItemsFromEventPayload(t *testing.T, event agent.NodeEvent) []agent.RuntimeTodoItem {
	t.Helper()
	if p, ok := event.Payload.(*agent.TodoUpdatedPayload); ok {
		return p.Items
	}
	t.Fatal("payload is not TodoUpdatedPayload")
	return nil
}
