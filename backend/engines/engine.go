// Package engines 定义外部 Agent CLI 引擎的执行边界。
package engines

import (
	"context"
	"time"

	"github.com/insmtx/Leros/backend/internal/runtime/events"
)

const (
	// EngineClaude 是 Claude Code 的注册名称。
	EngineClaude = "claude"
	// EngineCodex 是 Codex CLI 的注册名称。
	EngineCodex = "codex"
)

const (
	// EventProviderSessionStarted 表示提供者创建或暴露了一个原生会话 ID。
	EventProviderSessionStarted events.EventType = "provider_session.started"
)

// PermissionMode 控制引擎是否/如何请求用户审批工具调用。
type PermissionMode string

const (
	// PermissionModeBypass 跳过所有审批请求。
	PermissionModeBypass PermissionMode = "bypass"
	// PermissionModeOnRequest 将每个工具调用审批请求转发给用户。
	PermissionModeOnRequest PermissionMode = "on-request"
	// PermissionModeAuto 自动批准安全操作，将有风险的操作转发给用户。
	PermissionModeAuto PermissionMode = "auto"
)

// ApprovalRequest 描述需要用户审批的工具调用。
type ApprovalRequest struct {
	RequestID   string
	ToolCallID  string
	ToolName    string
	Arguments   map[string]any
	Description string
	Engine      string // "claude" | "codex"
}

// 统一审批决策值，前端 API 和引擎 Responder 共用。
const (
	ApprovalActionApprove = "approve"
	ApprovalActionDeny    = "deny"
	ApprovalActionAlways  = "always"
)

// ApprovalDecision 包含用户对审批请求的决策。
type ApprovalDecision struct {
	RequestID string
	Action    string // "approve" | "deny" | "always"
	Reason    string
}

// ApprovalHandler 处理来自引擎的审批请求。
// 实现必须是线程安全的。
type ApprovalHandler interface {
	// RequestApproval 提交审批请求并阻塞直到做出决策。
	// 返回决策，如果请求被取消/超时则返回错误。
	RequestApproval(ctx context.Context, req *ApprovalRequest) (*ApprovalDecision, error)
}

// ApprovalResponder 将审批决策写回引擎的标准输入。
// 每种引擎提供自己的实现，知道如何格式化引擎特定的协议。
type ApprovalResponder interface {
	WriteDecision(requestID string, action string) error
}

// PrepareRequest 包含引擎特定的工作区准备输入。
type PrepareRequest struct {
	WorkDir string
}

// ModelConfig 包含注入到 CLI 进程中的模型设置。
type ModelConfig struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
}

// RunRequest 包含执行一次外部 CLI 运行所需的所有输入。
type RunRequest struct {
	ExecutionID     string
	SessionID       string
	Resume          bool
	WorkDir         string
	SystemPrompt    string
	Prompt          string
	Model           ModelConfig
	ExtraEnv        []string
	Timeout         time.Duration
	PermissionMode  PermissionMode   // 控制审批行为
	ApprovalHandler ApprovalHandler  // 可选：由运行时注入，用于 on-request/auto 模式
}

// Process 是正在运行的外部 CLI 进程句柄。
type Process interface {
	PID() int
	Stop() error
}

// RunHandle 在引擎进程启动后返回。
type RunHandle struct {
	Process   Process
	Events    <-chan events.Event
	Responder ApprovalResponder // 引擎特定的审批响应写入器
}

// Engine 通过外部 AI CLI 执行提示。
type Engine interface {
	Prepare(ctx context.Context, req PrepareRequest) error
	RegisterMCP(ctx context.Context, cfg MCPServerConfig) error
	GetSkillDir() string
	Run(ctx context.Context, req RunRequest) (*RunHandle, error)
}
