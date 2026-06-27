package run

import (
	"context"
	"fmt"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

func TestMQStreamSinkPublishesStreamEventToStreamTopic(t *testing.T) {
	orgID := uint(1)
	sessionID := "session_test"
	task := messaging.NewRunCommand(
		"msg_test",
		messaging.RouteContext{
			OrgID:     orgID,
			SessionID: sessionID,
			WorkerID:  2,
		},
		messaging.TraceContext{
			TraceID:   "trace_test",
			RequestID: "request_test",
			TaskID:    "task_test",
			RunID:     "run_test",
		},
		messaging.RunCommandPayload{
			TaskType: messaging.TaskTypeAgentRun,
			Input: messaging.TaskInput{
				Type: messaging.InputTypeMessage,
				Messages: []messaging.ChatMessage{
					{ID: "101", Role: messaging.MessageRoleUser, Content: "one"},
					{ID: "102", Role: messaging.MessageRoleUser, Content: "two"},
				},
			},
		},
		nil,
	)
	publisher := &recordingPublisher{}
	sink := NewMQStreamSinkFromCommand(publisher, task)

	err := sink.Emit(context.Background(), &events.Event{
		ID:      "event_test",
		Type:    events.EventMessageDelta,
		RunID:   "run_test",
		TraceID: "trace_test",
		Seq:     3,
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	streamTopic, _ := messaging.RunEventSubject(orgID, sessionID, messaging.RunEventLaneStream)
	if len(publisher.calls) != 1 {
		t.Fatalf("expected one stream publish, got %d", len(publisher.calls))
	}
	if publisher.calls[0].topic != streamTopic {
		t.Fatalf("expected publish to stream topic %q, got %q", streamTopic, publisher.calls[0].topic)
	}
	evt, ok := publisher.calls[0].event.(messaging.RunEvent)
	if !ok {
		t.Fatalf("expected event type RunEvent, got %T", publisher.calls[0].event)
	}
	if evt.Body.Event != messaging.RunEventMessageDelta {
		t.Fatalf("expected event %q, got %q", messaging.RunEventMessageDelta, evt.Body.Event)
	}
	if evt.Body.Payload.Content != "hello" {
		t.Fatalf("expected content %q, got %q", "hello", evt.Body.Payload.Content)
	}
	if got := evt.Body.ReplyToMessageIDs; len(got) != 2 || got[0] != "101" || got[1] != "102" {
		t.Fatalf("reply_to_message_ids = %v, want [101 102]", got)
	}
}

func TestMQStreamSinkPublishesCompletedEventToStreamTopic(t *testing.T) {
	tests := []struct {
		name       string
		eventType  events.EventType
		wantStream messaging.RunEventType
		status     string
		message    string
	}{
		{
			name:       "completed",
			eventType:  events.EventCompleted,
			wantStream: messaging.RunEventRunCompleted,
			status:     "completed",
			message:    "done",
		},
		{
			name:       "failed",
			eventType:  events.EventFailed,
			wantStream: messaging.RunEventRunFailed,
			status:     "failed",
			message:    "error",
		},
		{
			name:       "cancelled",
			eventType:  events.EventCancelled,
			wantStream: messaging.RunEventRunCancelled,
			status:     "cancelled",
			message:    "done",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orgID := uint(1)
			sessionID := "session_test"
			task := messaging.WorkerCommand{
				Trace: messaging.TraceContext{
					TraceID: "trace_test",
					RunID:   "run_test",
				},
				Route: messaging.RouteContext{
					OrgID:     orgID,
					SessionID: sessionID,
					WorkerID:  2,
				},
			}

			publisher := &recordingPublisher{}
			sink := NewMQStreamSinkFromCommand(publisher, task)

			payload := events.RunCompletedPayload{
				Status: tt.status,
				Result: events.RunResultPayload{Message: tt.message},
			}
			event := events.NewRunCompleted(payload, tt.message)
			event.Type = tt.eventType
			event.RunID = "run_test"
			event.TraceID = "trace_test"
			event.Seq = 10

			if err := sink.Emit(context.Background(), event); err != nil {
				t.Fatalf("Emit() error = %v", err)
			}

			stateTopic, _ := messaging.RunEventSubject(orgID, sessionID, messaging.RunEventLaneState)
			if len(publisher.calls) != 1 {
				t.Fatalf("expected one state publish for terminal event, got %d", len(publisher.calls))
			}
			if publisher.calls[0].topic != stateTopic {
				t.Fatalf("expected publish to state topic %q, got %q", stateTopic, publisher.calls[0].topic)
			}
			evt, ok := publisher.calls[0].event.(messaging.RunEvent)
			if !ok {
				t.Fatalf("expected event type RunEvent, got %T", publisher.calls[0].event)
			}
			if evt.Body.Event != tt.wantStream {
				t.Fatalf("expected event %q, got %q", tt.wantStream, evt.Body.Event)
			}
			if evt.Body.RunCompleted == nil || evt.Body.RunCompleted.Status != tt.status {
				t.Fatalf("unexpected run_completed payload: %#v", evt.Body.RunCompleted)
			}
		})
	}
}

func TestMQStreamSinkTerminalEventDetachedContext(t *testing.T) {
	orgID := uint(1)
	sessionID := "session_test"
	task := messaging.WorkerCommand{
		Trace: messaging.TraceContext{
			TraceID: "trace_test",
			RunID:   "run_test",
		},
		Route: messaging.RouteContext{
			OrgID:     orgID,
			SessionID: sessionID,
			WorkerID:  2,
		},
	}

	publisher := &cancelSensitivePublisher{}
	sink := NewMQStreamSinkFromCommand(publisher, task)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sink.Emit(ctx, &events.Event{
		ID:      "event_test",
		Type:    events.EventCancelled,
		RunID:   "run_test",
		TraceID: "trace_test",
		Seq:     42,
		Content: `{"status":"cancelled","result":{"message":"done"},"events":[{"seq":1,"type":"message.delta","timestamp":1000,"payload":"a"}],"started_at":"2025-01-01T00:00:00Z","completed_at":"2025-01-01T00:01:00Z"}`,
	})
	if err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	stateTopic, _ := messaging.RunEventSubject(orgID, sessionID, messaging.RunEventLaneState)
	if len(publisher.calls) != 1 {
		t.Fatalf("expected state publish despite cancelled context, got %d", len(publisher.calls))
	}
	if publisher.calls[0].topic != stateTopic {
		t.Fatalf("expected publish to state topic %q, got %q", stateTopic, publisher.calls[0].topic)
	}
}

func TestMQStreamSinkPublishesTodoPayload(t *testing.T) {
	orgID := uint(1)
	sessionID := "session_test"
	task := messaging.WorkerCommand{
		Trace: messaging.TraceContext{
			TraceID: "trace_test",
			RunID:   "run_test",
		},
		Route: messaging.RouteContext{
			OrgID:     orgID,
			SessionID: sessionID,
			WorkerID:  2,
		},
	}
	publisher := &recordingPublisher{}
	sink := NewMQStreamSinkFromCommand(publisher, task)
	event := events.NewTodoSnapshot([]events.RuntimeTodoItem{
		{ID: "t1", Title: "Inspect code", Status: "pending"},
	})
	event.RunID = "run_test"
	event.TraceID = "trace_test"
	event.Seq = 4

	if err := sink.Emit(context.Background(), event); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	if len(publisher.calls) != 1 {
		t.Fatalf("expected one stream publish, got %d", len(publisher.calls))
	}
	evt, ok := publisher.calls[0].event.(messaging.RunEvent)
	if !ok {
		t.Fatalf("expected event type RunEvent, got %T", publisher.calls[0].event)
	}
	if evt.Body.Event != messaging.RunEventTodoSnapshot {
		t.Fatalf("expected event %q, got %q", messaging.RunEventTodoSnapshot, evt.Body.Event)
	}
	if len(evt.Body.Payload.Todos) != 1 || evt.Body.Payload.Todos[0].ID != "t1" {
		t.Fatalf("unexpected todo payload: %#v", evt.Body.Payload.Todos)
	}
}

func TestMQStreamSinkPublishesArtifactDeclaredPayload(t *testing.T) {
	task := messaging.WorkerCommand{
		Trace: messaging.TraceContext{
			TraceID: "trace_test",
			RunID:   "run_test",
		},
		Route: messaging.RouteContext{
			OrgID:     1,
			SessionID: "session_test",
			WorkerID:  2,
		},
	}
	publisher := &recordingPublisher{}
	sink := NewMQStreamSinkFromCommand(publisher, task)
	event := events.NewArtifactDeclared(events.ArtifactPayload{
		ArtifactID: "art_test",
		Title:      "Report",
		Filename:   "report.md",
		MimeType:   "text/markdown",
	})
	event.RunID = "run_test"
	event.TraceID = "trace_test"
	event.Seq = 5

	if err := sink.Emit(context.Background(), event); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	// Artifact declared goes to state lane
	stateTopic, _ := messaging.RunEventSubject(1, "session_test", messaging.RunEventLaneState)
	if len(publisher.calls) != 1 {
		t.Fatalf("expected one state publish, got %d", len(publisher.calls))
	}
	evt, ok := publisher.calls[0].event.(messaging.RunEvent)
	if !ok {
		t.Fatalf("expected event type RunEvent, got %T", publisher.calls[0].event)
	}
	if publisher.calls[0].topic != stateTopic {
		t.Fatalf("expected publish to state topic %q, got %q", stateTopic, publisher.calls[0].topic)
	}
	if evt.Body.Event != messaging.RunEventArtifactDeclared {
		t.Fatalf("expected event %q, got %q", messaging.RunEventArtifactDeclared, evt.Body.Event)
	}
	if evt.Body.Payload.Artifact == nil ||
		evt.Body.Payload.Artifact.ArtifactID != "art_test" ||
		evt.Body.Payload.Artifact.Filename != "report.md" ||
		evt.Body.Payload.Artifact.MimeType != "text/markdown" {
		t.Fatalf("unexpected artifact payload: %#v", evt.Body.Payload.Artifact)
	}
}

type recordingPublisher struct {
	calls []publishCall
}

type publishCall struct {
	topic string
	event any
}

func (p *recordingPublisher) Publish(_ context.Context, topic string, event any) error {
	p.calls = append(p.calls, publishCall{
		topic: topic,
		event: event,
	})
	return nil
}

func (p *recordingPublisher) Request(_ context.Context, _ string, _ any) (*nats.Msg, error) {
	return nil, fmt.Errorf("recordingPublisher: Request not supported")
}

type cancelSensitivePublisher struct {
	calls []publishCall
}

func (p *cancelSensitivePublisher) Publish(ctx context.Context, topic string, event any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.calls = append(p.calls, publishCall{
		topic: topic,
		event: event,
	})
	return nil
}

func (p *cancelSensitivePublisher) Request(_ context.Context, _ string, _ any) (*nats.Msg, error) {
	return nil, fmt.Errorf("cancelSensitivePublisher: Request not supported")
}
