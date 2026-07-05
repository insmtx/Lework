package domain

import (
	"encoding/json"
	"time"

	"github.com/insmtx/Leros/backend/pkg/messaging"
)

// RunStatus is the final SingerOS business status.
type RunStatus string

const (
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

// Usage describes business-visible model token usage for one run.
type Usage struct {
	TotalTokens       int `json:"total_tokens"`
	InputTokens       int `json:"input_tokens"`
	OutputTokens      int `json:"output_tokens"`
	CacheInputTokens  int `json:"cache_input_tokens"`
	CacheOutputTokens int `json:"cache_output_tokens"`
}

// ToolCallRecord is the business-visible final summary for one tool call.
type ToolCallRecord struct {
	CallID string          `json:"call_id,omitempty"`
	Name   string          `json:"name,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// RunResult is the finalized business result of one assistant run.
type RunResult struct {
	RunID       string                        `json:"run_id"`
	TraceID     string                        `json:"trace_id,omitempty"`
	Status      RunStatus                     `json:"status"`
	Message     string                        `json:"message,omitempty"`
	Error       string                        `json:"error,omitempty"`
	Usage       *Usage                        `json:"usage,omitempty"`
	ToolCalls   []ToolCallRecord              `json:"tool_calls,omitempty"`
	Artifacts   []messaging.ArtifactPayload   `json:"artifacts,omitempty"`
	Metadata    *messaging.RunMetadataPayload `json:"metadata,omitempty"`
	StartedAt   time.Time                     `json:"started_at"`
	CompletedAt time.Time                     `json:"completed_at,omitempty"`
}
