package eventpub

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/agent"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

type publisherRecorder struct {
	contextErr error
	topic      string
	event      any
}

func TestNATSEventSinkRoutesBothLanesAndPreservesRawToolPayload(t *testing.T) {
	publisher := &publisherRecorder{}
	sink := NewNATSEventSink(publisher, RunEventContext{
		OrgID:     1,
		WorkerID:  2,
		SessionID: "session-1",
		RequestID: "request-1",
		TaskID:    "task-1",
	})
	if err := sink.Emit(context.Background(), &agent.Event{
		RunID:   "run-1",
		TraceID: "trace-1",
		Seq:     1,
		Type:    "tool_call.started",
		Payload: json.RawMessage(`{"tool_call_id":"tool-1","name":"read","arguments":{"path":"README.md"}}`),
	}); err != nil {
		t.Fatalf("Emit(tool) error = %v", err)
	}
	if !strings.Contains(publisher.topic, ".run.stream") {
		t.Fatalf("tool topic = %q", publisher.topic)
	}
	streamEvent, ok := publisher.event.(messaging.RunEvent)
	if !ok || streamEvent.Body.Payload.ToolCall == nil {
		t.Fatalf("stream event = %#v", publisher.event)
	}
	encoded, err := json.Marshal(streamEvent)
	if err != nil {
		t.Fatalf("marshal stream event: %v", err)
	}
	if !strings.Contains(string(encoded), `"arguments":{"path":"README.md"}`) {
		t.Fatalf("raw arguments changed JSON shape: %s", encoded)
	}

	if err := sink.Emit(context.Background(), &agent.Event{
		RunID:   "run-1",
		TraceID: "trace-1",
		Seq:     2,
		Type:    "run.failed",
		Content: "provider failed",
		Payload: json.RawMessage(`{
			"status":"failed",
			"error":"provider failed",
			"usage":{"total_tokens":9},
			"artifacts":[{"artifact_id":"artifact-1"}],
			"events":[{"seq":1,"type":"tool_call.started","payload":{"tool_call_id":"tool-1"}}]
		}`),
	}); err != nil {
		t.Fatalf("Emit(failed) error = %v", err)
	}
	if !strings.Contains(publisher.topic, ".run.state") {
		t.Fatalf("terminal topic = %q", publisher.topic)
	}
	stateEvent, ok := publisher.event.(messaging.RunEvent)
	if !ok || stateEvent.Body.RunCompleted == nil || stateEvent.Body.Error == nil {
		t.Fatalf("state event = %#v", publisher.event)
	}
	if stateEvent.Body.RunCompleted.Usage == nil ||
		stateEvent.Body.RunCompleted.Usage.TotalTokens != 9 ||
		len(stateEvent.Body.RunCompleted.Artifacts) != 1 ||
		len(stateEvent.Body.RunCompleted.Events) != 1 {
		t.Fatalf("terminal archive = %#v", stateEvent.Body.RunCompleted)
	}
}

func (p *publisherRecorder) Publish(ctx context.Context, topic string, event any) error {
	p.contextErr = ctx.Err()
	p.topic = topic
	p.event = event
	return nil
}

func (*publisherRecorder) Request(context.Context, string, any) (*nats.Msg, error) {
	return nil, nil
}

func TestNATSEventSinkMapsTerminalPayloadAndDetachedContext(t *testing.T) {
	publisher := &publisherRecorder{}
	sink := NewNATSEventSink(publisher, RunEventContext{
		OrgID:             1,
		WorkerID:          2,
		SessionID:         "session-1",
		RequestID:         "request-1",
		TaskID:            "task-1",
		ReplyToMessageIDs: []string{"1", "1", "2"},
	})
	payload, err := json.Marshal(assistantdomain.TerminalPayload{
		Status:    string(assistantdomain.RunStatusCancelled),
		Message:   "已取消",
		Error:     "provider stopped: context canceled",
		Usage:     &agent.Usage{TotalTokens: 7},
		Artifacts: []assistantdomain.ArtifactRecord{{ArtifactID: "artifact-1", Title: "report"}},
		Events: []assistantdomain.TerminalEventRecord{{
			Seq:       2,
			LastSeq:   4,
			Type:      "message.delta",
			Timestamp: 123,
			Payload:   json.RawMessage(`{"message_id":"m1"}`),
		}},
	})
	if err != nil {
		t.Fatalf("marshal terminal payload: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sink.Emit(ctx, &agent.Event{
		RunID:   "run-1",
		Seq:     5,
		Type:    "run.cancelled",
		Content: "已取消",
		Payload: json.RawMessage(payload),
	}); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	if publisher.contextErr != nil {
		t.Fatalf("publish context error = %v, want detached context", publisher.contextErr)
	}
	event, ok := publisher.event.(messaging.RunEvent)
	if !ok {
		t.Fatalf("published event type = %T", publisher.event)
	}
	if event.Body.Error == nil || event.Body.Error.Message != "provider stopped: context canceled" {
		t.Fatalf("terminal error = %#v", event.Body.Error)
	}
	if event.Body.RunCompleted == nil ||
		len(event.Body.RunCompleted.Artifacts) != 1 ||
		len(event.Body.RunCompleted.Events) != 1 {
		t.Fatalf("run completed payload = %#v", event.Body.RunCompleted)
	}
	if got := event.Body.RunCompleted.Events[0]; got.Seq != 2 || got.LastSeq != 4 || len(got.Payload) == 0 {
		t.Fatalf("archived event = %#v", got)
	}
	if got := event.Body.ReplyToMessageIDs; len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Fatalf("reply IDs = %v", got)
	}
	if messaging.ClassifyRunEvent(event.Body.Event) != messaging.RunEventLaneState {
		t.Fatalf("terminal event lane = %s", messaging.ClassifyRunEvent(event.Body.Event))
	}
}

