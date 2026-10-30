package acp

import (
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/insmtx/Leros/backend/agent"
)

func TestParseTodos(t *testing.T) {
	raw := map[string]any{
		"todos": []any{
			map[string]any{"content": "first", "status": "in_progress", "priority": "high"},
			map[string]any{"content": "second", "status": "cancelled"},
			map[string]any{"title": "third", "status": "done"},
			map[string]any{"status": "pending"},
		},
	}
	items := ParseTodos(raw)
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 (missing content skipped)", len(items))
	}
	if items[0].ID != "todo_0" || items[0].Title != "first" || items[0].Status != "in_progress" {
		t.Fatalf("item0 = %+v", items[0])
	}
	if items[0].Priority != "high" {
		t.Fatalf("priority = %q", items[0].Priority)
	}
	if items[1].Status != "cancelled" {
		t.Fatalf("cancelled status = %q", items[1].Status)
	}
	if items[2].Title != "third" || items[2].Status != "completed" {
		t.Fatalf("item2 = %+v", items[2])
	}

	if got := ParseTodos(map[string]any{"todos": "nope"}); got != nil {
		t.Fatalf("malformed todos = %+v, want nil", got)
	}
	if got := ParseTodos("not-a-map"); got != nil {
		t.Fatalf("non-map raw = %+v, want nil", got)
	}
	if got := ParseTodos(map[string]any{"todos": []any{}}); got != nil {
		t.Fatalf("empty todos = %+v, want nil", got)
	}
}

func TestTranslatorTodowriteEmitsTodoUpdated(t *testing.T) {
	translator := NewTranslator(nil)
	todos := map[string]any{"todos": []any{
		map[string]any{"content": "step 1", "status": "pending"},
		map[string]any{"content": "step 2", "status": "in_progress"},
	}}

	translator.Handle(note(toolCallUpdate(acpsdk.ToolCallStatusPending, acpsdk.ToolKindOther, "todowrite", nil)))
	update := &acpsdk.SessionToolCallUpdate{
		ToolCallId: "c1",
		Status:     statusPtr(acpsdk.ToolCallStatusInProgress),
		RawInput:   todos,
	}
	events := translator.Handle(note(acpsdk.SessionUpdate{ToolCallUpdate: update}))
	if len(events) != 1 || events[0].Type != agent.NodeEventTodoUpdated {
		t.Fatalf("events = %+v, want todo.updated", events)
	}
	payload := events[0].Payload.(*agent.TodoUpdatedPayload)
	if len(payload.Items) != 2 || payload.Items[0].ID != "todo_0" || payload.Items[1].Status != "in_progress" {
		t.Fatalf("payload = %+v", payload)
	}

	// 同一份列表重复到达不重复发事件。
	if events := translator.Handle(note(acpsdk.SessionUpdate{ToolCallUpdate: update})); len(events) != 0 {
		t.Fatalf("duplicate todos emitted %d events", len(events))
	}

	// 列表变化后重新发事件。
	changed := &acpsdk.SessionToolCallUpdate{
		ToolCallId: "c1",
		Status:     statusPtr(acpsdk.ToolCallStatusInProgress),
		RawInput: map[string]any{"todos": []any{
			map[string]any{"content": "step 1", "status": "completed"},
			map[string]any{"content": "step 2", "status": "in_progress"},
		}},
	}
	events = translator.Handle(note(acpsdk.SessionUpdate{ToolCallUpdate: changed}))
	if len(events) != 1 {
		t.Fatalf("changed todos events = %d, want 1", len(events))
	}
}

func TestTranslatorTodowriteProducesNoToolCard(t *testing.T) {
	translator := NewTranslator(nil)
	translator.Handle(note(toolCallUpdate(acpsdk.ToolCallStatusPending, acpsdk.ToolKindOther, "todowrite", nil)))
	translator.Handle(note(acpsdk.SessionUpdate{ToolCallUpdate: &acpsdk.SessionToolCallUpdate{
		ToolCallId: "c1",
		Status:     statusPtr(acpsdk.ToolCallStatusInProgress),
		RawInput:   map[string]any{"todos": []any{map[string]any{"content": "x"}}},
	}}))

	events := translator.Handle(note(toolCallUpdate(acpsdk.ToolCallStatusCompleted, "", "", textBlocks("done"))))
	for _, event := range events {
		if event.Type == agent.NodeEventToolExecutionStart || event.Type == agent.NodeEventToolExecutionEnd {
			t.Fatalf("todowrite emitted tool card event: %s", event.Type)
		}
	}
}
