package cli

import (
	"context"

	"github.com/insmtx/Leros/backend/agent"
	runtimeprocess "github.com/insmtx/Leros/backend/agent/runtime/internal/process"
)

// InvocationRequest contains the provider-specific process input derived from
// one immutable agent execution request.
type InvocationRequest struct {
	ExecutionID     string
	ExecutionMode   agent.ExecutionMode
	SessionID       string
	Resume          bool
	WorkDir         string
	TaskDir         string
	SystemPrompt    string
	Prompt          string
	Messages        []agent.Message
	Tools           []agent.Tool
	AllowedTools    []string
	TraceID         string
	SessionKey      string
	Model           agent.ModelConfig
	ExtraEnv        []string
	PermissionMode  string
	ApprovalHandler agent.InteractionHandler
	MCPServers      []agent.MCPServerConfig
}

// Invocation is a running provider process and its normalized activity stream.
type Invocation struct {
	Process   runtimeprocess.Process
	Events    <-chan agent.NodeEvent
	Result    <-chan InvocationResult
	Responder agent.ApprovalResponder
	Questions agent.QuestionResponder
}

// InvocationResult is the terminal provider result for one CLI invocation.
// Process lifecycle is intentionally separate from observable NodeEvents.
type InvocationResult struct {
	Message           string
	Usage             *agent.Usage
	ProviderSessionID string
	Err               error
}

// Invoker starts a single external CLI provider process.
//
// It is deliberately narrower than agent.Runtime: it does not resolve runtime
// names, own provider-session persistence, or publish execution lifecycle.
type Invoker interface {
	Prepare(ctx context.Context, workDir string) error
	Invoke(ctx context.Context, request InvocationRequest) (*Invocation, error)
}
