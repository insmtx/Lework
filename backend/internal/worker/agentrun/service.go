package agentrun

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

// Service is the single business entry point for an Agent Run.
type Service struct {
	preparer      Preparer
	executor      *agent.Executor
	finalizer     Finalizer
	journal       JournalFactory
	planPublisher PlanPublisher
	sessionStore  ProviderSessionStore
}

// NewService creates a new AgentRun Service.
func NewService(
	preparer Preparer,
	executor *agent.Executor,
	finalizer Finalizer,
	journal JournalFactory,
	planPublisher PlanPublisher,
) *Service {
	return NewServiceWithSessionStore(preparer, executor, finalizer, journal, planPublisher, nil)
}

// NewServiceWithSessionStore creates a new Service with ProviderSessionStore for resume support.
func NewServiceWithSessionStore(
	preparer Preparer,
	executor *agent.Executor,
	finalizer Finalizer,
	journal JournalFactory,
	planPublisher PlanPublisher,
	sessionStore ProviderSessionStore,
) *Service {
	return &Service{
		preparer:      preparer,
		executor:      executor,
		finalizer:     finalizer,
		journal:       journal,
		planPublisher: planPublisher,
		sessionStore:  sessionStore,
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
	req *agentrundomain.RunRequest,
	eventContext EventContext,
	publisher RunEventPublisher,
) (*agentrundomain.RunResult, error) {
	if s == nil {
		return nil, fmt.Errorf("agent run service is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("run request is required")
	}
	if s.preparer == nil || s.executor == nil || s.finalizer == nil || s.journal == nil {
		return nil, fmt.Errorf("agent run service dependencies are incomplete")
	}

	// 1. Clone and normalize.
	cloned := agentrundomain.CloneRequest(req)
	if cloned.RunID == "" {
		cloned.RunID = fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
	}
	if cloned.Input.Type == "" {
		cloned.Input.Type = agentrundomain.InputTypeMessage
	}

	// 2. Start journal and emit run.started.
	j := s.journal.New(cloned, eventContext, publisher)
	if j == nil {
		return nil, fmt.Errorf("journal factory returned nil journal")
	}
	startedAt := time.Now().UTC()
	if err := j.Record(ctx, RunEventDraft{
		OccurredAt: startedAt,
		Body:       runStartedEvent(),
	}); err != nil {
		return nil, fmt.Errorf("record run.started: %w", err)
	}

	resolvedRuntime, err := s.executor.ResolveRuntimeKind(cloned.Runtime.Kind)
	if err != nil {
		return s.finishError(ctx, cloned, nil, j, "resolve_runtime", err, startedAt)
	}
	cloned.Runtime.Kind = resolvedRuntime

	// 3. Prepare.
	prepared, err := s.preparer.Prepare(ctx, cloned)
	if err != nil {
		return s.finishError(ctx, cloned, nil, j, "prepare", err, startedAt)
	}

	// 4. Execute via agent.Executor.
	// Create event router that filters internal events (execution.*, plan.ready)
	// and forwards business events to the journal for Seq assignment.
	nodeHandler := NewNodeHandler(
		j,
		s.planPublisher,
		s.sessionStore,
		prepared.Execution.Runtime,
		prepared.Execution.SessionKey,
	)
	runtimeResult, err := s.executor.Execute(ctx, prepared.Execution, nodeHandler)
	if err != nil {
		return s.finishError(ctx, cloned, prepared, j, "execute", err, startedAt)
	}
	if err := nodeHandler.PlanError(); err != nil {
		return s.finishError(ctx, cloned, prepared, j, "plan_publish", err, startedAt)
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
		if err := j.Record(ctx, RunEventDraft{Body: event}); err != nil {
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
	ensureRunMetadataRuntime(finalized.Result, runMetadataRuntime(cloned, prepared))
	termEvent := newTerminalEvent(finalized.Result, messaging.RunEventRunCompleted, j)
	if err := j.Record(ctx, RunEventDraft{
		OccurredAt: finalized.Result.CompletedAt,
		Body:       termEvent,
	}); err != nil {
		return finalized.Result, fmt.Errorf("record run.completed: %w", err)
	}

	// 8. Post-run best effort.
	s.finalizer.PostRunBestEffort(ctx, prepared, finalized.Result, j.Snapshot())

	return finalized.Result, nil
}

// finishError handles any failure during prepare/execute/finalize.
func (s *Service) finishError(
	ctx context.Context,
	req *agentrundomain.RunRequest,
	prepared *PreparedRun,
	j Journal,
	phase string,
	runErr error,
	startedAt time.Time,
) (*agentrundomain.RunResult, error) {
	if runErr == nil {
		return nil, nil
	}

	status := agentrundomain.RunStatusFailed
	message := ""
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		status = agentrundomain.RunStatusCancelled
		message = "已取消"
	}

	snapshot := JournalSnapshot{}
	if j != nil {
		snapshot = j.Snapshot()
	}
	secrets := runErrorSecrets(req, prepared)
	safeError := redactSecrets(runErr.Error(), secrets)
	result := &agentrundomain.RunResult{
		RunID:       req.RunID,
		TraceID:     req.TraceID,
		Status:      status,
		Message:     message,
		Error:       safeError,
		Usage:       snapshot.Usage,
		ToolCalls:   append([]agentrundomain.ToolCallRecord(nil), snapshot.ToolCalls...),
		StartedAt:   startedAt,
		CompletedAt: time.Now().UTC(),
		Metadata: &messaging.RunMetadataPayload{
			Phase:   phase,
			Runtime: runMetadataRuntime(req, prepared),
		},
	}

	eventType := messaging.RunEventRunFailed
	if status == agentrundomain.RunStatusCancelled {
		eventType = messaging.RunEventRunCancelled
	}

	termEvent := newTerminalEvent(result, eventType, j)
	var terminalErr error
	if j != nil {
		terminalErr = j.Record(ctx, RunEventDraft{
			OccurredAt: result.CompletedAt,
			Body:       termEvent,
		})
	}

	// Post-run best effort (do not modify result).
	if s != nil && s.finalizer != nil {
		s.finalizer.PostRunBestEffort(ctx, prepared, result, snapshot)
	}

	safeRunErr := redactError(runErr, secrets)
	if terminalErr != nil {
		return result, errors.Join(safeRunErr, fmt.Errorf("record terminal event: %w", terminalErr))
	}
	return result, safeRunErr
}

type redactedError struct {
	message string
	cause   error
}

func (e redactedError) Error() string { return e.message }

func (e redactedError) Unwrap() error { return e.cause }

func redactError(err error, secrets []string) error {
	if err == nil {
		return nil
	}
	message := redactSecrets(err.Error(), secrets)
	if message == err.Error() {
		return err
	}
	return redactedError{message: message, cause: err}
}

func runMetadataRuntime(req *agentrundomain.RunRequest, prepared *PreparedRun) string {
	if prepared != nil {
		if runtime := strings.TrimSpace(prepared.Execution.Runtime); runtime != "" {
			return runtime
		}
	}
	if req == nil {
		return ""
	}
	return strings.TrimSpace(req.Runtime.Kind)
}

func ensureRunMetadataRuntime(result *agentrundomain.RunResult, runtime string) {
	if result == nil {
		return
	}
	runtime = strings.TrimSpace(runtime)
	if runtime == "" {
		return
	}
	if result.Metadata == nil {
		result.Metadata = &messaging.RunMetadataPayload{}
	}
	if strings.TrimSpace(result.Metadata.Runtime) == "" {
		result.Metadata.Runtime = runtime
	}
}

func runErrorSecrets(req *agentrundomain.RunRequest, prepared *PreparedRun) []string {
	secrets := make([]string, 0, 2)
	if req != nil {
		secrets = appendSecret(secrets, req.Model.APIKey)
	}
	if prepared != nil {
		secrets = appendSecret(secrets, prepared.Execution.Model.APIKey)
	}
	return secrets
}

func appendSecret(secrets []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return secrets
	}
	for _, existing := range secrets {
		if existing == value {
			return secrets
		}
	}
	return append(secrets, value)
}

func redactSecrets(text string, secrets []string) string {
	if text == "" || len(secrets) == 0 {
		return text
	}
	redacted := text
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		redacted = strings.ReplaceAll(redacted, secret, "[REDACTED]")
	}
	return redacted
}

// runStartedEvent creates an unsequenced run.started body.
func runStartedEvent() messaging.RunEventBody {
	return messaging.RunEventBody{
		Event: messaging.RunEventRunStarted,
	}
}

// newTerminalEvent creates an unsequenced terminal RunEvent body.
func newTerminalEvent(
	result *agentrundomain.RunResult,
	eventType messaging.RunEventType,
	j Journal,
) messaging.RunEventBody {
	if result == nil {
		return messaging.RunEventBody{Event: eventType}
	}
	completed := &messaging.RunCompletedPayload{
		Status:      string(result.Status),
		Result:      messaging.RunResultPayload{Message: result.Message},
		Usage:       runUsageToMessaging(result.Usage),
		Artifacts:   append([]messaging.ArtifactPayload(nil), result.Artifacts...),
		StartedAt:   result.StartedAt.Format(time.RFC3339Nano),
		CompletedAt: result.CompletedAt.Format(time.RFC3339Nano),
		Metadata:    result.Metadata,
	}
	if j != nil {
		snap := j.Snapshot()
		completed.Events = append([]messaging.RunEventRecord(nil), snap.Events...)
	}

	body := messaging.RunEventBody{
		Event:        eventType,
		RunCompleted: completed,
		Payload: messaging.RunEventPayload{
			Content: result.Message,
		},
	}
	if eventType == messaging.RunEventRunFailed || eventType == messaging.RunEventRunCancelled {
		errorMessage := result.Error
		if errorMessage == "" {
			errorMessage = result.Message
		}
		body.Error = &messaging.RunEventError{Message: errorMessage}
	}
	return body
}
