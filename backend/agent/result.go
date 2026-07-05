package agent

import (
	"encoding/json"
)

// Usage describes model token usage when available.
type Usage struct {
	TotalTokens       int `json:"total_tokens"`
	InputTokens       int `json:"input_tokens"`
	OutputTokens      int `json:"output_tokens"`
	CacheInputTokens  int `json:"cache_input_tokens"`
	CacheOutputTokens int `json:"cache_output_tokens"`
}

// EnsureUsage returns a non-nil usage object with TotalTokens normalized to input + output.
func EnsureUsage(usage *Usage) *Usage {
	if usage == nil {
		return &Usage{}
	}
	normalized := *usage
	normalized.TotalTokens = normalized.InputTokens + normalized.OutputTokens
	return &normalized
}

// ToolCallRecord is a compact final tool call summary.
type ToolCallRecord struct {
	CallID string          `json:"call_id,omitempty"`
	Name   string          `json:"name,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}
