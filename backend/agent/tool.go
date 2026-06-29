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

// InteractionHandler handles approval and question requests from a Runtime.
// It is injected at Runtime construction time; Runtime MUST NOT depend on
// a package-level default.
type InteractionHandler interface {
	// RequestApproval asks for user approval on a tool call.
	// It blocks until a decision is made or the context is cancelled.
	RequestApproval(ctx context.Context, req *ApprovalRequest) (*ApprovalDecision, error)

	// RequestAnswer asks the user to answer a set of questions.
	// It blocks until answers are received or the context is cancelled.
	RequestAnswer(ctx context.Context, req *QuestionRequest) (*QuestionAnswer, error)
}

// ApprovalRequest carries the details needed for an approval decision.
type ApprovalRequest struct {
	RequestID   string
	ToolCallID  string
	ToolName    string
	Arguments   json.RawMessage
	Description string
	Runtime     string
}

// ApprovalDecision is the user's response to an approval request.
type ApprovalDecision struct {
	RequestID string
	Action    string // "approve" | "deny" | "always"
	Reason    string
}

// QuestionRequest carries one or more questions from a Runtime.
type QuestionRequest struct {
	RequestID   string
	SessionKey  string
	Questions   []QuestionItem
	ToolCallID  string
	Description string
	Runtime     string
}

// QuestionItem is a single question in a QuestionRequest.
type QuestionItem struct {
	Question    string
	Header      string
	Options     []QuestionOption
	MultiSelect bool
	Custom      bool
}

// QuestionOption is one option for a QuestionItem.
type QuestionOption struct {
	Label       string
	Description string
}

// QuestionAnswer carries the user's response to a QuestionRequest.
type QuestionAnswer struct {
	RequestID string
	Answers   [][]string
}
