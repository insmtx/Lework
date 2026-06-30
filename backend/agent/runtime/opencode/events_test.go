package opencode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
)

func TestHandleSSEEventQuestionAskedEmitsQuestionEvent(t *testing.T) {
	st := &runState{evtChan: make(chan agent.Event, 4)}

	st.handleSSEEvent(sseEvent{
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
	if event.Type != events.EventQuestionAsked {
		t.Fatalf("event type = %s, want %s", event.Type, events.EventQuestionAsked)
	}
	if event.Content != "今天是星期几？" {
		t.Fatalf("event content = %q", event.Content)
	}
	payload, err := events.DecodePayload[events.QuestionRequestPayload](&event)
	if err != nil {
		t.Fatalf("decode question payload: %v", err)
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

func TestHandleSSEEventFiltersConfiguredToolCall(t *testing.T) {
	st := &runState{evtChan: make(chan agent.Event, 4)}

	st.handleSSEEvent(sseEvent{
		Type: "session.next.tool.called",
		Properties: map[string]any{
			"callID": "call_question",
			"tool":   "question",
			"input": map[string]any{
				"questions": []any{
					map[string]any{"question": "今天是星期几？"},
				},
			},
		},
	})
	st.handleSSEEvent(sseEvent{
		Type: "session.next.tool.success",
		Properties: map[string]any{
			"callID": "call_question",
			"result": map[string]any{
				"answers": []any{"星期四"},
			},
		},
	})

	select {
	case event := <-st.evtChan:
		t.Fatalf("unexpected event for question tool call lifecycle: %#v", event)
	default:
	}
}

func TestHandleSSEEventPlanExitEmitsPlanConfirmation(t *testing.T) {
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
		evtChan:           make(chan agent.Event, 4),
		workDir:           workDir,
		session:           session,
		filteredToolCalls: make(map[string]string),
	}

	st.handleSSEEvent(sseEvent{
		Type: "session.next.tool.input.started",
		Properties: map[string]any{
			"callID": "call_plan",
			"name":   "plan_exit",
		},
	})
	st.handleSSEEvent(sseEvent{
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

	event := readEvent(t, st.evtChan)
	payload, err := events.DecodePayload[events.QuestionRequestPayload](&event)
	if err != nil {
		t.Fatal(err)
	}
	if payload.InteractionType != "plan_confirmation" {
		t.Fatalf("interaction type = %q", payload.InteractionType)
	}
	if event.Content != "以下是当前计划，是否执行？" {
		t.Fatalf("event content = %q", event.Content)
	}
	if len(payload.Questions) != 1 ||
		payload.Questions[0].Header != "计划确认" ||
		payload.Questions[0].Question != "以下是当前计划，是否执行？" ||
		payload.Questions[0].Custom ||
		len(payload.Questions[0].Options) != 2 ||
		payload.Questions[0].Options[0].Label != "Yes" ||
		payload.Questions[0].Options[1].Label != "No" {
		t.Fatalf("unexpected rewritten questions: %#v", payload.Questions)
	}
	if payload.Plan == nil || payload.Plan.Content != planContent || payload.Plan.Error != "" {
		t.Fatalf("unexpected plan handoff: %#v", payload.Plan)
	}

	st.handleSSEEvent(sseEvent{
		Type: "session.next.tool.called",
		Properties: map[string]any{
			"callID": "call_plan",
			"tool":   "plan_exit",
		},
	})
	st.handleSSEEvent(sseEvent{
		Type: "session.next.tool.success",
		Properties: map[string]any{
			"callID": "call_plan",
			"tool":   "plan_exit",
		},
	})
	if got := st.filteredToolName("call_plan"); got != "" {
		t.Fatalf("completed plan_exit mapping = %q, want cleared", got)
	}
	select {
	case event := <-st.evtChan:
		t.Fatalf("unexpected plan_exit tool event: %#v", event)
	default:
	}
}

func TestHandleSSEEventPlanExitCalledBeforeQuestionStillClassifies(t *testing.T) {
	st := &runState{
		evtChan:           make(chan agent.Event, 2),
		workDir:           t.TempDir(),
		filteredToolCalls: make(map[string]string),
	}
	st.handleSSEEvent(sseEvent{
		Type: "session.next.tool.called",
		Properties: map[string]any{
			"callID": "call_plan",
			"tool":   "plan_exit",
		},
	})
	st.handleSSEEvent(sseEvent{
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

	event := readEvent(t, st.evtChan)
	payload, err := events.DecodePayload[events.QuestionRequestPayload](&event)
	if err != nil {
		t.Fatal(err)
	}
	if payload.InteractionType != "plan_confirmation" {
		t.Fatalf("interaction type = %q", payload.InteractionType)
	}
}

func TestHandleSSEEventTodoUpdated(t *testing.T) {
	st := &runState{evtChan: make(chan agent.Event, 4)}

	st.handleSSEEvent(sseEvent{
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
	if event.Type != events.EventTodoUpdated {
		t.Fatalf("event type = %s, want %s", event.Type, events.EventTodoUpdated)
	}

	items, err := events.DecodePayload[[]events.RuntimeTodoItem](&event)
	if err != nil {
		t.Fatalf("decode todo items: %v", err)
	}
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
	st := &runState{evtChan: make(chan agent.Event, 4)}

	st.handleSSEEvent(sseEvent{
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
	items, err := events.DecodePayload[[]events.RuntimeTodoItem](&event)
	if err != nil {
		t.Fatalf("decode todo items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1 (empty content skipped): %#v", len(items), items)
	}
	if items[0].Title != "有效任务" {
		t.Fatalf("items[0].Title = %q", items[0].Title)
	}
}

func TestHandleSSEEventTodoUpdatedAllEmptySkipsEvent(t *testing.T) {
	st := &runState{evtChan: make(chan agent.Event, 4)}

	st.handleSSEEvent(sseEvent{
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

func TestHandleSSEEventTodoWriteToolFiltered(t *testing.T) {
	st := &runState{evtChan: make(chan agent.Event, 4)}

	// todowrite 工具调用不应产生 tool_call.started
	st.handleSSEEvent(sseEvent{
		Type: "session.next.tool.called",
		Properties: map[string]any{
			"callID": "call_todowrite",
			"tool":   "todowrite",
			"input":  map[string]any{"todos": "..."},
		},
	})
	// todowrite 成功不应产生 tool_call.completed
	st.handleSSEEvent(sseEvent{
		Type: "session.next.tool.success",
		Properties: map[string]any{
			"callID": "call_todowrite",
			"tool":   "todowrite",
			"result": map[string]any{"ok": true},
		},
	})
	// todowrite 失败不应产生 tool_call.failed
	st.handleSSEEvent(sseEvent{
		Type: "session.next.tool.failed",
		Properties: map[string]any{
			"callID": "call_todowrite",
			"tool":   "todowrite",
			"error":  map[string]any{"message": "something went wrong"},
		},
	})

	select {
	case event := <-st.evtChan:
		t.Fatalf("unexpected event for todowrite tool call lifecycle: %#v", event)
	default:
	}
}

func TestHandleSSEEventForwardsUnfilteredToolCall(t *testing.T) {
	st := &runState{evtChan: make(chan agent.Event, 4)}

	st.handleSSEEvent(sseEvent{
		Type: "session.next.tool.called",
		Properties: map[string]any{
			"callID": "call_shell",
			"tool":   "shell",
			"input":  map[string]any{"command": "date"},
		},
	})

	event := readEvent(t, st.evtChan)
	if event.Type != events.EventToolCallStarted {
		t.Fatalf("event type = %s, want %s", event.Type, events.EventToolCallStarted)
	}
	payload, err := events.DecodePayload[events.ToolCallPayload](&event)
	if err != nil {
		t.Fatalf("decode tool payload: %v", err)
	}
	if payload.ToolCallID != "call_shell" || payload.Name != "shell" {
		t.Fatalf("unexpected tool payload: %#v", payload)
	}
}

func readEvent(t *testing.T, ch <-chan agent.Event) agent.Event {
	t.Helper()
	select {
	case event := <-ch:
		return event
	default:
		t.Fatal("expected event")
		return agent.Event{}
	}
}
