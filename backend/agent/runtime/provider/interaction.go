package provider

import (
	"context"

	"github.com/insmtx/Leros/backend/agent"
)

// PermissionMode controls whether a provider requests approval for tool calls.
type PermissionMode string

const (
	// PermissionModeBypass skips provider approval requests.
	PermissionModeBypass PermissionMode = "bypass"
	// PermissionModeOnRequest forwards provider approval requests to the user.
	PermissionModeOnRequest PermissionMode = "on-request"
	// PermissionModeAuto automatically approves safe operations.
	PermissionModeAuto PermissionMode = "auto"
)

const (
	ApprovalActionApprove = "approve"
	ApprovalActionDeny    = "deny"
	ApprovalActionAlways  = "always"
)

// ApprovalHandler handles an approval request raised by a provider.
type ApprovalHandler interface {
	RequestApproval(ctx context.Context, req *agent.ApprovalRequest) (*agent.ApprovalDecision, error)
}

// ApprovalResponder writes an approval decision back to a provider process.
type ApprovalResponder interface {
	WriteDecision(requestID string, action string) error
}

// QuestionResponder writes answers back to a provider process.
type QuestionResponder interface {
	WriteAnswer(requestID string, answers [][]string) error
}

// QuestionHandler handles a question raised by a provider.
type QuestionHandler interface {
	RequestAnswer(ctx context.Context, req *agent.QuestionRequest) (*agent.QuestionAnswer, error)
}
