package agent_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	clauderuntime "github.com/insmtx/Leros/backend/agent/runtime/claude"
	codexruntime "github.com/insmtx/Leros/backend/agent/runtime/codex"
	nativeruntime "github.com/insmtx/Leros/backend/agent/runtime/native"
	opencoderuntime "github.com/insmtx/Leros/backend/agent/runtime/opencode"
	"github.com/ygpkg/yg-go/logs"
	"go.uber.org/zap/zapcore"
)

// ---------- helpers ----------

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func isRealModelTestEnabled() bool {
	return os.Getenv("LEROS_AGENT_REAL_MODEL_TESTS") == "1"
}

func testModelConfig() agent.ModelConfig {
	return agent.ModelConfig{
		Provider: "openai",
		Model:    "test",
		BaseURL:  "http://127.0.0.1:8787/v1",
		APIKey:   os.Getenv("LEROS_AGENT_TEST_API_KEY"),
	}
}

func testTimeoutContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 90*time.Second)
}

func projectRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	// currentFile is .../backend/agent/runtime/real_model_integration_test.go
	// project root is 3 levels up from runtime/.
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(currentFile))))
}

func testWorkDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(projectRoot(t), ".leros-workspace", "test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create test work dir: %v", err)
	}
	return dir
}

// testRuntimeFilter returns the runtime name to test, or empty to test all.
func testRuntimeFilter() string {
	return strings.ToLower(strings.TrimSpace(os.Getenv("LEROS_AGENT_TEST_RUNTIME")))
}

// ---------- observer ----------

type recordingObserver struct {
	t      *testing.T
	events []agent.NodeEvent
}

func (o *recordingObserver) Observe(_ context.Context, event agent.NodeEvent) error {
	o.t.Logf("[event] type=%s id=%s payload=%+v", event.Type, event.ID, event.Payload)
	o.events = append(o.events, event)
	return nil
}

// ---------- API key leak detection ----------

func assertNoAPIKeyLeak(t *testing.T, apiKey string, result agent.ExecutionResult, events []agent.NodeEvent, execErr error) {
	t.Helper()
	if apiKey == "" {
		return
	}

	if strings.Contains(result.Message, apiKey) {
		t.Errorf("ExecutionResult.Message leaks API key")
	}
	for _, call := range result.ToolCalls {
		if strings.Contains(fmt.Sprintf("%+v", call), apiKey) {
			t.Errorf("ExecutionResult.ToolCalls leaks API key: %+v", call)
		}
	}
	for _, ev := range events {
		if strings.Contains(fmt.Sprintf("%+v", ev), apiKey) {
			t.Errorf("NodeEvent leaks API key: type=%s id=%s payload=%+v", ev.Type, ev.ID, ev.Payload)
		}
	}
	if execErr != nil && strings.Contains(execErr.Error(), apiKey) {
		t.Errorf("execution error leaks API key: %v", execErr)
	}
}

// ---------- unified runtime table test ----------

