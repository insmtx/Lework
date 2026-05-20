package events

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestWorkerTaskMessageJSONShape verifies the JSON structure of WorkerTaskMessage.
// It ensures that trace and route fields are properly nested (not flattened to top level)
// and that the execution field is correctly named (not "target").
func TestWorkerTaskMessageJSONShape(t *testing.T) {
	message := WorkerTaskMessage{
		ID:        "msg_1",
		Type:      MessageTypeWorkerTask,
		CreatedAt: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
		Trace: TraceContext{
			TraceID:   "trace_1",
			RequestID: "req_1",
			TaskID:    "task_1",
			RunID:     "run_1",
		},
		Route: RouteContext{
			OrgID:     1001,
			SessionID: "sess_1",
			WorkerID:  1,
		},
		Body: WorkerTaskBody{
			TaskType: TaskTypeAgentRun,
			Actor: ActorContext{
				UserID:      "user_test",
				DisplayName: "Test User",
				Channel:     "test",
			},
			Execution: ExecutionTarget{
				AssistantID: "assistant_1",
				AgentID:     "agent_1",
			},
			Input: TaskInput{
				Type: InputTypeMessage,
				Text: "hello",
			},
		},
	}

	body, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}

	// Verify trace is nested, not at top level
	if _, ok := got["task_id"]; ok {
		t.Fatalf("task_id should live under trace, got top-level field in %s", body)
	}
	if _, ok := got["org_id"]; ok {
		t.Fatalf("org_id should live under route, got top-level field in %s", body)
	}

	// Verify message type
	if got["type"] != string(MessageTypeWorkerTask) {
		t.Fatalf("unexpected message type: %#v", got["type"])
	}

	// Verify trace object exists
	if _, ok := got["trace"].(map[string]any); !ok {
		t.Fatalf("expected trace object in %s", body)
	}

	// Verify route object exists
	if _, ok := got["route"].(map[string]any); !ok {
		t.Fatalf("expected route object in %s", body)
	}

	// Verify body object exists
	if _, ok := got["body"].(map[string]any); !ok {
		t.Fatalf("expected body object in %s", body)
	}

	// Verify execution field is named correctly (not "target")
	bodyObject := got["body"].(map[string]any)
	if _, ok := bodyObject["target"]; ok {
		t.Fatalf("target should be named execution in %s", body)
	}
	if _, ok := bodyObject["execution"].(map[string]any); !ok {
		t.Fatalf("expected execution object in %s", body)
	}
}

func TestMessageDeltaPayloadIncludesMessageID(t *testing.T) {
	messageID := uuid.NewString()
	event := NewMessageDelta(messageID, "hello")
	payload, err := DecodePayload[MessageDeltaPayload](event)
	if err != nil {
		t.Fatalf("decode message payload: %v", err)
	}
	if payload.MessageID != messageID || payload.Content != "hello" || payload.Role != string(MessageRoleAssistant) {
		t.Fatalf("unexpected message payload: %#v", payload)
	}
}

func TestRunEventRecordJSONUsesTimestampAndOmitsRunContext(t *testing.T) {
	record := RunEventRecord{
		Seq:       1,
		LastSeq:   2,
		Type:      EventMessageDelta,
		Timestamp: 1779243000000,
	}

	body, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if got["timestamp"] != float64(1779243000000) {
		t.Fatalf("expected numeric timestamp, got %s", body)
	}
	if _, ok := got["created_at"]; ok {
		t.Fatalf("record should not include created_at: %s", body)
	}
	if _, ok := got["id"]; ok {
		t.Fatalf("record should not include id: %s", body)
	}
	if _, ok := got["run_id"]; ok {
		t.Fatalf("record should not include run_id: %s", body)
	}
	if _, ok := got["trace_id"]; ok {
		t.Fatalf("record should not include trace_id: %s", body)
	}
}

// TestMessageStreamMessageJSONShape verifies the JSON structure of MessageStreamMessage.
// It ensures the message type and stream event type are correctly serialized.
func TestMessageStreamMessageJSONShape(t *testing.T) {
	message := MessageStreamMessage{
		ID:   "evt_1",
		Type: MessageTypeStream,
		Trace: TraceContext{
			TraceID: "trace_1",
			RunID:   "run_1",
		},
		Route: RouteContext{
			OrgID:     1001,
			SessionID: "sess_1",
			WorkerID:  1,
		},
		Body: StreamBody{
			Seq:   1,
			Event: StreamEventMessageDelta,
			Payload: StreamPayload{
				Role:    MessageRoleAssistant,
				Content: "hello",
			},
		},
	}

	body, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	var got struct {
		Type MessageType `json:"type"`
		Body struct {
			Seq   int64           `json:"seq"`
			Event StreamEventType `json:"event"`
		} `json:"body"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}

	// Verify message type
	if got.Type != MessageTypeStream {
		t.Fatalf("got type %q, want %q", got.Type, MessageTypeStream)
	}

	// Verify stream event type
	if got.Body.Event != StreamEventMessageDelta {
		t.Fatalf("got event %q, want %q", got.Body.Event, StreamEventMessageDelta)
	}

	// Verify sequence number
	if got.Body.Seq != 1 {
		t.Fatalf("got seq %d, want 1", got.Body.Seq)
	}
}
