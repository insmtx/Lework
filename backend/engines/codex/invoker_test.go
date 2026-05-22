package codex

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
)

func TestAdapterAskCurrentTime(t *testing.T) {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex CLI not found in PATH")
	}
	apiKey := firstNonEmptyEnv("LEROS_LLM_API_KEY")
	if apiKey == "" {
		t.Skip("set LEROS_LLM_API_KEY to run the real codex adapter test")
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
		Prompt:  "用一句中文回答当前系统时间。不要修改任何文件。",
		Model: engines.ModelConfig{
			Provider: "openai",
			APIKey:   apiKey,
			Model:    firstNonEmptyEnv("LEROS_LLM_MODEL"),
			BaseURL:  firstNonEmptyEnv("LEROS_LLM_BASE_URL"),
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

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
