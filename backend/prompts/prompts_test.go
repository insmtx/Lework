package prompts

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/config"
)

type mockExecutor struct {
	fn func(ctx context.Context, prompt string, cfg config.LLMConfig) (string, error)
}

func (m *mockExecutor) Execute(ctx context.Context, prompt string, cfg config.LLMConfig) (string, error) {
	return m.fn(ctx, prompt, cfg)
}

func TestRegisterAndGet(t *testing.T) {
	Register("test.greet", "Hello, {name}!")

	got := Get("test.greet")
	want := "Hello, {name}!"
	if got != want {
		t.Fatalf("Get = %q, want %q", got, want)
	}
}

func TestRegisterDuplicateReplacesTemplate(t *testing.T) {
	Register("test.dup", "first")
	Register("test.dup", "second")
	if got := Get("test.dup"); got != "second" {
		t.Fatalf("Get() = %q, want replacement", got)
	}
}

func TestGetMissingKeyReturnsEmpty(t *testing.T) {
	if got := Get("nonexistent"); got != "" {
		t.Fatalf("Get() = %q, want empty", got)
	}
}

func TestKeys(t *testing.T) {
	Register("test.a", "1")
	Register("test.b", "2")
	Register("test.c", "3")

	keys := Keys()
	if len(keys) < 3 {
		t.Fatalf("expected at least 3 keys, got %d", len(keys))
	}
	found := 0
	for _, k := range keys {
		switch k {
		case "test.a", "test.b", "test.c":
			found++
		}
	}
	if found != 3 {
		t.Fatalf("expected to find 3 test keys, found %d", found)
	}
}

func TestManagerRunRendersTemplate(t *testing.T) {
	var capturedPrompt string
	m := New(&mockExecutor{
		fn: func(_ context.Context, p string, _ config.LLMConfig) (string, error) {
			capturedPrompt = p
			return "result", nil
		},
	})
	m.Register("test", "Hello, {name}! You are {age} years old.")

	_, err := m.Run(context.Background(), "test", map[string]any{
		"name": "Alice",
		"age":  30,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	want := "Hello, Alice! You are 30 years old."
	if capturedPrompt != want {
		t.Fatalf("rendered prompt = %q, want %q", capturedPrompt, want)
	}
}

func TestManagerRunPassesLLMConfig(t *testing.T) {
	var capturedCfg config.LLMConfig
	m := New(&mockExecutor{
		fn: func(_ context.Context, p string, cfg config.LLMConfig) (string, error) {
			capturedCfg = cfg
			return p, nil
		},
	})
	m.Register("test", "prompt")

	_, err := m.Run(context.Background(), "test", nil,
		WithModel("gpt-4"),
		WithProvider("openai"),
		WithBaseURL("https://api.openai.com/v1"),
	)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if capturedCfg.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", capturedCfg.Model, "gpt-4")
	}
	if capturedCfg.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", capturedCfg.Provider, "openai")
	}
	if capturedCfg.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("BaseURL = %q, want %q", capturedCfg.BaseURL, "https://api.openai.com/v1")
	}
}

func TestManagerRunMissingKey(t *testing.T) {
	m := New(&mockExecutor{fn: func(_ context.Context, p string, _ config.LLMConfig) (string, error) { return p, nil }})
	if _, err := m.Run(context.Background(), "nonexistent", nil); err == nil {
		t.Fatal("Run() error = nil for missing key")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New(&mockExecutor{fn: func(_ context.Context, p string, _ config.LLMConfig) (string, error) { return p, nil }})
	m.Register("key", "template")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Get("key")
			m.Keys()
			m.Run(context.Background(), "key", nil)
		}()
	}
	wg.Wait()
}

func TestDefaultManagerBuiltinPrompts(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()

	execCalled := int32(0)
	exec := &mockExecutor{
		fn: func(_ context.Context, p string, _ config.LLMConfig) (string, error) {
			atomic.StoreInt32(&execCalled, 1)
			return p, nil
		},
	}

	SetDefaultExecutor(exec)

	if !strings.Contains(Get(KeyAgentSystemDefault), "你的名称是 lework") {
		t.Error("expected default system prompt to contain the current lework identity")
	}

	keys := Keys()
	if len(keys) == 0 {
		t.Fatal("expected built-in prompts to be registered")
	}

	matchCount := 0
	for _, k := range keys {
		switch k {
		case KeyAgentSystemDefault, KeyEventOrchestratorHeader, KeyEventOrchestratorTaskDefault,
			KeyEventOrchestratorTaskPullRequest, KeyEventOrchestratorTaskPush,
			KeyEventOrchestratorTaskIssueComment, KeyLLMTestConnectivity,
			KeyAgentNativeTaskCompletion, KeyAgentNativeToolEnforcement,
			KeyAgentNativeSkillLoading, KeyAgentNativeSkillUsageHint,
			KeyAgentSystemMemoryGuidance,
			KeyAgentSystemPlatformWechat, KeyAgentSystemPlatformFeishu,
			KeyAgentSystemPlatformSlack, KeyAgentSystemPlatformAPI,
			KeySessionTitle, KeyWorkTitle, KeyAgentNativeArtifactDeclaration:
			matchCount++
		}
	}
	if matchCount != 19 {
		t.Fatalf("expected 19 built-in keys, matched %d", matchCount)
	}

	_, err := Run(context.Background(), KeyLLMTestConnectivity, nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if atomic.LoadInt32(&execCalled) != 1 {
		t.Fatal("executor was not called")
	}
}

func TestRunWithoutExecutorReturnsError(t *testing.T) {
	m := &Manager{
		templates: map[string]string{"t": "p"},
	}
	_, err := m.Run(context.Background(), "t", nil)
	if err == nil {
		t.Fatal("expected error when executor not set on Manager")
	}
}

func TestNewNilExecutorReturnsRunError(t *testing.T) {
	manager := New(nil)
	manager.Register("test", "prompt")
	if _, err := manager.Run(context.Background(), "test", nil); err == nil {
		t.Fatal("Run() error = nil for nil executor")
	}
}

func TestSetDefaultExecutorNilMakesRunFail(t *testing.T) {
	SetDefaultExecutor(nil)
	if _, err := Run(context.Background(), KeyAgentSystemDefault, nil); err == nil {
		t.Fatal("Run() error = nil for nil default executor")
	}
	SetDefaultExecutor(NewEinoExecutor())
}

func TestTemplateMissingFieldPreservesPlaceholder(t *testing.T) {
	var capturedPrompt string
	m := New(&mockExecutor{
		fn: func(_ context.Context, p string, _ config.LLMConfig) (string, error) {
			capturedPrompt = p
			return p, nil
		},
	})
	m.Register("t", "Hello {name}, your {role} is {missing}")

	_, _ = m.Run(context.Background(), "t", map[string]any{
		"name": "Bob",
		"role": "admin",
	})

	if !strings.Contains(capturedPrompt, "{missing}") {
		t.Fatal("expected missing placeholder to be preserved")
	}
	if !strings.Contains(capturedPrompt, "Hello Bob") {
		t.Fatal("expected name to be rendered")
	}
}

func TestRunTimeoutViaContext(t *testing.T) {
	m := New(&mockExecutor{
		fn: func(ctx context.Context, p string, _ config.LLMConfig) (string, error) {
			select {
			case <-time.After(500 * time.Millisecond):
				return "done", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	})
	m.Register("t", "prompt")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := m.Run(ctx, "t", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}
