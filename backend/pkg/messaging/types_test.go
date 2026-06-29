package messaging

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEnvelopeJSONShape(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	payload := RunCommandPayload{
		TaskType: TaskTypeAgentRun,
		Actor: ActorContext{
			UserID: "user-1",
		},
		Input: TaskInput{
			Type: InputTypeMessage,
			Messages: []ChatMessage{
				{ID: "msg-1", Role: MessageRoleUser, Content: "hello"},
			},
		},
	}
	rawPayload, _ := json.Marshal(payload)

	env := Envelope[WorkerCommandBody]{
		ID:        "msg-001",
		Type:      MessageTypeWorkerCommand,
		CreatedAt: now,
		Trace: TraceContext{
			TraceID:   "trace-1",
			RequestID: "req-1",
			TaskID:    "task-1",
			RunID:     "run-1",
		},
		Route: RouteContext{
			OrgID:     1,
			SessionID: "sess-1",
			WorkerID:  2,
		},
		Body: WorkerCommandBody{
			CommandType: CommandTypeRun,
			Payload:     rawPayload,
		},
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("failed to marshal envelope: %v", err)
	}

	var decoded Envelope[WorkerCommandBody]
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}

	if decoded.ID != "msg-001" {
		t.Errorf("expected ID 'msg-001', got %q", decoded.ID)
	}
	if decoded.Type != MessageTypeWorkerCommand {
		t.Errorf("expected type worker.command, got %q", decoded.Type)
	}
	if decoded.Body.CommandType != CommandTypeRun {
		t.Errorf("expected command_type agent.run, got %q", decoded.Body.CommandType)
	}

	decodedPayload, err := DecodeCommandPayload[RunCommandPayload](&decoded.Body)
	if err != nil {
		t.Fatalf("failed to decode run command payload: %v", err)
	}
	if decodedPayload.Input.Messages[0].Content != "hello" {
		t.Errorf("expected message content 'hello', got %q", decodedPayload.Input.Messages[0].Content)
	}
}

func TestRunEventJSONShape(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	env := Envelope[RunEventBody]{
		ID:        "evt-001",
		Type:      MessageTypeRunEvent,
		CreatedAt: now,
		Trace: TraceContext{
			TraceID:   "trace-1",
			RequestID: "req-1",
			RunID:     "run-1",
		},
		Route: RouteContext{
			OrgID:     1,
			SessionID: "sess-1",
			WorkerID:  2,
		},
		Body: RunEventBody{
			Seq:   1,
			Event: RunEventRunStarted,
			Payload: RunEventPayload{
				Role:    MessageRoleAssistant,
				Content: "started",
			},
		},
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("failed to marshal run event: %v", err)
	}

	var decoded Envelope[RunEventBody]
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal run event: %v", err)
	}

	if decoded.Body.Event != RunEventRunStarted {
		t.Errorf("expected event run.started, got %q", decoded.Body.Event)
	}
	if decoded.Body.Payload.Content != "started" {
		t.Errorf("expected content 'started', got %q", decoded.Body.Payload.Content)
	}
}

func TestRunEventWithErrorJSONShape(t *testing.T) {
	env := Envelope[RunEventBody]{
		ID:        "evt-err",
		Type:      MessageTypeRunEvent,
		CreatedAt: time.Now().UTC(),
		Trace: TraceContext{
			TraceID: "trace-1",
			RunID:   "run-1",
		},
		Route: RouteContext{
			OrgID:     1,
			SessionID: "sess-1",
		},
		Body: RunEventBody{
			Seq:   10,
			Event: RunEventRunFailed,
			Error: &RunEventError{
				Code:    "E_RUNTIME",
				Message: "something went wrong",
			},
		},
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("failed to marshal error event: %v", err)
	}

	var decoded Envelope[RunEventBody]
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal error event: %v", err)
	}

	if decoded.Body.Error == nil {
		t.Fatal("expected error to be non-nil")
	}
	if decoded.Body.Error.Code != "E_RUNTIME" {
		t.Errorf("expected error code 'E_RUNTIME', got %q", decoded.Body.Error.Code)
	}
}

func TestWorkerCommandResultJSONShape(t *testing.T) {
	result := WorkerCommandResult{
		Success: true,
		Action:  "list",
		Data: []SkillListItem{
			{Name: "skill-a", Description: "desc a", Category: "tools", Source: "Leros", Trust: "trusted"},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}

	var decoded WorkerCommandResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if !decoded.Success {
		t.Error("expected success true")
	}
}

func TestWorkerCommandResultErrorJSONShape(t *testing.T) {
	result := WorkerCommandResult{
		Success: false,
		Action:  "install",
		Error:   "download failed",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal error result: %v", err)
	}

	var decoded WorkerCommandResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal error result: %v", err)
	}

	if decoded.Success {
		t.Error("expected success false")
	}
	if decoded.Error != "download failed" {
		t.Errorf("expected error 'download failed', got %q", decoded.Error)
	}
}
