package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Executor resolves a Runtime by name and drives the execution lifecycle:
//  1. Validate the execution request.
//  2. Resolve a Runtime implementation by name.
//  3. Emit execution.started through the observer.
//  4. Call Runtime.Execute.
//  5. Handle cancellation propagation and observer errors.
//  6. Emit execution.completed / execution.failed / execution.cancelled.
//  7. Return ExecutionResult.
type Executor struct {
	registry *Registry
}

// NewExecutor creates an Executor backed by the given Registry.
func NewExecutor(registry *Registry) *Executor {
	return &Executor{registry: registry}
}

// Execute runs the full execution lifecycle for a prepared run.
func (e *Executor) Execute(
	ctx context.Context,
	request ExecutionRequest,
	observer Observer,
) (ExecutionResult, error) {
	if e == nil || e.registry == nil {
		return ExecutionResult{}, fmt.Errorf("executor is not initialized")
	}
	if strings.TrimSpace(request.ExecutionID) == "" {
		return ExecutionResult{}, fmt.Errorf("execution id is required")
	}

	kind := strings.TrimSpace(request.Runtime)

	rt, err := e.registry.Resolve(kind)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("resolve runtime %q: %w", kind, err)
	}

	sink := observerSink{o: observer}
	if err := sink.Emit(ctx, executionEvent(request, "execution.started", "")); err != nil {
		return ExecutionResult{}, fmt.Errorf("observe execution.started: %w", err)
	}

	result, err := rt.Execute(ctx, request, observer)
	if err != nil {
		eventType := EventType("execution.failed")
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			eventType = "execution.cancelled"
		}
		observerErr := sink.Emit(context.WithoutCancel(ctx), executionEvent(request, eventType, err.Error()))
		if observerErr != nil {
			return ExecutionResult{}, errors.Join(err, fmt.Errorf("observe %s: %w", eventType, observerErr))
		}
		return ExecutionResult{}, err
	}
	if err := sink.Emit(ctx, executionEvent(request, "execution.completed", "")); err != nil {
		return ExecutionResult{}, fmt.Errorf("observe execution.completed: %w", err)
	}

	return result, nil
}

func executionEvent(request ExecutionRequest, eventType EventType, content string) *Event {
	return &Event{
		RunID:     request.ExecutionID,
		TraceID:   request.TraceID,
		Type:      eventType,
		CreatedAt: time.Now().UTC(),
		Content:   content,
	}
}

// Registry maps runtime kind names to Runtime implementations.
// It is populated at composition root (cmd/leros/worker.go) and is
// read-only during execution.
type Registry struct {
	runtimes    map[string]Runtime
	defaultKind string
}

// NewRegistry creates a new Registry.
func NewRegistry() *Registry {
	return &Registry{
		runtimes: make(map[string]Runtime),
	}
}

// Register adds a Runtime implementation to the registry.
// name is normalized to lowercase before storage.
func (r *Registry) Register(name string, rt Runtime) {
	if r == nil || rt == nil {
		return
	}
	name = normalizeKind(name)
	if name == "" {
		return
	}
	r.runtimes[name] = rt
}

// SetDefault sets the default runtime kind returned when Resolve receives an empty kind.
func (r *Registry) SetDefault(kind string) {
	if r == nil {
		return
	}
	r.defaultKind = normalizeKind(kind)
}

// Resolve returns the Runtime for the given kind.
// If kind is empty, the default is returned.
func (r *Registry) Resolve(kind string) (Runtime, error) {
	if r == nil {
		return nil, fmt.Errorf("registry is nil")
	}
	kind = normalizeKind(kind)
	if kind == "" {
		kind = r.defaultKind
	}
	rt, ok := r.runtimes[kind]
	if !ok {
		return nil, fmt.Errorf("runtime %q is not available", kind)
	}
	return rt, nil
}

// Names returns the registered runtime kind names.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.runtimes))
	for n := range r.runtimes {
		names = append(names, n)
	}
	return names
}

func normalizeKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

// Observer receives execution lifecycle events. An Observer that returns
// an error from any method terminates the execution.
type Observer interface {
	EventSink
}

// observerSink adapts an Observer to the EventSink interface.
type observerSink struct {
	o Observer
}

func (s observerSink) Emit(ctx context.Context, event *Event) error {
	if s.o == nil {
		return nil
	}
	return s.o.Emit(ctx, event)
}
