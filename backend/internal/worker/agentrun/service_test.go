package agentrun

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	modelrouter "github.com/insmtx/Leros/backend/internal/modelrouter"
	agentruncontext "github.com/insmtx/Leros/backend/internal/worker/agentrun/context"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

type preparerFunc func(context.Context, *agentrundomain.RunRequest) (*PreparedRun, error)

func (f preparerFunc) Prepare(
	ctx context.Context,
	req *agentrundomain.RunRequest,
) (*PreparedRun, error) {
	return f(ctx, req)
}

type runtimeFunc func(
	context.Context,
	agent.ExecutionRequest,
	agent.NodeObserver,
) (agent.ExecutionResult, error)

func (runtimeFunc) Name() string { return "test" }

func (f runtimeFunc) Execute(
	ctx context.Context,
	request agent.ExecutionRequest,
	observer agent.NodeObserver,
) (agent.ExecutionResult, error) {
	return f(ctx, request, observer)
}

type finalizerStub struct {
	result *agentrundomain.RunResult
	events []messaging.RunEventBody
	err    error
}

func (f finalizerStub) FinalizeRequired(
	_ context.Context,
	_ *PreparedRun,
	_ *agent.ExecutionResult,
	_ JournalSnapshot,
) (*Finalization, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &Finalization{Result: f.result, Events: f.events}, nil
}

func (finalizerStub) PostRunBestEffort(
	context.Context,
	*PreparedRun,
	*agentrundomain.RunResult,
	JournalSnapshot,
) {
}

type runEventRecorder struct {
	events []messaging.RunEvent
}

func (r *runEventRecorder) PublishRunEvent(
	_ context.Context,
	event messaging.RunEvent,
) error {
	r.events = append(r.events, event)
	return nil
}

type providerSessionRecorder struct {
	getKey  ProviderSessionKey
	resume  *ProviderSessionBinding
	binding *ProviderSessionBinding
}

func (s *providerSessionRecorder) GetProviderSession(
	_ context.Context,
	key ProviderSessionKey,
) (*ProviderSessionBinding, error) {
	s.getKey = key
	if s.resume == nil {
		return nil, nil
	}
	copied := *s.resume
	return &copied, nil
}

func (s *providerSessionRecorder) UpsertProviderSession(
	_ context.Context,
	binding *ProviderSessionBinding,
) error {
	if binding == nil {
		s.binding = nil
		return nil
	}
	copied := *binding
	s.binding = &copied
	return nil
}

func (s *providerSessionRecorder) MarkProviderSessionFailed(
	context.Context,
	ProviderSessionKey,
	string,
) error {
	return nil
}

func testEventContext(runID string) EventContext {
	return EventContext{
		OrgID: 1, WorkerID: 2, SessionID: "session-1",
		TraceID: "trace-1", RequestID: "request-1",
		TaskID: "task-1", RunID: runID,
		ReplyToMessageIDs: []string{"message-1"},
	}
}

