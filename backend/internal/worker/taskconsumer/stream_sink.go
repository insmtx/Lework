package taskconsumer

import (
	"context"
	"fmt"
	"time"

	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/ygpkg/yg-go/logs"
)

// ResultPublisher publishes worker run result events.
type ResultPublisher interface {
	eventbus.Publisher
}

// MQStreamSink publishes agent runtime completion events via JetStream.
type MQStreamSink struct {
	publisher ResultPublisher
	task      protocol.WorkerTaskMessage
}

// NewMQStreamSink creates a stream sink for one worker task.
func NewMQStreamSink(publisher ResultPublisher, task protocol.WorkerTaskMessage) *MQStreamSink {
	return &MQStreamSink{
		publisher: publisher,
		task:      task,
	}
}

// Emit publishes runtime events to the session stream topic via JetStream.
func (s *MQStreamSink) Emit(ctx context.Context, event *events.Event) error {
	if s == nil || s.publisher == nil || event == nil {
		return nil
	}

	topic := s.streamTopic()
	if topic == "" {
		return nil
	}

	msg := protocol.MessageStreamMessage{
		ID:        fmt.Sprintf("%s:%d", event.RunID, event.Seq),
		Type:      protocol.MessageTypeStream,
		CreatedAt: time.Now().UTC(),
		Trace: protocol.TraceContext{
			TraceID:   event.TraceID,
			RequestID: s.task.Trace.RequestID,
			TaskID:    s.task.Trace.TaskID,
			RunID:     event.RunID,
			ParentID:  s.task.Trace.ParentID,
		},
		Route: s.task.Route,
		Body: protocol.StreamBody{
			Seq:     event.Seq,
			Event:   streamEventType(event.Type),
			Payload: streamPayload(event),
		},
	}
	if msg.Body.Event == protocol.StreamEventRunFailed {
		msg.Body.Error = &protocol.StreamError{Message: event.Content}
	}

	if err := s.publisher.Publish(ctx, topic, msg); err != nil {
		logs.WarnContextf(ctx, "Failed to publish worker stream event to %s: %v", topic, err)
	}

	if msg.Body.Event == protocol.StreamEventRunCompleted || msg.Body.Event == protocol.StreamEventRunFailed {
		s.emitCompleted(ctx, event)
	}
	return nil
}

func (s *MQStreamSink) streamTopic() string {
	if s.task.Route.SessionID != "" {
		t, _ := dm.SessionResultStreamSubject(s.task.Route.OrgID, s.task.Route.SessionID)
		return t
	}
	t, err := dm.WorkerTaskSubject(s.task.Route.OrgID, s.task.Route.WorkerID)
	if err != nil {
		logs.Errorf("Failed to get worker task topic for stream sink: %v", err)
		return ""
	}
	return t
}

func (s *MQStreamSink) emitCompleted(ctx context.Context, event *events.Event) error {
	if s.task.Route.SessionID == "" {
		return nil
	}

	topic, err := dm.SessionMessageCompletedSubject(s.task.Route.OrgID, s.task.Route.SessionID)
	if err != nil {
		return fmt.Errorf("failed to get session completed subject: %w", err)
	}

	streamEvent := protocol.StreamEventRunCompleted
	if event.Type == events.EventFailed || event.Type == events.EventCancelled {
		streamEvent = protocol.StreamEventRunFailed
	}

	msg := protocol.MessageStreamMessage{
		ID:        fmt.Sprintf("%s:%d", event.RunID, event.Seq),
		Type:      protocol.MessageTypeStream,
		CreatedAt: time.Now().UTC(),
		Trace: protocol.TraceContext{
			TraceID:   event.TraceID,
			RequestID: s.task.Trace.RequestID,
			TaskID:    s.task.Trace.TaskID,
			RunID:     event.RunID,
			ParentID:  s.task.Trace.ParentID,
		},
		Route: s.task.Route,
		Body: protocol.StreamBody{
			Seq:          event.Seq,
			Event:        streamEvent,
			RunCompleted: completedPayloadFromEvent(event),
		},
	}
	if streamEvent == protocol.StreamEventRunFailed {
		msg.Body.Error = &protocol.StreamError{Message: event.Content}
	}

	if err := s.publisher.Publish(ctx, topic, msg); err != nil {
		logs.WarnContextf(ctx, "Failed to publish worker completed event to %s: %v", topic, err)
		return err
	}
	return nil
}

