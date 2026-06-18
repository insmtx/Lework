package taskconsumer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/insmtx/Leros/backend/pkg/dm"
)

func TestMQStreamSinkPublishesStreamEventToStreamTopic(t *testing.T) {
	orgID := uint(1)
	sessionID := "session_test"
	task := protocol.WorkerTaskMessage{
		Trace: protocol.TraceContext{
			TraceID:   "trace_test",
			RequestID: "request_test",
			TaskID:    "task_test",
			RunID:     "run_test",
		},
		Route: protocol.RouteContext{
			OrgID:     orgID,
			SessionID: sessionID,
			WorkerID:  2,
		},
		Body: protocol.WorkerTaskBody{
			Input: protocol.TaskInput{
				Messages: []protocol.ChatMessage{
					{ID: "101", Role: protocol.MessageRoleUser, Content: "one"},
					{ID: "102", Role: protocol.MessageRoleUser, Content: "two"},
				},
			},
		},
	}
	publisher := &recordingPublisher{}
	sink := NewMQStreamSink(publisher, task)

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

	streamTopic, _ := dm.SessionResultStreamSubject(orgID, sessionID)
	if len(publisher.calls) != 1 {
		t.Fatalf("expected one stream publish, got %d", len(publisher.calls))
	}
	if publisher.calls[0].topic != streamTopic {
		t.Fatalf("expected publish to stream topic %q, got %q", streamTopic, publisher.calls[0].topic)
	}
	streamMsg, ok := publisher.calls[0].event.(protocol.MessageStreamMessage)
	if !ok {
		t.Fatalf("expected stream publish event type MessageStreamMessage, got %T", publisher.calls[0].event)
	}
	if streamMsg.Body.Event != protocol.StreamEventMessageDelta {
		t.Fatalf("expected stream event %q, got %q", protocol.StreamEventMessageDelta, streamMsg.Body.Event)
	}
	if streamMsg.Body.Payload.Content != "hello" {
		t.Fatalf("expected content %q, got %q", "hello", streamMsg.Body.Payload.Content)
	}
	if got := streamMsg.Body.ReplyToMessageIDs; len(got) != 2 || got[0] != "101" || got[1] != "102" {
		t.Fatalf("reply_to_message_ids = %v, want [101 102]", got)
	}
}

func TestMQStreamSinkPublishesCompletedEventToSessionCompletedTopic(t *testing.T) {
	tests := []struct {
		name       string
		eventType  events.EventType
		wantStream protocol.StreamEventType
		status     string
		message    string
	}{
		{
			name:       "run completed",
			eventType:  events.EventCompleted,
			wantStream: protocol.StreamEventRunCompleted,
			status:     "completed",
			message:    "done",
		},
		{
			name:       "run failed",
			eventType:  events.EventFailed,
			wantStream: protocol.StreamEventRunFailed,
			status:     "failed",
			message:    "runtime unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orgID := uint(1)
			sessionID := "session_test"
			task := protocol.WorkerTaskMessage{
				Trace: protocol.TraceContext{
					TraceID:   "trace_test",
					RequestID: "request_test",
					TaskID:    "task_test",
					RunID:     "run_test",
				},
				Route: protocol.RouteContext{
					OrgID:     orgID,
					SessionID: sessionID,
					WorkerID:  2,
				},
			}
			publisher := &recordingPublisher{}
			sink := NewMQStreamSink(publisher, task)
			payload := events.RunCompletedPayload{
				Status: tt.status,
				Result: events.RunResultPayload{
					Message: tt.message,
				},
				Usage: &events.UsagePayload{
					InputTokens:  1,
					OutputTokens: 2,
					TotalTokens:  3,
				},
				Events: []events.RunEventRecord{
					{
						Seq:  1,
						Type: events.EventStarted,
					},
				},
				CompletedAt: time.Now().UTC(),
			}
			event := events.NewRunCompleted(payload, tt.message)
			event.Type = tt.eventType
			event.ID = "event_test"
			event.RunID = "run_test"
			event.TraceID = "trace_test"
			event.Seq = 7

			err := sink.Emit(context.Background(), event)
			if err != nil {
				t.Fatalf("Emit() error = %v", err)
			}

			streamTopic, _ := dm.SessionResultStreamSubject(orgID, sessionID)
			completedTopic, _ := dm.SessionMessageCompletedSubject(orgID, sessionID)
			if len(publisher.calls) != 2 {
				t.Fatalf("expected 2 publishes (stream + completed), got %d", len(publisher.calls))
			}
			if publisher.calls[0].topic != streamTopic {
				t.Fatalf("expected first publish to stream topic %q, got %q", streamTopic, publisher.calls[0].topic)
			}
			if publisher.calls[1].topic != completedTopic {
				t.Fatalf("expected second publish to completed topic %q, got %q", completedTopic, publisher.calls[1].topic)
			}
			completedMsg, ok := publisher.calls[1].event.(protocol.MessageStreamMessage)
			if !ok {
				t.Fatalf("expected completed publish event type %T, got %T", completedMsg, publisher.calls[1].event)
			}
			if completedMsg.Body.Event != tt.wantStream {
				t.Fatalf("expected completed event %q, got %q", tt.wantStream, completedMsg.Body.Event)
			}
			if completedMsg.Trace.TaskID != task.Trace.TaskID || completedMsg.Trace.RunID != task.Trace.RunID {
				t.Fatalf("completed trace mismatch: got task_id=%q run_id=%q", completedMsg.Trace.TaskID, completedMsg.Trace.RunID)
			}
			streamMsg, ok := publisher.calls[0].event.(protocol.MessageStreamMessage)
			if !ok {
				t.Fatalf("expected stream publish event type MessageStreamMessage, got %T", publisher.calls[0].event)
			}
			if streamMsg.Body.RunCompleted != nil {
				t.Fatalf("stream topic should not include run_completed payload")
			}
			if streamMsg.Body.Payload.Content != tt.message {
				t.Fatalf("expected stream payload content %q, got %q", tt.message, streamMsg.Body.Payload.Content)
			}
			if streamMsg.Body.Payload.Usage == nil || streamMsg.Body.Payload.Usage.TotalTokens != 3 {
				t.Fatalf("expected stream payload usage, got %#v", streamMsg.Body.Payload.Usage)
			}
			if completedMsg.Body.RunCompleted == nil {
				t.Fatalf("expected completed payload to be forwarded")
			}
			if completedMsg.Body.RunCompleted.Status != tt.status {
				t.Fatalf("expected completed payload status %q, got %q", tt.status, completedMsg.Body.RunCompleted.Status)
			}
			if completedMsg.Body.RunCompleted.Result.Message != tt.message {
				t.Fatalf("expected completed payload message %q, got %q", tt.message, completedMsg.Body.RunCompleted.Result.Message)
			}
			if len(completedMsg.Body.RunCompleted.Events) != 1 {
				t.Fatalf("expected completed payload events, got %#v", completedMsg.Body.RunCompleted.Events)
			}
			if completedMsg.Body.Payload.Content != "" {
				t.Fatalf("completed topic should not include payload content, got %q", completedMsg.Body.Payload.Content)
			}
			if tt.eventType == events.EventFailed {
				if streamMsg.Body.Error == nil || streamMsg.Body.Error.Message != tt.message {
					t.Fatalf("expected stream error message %q, got %#v", tt.message, streamMsg.Body.Error)
				}
				if completedMsg.Body.Error == nil || completedMsg.Body.Error.Message != tt.message {
					t.Fatalf("expected completed error message %q, got %#v", tt.message, completedMsg.Body.Error)
				}
			}
		})
	}
}

