// Package events defines the shared runtime event contract and domain message protocols.
//
// This package contains:
//   - Stable runtime event contract (Event, EventType) for API and engine interfaces
//   - Domain message protocols (Envelope, WorkerTaskMessage, MessageStreamMessage) for
//     inter-service communication via message queues (NATS JetStream)
package events

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// EventType identifies one observable runtime event emitted during execution.
type EventType string

const (
	// EventStarted indicates that a runtime run has started.
	EventStarted EventType = "run.started"
	// EventCompleted indicates that a runtime run completed successfully.
	EventCompleted EventType = "run.completed"
	// EventFailed indicates that a runtime run failed.
	EventFailed EventType = "run.failed"
	// EventCancelled indicates that a runtime run was cancelled.
	EventCancelled EventType = "run.cancelled"

	// EventMessageDelta contains human-readable assistant output.
	EventMessageDelta EventType = "message.delta"
	// EventReasoningDelta contains reasoning output when available.
	EventReasoningDelta EventType = "reasoning.delta"
	// EventResult contains the final assistant result for a runtime run.
	EventResult EventType = "message.result"

	// EventToolCallStarted indicates that a tool call started.
	EventToolCallStarted EventType = "tool_call.started"
	// EventToolCallCompleted indicates that a tool call completed.
	EventToolCallCompleted EventType = "tool_call.completed"
	// EventToolCallFailed indicates that a tool call failed.
	EventToolCallFailed EventType = "tool_call.failed"
)

// Event is the stable runtime event envelope for streaming execution.
type Event struct {
	ID        string     `json:"id,omitempty"`
	RunID     string     `json:"run_id,omitempty"`
	TraceID   string     `json:"trace_id,omitempty"`
	Seq       int64      `json:"seq,omitempty"`
	Type      EventType  `json:"type"`
	CreatedAt time.Time  `json:"created_at,omitempty"`
	Payload   RawPayload `json:"payload,omitempty"`
	Content   string     `json:"content,omitempty"`
}

// RawPayload stores structured event payload JSON while keeping Event easy to marshal.
type RawPayload json.RawMessage

// MarshalJSON serializes the raw payload bytes.
func (p RawPayload) MarshalJSON() ([]byte, error) {
	if len(p) == 0 {
		return []byte("null"), nil
	}
	return p, nil
}

// UnmarshalJSON stores the raw payload bytes.
func (p *RawPayload) UnmarshalJSON(data []byte) error {
	if p == nil {
		return nil
	}
	if string(data) == "null" {
		*p = nil
		return nil
	}
	*p = append((*p)[0:0], data...)
	return nil
}

