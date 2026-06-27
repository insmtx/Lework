package run

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/ygpkg/yg-go/logs"
)

// ResultPublisher publishes worker run result events.
type ResultPublisher interface {
	eventbus.Publisher
}

// MQStreamSink publishes agent runtime events as messaging.RunEvent via JetStream,
// routing high-frequency deltas to the run.stream lane and low-frequency state
// events to the run.state lane.
type MQStreamSink struct {
	publisher ResultPublisher
	route     messaging.RouteContext
	trace     messaging.TraceContext
	// inputMessages holds the original user input message IDs for reply tracing.
	inputMessages []messaging.ChatMessage
}

const terminalPublishTimeout = 5 * time.Second

// NewMQStreamSink creates a stream sink for one worker task from a runTask.
func NewMQStreamSink(publisher ResultPublisher, task runTask) *MQStreamSink {
	return &MQStreamSink{
		publisher:     publisher,
		route:         task.Route,
		trace:         task.Trace,
		inputMessages: task.Input.Messages,
	}
}

// NewMQStreamSinkFromCommand creates a stream sink from a messaging.WorkerCommand.
func NewMQStreamSinkFromCommand(publisher ResultPublisher, task messaging.WorkerCommand) *MQStreamSink {
	var msgs []messaging.ChatMessage
	if payload, err := messaging.DecodeCommandPayload[messaging.RunCommandPayload](&task.Body); err == nil {
		msgs = payload.Input.Messages
	}
	return &MQStreamSink{
		publisher:     publisher,
		route:         task.Route,
		trace:         task.Trace,
		inputMessages: msgs,
	}
}

// Emit publishes runtime events to the appropriate run event lane via JetStream.
//
// Event routing:
//   - run.stream lane: message.delta, reasoning.delta, tool_call.started/finished, todo.snapshot/updated
//   - run.state lane:  run.started, artifact.declared, approval.requested/resolved, question.asked/answered,
//     run.completed/failed/cancelled
//
// Terminal events (completed/failed/cancelled) use a detached context with timeout
// to ensure delivery even if the originating context is cancelled.
func (s *MQStreamSink) Emit(ctx context.Context, event *events.Event) error {
	if s == nil || s.publisher == nil || event == nil {
		return nil
	}

	runEventType := mapRunEventType(event.Type)

	msg := messaging.RunEvent{
		ID:        fmt.Sprintf("%s:%d", event.RunID, event.Seq),
		Type:      messaging.MessageTypeRunEvent,
		CreatedAt: time.Now().UTC(),
		Trace: messaging.TraceContext{
			TraceID:   event.TraceID,
			RequestID: s.trace.RequestID,
			TaskID:    s.trace.TaskID,
			RunID:     event.RunID,
			ParentID:  s.trace.ParentID,
		},
		Route: s.route,
		Body: messaging.RunEventBody{
			Seq:               event.Seq,
			Event:             runEventType,
			Payload:           mapRunEventPayload(event),
			ReplyToMessageIDs: s.replyToMessageIDs(),
		},
	}
	if runEventType == messaging.RunEventRunFailed {
		msg.Body.Error = &messaging.RunEventError{Message: event.Content}
	}

	// For terminal events, unmarshal the RunCompletedPayload directly
	// from the event payload JSON into the messaging type.
	if isTerminalRunEvent(runEventType) && len(event.Payload) > 0 {
		var completed messaging.RunCompletedPayload
		if err := json.Unmarshal(event.Payload, &completed); err == nil {
			msg.Body.RunCompleted = &completed
		}
	}

	// Classify event to lane
	lane := messaging.ClassifyRunEvent(runEventType)

	// Build lane subject
	topic, err := messaging.RunEventSubject(s.route.OrgID, s.route.SessionID, lane)
	if err != nil {
		logs.WarnContextf(ctx, "Failed to build run event subject: %v", err)
		return nil
	}

	publishCtx := ctx
	publishCancel := func() {}
	if isTerminalRunEvent(runEventType) {
		publishCtx, publishCancel = terminalPublishContext(ctx)
	}
	defer publishCancel()

	if err := s.publisher.Publish(publishCtx, topic, msg); err != nil {
		logs.WarnContextf(ctx, "Failed to publish run event to %s: %v", topic, err)
	}

	return nil
}