func TestMQStreamSinkPublishesTodoPayload(t *testing.T) {
	orgID := uint(1)
	sessionID := "session_test"
	task := protocol.WorkerTaskMessage{
		Trace: protocol.TraceContext{
			TraceID: "trace_test",
			RunID:   "run_test",
		},
		Route: protocol.RouteContext{
			OrgID:     orgID,
			SessionID: sessionID,
			WorkerID:  2,
		},
	}
	publisher := &recordingPublisher{}
	sink := NewMQStreamSink(publisher, task)
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
	streamMsg, ok := publisher.calls[0].event.(protocol.MessageStreamMessage)
	if !ok {
		t.Fatalf("expected stream publish event type MessageStreamMessage, got %T", publisher.calls[0].event)
	}
	if streamMsg.Body.Event != protocol.StreamEventTodoSnapshot {
		t.Fatalf("expected event %q, got %q", protocol.StreamEventTodoSnapshot, streamMsg.Body.Event)
	}
	if len(streamMsg.Body.Payload.Todos) != 1 || streamMsg.Body.Payload.Todos[0].ID != "t1" {
		t.Fatalf("unexpected todo payload: %#v", streamMsg.Body.Payload.Todos)
	}
}

func TestMQStreamSinkPublishesArtifactDeclaredPayload(t *testing.T) {
	task := protocol.WorkerTaskMessage{
		Trace: protocol.TraceContext{
			TraceID: "trace_test",
			RunID:   "run_test",
		},
		Route: protocol.RouteContext{
			OrgID:     1,
			SessionID: "session_test",
			WorkerID:  2,
		},
	}
	publisher := &recordingPublisher{}
	sink := NewMQStreamSink(publisher, task)
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
	if len(publisher.calls) != 1 {
		t.Fatalf("expected one stream publish, got %d", len(publisher.calls))
	}
	streamMsg, ok := publisher.calls[0].event.(protocol.MessageStreamMessage)
	if !ok {
		t.Fatalf("expected stream publish event type MessageStreamMessage, got %T", publisher.calls[0].event)
	}
	if streamMsg.Body.Event != protocol.StreamEventArtifactDeclared {
		t.Fatalf("expected event %q, got %q", protocol.StreamEventArtifactDeclared, streamMsg.Body.Event)
	}
	if streamMsg.Body.Payload.Artifact == nil ||
		streamMsg.Body.Payload.Artifact.ArtifactID != "art_test" ||
		streamMsg.Body.Payload.Artifact.Filename != "report.md" ||
		streamMsg.Body.Payload.Artifact.MimeType != "text/markdown" {
		t.Fatalf("unexpected artifact payload: %#v", streamMsg.Body.Payload.Artifact)
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
