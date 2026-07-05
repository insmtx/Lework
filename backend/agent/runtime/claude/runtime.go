// Package claude provides the Claude Code Runtime.
package claude

import (
	"context"
	"fmt"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
)

const (
	// Kind is the canonical runtime kind for Claude Code.
	Kind = agent.RuntimeKindClaude
)

// Runtime executes requests through Claude Code.
type Runtime struct {
	driver *cli.Driver
}

// New creates a Claude Runtime backed by the configured CLI binary.
func New(binary string, options agent.RuntimeAdapterOptions, _ string) (*Runtime, error) {
	return NewWithInvoker(NewAdapter(binary, nil), options)
}

// NewWithInvoker creates a Claude Runtime with an injected process invoker.
func NewWithInvoker(invoker cli.Invoker, options agent.RuntimeAdapterOptions) (*Runtime, error) {
	driver, err := cli.NewDriver(Kind, invoker, options)
	if err != nil {
		return nil, err
	}
	return &Runtime{driver: driver}, nil
}

func (r *Runtime) Name() string {
	return Kind
}

func (r *Runtime) Execute(
	ctx context.Context,
	request agent.ExecutionRequest,
	observer agent.NodeObserver,
) (agent.ExecutionResult, error) {
	if r == nil || r.driver == nil {
		return agent.ExecutionResult{}, fmt.Errorf("claude runtime is not initialized")
	}
	return r.driver.RunInvocation(ctx, request, observer)
}

var _ agent.Runtime = (*Runtime)(nil)
