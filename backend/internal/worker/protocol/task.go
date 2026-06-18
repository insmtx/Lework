package protocol

// TaskType represents the type of task requested for Worker execution.
type TaskType string

const (
	// TaskTypeAgentRun requests the Worker to execute an Agent run.
	TaskTypeAgentRun TaskType = "agent.run"
)

// InputType represents the primary form of task input.
type InputType string

const (
	// InputTypeMessage represents normal conversation message input.
	InputTypeMessage InputType = "message"
	// InputTypeTaskInstruction represents direct task instruction input.
	InputTypeTaskInstruction InputType = "task_instruction"
)

// MessageRole represents the producer role in conversations or stream messages.
type MessageRole string

const (
	// MessageRoleUser represents human user or external user messages.
	MessageRoleUser MessageRole = "user"
	// MessageRoleAssistant represents assistant messages.
	MessageRoleAssistant MessageRole = "assistant"
	// MessageRoleSystem represents system messages.
	MessageRoleSystem MessageRole = "system"
	// MessageRoleTool represents tool result messages.
	MessageRoleTool MessageRole = "tool"
)

// WorkerTaskMessage is the task message protocol from Server to Worker.
type WorkerTaskMessage = Envelope[WorkerTaskBody]

// WorkerTaskBody is the payload of task messages from Server to Worker.
type WorkerTaskBody struct {
	TaskType TaskType `json:"task_type"`

	Actor     ActorContext     `json:"actor"`
	Execution ExecutionTarget  `json:"execution"`
	Workspace WorkspaceOptions `json:"workspace,omitempty"`
	Input     TaskInput        `json:"input"`

	Model   ModelOptions   `json:"model,omitempty"`
	Runtime RuntimeOptions `json:"runtime,omitempty"`
	Policy  TaskPolicy     `json:"policy,omitempty"`
}

// ActorContext describes the identity of the task initiator.
type ActorContext struct {
	UserID      string `json:"user_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Channel     string `json:"channel,omitempty"`
	ExternalID  string `json:"external_id,omitempty"`
	AccountID   string `json:"account_id,omitempty"`
}

// ExecutionTarget describes the execution target and capability scope for this task.
type ExecutionTarget struct {
	AssistantID string   `json:"assistant_id,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	Tools       []string `json:"tools,omitempty"`
}

// WorkspaceOptions identifies the isolated project workspace for a task run.
type WorkspaceOptions struct {
	ProjectID string `json:"project_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
}

// TaskInput is the standardized task input consumed by Worker Runtime.
type TaskInput struct {
	Type        InputType      `json:"type"`
	Messages    []ChatMessage  `json:"messages,omitempty"`
	Attachments []Attachment   `json:"attachments,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// ChatMessage is a compact conversation message snapshot.
type ChatMessage struct {
	ID      string      `json:"id,omitempty"`
	Role    MessageRole `json:"role"`
	Content string      `json:"content"`
}

// Attachment describes an attachment available in task input.
type Attachment struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	URL      string `json:"url,omitempty"`
}

// ModelOptions carries the LLM call configuration for one worker task.
type ModelOptions struct {
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	BaseURLHasV1 bool   `json:"base_url_has_v1,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
}

// RuntimeOptions controls the execution parameters for Worker Runtime.
type RuntimeOptions struct {
	Kind    string `json:"kind,omitempty"`
	WorkDir string `json:"work_dir,omitempty"`
	MaxStep int    `json:"max_step,omitempty"`
}

// TaskPolicy carries the policy switches that Worker tasks must follow.
type TaskPolicy struct {
	RequireApproval bool   `json:"require_approval,omitempty"`
	PermissionMode  string `json:"permission_mode,omitempty"` // "bypass" | "on-request" | "auto"; empty defaults to bypass
}
