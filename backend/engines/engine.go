// Package engines defines the execution boundary for external agent CLI engines.
package engines

import (
	"context"
	"io"
	"time"

	"github.com/insmtx/Leros/backend/internal/runtime/events"
)

const (
	// EngineClaude is the registry name for Claude Code.
	EngineClaude = "claude"
	// EngineCodex is the registry name for Codex CLI.
	EngineCodex = "codex"
)

const (
	// EventProviderSessionStarted indicates that the provider created or exposed a native session ID.
	EventProviderSessionStarted events.EventType = "provider_session.started"
)

// PermissionMode controls whether/how the engine requests user approval for tool calls.
type PermissionMode string

const (
	// PermissionModeBypass skips all approval requests — equivalent to current behavior.
	PermissionModeBypass PermissionMode = "bypass"
	// PermissionModeOnRequest forwards every tool call approval request to the user.
	PermissionModeOnRequest PermissionMode = "on-request"
	// PermissionModeAuto automatically approves safe operations and forwards risky ones.
	PermissionModeAuto PermissionMode = "auto"
)

// ApprovalRequest describes a tool call that needs user approval.
type ApprovalRequest struct {
	RequestID  string
	ToolCallID string
	ToolName   string
	Arguments  map[string]any
	Description string
	Engine     string // "claude" | "codex"
}

// ApprovalDecision carries the user's decision on an approval request.
type ApprovalDecision struct {
	RequestID string
	Action    string // "approved" | "rejected"
	Reason    string
}

// ApprovalHandler processes approval requests from engines.
// Implementations must be thread-safe.
type ApprovalHandler interface {
	// RequestApproval submits an approval request and blocks until a decision is made.
	// Returns the decision or an error if the request was cancelled/timed out.
	RequestApproval(ctx context.Context, req *ApprovalRequest) (*ApprovalDecision, error)
}

// PrepareRequest contains engine-specific workspace preparation input.
type PrepareRequest struct {
	WorkDir string
}

// ModelConfig carries model settings injected into CLI processes.
type ModelConfig struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
}

// RunRequest contains all input needed to execute one external CLI run.
type RunRequest struct {
	ExecutionID    string
	SessionID      string
	Resume         bool
	WorkDir        string
	SystemPrompt   string
	Prompt         string
	Model          ModelConfig
	ExtraEnv       []string
	Timeout        time.Duration
	PermissionMode PermissionMode    // controls approval behavior
	ApprovalHandler ApprovalHandler  // optional: injected by runtime for on-request/auto modes
}

// Process is a running external CLI process handle.
type Process interface {
	PID() int
	Stop() error
}

// RunHandle is returned after an engine process starts.
type RunHandle struct {
	Process     Process
	Events      <-chan events.Event
	StdinWriter io.Writer // used to write approval decisions back to the CLI process
}

// Engine executes prompts through an external AI CLI.
type Engine interface {
	Prepare(ctx context.Context, req PrepareRequest) error
	RegisterMCP(ctx context.Context, cfg MCPServerConfig) error
	GetSkillDir() string
	Run(ctx context.Context, req RunRequest) (*RunHandle, error)
}