func completedPayloadFromEvent(event *events.Event) *events.RunCompletedPayload {
	if event == nil {
		return nil
	}
	switch event.Type {
	case events.EventCompleted, events.EventFailed, events.EventCancelled:
	default:
		return nil
	}
	completedPayload, err := events.DecodePayload[events.RunCompletedPayload](event)
	if err != nil {
		return nil
	}
	return &completedPayload
}

func streamPayload(event *events.Event) protocol.StreamPayload {
	if event == nil {
		return protocol.StreamPayload{Role: protocol.MessageRoleAssistant}
	}
	payload := protocol.StreamPayload{
		Role:    protocol.MessageRoleAssistant,
		Content: event.Content,
	}
	switch event.Type {
	case events.EventMessageDelta, events.EventReasoningDelta:
		messagePayload, err := events.DecodePayload[events.MessageDeltaPayload](event)
		if err == nil {
			payload.MessageID = messagePayload.MessageID
			payload.Role = protocol.MessageRole(messagePayload.Role)
			payload.Content = messagePayload.Content
			if payload.Role == "" {
				payload.Role = protocol.MessageRoleAssistant
			}
		}
	case events.EventToolCallStarted:
		toolPayload, err := events.DecodePayload[events.ToolCallPayload](event)
		if err == nil {
			payload.ToolCall = &toolPayload
		}
	case events.EventToolCallCompleted, events.EventToolCallFailed:
		resultPayload, err := events.DecodePayload[events.ToolCallResultPayload](event)
		if err == nil {
			payload.ToolResult = &resultPayload
		}
	case events.EventTodoSnapshot, events.EventTodoUpdated:
		todos, err := events.DecodePayload[[]events.RuntimeTodoItem](event)
		if err == nil {
			payload.Todos = todos
		}
	case events.EventArtifactDeclared:
		artifact, err := events.DecodePayload[events.ArtifactPayload](event)
		if err == nil {
			payload.Artifact = &artifact
		}
	case events.EventApprovalRequested:
		approvalReq, err := events.DecodePayload[events.ApprovalRequestPayload](event)
		if err == nil {
			payload.ApprovalRequest = &approvalReq
		}
	case events.EventApprovalResolved:
		approvalDec, err := events.DecodePayload[events.ApprovalDecisionPayload](event)
		if err == nil {
			payload.ApprovalDecision = &approvalDec
		}
	case events.EventCompleted, events.EventFailed, events.EventCancelled:
		completedPayload, err := events.DecodePayload[events.RunCompletedPayload](event)
		if err == nil {
			payload.Content = completedPayload.Result.Message
			payload.Usage = completedPayload.Usage
		}
	}
	return payload
}

func streamEventType(eventType events.EventType) protocol.StreamEventType {
	switch eventType {
	case events.EventStarted:
		return protocol.StreamEventRunStarted
	case events.EventCompleted:
		return protocol.StreamEventRunCompleted
	case events.EventFailed, events.EventCancelled:
		return protocol.StreamEventRunFailed
	case events.EventMessageDelta:
		return protocol.StreamEventMessageDelta
	case events.EventReasoningDelta:
		return protocol.StreamEventReasoningDelta
	case events.EventResult:
		return protocol.StreamEventMessageCompleted
	case events.EventToolCallStarted:
		return protocol.StreamEventToolCallStarted
	case events.EventToolCallCompleted:
		return protocol.StreamEventToolCallFinished
	case events.EventToolCallFailed:
		return protocol.StreamEventToolCallFinished
	case events.EventTodoSnapshot:
		return protocol.StreamEventTodoSnapshot
	case events.EventTodoUpdated:
		return protocol.StreamEventTodoUpdated
	case events.EventArtifactDeclared:
		return protocol.StreamEventArtifactDeclared
	case events.EventApprovalRequested:
		return protocol.StreamEventApprovalRequested
	case events.EventApprovalResolved:
		return protocol.StreamEventApprovalResolved
	default:
		return protocol.StreamEventMessageDelta
	}
}

var _ events.Sink = (*MQStreamSink)(nil)
