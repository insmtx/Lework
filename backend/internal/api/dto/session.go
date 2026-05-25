package dto

import "github.com/insmtx/Leros/backend/internal/runtime/events"

type SessionEvent struct {
	Type      events.EventType `json:"type"`
	SessionID string           `json:"session_id"`
	Payload   interface{}      `json:"payload"`
	Sequence  int64            `json:"sequence"`
	Timestamp int64            `json:"timestamp"` // Unix timestamp in milliseconds
}

type MessageDeltaPayload = events.MessageDeltaPayload

type RunStatusPayload struct {
	Status  string `json:"status"`
	RunID   string `json:"run_id,omitempty"`
	Message string `json:"message,omitempty"`
}

type ToolCallDeltaPayload = events.ToolCallPayload

type ToolCallResultPayload struct {
	ToolCallID string      `json:"tool_call_id"`
	Name       string      `json:"name"`
	Result     interface{} `json:"result"`
	Status     string      `json:"status"` // success | error
}

type RuntimeTodoItemPayload = events.RuntimeTodoItem
