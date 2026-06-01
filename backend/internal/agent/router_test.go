package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/engines/claude"
	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/runtime/drivers/externalcli"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
)

func TestRuntimeRouterUsesRequestedRuntime(t *testing.T) {
	router := agent.NewRuntimeRouter(agent.RuntimeKindLeros)
	lerosRunner := &testRunner{message: "leros"}
	codexRunner := &testRunner{message: "codex"}

	if err := router.Register(agent.RuntimeKindLeros, lerosRunner); err != nil {
		t.Fatalf("register leros: %v", err)
	}
	if err := router.Register("codex", codexRunner); err != nil {
		t.Fatalf("register codex: %v", err)
	}

	result, err := router.Run(context.Background(), &agent.RequestContext{
		Runtime: agent.RuntimeOptions{Kind: "codex"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Message != "codex" {
		t.Fatalf("expected codex runner, got %q", result.Message)
	}
}

func TestRuntimeRouterUsesDefaultRuntime(t *testing.T) {
	router := agent.NewRuntimeRouter(agent.RuntimeKindLeros)
	if err := router.Register(agent.RuntimeKindLeros, &testRunner{message: "default"}); err != nil {
		t.Fatalf("register default: %v", err)
	}

	result, err := router.Run(context.Background(), &agent.RequestContext{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Message != "default" {
		t.Fatalf("expected default runner, got %q", result.Message)
	}
}

type testRunner struct {
	message string
}

func (r *testRunner) Run(_ context.Context, req *agent.RequestContext) (*agent.RunResult, error) {
	return &agent.RunResult{
		RunID:   req.RunID,
		TraceID: req.TraceID,
		Status:  agent.RunStatusCompleted,
		Message: r.message,
	}, nil
}

func TestRuntimeRouterClaudeRunnerCallsLerosEchoTool(t *testing.T) {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		t.Skipf("claude CLI not found in PATH: %v", err)
	}

	repoRoot := findRepoRoot(t)
	llmConfig := loadRealLLMConfig(t)
	t.Logf("using model config: provider=%q model=%q base_url_set=%t api_key_set=%t",
		llmConfig.Provider, llmConfig.Model, llmConfig.BaseURL != "", llmConfig.APIKey != "")

	claudeEngine := claude.NewAdapter(claudePath, nil)
	claudeRunner, err := externalcli.NewRunner(engines.EngineClaude, claudeEngine)
	if err != nil {
		t.Fatalf("new claude runner: %v", err)
	}

	router := agent.NewRuntimeRouter(engines.EngineClaude)
	if err := router.Register(engines.EngineClaude, claudeRunner); err != nil {
		t.Fatalf("register claude runner: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	eventSink := events.Sink(events.SinkFunc(func(_ context.Context, event *events.Event) error {
		encoded, _ := json.Marshal(event)
		t.Logf("runtime event: %s", string(encoded))
		return nil
	}))

	result, err := router.Run(ctx, &agent.RequestContext{
		RunID:   "run_claude_echo_integration",
		TraceID: "trace_claude_echo_integration",
		Assistant: agent.AssistantContext{
			ID:   "assistant_integration_test",
			Name: "Claude CLI Integration Test",
		},
		Actor: agent.ActorContext{
			UserID:  "integration_test",
			Channel: "go_test",
		},
		Input: agent.InputContext{
			Type: agent.InputTypeTaskInstruction,
			Messages: []agent.InputMessage{{
				Role:    "user",
				Content: `Call the configured Leros MCP tool leros_echo with message "hello from claude runner", then return the tool result JSON.`,
			}},
		},
		Runtime: agent.RuntimeOptions{
			Kind:    engines.EngineClaude,
			WorkDir: repoRoot,
		},
		EventSink: eventSink,
	})
	if err != nil {
		if result != nil {
			resultJSON, _ := json.MarshalIndent(result, "", "  ")
			t.Logf("failed run result:\n%s", string(resultJSON))
		}
		if strings.Contains(err.Error(), "Not logged in") || strings.Contains(err.Error(), "authentication_failed") {
			t.Skipf("claude CLI is not authenticated: %v", err)
		}
		t.Fatalf("run claude runner: %v", err)
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	t.Logf("final run result:\n%s", string(resultJSON))

	if result.Status != agent.RunStatusCompleted {
		t.Fatalf("expected completed status, got %s", result.Status)
	}
	if !strings.Contains(result.Message, "hello from claude runner") {
		t.Fatalf("expected final result to include echo message, got %q", result.Message)
	}
	if !strings.Contains(result.Message, "Leros") {
		t.Fatalf("expected final result to include echo server name, got %q", result.Message)
	}
}

func loadRealLLMConfig(t *testing.T) *config.LLMConfig {
	t.Helper()

	llmConfig := &config.LLMConfig{
		Provider: strings.TrimSpace(os.Getenv("LEROS_LLM_PROVIDER")),
		APIKey:   strings.TrimSpace(os.Getenv("LEROS_LLM_API_KEY")),
		Model:    strings.TrimSpace(os.Getenv("LEROS_LLM_MODEL")),
		BaseURL:  strings.TrimSpace(os.Getenv("LEROS_LLM_BASE_URL")),
	}
	if llmConfig.APIKey == "" {
		llmConfig.APIKey = strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	}
	if llmConfig.APIKey == "" {
		llmConfig.APIKey = strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN"))
	}
	if llmConfig.Provider == "" && llmConfig.APIKey != "" {
		llmConfig.Provider = "anthropic"
	}
	if llmConfig.APIKey == "" {
		t.Log("no model API key found in env; relying on existing claude CLI authentication")
	}

	return llmConfig
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found while searching for repository root")
		}
		dir = parent
	}
}
