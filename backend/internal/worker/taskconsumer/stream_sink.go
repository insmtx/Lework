package taskconsumer

import (
	"context"
	"fmt"
	"time"

	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
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
	task      events.WorkerTaskMessage
}

// NewMQStreamSink creates a stream sink for one worker task.
func NewMQStreamSink(publisher ResultPublisher, task events.WorkerTaskMessage) *MQStreamSink {
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

	msg := events.MessageStreamMessage{
		ID:        fmt.Sprintf("%s:%d", event.RunID, event.Seq),
		Type:      events.MessageTypeStream,
		CreatedAt: time.Now().UTC(),
		Trace: events.TraceContext{
			TraceID:   event.TraceID,
			RequestID: s.task.Trace.RequestID,
			TaskID:    s.task.Trace.TaskID,
			RunID:     event.RunID,
			ParentID:  s.task.Trace.ParentID,
		},
		Route: s.task.Route,
		Body: events.StreamBody{
			Seq:     event.Seq,
			Event:   streamEventType(event.Type),
			Payload: streamPayload(event),
		},
	}
	if msg.Body.Event == events.StreamEventRunFailed {
		msg.Body.Error = &events.StreamError{Message: event.Content}
	}
	if msg.Body.Event == events.StreamEventUsage {
		usagePayload, err := events.DecodePayload[events.UsagePayload](event)
		if err == nil {
			msg.Body.Usage = &events.UsagePayload{
				InputTokens:  usagePayload.InputTokens,
				OutputTokens: usagePayload.OutputTokens,
				TotalTokens:  usagePayload.TotalTokens,
			}
		}
	}

	if err := s.publisher.Publish(ctx, topic, msg); err != nil {
		logs.WarnContextf(ctx, "Failed to publish worker stream event to %s: %v", topic, err)
	}

	if msg.Body.Event == events.StreamEventRunCompleted || msg.Body.Event == events.StreamEventRunFailed {
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

	streamEvent := events.StreamEventRunCompleted
	if event.Type == events.EventFailed || event.Type == events.EventCancelled {
		streamEvent = events.StreamEventRunFailed
	}

	msg := events.MessageStreamMessage{
		ID:        fmt.Sprintf("%s:%d", event.RunID, event.Seq),
		Type:      events.MessageTypeStream,
		CreatedAt: time.Now().UTC(),
		Trace: events.TraceContext{
			TraceID:   event.TraceID,
			RequestID: s.task.Trace.RequestID,
			TaskID:    s.task.Trace.TaskID,
			RunID:     event.RunID,
			ParentID:  s.task.Trace.ParentID,
		},
		Route: s.task.Route,
		Body: events.StreamBody{
			Seq:   event.Seq,
			Event: streamEvent,
			Payload: events.StreamPayload{
				Role:    events.MessageRoleAssistant,
				Content: event.Content,
			},
		},
	}
	if streamEvent == events.StreamEventRunFailed {
		msg.Body.Error = &events.StreamError{Message: event.Content}
	}

	if err := s.publisher.Publish(ctx, topic, msg); err != nil {
		logs.WarnContextf(ctx, "Failed to publish worker completed event to %s: %v", topic, err)
		return err
	}
	return nil
}

func streamPayload(event *events.Event) events.StreamPayload {
	if event == nil {
		return events.StreamPayload{Role: events.MessageRoleAssistant}
	}
	payload := events.StreamPayload{
		Role:    events.MessageRoleAssistant,
		Content: event.Content,
	}
	switch event.Type {
	case events.EventMessageDelta, events.EventReasoningDelta:
		messagePayload, err := events.DecodePayload[events.MessageDeltaPayload](event)
		if err == nil {
			payload.MessageID = messagePayload.MessageID
			payload.Role = events.MessageRole(messagePayload.Role)
			payload.Content = messagePayload.Content
			if payload.Role == "" {
				payload.Role = events.MessageRoleAssistant
			}
		}
	case events.EventToolCallStarted:
		toolPayload, err := events.DecodePayload[events.ToolCallPayload](event)
		if err == nil {
			payload.ToolCall = &events.ToolCallEvent{
				ID:        toolPayload.ToolCallID,
				Name:      toolPayload.Name,
				Arguments: toolPayload.Arguments,
			}
		}
	case events.EventToolCallCompleted:
		resultPayload, err := events.DecodePayload[events.ToolCallResultPayload](event)
		if err == nil {
			result, _ := resultPayload.Result.(map[string]any)
			payload.ToolResult = &events.ToolResultEvent{
				ToolCallID: resultPayload.ToolCallID,
				Name:       resultPayload.Name,
				Result:     result,
			}
		}
	}
	return payload
}

func streamEventType(eventType events.EventType) events.StreamEventType {
	switch eventType {
	case events.EventStarted:
		return events.StreamEventRunStarted
	case events.EventCompleted:
		return events.StreamEventRunCompleted
	case events.EventFailed, events.EventCancelled:
		return events.StreamEventRunFailed
	case events.EventMessageDelta, events.EventReasoningDelta:
		return events.StreamEventMessageDelta
	case events.EventResult:
		return events.StreamEventMessageCompleted
	case events.EventToolCallStarted:
		return events.StreamEventToolCallStarted
	case events.EventToolCallCompleted:
		return events.StreamEventToolCallFinished
	case events.EventToolCallFailed:
		return events.StreamEventToolCallFinished
	case events.EventUsage:
		return events.StreamEventUsage
	default:
		return events.StreamEventMessageDelta
	}
}

var _ events.Sink = (*MQStreamSink)(nil)