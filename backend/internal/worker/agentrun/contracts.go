package agentrun

import (
	"context"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

// Preparer converts a business run request into an immutable prepared run.
type Preparer interface {
	Prepare(ctx context.Context, req *agentrundomain.RunRequest) (*PreparedRun, error)
}

// ToolProvider resolves business tools into the neutral execution contract.
type ToolProvider interface {
	ToolsFor(
		req *agentrundomain.RunRequest,
		workspace WorkspacePreparation,
	) ([]agent.Tool, error)
}

// PlanPublisher processes plan.ready events emitted by the runtime.
// It reads the plan file, uploads it, and returns a plan.published event.
// Upload failures return PlanPublishError; AgentRun classifies those failures
// as business plan_publish errors instead of Runtime execution errors.
type PlanPublisher interface {
	Publish(ctx context.Context, event agent.NodeEvent) (*messaging.RunEventBody, error)
}

// Finalizer performs required business finalization and best-effort post-run work.
type Finalizer interface {
	FinalizeRequired(
		ctx context.Context,
		run *PreparedRun,
		runtimeResult *agent.ExecutionResult,
		snapshot JournalSnapshot,
	) (*Finalization, error)
	PostRunBestEffort(
		ctx context.Context,
		run *PreparedRun,
		result *agentrundomain.RunResult,
		snapshot JournalSnapshot,
	)
}

// Finalization holds the final business result and events emitted before terminal.
type Finalization struct {
	Result *agentrundomain.RunResult
	Events []messaging.RunEventBody
}

// EventContext carries immutable routing and tracing values for one business run.
type EventContext struct {
	OrgID             uint
	WorkerID          uint
	SessionID         string
	TraceID           string
	RequestID         string
	TaskID            string
	RunID             string
	ParentID          string
	ReplyToMessageIDs []string
}

// RunEventPublisher publishes a fully constructed Worker/Server business event.
type RunEventPublisher interface {
	PublishRunEvent(ctx context.Context, event messaging.RunEvent) error
}

// RunEventDraft is an unsequenced business event produced inside AgentRun.
type RunEventDraft struct {
	OccurredAt time.Time
	Body       messaging.RunEventBody
}

// Journal records, sequences, archives, and publishes events for one business run.
type Journal interface {
	Record(ctx context.Context, event RunEventDraft) error
	Snapshot() JournalSnapshot
}

// JournalFactory creates a Journal bound to a run and downstream observer.
type JournalFactory interface {
	New(
		req *agentrundomain.RunRequest,
		eventContext EventContext,
		publisher RunEventPublisher,
	) Journal
}

// JournalSnapshot is the immutable activity summary used by finalization.
type JournalSnapshot struct {
	ToolCalls    []agentrundomain.ToolCallRecord
	Usage        *agentrundomain.Usage
	MessageCount int
	ToolFailures int
	ToolNames    []string
	Events       []messaging.RunEventRecord
}
