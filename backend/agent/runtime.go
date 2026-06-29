package agent

import "context"

const (
	// RuntimeKindLeros is the built-in Leros agent runtime.
	RuntimeKindLeros = "leros"
)

// Message is a business-neutral conversation message supplied to a Runtime.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ModelConfig is the fully resolved model configuration for one execution.
type ModelConfig struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
}

// ExecutionPolicy controls generic runtime behavior.
type ExecutionPolicy struct {
	PermissionMode string
	MaxSteps       int
	AllowedTools   []string
}

// FilesystemContext contains the already prepared runtime directories.
type FilesystemContext struct {
	WorkDir string
	RepoDir string
	TaskDir string
}

// ExecutionRequest is a fully prepared, business-neutral Runtime input.
type ExecutionRequest struct {
	ExecutionID string
	TraceID     string
	Runtime     string
	SessionKey  string
	InstanceKey string

	SystemPrompt string
	Prompt       string
	Messages     []Message
	Model        ModelConfig
	Tools        []Tool
	Policy       ExecutionPolicy
	Filesystem   FilesystemContext
}

// ExecutionResult is the low-level result returned by a Runtime before business finalization.
type ExecutionResult struct {
	Message                string
	Usage                  *Usage
	ToolCalls              []ToolCallRecord
	ProviderConversationID string
}

// Runtime executes a fully prepared request against a specific provider.
//
// Runtime MUST NOT:
//   - Emit run.started, run.completed, run.failed, or run.cancelled events.
//   - Mutate ExecutionRequest.
//   - Access NATS, messaging, or Session persistence.
type Runtime interface {
	Name() string
	Execute(ctx context.Context, request ExecutionRequest, observer Observer) (ExecutionResult, error)
}

// RuntimeResolver maps a runtime kind string to a Runtime implementation.
type RuntimeResolver interface {
	Resolve(kind string) (Runtime, error)
}
