package service

import (
	"encoding/json"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/insmtx/Leros/backend/types"
)

// ProjectStreamMessage converts a worker stream message into the public session event shape.
func ProjectStreamMessage(streamMsg protocol.MessageStreamMessage) (*dto.SessionEvent, bool) {
	event := &dto.SessionEvent{
		SessionID: streamMsg.Route.SessionID,
		Sequence:  streamMsg.Body.Seq,
		Timestamp: streamMsg.CreatedAt.UnixMilli(),
	}

	switch streamMsg.Body.Event {
	case protocol.StreamEventMessageDelta:
		event.Type = events.EventMessageDelta
		event.Payload = dto.MessageDeltaPayload{
			MessageID: streamMsg.Body.Payload.MessageID,
			Role:      string(streamMsg.Body.Payload.Role),
			Content:   streamMsg.Body.Payload.Content,
		}
	case protocol.StreamEventReasoningDelta:
		event.Type = events.EventReasoningDelta
		event.Payload = dto.MessageDeltaPayload{
			MessageID: streamMsg.Body.Payload.MessageID,
			Role:      string(streamMsg.Body.Payload.Role),
			Content:   streamMsg.Body.Payload.Content,
		}
	case protocol.StreamEventToolCallStarted:
		if streamMsg.Body.Payload.ToolCall == nil {
			return nil, false
		}
		event.Type = events.EventToolCallStarted
		event.Payload = dto.ToolCallDeltaPayload{
			ToolCallID: streamMsg.Body.Payload.ToolCall.ToolCallID,
			Name:       streamMsg.Body.Payload.ToolCall.Name,
			Arguments:  streamMsg.Body.Payload.ToolCall.Arguments,
		}
	case protocol.StreamEventToolCallFinished:
		if streamMsg.Body.Payload.ToolResult == nil {
			return nil, false
		}
		event.Type = events.EventToolCallResult
		event.Payload = toolCallResultPayload(streamMsg.Body.Payload.ToolResult)
	case protocol.StreamEventTodoSnapshot:
		event.Type = events.EventTodoSnapshot
		event.Payload = todoPayload(streamMsg.Body.Payload.Todos)
	case protocol.StreamEventTodoUpdated:
		event.Type = events.EventTodoUpdated
		event.Payload = todoPayload(streamMsg.Body.Payload.Todos)
	case protocol.StreamEventArtifactDeclared:
		if streamMsg.Body.Payload.Artifact == nil {
			return nil, false
		}
		event.Type = events.EventArtifactDeclared
		event.Payload = publicStreamArtifactPayload(*streamMsg.Body.Payload.Artifact)
	case protocol.StreamEventRunStarted:
		event.Type = events.EventStarted
	case protocol.StreamEventRunCompleted:
		event.Type = events.EventCompleted
		if streamMsg.Body.RunCompleted != nil {
			event.Payload = publicRunCompletedPayload(streamMsg.Body.RunCompleted)
		} else {
			event.Payload = dto.RunStatusPayload{
				Status:  "completed",
				RunID:   streamMsg.Trace.RunID,
				Message: streamMsg.Body.Payload.Content,
			}
		}
	case protocol.StreamEventApprovalRequested:
		event.Type = events.EventApprovalRequested
		if streamMsg.Body.Payload.ApprovalRequest != nil {
			event.Payload = *streamMsg.Body.Payload.ApprovalRequest
		}
	case protocol.StreamEventApprovalResolved:
		event.Type = events.EventApprovalResolved
		if streamMsg.Body.Payload.ApprovalDecision != nil {
			event.Payload = *streamMsg.Body.Payload.ApprovalDecision
		}
	case protocol.StreamEventQuestionAsked:
		event.Type = events.EventQuestionAsked
		if streamMsg.Body.Payload.QuestionRequest != nil {
			event.Payload = *streamMsg.Body.Payload.QuestionRequest
		}
	case protocol.StreamEventQuestionAnswered:
		event.Type = events.EventQuestionAnswered
		if streamMsg.Body.Payload.QuestionAnswer != nil {
			event.Payload = *streamMsg.Body.Payload.QuestionAnswer
		}
	case protocol.StreamEventRunFailed:
		event.Type = events.EventFailed
		message := streamMsg.Body.Payload.Content
		if streamMsg.Body.Error != nil {
			message = streamMsg.Body.Error.Message
		}
		event.Payload = dto.RunStatusPayload{
			Status:  "failed",
			RunID:   streamMsg.Trace.RunID,
			Message: message,
		}
	default:
		return nil, false
	}

	return event, true
}

// ProjectRunEventRecord converts a persisted runtime event chunk into the public session event shape.
func ProjectRunEventRecord(sessionID string, chunk types.MessageChunk) (*contract.SessionEvent, bool) {
	event := &contract.SessionEvent{
		SessionID: sessionID,
		Sequence:  chunk.Seq,
		Timestamp: chunk.Timestamp,
	}

	switch events.EventType(chunk.Type) {
	case events.EventStarted:
		event.Type = string(events.EventStarted)
	case events.EventCompleted:
		event.Type = string(events.EventCompleted)
		if payload, ok := decodeChunkPayload[events.RunCompletedPayload](chunk); ok {
			event.Payload = payload
		}
	case events.EventFailed, events.EventCancelled:
		event.Type = string(events.EventFailed)
		if payload, ok := decodeChunkPayload[events.RunCompletedPayload](chunk); ok {
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
		event.Payload = payload
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
		MimeType:     payload.MimeType,
		ArtifactType: payload.ArtifactType,
		CreatedAt:    payload.CreatedAt,
	}
}

func publicRunCompletedPayload(payload *events.RunCompletedPayload) *events.RunCompletedPayload {
	if payload == nil {
		return nil
	}
	result := *payload
	if len(payload.Artifacts) > 0 {
		result.Artifacts = make([]events.ArtifactPayload, 0, len(payload.Artifacts))
		for _, artifact := range payload.Artifacts {
			result.Artifacts = append(result.Artifacts, publicStreamArtifactPayload(artifact))
		}
	}
	return &result
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

func toolCallResultPayload(result *events.ToolCallResultPayload) dto.ToolCallResultPayload {
	status := "success"
	value := result.Result
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
