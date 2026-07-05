package dto

import (
	"encoding/json"

	"github.com/insmtx/Leros/backend/pkg/messaging"
)

// MessageDeltaPayload is the public message/reasoning delta payload.
type MessageDeltaPayload struct {
	MessageID string `json:"message_id,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   string `json:"content"`
}

// RunStatusPayload is the public fallback payload for terminal events.
type RunStatusPayload struct {
	Status  string `json:"status"`
	RunID   string `json:"run_id,omitempty"`
	Message string `json:"message,omitempty"`
}

// ToolCallDeltaPayload is the public tool-start payload.
type ToolCallDeltaPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
}

// ToolCallResultPayload is the public normalized tool result.
type ToolCallResultPayload struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Result     any    `json:"result"`
	Status     string `json:"status"` // success | error
}

// RuntimeTodoItemPayload is the public runtime todo item.
type RuntimeTodoItemPayload struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority string `json:"priority,omitempty"`
}

// RunTerminalPayload is the public projection of a completed, failed, or cancelled run.
type RunTerminalPayload struct {
	Status      string                        `json:"status"`
	Result      messaging.RunResultPayload    `json:"result"`
	Error       string                        `json:"error,omitempty"`
	Artifacts   []messaging.ArtifactPayload   `json:"artifacts,omitempty"`
	Usage       *messaging.UsagePayload       `json:"usage,omitempty"`
	Events      []messaging.RunEventRecord    `json:"events,omitempty"`
	StartedAt   string                        `json:"started_at,omitempty"`
	CompletedAt string                        `json:"completed_at,omitempty"`
	Metadata    *messaging.RunMetadataPayload `json:"metadata,omitempty"`
}
