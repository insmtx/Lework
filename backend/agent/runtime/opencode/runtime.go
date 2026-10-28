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
// 进度空闲超时来自宿主配置 options.ProgressIdleTimeout，零值沿用内置缺省值。
//
// 构造前会探测二进制版本并锁定 2.1.x：ACP 传输依赖该窗口的 resume/usage 形状，
// 让版本不符在启动期显式失败，而不是运行到一半才暴露协议差异。
func New(
	binary string,
	options agent.RuntimeAdapterOptions,
	dataDir string,
) (*Runtime, error) {
	version, err := ProbeVersion(context.Background(), binary)
	if err != nil {
		return nil, err
	}
	if err := version.RequireACPCompatible(); err != nil {
		return nil, err
	}
	return NewWithInvoker(NewAdapter(binary, nil, dataDir, options.ProgressIdleTimeout), options)
}

// NewWithInvoker creates an OpenCode Runtime with an injected process invoker.
// 该入口不做版本探测：注入的 invoker 不一定是真实二进制（单测替身），
// 版本门禁属于进程构造路径，由 New 负责。
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
