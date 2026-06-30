package messaging

import (
	"encoding/json"
	"fmt"
	"time"
)

// CommandType 表示 server 发给 worker 的命令类型。
type CommandType string

const (
	// CommandTypeRun 请求 Worker 执行 Agent run。
	CommandTypeRun CommandType = "agent.run"
	// CommandTypeCancel 请求 Worker 取消正在运行的 Agent run。
	CommandTypeCancel CommandType = "run.cancel"
	// CommandTypeApprovalResolve 发送审批决策给 Worker。
	CommandTypeApprovalResolve CommandType = "approval.resolve"
	// CommandTypeQuestionAnswer 发送问题答案给 Worker。
	CommandTypeQuestionAnswer CommandType = "question.answer"
	// CommandTypeSkill 统一 skill 管理命令。
	CommandTypeSkill CommandType = "skill.manage"
)

// Lane 表示命令分发到哪个 lane subject。
type Lane string

const (
	LaneRun         Lane = "cmd.run"
	LaneControl     Lane = "cmd.control"
	LaneInteraction Lane = "cmd.interaction"
	LaneSkill       Lane = "cmd.skill"
)

// CommandLane 根据命令类型返回对应的 lane。
func CommandLane(cmdType CommandType) Lane {
	switch cmdType {
	case CommandTypeRun:
		return LaneRun
	case CommandTypeCancel:
		return LaneControl
	case CommandTypeApprovalResolve, CommandTypeQuestionAnswer:
		return LaneInteraction
	case CommandTypeSkill:
		return LaneSkill
	default:
		return LaneRun
	}
}

// WorkerCommand 是 Server -> Worker 的统一命令消息。
type WorkerCommand = Envelope[WorkerCommandBody]

// WorkerCommandBody 是所有 worker 命令的联合 body。
//
// 根据 CommandType 不同，Payload 携带不同类型的数据：
//   - agent.run:         RunCommandPayload
//   - run.cancel:        CancelRunCommandPayload
//   - approval.resolve:  ApprovalResolveCommandPayload
//   - question.answer:   QuestionAnswerCommandPayload
//   - skill.manage:      SkillCommandPayload
type WorkerCommandBody struct {
	CommandType CommandType     `json:"command_type"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	// ReplyTo 是 server Request() 注入的 NATS inbox，worker 通过 core NATS 直接回复。
	ReplyTo string `json:"reply_to,omitempty"`
}

// DecodeCommandPayload 从 WorkerCommandBody.Payload 解码为指定类型。
func DecodeCommandPayload[T any](body *WorkerCommandBody) (T, error) {
	var zero T
	if body == nil || len(body.Payload) == 0 {
		return zero, fmt.Errorf("command payload is empty")
	}
	if err := json.Unmarshal(body.Payload, &zero); err != nil {
		return zero, fmt.Errorf("decode command payload: %w", err)
	}
	return zero, nil
}

// ---- Payload Types ----

// RunCommandPayload 是 agent.run 命令的 payload。
type RunCommandPayload struct {
	TaskType      TaskType `json:"task_type"`
	ExecutionMode string   `json:"execution_mode,omitempty"`

	Actor     ActorContext     `json:"actor"`
	Execution ExecutionTarget  `json:"execution"`
	Workspace WorkspaceOptions `json:"workspace,omitempty"`
	Input     TaskInput        `json:"input"`

	Model   ModelOptions   `json:"model,omitempty"`
	Runtime RuntimeOptions `json:"runtime,omitempty"`
	Policy  TaskPolicy     `json:"policy,omitempty"`
}

// CancelRunCommandPayload 是 run.cancel 命令的 payload。
type CancelRunCommandPayload struct {
	RunID  string `json:"run_id"`
	Reason string `json:"reason,omitempty"`
}

// ApprovalResolveCommandPayload 是 approval.resolve 命令的 payload。
type ApprovalResolveCommandPayload struct {
	Action string `json:"action"` // "approve" | "deny" | "always"
	Reason string `json:"reason,omitempty"`
}

// QuestionAnswerCommandPayload 是 question.answer 命令的 payload。
type QuestionAnswerCommandPayload struct {
	Answers [][]string `json:"answers"`
}

// SkillCommandPayload 是 skill.manage 命令的 payload。
type SkillCommandPayload struct {
	Action  string `json:"action"`             // "install" | "list" | "uninstall" | "detail" | "import"
	Source  string `json:"source,omitempty"`   // "Leros" | "github" | "skills-sh" | "url"
	SkillID string `json:"skill_id,omitempty"` // install identifier
	Version string `json:"version,omitempty"`  // optional version for install
	Name    string `json:"name,omitempty"`     // for uninstall / detail: the skill name
	// DownloadURL is the URL from which the worker downloads the skill file during "import".
	DownloadURL string `json:"download_url,omitempty"`
}

// ---- Command Builders ----

// NewRunCommand 构造一个 agent.run WorkerCommand。
func NewRunCommand(
	envID string,
	route RouteContext,
	trace TraceContext,
	payload RunCommandPayload,
	metadata *RunCommandMetadata,
) WorkerCommand {
	raw, _ := json.Marshal(payload)
	metadataRaw, _ := json.Marshal(metadata)
	if metadata == nil {
		metadataRaw = nil
	}
	return WorkerCommand{
		ID:        envID,
		Type:      MessageTypeWorkerCommand,
		CreatedAt: time.Now().UTC(),
		Trace:     trace,
		Route:     route,
		Body: WorkerCommandBody{
			CommandType: CommandTypeRun,
			Payload:     raw,
		},
		Metadata: metadataRaw,
	}
}

// RunCommandMetadata contains typed optional metadata for agent.run commands.
type RunCommandMetadata struct {
	SessionID   string `json:"session_id,omitempty"`
	MessageType string `json:"message_type,omitempty"`
	Sequence    int64  `json:"sequence,omitempty"`
}

// NewCancelRunCommand 构造一个 run.cancel WorkerCommand。
func NewCancelRunCommand(envID string, route RouteContext, payload CancelRunCommandPayload, runID string) WorkerCommand {
	raw, _ := json.Marshal(payload)
	return WorkerCommand{
		ID:        envID,
		Type:      MessageTypeWorkerCommand,
		CreatedAt: time.Now().UTC(),
		Trace: TraceContext{
			RunID: runID,
		},
		Route: route,
		Body: WorkerCommandBody{
			CommandType: CommandTypeCancel,
			Payload:     raw,
		},
	}
}

// NewApprovalResolveCommand 构造一个 approval.resolve WorkerCommand。
func NewApprovalResolveCommand(envID string, route RouteContext, payload ApprovalResolveCommandPayload, requestID string) WorkerCommand {
	raw, _ := json.Marshal(payload)
	return WorkerCommand{
		ID:        envID,
		Type:      MessageTypeWorkerCommand,
		CreatedAt: time.Now().UTC(),
		Trace: TraceContext{
			RequestID: requestID,
		},
		Route: route,
		Body: WorkerCommandBody{
			CommandType: CommandTypeApprovalResolve,
			Payload:     raw,
		},
	}
}

// NewQuestionAnswerCommand 构造一个 question.answer WorkerCommand。
func NewQuestionAnswerCommand(envID string, route RouteContext, payload QuestionAnswerCommandPayload, requestID string) WorkerCommand {
	raw, _ := json.Marshal(payload)
	return WorkerCommand{
		ID:        envID,
		Type:      MessageTypeWorkerCommand,
		CreatedAt: time.Now().UTC(),
		Trace: TraceContext{
			RequestID: requestID,
		},
		Route: route,
		Body: WorkerCommandBody{
			CommandType: CommandTypeQuestionAnswer,
			Payload:     raw,
		},
	}
}

// NewSkillCommand 构造一个 skill.manage WorkerCommand。
func NewSkillCommand(envID string, route RouteContext, payload SkillCommandPayload, replyTo string) WorkerCommand {
	raw, _ := json.Marshal(payload)
	return WorkerCommand{
		ID:        envID,
		Type:      MessageTypeWorkerCommand,
		CreatedAt: time.Now().UTC(),
		Route:     route,
		Body: WorkerCommandBody{
			CommandType: CommandTypeSkill,
			Payload:     raw,
			ReplyTo:     replyTo,
		},
	}
}

// ---- Retained types (shared across payloads) ----

// WorkerCommandResult 是 Worker -> Server 的同步响应（用于 skill request/reply）。
type WorkerCommandResult struct {
	Success bool   `json:"success"`
	Action  string `json:"action"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// SkillListItem 表示已安装的 skill。
type SkillListItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Source      string `json:"source"`
	Trust       string `json:"trust"`
}

