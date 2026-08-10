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
	// CommandTypeProjectFileRestore 请求 Worker 恢复项目文件历史版本。
	CommandTypeProjectFileRestore CommandType = "project.file.restore"
)

// Lane 表示命令分发到哪个 lane subject。
type Lane string

const (
	LaneRun         Lane = "cmd.run"
	LaneControl     Lane = "cmd.control"
	LaneInteraction Lane = "cmd.interaction"
	LaneFile        Lane = "cmd.file"
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
	case CommandTypeProjectFileRestore:
		return LaneFile
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
//   - project.file.restore: ProjectFileRestoreCommandPayload
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
	Project   ProjectContext   `json:"project,omitempty"`
	Input     TaskInput        `json:"input"`

	Model   ModelOptions     `json:"model,omitempty"`
	Runtime RuntimeOptions   `json:"runtime,omitempty"`
	Policy  TaskPolicy       `json:"policy,omitempty"`
	Plugins []PluginSnapshot `json:"plugins,omitempty"`

	// 业务主键 ID，用于 llm_history 等调用记录关联。
	// 以下均为对应表的自增主键（int），区别于其它字段中的 public_id（string）。
	//
	//   ProjectID   leros_project.id          -> 区别于 Workspace.ProjectID（project public_id）
	//   SessionID   leros_session.id          -> 区别于 RouteContext.SessionID（session public_id）
	//   MessageID   leros_session_message.id  -> 当前触发 run 的消息主键
	//   AssistantID       leros_digital_assistant.id          -> 区别于 Execution.AssistantPublicID（assistant public_id）
	//   AssistantPublicID leros_digital_assistant.public_id    -> 用于 worker 侧展示和对外追溯
	//   Uin               leros_user.id                        -> 区别于 ActorContext.UserID（fmt.Sprintf("%d", uin)）
	ProjectID         uint   `json:"project_id"`
	SessionID         uint   `json:"session_id"`
	MessageID         uint   `json:"message_id"`
	AssistantID       uint   `json:"assistant_id"`
	AssistantPublicID string `json:"assistant_public_id,omitempty"`
	Uin               uint   `json:"uin"`

	// NotAfter Worker 最晚允许开始时间（RFC3339，UTC）。超过该时间 Worker 应拒绝执行。
	// 需要持久化进 worker inbox，崩溃恢复时仍能生效。
	NotAfter string `json:"not_after,omitempty"`
}

// PluginSnapshot is the immutable plugin revision selected when a run is published.
// Definition is the immutable plugin configuration selected for this run.
type PluginSnapshot struct {
	PluginID   string          `json:"plugin_id"`
	Code       string          `json:"code"`
	Kind       string          `json:"kind"`
	Revision   int             `json:"revision"`
	Definition json.RawMessage `json:"definition"`
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

// ProjectFileRestoreCommandPayload 是 project.file.restore 命令的 payload。
type ProjectFileRestoreCommandPayload struct {
	ProjectPublicID string `json:"project_public_id"`
	RelativePath    string `json:"relative_path"`
	Branch          string `json:"branch,omitempty"`
	DownloadURL     string `json:"download_url"`
	AuthorName      string `json:"author_name,omitempty"`
	AuthorEmail     string `json:"author_email,omitempty"`
}

// ProjectFileRestoreResult 是 Worker 完成项目文件恢复后的同步响应。
type ProjectFileRestoreResult struct {
	Success      bool   `json:"success"`
	RelativePath string `json:"relative_path,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	Error        string `json:"error,omitempty"`
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

// NewProjectFileRestoreCommand 构造 project.file.restore WorkerCommand。
func NewProjectFileRestoreCommand(envID string, route RouteContext, payload ProjectFileRestoreCommandPayload) WorkerCommand {
	raw, _ := json.Marshal(payload)
	return WorkerCommand{
		ID:        envID,
		Type:      MessageTypeWorkerCommand,
		CreatedAt: time.Now().UTC(),
		Route:     route,
		Body: WorkerCommandBody{
			CommandType: CommandTypeProjectFileRestore,
			Payload:     raw,
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
	// AssistantID 是 leros_digital_assistant.id，自增主键，用于 llm_history 关联。
	AssistantID uint `json:"assistant_id,omitempty"`
	// AssistantPublicID 是 leros_digital_assistant.public_id，用于 worker 侧日志展示。
	AssistantPublicID string   `json:"assistant_public_id,omitempty"`
	AssistantName     string   `json:"assistant_name,omitempty"`
	AssistantDesc     string   `json:"assistant_desc,omitempty"`
	SystemPrompt      string   `json:"system_prompt,omitempty"`
	Skills            []string `json:"skills,omitempty"`
	Tools             []string `json:"tools,omitempty"`
}

type WorkspaceOptions struct {
	ProjectID string `json:"project_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
}

// ProjectContext carries project business context to the worker.
type ProjectContext struct {
	Name        string        `json:"name,omitempty"`
	Description string        `json:"description,omitempty"`
	Objective   string        `json:"objective,omitempty"`
	Members     []MemberBrief `json:"members,omitempty"`
}

// MemberBrief is a lightweight project member snapshot.
type MemberBrief struct {
	MemberID      uint   `json:"member_id"`
	MemberType    string `json:"member_type"` // user / assistant
	MemberRole    string `json:"member_role"` // owner / admin / member / viewer
	Name          string `json:"name"`
	IsDefault     bool   `json:"is_default,omitempty"`
	IsCurrentExec bool   `json:"is_current_exec,omitempty"` // marks the assistant executing this run
	IsCurrentUser bool   `json:"is_current_user,omitempty"` // marks the user who initiated this run
}

type TaskInput struct {
	Type        InputType     `json:"type"`
	Messages    []ChatMessage `json:"messages,omitempty"`
	Attachments []Attachment  `json:"attachments,omitempty"`
}

type ChatMessage struct {
	ID         string      `json:"id,omitempty"`
	Role       MessageRole `json:"role"`
	Content    string      `json:"content"`
	SenderName string      `json:"sender_name,omitempty"`
}

type Attachment struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	URL      string `json:"url,omitempty"`
}

type ModelOptions struct {
	ModelID      uint   `json:"model_id,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	BaseURLHasV1 bool   `json:"base_url_has_v1,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	// Vision 表示该模型是否支持图片（多模态）输入。
	Vision bool `json:"vision,omitempty"`
}

type RuntimeOptions struct {
	Kind    string `json:"kind,omitempty"`
	WorkDir string `json:"work_dir,omitempty"`
}

type TaskPolicy struct {
	RequireApproval bool   `json:"require_approval,omitempty"`
	PermissionMode  string `json:"permission_mode,omitempty"`
}
