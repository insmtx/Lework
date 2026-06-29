// Package native provides the built-in Leros (Eino) Runtime.
package native

import (
	"context"
	"fmt"

	"github.com/insmtx/Leros/backend/agent"
)

const (
	// Kind is the canonical runtime kind for the built-in Leros runtime.
	Kind = "leros"
)

// Runtime executes requests directly through the in-process Eino runner.
type Runtime struct {
	executor Executor
}

// Executor is the in-process execution backend used by the native Runtime.
type Executor interface {
	Execute(context.Context, agent.ExecutionRequest, agent.Observer) (agent.ExecutionResult, error)
}

// New creates the native Runtime.
func New() (*Runtime, error) {
	runner, err := NewRunner(context.Background())
	if err != nil {
		return nil, fmt.Errorf("create native runner: %w", err)
	}
	return NewWithExecutor(runner)
}

// NewWithExecutor creates a native Runtime with an injected execution backend.
func NewWithExecutor(executor Executor) (*Runtime, error) {
	if executor == nil {
		return nil, fmt.Errorf("native executor is required")
	}
	return &Runtime{executor: executor}, nil
}

func (r *Runtime) Name() string {
	return Kind
}

func (r *Runtime) Execute(
	ctx context.Context,
	request agent.ExecutionRequest,
	observer agent.Observer,
) (agent.ExecutionResult, error) {
	if r == nil || r.executor == nil {
		return agent.ExecutionResult{}, fmt.Errorf("native runtime is not initialized")
	}
	return r.executor.Execute(ctx, request, observer)
}

var _ agent.Runtime = (*Runtime)(nil)
