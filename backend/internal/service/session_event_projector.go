package service

import (
	"encoding/json"
	"time"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

const (
	sessionEventMessageResult = "message.result"
	sessionEventToolResult    = "tool_call.result"
)

// ProjectRunEvent converts a messaging.RunEvent into the public session event shape.
func ProjectRunEvent(runEvent messaging.RunEvent) (*contract.SessionEvent, bool) {
	event := &contract.SessionEvent{
		SessionID: runEvent.Route.SessionID,
		Sequence:  runEvent.Body.Seq,
		Timestamp: runEvent.CreatedAt.UnixMilli(),
	}

	switch runEvent.Body.Event {
	case messaging.RunEventMessageDelta:
		event.Type = string(messaging.RunEventMessageDelta)
		event.Payload = dto.MessageDeltaPayload{
			MessageID: runEvent.Body.Payload.MessageID,
			Role:      string(runEvent.Body.Payload.Role),
			Content:   runEvent.Body.Payload.Content,
		}
	case messaging.RunEventReasoningDelta:
		event.Type = string(messaging.RunEventReasoningDelta)
		event.Payload = dto.MessageDeltaPayload{
			MessageID: runEvent.Body.Payload.MessageID,
			Role:      string(runEvent.Body.Payload.Role),
			Content:   runEvent.Body.Payload.Content,
		}
	case messaging.RunEventMessageCompleted:
		event.Type = sessionEventMessageResult
		event.Payload = dto.MessageDeltaPayload{
			MessageID: runEvent.Body.Payload.MessageID,
			Role:      string(runEvent.Body.Payload.Role),
			Content:   runEvent.Body.Payload.Content,
		}
	case messaging.RunEventToolCallStarted:
		if runEvent.Body.Payload.ToolCall == nil {
			return nil, false
		}
		event.Type = string(messaging.RunEventToolCallStarted)
		event.Payload = dto.ToolCallDeltaPayload{
			ToolCallID: runEvent.Body.Payload.ToolCall.ToolCallID,
			Name:       runEvent.Body.Payload.ToolCall.Name,
			Arguments:  append(json.RawMessage(nil), runEvent.Body.Payload.ToolCall.Arguments...),
		}
	case messaging.RunEventToolCallFinished:
		if runEvent.Body.Payload.ToolResult == nil {
			return nil, false
		}
		event.Type = sessionEventToolResult
		event.Payload = toolCallResultPayload(runEvent.Body.Payload.ToolResult)
	case messaging.RunEventTodoSnapshot:
		event.Type = string(messaging.RunEventTodoSnapshot)
		event.Payload = todoPayload(runEvent.Body.Payload.Todos)
	case messaging.RunEventTodoUpdated:
		event.Type = string(messaging.RunEventTodoUpdated)
		event.Payload = todoPayload(runEvent.Body.Payload.Todos)
	case messaging.RunEventArtifactDeclared:
		if runEvent.Body.Payload.Artifact == nil {
			return nil, false
		}
		event.Type = string(messaging.RunEventArtifactDeclared)
		event.Payload = publicStreamArtifactPayload(
			*runEvent.Body.Payload.Artifact,
			runEvent.CreatedAt,
		)
	case messaging.RunEventRunStarted:
		event.Type = string(messaging.RunEventRunStarted)
	case messaging.RunEventRunCompleted:
		event.Type = string(messaging.RunEventRunCompleted)
		if runEvent.Body.RunCompleted != nil {
			event.Payload = terminalPayloadFromMessaging(
				runEvent.Body.RunCompleted,
				runEvent.Body.Error,
			)
		}
	case messaging.RunEventApprovalRequested:
		event.Type = string(messaging.RunEventApprovalRequested)
		if runEvent.Body.Payload.ApprovalRequest != nil {
			event.Payload = *runEvent.Body.Payload.ApprovalRequest
		}
	case messaging.RunEventApprovalResolved:
		event.Type = string(messaging.RunEventApprovalResolved)
		if runEvent.Body.Payload.ApprovalDecision != nil {
			event.Payload = *runEvent.Body.Payload.ApprovalDecision
		}
	case messaging.RunEventQuestionAsked:
		event.Type = string(messaging.RunEventQuestionAsked)
		if runEvent.Body.Payload.QuestionRequest != nil {
			event.Payload = *runEvent.Body.Payload.QuestionRequest
		}
	case messaging.RunEventQuestionAnswered:
		event.Type = string(messaging.RunEventQuestionAnswered)
		if runEvent.Body.Payload.QuestionAnswer != nil {
			event.Payload = *runEvent.Body.Payload.QuestionAnswer
		}
	case messaging.RunEventPlanPublished:
		event.Type = string(messaging.RunEventPlanPublished)
		if runEvent.Body.Payload.PlanPublished != nil {
			event.Payload = *runEvent.Body.Payload.PlanPublished
		}
	case messaging.RunEventWorkTitleUpdated:
		if runEvent.Body.Payload.WorkTitle == nil {
			return nil, false
		}
		event.Type = string(messaging.RunEventWorkTitleUpdated)
		event.Payload = *runEvent.Body.Payload.WorkTitle
	case messaging.RunEventRunFailed:
		event.Type = string(messaging.RunEventRunFailed)
		event.Payload = terminalOrStatusPayload(runEvent, "failed", "")
	case messaging.RunEventRunCancelled:
		event.Type = string(messaging.RunEventRunCancelled)
		event.Payload = terminalOrStatusPayload(runEvent, "cancelled", "已取消")
	default:
		return nil, false
	}

	return event, true
}

// ProjectRunEventRecord projects a persisted event by first restoring its
// canonical messaging.RunEvent representation.
func ProjectRunEventRecord(
	sessionID string,
	chunk types.MessageChunk,
) (*contract.SessionEvent, bool) {
	runEvent, ok := runEventFromRecord(sessionID, chunk)
	if !ok {
		return nil, false
	}
	return ProjectRunEvent(runEvent)
}

func runEventFromRecord(
	sessionID string,
	chunk types.MessageChunk,
) (messaging.RunEvent, bool) {
	eventType, ok := normalizeRecordedEventType(chunk.Type)
	if !ok {
		return messaging.RunEvent{}, false
	}
	runEvent := messaging.RunEvent{
		Type:      messaging.MessageTypeRunEvent,
		CreatedAt: time.UnixMilli(chunk.Timestamp).UTC(),
		Route:     messaging.RouteContext{SessionID: sessionID},
		Body: messaging.RunEventBody{
			Seq:   chunk.Seq,
			Event: eventType,
		},
	}

	switch eventType {
	case messaging.RunEventRunStarted:
		return runEvent, true
	case messaging.RunEventRunCompleted, messaging.RunEventRunFailed,
		messaging.RunEventRunCancelled:
		completed, runError, decoded := decodeRecordedTerminal(chunk)
		if !decoded {
			return messaging.RunEvent{}, false
		}
		runEvent.Body.RunCompleted = completed
		runEvent.Body.Error = runError
	case messaging.RunEventMessageDelta, messaging.RunEventReasoningDelta,
		messaging.RunEventMessageCompleted:
		payload, decoded := decodeChunkPayload[recordedMessagePayload](chunk)
		if !decoded {
			return messaging.RunEvent{}, false
		}
		runEvent.Body.Payload.MessageID = payload.MessageID
		runEvent.Body.Payload.Role = payload.Role
		runEvent.Body.Payload.Content = payload.Content
		runEvent.Body.Payload.Usage = payload.Usage
	case messaging.RunEventToolCallStarted:
		payload, decoded := decodeChunkPayload[messaging.ToolCallPayload](chunk)
		if !decoded {
			return messaging.RunEvent{}, false
		}
		runEvent.Body.Payload.ToolCall = &payload
	case messaging.RunEventToolCallFinished:
		payload, decoded := decodeChunkPayload[messaging.ToolCallResultPayload](chunk)
		if !decoded {
			return messaging.RunEvent{}, false
		}
		if chunk.Type == "tool_call.failed" {
			payload.IsError = true
		}
		runEvent.Body.Payload.ToolResult = &payload
	case messaging.RunEventTodoSnapshot, messaging.RunEventTodoUpdated:
		payload, decoded := decodeChunkPayload[[]messaging.RuntimeTodoItem](chunk)
		if !decoded {
			var wrapped struct {
				Items []messaging.RuntimeTodoItem `json:"items"`
			}
			wrapped, decoded = decodeChunkPayload[struct {
				Items []messaging.RuntimeTodoItem `json:"items"`
			}](chunk)
			if !decoded {
				return messaging.RunEvent{}, false
			}
			payload = wrapped.Items
		}
		runEvent.Body.Payload.Todos = payload
	case messaging.RunEventApprovalRequested:
		payload, decoded := decodeChunkPayload[messaging.ApprovalRequestPayload](chunk)
		if !decoded {
			return messaging.RunEvent{}, false
		}
		runEvent.Body.Payload.ApprovalRequest = &payload
	case messaging.RunEventApprovalResolved:
		payload, decoded := decodeChunkPayload[messaging.ApprovalDecisionPayload](chunk)
		if !decoded {
			return messaging.RunEvent{}, false
		}
		runEvent.Body.Payload.ApprovalDecision = &payload
	case messaging.RunEventQuestionAsked:
		payload, decoded := decodeChunkPayload[messaging.QuestionRequestPayload](chunk)
		if !decoded {
			return messaging.RunEvent{}, false
		}
		runEvent.Body.Payload.QuestionRequest = &payload
	case messaging.RunEventQuestionAnswered:
		payload, decoded := decodeChunkPayload[messaging.QuestionAnswerPayload](chunk)
		if !decoded {
			return messaging.RunEvent{}, false
		}
		runEvent.Body.Payload.QuestionAnswer = &payload
	case messaging.RunEventPlanPublished:
		payload, decoded := decodeChunkPayload[messaging.PlanPublishedPayload](chunk)
		if !decoded {
			return messaging.RunEvent{}, false
		}
		runEvent.Body.Payload.PlanPublished = &payload
	case messaging.RunEventArtifactDeclared:
		payload, decoded := decodeChunkPayload[messaging.ArtifactPayload](chunk)
		if !decoded {
			return messaging.RunEvent{}, false
		}
		runEvent.Body.Payload.Artifact = &payload
	case messaging.RunEventWorkTitleUpdated:
		payload, decoded := decodeChunkPayload[messaging.WorkTitleUpdatedPayload](chunk)
		if !decoded {
			return messaging.RunEvent{}, false
		}
		runEvent.Body.Payload.WorkTitle = &payload
	}
	return runEvent, true
}

func normalizeRecordedEventType(value string) (messaging.RunEventType, bool) {
	switch value {
	case string(messaging.RunEventRunStarted):
		return messaging.RunEventRunStarted, true
	case string(messaging.RunEventRunCompleted):
		return messaging.RunEventRunCompleted, true
	case string(messaging.RunEventRunFailed):
		return messaging.RunEventRunFailed, true
	case string(messaging.RunEventRunCancelled):
		return messaging.RunEventRunCancelled, true
	case string(messaging.RunEventMessageDelta):
		return messaging.RunEventMessageDelta, true
	case string(messaging.RunEventReasoningDelta):
		return messaging.RunEventReasoningDelta, true
	case string(messaging.RunEventMessageCompleted), "message.result", "message.complete":
		return messaging.RunEventMessageCompleted, true
	case string(messaging.RunEventToolCallStarted):
		return messaging.RunEventToolCallStarted, true
	case string(messaging.RunEventToolCallFinished),
		"tool_call.completed", "tool_call.failed", "tool_call.result":
		return messaging.RunEventToolCallFinished, true
	case string(messaging.RunEventTodoSnapshot):
		return messaging.RunEventTodoSnapshot, true
	case string(messaging.RunEventTodoUpdated):
		return messaging.RunEventTodoUpdated, true
	case string(messaging.RunEventApprovalRequested):
		return messaging.RunEventApprovalRequested, true
	case string(messaging.RunEventApprovalResolved):
		return messaging.RunEventApprovalResolved, true
	case string(messaging.RunEventQuestionAsked):
		return messaging.RunEventQuestionAsked, true
	case string(messaging.RunEventQuestionAnswered):
		return messaging.RunEventQuestionAnswered, true
	case string(messaging.RunEventPlanPublished):
		return messaging.RunEventPlanPublished, true
	case string(messaging.RunEventArtifactDeclared):
		return messaging.RunEventArtifactDeclared, true
	case string(messaging.RunEventWorkTitleUpdated):
		return messaging.RunEventWorkTitleUpdated, true
	default:
		return "", false
	}
}

type recordedMessagePayload struct {
	MessageID string                  `json:"message_id,omitempty"`
	Role      messaging.MessageRole   `json:"role,omitempty"`
	Content   string                  `json:"content,omitempty"`
	Message   string                  `json:"message,omitempty"`
	Usage     *messaging.UsagePayload `json:"usage,omitempty"`
}

type recordedTerminalPayload struct {
	Status      string                        `json:"status"`
	Message     string                        `json:"message,omitempty"`
	Error       string                        `json:"error,omitempty"`
	Artifacts   []messaging.ArtifactPayload   `json:"artifacts,omitempty"`
	Usage       *messaging.UsagePayload       `json:"usage,omitempty"`
	Events      []messaging.RunEventRecord    `json:"events,omitempty"`
	StartedAt   string                        `json:"started_at,omitempty"`
	CompletedAt string                        `json:"completed_at,omitempty"`
	Metadata    *messaging.RunMetadataPayload `json:"metadata,omitempty"`
	Result      messaging.RunResultPayload    `json:"result,omitempty"`
}

func decodeRecordedTerminal(
	chunk types.MessageChunk,
) (*messaging.RunCompletedPayload, *messaging.RunEventError, bool) {
	payload, ok := decodeChunkPayload[recordedTerminalPayload](chunk)
	if !ok {
		return nil, nil, false
	}
	if payload.Result.Message == "" {
		payload.Result.Message = payload.Message
	}
	completed := &messaging.RunCompletedPayload{
		Status: payload.Status, Result: payload.Result,
		Artifacts: payload.Artifacts, Usage: payload.Usage,
		Events: payload.Events, StartedAt: payload.StartedAt,
		CompletedAt: payload.CompletedAt, Metadata: payload.Metadata,
	}
	var runError *messaging.RunEventError
	if payload.Error != "" {
		runError = &messaging.RunEventError{Message: payload.Error}
	}
	return completed, runError, true
}

func terminalOrStatusPayload(
	runEvent messaging.RunEvent,
	status string,
	fallback string,
) any {
	if runEvent.Body.RunCompleted != nil {
		return terminalPayloadFromMessaging(
			runEvent.Body.RunCompleted,
			runEvent.Body.Error,
		)
	}
	message := runEvent.Body.Payload.Content
	if runEvent.Body.Error != nil {
		message = runEvent.Body.Error.Message
	}
	if message == "" {
		message = fallback
	}
	return dto.RunStatusPayload{
		Status: status, RunID: runEvent.Trace.RunID, Message: message,
	}
}

func terminalPayloadFromMessaging(
	payload *messaging.RunCompletedPayload,
	runError *messaging.RunEventError,
) dto.RunTerminalPayload {
	if payload == nil {
		return dto.RunTerminalPayload{}
	}
	result := dto.RunTerminalPayload{
		Status: payload.Status, Result: payload.Result,
		Artifacts: cloneArtifacts(payload.Artifacts),
		Usage:     cloneUsage(payload.Usage),
		Events:    cloneEventRecords(payload.Events),
		StartedAt: payload.StartedAt, CompletedAt: payload.CompletedAt,
		Metadata: cloneRunMetadata(payload.Metadata),
	}
	if runError != nil {
		result.Error = runError.Message
	}
	return result
}

func toolCallResultPayload(
	result *messaging.ToolCallResultPayload,
) dto.ToolCallResultPayload {
	status := "success"
	var value any
	if len(result.Result) > 0 {
		if err := json.Unmarshal(result.Result, &value); err != nil {
			value = string(result.Result)
		}
	}
	if result.IsError {
		status = "error"
		if value == nil {
			value = result.Error
		}
	}
	return dto.ToolCallResultPayload{
		ToolCallID: result.ToolCallID,
		Name:       result.Name,
		Result:     value,
		Status:     status,
	}
}

func todoPayload(items []messaging.RuntimeTodoItem) []dto.RuntimeTodoItemPayload {
	if len(items) == 0 {
		return []dto.RuntimeTodoItemPayload{}
	}
	result := make([]dto.RuntimeTodoItemPayload, len(items))
	for index, item := range items {
		result[index] = dto.RuntimeTodoItemPayload{
			ID: item.ID, Title: item.Title, Status: item.Status, Priority: item.Priority,
		}
	}
	return result
}

func publicStreamArtifactPayload(
	payload messaging.ArtifactPayload,
	createdAt time.Time,
) messaging.ArtifactPayload {
	return messaging.ArtifactPayload{
		ArtifactID: payload.ArtifactID, Title: payload.Title,
		Filename: payload.Filename, Description: payload.Description,
		MimeType: payload.MimeType, ArtifactType: payload.ArtifactType,
		FileSize: payload.FileSize, CreatedAt: firstNonEmpty(
			payload.CreatedAt,
			createdAt.Format(time.RFC3339Nano),
		),
		StorageURI: payload.StorageURI, Sha256: payload.Sha256,
	}
}

func decodeChunkPayload[T any](chunk types.MessageChunk) (T, bool) {
	var value T
	if len(chunk.Payload) == 0 {
		return value, false
	}
	if err := json.Unmarshal(chunk.Payload, &value); err != nil {
		return value, false
	}
	return value, true
}

func cloneArtifacts(
	source []messaging.ArtifactPayload,
) []messaging.ArtifactPayload {
	return append([]messaging.ArtifactPayload(nil), source...)
}

func cloneUsage(source *messaging.UsagePayload) *messaging.UsagePayload {
	if source == nil {
		return &messaging.UsagePayload{}
	}
	return &messaging.UsagePayload{
		TotalTokens:       source.InputTokens + source.OutputTokens,
		InputTokens:       source.InputTokens,
		OutputTokens:      source.OutputTokens,
		CacheInputTokens:  source.CacheInputTokens,
		CacheOutputTokens: source.CacheOutputTokens,
	}
}

func cloneEventRecords(
	source []messaging.RunEventRecord,
) []messaging.RunEventRecord {
	result := make([]messaging.RunEventRecord, len(source))
	for index, record := range source {
		result[index] = record
		result[index].Payload = append(json.RawMessage(nil), record.Payload...)
	}
	return result
}

func cloneRunMetadata(
	source *messaging.RunMetadataPayload,
) *messaging.RunMetadataPayload {
	if source == nil {
		return nil
	}
	cloned := *source
	return &cloned
}