func TestNATSEventSinkWireGolden(t *testing.T) {
	terminalPayload := func(status, message, errorText string) json.RawMessage {
		raw, err := json.Marshal(assistantdomain.TerminalPayload{
			Status:  status,
			Message: message,
			Error:   errorText,
		})
		if err != nil {
			t.Fatalf("marshal terminal payload: %v", err)
		}
		return raw
	}
	tests := []struct {
		name     string
		event    agent.Event
		wantLane messaging.RunEventLane
		wantBody string
	}{
		{
			name:     "started",
			event:    agent.Event{RunID: "run-1", Seq: 1, Type: "run.started", Content: "started"},
			wantLane: messaging.RunEventLaneState,
			wantBody: `{"seq":1,"event":"run.started","payload":{"role":"assistant","content":"started"},"reply_to_message_ids":["message-1"]}`,
		},
		{
			name: "activity",
			event: agent.Event{
				RunID:   "run-1",
				Seq:     2,
				Type:    "message.delta",
				Payload: json.RawMessage(`{"message_id":"assistant-1","role":"assistant","content":"hello"}`),
			},
			wantLane: messaging.RunEventLaneStream,
			wantBody: `{"seq":2,"event":"message.delta","payload":{"message_id":"assistant-1","role":"assistant","content":"hello"},"reply_to_message_ids":["message-1"]}`,
		},
		{
			name: "completed",
			event: agent.Event{
				RunID: "run-1", Seq: 3, Type: "run.completed", Content: "done",
				Payload: terminalPayload("completed", "done", ""),
			},
			wantLane: messaging.RunEventLaneState,
			wantBody: `{"seq":3,"event":"run.completed","payload":{"role":"assistant","content":"done"},"reply_to_message_ids":["message-1"],"run_completed":{"status":"completed","result":{"message":"done"}}}`,
		},
		{
			name: "failed",
			event: agent.Event{
				RunID: "run-1", Seq: 4, Type: "run.failed", Content: "failed",
				Payload: terminalPayload("failed", "failed", "provider failed"),
			},
			wantLane: messaging.RunEventLaneState,
			wantBody: `{"seq":4,"event":"run.failed","payload":{"role":"assistant","content":"failed"},"reply_to_message_ids":["message-1"],"run_completed":{"status":"failed","result":{"message":"failed"}},"error":{"message":"provider failed"}}`,
		},
		{
			name: "cancelled",
			event: agent.Event{
				RunID: "run-1", Seq: 5, Type: "run.cancelled", Content: "已取消",
				Payload: terminalPayload("cancelled", "已取消", "context canceled"),
			},
			wantLane: messaging.RunEventLaneState,
			wantBody: `{"seq":5,"event":"run.cancelled","payload":{"role":"assistant","content":"已取消"},"reply_to_message_ids":["message-1"],"run_completed":{"status":"cancelled","result":{"message":"已取消"}},"error":{"message":"context canceled"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			publisher := &publisherRecorder{}
			sink := NewNATSEventSink(publisher, RunEventContext{
				OrgID:             1,
				WorkerID:          2,
				SessionID:         "session-1",
				ReplyToMessageIDs: []string{"message-1"},
			})
			if err := sink.Emit(context.Background(), &test.event); err != nil {
				t.Fatalf("Emit() error = %v", err)
			}
			published, ok := publisher.event.(messaging.RunEvent)
			if !ok {
				t.Fatalf("published event type = %T", publisher.event)
			}
			if lane := messaging.ClassifyRunEvent(published.Body.Event); lane != test.wantLane {
				t.Fatalf("lane = %s, want %s", lane, test.wantLane)
			}
			assertGoldenJSON(t, published.Body, test.wantBody)
		})
	}
}

func assertGoldenJSON(t *testing.T, got any, want string) {
	t.Helper()
	gotRaw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal actual JSON: %v", err)
	}
	var gotValue any
	var wantValue any
	if err := json.Unmarshal(gotRaw, &gotValue); err != nil {
		t.Fatalf("decode actual JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("decode golden JSON: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("wire JSON mismatch\n got: %s\nwant: %s", gotRaw, want)
	}
}
