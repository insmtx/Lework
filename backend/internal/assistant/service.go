// Package assistant is the SingerOS business wrapper layer for agent runs.
//
// Service is the single business entry point for an Agent Run:
//
//	validate → run.started → Preparer → agent.Executor → Finalizer → terminal → post-run
//
// It owns the business RunRequest/RunResult, wires Preparer and Finalizer
// via ports, and delegates execution to the agent.Executor.
package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
)

// Service is the single business entry point for an Agent Run.
type Service struct {
	preparer  Preparer
	executor  *agent.Executor
	finalizer Finalizer
	journal   JournalFactory
}

// NewService creates a new assistant Service.
func NewService(
	preparer Preparer,
	executor *agent.Executor,
	finalizer Finalizer,
	journal JournalFactory,
) *Service {
	return &Service{
		preparer:  preparer,
		executor:  executor,
		finalizer: finalizer,
		journal:   journal,
	}
}

// Run executes one business agent run. It:
//  1. Validates the request
//  2. Emits run.started
//  3. Calls Preparer to build ExecutionRequest
//  4. Calls agent.Executor for the runtime lifecycle
//  5. Calls Finalizer for required post-run tasks
//  6. Emits artifact events
//  7. Emits exactly one terminal event
//  8. Runs best-effort post-run tasks
func (s *Service) Run(
	ctx context.Context,
	req *assistantdomain.RunRequest,
	sink agent.EventSink,
) (*assistantdomain.RunResult, error) {
	if s == nil {
		return nil, fmt.Errorf("assistant service is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("run request is required")
	}
	if s.preparer == nil || s.executor == nil || s.finalizer == nil || s.journal == nil {
		return nil, fmt.Errorf("assistant service dependencies are incomplete")
	}

	// 1. Clone and normalize.
	cloned := assistantdomain.CloneRequest(req)
	if cloned.RunID == "" {
		cloned.RunID = fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
	}
	if cloned.Input.Type == "" {
		cloned.Input.Type = assistantdomain.InputTypeMessage
	}

	// 2. Start journal and emit run.started.
	j := s.journal.New(cloned, sink)
	if j == nil {
		return nil, fmt.Errorf("journal factory returned nil journal")
	}
	startedAt := time.Now().UTC()
	if err := j.Record(ctx, runStartedEvent(cloned, startedAt)); err != nil {
		return nil, fmt.Errorf("record run.started: %w", err)
	}

	// 3. Prepare.
	prepared, err := s.preparer.Prepare(ctx, cloned)
	if err != nil {
		return s.finishError(ctx, cloned, nil, j, "prepare", err, startedAt)
	}

	// 4. Execute via agent.Executor.
	runtimeResult, err := s.executor.Execute(ctx, prepared.Execution, journalObserver{j})
	if err != nil {
		return s.finishError(ctx, cloned, prepared, j, "execute", err, startedAt)
	}

	// 5. Required finalize.
	finalized, err := s.finalizer.FinalizeRequired(ctx, prepared, &runtimeResult, j.Snapshot())
	if err != nil {
		return s.finishError(ctx, cloned, prepared, j, "finalize", err, startedAt)
	}
	if finalized == nil || finalized.Result == nil {
		return s.finishError(
			ctx,
			cloned,
			prepared,
			j,
			"finalize",
			fmt.Errorf("finalizer returned an incomplete result"),
			startedAt,
		)
	}

	// 6. Record artifact events.
	for _, event := range finalized.Events {
		if err := j.Record(ctx, event); err != nil {
			return s.finishError(
				ctx,
				cloned,
				prepared,
				j,
				"artifact_publish",
				fmt.Errorf("record artifact event: %w", err),
				startedAt,
			)
		}
	}

	// 7. Emit exactly one terminal event with full payload.
	if finalized.Result.StartedAt.IsZero() {
		finalized.Result.StartedAt = startedAt
	}
	if finalized.Result.CompletedAt.IsZero() {
		finalized.Result.CompletedAt = time.Now().UTC()
	}
	termEvent, err := newTerminalEvent(finalized.Result, agent.EventType("run.completed"), j)
	if err != nil {
		return s.finishError(ctx, cloned, prepared, j, "terminal_encode", err, startedAt)
	}
	if err := j.Record(ctx, termEvent); err != nil {
		return finalized.Result, fmt.Errorf("record run.completed: %w", err)
	}

	// 8. Post-run best effort.
	s.finalizer.PostRunBestEffort(ctx, prepared, finalized.Result, j.Snapshot())

	return finalized.Result, nil
}

// finishError handles any failure during prepare/execute/finalize.
func (s *Service) finishError(
	ctx context.Context,
	req *assistantdomain.RunRequest,
	prepared *PreparedRun,
	j Journal,
	phase string,
	runErr error,
	startedAt time.Time,
) (*assistantdomain.RunResult, error) {
	if runErr == nil {
		return nil, nil
	}

	status := assistantdomain.RunStatusFailed
	message := ""
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		status = assistantdomain.RunStatusCancelled
		message = "已取消"
	}

	snapshot := JournalSnapshot{}
	if j != nil {
		snapshot = j.Snapshot()
	}
	result := &assistantdomain.RunResult{
		RunID:       req.RunID,
		TraceID:     req.TraceID,
		Status:      status,
		Message:     message,
		Error:       runErr.Error(),
		Usage:       snapshot.Usage,
		ToolCalls:   append([]agent.ToolCallRecord(nil), snapshot.ToolCalls...),
		StartedAt:   startedAt,
		CompletedAt: time.Now().UTC(),
		Metadata:    &assistantdomain.RunMetadata{Phase: phase},
	}

	eventType := agent.EventType("run.failed")
	if status == assistantdomain.RunStatusCancelled {
		eventType = "run.cancelled"
	}

	termEvent, terminalErr := newTerminalEvent(result, eventType, j)
	if j != nil {
		if terminalErr == nil {
			terminalErr = j.Record(ctx, termEvent)
		}
	}

	// Post-run best effort (do not modify result).
	if s != nil && s.finalizer != nil {
		s.finalizer.PostRunBestEffort(ctx, prepared, result, snapshot)
	}

	if terminalErr != nil {
		return result, errors.Join(runErr, fmt.Errorf("record terminal event: %w", terminalErr))
	}
	return result, runErr
}

