package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/types"
)

func TestProjectStreamMessageKeepsReasoningDeltaSeparate(t *testing.T) {
	streamMsg := events.MessageStreamMessage{
		CreatedAt: time.UnixMilli(1779243000000).UTC(),
		Route:     events.RouteContext{SessionID: "sess_test"},
		Body: events.StreamBody{
			Seq:   7,
			Event: events.StreamEventReasoningDelta,
			Payload: events.StreamPayload{
				MessageID: "msg_1",
				Role:      events.MessageRoleAssistant,
				Content:   "thinking",
			},
		},
	}

	event, ok := ProjectStreamMessage(streamMsg)
	if !ok {
		t.Fatal("expected reasoning event to project")
	}
	if event.Type != dto.SessionEventTypeReasoningDelta {
		t.Fatalf("got type %q, want %q", event.Type, dto.SessionEventTypeReasoningDelta)
	}
	payload, ok := event.Payload.(dto.MessageDeltaPayload)
	if !ok || payload.Content != "thinking" || payload.MessageID != "msg_1" {
		t.Fatalf("unexpected payload: %#v", event.Payload)
	}
}

func TestProjectRunEventRecordMatchesSessionEventShape(t *testing.T) {
	raw, err := json.Marshal(events.ToolCallResultPayload{
		ToolCallID: "call_1",
		Name:       "memory",
		Result:     map[string]any{"ok": true},
		IsError:    false,
		ElapsedMS:  12,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	event, ok := ProjectRunEventRecord("sess_test", types.MessageChunk{
		Seq:       8,
		Type:      string(events.EventToolCallCompleted),
		Timestamp: 1779243000000,
		Payload:   raw,
	})
	if !ok {
		t.Fatal("expected tool result event to project")
	}
	if event.Type != string(dto.SessionEventTypeToolCallResult) || event.SessionID != "sess_test" || event.Sequence != 8 {
		t.Fatalf("unexpected projected event: %#v", event)
	}
	payload, ok := event.Payload.(dto.ToolCallResultPayload)
	if !ok {
		t.Fatalf("unexpected payload type: %#v", event.Payload)
	}
	if payload.ToolCallID != "call_1" || payload.Name != "memory" || payload.Status != "success" {
		t.Fatalf("unexpected tool result payload: %#v", payload)
	}
}

func TestProjectStreamMessageProjectsTodoSnapshotPayloadAsArray(t *testing.T) {
	streamMsg := events.MessageStreamMessage{
		CreatedAt: time.UnixMilli(1779243000000).UTC(),
		Route:     events.RouteContext{SessionID: "sess_test"},
		Body: events.StreamBody{
			Seq:   9,
			Event: events.StreamEventTodoSnapshot,
			Payload: events.StreamPayload{
				Todos: []events.RuntimeTodoItem{
					{ID: "t1", Title: "Inspect code", Status: "completed"},
				},
			},
		},
	}

	event, ok := ProjectStreamMessage(streamMsg)
	if !ok {
		t.Fatal("expected todo event to project")
	}
	if event.Type != dto.SessionEventTypeTodoSnapshot {
		t.Fatalf("got type %q, want %q", event.Type, dto.SessionEventTypeTodoSnapshot)
	}
	payload, ok := event.Payload.([]dto.RuntimeTodoItemPayload)
	if !ok {
		t.Fatalf("unexpected payload type: %#v", event.Payload)
	}
	if len(payload) != 1 || payload[0].ID != "t1" || payload[0].Title != "Inspect code" || payload[0].Status != "completed" {
		t.Fatalf("unexpected todo payload: %#v", payload)
	}
}
