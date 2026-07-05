package agent

import (
	"context"
	"encoding/json"
)

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

// ApprovalResponder writes an approval decision back to a provider runtime.
type ApprovalResponder interface {
	WriteDecision(requestID string, action string) error
}

// QuestionResponder writes question answers back to a provider runtime.
type QuestionResponder interface {
	WriteAnswer(requestID string, answers [][]string) error
}

const (
	// ApprovalActionApprove approves one provider operation.
	ApprovalActionApprove = "approve"
	// ApprovalActionDeny rejects one provider operation.
	ApprovalActionDeny = "deny"
	// ApprovalActionAlways approves matching future provider operations.
	ApprovalActionAlways = "always"
)

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
