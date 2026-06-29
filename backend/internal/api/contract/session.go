package contract

import (
	"context"

	"github.com/insmtx/Leros/backend/agent/runtime/events"
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
	StreamSessionEvents(ctx context.Context, sessionID string, replay bool, sink events.Sink) error

	// HandleSessionRunStarted marks source user messages as processing and records replay metadata.
	HandleSessionRunStarted(ctx context.Context, req *SessionRunStartedRequest) error

	// CompleteSessionMessage persists the final assistant message for a completed session run.
	CompleteSessionMessage(ctx context.Context, req *CompleteSessionMessageRequest) error
	// FailedSessionMessage persists the final assistant message for a failed session run.
	FailedSessionMessage(ctx context.Context, req *FailedSessionMessageRequest) error

	// HandleSessionTitleRequest handles an asynchronous session title update request.
	HandleSessionTitleRequest(ctx context.Context, sessionID string) error

	// SubmitApproval forwards an approval decision to the worker via NATS.
	SubmitApproval(ctx context.Context, req *SubmitApprovalRequest) error

	// SubmitQuestionAnswer forwards a question answer to the worker via NATS.
	SubmitQuestionAnswer(ctx context.Context, req *SubmitQuestionAnswerRequest) error

	// CancelSessionRun cancels an active agent run for the given session.
	CancelSessionRun(ctx context.Context, sessionID string, req *CancelSessionRunRequest) (*CancelSessionRunResponse, error)

	// SetSessionStreamStartSeq records the NATS stream sequence for the first
	// run.stream event of a session, used by the stream projector for SSE replay.
	SetSessionStreamStartSeq(ctx context.Context, sessionID string, streamSeq uint64) error
}

// CancelSessionRunRequest is the request body for cancelling a session agent run.
type CancelSessionRunRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	RunID     string `json:"run_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// CancelSessionRunResponse is the response for a cancel session run request.
type CancelSessionRunResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"` // "cancelled" | "no_active_run"
}
