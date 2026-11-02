// Package opencode adapts the OpenCode CLI to the agent Runtime contract.
// 使用 opencode v2 的 ACP（stdio JSON-RPC）进行通信。
package opencode

import (
	"context"
	"time"

	"github.com/insmtx/Lework/backend/agent/runtime/internal/cli"
)

// Adapter 通过 OpenCode CLI 的 ACP 模式执行提示。
type Adapter struct {
	invoker cli.Invoker
}

// NewAdapter 创建 OpenCode Runtime 适配器（ACP 模式）。
// progressIdleTimeout 为宿主配置的进度空闲超时，<=0 时由 ACP invoker 使用内置缺省值。
func NewAdapter(binary string, extraEnv map[string]string, dataDir string, progressIdleTimeout time.Duration) *Adapter {
	return &Adapter{invoker: NewACPInvoker(binary, extraEnv, dataDir, progressIdleTimeout)}
}

// Prepare performs provider-specific workspace setup.
func (a *Adapter) Prepare(_ context.Context, _ string) error {
	return nil
}

// Invoke starts OpenCode ACP and returns its process activity stream.
func (a *Adapter) Invoke(ctx context.Context, req cli.InvocationRequest) (*cli.Invocation, error) {
	handle, err := a.invoker.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	return handle, nil
}

var _ cli.Invoker = (*Adapter)(nil)