func TestServiceRunEmitsOneTerminalAndPreservesInput(t *testing.T) {
	registry := agent.NewRegistry()
	registry.Register("test", runtimeFunc(func(
		ctx context.Context,
		_ agent.ExecutionRequest,
		observer agent.NodeObserver,
	) (agent.ExecutionResult, error) {
		if err := observer.Observe(ctx, agent.NodeEvent{
			Type: agent.NodeEventMessageUpdate,
			Payload: &agent.MessageUpdatePayload{
				MessageID: "m1", Role: "assistant", Content: "hello",
			},
		}); err != nil {
			return agent.ExecutionResult{}, err
		}
		return agent.ExecutionResult{
			Message: "done",
			Usage:   &agent.Usage{InputTokens: 1, OutputTokens: 2},
		}, nil
	}))
	registry.SetDefault("test")

	input := &agentrundomain.RunRequest{
		RunID: "run-1",
		Input: agentrundomain.InputContext{
			Type:     agentrundomain.InputTypeMessage,
			Messages: []agentrundomain.InputMessage{{Role: "user", Content: "original"}},
		},
	}
	service := NewService(
		preparerFunc(func(
			_ context.Context,
			req *agentrundomain.RunRequest,
		) (*PreparedRun, error) {
			req.Input.Messages[0].Content = "prepared"
			return &PreparedRun{
				Request: req,
				Execution: agent.ExecutionRequest{
					ExecutionID: req.RunID,
					Runtime:     "test",
				},
			}, nil
		}),
		agent.NewExecutor(registry),
		finalizerStub{
			result: &agentrundomain.RunResult{
				RunID:     "run-1",
				Status:    agentrundomain.RunStatusCompleted,
				Message:   "done",
				Usage:     &agentrundomain.Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3},
				Artifacts: []messaging.ArtifactPayload{{ArtifactID: "artifact-1", Title: "report"}},
			},
			events: []messaging.RunEventBody{{
				Event: messaging.RunEventArtifactDeclared,
				Payload: messaging.RunEventPayload{
					Artifact: &messaging.ArtifactPayload{ArtifactID: "artifact-1"},
				},
			}},
		},
		NewJournalFactory(),
		nil,
	)
	recorder := &runEventRecorder{}
	result, err := service.Run(
		context.Background(),
		input,
		testEventContext("run-1"),
		recorder,
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if input.Input.Messages[0].Content != "original" {
		t.Fatalf("input was mutated: %#v", input.Input.Messages)
	}
	if result.StartedAt.IsZero() || result.CompletedAt.IsZero() ||
		result.CompletedAt.Before(result.StartedAt) {
		t.Fatalf("invalid timestamps: started=%s completed=%s", result.StartedAt, result.CompletedAt)
	}

	var terminal *messaging.RunEvent
	for index := range recorder.events {
		event := &recorder.events[index]
		if isTerminalRunEvent(event.Body.Event) {
			if terminal != nil {
				t.Fatalf("multiple terminal events: %#v", recorder.events)
			}
			terminal = event
		}
		if event.Body.Seq != int64(index+1) {
			t.Fatalf("event seq = %d at index %d", event.Body.Seq, index)
		}
	}
	if terminal == nil || terminal.Body.Event != messaging.RunEventRunCompleted {
		t.Fatalf("terminal event = %#v", terminal)
	}
	if terminal.Body.RunCompleted == nil ||
		len(terminal.Body.RunCompleted.Events) != 3 {
		t.Fatalf("terminal archive = %#v, want started, delta, artifact", terminal.Body.RunCompleted)
	}
	if terminal.Route.SessionID != "session-1" ||
		terminal.Trace.RequestID != "request-1" {
		t.Fatalf("terminal context = %#v %#v", terminal.Route, terminal.Trace)
	}
}

