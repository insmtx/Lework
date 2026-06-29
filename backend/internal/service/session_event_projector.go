package service

import (
	"encoding/json"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

// ProjectRunEvent converts a messaging.RunEvent into the public session event shape.
// This is the preferred entry point for the new messaging architecture.
func ProjectRunEvent(runEvent messaging.RunEvent) (*dto.SessionEvent, bool) {
	event := &dto.SessionEvent{
		SessionID: runEvent.Route.SessionID,
		Sequence:  runEvent.Body.Seq,
		Timestamp: runEvent.CreatedAt.UnixMilli(),
	}

	switch runEvent.Body.Event {
	case messaging.RunEventMessageDelta:
		event.Type = events.EventMessageDelta
		event.Payload = dto.MessageDeltaPayload{
			MessageID: runEvent.Body.Payload.MessageID,
			Role:      string(runEvent.Body.Payload.Role),
			Content:   runEvent.Body.Payload.Content,
		}
	case messaging.RunEventReasoningDelta:
		event.Type = events.EventReasoningDelta
		event.Payload = dto.MessageDeltaPayload{
			MessageID: runEvent.Body.Payload.MessageID,
			Role:      string(runEvent.Body.Payload.Role),
			Content:   runEvent.Body.Payload.Content,
		}
	case messaging.RunEventToolCallStarted:
		if runEvent.Body.Payload.ToolCall == nil {
			return nil, false
		}
		event.Type = events.EventToolCallStarted
		event.Payload = dto.ToolCallDeltaPayload{
			ToolCallID: runEvent.Body.Payload.ToolCall.ToolCallID,
			Name:       runEvent.Body.Payload.ToolCall.Name,
			Arguments:  events.MarshalRaw(runEvent.Body.Payload.ToolCall.Arguments),
		}
	case messaging.RunEventToolCallFinished:
		if runEvent.Body.Payload.ToolResult == nil {
			return nil, false
		}
		event.Type = events.EventToolCallResult
		event.Payload = toolCallResultPayload(&events.ToolCallResultPayload{
			ToolCallID: runEvent.Body.Payload.ToolResult.ToolCallID,
			Name:       runEvent.Body.Payload.ToolResult.Name,
			Result:     events.MarshalRaw(runEvent.Body.Payload.ToolResult.Result),
			Error:      runEvent.Body.Payload.ToolResult.Error,
			IsError:    runEvent.Body.Payload.ToolResult.IsError,
			ElapsedMS:  runEvent.Body.Payload.ToolResult.ElapsedMS,
		})
	case messaging.RunEventTodoSnapshot:
		event.Type = events.EventTodoSnapshot
		event.Payload = todoPayloadFromMessaging(runEvent.Body.Payload.Todos)
	case messaging.RunEventTodoUpdated:
		event.Type = events.EventTodoUpdated
		event.Payload = todoPayloadFromMessaging(runEvent.Body.Payload.Todos)
	case messaging.RunEventArtifactDeclared:
		if runEvent.Body.Payload.Artifact == nil {
			return nil, false
		}
		event.Type = events.EventArtifactDeclared
		event.Payload = publicStreamArtifactPayload(events.ArtifactPayload{
			ArtifactID:   runEvent.Body.Payload.Artifact.ArtifactID,
			Title:        runEvent.Body.Payload.Artifact.Title,
			Filename:     runEvent.Body.Payload.Artifact.Filename,
			Description:  runEvent.Body.Payload.Artifact.Description,
			MimeType:     runEvent.Body.Payload.Artifact.MimeType,
			ArtifactType: runEvent.Body.Payload.Artifact.ArtifactType,
			FileSize:     runEvent.Body.Payload.Artifact.FileSize,
			StorageURI:   runEvent.Body.Payload.Artifact.StorageURI,
			Sha256:       runEvent.Body.Payload.Artifact.Sha256,
			CreatedAt:    runEvent.CreatedAt,
		})
	case messaging.RunEventRunStarted:
		event.Type = events.EventStarted
	case messaging.RunEventRunCompleted:
		event.Type = events.EventCompleted
		if runEvent.Body.RunCompleted != nil {
			event.Payload = terminalPayloadFromMessaging(runEvent.Body.RunCompleted, runEvent.Body.Error)
		}
	case messaging.RunEventApprovalRequested:
		event.Type = events.EventApprovalRequested
		if runEvent.Body.Payload.ApprovalRequest != nil {
			event.Payload = *runEvent.Body.Payload.ApprovalRequest
		}
	case messaging.RunEventApprovalResolved:
		event.Type = events.EventApprovalResolved
		if runEvent.Body.Payload.ApprovalDecision != nil {
			event.Payload = *runEvent.Body.Payload.ApprovalDecision
		}
	case messaging.RunEventQuestionAsked:
		event.Type = events.EventQuestionAsked
		if runEvent.Body.Payload.QuestionRequest != nil {
			event.Payload = *runEvent.Body.Payload.QuestionRequest
		}
	case messaging.RunEventQuestionAnswered:
		event.Type = events.EventQuestionAnswered
		if runEvent.Body.Payload.QuestionAnswer != nil {
			event.Payload = *runEvent.Body.Payload.QuestionAnswer
		}
	case messaging.RunEventRunFailed:
		event.Type = events.EventFailed
		message := runEvent.Body.Payload.Content
		if runEvent.Body.Error != nil {
			message = runEvent.Body.Error.Message
		}
		if runEvent.Body.RunCompleted != nil {
			event.Payload = terminalPayloadFromMessaging(runEvent.Body.RunCompleted, runEvent.Body.Error)
		} else {
			event.Payload = dto.RunStatusPayload{
				Status:  "failed",
				RunID:   runEvent.Trace.RunID,
				Message: message,
			}
		}
	case messaging.RunEventRunCancelled:
		event.Type = events.EventCancelled
		message := "已取消"
		if runEvent.Body.RunCompleted != nil && runEvent.Body.RunCompleted.Result.Message != "" {
			message = runEvent.Body.RunCompleted.Result.Message
		}
		if runEvent.Body.RunCompleted != nil {
			event.Payload = terminalPayloadFromMessaging(runEvent.Body.RunCompleted, runEvent.Body.Error)
		} else {
			event.Payload = dto.RunStatusPayload{
				Status:  "cancelled",
				RunID:   runEvent.Trace.RunID,
				Message: message,
			}
		}
	default:
		return nil, false
	}

	return event, true
}

func todoPayloadFromMessaging(items []messaging.RuntimeTodoItem) []dto.RuntimeTodoItemPayload {
	if len(items) == 0 {
		return []dto.RuntimeTodoItemPayload{}
	}
	result := make([]dto.RuntimeTodoItemPayload, 0, len(items))
	for _, item := range items {
		result = append(result, dto.RuntimeTodoItemPayload{
			ID:       item.ID,
			Title:    item.Title,
			Status:   item.Status,
			Priority: item.Priority,
		})
	}
	return result
}

// ProjectRunEventRecord converts a persisted runtime event chunk into the public session event shape.
func ProjectRunEventRecord(sessionID string, chunk types.MessageChunk) (*contract.SessionEvent, bool) {
	event := &contract.SessionEvent{
		SessionID: sessionID,
		Sequence:  chunk.Seq,
		Timestamp: chunk.Timestamp,
	}

	switch agent.EventType(chunk.Type) {
	case events.EventStarted:
		event.Type = string(events.EventStarted)
	case events.EventCompleted:
		event.Type = string(events.EventCompleted)
		if payload, ok := decodeTerminalChunk(chunk); ok {
			event.Payload = payload
		}
	case events.EventFailed:
		event.Type = string(events.EventFailed)
		if payload, ok := decodeTerminalChunk(chunk); ok {
			event.Payload = payload
		}
	case events.EventCancelled:
		event.Type = string(events.EventCancelled)
		if payload, ok := decodeTerminalChunk(chunk); ok {
			event.Payload = payload
		}
	case events.EventMessageDelta:
		payload, ok := decodeChunkPayload[events.MessageDeltaPayload](chunk)
		if !ok {
			return nil, false
		}
		event.Type = string(events.EventMessageDelta)
		event.Payload = dto.MessageDeltaPayload{
			MessageID: payload.MessageID,
			Role:      payload.Role,
			Content:   payload.Content,
		}
	case events.EventReasoningDelta:
		payload, ok := decodeChunkPayload[events.MessageDeltaPayload](chunk)
		if !ok {
			return nil, false
		}
		event.Type = string(events.EventReasoningDelta)
		event.Payload = dto.MessageDeltaPayload{
			MessageID: payload.MessageID,
			Role:      payload.Role,
			Content:   payload.Content,
		}
	case events.EventToolCallStarted:
		payload, ok := decodeChunkPayload[events.ToolCallPayload](chunk)
		if !ok {
			return nil, false
		}
		event.Type = string(events.EventToolCallStarted)
		event.Payload = dto.ToolCallDeltaPayload{
			ToolCallID: payload.ToolCallID,
			Name:       payload.Name,
			Arguments:  payload.Arguments,
		}
	case events.EventToolCallCompleted, events.EventToolCallFailed:
		payload, ok := decodeChunkPayload[events.ToolCallResultPayload](chunk)
		if !ok {
			return nil, false
		}
		event.Type = string(events.EventToolCallResult)
		event.Payload = toolCallResultPayload(&payload)
	case events.EventTodoSnapshot:
		payload, ok := decodeChunkPayload[[]events.RuntimeTodoItem](chunk)
		if !ok {
			return nil, false
		}
		event.Type = string(events.EventTodoSnapshot)
		event.Payload = todoPayload(payload)
	case events.EventTodoUpdated:
		payload, ok := decodeChunkPayload[[]events.RuntimeTodoItem](chunk)
		if !ok {
			return nil, false
		}
		event.Type = string(events.EventTodoUpdated)
		event.Payload = todoPayload(payload)
	case events.EventApprovalRequested:
		payload, ok := decodeChunkPayload[events.ApprovalRequestPayload](chunk)
		if !ok {
			return nil, false
		}
		event.Type = string(events.EventApprovalRequested)
		event.Payload = payload
	case events.EventApprovalResolved:
		payload, ok := decodeChunkPayload[events.ApprovalDecisionPayload](chunk)
		if !ok {
			return nil, false
		}
		event.Type = string(events.EventApprovalResolved)
		event.Payload = payload
	case events.EventQuestionAsked:
		payload, ok := decodeChunkPayload[events.QuestionRequestPayload](chunk)
		if !ok {
			return nil, false
		}
		event.Type = string(events.EventQuestionAsked)
		event.Payload = payload
	case events.EventQuestionAnswered:
		payload, ok := decodeChunkPayload[events.QuestionAnswerPayload](chunk)
		if !ok {
			return nil, false
		}
		event.Type = string(events.EventQuestionAnswered)
		event.Payload = payload
	case events.EventArtifactDeclared:
		payload, ok := decodeChunkPayload[events.ArtifactPayload](chunk)
		if !ok {
			return nil, false
		}
		event.Type = string(events.EventArtifactDeclared)
		event.Payload = publicStreamArtifactPayload(payload)
	default:
		return nil, false
	}

	return event, true
}

func publicStreamArtifactPayload(payload events.ArtifactPayload) events.ArtifactPayload {
	return events.ArtifactPayload{
		ArtifactID:   payload.ArtifactID,
		Title:        payload.Title,
		Filename:     payload.Filename,
		Description:  payload.Description,
		MimeType:     payload.MimeType,
		ArtifactType: payload.ArtifactType,
		FileSize:     payload.FileSize,
		CreatedAt:    payload.CreatedAt,
		StorageURI:   payload.StorageURI,
		Sha256:       payload.Sha256,
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

func decodeTerminalChunk(chunk types.MessageChunk) (dto.RunTerminalPayload, bool) {
	payload, ok := decodeChunkPayload[assistantdomain.TerminalPayload](chunk)
	if !ok {
		return dto.RunTerminalPayload{}, false
	}
	return terminalPayloadFromDomain(payload), true
}

func terminalPayloadFromDomain(payload assistantdomain.TerminalPayload) dto.RunTerminalPayload {
	return dto.RunTerminalPayload{
		Status:      payload.Status,
		Result:      messaging.RunResultPayload{Message: payload.Message},
		Error:       payload.Error,
		Artifacts:   append([]assistantdomain.ArtifactRecord(nil), payload.Artifacts...),
		Usage:       payload.Usage,
		Events:      append([]assistantdomain.TerminalEventRecord(nil), payload.Events...),
		StartedAt:   payload.StartedAt,
		CompletedAt: payload.CompletedAt,
		Metadata:    payload.Metadata,
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
		Status:      payload.Status,
		Result:      payload.Result,
		StartedAt:   payload.StartedAt,
		CompletedAt: payload.CompletedAt,
	}
	if runError != nil {
		result.Error = runError.Message
	}
	if payload.Usage != nil {
		result.Usage = &agent.Usage{
			InputTokens:  payload.Usage.InputTokens,
			OutputTokens: payload.Usage.OutputTokens,
			TotalTokens:  payload.Usage.TotalTokens,
		}
	}
	for _, artifact := range payload.Artifacts {
		result.Artifacts = append(result.Artifacts, assistantdomain.ArtifactRecord{
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
	for _, record := range payload.Events {
		result.Events = append(result.Events, assistantdomain.TerminalEventRecord{
			Seq:       record.Seq,
			LastSeq:   record.LastSeq,
			Type:      record.Type,
			Timestamp: record.Timestamp,
			Payload:   append(json.RawMessage(nil), record.Payload...),
		})
	}
	if payload.Metadata != nil {
		result.Metadata = &assistantdomain.RunMetadata{
			Runtime:    payload.Metadata.Runtime,
			WorkDir:    payload.Metadata.WorkDir,
			ProviderID: payload.Metadata.ProviderID,
			SessionID:  payload.Metadata.SessionID,
			Phase:      payload.Metadata.Phase,
			Resume:     payload.Metadata.Resume,
		}
	}
	return result
}

func toolCallResultPayload(result *events.ToolCallResultPayload) dto.ToolCallResultPayload {
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

func todoPayload(items []events.RuntimeTodoItem) []dto.RuntimeTodoItemPayload {
	if len(items) == 0 {
		return []dto.RuntimeTodoItemPayload{}
	}
	result := make([]dto.RuntimeTodoItemPayload, 0, len(items))
	for _, item := range items {
		result = append(result, dto.RuntimeTodoItemPayload{
			ID:       item.ID,
			Title:    item.Title,
			Status:   item.Status,
			Priority: item.Priority,
		})
	}
	return result
}
