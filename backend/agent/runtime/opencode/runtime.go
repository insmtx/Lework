// Package opencode provides the OpenCode Runtime type.
package opencode

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
	"github.com/ygpkg/yg-go/logs"
)

const (
	// Kind is the canonical runtime kind for OpenCode.
	Kind = agent.RuntimeKindOpenCode
)

// Runtime executes requests through OpenCode.
type Runtime struct {
	driver *cli.Driver
	gate   *versionGate
}

// New creates an OpenCode Runtime backed by the configured CLI binary.
// 进度空闲超时来自宿主配置 options.ProgressIdleTimeout，零值沿用内置缺省值。
//
// 版本门禁只作用于“这个 Runtime 能不能用”，不再拖垮整个 worker：缓存命中直接放行；
// 解析出版本但不兼容时返回错误（注册循环会跳过该 Runtime）；探测超时则注册为待定，
// 由后台补探测，Run 前最多等 executeProbeWait。历史上这里是同步 5s 探测 + 失败即
// 返回 error，叠加 worker 的 Fatalf 导致一个坏 CLI 让整机起不来。
func New(
	binary string,
	options agent.RuntimeAdapterOptions,
	dataDir string,
) (*Runtime, error) {
	binary = strings.TrimSpace(binary)
	if binary == "" {
		binary = defaultBinary
	}

	gate := newVersionGate(binary, dataDir)
	if version, ok := gate.cachedVersion(); ok {
		if err := gate.enforce(version); err != nil {
			return nil, err
		}
		return newRuntimeWithGate(binary, options, dataDir, gate)
	}

	version, err := ProbeVersion(context.Background(), binary, quickProbeTimeout)
	switch {
	case err == nil:
		if gateErr := gate.enforce(version); gateErr != nil {
			return nil, gateErr
		}
		gate.resolve(version, nil)
		if cacheErr := gate.saveCache(version); cacheErr != nil {
			logs.Debugf("opencode version probe cache write skipped: %v", cacheErr)
		}
	case errors.Is(err, context.DeadlineExceeded):
		logs.Warnf("opencode version probe timed out after %s; registering runtime without blocking startup: %v",
			quickProbeTimeout, err)
		gate.startBackgroundProbe()
	default:
		return nil, err
	}

	return newRuntimeWithGate(binary, options, dataDir, gate)
}

func newRuntimeWithGate(
	binary string,
	options agent.RuntimeAdapterOptions,
	dataDir string,
	gate *versionGate,
) (*Runtime, error) {
	runtime, err := NewWithInvoker(NewAdapter(binary, nil, dataDir, options.ProgressIdleTimeout), options)
	if err != nil {
		return nil, err
	}
	runtime.gate = gate
	return runtime, nil
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
	if r.gate != nil {
		if _, err, resolved := r.gate.wait(executeProbeWait); resolved && err != nil {
			return agent.ExecutionResult{}, fmt.Errorf("opencode runtime unavailable: %w", err)
		}
	}
	return r.driver.RunInvocation(ctx, request, observer)
}

var _ agent.Runtime = (*Runtime)(nil)
