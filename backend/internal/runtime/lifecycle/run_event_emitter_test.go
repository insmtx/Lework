package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	lifecyclecontext "github.com/insmtx/Leros/backend/internal/runtime/lifecycle/context"
)

func TestRunnerEmitsSuccessResultAndCompletedArchiveThroughSink(t *testing.T) {
	sink := &recordingSink{}
	runner := NewRunner(successRuntime{}, lifecyclecontext.NewContextBuilder(lifecyclecontext.ContextBuilder{
		BaseSystemPrompt: "base",
	}), nil)

	result, err := runner.Run(context.Background(), lifecycleTestRequest(sink))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result == nil || result.Status != agent.RunStatusCompleted {
		t.Fatalf("expected completed result, got %+v", result)
	}

	got := sink.Events()
	if len(got) != 3 {
		t.Fatalf("expected started, result, completed events, got %d", len(got))
	}
	expectedTypes := []events.EventType{
		events.EventStarted,
		events.EventResult,
		events.EventCompleted,
	}
	for i, expected := range expectedTypes {
		if got[i].Type != expected {
			t.Fatalf("event %d: expected %s, got %s", i, expected, got[i].Type)
		}
		if i > 0 && got[i].Seq != got[i-1].Seq+1 {
			t.Fatalf("event %d: expected contiguous seq, got prev=%d current=%d", i, got[i-1].Seq, got[i].Seq)
		}
	}
	if got[1].Content != "final answer" {
		t.Fatalf("expected final result content, got %q", got[1].Content)
	}
	if got[2].Content != "final answer" {
		t.Fatalf("expected completed content, got %q", got[2].Content)
	}
	completed, err := events.DecodePayload[events.RunCompletedPayload](got[2])
	if err != nil {
		t.Fatalf("decode completed payload: %v", err)
	}
	if completed.Usage == nil || completed.Usage.TotalTokens != 3 {
		t.Fatalf("expected completed usage payload, got %#v", completed.Usage)
	}
	if completed.Result.Message != "final answer" {
		t.Fatalf("expected completed final result, got %#v", completed.Result)
	}
	if !completed.StartedAt.Equal(got[0].CreatedAt) {
		t.Fatalf("expected completed started_at from started event, got payload=%s started_event=%s", completed.StartedAt, got[0].CreatedAt)
	}
	for _, event := range completed.Events {
		if event.Type == events.EventCompleted {
			t.Fatalf("completed archive should not include itself: %#v", completed.Events)
		}
	}
}

func TestRunnerEmitsFailureThroughSink(t *testing.T) {
	sink := &recordingSink{}
	runner := NewRunner(&errorRuntime{err: errors.New("runtime unavailable")}, lifecyclecontext.NewContextBuilder(lifecyclecontext.ContextBuilder{
		BaseSystemPrompt: "base",
	}), nil)

	result, err := runner.Run(context.Background(), lifecycleTestRequest(sink))
	if err == nil {
		t.Fatal("expected run error")
	}
	if result == nil {
		t.Fatal("expected failed result")
	}
	if result.Status != agent.RunStatusFailed {
		t.Fatalf("expected failed status, got %s", result.Status)
	}
	if result.Error != "runtime unavailable" {
		t.Fatalf("expected error message, got %q", result.Error)
	}
	if phase := result.Metadata["phase"]; phase != string(RunPhaseRuntime) {
		t.Fatalf("expected runtime phase, got %v", phase)
	}

	got := sink.Events()
	if len(got) != 2 {
		t.Fatalf("expected started and terminal events, got %d", len(got))
	}
	if got[0].Type != events.EventStarted {
		t.Fatalf("expected started event, got %s", got[0].Type)
	}
	if got[1].Type != events.EventFailed {
		t.Fatalf("expected failed event, got %s", got[1].Type)
	}
	if got[1].Seq != got[0].Seq+1 {
		t.Fatalf("expected failure event seq to continue, got started=%d failed=%d", got[0].Seq, got[1].Seq)
	}
	if !result.StartedAt.Equal(got[0].CreatedAt) {
		t.Fatalf("expected failed result started_at from started event, got result=%s started_event=%s", result.StartedAt, got[0].CreatedAt)
	}
	if got[1].Content != "runtime unavailable" {
		t.Fatalf("expected event error content, got %q", got[1].Content)
	}
	completed, err := events.DecodePayload[events.RunCompletedPayload](got[1])
	if err != nil {
		t.Fatalf("decode failed payload: %v", err)
	}
	if completed.Status != string(agent.RunStatusFailed) {
		t.Fatalf("expected failed payload status, got %q", completed.Status)
	}
	if completed.Result.Message != "runtime unavailable" {
		t.Fatalf("expected failed result message, got %#v", completed.Result)
	}
	if len(completed.Events) == 0 || completed.Events[0].Type != events.EventStarted {
		t.Fatalf("expected failed payload to archive intermediate events, got %#v", completed.Events)
	}
}

