package agent

import (
	"encoding/json"
	"time"
)

// EventType identifies an observable runtime event emitted during execution.
type EventType string

// Event is the stable runtime event envelope emitted during execution.
type Event struct {
	ID        string          `json:"id,omitempty"`
	RunID     string          `json:"run_id,omitempty"`
	TraceID   string          `json:"trace_id,omitempty"`
	Seq       int64           `json:"seq,omitempty"`
	Type      EventType       `json:"type"`
	CreatedAt time.Time       `json:"created_at,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Content   string          `json:"content,omitempty"`
}

// Usage describes model token usage when available.
type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

// ToolCallRecord is a compact final tool call summary.
type ToolCallRecord struct {
	CallID string          `json:"call_id,omitempty"`
	Name   string          `json:"name,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}