func isTerminalRunEvent(event messaging.RunEventType) bool {
	return event == messaging.RunEventRunCompleted ||
		event == messaging.RunEventRunFailed ||
		event == messaging.RunEventRunCancelled
}

func terminalPublishContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, terminalPublishTimeout)
}

func mapRunEventType(eventType events.EventType) messaging.RunEventType {
	switch eventType {
	case events.EventStarted:
		return messaging.RunEventRunStarted
	case events.EventCompleted:
		return messaging.RunEventRunCompleted
	case events.EventCancelled:
		return messaging.RunEventRunCancelled
	case events.EventFailed:
		return messaging.RunEventRunFailed
	case events.EventMessageDelta:
		return messaging.RunEventMessageDelta
	case events.EventReasoningDelta:
		return messaging.RunEventReasoningDelta
	case events.EventResult:
		return messaging.RunEventMessageCompleted
	case events.EventToolCallStarted:
		return messaging.RunEventToolCallStarted
	case events.EventToolCallCompleted:
		return messaging.RunEventToolCallFinished
	case events.EventToolCallFailed:
		return messaging.RunEventToolCallFinished
	case events.EventTodoSnapshot:
		return messaging.RunEventTodoSnapshot
	case events.EventTodoUpdated:
		return messaging.RunEventTodoUpdated
	case events.EventArtifactDeclared:
		return messaging.RunEventArtifactDeclared
	case events.EventApprovalRequested:
		return messaging.RunEventApprovalRequested
	case events.EventApprovalResolved:
		return messaging.RunEventApprovalResolved
	case events.EventQuestionAsked:
		return messaging.RunEventQuestionAsked
	case events.EventQuestionAnswered:
		return messaging.RunEventQuestionAnswered
	default:
		return messaging.RunEventMessageDelta
	}
}

