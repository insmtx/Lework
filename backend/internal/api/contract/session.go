package contract

import (
	"context"

	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
)

// SessionService 定义会话服务接口
type SessionService interface {
	// Session CRUD
	CreateSession(ctx context.Context, req *CreateSessionRequest) (*Session, error)
	GetSession(ctx context.Context, sessionID string) (*Session, error)
	UpdateSession(ctx context.Context, sessionID string, req *UpdateSessionRequest) (*Session, error)
	DeleteSession(ctx context.Context, sessionID string) error
	ListSessions(ctx context.Context, req *ListSessionsRequest) (*SessionList, error)

	// Lifecycle management
	ActivateSession(ctx context.Context, sessionID string) error
	PauseSession(ctx context.Context, sessionID string) error
	EndSession(ctx context.Context, sessionID string) error
	ResumeSession(ctx context.Context, sessionID string) error

	// Message management
	AddMessage(ctx context.Context, sessionID string, req *AddMessageRequest) (*SessionMessage, error)
	GetSessionMessages(ctx context.Context, sessionID string, page, perPage int) (*MessageList, error)
	DeleteMessage(ctx context.Context, messageID uint) error
	ClearSessionMessages(ctx context.Context, sessionID string) error

	// Event streaming
	StreamSessionEvents(ctx context.Context, sessionID string, lastSequence int64, sink events.Sink) error

	// CompleteSessionMessage 处理 session 完成事件，将最终回复消息入库
	CompleteSessionMessage(ctx context.Context, req *CompleteSessionMessageRequest) error
	// FailedSessionMessage 处理 session 失败事件，将错误消息入库
	FailedSessionMessage(ctx context.Context, req *FailedSessionMessageRequest) error

	// HandleSessionTitleRequest 异步处理会话标题更新请求
	HandleSessionTitleRequest(ctx context.Context, sessionID string) error
}
