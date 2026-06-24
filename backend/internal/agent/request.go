package agent

import (
	"fmt"
	"strings"
)

// InputType describes the primary shape of the run input.
type InputType string

const (
	InputTypeMessage         InputType = "message"
	InputTypeTaskInstruction InputType = "task_instruction"
	InputTypeEvent           InputType = "event"
)

// RequestContext is the normalized execution snapshot consumed by runtime.
type RequestContext struct {
	RunID        string              `json:"run_id"`
	TraceID      string              `json:"trace_id,omitempty"`
	TaskID       string              `json:"task_id,omitempty"`
	Assistant    AssistantContext    `json:"assistant"`
	Actor        ActorContext        `json:"actor"`
	Conversation ConversationContext `json:"conversation,omitempty"`
	Workspace    WorkspaceContext    `json:"workspace,omitempty"`
	Input        InputContext        `json:"input"`
	Runtime      RuntimeOptions      `json:"runtime,omitempty"`
	Model        ModelOptions        `json:"model,omitempty"`
	Capability   CapabilityContext   `json:"capability,omitempty"`
	Policy       PolicyContext       `json:"policy,omitempty"`
	Metadata     map[string]any      `json:"metadata,omitempty"`

	SystemPrompt string    `json:"-"`
	EventSink    EventSink `json:"-"`
}

// AssistantContext is the assistant snapshot used for one run.
type AssistantContext struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
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
}

// InputContext is the normalized input passed to the agent.
type InputContext struct {
	Type        InputType      `json:"type"`
	Messages    []InputMessage `json:"messages,omitempty"`
	Attachments []Attachment   `json:"attachments,omitempty"`
}

// InputMessage is a simple role/content message snapshot.
type InputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Attachment describes an input attachment made available to the run.
type Attachment struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	URL      string `json:"url,omitempty"`
}

// RuntimeOptions controls runtime execution behavior.
type RuntimeOptions struct {
	Kind    string `json:"kind,omitempty"`
	WorkDir string `json:"work_dir,omitempty"`
	MaxStep int    `json:"max_step,omitempty"`
}

// ModelOptions lets callers override model behavior when supported.
type ModelOptions struct {
	Provider     string  `json:"provider,omitempty"`
	Model        string  `json:"model,omitempty"`
	APIKey       string  `json:"-"`
	BaseURL      string  `json:"base_url,omitempty"`
	BaseURLHasV1 bool    `json:"base_url_has_v1,omitempty"`
	Temperature  float64 `json:"temperature,omitempty"`
}

// CapabilityContext describes allowed capabilities for one run.
type CapabilityContext struct {
	AllowedTools []string `json:"allowed_tools,omitempty"`
}

// PolicyContext carries policy knobs for one run.
type PolicyContext struct {
	RequireApproval bool   `json:"require_approval,omitempty"`
	PermissionMode  string `json:"permission_mode,omitempty"` // "bypass" | "on-request" | "auto"; empty defaults to bypass
}

// BuildAttachmentText formats input attachments as a text block for prompt injection.
func BuildAttachmentText(attachments []Attachment) string {
	if len(attachments) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n[Attachments]\n")
	sb.WriteString("The following files were uploaded by the user:\n")
	for _, a := range attachments {
		sb.WriteString(fmt.Sprintf("- %s", a.Name))
		if a.URL != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", a.URL))
		}
		if a.MimeType != "" {
			sb.WriteString(fmt.Sprintf(" [%s]", a.MimeType))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// BuildUserInput joins the user-side messages from the request into a formatted text.
func BuildUserInput(req *RequestContext) string {
	if req == nil {
		return ""
	}
	if len(req.Input.Messages) > 0 {
		lines := make([]string, 0, len(req.Input.Messages))
		for _, message := range req.Input.Messages {
			if strings.TrimSpace(message.Content) == "" {
				continue
			}
			role := message.Role
			if role == "" {
				role = "user"
			}
			lines = append(lines, fmt.Sprintf("%s: %s", role, message.Content))
		}
		return strings.Join(lines, "\n")
	}
	return ""
}