func TestRunnerEmitsArtifactsBeforeCompletedArchive(t *testing.T) {
	sink := &recordingSink{}
	runner := NewRunner(successRuntime{}, lifecyclecontext.NewContextBuilder(lifecyclecontext.ContextBuilder{
		BaseSystemPrompt: "base",
	}), nil)
	runner.SetArtifactRecorder(fakeArtifactRecorder{
		artifacts: []events.ArtifactPayload{
			{ArtifactID: "art_test", Title: "Report", Filename: "report.md", MimeType: "text/markdown", ArtifactType: "file"},
		},
	})

	_, err := runner.Run(context.Background(), lifecycleTestRequest(sink))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	got := sink.Events()
	if len(got) != 4 {
		t.Fatalf("expected started, artifact, result, completed events, got %d", len(got))
	}
	expectedTypes := []events.EventType{
		events.EventStarted,
		events.EventArtifactDeclared,
		events.EventResult,
		events.EventCompleted,
	}
	for i, expected := range expectedTypes {
		if got[i].Type != expected {
			t.Fatalf("event %d: expected %s, got %s", i, expected, got[i].Type)
		}
	}
	completed, err := events.DecodePayload[events.RunCompletedPayload](got[3])
	if err != nil {
		t.Fatalf("decode completed payload: %v", err)
	}
	if len(completed.Events) != 2 || completed.Events[1].Type != events.EventArtifactDeclared {
		t.Fatalf("expected completed archive to include artifact before result, got %#v", completed.Events)
	}
	if len(completed.Artifacts) != 1 ||
		completed.Artifacts[0].ArtifactID != "art_test" ||
		completed.Artifacts[0].Filename != "report.md" ||
		completed.Artifacts[0].MimeType != "text/markdown" {
		t.Fatalf("expected completed payload artifacts, got %#v", completed.Artifacts)
	}
}

func TestRunnerEmitsCancelledThroughSink(t *testing.T) {
	sink := &recordingSink{}
	runner := NewRunner(&errorRuntime{err: context.DeadlineExceeded}, lifecyclecontext.NewContextBuilder(lifecyclecontext.ContextBuilder{
		BaseSystemPrompt: "base",
	}), nil)

	result, err := runner.Run(context.Background(), lifecycleTestRequest(sink))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
	if result == nil || result.Status != agent.RunStatusCancelled {
		t.Fatalf("expected cancelled result, got %+v", result)
	}
	got := sink.Events()
	if len(got) != 2 {
		t.Fatalf("expected started and terminal events, got %d", len(got))
	}
	if got[0].Type != events.EventStarted {
		t.Fatalf("expected started event, got %s", got[0].Type)
	}
	if got[1].Type != events.EventCancelled {
		t.Fatalf("expected cancelled event, got %s", got[1].Type)
	}
}

func TestRunnerRecoversPanicThroughSink(t *testing.T) {
	sink := &recordingSink{}
	runner := NewRunner(panicRuntime{}, lifecyclecontext.NewContextBuilder(lifecyclecontext.ContextBuilder{
		BaseSystemPrompt: "base",
	}), nil)

	result, err := runner.Run(context.Background(), lifecycleTestRequest(sink))
	if err == nil {
		t.Fatal("expected panic error")
	}
	if result == nil || result.Status != agent.RunStatusFailed {
		t.Fatalf("expected failed result, got %+v", result)
	}
	if phase := result.Metadata["phase"]; phase != string(RunPhasePanic) {
		t.Fatalf("expected panic phase, got %v", phase)
	}
	got := sink.Events()
	if len(got) != 2 {
		t.Fatalf("expected started and terminal events, got %d", len(got))
	}
	if got[0].Type != events.EventStarted {
		t.Fatalf("expected started event, got %s", got[0].Type)
	}
	if got[1].Type != events.EventFailed {
		t.Fatalf("expected failed event, got %s", got[1].Type)
	}
}

func lifecycleTestRequest(sink events.Sink) *agent.RequestContext {
	return &agent.RequestContext{
		RunID: "run_lifecycle_error",
		Input: agent.InputContext{
			Type: agent.InputTypeMessage,
			Text: "hello",
		},
		Model: agent.ModelOptions{
			Provider: "test",
			Model:    "test-model",
			APIKey:   "test-key",
		},
		EventSink: sink,
	}
}

type errorRuntime struct {
	err error
}

func (r *errorRuntime) Run(context.Context, *agent.RequestContext) (*agent.RunResult, error) {
	return &agent.RunResult{
		Status: agent.RunStatusFailed,
		Metadata: map[string]any{
			"runtime": "test",
		},
	}, r.err
}

type successRuntime struct{}

func (successRuntime) Run(_ context.Context, req *agent.RequestContext) (*agent.RunResult, error) {
	return &agent.RunResult{
		RunID:   req.RunID,
		TraceID: req.TraceID,
		Status:  agent.RunStatusCompleted,
		Message: "final answer",
		Usage: &events.UsagePayload{
			InputTokens:  1,
			OutputTokens: 2,
			TotalTokens:  3,
		},
	}, nil
}

type panicRuntime struct{}

func (panicRuntime) Run(context.Context, *agent.RequestContext) (*agent.RunResult, error) {
	panic("runtime exploded")
}

type fakeArtifactRecorder struct {
	artifacts []events.ArtifactPayload
}

func (r fakeArtifactRecorder) Record(context.Context, *agent.RequestContext) ([]events.ArtifactPayload, error) {
	return r.artifacts, nil
}

type recordingSink struct {
	events []*events.Event
}

func (s *recordingSink) Emit(_ context.Context, event *events.Event) error {
	if event == nil {
		return nil
	}
	copied := *event
	s.events = append(s.events, &copied)
	return nil
}

func (s *recordingSink) Events() []*events.Event {
	return append([]*events.Event{}, s.events...)
}
