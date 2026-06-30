// Package eventpub provides the NATS-backed EventSink implementation
// that maps agent.Event to messaging.RunEvent and publishes to the appropriate JetStream lane.
package eventpub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

const terminalPublishTimeout = 5 * time.Second

// RunEventContext carries the routing/tracing context for a single run's events.
type RunEventContext struct {
	OrgID             uint
	WorkerID          uint
	SessionID         string
	TraceID           string
	RequestID         string
	TaskID            string
	RunID             string
	ParentID          string
	ReplyToMessageIDs []string
}

// EventSinkFactory creates EventSinks bound to a specific run context.
type EventSinkFactory interface {
	NewEventSink(ctx RunEventContext) agent.EventSink
}

// NATSEventSink publishes agent.Event as messaging.RunEvent via JetStream,
// routing high-frequency deltas to the run.stream lane and low-frequency state
// events to the run.state lane.
type NATSEventSink struct {
	bus           eventbus.Publisher
	eventContext  RunEventContext
	replyToMsgIDs []string
}

// NewNATSEventSink creates a new NATS-backed event sink.
func NewNATSEventSink(bus eventbus.Publisher, eventContext RunEventContext) *NATSEventSink {
	return &NATSEventSink{
		bus:           bus,
		eventContext:  eventContext,
		replyToMsgIDs: dedupMessageIDs(eventContext.ReplyToMessageIDs),
	}
}

// Emit publishes an agent event to the appropriate NATS lane.
// Returns an error when the event cannot be delivered (subject build failure
// or NATS publish failure). Terminal events use a without-cancel + timeout context.
func (s *NATSEventSink) Emit(ctx context.Context, event *agent.Event) error {
	if s == nil || s.bus == nil || event == nil {
		return nil
	}

	runEventType, err := mapRunEventType(event.Type)
	if err != nil {
		return fmt.Errorf("map event type %q: %w", event.Type, err)
	}

	msg := messaging.RunEvent{
		ID:        fmt.Sprintf("%s:%d", event.RunID, event.Seq),
		Type:      messaging.MessageTypeRunEvent,
		CreatedAt: time.Now().UTC(),
		Trace: messaging.TraceContext{
			TraceID:   event.TraceID,
			RequestID: s.eventContext.RequestID,
			TaskID:    s.eventContext.TaskID,
			RunID:     event.RunID,
			ParentID:  s.eventContext.ParentID,
		},
		Route: messaging.RouteContext{
			OrgID:     s.eventContext.OrgID,
			WorkerID:  s.eventContext.WorkerID,
			SessionID: s.eventContext.SessionID,
		},
		Body: messaging.RunEventBody{
			Seq:               event.Seq,
			Event:             runEventType,
			Payload:           mapRunEventPayload(event),
			ReplyToMessageIDs: s.replyToMsgIDs,
		},
	}

	// For terminal events, unmarshal the RunCompletedPayload from the structured payload.
	if isTerminalRunEvent(runEventType) && len(event.Payload) > 0 {
		var tp assistantdomain.TerminalPayload
		if json.Unmarshal(event.Payload, &tp) == nil {
			msg.Body.RunCompleted = &messaging.RunCompletedPayload{
				Status:      tp.Status,
				Result:      messaging.RunResultPayload{Message: tp.Message},
				Usage:       convertUsage(tp.Usage),
				StartedAt:   tp.StartedAt,
				CompletedAt: tp.CompletedAt,
				Metadata:    metadataPayload(tp.Metadata),
				Artifacts:   convertArtifacts(tp.Artifacts),
			}
			// Convert artifact events.
			if len(tp.Events) > 0 {
				msg.Body.RunCompleted.Events = make([]messaging.RunEventRecord, 0, len(tp.Events))
				for _, ev := range tp.Events {
					msg.Body.RunCompleted.Events = append(msg.Body.RunCompleted.Events, messaging.RunEventRecord{
						Seq:       ev.Seq,
						LastSeq:   ev.LastSeq,
						Type:      ev.Type,
						Timestamp: ev.Timestamp,
						Payload:   append(json.RawMessage(nil), ev.Payload...),
					})
				}
			}
		}
	}
	if runEventType == messaging.RunEventRunFailed || runEventType == messaging.RunEventRunCancelled {
		msg.Body.Error = &messaging.RunEventError{Message: event.Content}
		// Include technical error from TerminalPayload if available.
		if len(event.Payload) > 0 {
			var tp assistantdomain.TerminalPayload
			if json.Unmarshal(event.Payload, &tp) == nil && tp.Error != "" {
				msg.Body.Error.Message = tp.Error
			}
		}
	}

	// Classify event to lane.
	lane := messaging.ClassifyRunEvent(runEventType)

	// Build lane subject.
	topic, err := messaging.RunEventSubject(s.eventContext.OrgID, s.eventContext.SessionID, lane)
	if err != nil {
		return fmt.Errorf("build run event subject: %w", err)
	}

	publishCtx := ctx
	publishCancel := func() {}
	if isTerminalRunEvent(runEventType) {
		publishCtx, publishCancel = terminalPublishContext(ctx)
	}
	defer publishCancel()

	if err := s.bus.Publish(publishCtx, topic, msg); err != nil {
		return fmt.Errorf("publish run event to %s: %w", topic, err)
	}

	return nil
}

