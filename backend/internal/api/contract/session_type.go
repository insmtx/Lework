package contract

import (
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/types"
)

// CreateSessionRequest creates a session.
type CreateSessionRequest struct {
	SessionID   string                `json:"session_id,omitempty"`
	Type        string                `json:"type" binding:"required"`
	AssistantID uint                  `json:"assistant_id,omitempty"`
	Title       string                `json:"title,omitempty"`
	Metadata    *types.ObjectMetadata `json:"metadata,omitempty"`
	ExpiredAt   *time.Time            `json:"expired_at,omitempty"`
}

// UpdateSessionRequest updates basic session fields.
type UpdateSessionRequest struct {
	Title     string                `json:"title,omitempty"`
	Metadata  *types.ObjectMetadata `json:"metadata,omitempty"`
	ExpiredAt *time.Time            `json:"expired_at,omitempty"`
}

// ListSessionsRequest queries sessions.
type ListSessionsRequest struct {
	Type          *string `json:"type,omitempty"`
	Status        *string `json:"status,omitempty"`
	AssistantID   *uint   `json:"assistant_id,omitempty"`
	AssistantCode *string `json:"assistant_code,omitempty"`
	Keyword       *string `json:"keyword,omitempty"`
	types.Pagination
}

// AddMessageRequest adds a message to a session.
type AddMessageRequest struct {
	Role          string                    `json:"role" binding:"required"`
	Content       string                    `json:"content" binding:"required"`
	ExecutionMode agent.ExecutionMode       `json:"execution_mode,omitempty" binding:"omitempty,oneof=default plan"`
	MessageType   string                    `json:"message_type,omitempty"`
	Chunks        []types.MessageChunk      `json:"chunks,omitempty"`
	Attachments   []types.MessageAttachment `json:"attachments,omitempty"`
	Thinking      string                    `json:"thinking,omitempty"`
	Metadata      *types.ObjectMetadata     `json:"metadata,omitempty"`
	Usage         *types.MessageUsage       `json:"usage,omitempty"`
}

// Session is the API response shape for a conversation.
// SubmitApprovalRequest forwards an approval decision to the worker via NATS.
type SubmitApprovalRequest struct {
	OrgID     uint   `json:"-"`
	WorkerID  uint   `json:"-"` // TODO: 由 session 关联的运行时动态获取
	SessionID string `json:"session_id"`
	RequestID string `json:"request_id"`
	Action    string `json:"action"`
	Reason    string `json:"reason,omitempty"`
}

// SubmitQuestionAnswerRequest forwards a question answer to the worker via NATS.
type SubmitQuestionAnswerRequest struct {
	OrgID     uint       `json:"-"`
	WorkerID  uint       `json:"-"`
	SessionID string     `json:"session_id"`
	RequestID string     `json:"request_id"`
	Answers   [][]string `json:"answers"`
}

type Session struct {
	SessionID            string                `json:"session_id"`
	Type                 string                `json:"type"`
	Uin                  uint                  `json:"uin"`
	OrgID                uint                  `json:"org_id"`
	AssistantID          uint                  `json:"assistant_id"`
	AllocatedAssistantID uint                  `json:"allocated_assistant_id"`
	AssistantCode        string                `json:"assistant_code"`
	Status               string                `json:"status"`
	RuntimeStatus        string                `json:"runtime_status"`
	Title                string                `json:"title"`
	TitleManuallySet     bool                  `json:"title_manually_set,omitempty"`
	Metadata             *types.ObjectMetadata `json:"metadata,omitempty"`
	MessageCount         int                   `json:"message_count"`
	LastMessageAt        *time.Time            `json:"last_message_at,omitempty"`
	ExpiredAt            *time.Time            `json:"expired_at,omitempty"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
}

// SessionMessage is the API response shape for a persisted conversation message.
type SessionMessage struct {
	ID          string                    `json:"id"`
	SessionID   string                    `json:"session_id"`
	Role        string                    `json:"role"`
	Content     string                    `json:"content"`
	ErrorMsg    string                    `json:"error_msg,omitempty"`
	Chunks      []SessionEvent            `json:"chunks,omitempty"`
	Artifacts   []types.MessageArtifact   `json:"artifacts,omitempty"`
	Attachments []types.MessageAttachment `json:"attachments,omitempty"`
	Timestamp   int64                     `json:"timestamp"`
	MessageType string                    `json:"message_type,omitempty"`
	Metadata    *types.ObjectMetadata     `json:"metadata,omitempty"`
	Usage       *types.MessageUsage       `json:"usage,omitempty"`
	Sequence    int64                     `json:"sequence"`
	CreatedAt   time.Time                 `json:"created_at"`
}

// SessionEvent is the public event shape embedded in persisted message chunks.
type SessionEvent struct {
	Type      string      `json:"type"`
	SessionID string      `json:"session_id"`
	Payload   interface{} `json:"payload,omitempty"`
	Sequence  int64       `json:"sequence"`
	Timestamp int64       `json:"timestamp"`
}

// SessionList is a paginated session response.
type SessionList struct {
	Total  int64     `json:"total"`
	Offset int       `json:"offset"`
	Limit  int       `json:"limit"`
	Items  []Session `json:"items"`
}

// MessageList is a paginated session message response.
type MessageList struct {
	Total int64            `json:"total"`
	Page  int              `json:"page"`
	Items []SessionMessage `json:"items"`
}

// GetSessionMessagesRequest queries paged session messages.
type GetSessionMessagesRequest struct {
	SessionID string `json:"session_id"`
	Page      int    `json:"page,omitempty"`
	PerPage   int    `json:"per_page,omitempty"`
}

// CompleteSessionMessageRequest persists a completed assistant message.
type CompleteSessionMessageRequest struct {
	SessionID         string                  `json:"session_id"`
	Content           string                  `json:"content"`
	ReplyToMessageIDs []string                `json:"reply_to_message_ids,omitempty"`
	Chunks            []types.MessageChunk    `json:"chunks,omitempty"`
	Artifacts         []types.MessageArtifact `json:"artifacts,omitempty"`
	Metadata          *types.ObjectMetadata   `json:"metadata,omitempty"`
	Usage             *types.MessageUsage     `json:"usage,omitempty"`
	Seq               int64                   `json:"seq"`
	CreatedAt         time.Time               `json:"created_at"`
}

// FailedSessionMessageRequest persists a failed assistant message.
type FailedSessionMessageRequest struct {
	SessionID         string                  `json:"session_id"`
	Content           string                  `json:"content,omitempty"`
	ReplyToMessageIDs []string                `json:"reply_to_message_ids,omitempty"`
	Chunks            []types.MessageChunk    `json:"chunks,omitempty"`
	Artifacts         []types.MessageArtifact `json:"artifacts,omitempty"`
	ErrorMsg          string                  `json:"error_msg"`
	ErrorCode         string                  `json:"error_code,omitempty"`
	Status            string                  `json:"status,omitempty"`
	Metadata          *types.ObjectMetadata   `json:"metadata,omitempty"`
	Usage             *types.MessageUsage     `json:"usage,omitempty"`
	Seq               int64                   `json:"seq"`
	CreatedAt         time.Time               `json:"created_at"`
}

// SessionRunStartedRequest marks user messages as processing when a worker run starts.
type SessionRunStartedRequest struct {
	SessionID         string   `json:"session_id"`
	ReplyToMessageIDs []string `json:"reply_to_message_ids,omitempty"`
	RequestID         string   `json:"request_id,omitempty"`
	StreamStartSeq    uint64   `json:"stream_start_seq"`
	StateStartSeq     uint64   `json:"state_start_seq,omitempty"`
}
