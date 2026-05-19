package agent

import (
	"context"
	"time"

	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
)

// Runner 是数字员工单次运行的执行边界。
type Runner interface {
	// Run 执行一次数字员工运行，请求中可携带 EventSink 用于流式事件输出。
	Run(ctx context.Context, req *RequestContext) (*RunResult, error)
}

// InputType describes the primary shape of the run input.
type InputType string

const (
	InputTypeMessage         InputType = "message"
	InputTypeTaskInstruction InputType = "task_instruction"
	InputTypeEvent           InputType = "event"
)

// RequestContext is the normalized execution snapshot consumed by runtime.
type RequestContext struct {
	// RunID 是本次 runtime 执行的唯一标识。
	RunID string `json:"run_id"`

	// TraceID 是跨模块链路追踪标识，用于串联消息、任务、工具调用和日志。
	TraceID string `json:"trace_id,omitempty"`

	// TaskID 是业务任务标识，仅 task 场景需要。
	TaskID string `json:"task_id,omitempty"`

	// Assistant 是被调用数字员工在本次运行中的快照。
	Assistant AssistantContext `json:"assistant"`

	// Actor 是触发本次运行的人、系统或上游代理身份。
	Actor ActorContext `json:"actor"`

	// Conversation 是对话上下文快照，用于恢复最近消息和会话状态。
	Conversation ConversationContext `json:"conversation,omitempty"`

	// Input 是本次运行的标准化输入内容。
	Input InputContext `json:"input"`

	// Runtime 是 runtime 执行参数，例如最大 agent 步数。
	Runtime RuntimeOptions `json:"runtime,omitempty"`

	// Model 是模型选择和采样参数覆盖项。
	Model ModelOptions `json:"model,omitempty"`

	// Capability 描述本次运行允许使用的能力范围。
	Capability CapabilityContext `json:"capability,omitempty"`

	// Policy 描述本次运行需要遵守的策略约束。
	Policy PolicyContext `json:"policy,omitempty"`

	// Metadata 存放调用方透传的扩展信息
	Metadata map[string]any `json:"metadata,omitempty"`

	// SystemPrompt 是生命周期层构建后的最终系统提示词，不参与 JSON 序列化。
	SystemPrompt string `json:"-"`

	// EventSink 接收运行过程中的流式事件，不参与 JSON 序列化。
	EventSink events.Sink `json:"-"`
}

// AssistantContext is the assistant snapshot used for one run.
type AssistantContext struct {
	// ID 是数字员工或 assistant 的唯一标识。
	ID string `json:"id"`

	// Name 是数字员工展示名称。
	Name string `json:"name,omitempty"`

	// Role 是数字员工在本次运行中的角色描述。
	Role string `json:"role,omitempty"`

	// SystemPrompt 是本次运行追加到默认系统提示词中的 assistant 专属提示。
	SystemPrompt string `json:"system_prompt,omitempty"`

	// Skills 是本次运行显式启用或提示的技能标识列表。
	Skills []string `json:"skills,omitempty"`

	// Tools 是本次运行显式启用或提示的工具标识列表。
	Tools []string `json:"tools,omitempty"`
}

// ActorContext describes the human or system actor that initiated the run.
type ActorContext struct {
	// UserID 是 Leros 内部用户或调用主体标识。
	UserID string `json:"user_id"`

	// DisplayName 是调用主体的展示名称。
	DisplayName string `json:"display_name,omitempty"`

	// Channel 是调用来源渠道，例如 web、github、feishu、wework。
	Channel string `json:"channel,omitempty"`

	// ExternalID 是渠道侧用户标识。
	ExternalID string `json:"external_id,omitempty"`

	// AccountID 是调用主体指定或解析出的授权账号标识。
	AccountID string `json:"account_id,omitempty"`
}

// ConversationContext carries recent conversation state when available.
type ConversationContext struct {
	// ID 是对话或会话标识。
	ID string `json:"id,omitempty"`

	// Messages 是传入 runtime 的最近对话消息快照。
	Messages []InputMessage `json:"messages,omitempty"`
}

