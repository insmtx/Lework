package service

import (
	"encoding/json"

	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/types"
)

// ProjectStreamMessage converts a worker stream message into the public session event shape.
func ProjectStreamMessage(streamMsg events.MessageStreamMessage) (*dto.SessionEvent, bool) {
	event := &dto.SessionEvent{
		SessionID: streamMsg.Route.SessionID,
		Sequence:  streamMsg.Body.Seq,
		Timestamp: streamMsg.CreatedAt.UnixMilli(),
	}

	switch streamMsg.Body.Event {
	case events.StreamEventMessageDelta:
		event.Type = dto.SessionEventTypeMessageDelta
		event.Payload = dto.MessageDeltaPayload{
			MessageID: streamMsg.Body.Payload.MessageID,
			Role:      string(streamMsg.Body.Payload.Role),
			Content:   streamMsg.Body.Payload.Content,
		}
	case events.StreamEventReasoningDelta:
		event.Type = dto.SessionEventTypeReasoningDelta
		event.Payload = dto.MessageDeltaPayload{
			MessageID: streamMsg.Body.Payload.MessageID,
			Role:      string(streamMsg.Body.Payload.Role),
			Content:   streamMsg.Body.Payload.Content,
		}
	case events.StreamEventToolCallStarted:
		if streamMsg.Body.Payload.ToolCall == nil {
			return nil, false
		}
		event.Type = dto.SessionEventTypeToolCallStarted
		event.Payload = dto.ToolCallDeltaPayload{
			ToolCallID: streamMsg.Body.Payload.ToolCall.ToolCallID,
			Name:       streamMsg.Body.Payload.ToolCall.Name,
			Arguments:  streamMsg.Body.Payload.ToolCall.Arguments,
		}
	case events.StreamEventToolCallFinished:
		if streamMsg.Body.Payload.ToolResult == nil {
			return nil, false
		}
		event.Type = dto.SessionEventTypeToolCallResult
		event.Payload = toolCallResultPayload(streamMsg.Body.Payload.ToolResult)
	case events.StreamEventRunStarted:
		event.Type = dto.SessionEventTypeRunStarted
	case events.StreamEventRunCompleted:
		event.Type = dto.SessionEventTypeRunCompleted
		if streamMsg.Body.RunCompleted != nil {
			event.Payload = streamMsg.Body.RunCompleted
		} else {
			event.Payload = dto.RunStatusPayload{
				Status:  "completed",
				RunID:   streamMsg.Trace.RunID,
				Message: streamMsg.Body.Payload.Content,
			}
		}
	case events.StreamEventRunFailed:
		event.Type = dto.SessionEventTypeRunFailed
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
		event.Type = string(dto.SessionEventTypeRunStarted)
	case events.EventCompleted:
		event.Type = string(dto.SessionEventTypeRunCompleted)
		if payload, ok := decodeChunkPayload[events.RunCompletedPayload](chunk); ok {
			event.Payload = payload
		}
	case events.EventFailed, events.EventCancelled:
		event.Type = string(dto.SessionEventTypeRunFailed)
		if payload, ok := decodeChunkPayload[events.RunCompletedPayload](chunk); ok {
			event.Payload = payload
		}
	case events.EventMessageDelta:
		payload, ok := decodeChunkPayload[events.MessageDeltaPayload](chunk)
		if !ok {
			return nil, false
		}
		event.Type = string(dto.SessionEventTypeMessageDelta)
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
		event.Type = string(dto.SessionEventTypeReasoningDelta)
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
		event.Type = string(dto.SessionEventTypeToolCallStarted)
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
		event.Type = string(dto.SessionEventTypeToolCallResult)
		event.Payload = toolCallResultPayload(&payload)
	default:
		return nil, false
	}

	return event, true
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