// runStartedEvent creates a run.started event.
func runStartedEvent(req *assistantdomain.RunRequest, startedAt time.Time) *agent.Event {
	return &agent.Event{
		RunID:     req.RunID,
		TraceID:   req.TraceID,
		Type:      agent.EventType("run.started"),
		CreatedAt: startedAt,
		Content:   req.RunID,
	}
}

// newTerminalEvent creates a terminal event with a structured TerminalPayload.
func newTerminalEvent(result *assistantdomain.RunResult, eventType agent.EventType, j Journal) (*agent.Event, error) {
	if result == nil {
		return nil, fmt.Errorf("terminal result is required")
	}
	tp := assistantdomain.TerminalPayload{
		Status:      string(result.Status),
		Message:     result.Message,
		Error:       result.Error,
		Usage:       result.Usage,
		ToolCalls:   result.ToolCalls,
		Artifacts:   result.Artifacts,
		StartedAt:   result.StartedAt.Format(time.RFC3339Nano),
		CompletedAt: result.CompletedAt.Format(time.RFC3339Nano),
		Metadata:    result.Metadata,
	}
	if j != nil {
		snap := j.Snapshot()
		tp.Events = make([]assistantdomain.TerminalEventRecord, 0, len(snap.Events))
		for _, rec := range snap.Events {
			tp.Events = append(tp.Events, assistantdomain.TerminalEventRecord{
				Seq:       rec.Seq,
				LastSeq:   rec.LastSeq,
				Type:      string(rec.Type),
				Timestamp: rec.Timestamp,
				Payload:   append(json.RawMessage(nil), rec.Payload...),
			})
		}
	}
	raw, err := json.Marshal(tp)
	if err != nil {
		return nil, fmt.Errorf("marshal terminal payload: %w", err)
	}
	return &agent.Event{
		RunID:     result.RunID,
		TraceID:   result.TraceID,
		Type:      eventType,
		CreatedAt: result.CompletedAt,
		Content:   result.Message,
		Payload:   json.RawMessage(raw),
	}, nil
}

// journalObserver adapts a Journal to agent.EventSink for the executor.
type journalObserver struct {
	j Journal
}

func (o journalObserver) Emit(ctx context.Context, event *agent.Event) error {
	if o.j == nil {
		return nil
	}
	if event != nil && (event.Type == "execution.started" ||
		event.Type == "execution.completed" ||
		event.Type == "execution.failed" ||
		event.Type == "execution.cancelled") {
		return nil
	}
	return o.j.Record(ctx, event)
}
