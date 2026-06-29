package assistant

import (
	"context"
	"encoding/json"

	"github.com/insmtx/Leros/backend/agent"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
)

// Preparer converts a business run request into an immutable prepared run.
type Preparer interface {
	Prepare(ctx context.Context, req *assistantdomain.RunRequest) (*PreparedRun, error)
}

// ToolProvider resolves business tools into the neutral execution contract.
type ToolProvider interface {
	ToolsFor(
		req *assistantdomain.RunRequest,
		workspace WorkspacePreparation,
	) ([]agent.Tool, error)
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
		result *assistantdomain.RunResult,
		snapshot JournalSnapshot,
	)
}

// Finalization holds the final business result and events emitted before terminal.
type Finalization struct {
	Result *assistantdomain.RunResult
	Events []*agent.Event
}

// Journal records, sequences, archives, and forwards events for one business run.
type Journal interface {
	Record(ctx context.Context, event *agent.Event) error
	Snapshot() JournalSnapshot
}

// JournalFactory creates a Journal bound to a run and downstream sink.
type JournalFactory interface {
	New(req *assistantdomain.RunRequest, sink agent.EventSink) Journal
}

// JournalSnapshot is the immutable activity summary used by finalization.
type JournalSnapshot struct {
	ToolCalls    []agent.ToolCallRecord
	Usage        *agent.Usage
	MessageCount int
	ToolFailures int
	ToolNames    []string
	Events       []JournalEventRecord
}

// JournalEventRecord is an archived non-terminal event.
type JournalEventRecord struct {
	Seq       int64           `json:"seq"`
	LastSeq   int64           `json:"last_seq,omitempty"`
	Type      agent.EventType `json:"type"`
	Timestamp int64           `json:"timestamp"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}
