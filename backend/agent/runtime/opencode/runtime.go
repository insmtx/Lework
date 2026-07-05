// Package opencode provides the OpenCode Runtime type.
package opencode

import (
	"context"
	"fmt"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
)

const (
	// Kind is the canonical runtime kind for OpenCode.
	Kind = agent.RuntimeKindOpenCode
)

// Runtime executes requests through OpenCode.
type Runtime struct {
	driver *cli.Driver
}

// New creates an OpenCode Runtime backed by the configured CLI binary.
func New(
	binary string,
	options agent.RuntimeAdapterOptions,
	dataDir string,
) (*Runtime, error) {
	return NewWithInvoker(NewAdapter(binary, nil, dataDir), options)
}

// NewWithInvoker creates an OpenCode Runtime with an injected process invoker.
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
		return agent.ExecutionResult{}, fmt.Errorf("opencode runtime is not initialized")
	}
	return r.driver.RunInvocation(ctx, request, observer)
}

var _ agent.Runtime = (*Runtime)(nil)