func convertArtifacts(artifacts []assistantdomain.ArtifactRecord) []messaging.ArtifactPayload {
	if len(artifacts) == 0 {
		return nil
	}
	result := make([]messaging.ArtifactPayload, 0, len(artifacts))
	for _, artifact := range artifacts {
		result = append(result, messaging.ArtifactPayload{
			ArtifactID:   artifact.ArtifactID,
			Title:        artifact.Title,
			Filename:     artifact.Filename,
			OriginalName: artifact.OriginalName,
			Description:  artifact.Description,
			MimeType:     artifact.MimeType,
			ArtifactType: artifact.ArtifactType,
			FileSize:     artifact.FileSize,
			RelativePath: artifact.RelativePath,
			StorageKey:   artifact.StorageKey,
			StorageURI:   artifact.StorageURI,
			Sha256:       artifact.Sha256,
			Source:       artifact.Source,
			Status:       artifact.Status,
		})
	}
	return result
}

func metadataPayload(metadata *assistantdomain.RunMetadata) *messaging.RunMetadataPayload {
	if metadata == nil {
		return nil
	}
	return &messaging.RunMetadataPayload{
		Runtime:    metadata.Runtime,
		WorkDir:    metadata.WorkDir,
		ProviderID: metadata.ProviderID,
		SessionID:  metadata.SessionID,
		Phase:      metadata.Phase,
		Resume:     metadata.Resume,
	}
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

// mapRunEventType maps agent EventType to messaging RunEventType.
// Returns an error for unknown event types instead of silently mapping to a default.
func mapRunEventType(eventType agent.EventType) (messaging.RunEventType, error) {
	switch eventType {
	case "run.started":
		return messaging.RunEventRunStarted, nil
	case "run.completed":
		return messaging.RunEventRunCompleted, nil
	case "run.cancelled":
		return messaging.RunEventRunCancelled, nil
	case "run.failed":
		return messaging.RunEventRunFailed, nil
	case "message.delta":
		return messaging.RunEventMessageDelta, nil
	case "reasoning.delta":
		return messaging.RunEventReasoningDelta, nil
	case "message.result":
		return messaging.RunEventMessageCompleted, nil
	case "tool_call.started":
		return messaging.RunEventToolCallStarted, nil
	case "tool_call.completed", "tool_call.failed":
		return messaging.RunEventToolCallFinished, nil
	case "todo.snapshot":
		return messaging.RunEventTodoSnapshot, nil
	case "todo.updated":
		return messaging.RunEventTodoUpdated, nil
	case "artifact.declared":
		return messaging.RunEventArtifactDeclared, nil
	case "approval.requested":
		return messaging.RunEventApprovalRequested, nil
	case "approval.resolved":
		return messaging.RunEventApprovalResolved, nil
	case "question.asked":
		return messaging.RunEventQuestionAsked, nil
	case "question.answered":
		return messaging.RunEventQuestionAnswered, nil
	default:
		return "", fmt.Errorf("unknown agent event type: %q", eventType)
	}
}

// convertUsage converts agent.Usage to messaging.UsagePayload.
func convertUsage(u *agent.Usage) *messaging.UsagePayload {
	if u == nil {
		return nil
	}
	return &messaging.UsagePayload{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		TotalTokens:  u.TotalTokens,
	}
}

// mapRunEventPayload maps agent.Event payload to messaging.RunEventPayload.
func mapRunEventPayload(event *agent.Event) messaging.RunEventPayload {
	if event == nil {
		return messaging.RunEventPayload{Role: messaging.MessageRoleAssistant}
	}
	payload := messaging.RunEventPayload{
		Role:    messaging.MessageRoleAssistant,
		Content: event.Content,
	}
	switch event.Type {
	case "message.delta", "reasoning.delta":
		if len(event.Payload) > 0 {
			var mp events.MessageDeltaPayload
			if json.Unmarshal(event.Payload, &mp) == nil {
				payload.MessageID = mp.MessageID
				payload.Role = messaging.MessageRole(mp.Role)
				payload.Content = mp.Content
				if payload.Role == "" {
					payload.Role = messaging.MessageRoleAssistant
				}
			}
		}
	case "tool_call.started":
		if len(event.Payload) > 0 {
			var tp events.ToolCallPayload
			if json.Unmarshal(event.Payload, &tp) == nil {
				payload.ToolCall = &messaging.ToolCallPayload{
					ToolCallID: tp.ToolCallID,
					Name:       tp.Name,
					Arguments:  tp.Arguments,
				}
			}
		}
	case "tool_call.completed", "tool_call.failed":
		if len(event.Payload) > 0 {
			var rp events.ToolCallResultPayload
			if json.Unmarshal(event.Payload, &rp) == nil {
				payload.ToolResult = &messaging.ToolCallResultPayload{
					ToolCallID: rp.ToolCallID,
					Name:       rp.Name,
					Result:     rp.Result,
					Error:      rp.Error,
					IsError:    rp.IsError,
					ElapsedMS:  rp.ElapsedMS,
				}
			}
		}
	case "todo.snapshot", "todo.updated":
		if len(event.Payload) > 0 {
			var todos []events.RuntimeTodoItem
			if json.Unmarshal(event.Payload, &todos) == nil {
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
		}
	case "artifact.declared":
		if len(event.Payload) > 0 {
			var ap events.ArtifactPayload
			if json.Unmarshal(event.Payload, &ap) == nil {
				payload.Artifact = &messaging.ArtifactPayload{
					ArtifactID:   ap.ArtifactID,
					Title:        ap.Title,
					Filename:     ap.Filename,
					OriginalName: ap.OriginalName,
					Description:  ap.Description,
					MimeType:     ap.MimeType,
					ArtifactType: ap.ArtifactType,
					FileSize:     ap.FileSize,
					RelativePath: ap.RelativePath,
					StorageKey:   ap.StorageKey,
					StorageURI:   ap.StorageURI,
					Sha256:       ap.Sha256,
					Source:       ap.Source,
					Status:       ap.Status,
				}
			}
		}
	case "approval.requested":
		if len(event.Payload) > 0 {
			var ar events.ApprovalRequestPayload
			if json.Unmarshal(event.Payload, &ar) == nil {
				payload.ApprovalRequest = &messaging.ApprovalRequestPayload{
					RequestID:   ar.RequestID,
					ToolName:    ar.ToolName,
					ToolCallID:  ar.ToolCallID,
					Description: ar.Description,
					Arguments:   ar.Arguments,
					Metadata:    ar.Metadata,
				}
			}
		}
	case "approval.resolved":
		if len(event.Payload) > 0 {
			var ad events.ApprovalDecisionPayload
			if json.Unmarshal(event.Payload, &ad) == nil {
				payload.ApprovalDecision = &messaging.ApprovalDecisionPayload{
					RequestID: ad.RequestID,
					Action:    ad.Action,
					Reason:    ad.Reason,
				}
			}
		}
	case "question.asked":
		if len(event.Payload) > 0 {
			var qr events.QuestionRequestPayload
			if json.Unmarshal(event.Payload, &qr) == nil {
				payload.QuestionRequest = mapQuestionRequestPayload(qr)
			}
		}
	case "question.answered":
		if len(event.Payload) > 0 {
			var qa events.QuestionAnswerPayload
			if json.Unmarshal(event.Payload, &qa) == nil {
				payload.QuestionAnswer = &messaging.QuestionAnswerPayload{
					RequestID: qa.RequestID,
					Answers:   qa.Answers,
				}
			}
		}
	}
	return payload
}

func mapQuestionRequestPayload(q events.QuestionRequestPayload) *messaging.QuestionRequestPayload {
	mq := &messaging.QuestionRequestPayload{
		RequestID:       q.RequestID,
		SessionID:       q.SessionID,
		ToolCallID:      q.ToolCallID,
		MessageID:       q.MessageID,
		InteractionType: q.InteractionType,
		Metadata:        q.Metadata,
	}
	if q.Plan != nil {
		mq.Plan = &messaging.PlanHandoffPayload{
			Content:  q.Plan.Content,
			FilePath: q.Plan.FilePath,
			Error:    q.Plan.Error,
		}
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

func dedupMessageIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

var _ agent.EventSink = (*NATSEventSink)(nil)
