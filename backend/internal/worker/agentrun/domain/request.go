package domain

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/internal/consts"
)

// InputType describes the primary shape of the run input.
type InputType string

const (
	InputTypeMessage         InputType = "message"
	InputTypeTaskInstruction InputType = "task_instruction"
	InputTypeEvent           InputType = "event"
)

// ExecutionMode describes the business-requested execution behavior before it
// is translated into the agent runtime contract.
type ExecutionMode string

const (
	// ExecutionModeDefault keeps the runtime's normal execution behavior.
	ExecutionModeDefault ExecutionMode = "default"
	// ExecutionModePlan requests planning behavior when the runtime supports it.
	ExecutionModePlan ExecutionMode = "plan"
)

// BusinessKeys carries the business primary key IDs used for LLM call record association.
type BusinessKeys struct {
	ProjectPKID       uint   `json:"project_pk_id"`
	SessionPKID       uint   `json:"session_pk_id"`
	MessagePKID       uint   `json:"message_pk_id"`
	AssistantID       uint   `json:"assistant_id,omitempty"`        // leros_digital_assistant.id
	AssistantPublicID string `json:"assistant_public_id,omitempty"` // leros_digital_assistant.public_id
	WorkerPublicID    string `json:"worker_public_id,omitempty"`    // leros_worker_deployment.public_id
	UinPK             uint   `json:"uin_pk"`
}

// RunRequest is the normalized execution snapshot consumed by runtime.
type RunRequest struct {
	RunID         string              `json:"run_id"`
	TraceID       string              `json:"trace_id,omitempty"`
	TaskID        string              `json:"task_id,omitempty"`
	ExecutionMode ExecutionMode       `json:"execution_mode,omitempty"`
	Assistant     AssistantContext    `json:"assistant"`
	Actor         ActorContext        `json:"actor"`
	Conversation  ConversationContext `json:"conversation,omitempty"`
	Workspace     WorkspaceContext    `json:"workspace,omitempty"`
	Project       ProjectContext      `json:"project,omitempty"`
	Input         InputContext        `json:"input"`
	Runtime       RuntimeOptions      `json:"runtime,omitempty"`
	Model         ModelOptions        `json:"model,omitempty"`
	Capability    CapabilityContext   `json:"capability,omitempty"`
	Policy        PolicyContext       `json:"policy,omitempty"`
	Plugins       []PluginSnapshot    `json:"plugins,omitempty"`
	BusinessKeys  BusinessKeys        `json:"business_keys"`
}

// PluginSnapshot is the worker-owned execution view of one immutable plugin revision.
type PluginSnapshot struct {
	PluginID   string          `json:"plugin_id"`
	Code       string          `json:"code"`
	Kind       string          `json:"kind"`
	Revision   int             `json:"revision"`
	Definition json.RawMessage `json:"definition"`
}

// AssistantContext is the assistant snapshot used for one run.
type AssistantContext struct {
	// ID 是 leros_digital_assistant.id，自增主键，用于 llm_history 关联。
	ID uint `json:"id,omitempty"`
	// PublicID 是 leros_digital_assistant.public_id，用于标识执行本次运行的 AI 队友。
	PublicID     string   `json:"public_id,omitempty"`
	Name         string   `json:"name,omitempty"`
	Description  string   `json:"description,omitempty"`
	Role         string   `json:"role,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	Skills       []string `json:"skills,omitempty"`
	Tools        []string `json:"tools,omitempty"`
}

// ActorContext describes the human or system actor that initiated the run.
type ActorContext struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name,omitempty"`
	Channel     string `json:"channel,omitempty"`
	ExternalID  string `json:"external_id,omitempty"`
	AccountID   string `json:"account_id,omitempty"`
}

// ConversationContext carries recent conversation state when available.
type ConversationContext struct {
	ID       string         `json:"id,omitempty"`
	Messages []InputMessage `json:"messages,omitempty"`
}

