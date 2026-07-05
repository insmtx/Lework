package agent

import (
	"context"
	"fmt"
	"strings"
)

// Executor resolves a Runtime by name and drives the execution lifecycle:
//  1. Validate the execution request.
//  2. Resolve a Runtime implementation by name.
//  3. Wrap observer in SerialObserver for ordered, serial event delivery.
//  4. Call Runtime.Execute with the SerialObserver.
//  5. Return ExecutionResult.
//
// Executor does NOT emit execution lifecycle events. The function return value
// (ExecutionResult, error) expresses success, failure, or cancellation.
type Executor struct {
	registry *Registry
}

// NewExecutor creates an Executor backed by the given Registry.
func NewExecutor(registry *Registry) *Executor {
	return &Executor{registry: registry}
}

// ResolveRuntimeKind returns the canonical runtime kind that would be used for execution.
// If kind is empty, the registry default runtime kind is returned.
func (e *Executor) ResolveRuntimeKind(kind string) (string, error) {
	if e == nil || e.registry == nil {
		return "", fmt.Errorf("executor is not initialized")
	}
	_, resolvedKind, err := e.registry.ResolveWithKind(kind)
	if err != nil {
		return "", err
	}
	return resolvedKind, nil
}

// Execute runs the full execution lifecycle for a prepared run.
func (e *Executor) Execute(
	ctx context.Context,
	request ExecutionRequest,
	observer NodeObserver,
) (ExecutionResult, error) {
	if e == nil || e.registry == nil {
		return ExecutionResult{}, fmt.Errorf("executor is not initialized")
	}
	if strings.TrimSpace(request.ExecutionID) == "" {
		return ExecutionResult{}, fmt.Errorf("execution id is required")
	}

	kind := strings.TrimSpace(request.Runtime)

	rt, resolvedKind, err := e.registry.ResolveWithKind(kind)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("resolve runtime %q: %w", kind, err)
	}
	request.Runtime = resolvedKind

	// Wrap observer in SerialObserver to guarantee ordered, serial event delivery
	// across concurrent runtime activity (e.g. parallel tool completions).
	serial := NewSerialObserver(observer)

	result, err := rt.Execute(ctx, request, serial)
	if err != nil {
		return ExecutionResult{}, err
	}
	result.Usage = EnsureUsage(result.Usage)

	return result, nil
}
