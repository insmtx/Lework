package agent

import (
	"context"
)

const (
	// RuntimeKindLeros is the built-in Leros agent runtime.
	RuntimeKindLeros = "leros"
	// RuntimeKindClaude is the Claude Code runtime.
	RuntimeKindClaude = "claude"
	// RuntimeKindCodex is the Codex CLI runtime.
	RuntimeKindCodex = "codex"
	// RuntimeKindOpenCode is the OpenCode runtime.
	RuntimeKindOpenCode = "opencode"
	// RunSkillsDirEnvVar exposes the task-private Skill root to runtime processes.
	RunSkillsDirEnvVar = "LEROS_RUN_SKILLS_DIR"
)

// ExecutionMode controls runtime behavior independently from any host business model.
type ExecutionMode string

const (
	// ExecutionModeDefault keeps the runtime's normal execution behavior.
	ExecutionModeDefault ExecutionMode = "default"
	// ExecutionModePlan requests planning behavior when the runtime supports it.
	ExecutionModePlan ExecutionMode = "plan"
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
	// Vision 表示该模型是否声明支持图片（多模态）输入。
	Vision bool
	// TopP/FrequencyPenalty/PresencePenalty 采样参数，仅 opencode runtime 消费。
	TopP             *float64
	FrequencyPenalty *float64
	PresencePenalty  *float64
	// ContextLimit/OutputLimit 模型上下文与单次输出上限；0 表示未设置，走默认。
	ContextLimit int
	OutputLimit  int
}

// Attachment is a multimodal file (e.g. an image) supplied with one execution.
// Data holds the raw bytes when the file is inlined (e.g. embedded as a base64
// data URL); runtimes decide how to attach them. When Data is empty, the file may
// still be materialized on disk under Filesystem.UploadRelDir, which the runtime
// can combine with Name to locate it. It is intended for vision/multimodal-capable
// inputs only — plain text attachments should stay in the prompt.
type Attachment struct {
	MIME string
	// Name is the display filename (e.g. "头像.jpeg"); it is not a path.
	Name string
	Data []byte
}

// ExecutionPolicy controls generic runtime behavior.
type ExecutionPolicy struct {
	PermissionMode string
	AllowedTools   []string
}

// FilesystemContext contains the already prepared runtime directories.
type FilesystemContext struct {
	WorkDir  string
	RepoDir  string
	TaskDir  string
	SkillDir string
	// UploadRelDir is the workspace-relative subdirectory (relative to RepoDir)
	// where attachments are materialized on disk, e.g. "uploads". Runtimes may
	// combine it with an Attachment.Name to locate a non-inlined attachment.
	// It is empty when no uploads directory is provisioned.
	UploadRelDir string
}

// ProviderSession carries pre-resolved provider session information for resume.
type ProviderSession struct {
	ID     string
	Resume bool
}

// ExecutionRequest is a fully prepared, business-neutral Runtime input.
type ExecutionRequest struct {
	ExecutionID string
	TraceID     string
	Runtime     string
	SessionKey  string
	InstanceKey string
	Mode        ExecutionMode

	SystemPrompt    string
	Prompt          string
	Messages        []Message
	Attachments     []Attachment
	Model           ModelConfig
	Tools           []Tool
	MCPServers      []MCPServerConfig
	ExtraEnv        []string
	Policy          ExecutionPolicy
	Filesystem      FilesystemContext
	ProviderSession ProviderSession
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
	Execute(ctx context.Context, request ExecutionRequest, observer NodeObserver) (ExecutionResult, error)
}

// RuntimeResolver maps a runtime kind string to a Runtime implementation.
type RuntimeResolver interface {
	Resolve(kind string) (Runtime, error)
}
