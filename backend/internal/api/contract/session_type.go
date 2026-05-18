package contract

import (
	"time"

	"github.com/insmtx/Leros/backend/types"
)

// CreateSessionRequest 创建会话请求
type CreateSessionRequest struct {
	SessionID   string                 `json:"session_id,omitempty"`
	Type        string                 `json:"type" binding:"required"`
	AssistantID uint                   `json:"assistant_id,omitempty"`
	Title       string                 `json:"title,omitempty"`
	Metadata    *types.SessionMetadata `json:"metadata,omitempty"`
	ExpiredAt   *time.Time             `json:"expired_at,omitempty"`
}

// UpdateSessionRequest 更新会话请求
type UpdateSessionRequest struct {
	Title     string                 `json:"title,omitempty"`
	Metadata  *types.SessionMetadata `json:"metadata,omitempty"`
	ExpiredAt *time.Time             `json:"expired_at,omitempty"`
}

// ListSessionsRequest 查询会话列表请求
type ListSessionsRequest struct {
	Type          *string `json:"type,omitempty"`
	Status        *string `json:"status,omitempty"`
	AssistantID   *uint   `json:"assistant_id,omitempty"`
	AssistantCode *string `json:"assistant_code,omitempty"`
	Keyword       *string `json:"keyword,omitempty"`
	Pagination
}

// AddMessageRequest 添加消息请求
type AddMessageRequest struct {
	Role        string                 `json:"role" binding:"required"`
	Content     string                 `json:"content" binding:"required"`
	MessageType string                 `json:"message_type,omitempty"`
	Status      string                 `json:"status,omitempty"`
	Chunks      []string               `json:"chunks,omitempty"`
	Thinking    string                 `json:"thinking,omitempty"`
	ToolCalls   []types.ToolCall       `json:"tool_calls,omitempty"`
	Metadata    *types.MessageMetadata `json:"metadata,omitempty"`
}

// Session 会话响应结构（对应前端的 Conversation）
type Session struct {
	ID                   uint                   `json:"id"`
	SessionID            string                 `json:"session_id"`
	Type                 string                 `json:"type"`
	Uin                  uint                   `json:"uin"`
	OrgID                uint                   `json:"org_id"`
	AssistantID          uint                   `json:"assistant_id"`
	AllocatedAssistantID uint                   `json:"allocated_assistant_id"`
	AssistantCode        string                 `json:"assistant_code"`
	Status               string                 `json:"status"`
	Title                string                 `json:"title"`
	TitleManuallySet     bool                   `json:"title_manually_set,omitempty"`
	Metadata             *types.SessionMetadata `json:"metadata,omitempty"`
	MessageCount         int                    `json:"message_count"`
	LastMessageAt        *time.Time             `json:"last_message_at,omitempty"`
	ExpiredAt            *time.Time             `json:"expired_at,omitempty"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
}

// SessionMessage 消息响应结构（对齐前端 Message 模型）
type SessionMessage struct {
	ID          string                 `json:"id"`              // 前端用 string
	SessionID   string                 `json:"conversation_id"` // 对应前端的 conversationId
	Role        string                 `json:"role"`
	Content     string                 `json:"content"`
	Chunks      []string               `json:"chunks,omitempty"` // 流式片段
	Status      string                 `json:"status"`           // sending/streaming/complete/error
	Timestamp   int64                  `json:"timestamp"`        // Unix 毫秒时间戳
	ToolCalls   []types.ToolCall       `json:"tool_calls,omitempty"`
	Thinking    string                 `json:"thinking,omitempty"` // 思维链
	MessageType string                 `json:"message_type,omitempty"`
	Metadata    *types.MessageMetadata `json:"metadata,omitempty"`
	Sequence    int64                  `json:"sequence"`
	CreatedAt   time.Time              `json:"created_at"`
}

// SessionList 会话列表响应
type SessionList struct {
	Total  int64     `json:"total"`
	Offset int       `json:"offset"`
	Limit  int       `json:"limit"`
	Items  []Session `json:"items"`
}

// MessageList 消息列表响应
type MessageList struct {
	Total int64            `json:"total"`
	Page  int              `json:"page"`
	Items []SessionMessage `json:"items"`
}

// CompleteSessionMessageRequest 处理 session 完成事件请求
type CompleteSessionMessageRequest struct {
	SessionID string                 `json:"session_id"`
	Content   string                 `json:"content"`
	ToolCalls []types.ToolCall       `json:"tool_calls,omitempty"`
	Metadata  *types.MessageMetadata `json:"metadata,omitempty"`
	Seq       int64                  `json:"seq"`
	CreatedAt time.Time              `json:"created_at"`
}

// SessionTitleRequest 会话标题更新请求（异步处理）
type SessionTitleRequest struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
}

// FailedSessionMessageRequest 处理 session 失败事件请求
type FailedSessionMessageRequest struct {
	SessionID string `json:"session_id"`
	ErrorMsg  string `json:"error_msg"`
	ErrorCode string `json:"error_code,omitempty"`
	Seq       int64  `json:"seq"`
	CreatedAt time.Time `json:"created_at"`
}
