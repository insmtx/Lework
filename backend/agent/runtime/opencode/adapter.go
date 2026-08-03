// Package opencode adapts the OpenCode CLI to the agent Runtime contract.
// 使用 opencode serve 模式，通过 HTTP REST API + SSE 进行通信。
package opencode

import (
	"context"

	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
)

// Adapter 通过 OpenCode CLI serve 模式执行提示。
type Adapter struct {
	invoker *ServerInvoker
}

// NewAdapter 创建 OpenCode Runtime 适配器（serve 模式）。
func NewAdapter(binary string, extraEnv map[string]string, dataDir string) *Adapter {
	if binary == "" {
		binary = "opencode"
	}
	return &Adapter{invoker: NewServerInvoker(binary, extraEnv, dataDir)}
}

// Prepare performs provider-specific workspace setup.
func (a *Adapter) Prepare(_ context.Context, _ string) error {
	return nil
}

// Invoke starts OpenCode serve and returns its process activity stream.
func (a *Adapter) Invoke(ctx context.Context, req cli.InvocationRequest) (*cli.Invocation, error) {
	handle, err := a.invoker.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	return handle, nil
}

var _ cli.Invoker = (*Adapter)(nil)