// MessageDeltaPayload is the standard payload for assistant text deltas.
type MessageDeltaPayload struct {
	MessageID string `json:"message_id,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   string `json:"content"`
}

// ToolCallPayload is the standard payload for tool call start and argument events.
type ToolCallPayload struct {
	ToolCallID string         `json:"tool_call_id"`
	Name       string         `json:"name"`
	Arguments  map[string]any `json:"arguments,omitempty"`
}

// ToolCallResultPayload is the standard payload for tool call terminal events.
type ToolCallResultPayload struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name,omitempty"`
	Result     any    `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	IsError    bool   `json:"is_error"`
	ElapsedMS  int64  `json:"elapsed_ms,omitempty"`
}

// UsagePayload describes model token usage when available.
type UsagePayload struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

// MessageResultPayload describes the final assistant result and optional usage metadata.
type MessageResultPayload struct {
	Message string        `json:"message,omitempty"`
	Usage   *UsagePayload `json:"usage,omitempty"`
}

// RunResultPayload describes the final assistant result archived in run.completed.
type RunResultPayload struct {
	Message string `json:"message,omitempty"`
}

// RunEventRecord is a normalized, archived runtime event.
type RunEventRecord struct {
	Seq       int64      `json:"seq,omitempty"`
	LastSeq   int64      `json:"last_seq,omitempty"`
	Type      EventType  `json:"type"`
	Timestamp int64      `json:"timestamp,omitempty"`
	Payload   RawPayload `json:"payload,omitempty"`
}

// RunCompletedPayload archives the complete successful runtime run.
type RunCompletedPayload struct {
	Status      string           `json:"status"`
	Result      RunResultPayload `json:"result"`
	Usage       *UsagePayload    `json:"usage,omitempty"`
	Events      []RunEventRecord `json:"events,omitempty"`
	StartedAt   time.Time        `json:"started_at,omitempty"`
	CompletedAt time.Time        `json:"completed_at,omitempty"`
	Metadata    map[string]any   `json:"metadata,omitempty"`
}

// NewMessageDelta creates a standard assistant message delta event.
func NewMessageDelta(messageID string, content string) *Event {
	return newPayloadEvent(EventMessageDelta, MessageDeltaPayload{
		MessageID: strings.TrimSpace(messageID),
		Role:      string(MessageRoleAssistant),
		Content:   content,
	}, content)
}

// NewReasoningDelta creates a standard reasoning delta event.
func NewReasoningDelta(messageID string, content string) *Event {
	return newPayloadEvent(EventReasoningDelta, MessageDeltaPayload{
		MessageID: strings.TrimSpace(messageID),
		Role:      string(MessageRoleAssistant),
		Content:   content,
	}, content)
}

// NewToolCallStarted creates a standard tool call start event.
func NewToolCallStarted(toolCallID string, name string, arguments map[string]any) *Event {
	return newPayloadEvent(EventToolCallStarted, ToolCallPayload{
		ToolCallID: strings.TrimSpace(toolCallID),
		Name:       name,
		Arguments:  arguments,
	}, "")
}

// NewToolCallCompleted creates a standard successful tool call terminal event.
func NewToolCallCompleted(toolCallID string, name string, result any, elapsedMS int64) *Event {
	return newPayloadEvent(EventToolCallCompleted, ToolCallResultPayload{
		ToolCallID: strings.TrimSpace(toolCallID),
		Name:       name,
		Result:     result,
		IsError:    false,
		ElapsedMS:  elapsedMS,
	}, "")
}

// NewToolCallFailed creates a standard failed tool call terminal event.
func NewToolCallFailed(toolCallID string, name string, message string, elapsedMS int64) *Event {
	return newPayloadEvent(EventToolCallFailed, ToolCallResultPayload{
		ToolCallID: strings.TrimSpace(toolCallID),
		Name:       name,
		Error:      message,
		IsError:    true,
		ElapsedMS:  elapsedMS,
	}, "")
}

// NewMessageResult creates a standard final assistant result event.
func NewMessageResult(message string, usage *UsagePayload) *Event {
	return newPayloadEvent(EventResult, MessageResultPayload{
		Message: message,
		Usage:   usage,
	}, message)
}

// NewRunCompleted creates a standard completed run archive event.
func NewRunCompleted(payload RunCompletedPayload, contentFallback string) *Event {
	return newPayloadEvent(EventCompleted, payload, contentFallback)
}

// DecodePayload decodes a structured payload from an event.
func DecodePayload[T any](event *Event) (T, error) {
	var zero T
	if event == nil {
		return zero, fmt.Errorf("event is nil")
	}
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &zero); err != nil {
			return zero, err
		}
		return zero, nil
	}
	if strings.TrimSpace(event.Content) == "" {
		return zero, fmt.Errorf("event payload is empty")
	}
	if err := json.Unmarshal([]byte(event.Content), &zero); err != nil {
		return zero, err
	}
	return zero, nil
}

func newPayloadEvent(eventType EventType, payload any, contentFallback string) *Event {
	raw, content := marshalPayload(payload)
	if contentFallback != "" {
		content = contentFallback
	}
	return &Event{
		Type:    eventType,
		Payload: raw,
		Content: content,
	}
}

func marshalPayload(payload any) (RawPayload, string) {
	if payload == nil {
		return nil, ""
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Sprintf("%v", payload)
	}
	return RawPayload(raw), string(raw)
}
