package lifecycle

import (
	"context"
	"fmt"
	"time"

	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	"github.com/ygpkg/yg-go/logs"
)

// Runner 在具体运行时外层统一执行 Agent 生命周期。
type Runner struct {
	delegate         agent.Runner
	builder          *ContextBuilder
	toolAvailability ToolAvailability
}

// ToolAvailability resolves registered tool names for lifecycle hooks.
type ToolAvailability interface {
	AvailableToolNames(names []string) []string
}

func newRunner(delegate agent.Runner, builder *ContextBuilder, toolAvailability ToolAvailability) *Runner {
	return &Runner{
		delegate:         delegate,
		builder:          builder,
		toolAvailability: toolAvailability,
	}
}

// Run 构建统一上下文、执行具体运行时，并在结束后触发自我学习检查。
func (r *Runner) Run(ctx context.Context, req *agent.RequestContext) (result *agent.RunResult, runErr error) {
	startedAt := time.Now().UTC()
	var journal *RunJournal
	defer func() {
		if recovered := recover(); recovered != nil {
			err, ok := recovered.(error)
			if !ok {
				err = fmt.Errorf("%v", recovered)
			}
			result, runErr = emitFailed(ctx, journal, req, startedAt, RunPhasePanic, fmt.Errorf("agent runtime panic: %w", err), nil)
		}
	}()

	result, journal, runErr = r.run(ctx, req, startedAt)
	return result, runErr
}

func (r *Runner) run(ctx context.Context, req *agent.RequestContext, startedAt time.Time) (*agent.RunResult, *RunJournal, error) {
	if r == nil || r.delegate == nil {
		result, err := emitFailed(ctx, nil, req, startedAt, RunPhasePrepare, fmt.Errorf("delegate runner is required"), nil)
		return result, nil, err
	}
	if r.builder == nil {
		result, err := emitFailed(ctx, nil, req, startedAt, RunPhasePrepare, fmt.Errorf("context builder is required"), nil)
		return result, nil, err
	}

	if req != nil {
		normalizeRunRequest(req)
	}
	journal := NewRunJournal(req, nil)
	if req != nil {
		journal = NewRunJournal(req, req.EventSink)
		req.EventSink = journal
	}
	if err := appendLifecycleEvent(ctx, journal, req, events.EventStarted, ""); err != nil {
		result, runErr := emitFailed(ctx, journal, req, startedAt, RunPhasePrepare, err, nil)
		return result, journal, runErr
	}

	logs.InfoContextf(ctx, "Agent lifecycle run started: run_id=%s trace_id=%s task_id=%s runtime=%s assistant_id=%s input_type=%s",
		requestRunID(req),
		requestTraceID(req),
		requestTaskID(req),
		requestRuntimeKind(req),
		requestAssistantID(req),
		requestInputType(req),
	)

	prepared, err := r.builder.Prepare(ctx, req)
	if err != nil {
		logs.WarnContextf(ctx, "Agent lifecycle context prepare failed: run_id=%s trace_id=%s error=%v",
			requestRunID(req), requestTraceID(req), err)
		result, runErr := emitFailed(ctx, journal, req, startedAt, RunPhasePrepare, err, nil)
		return result, journal, runErr
	}
	prepared.EventSink = journal
	logs.InfoContextf(ctx, "Agent lifecycle context prepared: run_id=%s trace_id=%s system_prompt_len=%d skills=%d tools=%d messages=%d attachments=%d",
		prepared.RunID,
		prepared.TraceID,
		len(prepared.SystemPrompt),
		len(prepared.Assistant.Skills),
		len(prepared.Assistant.Tools),
		len(prepared.Input.Messages),
		len(prepared.Input.Attachments),
	)

	if err := EnsureModelConfig(ctx, prepared); err != nil {
		logs.WarnContextf(ctx, "Agent lifecycle model config failed: run_id=%s trace_id=%s model_id=%d error=%v",
			prepared.RunID, prepared.TraceID, prepared.Model.ID, err)
		result, runErr := emitFailed(ctx, journal, prepared, startedAt, RunPhaseModel, err, nil)
		return result, journal, runErr
	}
	logs.InfoContextf(ctx, "Agent lifecycle model config ready: run_id=%s trace_id=%s model_id=%d provider=%s model=%s base_url_set=%t",
		prepared.RunID,
		prepared.TraceID,
		prepared.Model.ID,
		prepared.Model.Provider,
		prepared.Model.Model,
		prepared.Model.BaseURL != "",
	)

	// logs.InfoContextf(ctx, "system prompt:%s", prepared.SystemPrompt)

	delegateStartedAt := time.Now()
	logs.InfoContextf(ctx, "Agent lifecycle delegate run started: run_id=%s trace_id=%s runtime=%s",
		prepared.RunID, prepared.TraceID, prepared.Runtime.Kind)

	result, runErr := r.delegate.Run(ctx, prepared)
	if runErr != nil {
		logs.WarnContextf(ctx, "Agent lifecycle delegate run failed: run_id=%s trace_id=%s elapsed=%s error=%v",
			prepared.RunID, prepared.TraceID, time.Since(delegateStartedAt), runErr)
		result, runErr = emitFailed(ctx, journal, prepared, startedAt, RunPhaseRuntime, runErr, metadataFromResult(result))
	} else {
		logs.InfoContextf(ctx, "Agent lifecycle delegate run completed: run_id=%s trace_id=%s status=%s elapsed=%s",
			prepared.RunID, prepared.TraceID, resultStatus(result), time.Since(delegateStartedAt))
		if err := emitSucceeded(ctx, journal, prepared, result); err != nil {
			logs.WarnContextf(ctx, "Agent lifecycle success event emit failed: run_id=%s trace_id=%s error=%v",
				prepared.RunID, prepared.TraceID, err)
			return result, journal, err
		}
	}

	if err := r.AfterRunLearning(ctx, prepared, result, journal.Trace()); err != nil {
		logs.WarnContextf(ctx, "Leros lifecycle learning check failed: %v", err)
	}
	logs.InfoContextf(ctx, "Agent lifecycle run finished: run_id=%s trace_id=%s status=%s elapsed=%s",
		prepared.RunID, prepared.TraceID, resultStatus(result), time.Since(startedAt))
	return result, journal, runErr
}

var _ agent.Runner = (*Runner)(nil)

func requestRunID(req *agent.RequestContext) string {
	if req == nil {
		return ""
	}
	return req.RunID
}

func requestTraceID(req *agent.RequestContext) string {
	if req == nil {
		return ""
	}
	return req.TraceID
}

func requestTaskID(req *agent.RequestContext) string {
	if req == nil {
		return ""
	}
	return req.TaskID
}

func requestRuntimeKind(req *agent.RequestContext) string {
	if req == nil {
		return ""
	}
	return req.Runtime.Kind
}

func requestAssistantID(req *agent.RequestContext) string {
	if req == nil {
		return ""
	}
	return req.Assistant.ID
}

func requestInputType(req *agent.RequestContext) agent.InputType {
	if req == nil {
		return ""
	}
	return req.Input.Type
}

func resultStatus(result *agent.RunResult) agent.RunStatus {
	if result == nil {
		return ""
	}
	return result.Status
}

func metadataFromResult(result *agent.RunResult) map[string]any {
	if result == nil || result.Metadata == nil {
		return nil
	}
	metadata := make(map[string]any, len(result.Metadata))
	for key, value := range result.Metadata {
		metadata[key] = value
	}
	return metadata
}
