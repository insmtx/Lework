// Package run provides the cmd.run lane handler for worker agent run commands.
//
// Handler is a NATS adapter: it decodes WorkerCommands, validates routes/payloads,
// tracks NATS delivery sequences, and delegates scheduling + execution to the
// RunCoordinator. Workspace preparation, attachment ingestion, and git operations
// are owned by the assistant layer.
package run

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/internal/assistant"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/worker/eventpub"
	runcoord "github.com/insmtx/Leros/backend/internal/worker/run"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/pkg/seqtracker"
	"github.com/nats-io/nats.go"
	"github.com/ygpkg/yg-go/logs"
)

// Config controls a worker run handler.
type Config struct {
	OrgID          uint
	WorkerID       uint
	Env            string
	MaxConcurrency int           // passed to Coordinator
	DebounceWindow time.Duration // passed to Coordinator
	SeqTrackerPath string        // path to SQLite seq tracker database
}

// runTask is the internal expanded task representation.
type runTask struct {
	ID           string
	CreatedAt    time.Time
	Trace        messaging.TraceContext
	Route        messaging.RouteContext
	DeliverySeqs []uint64

	TaskType      messaging.TaskType
	ExecutionMode string
	Actor         messaging.ActorContext
	Execution     messaging.ExecutionTarget
	Workspace     messaging.WorkspaceOptions
	Input         messaging.TaskInput
	Model         messaging.ModelOptions
	Runtime       messaging.RuntimeOptions
	Policy        messaging.TaskPolicy
}

// Handler receives run commands and delegates to the RunCoordinator.
type Handler struct {
	cfg         Config
	publisher   eventbus.Publisher
	coordinator *runcoord.Coordinator
	seqTracker  seqtracker.SeqTracker
}

// New creates a worker run handler backed by the assistant Service through a Coordinator.
func New(
	cfg Config,
	pub eventbus.Publisher,
	assistantSvc *assistant.Service,
) (*Handler, error) {
	if cfg.OrgID == 0 {
		return nil, fmt.Errorf("worker org_id is required")
	}
	if cfg.WorkerID == 0 {
		return nil, fmt.Errorf("worker worker_id is required")
	}
	if pub == nil {
		return nil, fmt.Errorf("publisher is required")
	}
	if assistantSvc == nil {
		return nil, fmt.Errorf("assistant service is required")
	}

	maxConc := cfg.MaxConcurrency
	if maxConc <= 0 {
		maxConc = 20
	}
	window := cfg.DebounceWindow
	if window <= 0 {
		window = 1500 * time.Millisecond
	}

	var tracker seqtracker.SeqTracker
	if strings.TrimSpace(cfg.SeqTrackerPath) != "" {
		var err error
		tracker, err = seqtracker.NewSQLiteTracker(cfg.SeqTrackerPath)
		if err != nil {
			return nil, fmt.Errorf("create seq tracker: %w", err)
		}
	}

	h := &Handler{
		cfg:        cfg,
		publisher:  pub,
		seqTracker: tracker,
	}

	// The executeFunc builds the event sink from the submission context and calls assistant.Service.
	coord, err := runcoord.NewCoordinator(runcoord.Config{
		MaxConcurrency: maxConc,
		DebounceWindow: window,
	}, h.executeSubmission(assistantSvc))
	if err != nil {
		return nil, err
	}
	h.coordinator = coord
	return h, nil
}

// executeSubmission returns an ExecuteFunc that wraps assistant.Service.Run.
func (h *Handler) executeSubmission(svc *assistant.Service) runcoord.ExecuteFunc {
	return func(ctx context.Context, sub runcoord.RunSubmission, sink agent.EventSink) (*assistantdomain.RunResult, error) {
		ec := sub.EventContext

		// Build NATSEventSink if no external sink was provided.
		if sink == nil {
			sink = eventpub.NewNATSEventSink(h.publisher, eventpub.RunEventContext{
				OrgID:             ec.OrgID,
				WorkerID:          ec.WorkerID,
				SessionID:         ec.SessionID,
				TraceID:           ec.TraceID,
				RequestID:         ec.RequestID,
				TaskID:            ec.TaskID,
				RunID:             ec.RunID,
				ParentID:          ec.ParentID,
				ReplyToMessageIDs: ec.ReplyToMessageIDs,
			})
		}

		if sub.Request == nil {
			return nil, fmt.Errorf("submission request is nil")
		}

		logs.InfoContextf(ctx,
			"Starting worker task run: task_id=%s run_id=%s runtime=%s assistant_id=%s",
			sub.Request.TaskID,
			sub.Request.RunID,
			sub.Request.Runtime.Kind,
			sub.Request.Assistant.ID,
		)

		return svc.Run(ctx, sub.Request, sink)
	}
}

// RunSubject returns the NATS subject for this handler's cmd.run lane.
func (h *Handler) RunSubject() string {
	topic, err := messaging.WorkerCommandSubject(h.cfg.OrgID, h.cfg.WorkerID, messaging.LaneRun)
	if err != nil {
		logs.Errorf("Failed to get worker task topic for org_id=%d worker_id=%d: %v", h.cfg.OrgID, h.cfg.WorkerID, err)
	}
	return topic
}

