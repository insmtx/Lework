// Package codex adapts the Codex CLI to the agent Runtime contract.
// 使用 codex app-server --listen stdio:// 模式进行通信。
package codex

import (
	"context"

	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
)

// Adapter 通过 Codex CLI app-server 模式执行提示。
type Adapter struct {
	invoker *AppServerInvoker
}

// NewAdapter 创建 Codex CLI 引擎适配器（app-server 模式）。
func NewAdapter(binary string, extraEnv map[string]string) *Adapter {
	if binary == "" {
		binary = "codex"
	}
	return &Adapter{invoker: NewAppServerInvoker(binary, extraEnv)}
}

// Prepare performs provider-specific workspace setup.
func (a *Adapter) Prepare(_ context.Context, _ string) error {
	return nil
}

// Invoke starts Codex CLI and returns its process activity stream.
func (a *Adapter) Invoke(ctx context.Context, req cli.InvocationRequest) (*cli.Invocation, error) {
	handle, err := a.invoker.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	return handle, nil
}

var _ cli.Invoker = (*Adapter)(nil)
