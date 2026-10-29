package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	runtimecli "github.com/insmtx/Leros/backend/internal/cli"
	"github.com/insmtx/Leros/backend/internal/worker/runtimehost"
)

type stubRuntime struct {
	kind string
}

func (s stubRuntime) Name() string { return s.kind }

func (s stubRuntime) Execute(context.Context, agent.ExecutionRequest, agent.NodeObserver) (agent.ExecutionResult, error) {
	return agent.ExecutionResult{}, nil
}

// withRuntimeHooks 替换发现/构造钩子并在用例结束后恢复，避免用例间串扰。
func withRuntimeHooks(
	t *testing.T,
	discover func() []runtimecli.CLIToolStatus,
	build runtimeBuildFunc,
) {
	t.Helper()
	originalDiscover, originalBuild := discoverCLIRuntimes, buildCLIRuntime
	if discover != nil {
		discoverCLIRuntimes = discover
	}
	if build != nil {
		buildCLIRuntime = build
	}
	t.Cleanup(func() {
		discoverCLIRuntimes = originalDiscover
		buildCLIRuntime = originalBuild
	})
}

func installedStatus(name, version string) runtimecli.CLIToolStatus {
	return runtimecli.CLIToolStatus{
		Name:      name,
		Binary:    name,
		Path:      "/usr/local/bin/" + name,
		Installed: true,
		Version:   version,
	}
}

// TestRegisterCLIRuntimesDegradesSingleRuntimeFailure 固化“一个 CLI 坏掉不拖垮 worker”。
func TestRegisterCLIRuntimesDegradesSingleRuntimeFailure(t *testing.T) {
	t.Setenv(envStrictRuntimeGate, "")

	withRuntimeHooks(t, func() []runtimecli.CLIToolStatus {
		return []runtimecli.CLIToolStatus{
			installedStatus("claude", "2.1.246"),
			installedStatus("opencode", "unknown"),
			installedStatus("codex", "0.149.1"),
			{Name: "missing", Binary: "missing", Installed: false},
		}
	}, func(kind, _ string, _ agent.RuntimeAdapterOptions) (agent.Runtime, error) {
		if kind == "opencode" {
			return nil, errors.New("probe /usr/local/bin/opencode --version: context deadline exceeded")
		}
		return stubRuntime{kind: kind}, nil
	})

	runtimes := map[string]agent.Runtime{}
	registered := map[string]struct{}{}
	kinds, unavailable, err := registerCLIRuntimes(
		context.Background(),
		Options{InteractionRouter: runtimehost.NewInteractionRouter()},
		runtimes,
		registered,
	)
	if err != nil {
		t.Fatalf("registerCLIRuntimes() error = %v, want nil", err)
	}
	if len(kinds) != 2 || len(runtimes) != 2 {
		t.Fatalf("registered = %v, want claude+codex only", kinds)
	}
	if _, ok := runtimes["opencode"]; ok {
		t.Fatal("opencode must be skipped when its runtime construction fails")
	}
	if reason := unavailable["opencode"]; !strings.Contains(reason, "context deadline exceeded") {
		t.Fatalf("unavailable[opencode] = %q, want probe failure reason", reason)
	}
	if _, ok := registered["missing"]; ok {
		t.Fatal("runtime not installed must not be registered")
	}
}

// TestRegisterCLIRuntimesStrictGateRestoresOldBehavior 覆盖应急回退开关。
func TestRegisterCLIRuntimesStrictGateRestoresOldBehavior(t *testing.T) {
	t.Setenv(envStrictRuntimeGate, "1")

	withRuntimeHooks(t, func() []runtimecli.CLIToolStatus {
		return []runtimecli.CLIToolStatus{installedStatus("opencode", "unknown")}
	}, func(string, string, agent.RuntimeAdapterOptions) (agent.Runtime, error) {
		return nil, errors.New("probe failed")
	})

	_, _, err := registerCLIRuntimes(
		context.Background(),
		Options{InteractionRouter: runtimehost.NewInteractionRouter()},
		map[string]agent.Runtime{},
		map[string]struct{}{},
	)
	if err == nil {
		t.Fatal("registerCLIRuntimes() error = nil, want strict-gate failure")
	}
}

// TestRegisterCLIRuntimesRequiresInteractionRouter 保持既有的装配约束。
func TestRegisterCLIRuntimesRequiresInteractionRouter(t *testing.T) {
	withRuntimeHooks(t, func() []runtimecli.CLIToolStatus {
		return []runtimecli.CLIToolStatus{installedStatus("claude", "2.1.246")}
	}, func(kind, _ string, _ agent.RuntimeAdapterOptions) (agent.Runtime, error) {
		return stubRuntime{kind: kind}, nil
	})

	_, _, err := registerCLIRuntimes(
		context.Background(),
		Options{},
		map[string]agent.Runtime{},
		map[string]struct{}{},
	)
	if err == nil {
		t.Fatal("registerCLIRuntimes() error = nil, want interaction router error")
	}
}

// TestFallbackDefaultRuntime 覆盖默认 Runtime 不可用时的回退。
func TestFallbackDefaultRuntime(t *testing.T) {
	registered := map[string]struct{}{"leros": {}, "claude": {}}
	unavailable := map[string]string{"opencode": "probe timeout"}

	tests := []struct {
		name     string
		selected string
		want     string
	}{
		{name: "available default stays", selected: "claude", want: "claude"},
		{name: "unavailable default falls back", selected: "opencode", want: agent.RuntimeKindLeros},
		{name: "unknown default falls back", selected: "gemini", want: agent.RuntimeKindLeros},
		{name: "empty default falls back", selected: "", want: agent.RuntimeKindLeros},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fallbackDefaultRuntime(tt.selected, registered, unavailable); got != tt.want {
				t.Fatalf("fallbackDefaultRuntime(%q) = %q, want %q", tt.selected, got, tt.want)
			}
		})
	}
}
