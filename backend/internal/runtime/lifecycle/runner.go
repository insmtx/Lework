package lifecycle

import (
	"context"
	"fmt"
	"time"

	"github.com/insmtx/Leros/backend/internal/agent"
	lifecyclecontext "github.com/insmtx/Leros/backend/internal/runtime/lifecycle/context"
	lifecyclejournal "github.com/insmtx/Leros/backend/internal/runtime/lifecycle/journal"
	"github.com/insmtx/Leros/backend/internal/runtime/lifecycle/steps"
	"github.com/ygpkg/yg-go/logs"
)

type Runner struct {
	delegate         agent.Runner
	builder          *lifecyclecontext.ContextBuilder
	toolAvailability ToolAvailability
	artifactRecorder steps.ArtifactRecorder
	learning         *steps.LearningService
	pipeline         Pipeline
}

type ToolAvailability = steps.ToolAvailability

func NewRunner(delegate agent.Runner, builder *lifecyclecontext.ContextBuilder, toolAvailability ToolAvailability) *Runner {
	r := &Runner{
		delegate:         delegate,
		builder:          builder,
		toolAvailability: toolAvailability,
	}
	r.learning = &steps.LearningService{
		Builder:          builder,
		Delegate:         delegate,
		ToolAvailability: toolAvailability,
	}
	r.pipeline = r.defaultPipeline()
	return r
}

func (r *Runner) defaultPipeline() Pipeline {
	var sessionMessages lifecyclecontext.SessionMessageProvider
	if r != nil && r.builder != nil {
		sessionMessages = r.builder.SessionMessages
	}
	return Pipeline{
		steps.NormalizeStep{},
		steps.JournalStep{},
		steps.ModelStep{},
		steps.ContextStep{Builder: r.builder},
		steps.AuthorizeStep{},
		steps.StartEventStep{},
		steps.ArtifactBaselineStep{},
		steps.ExecuteStep{Delegate: r.delegate},
		steps.ArtifactReconcileStep{},
		steps.ArtifactStep{Recorder: r.artifactRecorder},
		steps.PersistStep{},
		steps.LearningStep{Service: r.learning},
		steps.SessionCompleteStep{Provider: sessionMessages},
	}
}

// SetArtifactRecorder configures manifest artifact recording for future runs.
func (r *Runner) SetArtifactRecorder(recorder steps.ArtifactRecorder) {
	if r == nil {
		return
	}
	r.artifactRecorder = recorder
	r.pipeline = r.defaultPipeline()
}

func (r *Runner) Run(ctx context.Context, req *agent.RequestContext) (result *agent.RunResult, runErr error) {
	startedAt := time.Now().UTC()
	state := &RunState{
		OriginalRequest: req,
		Request:         req,
		StartedAt:       startedAt,
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err, ok := recovered.(error)
			if !ok {
				err = fmt.Errorf("%v", recovered)
			}
			result, runErr = lifecyclejournal.EmitFailed(ctx, state.Journal, state.Request, lifecyclejournal.RunPhasePanic, fmt.Errorf("agent runtime panic: %w", err), nil)
		}
	}()

	if r == nil {
		return lifecyclejournal.EmitFailed(ctx, nil, req, lifecyclejournal.RunPhasePrepare, fmt.Errorf("lifecycle runner is required"), nil)
	}
	if r.delegate == nil {
		return lifecyclejournal.EmitFailed(ctx, nil, req, lifecyclejournal.RunPhasePrepare, fmt.Errorf("delegate runner is required"), nil)
	}
	if r.builder == nil {
		return lifecyclejournal.EmitFailed(ctx, nil, req, lifecyclejournal.RunPhasePrepare, fmt.Errorf("context builder is required"), nil)
	}

	logs.InfoContextf(ctx, "Agent lifecycle run started: run_id=%s trace_id=%s task_id=%s runtime=%s assistant_id=%s input_type=%s",
		requestRunID(req),
		requestTraceID(req),
		requestTaskID(req),
		requestRuntimeKind(req),
		requestAssistantID(req),
		requestInputType(req),
	)

	err := RunPipeline(ctx, r.pipeline, state)
	if state.Skipped {
		return nil, nil
	}
	if err != nil {
		phase := phaseForError(state, err)
		result, runErr = lifecyclejournal.EmitFailed(ctx, state.Journal, state.Request, phase, err, metadataFromResult(state.Result))
	} else {
		result, runErr = state.Result, state.Err
	}

	logs.InfoContextf(ctx, "Agent lifecycle run finished: run_id=%s trace_id=%s status=%s elapsed=%s",
		requestRunID(state.Request), requestTraceID(state.Request), resultStatus(result), time.Since(startedAt))
	return result, runErr
}

var _ agent.Runner = (*Runner)(nil)

func (r *Runner) BeforeCompact(ctx context.Context, req *agent.RequestContext) error {
	if r == nil || r.learning == nil {
		return nil
	}
	return r.learning.BeforeCompact(ctx, req)
}

func (r *Runner) BeforeReset(ctx context.Context, req *agent.RequestContext) error {
	if r == nil || r.learning == nil {
		return nil
	}
	return r.learning.BeforeReset(ctx, req)
}

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