func TestServiceRunCancellationSeparatesMessageAndError(t *testing.T) {
	registry := agent.NewRegistry()
	registry.Register("test", runtimeFunc(func(
		context.Context,
		agent.ExecutionRequest,
		agent.NodeObserver,
	) (agent.ExecutionResult, error) {
		return agent.ExecutionResult{}, context.Canceled
	}))
	registry.SetDefault("test")
	service := NewService(
		preparerFunc(func(
			_ context.Context,
			req *agentrundomain.RunRequest,
		) (*PreparedRun, error) {
			return &PreparedRun{
				Request: req,
				Execution: agent.ExecutionRequest{
					ExecutionID: req.RunID,
					Runtime:     "test",
				},
			}, nil
		}),
		agent.NewExecutor(registry),
		finalizerStub{},
		NewJournalFactory(),
		nil,
	)
	recorder := &runEventRecorder{}
	result, err := service.Run(
		context.Background(),
		&agentrundomain.RunRequest{RunID: "run-cancel"},
		testEventContext("run-cancel"),
		recorder,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if result.Message != "已取消" || result.Error != context.Canceled.Error() {
		t.Fatalf("result = %#v", result)
	}
	terminal := recorder.events[len(recorder.events)-1]
	if terminal.Body.Event != messaging.RunEventRunCancelled ||
		terminal.Body.Payload.Content != "已取消" ||
		terminal.Body.Error == nil ||
		terminal.Body.Error.Message != context.Canceled.Error() {
		t.Fatalf("terminal event = %#v", terminal)
	}
}

func TestServiceRunRedactsAPIKeyFromFailureEventsAndProviderSession(t *testing.T) {
	const apiKey = "sk-secret-runtime-key"
	registry := agent.NewRegistry()
	registry.Register("test", runtimeFunc(func(
		ctx context.Context,
		request agent.ExecutionRequest,
		observer agent.NodeObserver,
	) (agent.ExecutionResult, error) {
		if request.Model.APIKey != apiKey {
			t.Fatalf("runtime API key = %q, want %q", request.Model.APIKey, apiKey)
		}
		if err := observer.Observe(ctx, agent.NodeEvent{
			ExecutionID: request.ExecutionID,
			TraceID:     request.TraceID,
			Type:        agent.NodeEventAgentStart,
			Payload:     &agent.AgentStartedPayload{ProviderSessionID: "provider-session-1"},
		}); err != nil {
			return agent.ExecutionResult{}, err
		}
		if err := observer.Observe(ctx, agent.NodeEvent{
			ExecutionID: request.ExecutionID,
			TraceID:     request.TraceID,
			Type:        agent.NodeEventMessageUpdate,
			Payload: &agent.MessageUpdatePayload{
				MessageID: "m1", Role: "assistant", Content: "safe content",
			},
			Metadata: agent.NodeEventMetadata{"engine": "test"},
		}); err != nil {
			return agent.ExecutionResult{}, err
		}
		return agent.ExecutionResult{}, errors.New("provider rejected key " + apiKey)
	}))
	registry.SetDefault("test")

	input := &agentrundomain.RunRequest{
		RunID:   "run-redact",
		TraceID: "trace-redact",
		Model: agentrundomain.ModelOptions{
			Provider: "test-provider",
			Model:    "test-model",
			APIKey:   apiKey,
		},
	}
	sessionStore := &providerSessionRecorder{}
	service := NewServiceWithSessionStore(
		preparerFunc(func(
			_ context.Context,
			req *agentrundomain.RunRequest,
		) (*PreparedRun, error) {
			return &PreparedRun{
				Request: req,
				Execution: agent.ExecutionRequest{
					ExecutionID: req.RunID,
					TraceID:     req.TraceID,
					Runtime:     "test",
					SessionKey:  "session-redact",
					Model: agent.ModelConfig{
						Provider: req.Model.Provider,
						Model:    req.Model.Model,
						APIKey:   req.Model.APIKey,
					},
				},
			}, nil
		}),
		agent.NewExecutor(registry),
		finalizerStub{},
		NewJournalFactory(),
		nil,
		sessionStore,
	)
	recorder := &runEventRecorder{}
	result, err := service.Run(context.Background(), input, testEventContext("run-redact"), recorder)
	if err == nil {
		t.Fatal("Run() error = nil")
	}
	if strings.Contains(err.Error(), apiKey) {
		t.Fatalf("returned error leaked API key: %q", err.Error())
	}
	if result == nil || strings.Contains(result.Error, apiKey) {
		t.Fatalf("result leaked API key: %#v", result)
	}
	if result.Metadata == nil || result.Metadata.Runtime != "test" {
		t.Fatalf("result metadata = %#v, want runtime test", result.Metadata)
	}
	if sessionStore.binding == nil {
		t.Fatal("provider session was not stored")
	}
	if sessionStore.binding.InternalSessionID != "session-redact" ||
		sessionStore.binding.Provider != "test" ||
		sessionStore.binding.ProviderSessionID != "provider-session-1" {
		t.Fatalf("provider session binding = %#v", sessionStore.binding)
	}
	bindingJSON, err := json.Marshal(sessionStore.binding)
	if err != nil {
		t.Fatalf("marshal binding: %v", err)
	}
	if strings.Contains(string(bindingJSON), apiKey) {
		t.Fatalf("provider session binding leaked API key: %s", bindingJSON)
	}
	eventsJSON, err := json.Marshal(recorder.events)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	if strings.Contains(string(eventsJSON), apiKey) {
		t.Fatalf("run events leaked API key: %s", eventsJSON)
	}
	var terminal *messaging.RunEvent
	for index := range recorder.events {
		event := &recorder.events[index]
		if isTerminalRunEvent(event.Body.Event) {
			terminal = event
		}
	}
	if terminal == nil || terminal.Body.Event != messaging.RunEventRunFailed {
		t.Fatalf("terminal event = %#v", terminal)
	}
	if terminal.Body.Error == nil || !strings.Contains(terminal.Body.Error.Message, "[REDACTED]") {
		t.Fatalf("terminal error was not redacted: %#v", terminal.Body.Error)
	}
	if terminal.Body.RunCompleted == nil || len(terminal.Body.RunCompleted.Events) != 2 {
		t.Fatalf("terminal archive = %#v, want started and message delta", terminal.Body.RunCompleted)
	}
	if terminal.Body.RunCompleted.Metadata == nil || terminal.Body.RunCompleted.Metadata.Runtime != "test" {
		t.Fatalf("terminal metadata = %#v, want runtime test", terminal.Body.RunCompleted.Metadata)
	}
}

func TestServiceRunResolvesDefaultRuntimeBeforeProviderSessionLookup(t *testing.T) {
	sessionStore := &providerSessionRecorder{
		resume: &ProviderSessionBinding{
			InternalSessionID: "conversation-1",
			Provider:          "test",
			ProviderSessionID: "provider-session-1",
			Status:            "active",
		},
	}
	registry := agent.NewRegistry()
	registry.Register("test", runtimeFunc(func(
		ctx context.Context,
		request agent.ExecutionRequest,
		observer agent.NodeObserver,
	) (agent.ExecutionResult, error) {
		if request.Runtime != "test" {
			t.Fatalf("execution runtime = %q, want test", request.Runtime)
		}
		if request.ProviderSession.ID != "provider-session-1" || !request.ProviderSession.Resume {
			t.Fatalf("execution provider session = %#v", request.ProviderSession)
		}
		if err := observer.Observe(ctx, agent.NodeEvent{
			ExecutionID: request.ExecutionID,
			TraceID:     request.TraceID,
			Type:        agent.NodeEventAgentStart,
			Payload:     &agent.AgentStartedPayload{ProviderSessionID: "provider-session-2"},
		}); err != nil {
			return agent.ExecutionResult{}, err
		}
		return agent.ExecutionResult{Message: "resumed"}, nil
	}))
	registry.SetDefault("test")

	preparer := NewPreparerWithSessionStore(
		agentruncontext.NewContextBuilder(agentruncontext.ContextBuilder{}),
		&workspaceManagerStub{preparation: WorkspacePreparation{WorkDir: "/workspace/repo"}},
		nil,
		modelrouter.NewModelStore(),
		nil,
		sessionStore,
	)
	service := NewServiceWithSessionStore(
		preparer,
		agent.NewExecutor(registry),
		finalizerStub{result: &agentrundomain.RunResult{
			RunID:   "run-resume",
			Status:  agentrundomain.RunStatusCompleted,
			Message: "resumed",
		}},
		NewJournalFactory(),
		nil,
		sessionStore,
	)

	recorder := &runEventRecorder{}
	result, err := service.Run(
		context.Background(),
		&agentrundomain.RunRequest{
			RunID: "run-resume",
			Conversation: agentrundomain.ConversationContext{
				ID: "conversation-1",
			},
			Input: agentrundomain.InputContext{
				Type:     agentrundomain.InputTypeMessage,
				Messages: []agentrundomain.InputMessage{{Role: "user", Content: "continue"}},
			},
			Model: agentrundomain.ModelOptions{
				Provider: "openai",
				Model:    "test-model",
				APIKey:   "test-key",
			},
		},
		testEventContext("run-resume"),
		recorder,
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Metadata == nil || result.Metadata.Runtime != "test" {
		t.Fatalf("result metadata = %#v, want runtime test", result.Metadata)
	}
	if sessionStore.getKey.InternalSessionID != "conversation-1" || sessionStore.getKey.Provider != "test" {
		t.Fatalf("provider session lookup key = %#v", sessionStore.getKey)
	}
	if sessionStore.binding == nil {
		t.Fatal("provider session was not stored")
	}
	if sessionStore.binding.InternalSessionID != "conversation-1" ||
		sessionStore.binding.Provider != "test" ||
		sessionStore.binding.ProviderSessionID != "provider-session-2" {
		t.Fatalf("provider session binding = %#v", sessionStore.binding)
	}
	terminal := recorder.events[len(recorder.events)-1]
	if terminal.Body.RunCompleted == nil || terminal.Body.RunCompleted.Metadata == nil ||
		terminal.Body.RunCompleted.Metadata.Runtime != "test" {
		t.Fatalf("terminal metadata = %#v, want runtime test", terminal.Body.RunCompleted)
	}
}

func TestServiceRunClassifiesPlanPublishFailureAsBusinessPhase(t *testing.T) {
	registry := agent.NewRegistry()
	registry.Register("test", runtimeFunc(func(
		ctx context.Context,
		request agent.ExecutionRequest,
		observer agent.NodeObserver,
	) (agent.ExecutionResult, error) {
		if err := observer.Observe(ctx, agent.NodeEvent{
			ExecutionID: request.ExecutionID,
			TraceID:     request.TraceID,
			Type:        agent.NodeEventPlanReady,
			Payload: &agent.PlanReadyPayload{
				Path:              "/workspace/PLAN.md",
				ProviderSessionID: "provider-session-1",
			},
		}); err != nil {
			return agent.ExecutionResult{}, err
		}
		return agent.ExecutionResult{Message: "runtime completed"}, nil
	}))
	registry.SetDefault("test")
	planErr := &PlanPublishError{Phase: "upload", Err: errors.New("object storage unavailable")}
	service := NewService(
		preparerFunc(func(
			_ context.Context,
			req *agentrundomain.RunRequest,
		) (*PreparedRun, error) {
			return &PreparedRun{
				Request: req,
				Execution: agent.ExecutionRequest{
					ExecutionID: req.RunID,
					TraceID:     req.TraceID,
					Runtime:     "test",
				},
			}, nil
		}),
		agent.NewExecutor(registry),
		finalizerStub{
			result: &agentrundomain.RunResult{
				RunID:   "run-plan-failed",
				Status:  agentrundomain.RunStatusCompleted,
				Message: "should not finalize",
			},
		},
		NewJournalFactory(),
		&planPublisherStub{err: planErr},
	)
	recorder := &runEventRecorder{}
	result, err := service.Run(
		context.Background(),
		&agentrundomain.RunRequest{RunID: "run-plan-failed", TraceID: "trace-plan-failed"},
		testEventContext("run-plan-failed"),
		recorder,
	)
	if !errors.Is(err, planErr) {
		t.Fatalf("Run() error = %v, want plan publish error", err)
	}
	if result == nil || result.Metadata == nil || result.Metadata.Phase != "plan_publish" {
		t.Fatalf("result metadata = %#v", result)
	}
	terminal := recorder.events[len(recorder.events)-1]
	if terminal.Body.Event != messaging.RunEventRunFailed {
		t.Fatalf("terminal event = %#v", terminal)
	}
	if terminal.Body.RunCompleted == nil || terminal.Body.RunCompleted.Metadata == nil ||
		terminal.Body.RunCompleted.Metadata.Phase != "plan_publish" {
		t.Fatalf("terminal metadata = %#v", terminal.Body.RunCompleted)
	}
}

func TestServiceRejectsNilRequestAndIncompleteDependencies(t *testing.T) {
	if _, err := (&Service{}).Run(
		context.Background(),
		&agentrundomain.RunRequest{},
		EventContext{},
		nil,
	); err == nil {
		t.Fatal("Run() error = nil for incomplete service")
	}
	service := NewService(
		preparerFunc(nil),
		&agent.Executor{},
		finalizerStub{},
		NewJournalFactory(),
		nil,
	)
	if _, err := service.Run(
		context.Background(),
		nil,
		EventContext{},
		nil,
	); err == nil {
		t.Fatal("Run() error = nil for nil request")
	}
}

func TestJournalArchivesPayloadUsageAndToolResults(t *testing.T) {
	recorder := &runEventRecorder{}
	journal := NewJournal(
		&agentrundomain.RunRequest{RunID: "run-1", TraceID: "trace-1"},
		testEventContext("run-1"),
		recorder,
	)
	handler := NewNodeHandler(journal, nil, nil, "test", "")
	now := time.Now().UTC()
	events := []agent.NodeEvent{
		{
			Type:       agent.NodeEventMessageUpdate,
			OccurredAt: now,
			Payload: &agent.MessageUpdatePayload{
				MessageID: "m1", Role: "assistant", Content: "a",
			},
		},
		{
			Type:       agent.NodeEventMessageUpdate,
			OccurredAt: now.Add(time.Millisecond),
			Payload: &agent.MessageUpdatePayload{
				MessageID: "m1", Role: "assistant", Content: "b",
			},
		},
		{
			Type: agent.NodeEventToolExecutionEnd,
			Payload: &agent.ToolExecutionEndPayload{
				ToolCallID: "t1", Name: "read",
				Result: json.RawMessage(`{"ok":true}`),
			},
		},
		{
			Type: agent.NodeEventMessageEnd,
			Payload: &agent.MessageEndPayload{
				MessageID: "m1", Content: "done",
				Usage: &agent.Usage{InputTokens: 4, OutputTokens: 5},
			},
		},
	}
	for _, event := range events {
		if err := handler.Observe(context.Background(), event); err != nil {
			t.Fatalf("Observe() error = %v", err)
		}
	}
	snapshot := journal.Snapshot()
	if snapshot.Usage == nil || snapshot.Usage.TotalTokens != 9 {
		t.Fatalf("usage = %#v", snapshot.Usage)
	}
	if len(snapshot.ToolCalls) != 1 || snapshot.ToolCalls[0].CallID != "t1" {
		t.Fatalf("tool calls = %#v", snapshot.ToolCalls)
	}
	if len(snapshot.Events) != 3 {
		t.Fatalf("events = %#v, want merged delta, tool result, and message completed", snapshot.Events)
	}
	if snapshot.Events[0].Seq == snapshot.Events[0].LastSeq {
		t.Fatalf("delta record was not merged: %#v", snapshot.Events[0])
	}
	var mergedDelta struct {
		MessageID string `json:"message_id"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(snapshot.Events[0].Payload, &mergedDelta); err != nil {
		t.Fatalf("decode merged delta payload: %v", err)
	}
	if mergedDelta.MessageID != "m1" || mergedDelta.Content != "ab" {
		t.Fatalf("merged delta payload = %#v, want content ab", mergedDelta)
	}
}
