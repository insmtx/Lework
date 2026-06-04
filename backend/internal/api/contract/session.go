package contract

import (
	"context"

	"github.com/insmtx/Leros/backend/internal/runtime/events"
)

// SessionService defines the session service contract.
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

	// CompleteSessionMessage persists the final assistant message for a completed session run.
	CompleteSessionMessage(ctx context.Context, req *CompleteSessionMessageRequest) error
	// FailedSessionMessage persists the final assistant message for a failed session run.
	FailedSessionMessage(ctx context.Context, req *FailedSessionMessageRequest) error

	// HandleSessionTitleRequest handles an asynchronous session title update request.
	HandleSessionTitleRequest(ctx context.Context, sessionID string) error

	// SubmitApproval forwards an approval decision to the worker via NATS.
	SubmitApproval(ctx context.Context, req *SubmitApprovalRequest) error
}
