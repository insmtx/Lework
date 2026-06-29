package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
)

type preparerFunc func(context.Context, *assistantdomain.RunRequest) (*PreparedRun, error)

func (f preparerFunc) Prepare(ctx context.Context, req *assistantdomain.RunRequest) (*PreparedRun, error) {
	return f(ctx, req)
}

type runtimeFunc func(context.Context, agent.ExecutionRequest, agent.Observer) (agent.ExecutionResult, error)

func (runtimeFunc) Name() string { return "test" }

func (f runtimeFunc) Execute(ctx context.Context, request agent.ExecutionRequest, observer agent.Observer) (agent.ExecutionResult, error) {
	return f(ctx, request, observer)
}

type finalizerStub struct {
	result *assistantdomain.RunResult
	events []*agent.Event
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

func (finalizerStub) PostRunBestEffort(context.Context, *PreparedRun, *assistantdomain.RunResult, JournalSnapshot) {
}

type eventRecorder struct {
	events []*agent.Event
}

func (r *eventRecorder) Emit(_ context.Context, event *agent.Event) error {
	copied := *event
	copied.Payload = append(json.RawMessage(nil), event.Payload...)
	r.events = append(r.events, &copied)
	return nil
}

func TestServiceRunEmitsOneTerminalAndPreservesInput(t *testing.T) {
	registry := agent.NewRegistry()
	registry.Register("test", runtimeFunc(func(ctx context.Context, _ agent.ExecutionRequest, observer agent.Observer) (agent.ExecutionResult, error) {
		if err := observer.Emit(ctx, &agent.Event{
			Type:    "message.delta",
			Payload: json.RawMessage(`{"message_id":"m1","content":"hello"}`),
			Content: "hello",
		}); err != nil {
			return agent.ExecutionResult{}, err
		}
		return agent.ExecutionResult{Message: "done", Usage: &agent.Usage{TotalTokens: 3}}, nil
	}))
	registry.SetDefault("test")

	input := &assistantdomain.RunRequest{
		RunID: "run-1",
		Input: assistantdomain.InputContext{
			Type:     assistantdomain.InputTypeMessage,
			Messages: []assistantdomain.InputMessage{{Role: "user", Content: "original"}},
		},
	}
	service := NewService(
		preparerFunc(func(_ context.Context, req *assistantdomain.RunRequest) (*PreparedRun, error) {
			req.Input.Messages[0].Content = "prepared"
			return &PreparedRun{Request: req, Execution: agent.ExecutionRequest{ExecutionID: req.RunID}}, nil
		}),
		agent.NewExecutor(registry),
		finalizerStub{
			result: &assistantdomain.RunResult{
				RunID:     "run-1",
				Status:    assistantdomain.RunStatusCompleted,
				Message:   "done",
				Usage:     &agent.Usage{TotalTokens: 3},
				Artifacts: []assistantdomain.ArtifactRecord{{ArtifactID: "artifact-1", Title: "report"}},
			},
			events: []*agent.Event{{Type: "artifact.declared", Payload: json.RawMessage(`{"artifact_id":"artifact-1"}`)}},
		},
		NewJournalFactory(),
	)
	recorder := &eventRecorder{}
	result, err := service.Run(context.Background(), input, recorder)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if input.Input.Messages[0].Content != "original" {
		t.Fatalf("input was mutated: %#v", input.Input.Messages)
	}
	if result.StartedAt.IsZero() || result.CompletedAt.IsZero() || result.CompletedAt.Before(result.StartedAt) {
		t.Fatalf("invalid timestamps: started=%s completed=%s", result.StartedAt, result.CompletedAt)
	}

	var terminalCount int
	var terminal *agent.Event
	for _, event := range recorder.events {
		if event.Type == "run.completed" || event.Type == "run.failed" || event.Type == "run.cancelled" {
			terminalCount++
			terminal = event
		}
	}
	if terminalCount != 1 {
		t.Fatalf("terminal event count = %d, events = %#v", terminalCount, recorder.events)
	}
	var payload assistantdomain.TerminalPayload
	if err := json.Unmarshal(terminal.Payload, &payload); err != nil {
		t.Fatalf("decode terminal payload: %v", err)
	}
	if len(payload.Events) != 3 {
		t.Fatalf("archived events = %#v, want started, delta, artifact", payload.Events)
	}
	if len(payload.Artifacts) != 1 || payload.Artifacts[0].ArtifactID != "artifact-1" {
		t.Fatalf("terminal artifacts = %#v", payload.Artifacts)
	}
}

func TestServiceRunCancellationSeparatesMessageAndError(t *testing.T) {
	registry := agent.NewRegistry()
	registry.Register("test", runtimeFunc(func(context.Context, agent.ExecutionRequest, agent.Observer) (agent.ExecutionResult, error) {
		return agent.ExecutionResult{}, context.Canceled
	}))
	registry.SetDefault("test")
	service := NewService(
		preparerFunc(func(_ context.Context, req *assistantdomain.RunRequest) (*PreparedRun, error) {
			return &PreparedRun{Request: req, Execution: agent.ExecutionRequest{ExecutionID: req.RunID}}, nil
		}),
		agent.NewExecutor(registry),
		finalizerStub{},
		NewJournalFactory(),
	)
	recorder := &eventRecorder{}
	result, err := service.Run(context.Background(), &assistantdomain.RunRequest{RunID: "run-cancel"}, recorder)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if result.Message != "已取消" || result.Error != context.Canceled.Error() {
		t.Fatalf("result = %#v", result)
	}
	terminal := recorder.events[len(recorder.events)-1]
	if terminal.Type != "run.cancelled" || terminal.Content != "已取消" {
		t.Fatalf("terminal event = %#v", terminal)
	}
}

func TestServiceRejectsNilRequestAndIncompleteDependencies(t *testing.T) {
	if _, err := (&Service{}).Run(context.Background(), &assistantdomain.RunRequest{}, nil); err == nil {
		t.Fatal("Run() error = nil for incomplete service")
	}
	service := NewService(preparerFunc(nil), &agent.Executor{}, finalizerStub{}, NewJournalFactory())
	if _, err := service.Run(context.Background(), nil, nil); err == nil {
		t.Fatal("Run() error = nil for nil request")
	}
}

func TestServiceTurnsTerminalEncodingFailureIntoOneFailedTerminal(t *testing.T) {
	registry := agent.NewRegistry()
	registry.Register("test", runtimeFunc(func(
		context.Context,
		agent.ExecutionRequest,
		agent.Observer,
	) (agent.ExecutionResult, error) {
		return agent.ExecutionResult{Message: "done"}, nil
	}))
	registry.SetDefault("test")
	service := NewService(
		preparerFunc(func(_ context.Context, req *assistantdomain.RunRequest) (*PreparedRun, error) {
			return &PreparedRun{
				Request:   req,
				Execution: agent.ExecutionRequest{ExecutionID: req.RunID},
			}, nil
		}),
		agent.NewExecutor(registry),
		finalizerStub{result: &assistantdomain.RunResult{
			RunID:   "run-invalid-json",
			Status:  assistantdomain.RunStatusCompleted,
			Message: "done",
			ToolCalls: []agent.ToolCallRecord{{
				CallID: "tool-1",
				Result: json.RawMessage(`{"invalid"`),
			}},
		}},
		NewJournalFactory(),
	)
	recorder := &eventRecorder{}
	result, err := service.Run(
		context.Background(),
		&assistantdomain.RunRequest{RunID: "run-invalid-json"},
		recorder,
	)
	if err == nil {
		t.Fatal("Run() error = nil, want terminal encoding error")
	}
	if result == nil || result.Status != assistantdomain.RunStatusFailed ||
		result.Metadata == nil || result.Metadata.Phase != "terminal_encode" {
		t.Fatalf("result = %#v", result)
	}
	var terminalEvents []*agent.Event
	for _, event := range recorder.events {
		if event.Type == "run.completed" || event.Type == "run.failed" || event.Type == "run.cancelled" {
			terminalEvents = append(terminalEvents, event)
		}
	}
	if len(terminalEvents) != 1 || terminalEvents[0].Type != "run.failed" {
		t.Fatalf("terminal events = %#v", terminalEvents)
	}
}

func TestJournalArchivesPayloadUsageAndToolResults(t *testing.T) {
	journal := NewJournal(&assistantdomain.RunRequest{RunID: "run-1", TraceID: "trace-1"}, nil)
	now := time.Now().UTC()
	events := []*agent.Event{
		{Type: "message.delta", CreatedAt: now, Payload: json.RawMessage(`{"message_id":"m1","content":"a"}`)},
		{Type: "message.delta", CreatedAt: now.Add(time.Millisecond), Payload: json.RawMessage(`{"message_id":"m1","content":"b"}`)},
		{Type: "tool_call.completed", Payload: json.RawMessage(`{"tool_call_id":"t1","name":"read","result":{"ok":true}}`)},
		{Type: "message.result", Payload: json.RawMessage(`{"message":"done","usage":{"total_tokens":9}}`)},
	}
	for _, event := range events {
		if err := journal.Record(context.Background(), event); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}
	snapshot := journal.Snapshot()
	if snapshot.Usage == nil || snapshot.Usage.TotalTokens != 9 {
		t.Fatalf("usage = %#v", snapshot.Usage)
	}
	if len(snapshot.ToolCalls) != 1 || snapshot.ToolCalls[0].CallID != "t1" {
		t.Fatalf("tool calls = %#v", snapshot.ToolCalls)
	}
	if len(snapshot.Events) != 2 {
		t.Fatalf("events = %#v, want merged delta and tool result", snapshot.Events)
	}
	if snapshot.Events[0].Seq == snapshot.Events[0].LastSeq {
		t.Fatalf("delta record was not merged: %#v", snapshot.Events[0])
	}
	if len(snapshot.Events[0].Payload) == 0 {
		t.Fatalf("delta payload was not archived: %#v", snapshot.Events[0])
	}
}
