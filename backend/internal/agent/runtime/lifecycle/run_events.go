package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	"github.com/ygpkg/yg-go/logs"
)

// RunPhase indicates the lifecycle stage where a runtime error occurred.
type RunPhase string

const (
	RunPhasePrepare RunPhase = "prepare"
	RunPhaseModel   RunPhase = "model"
	RunPhaseRuntime RunPhase = "runtime"
	RunPhasePanic   RunPhase = "panic"
)

func emitSucceeded(ctx context.Context, journal *RunJournal, req *agent.RequestContext, result *agent.RunResult) error {
	if req == nil || result == nil || result.Status != agent.RunStatusCompleted {
		return nil
	}
	if result.Message != "" {
		if err := appendLifecycleEvent(ctx, journal, req, events.EventResult, result.Message); err != nil {
			return err
		}
	}
	return emitTerminalRunEvent(ctx, journal, req, result, events.EventCompleted)
}

func emitFailed(ctx context.Context, journal *RunJournal, req *agent.RequestContext, phase RunPhase, err error, metadata map[string]any) (*agent.RunResult, error) {
	if err == nil {
		return nil, nil
	}
	if req != nil {
		normalizeRunRequest(req)
	}

	status, eventType := failureStatus(err)
	message := err.Error()
	logs.WarnContextf(ctx, "Agent run failed: run_id=%s trace_id=%s task_id=%s runtime=%s phase=%s status=%s error=%v",
		requestRunID(req),
		requestTraceID(req),
		requestTaskID(req),
		requestRuntimeKind(req),
		phase,
		status,
		err,
	)

	result := &agent.RunResult{
		RunID:       requestRunID(req),
		TraceID:     requestTraceID(req),
		Status:      status,
		Error:       message,
		StartedAt:   failureStartedAt(journal),
		CompletedAt: time.Now().UTC(),
		Metadata:    metadataWithLifecyclePhase(metadata, phase),
	}
	if emitErr := emitTerminalRunEvent(ctx, journal, req, result, eventType); emitErr != nil {
		logs.WarnContextf(ctx, "Agent run failure event emit failed: run_id=%s trace_id=%s phase=%s error=%v",
			requestRunID(req), requestTraceID(req), phase, emitErr)
	}
	return result, err
}

func emitTerminalRunEvent(ctx context.Context, journal *RunJournal, req *agent.RequestContext, result *agent.RunResult, eventType events.EventType) error {
	if result == nil {
		return nil
	}
	normalizeTerminalResult(journal, result)
	result.Metadata = mergeRunMetadata(req, result.Metadata)
	payload := terminalRunPayload(journal, result)
	event := events.NewRunCompleted(payload, resultMessage(result))
	event.Type = eventType
	return appendEvent(ctx, journal, req, event)
}

func normalizeTerminalResult(journal *RunJournal, result *agent.RunResult) {
	if result == nil || !result.StartedAt.IsZero() {
		return
	}
	if startedAt := journal.StartedAt(); !startedAt.IsZero() {
		result.StartedAt = startedAt
	}
}

func terminalRunPayload(journal *RunJournal, result *agent.RunResult) events.RunCompletedPayload {
	if journal != nil {
		return journal.CompletedPayload(result)
	}
	return events.RunCompletedPayload{
		Status: string(result.Status),
		Result: events.RunResultPayload{
			Message: resultMessage(result),
		},
		Usage:       result.Usage,
		StartedAt:   result.StartedAt,
		CompletedAt: result.CompletedAt,
		Metadata:    result.Metadata,
	}
}

func failureStartedAt(journal *RunJournal) time.Time {
	if startedAt := journal.StartedAt(); !startedAt.IsZero() {
		return startedAt
	}
	return time.Now().UTC()
}

func appendLifecycleEvent(ctx context.Context, journal *RunJournal, req *agent.RequestContext, eventType events.EventType, message string) error {
	return appendEvent(ctx, journal, req, &events.Event{
		Type:    eventType,
		Content: message,
	})
}

func appendEvent(ctx context.Context, journal *RunJournal, req *agent.RequestContext, event *events.Event) error {
	if event == nil {
		return nil
	}
	if journal == nil {
		if req == nil || req.EventSink == nil {
			return nil
		}
		if event.RunID == "" {
			event.RunID = req.RunID
		}
		if event.TraceID == "" {
			event.TraceID = req.TraceID
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now().UTC()
		}
		if event.ID == "" && event.RunID != "" && event.Seq > 0 {
			event.ID = fmt.Sprintf("%s:%d", event.RunID, event.Seq)
		}
		return req.EventSink.Emit(ctx, event)
	}
	return journal.Append(ctx, event)
}

func failureStatus(err error) (agent.RunStatus, events.EventType) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return agent.RunStatusCancelled, events.EventCancelled
	}
	return agent.RunStatusFailed, events.EventFailed
}

func normalizeRunRequest(req *agent.RequestContext) {
	if req.RunID == "" {
		req.RunID = fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
	}
	if req.Input.Type == "" {
		req.Input.Type = agent.InputTypeMessage
	}
}

func metadataWithLifecyclePhase(metadata map[string]any, phase RunPhase) map[string]any {
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["phase"] = string(phase)
	return metadata
}

func mergeRunMetadata(req *agent.RequestContext, resultMetadata map[string]any) map[string]any {
	if req == nil && len(resultMetadata) == 0 {
		return nil
	}
	merged := map[string]any{}
	if req != nil {
		for key, value := range req.Metadata {
			merged[key] = value
		}
	}
	for key, value := range resultMetadata {
		merged[key] = value
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}
