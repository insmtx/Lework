package codex

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
)

func TestAdapterAskCurrentTime(t *testing.T) {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex CLI not found in PATH")
	}

	workDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	adapter := NewAdapter(codexPath, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	handle, err := adapter.Run(ctx, engines.RunRequest{
		WorkDir: workDir,
		Prompt:  "Answer with the current system time. Do not modify files.",
		Model: engines.ModelConfig{
			Provider: "openai",
			APIKey:   "sk-test",
			Model:    "deepseek/deepseek-v4-flash",
			BaseURL:  "http://127.0.0.1:8081",
		},
		Timeout: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("run codex adapter: %v", err)
	}

	var finalEvent events.Event
	var result string
	for event := range handle.Events {
		t.Logf("received event: type=%s, content=%s", event.Type, event.Content)
		if event.Type == events.EventResult {
			result = strings.TrimSpace(event.Content)
		}
		finalEvent = event
	}
	if finalEvent.Type == events.EventFailed {
		t.Fatalf("codex execution failed: %s", finalEvent.Content)
	}
	if finalEvent.Type != events.EventCompleted {
		t.Fatalf("unexpected final event: %#v", finalEvent)
	}

	if result == "" {
		t.Fatal("expected non-empty codex result")
	}
	t.Logf("codex current time result: %s", result)
}

func TestParseCodexLineEmitsResult(t *testing.T) {
	event := parseCodexLine(`{"type":"item.completed","item":{"type":"agent_message","text":"final"}}`)
	if event.Type != events.EventResult || event.Content != "final" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestParseCodexLineCapturesThread(t *testing.T) {
	event := parseCodexLine(`{"type":"thread.started","thread_id":"thread-1"}`)
	if event.Type != engines.EventProviderSessionStarted || event.Content != "thread-1" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestParseCodexLineEmitsTodoSnapshot(t *testing.T) {
	event := parseCodexLine(`{"type":"item.updated","item":{"id":"todo_list_1","type":"todo_list","items":[{"text":"Inspect code","completed":false},{"text":"Run tests","completed":true}]}}`)
	if event.Type != events.EventTodoSnapshot {
		t.Fatalf("expected todo snapshot, got %#v", event)
	}
	items, err := events.DecodePayload[[]events.RuntimeTodoItem](&event)
	if err != nil {
		t.Fatalf("decode todo snapshot: %v", err)
	}
	if len(items) != 2 || items[0].Title != "Inspect code" || items[0].Status != "pending" || items[1].Status != "completed" {
		t.Fatalf("unexpected todo items: %#v", items)
	}
}

func TestBuildArgsInjectsLerosProviderConfig(t *testing.T) {
	args := buildArgs("", false, engines.RunRequest{
		Model: engines.ModelConfig{
			Model:   "gpt-test",
			BaseURL: "http://127.0.0.1:8081",
		},
	})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, `model_provider="leros"`) {
		t.Fatalf("expected leros provider config, got %v", args)
	}
	if !strings.Contains(joined, `model_providers.leros.base_url="http://127.0.0.1:8081/v1"`) {
		t.Fatalf("expected leros provider base url config, got %v", args)
	}
	if !strings.Contains(joined, `model_providers.leros.env_key="OPENAI_API_KEY"`) {
		t.Fatalf("expected leros provider env key config, got %v", args)
	}
	if strings.Contains(joined, `model_providers.leros.wire_api`) {
		t.Fatalf("unexpected wire api config: %v", args)
	}
	if !strings.Contains(joined, "--model gpt-test") {
		t.Fatalf("expected model arg, got %v", args)
	}
}

func TestBuildArgsResumeReadsPromptFromStdin(t *testing.T) {
	args := buildArgs("thread-1", true, engines.RunRequest{
		Prompt: "continue the task",
	})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "continue the task") {
		t.Fatalf("expected prompt not to be injected into args: %v", args)
	}
	if args[len(args)-1] != "-" {
		t.Fatalf("expected resume prompt marker to read stdin, got %v", args)
	}
}

func TestCodexModelEnvUsesOpenAIEnvKeys(t *testing.T) {
	env := codexModelEnv(engines.ModelConfig{
		APIKey:  "sk-test",
		BaseURL: "http://127.0.0.1:8081/v1/",
	})
	if env["OPENAI_API_KEY"] != "sk-test" {
		t.Fatalf("unexpected api key env: %#v", env)
	}
	if env["OPENAI_API_BASE"] != "http://127.0.0.1:8081/v1" {
		t.Fatalf("unexpected api base env: %#v", env)
	}
	if env["OPENAI_BASE_URL"] != "http://127.0.0.1:8081/v1" {
		t.Fatalf("unexpected base url env: %#v", env)
	}
	if env["CODEX_QUIET_MODE"] != "1" {
		t.Fatalf("unexpected quiet mode env: %#v", env)
	}
}