// WorkspaceContext identifies the project workspace owned by this run.
type WorkspaceContext struct {
	OrgID     uint   `json:"org_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	RepoDir   string `json:"repo_dir,omitempty"`
	// SkillDir is the per-run project view of Skills prepared by the worker.
	// It is intentionally not supplied by the server.
	SkillDir string `json:"skill_dir,omitempty"`
}

// ProjectContext is the project snapshot used for one run.
type ProjectContext struct {
	Name        string        `json:"name,omitempty"`
	Description string        `json:"description,omitempty"`
	Objective   string        `json:"objective,omitempty"`
	Members     []MemberBrief `json:"members,omitempty"`
}

// MemberBrief is a lightweight project member snapshot.
type MemberBrief struct {
	MemberID      uint   `json:"member_id"`
	MemberType    string `json:"member_type"`
	MemberRole    string `json:"member_role"`
	Name          string `json:"name"`
	IsDefault     bool   `json:"is_default,omitempty"`
	IsCurrentExec bool   `json:"is_current_exec,omitempty"`
	IsCurrentUser bool   `json:"is_current_user,omitempty"`
}

// InputContext is the normalized input passed to the agent.
type InputContext struct {
	Type        InputType      `json:"type"`
	Messages    []InputMessage `json:"messages,omitempty"`
	Attachments []Attachment   `json:"attachments,omitempty"`
}

// InputMessage is a simple role/content message snapshot.
type InputMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	SenderName string `json:"sender_name,omitempty"`
}

// Attachment describes an input attachment made available to the run.
type Attachment struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	URL      string `json:"url,omitempty"`
	// Data holds decoded bytes for multimodal attachments (e.g. images).
	// It is populated in-process by the preparer and never serialized.
	Data []byte `json:"-"`
}

// RuntimeOptions controls runtime execution behavior.
type RuntimeOptions struct {
	Kind    string `json:"kind,omitempty"`
	WorkDir string `json:"work_dir,omitempty"`
}

// ModelOptions lets callers override model behavior when supported.
type ModelOptions struct {
	ModelID      uint    `json:"model_id,omitempty"`
	Provider     string  `json:"provider,omitempty"`
	Model        string  `json:"model,omitempty"`
	APIKey       string  `json:"-"`
	BaseURL      string  `json:"base_url,omitempty"`
	BaseURLHasV1 bool    `json:"base_url_has_v1,omitempty"`
	Temperature  float64 `json:"temperature,omitempty"`
	// Vision 表示该模型是否支持图片（多模态）输入。
	Vision bool `json:"vision,omitempty"`
}

// CapabilityContext describes allowed capabilities for one run.
type CapabilityContext struct {
	AllowedTools []string `json:"allowed_tools,omitempty"`
}

// PolicyContext carries policy knobs for one run.
type PolicyContext struct {
	RequireApproval bool   `json:"require_approval,omitempty"`
	PermissionMode  string `json:"permission_mode,omitempty"`
}

// BuildAttachmentText formats input attachments as a text block for prompt injection.
//
// 仅图片作为视觉内容随消息内联注入（见 preparer 的 multimodalAttachmentsForRuntime
// 与 opencode 的 modalityConfig：vision 模型只声明 image 输入），模型可直接查看，
// 无需再调用 read 工具；PDF/音视频不具视觉能力，由 opencode 降级为文本提示，
// 因此归入"按路径读取"。文本块据此分流，避免模型对图片调用 read 拿到
// "Image read successfully"后产生幻觉。
func BuildAttachmentText(attachments []Attachment) string {
	if len(attachments) == 0 {
		return ""
	}
	var inline, external []Attachment
	for _, a := range attachments {
		if IsVisualMime(a.MimeType) {
			inline = append(inline, a)
		} else {
			external = append(external, a)
		}
	}
	var sb strings.Builder
	sb.WriteString("\n[Attachments]\n")
	if len(inline) > 0 {
		sb.WriteString("The user attached the following files. Their visual content is already provided in this message, you can see them directly (do NOT call the read tool on them):\n")
		for _, a := range inline {
			sb.WriteString(fmt.Sprintf("- %s", a.Name))
			if a.MimeType != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", a.MimeType))
			}
			sb.WriteString("\n")
		}
	}
	if len(external) > 0 {
		sb.WriteString("The following files were attached by the user in this message, read them on disk to understand their content:\n")
		for _, a := range external {
			sb.WriteString(fmt.Sprintf("- %s\n", a.Name))
			if a.Name != "" {
				sb.WriteString(fmt.Sprintf("  Location: %s/%s\n", consts.RepoDirUploads, a.Name))
			}
			if a.URL != "" {
				sb.WriteString(fmt.Sprintf("  URL: %s\n", a.URL))
			}
			if a.MimeType != "" {
				sb.WriteString(fmt.Sprintf("  Type: %s\n", a.MimeType))
			}
		}
	}
	return sb.String()
}

// IsVisualMime 判断附件是否随消息内联为视觉内容。仅图片如此——vision 模型声明的
// 输入模态只含 image（见 opencode modalityConfig），PDF/音视频已整体退出多模态管线，
// 一律按磁盘路径读取，不再作为视觉输入。
func IsVisualMime(mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if mime == "" {
		return false
	}
	return strings.HasPrefix(mime, "image/")
}

// BuildUserInput joins the user-side messages from the request into a formatted text.
func BuildUserInput(req *RunRequest) string {
	if req == nil {
		return ""
	}
	if len(req.Input.Messages) > 0 {
		lines := make([]string, 0, len(req.Input.Messages))
		for i, message := range req.Input.Messages {
			if strings.TrimSpace(message.Content) == "" {
				continue
			}
			name := strings.TrimSpace(message.SenderName)
			if name == "" {
				name = message.Role
				if name == "" {
					name = "user"
				}
			}
			if message.Role == "assistant" {
				lines = append(lines, fmt.Sprintf("【AI 队友回复】\n[%d] AI 队友 「%s」发送：「%s」", i+1, name, message.Content))
			} else {
				lines = append(lines, fmt.Sprintf("【用户问题】\n[%d] 用户 「%s」发送：「%s」", i+1, name, message.Content))
			}
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

// CloneRequest returns a deep copy of the RunRequest.
func CloneRequest(req *RunRequest) *RunRequest {
	if req == nil {
		return nil
	}
	clone := *req

	clone.Assistant.Skills = copyStringSlice(req.Assistant.Skills)
	clone.Assistant.Tools = copyStringSlice(req.Assistant.Tools)

	clone.Conversation.Messages = copyInputMessages(req.Conversation.Messages)

	clone.Input.Messages = copyInputMessages(req.Input.Messages)
	clone.Input.Attachments = copyAttachments(req.Input.Attachments)

	clone.Capability.AllowedTools = copyStringSlice(req.Capability.AllowedTools)
	clone.Plugins = make([]PluginSnapshot, len(req.Plugins))
	for i, plugin := range req.Plugins {
		clone.Plugins[i] = plugin
		clone.Plugins[i].Definition = append(json.RawMessage(nil), plugin.Definition...)
	}

	return &clone
}

func copyStringSlice(src []string) []string {
	if src == nil {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

func copyInputMessages(src []InputMessage) []InputMessage {
	if src == nil {
		return nil
	}
	dst := make([]InputMessage, len(src))
	copy(dst, src)
	return dst
}

func copyAttachments(src []Attachment) []Attachment {
	if src == nil {
		return nil
	}
	dst := make([]Attachment, len(src))
	copy(dst, src)
	return dst
}
