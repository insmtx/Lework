package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type runtimeStub struct {
	name   string
	result ExecutionResult
	err    error
}

func (r runtimeStub) Name() string { return r.name }

func (r runtimeStub) Execute(context.Context, ExecutionRequest, Observer) (ExecutionResult, error) {
	return r.result, r.err
}

type observerRecorder struct {
	events []*Event
	errAt  EventType
}

func (o *observerRecorder) Emit(_ context.Context, event *Event) error {
	if event != nil {
		o.events = append(o.events, event)
		if event.Type == o.errAt {
			return errors.New("observer failed")
		}
	}
	return nil
}

func TestExecutorUsesDefaultRuntimeAndEmitsLifecycle(t *testing.T) {
	registry := NewRegistry()
	registry.Register("native", runtimeStub{name: "native", result: ExecutionResult{Message: "done"}})
	registry.SetDefault("native")
	observer := &observerRecorder{}

	result, err := NewExecutor(registry).Execute(context.Background(), ExecutionRequest{
		ExecutionID: "run-1",
		TraceID:     "trace-1",
	}, observer)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Message != "done" {
		t.Fatalf("result message = %q, want done", result.Message)
	}
	if len(observer.events) != 2 ||
		observer.events[0].Type != "execution.started" ||
		observer.events[1].Type != "execution.completed" {
		t.Fatalf("lifecycle events = %#v", observer.events)
	}
}

func TestExecutorEmitsCancelled(t *testing.T) {
	registry := NewRegistry()
	registry.Register("native", runtimeStub{name: "native", err: context.Canceled})
	registry.SetDefault("native")
	observer := &observerRecorder{}

	_, err := NewExecutor(registry).Execute(context.Background(), ExecutionRequest{ExecutionID: "run-1"}, observer)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() error = %v, want context.Canceled", err)
	}
	if len(observer.events) != 2 || observer.events[1].Type != "execution.cancelled" {
		t.Fatalf("lifecycle events = %#v", observer.events)
	}
}

func TestExecutorStopsOnObserverError(t *testing.T) {
	registry := NewRegistry()
	registry.Register("native", runtimeStub{name: "native", result: ExecutionResult{}})
	registry.SetDefault("native")
	observer := &observerRecorder{errAt: "execution.started"}

	_, err := NewExecutor(registry).Execute(context.Background(), ExecutionRequest{ExecutionID: "run-1"}, observer)
	if err == nil {
		t.Fatal("Execute() error = nil, want observer error")
	}
}

func TestExecutorRejectsUnavailableRuntimeBeforeLifecycle(t *testing.T) {
	observer := &observerRecorder{}

	_, err := NewExecutor(NewRegistry()).Execute(context.Background(), ExecutionRequest{
		ExecutionID: "run-1",
		Runtime:     "missing",
	}, observer)
	if err == nil {
		t.Fatal("Execute() error = nil, want resolution error")
	}
	if len(observer.events) != 0 {
		t.Fatalf("lifecycle events = %#v, want none", observer.events)
	}
}

func TestExecutorEmitsFailedForRuntimeError(t *testing.T) {
	runtimeErr := errors.New("runtime failed")
	registry := NewRegistry()
	registry.Register("native", runtimeStub{name: "native", err: runtimeErr})
	registry.SetDefault("native")
	observer := &observerRecorder{}

	_, err := NewExecutor(registry).Execute(context.Background(), ExecutionRequest{ExecutionID: "run-1"}, observer)
	if !errors.Is(err, runtimeErr) {
		t.Fatalf("Execute() error = %v, want runtime error", err)
	}
	if len(observer.events) != 2 ||
		observer.events[0].Type != "execution.started" ||
		observer.events[1].Type != "execution.failed" ||
		observer.events[1].Content != runtimeErr.Error() {
		t.Fatalf("lifecycle events = %#v", observer.events)
	}
}

func TestExecutorReturnsTerminalObserverError(t *testing.T) {
	registry := NewRegistry()
	registry.Register("native", runtimeStub{name: "native", result: ExecutionResult{Message: "done"}})
	registry.SetDefault("native")
	observer := &observerRecorder{errAt: "execution.completed"}

	_, err := NewExecutor(registry).Execute(context.Background(), ExecutionRequest{ExecutionID: "run-1"}, observer)
	if err == nil || !strings.Contains(err.Error(), "observe execution.completed") {
		t.Fatalf("Execute() error = %v, want completed observer error", err)
	}
}

func TestExecutorJoinsRuntimeAndFailedObserverErrors(t *testing.T) {
	runtimeErr := errors.New("runtime failed")
	registry := NewRegistry()
	registry.Register("native", runtimeStub{name: "native", err: runtimeErr})
	registry.SetDefault("native")
	observer := &observerRecorder{errAt: "execution.failed"}

	_, err := NewExecutor(registry).Execute(context.Background(), ExecutionRequest{ExecutionID: "run-1"}, observer)
	if !errors.Is(err, runtimeErr) || !strings.Contains(err.Error(), "observe execution.failed") {
		t.Fatalf("Execute() error = %v, want joined runtime and observer errors", err)
	}
}