// SkillDetailData 表示已安装 skill 的完整详情，包括 SKILL.md 内容。
type SkillDetailData struct {
	SkillID     string   `json:"skill_id,omitempty"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Source      string   `json:"source"`
	Trust       string   `json:"trust"`
	Version     string   `json:"version"`
	SkillMD     string   `json:"skill_md"`
	Tags        []string `json:"tags"`
	Files       []string `json:"files"`
}

// ---- agent.run task types ----

type TaskType string

const (
	TaskTypeAgentRun TaskType = "agent.run"
)

type InputType string

const (
	InputTypeMessage         InputType = "message"
	InputTypeTaskInstruction InputType = "task_instruction"
)

type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
	MessageRoleTool      MessageRole = "tool"
)

type ActorContext struct {
	UserID      string `json:"user_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Channel     string `json:"channel,omitempty"`
	ExternalID  string `json:"external_id,omitempty"`
	AccountID   string `json:"account_id,omitempty"`
}

type ExecutionTarget struct {
	AssistantID string   `json:"assistant_id,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	Tools       []string `json:"tools,omitempty"`
}

type WorkspaceOptions struct {
	ProjectID string `json:"project_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
}

type TaskInput struct {
	Type        InputType     `json:"type"`
	Messages    []ChatMessage `json:"messages,omitempty"`
	Attachments []Attachment  `json:"attachments,omitempty"`
}

type ChatMessage struct {
	ID      string      `json:"id,omitempty"`
	Role    MessageRole `json:"role"`
	Content string      `json:"content"`
}

type Attachment struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	URL      string `json:"url,omitempty"`
}

type ModelOptions struct {
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	BaseURLHasV1 bool   `json:"base_url_has_v1,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
}

type RuntimeOptions struct {
	Kind    string `json:"kind,omitempty"`
	WorkDir string `json:"work_dir,omitempty"`
	MaxStep int    `json:"max_step,omitempty"`
}

type TaskPolicy struct {
	RequireApproval bool   `json:"require_approval,omitempty"`
	PermissionMode  string `json:"permission_mode,omitempty"`
}