func TestRealModelIntegration(t *testing.T) {
	logs.SetLevel(zapcore.DebugLevel)

	if !isRealModelTestEnabled() {
		t.Skip("set LEROS_AGENT_REAL_MODEL_TESTS=1 to run real model integration tests")
	}

	modelConfig := testModelConfig()
	if modelConfig.APIKey == "" {
		t.Skip("set LEROS_AGENT_TEST_API_KEY to run real model integration tests")
	}

	workDir := testWorkDir(t)
	filter := testRuntimeFilter()

	// runtimeEntry describes one runtime under test. Each entry provides
	// a setup function that returns (skip, runtime, cleanup).
	// Native and CLI runtimes share the same shape — the difference is
	// only in how the runtime is constructed, not in how it is executed.
	type runtimeEntry struct {
		name  string
		kind  string
		setup func(t *testing.T) (skip bool, rt agent.Runtime)
	}

	entries := []runtimeEntry{
		{
			name: nativeruntime.Kind,
			kind: nativeruntime.Kind,
			setup: func(t *testing.T) (bool, agent.Runtime) {
				rt, err := nativeruntime.New()
				if err != nil {
					t.Fatalf("create native runtime: %v", err)
				}
				return false, rt
			},
		},
		{
			name: codexruntime.Kind,
			kind: codexruntime.Kind,
			setup: func(t *testing.T) (bool, agent.Runtime) {
				binary := firstNonEmptyEnv("LEROS_AGENT_CODEX_BINARY")
				if binary == "" {
					binary = "codex"
				}
				if _, err := exec.LookPath(binary); err != nil {
					return true, nil
				}
				rt, err := codexruntime.New(binary, agent.RuntimeAdapterOptions{}, "")
				if err != nil {
					t.Fatalf("create codex runtime: %v", err)
				}
				return false, rt
			},
		},
		{
			name: opencoderuntime.Kind,
			kind: opencoderuntime.Kind,
			setup: func(t *testing.T) (bool, agent.Runtime) {
				binary := firstNonEmptyEnv("LEROS_AGENT_OPENCODE_BINARY")
				if binary == "" {
					binary = "opencode"
				}
				if _, err := exec.LookPath(binary); err != nil {
					return true, nil
				}
				rt, err := opencoderuntime.New(binary, agent.RuntimeAdapterOptions{}, "")
				if err != nil {
					t.Fatalf("create opencode runtime: %v", err)
				}
				return false, rt
			},
		},
		{
			name: clauderuntime.Kind,
			kind: clauderuntime.Kind,
			setup: func(t *testing.T) (bool, agent.Runtime) {
				binary := firstNonEmptyEnv("LEROS_AGENT_CLAUDE_BINARY")
				if binary == "" {
					binary = "claude"
				}
				if _, err := exec.LookPath(binary); err != nil {
					return true, nil
				}
				rt, err := clauderuntime.New(binary, agent.RuntimeAdapterOptions{}, "")
				if err != nil {
					t.Fatalf("create claude runtime: %v", err)
				}
				return false, rt
			},
		},
	}

	for _, entry := range entries {
		t.Run(entry.name, func(t *testing.T) {
			if filter != "" && entry.name != filter {
				t.Skipf("LEROS_AGENT_TEST_RUNTIME=%s, skipping %s", filter, entry.name)
			}

			skip, rt := entry.setup(t)
			if skip {
				t.Skipf("%s binary not found", entry.name)
			}

			ctx, cancel := testTimeoutContext(t)
			defer cancel()

			registry := agent.NewRegistry()
			registry.Register(entry.kind, rt)
			registry.SetDefault(entry.kind)
			executor := agent.NewExecutor(registry)

			observer := &recordingObserver{t: t}
			result, err := executor.Execute(ctx, agent.ExecutionRequest{
				ExecutionID:  fmt.Sprintf("real-%s-pong", entry.name),
				Runtime:      entry.kind,
				Prompt:       "Reply with exactly one word: pong.",
				SystemPrompt: "You are a test assistant. Keep responses minimal.",
				Model:        modelConfig,
				Filesystem: agent.FilesystemContext{
					WorkDir: workDir,
					TaskDir: filepath.Join(workDir, entry.name),
				},
			}, observer)

			assertNoAPIKeyLeak(t, modelConfig.APIKey, result, observer.events, err)

			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if strings.TrimSpace(result.Message) == "" {
				t.Fatal("expected non-empty result message")
			}
			if !strings.Contains(strings.ToLower(result.Message), "pong") {
				t.Fatalf("expected result to contain 'pong', got %q", result.Message)
			}

			hasDelta := false
			for _, ev := range observer.events {
				if ev.Type == agent.NodeEventMessageUpdate {
					hasDelta = true
					break
				}
			}
			if !hasDelta {
				t.Log("no message.delta event received, but result is non-empty")
			}

			t.Logf("%s: total events=%d, result message=%s", entry.name, len(observer.events), result.Message)
		})
	}
}
