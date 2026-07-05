package agent

import (
	"context"
	"errors"
	"testing"
)

type runtimeStub struct {
	name   string
	result ExecutionResult
	err    error
}

func (r runtimeStub) Name() string { return r.name }

func (r runtimeStub) Execute(context.Context, ExecutionRequest, NodeObserver) (ExecutionResult, error) {
	return r.result, r.err
}

type observerRecorder struct {
	events []NodeEvent
}

func (o *observerRecorder) Observe(_ context.Context, event NodeEvent) error {
	o.events = append(o.events, event)
	return nil
}

// runtimeFunc adapts a function to the Runtime interface.
type runtimeFunc func(context.Context, ExecutionRequest, NodeObserver) (ExecutionResult, error)

func (f runtimeFunc) Name() string { return "test" }

func (f runtimeFunc) Execute(ctx context.Context, request ExecutionRequest, observer NodeObserver) (ExecutionResult, error) {
	return f(ctx, request, observer)
}

func TestExecutorReturnsResult(t *testing.T) {
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
}

func TestExecutorReturnsRuntimeError(t *testing.T) {
	runtimeErr := errors.New("runtime failed")
	registry := NewRegistry()
	registry.Register("native", runtimeStub{name: "native", err: runtimeErr})
	registry.SetDefault("native")
	observer := &observerRecorder{}

	_, err := NewExecutor(registry).Execute(context.Background(), ExecutionRequest{ExecutionID: "run-1"}, observer)
	if !errors.Is(err, runtimeErr) {
		t.Fatalf("Execute() error = %v, want runtime error", err)
	}
}

func TestExecutorRejectsUnavailableRuntime(t *testing.T) {
	observer := &observerRecorder{}

	_, err := NewExecutor(NewRegistry()).Execute(context.Background(), ExecutionRequest{
		ExecutionID: "run-1",
		Runtime:     "missing",
	}, observer)
	if err == nil {
		t.Fatal("Execute() error = nil, want resolution error")
	}
}

func TestExecutorPassesSerialObserverToRuntime(t *testing.T) {
	// Verify the Runtime receives a SerialObserver, not the raw observer.
	var receivedNodeObserver NodeObserver
	registry := NewRegistry()
	registry.Register("native", runtimeFunc(func(ctx context.Context, request ExecutionRequest, observer NodeObserver) (ExecutionResult, error) {
		receivedNodeObserver = observer
		return ExecutionResult{Message: "done"}, nil
	}))
	registry.SetDefault("native")

	observer := &observerRecorder{}
	_, err := NewExecutor(registry).Execute(context.Background(), ExecutionRequest{
		ExecutionID: "run-1",
	}, observer)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if receivedNodeObserver == nil {
		t.Fatal("Runtime received nil observer")
	}
	// Should be a SerialObserver wrapping our recorder.
	if _, ok := receivedNodeObserver.(*SerialObserver); !ok {
		t.Fatalf("Runtime received %T, want *SerialObserver", receivedNodeObserver)
	}
}