func mapRunEventPayload(event *events.Event) messaging.RunEventPayload {
	if event == nil {
		return messaging.RunEventPayload{Role: messaging.MessageRoleAssistant}
	}
	payload := messaging.RunEventPayload{
		Role:    messaging.MessageRoleAssistant,
		Content: event.Content,
	}
	switch event.Type {
	case events.EventMessageDelta, events.EventReasoningDelta:
		messagePayload, err := events.DecodePayload[events.MessageDeltaPayload](event)
		if err == nil {
			payload.MessageID = messagePayload.MessageID
			payload.Role = messaging.MessageRole(messagePayload.Role)
			payload.Content = messagePayload.Content
			if payload.Role == "" {
				payload.Role = messaging.MessageRoleAssistant
			}
		}
	case events.EventToolCallStarted:
		toolPayload, err := events.DecodePayload[events.ToolCallPayload](event)
		if err == nil {
			payload.ToolCall = &messaging.ToolCallPayload{
				ToolCallID: toolPayload.ToolCallID,
				Name:       toolPayload.Name,
				Arguments:  toolPayload.Arguments,
			}
		}
	case events.EventToolCallCompleted, events.EventToolCallFailed:
		resultPayload, err := events.DecodePayload[events.ToolCallResultPayload](event)
		if err == nil {
			payload.ToolResult = &messaging.ToolCallResultPayload{
				ToolCallID: resultPayload.ToolCallID,
				Name:       resultPayload.Name,
				Result:     resultPayload.Result,
				Error:      resultPayload.Error,
				IsError:    resultPayload.IsError,
				ElapsedMS:  resultPayload.ElapsedMS,
			}
		}
	case events.EventTodoSnapshot, events.EventTodoUpdated:
		todos, err := events.DecodePayload[[]events.RuntimeTodoItem](event)
		if err == nil {
			payload.Todos = make([]messaging.RuntimeTodoItem, len(todos))
			for i, td := range todos {
				payload.Todos[i] = messaging.RuntimeTodoItem{
					ID:       td.ID,
					Title:    td.Title,
					Status:   td.Status,
					Priority: td.Priority,
				}
			}
		}
	case events.EventArtifactDeclared:
		artifact, err := events.DecodePayload[events.ArtifactPayload](event)
		if err == nil {
			payload.Artifact = &messaging.ArtifactPayload{
				ArtifactID:   artifact.ArtifactID,
				Title:        artifact.Title,
				Filename:     artifact.Filename,
				Description:  artifact.Description,
				MimeType:     artifact.MimeType,
				ArtifactType: artifact.ArtifactType,
				FileSize:     artifact.FileSize,
				RelativePath: artifact.RelativePath,
				StorageKey:   artifact.StorageKey,
				Sha256:       artifact.Sha256,
				Source:       artifact.Source,
				Status:       artifact.Status,
			}
		}
	case events.EventApprovalRequested:
		approvalReq, err := events.DecodePayload[events.ApprovalRequestPayload](event)
		if err == nil {
			payload.ApprovalRequest = &messaging.ApprovalRequestPayload{
				RequestID:   approvalReq.RequestID,
				ToolName:    approvalReq.ToolName,
				ToolCallID:  approvalReq.ToolCallID,
				Description: approvalReq.Description,
				Arguments:   approvalReq.Arguments,
				Metadata:    approvalReq.Metadata,
			}
		}
	case events.EventApprovalResolved:
		approvalDec, err := events.DecodePayload[events.ApprovalDecisionPayload](event)
		if err == nil {
			payload.ApprovalDecision = &messaging.ApprovalDecisionPayload{
				RequestID: approvalDec.RequestID,
				Action:    approvalDec.Action,
				Reason:    approvalDec.Reason,
			}
		}
	case events.EventQuestionAsked:
		questionReq, err := events.DecodePayload[events.QuestionRequestPayload](event)
		if err == nil {
			payload.QuestionRequest = mapQuestionRequestPayload(questionReq)
		}
	case events.EventQuestionAnswered:
		questionAns, err := events.DecodePayload[events.QuestionAnswerPayload](event)
		if err == nil {
			payload.QuestionAnswer = &messaging.QuestionAnswerPayload{
				RequestID: questionAns.RequestID,
				Answers:   questionAns.Answers,
			}
		}
	case events.EventCompleted, events.EventFailed, events.EventCancelled:
		payload.Content = event.Content
	}
	return payload
}

func mapQuestionRequestPayload(q events.QuestionRequestPayload) *messaging.QuestionRequestPayload {
	mq := &messaging.QuestionRequestPayload{
		RequestID:  q.RequestID,
		SessionID:  q.SessionID,
		ToolCallID: q.ToolCallID,
		MessageID:  q.MessageID,
		Metadata:   q.Metadata,
	}
	for _, qi := range q.Questions {
		mqi := messaging.QuestionItem{
			Question:    qi.Question,
			Header:      qi.Header,
			MultiSelect: qi.MultiSelect,
			Custom:      qi.Custom,
		}
		for _, opt := range qi.Options {
			mqi.Options = append(mqi.Options, messaging.QuestionOption{
				Label:       opt.Label,
				Description: opt.Description,
			})
		}
		mq.Questions = append(mq.Questions, mqi)
	}
	return mq
}

func (s *MQStreamSink) replyToMessageIDs() []string {
	if len(s.inputMessages) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(s.inputMessages))
	for _, message := range s.inputMessages {
		id := strings.TrimSpace(message.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

var _ events.Sink = (*MQStreamSink)(nil)
