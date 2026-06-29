package externalcli

import (
	"context"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/provider"
)

// InvocationRequest contains the provider-specific process input derived from
// one immutable agent execution request.
type InvocationRequest struct {
	ExecutionID     string
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
	PermissionMode  provider.PermissionMode
	ApprovalHandler provider.ApprovalHandler
	MCPServers      []provider.MCPServerConfig
}

// Invocation is a running provider process and its normalized activity stream.
type Invocation struct {
	Process   provider.Process
	Events    <-chan agent.Event
	Responder provider.ApprovalResponder
	Questions provider.QuestionResponder
}

// Invoker starts a single external CLI provider process.
//
// It is deliberately narrower than agent.Runtime: it does not resolve runtime
// names, own provider-session persistence, or publish execution lifecycle.
type Invoker interface {
	Prepare(ctx context.Context, workDir string) error
	Invoke(ctx context.Context, request InvocationRequest) (*Invocation, error)
}

// DriverOptions configures shared provider-session and interaction facilities.
type DriverOptions struct {
	SessionStore    ProviderSessionStore
	ApprovalHandler provider.ApprovalHandler
	QuestionHandler provider.QuestionHandler
	MCPServers      []provider.MCPServerConfig
}
