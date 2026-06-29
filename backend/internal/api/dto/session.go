package dto

import (
	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

type SessionEvent struct {
	Type      agent.EventType `json:"type"`
	SessionID string          `json:"session_id"`
	Payload   interface{}     `json:"payload"`
	Sequence  int64           `json:"sequence"`
	Timestamp int64           `json:"timestamp"` // Unix timestamp in milliseconds
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

type ArtifactPayload = events.ArtifactPayload

// RunTerminalPayload is the public projection of a completed, failed, or cancelled run.
type RunTerminalPayload struct {
	Status      string                                `json:"status"`
	Result      messaging.RunResultPayload            `json:"result"`
	Error       string                                `json:"error,omitempty"`
	Artifacts   []assistantdomain.ArtifactRecord      `json:"artifacts,omitempty"`
	Usage       *agent.Usage                          `json:"usage,omitempty"`
	Events      []assistantdomain.TerminalEventRecord `json:"events,omitempty"`
	StartedAt   string                                `json:"started_at,omitempty"`
	CompletedAt string                                `json:"completed_at,omitempty"`
	Metadata    *assistantdomain.RunMetadata          `json:"metadata,omitempty"`
}
