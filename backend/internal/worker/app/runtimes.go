package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	runtimecli "github.com/insmtx/Leros/backend/internal/cli"
	"github.com/ygpkg/yg-go/logs"
)

// envStrictRuntimeGate 是应急开关：置 1 时恢复“任一外部 Runtime 启动失败即退出”的
// 旧行为，便于线上发现降级逻辑异常时快速回退，而不必回滚镜像。
const envStrictRuntimeGate = "LEROS_STRICT_RUNTIME_GATE"

// runtimeBuildFunc 抽成函数类型，是为了让“单个 Runtime 失败只降级该 Runtime”这条
// 策略可以脱离真实 CLI 单测。
type runtimeBuildFunc func(kind, binary string, options agent.RuntimeAdapterOptions) (agent.Runtime, error)

var (
	discoverCLIRuntimes = runtimecli.DiscoverAvailableCLI
	buildCLIRuntime     = runtimeBuildFunc(newRuntime)
)

// registerCLIRuntimes 按发现结果逐个构造外部 CLI Runtime。
//
// 单个 Runtime 构造失败只记录到 unavailable 并跳过：一个 CLI 坏掉不应该让整个 worker
// 进程退出。历史行为是任意外部 Runtime 失败即 `return nil, err`，opencode 版本探测
// 超时因此拖垮了同时装有 claude/codex 的 worker。
func registerCLIRuntimes(
	ctx context.Context,
	opts Options,
	runtimes map[string]agent.Runtime,
	registered map[string]struct{},
) ([]string, map[string]string, error) {
	statuses := discoverCLIRuntimes()
	kinds := make([]string, 0, len(statuses))
	unavailable := make(map[string]string)

	for _, status := range statuses {
		if !status.Installed {
			continue
		}
		if opts.InteractionRouter == nil {
			return nil, nil, fmt.Errorf("interaction router is required for runtime %q", status.Name)
		}
		normalized := normalizeRuntimeKind(status.Name)
		runtime, err := buildCLIRuntime(normalized, status.Path, agent.RuntimeAdapterOptions{
			InteractionHandler:  opts.InteractionRouter,
			MCPServers:          buildMCPServersFromConfig(opts.CLIConfig),
			ProgressIdleTimeout: opts.ProgressIdleTimeout,
		})
		if err != nil {
			if strictRuntimeGate() {
				return nil, nil, fmt.Errorf("runtime %q unavailable: %w", normalized, err)
			}
			logs.WarnContextf(ctx, "agent runtime %q unavailable, continuing with remaining runtimes: %v", normalized, err)
			unavailable[normalized] = err.Error()
			continue
		}
		runtimes[normalized] = runtime
		registered[normalized] = struct{}{}
		kinds = append(kinds, normalized)
		logs.Infof("Registering agent runtime: %s (version=%s path=%s)", normalized, status.Version, status.Path)
	}

	return kinds, unavailable, nil
}

// fallbackDefaultRuntime 在请求的默认 Runtime 不可用时回退到原生 leros。
//
// 显式 `worker opencode` 启动也走回退：进程能活下来、其它 Runtime 可服务，比让整个
// worker 起不来更符合“单点故障不扩散”的目标；请求里显式指定不可用 Runtime 时，
// 由 agent.Registry 在解析阶段给出单次失败。
func fallbackDefaultRuntime(selected string, registered map[string]struct{}, unavailable map[string]string) string {
	normalized := normalizeRuntimeKind(selected)
	if normalized == "" {
		return agent.RuntimeKindLeros
	}
	if _, ok := registered[normalized]; ok {
		return normalized
	}
	if reason := unavailable[normalized]; reason != "" {
		logs.Warnf("default agent runtime %q is unavailable (%s); falling back to %q",
			selected, reason, agent.RuntimeKindLeros)
	} else {
		logs.Warnf("default agent runtime %q is not registered; falling back to %q",
			selected, agent.RuntimeKindLeros)
	}
	return agent.RuntimeKindLeros
}

// strictRuntimeGate 报告是否启用了“Runtime 失败即退出”的旧行为。
func strictRuntimeGate() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envStrictRuntimeGate))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// sortedRuntimeKinds 让启动汇总日志稳定可读、便于值班比对。
func sortedRuntimeKinds(registered map[string]struct{}) []string {
	kinds := make([]string, 0, len(registered))
	for kind := range registered {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}