// HandleRunCommand handles run commands from the command dispatcher.
//
// It decodes the payload, validates routing, tracks sequences, builds a
// RunSubmission, and submits to the Coordinator for scheduling+execution.
func (h *Handler) HandleRunCommand(ctx context.Context, cmd messaging.WorkerCommand, msg *nats.Msg) error {
	payload, err := messaging.DecodeCommandPayload[messaging.RunCommandPayload](&cmd.Body)
	if err != nil {
		return fmt.Errorf("run command payload decode: %w", err)
	}

	task := runTask{
		ID:            cmd.ID,
		CreatedAt:     cmd.CreatedAt,
		Trace:         cmd.Trace,
		Route:         cmd.Route,
		TaskType:      payload.TaskType,
		ExecutionMode: payload.ExecutionMode,
		Actor:         payload.Actor,
		Execution:     payload.Execution,
		Workspace:     payload.Workspace,
		Input:         payload.Input,
		Model:         payload.Model,
		Runtime:       payload.Runtime,
		Policy:        payload.Policy,
	}

	// Validate route.
	if err := h.validateRouteTask(task); err != nil {
		return err
	}
	if task.TaskType != messaging.TaskTypeAgentRun {
		return fmt.Errorf("unsupported task type %q", task.TaskType)
	}
	if err := validateModelConfig(task.Model); err != nil {
		return err
	}

	// Track seq for crash recovery from NATS metadata.
	var seq uint64
	if meta, err := msg.Metadata(); err == nil {
		seq = meta.Sequence.Stream
	}
	topic := h.RunSubject()
	if h.seqTracker != nil {
		if isTerminal, err := h.seqTracker.IsTerminal(ctx, topic, seq); err == nil && isTerminal {
			logs.InfoContextf(ctx, "Skipping terminal run command: topic=%s seq=%d", topic, seq)
			return nil
		}
		_ = h.seqTracker.TrackReceived(ctx, topic, seq,
			task.Route.SessionID, task.ID, task.Trace.TaskID, task.Trace.RunID)
	}

	if seq != 0 {
		task.DeliverySeqs = []uint64{seq}
	}

	logs.InfoContextf(ctx,
		"Received run command: msg_id=%s task_id=%s run_id=%s org_id=%d worker_id=%d session_id=%s task_type=%s seq=%d",
		task.ID, task.Trace.TaskID, task.Trace.RunID,
		task.Route.OrgID, task.Route.WorkerID, task.Route.SessionID,
		task.TaskType, seq,
	)

	// Mark seqs as processing.
	seqs := task.DeliverySeqs
	for _, s := range seqs {
		if h.seqTracker != nil {
			_ = h.seqTracker.MarkProcessing(ctx, topic, s)
		}
	}

	// Build submission and delegate to coordinator.
	req := RequestFromWorkerTask(task)
	submission := runcoord.RunSubmission{
		Request: req,
		EventContext: runcoord.RunEventContext{
			OrgID:             task.Route.OrgID,
			WorkerID:          task.Route.WorkerID,
			SessionID:         task.Route.SessionID,
			TraceID:           task.Trace.TraceID,
			RequestID:         task.Trace.RequestID,
			TaskID:            task.Trace.TaskID,
			RunID:             task.Trace.RunID,
			ParentID:          task.Trace.ParentID,
			ReplyToMessageIDs: replyToMessageIDs(task.Input.Messages),
		},
		DeliverySeqs: seqs,
	}

	_, execErr := h.coordinator.Submit(ctx, submission)

	// Mark seqs as completed/failed.
	for _, s := range seqs {
		if h.seqTracker != nil {
			if execErr != nil {
				_ = h.seqTracker.MarkFailed(ctx, topic, s, execErr.Error())
			} else {
				_ = h.seqTracker.MarkCompleted(ctx, topic, s)
			}
		}
	}

	return execErr
}

func replyToMessageIDs(messages []messaging.ChatMessage) []string {
	ids := make([]string, 0, len(messages))
	seen := make(map[string]struct{}, len(messages))
	for _, message := range messages {
		id := strings.TrimSpace(message.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// HandleControlCommand handles control commands (cancel) from the command dispatcher.
func (h *Handler) HandleControlCommand(ctx context.Context, cmd messaging.WorkerCommand) error {
	switch cmd.Body.CommandType {
	case messaging.CommandTypeCancel:
		payload, err := messaging.DecodeCommandPayload[messaging.CancelRunCommandPayload](&cmd.Body)
		if err != nil {
			logs.WarnContextf(ctx, "Failed to decode cancel payload: %v", err)
			return err
		}
		h.coordinator.Cancel(ctx, cmd.Route.OrgID, cmd.Route.WorkerID, cmd.Route.SessionID, payload.RunID)
	default:
		logs.WarnContextf(ctx, "unknown control command type: %s", cmd.Body.CommandType)
	}
	return nil
}

// Close shuts down the handler gracefully, waiting for all in-flight runs.
func (h *Handler) Close() error {
	if h.coordinator != nil {
		_ = h.coordinator.Close()
	}
	if h.seqTracker != nil {
		return h.seqTracker.Close()
	}
	return nil
}

func (h *Handler) validateRouteTask(task runTask) error {
	if task.Route.OrgID != 0 && task.Route.OrgID != h.cfg.OrgID {
		return fmt.Errorf("task org_id %d does not match worker org_id %d", task.Route.OrgID, h.cfg.OrgID)
	}
	if task.Route.WorkerID != 0 && task.Route.WorkerID != h.cfg.WorkerID {
		return fmt.Errorf("task worker_id %d does not match worker_id %d", task.Route.WorkerID, h.cfg.WorkerID)
	}
	return nil
}

func validateModelConfig(model messaging.ModelOptions) error {
	if strings.TrimSpace(model.Provider) == "" {
		return fmt.Errorf("llm provider is required")
	}
	if strings.TrimSpace(model.Model) == "" {
		return fmt.Errorf("llm model is required")
	}
	if strings.TrimSpace(model.APIKey) == "" {
		return fmt.Errorf("llm api_key is required")
	}
	return nil
}