// InputContext is the normalized input passed to the agent.
type InputContext struct {
	// Type 表示输入形态，例如普通消息、任务指令或外部事件。
	Type InputType `json:"type"`

	// Text 是本次运行的主文本输入。
	Text string `json:"text,omitempty"`

	// Messages 是多条输入消息，适合直接携带对话片段。
	Messages []InputMessage `json:"messages,omitempty"`

	// Attachments 是随输入携带的文件、图片等附件。
	Attachments []Attachment `json:"attachments,omitempty"`
}

// InputMessage is a simple role/content message snapshot.
type InputMessage struct {
	// Role 是消息角色，例如 user、assistant、system。
	Role string `json:"role"`

	// Content 是消息文本内容。
	Content string `json:"content"`
}

// Attachment describes an input attachment made available to the run.
type Attachment struct {
	// ID 是附件标识。
	ID string `json:"id,omitempty"`

	// Name 是附件文件名或展示名。
	Name string `json:"name,omitempty"`

	// MimeType 是附件 MIME 类型。
	MimeType string `json:"mime_type,omitempty"`

	// URL 是附件可访问地址。
	URL string `json:"url,omitempty"`
}

// RuntimeOptions controls runtime execution behavior.
type RuntimeOptions struct {
	// Kind 是本次运行选择的 runtime，例如 leros、codex、claude。
	Kind string `json:"kind,omitempty"`

	// WorkDir ： runtime 执行时使用的工作目录
	WorkDir string `json:"work_dir,omitempty"`

	// MaxStep 是 agent 单次运行允许的最大推理/工具循环步数。
	MaxStep int `json:"max_step,omitempty"`
}

// ModelOptions lets callers override model behavior when supported.
type ModelOptions struct {
	// ID 是数据库中持久化的模型配置ID；为空时运行时回退到组织默认模型。
	ID uint `json:"id,omitempty"`

	// Provider 是模型供应商标识。
	Provider string `json:"provider,omitempty"`

	// Model 是具体模型名称。
	Model string `json:"model,omitempty"`

	// APIKey 是运行期解析出的模型凭证，不参与请求序列化。
	APIKey string `json:"-"`

	// BaseURL 是运行期解析出的模型服务地址。
	BaseURL string `json:"base_url,omitempty"`

	// Temperature 是模型采样温度。
	Temperature float64 `json:"temperature,omitempty"`
}

// CapabilityContext describes allowed capabilities for one run.
type CapabilityContext struct {
	// AllowedTools 是本次运行允许暴露给 agent 的工具名称列表。
	AllowedTools []string `json:"allowed_tools,omitempty"`
}

// PolicyContext carries policy knobs for one run.
type PolicyContext struct {
	// RequireApproval 表示涉及外部副作用的操作是否需要审批。
	RequireApproval bool `json:"require_approval,omitempty"`
}

// RunStatus is the final status returned from Run.
type RunStatus string

const (
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

// RunResult is the final result of one agent run.
type RunResult struct {
	// RunID 是本次 runtime 执行的唯一标识。
	RunID string `json:"run_id"`

	// TraceID 是跨模块链路追踪标识。
	TraceID string `json:"trace_id,omitempty"`

	// Status 是本次运行最终状态。
	Status RunStatus `json:"status"`

	// Message 是最终 assistant 文本结果。
	Message string `json:"message,omitempty"`

	// Error 是失败或取消时的错误摘要。
	Error string `json:"error,omitempty"`

	// Usage 是模型 token 用量统计。
	Usage *events.UsagePayload `json:"usage,omitempty"`

	// ToolCalls 是本次运行的工具调用摘要。
	ToolCalls []ToolCallRecord `json:"tool_calls,omitempty"`

	// Metadata 存放运行结果的扩展信息。
	Metadata map[string]any `json:"metadata,omitempty"`

	// StartedAt 是 runtime 开始执行时间。
	StartedAt time.Time `json:"started_at"`

	// CompletedAt 是 runtime 完成、失败或取消时间。
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

// ToolCallRecord is a compact final tool call summary.
type ToolCallRecord struct {
	// CallID 是模型侧工具调用 ID。
	CallID string `json:"call_id,omitempty"`

	// Name 是工具名称。
	Name string `json:"name,omitempty"`

	// Result 是工具执行结果摘要。
	Result map[string]any `json:"result,omitempty"`

	// Error 是工具执行失败时的错误摘要。
	Error string `json:"error,omitempty"`
}
