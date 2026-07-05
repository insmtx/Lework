package agent

import (
	"context"
	"encoding/json"
)

// ToolDefinition describes a tool exposed to a Runtime.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolResult is the result returned by a tool execution.
type ToolResult struct {
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
	IsError bool   `json:"is_error"`
}

// Tool is the contract for a callable tool within an agent Runtime.
// Implementations decode json.RawMessage into a typed request struct,
// execute the operation, and return a ToolResult.
type Tool interface {
	// Definition returns the tool metadata (name, description, parameters schema).
	Definition() ToolDefinition

	// Execute runs the tool with the given JSON input.
	Execute(ctx context.Context, input json.RawMessage) (ToolResult, error)
}
