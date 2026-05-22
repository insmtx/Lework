package leros

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/internal/agent"
	einoadapter "github.com/insmtx/Leros/backend/internal/agent/eino"
	"github.com/insmtx/Leros/backend/internal/agent/runtime/deps"
	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/tools"
	memorytools "github.com/insmtx/Leros/backend/tools/memory"
	nodetools "github.com/insmtx/Leros/backend/tools/node"
	skillmanagetools "github.com/insmtx/Leros/backend/tools/skill_manage"
	skillusetools "github.com/insmtx/Leros/backend/tools/skill_use"
	todotools "github.com/insmtx/Leros/backend/tools/todo"
	"github.com/ygpkg/yg-go/logs"
	"go.uber.org/zap/zapcore"
)

func TestRunnerBuildSystemPromptOnlyKeepsRuntimePrompt(t *testing.T) {
	runner := &Runner{
		systemPrompt: "Base runtime prompt.",
	}

	prompt, err := runner.buildSystemPrompt(&agent.RequestContext{
		Assistant: agent.AssistantContext{
			SystemPrompt: "Assistant-specific prompt.",
		},
		Conversation: agent.ConversationContext{
			Messages: []agent.InputMessage{
				{Role: "user", Content: "请记住这个项目使用Go。"},
			},
		},
	})
	if err != nil {
		t.Fatalf("build system prompt: %v", err)
	}

	for _, expected := range []string{
		"Base runtime prompt.",
		"Assistant-specific prompt.",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q, got %s", expected, prompt)
		}
	}
	for _, unexpected := range []string{
		"Available skills:",
		"## Skill:",
		"<session-summary>",
		"请记住这个项目使用Go。",
	} {
		if strings.Contains(prompt, unexpected) {
			t.Fatalf("expected prompt not to contain %q, got %s", unexpected, prompt)
		}
	}
}

func TestRunnerBuildRunStateMergesDefaultAndRequestTools(t *testing.T) {
	registry := tools.NewRegistry()
	if err := memorytools.Register(registry); err != nil {
		t.Fatalf("register memory tools: %v", err)
	}
	if err := skillusetools.Register(registry, skillcatalog.NewEmptyCatalog()); err != nil {
		t.Fatalf("register skill use tools: %v", err)
	}
	if err := skillmanagetools.Register(registry); err != nil {
		t.Fatalf("register skill manage tools: %v", err)
	}
	if err := todotools.Register(registry); err != nil {
		t.Fatalf("register todo tools: %v", err)
	}
	if err := nodetools.Register(registry); err != nil {
		t.Fatalf("register node tools: %v", err)
	}

	runner := &Runner{
		toolAdapter: einoadapter.NewToolAdapter(registry),
	}
	state, err := runner.buildRunState(&agent.RequestContext{
		RunID: "run_tools",
		Input: agent.InputContext{
			Type: agent.InputTypeMessage,
			Text: "hello",
		},
		Capability: agent.CapabilityContext{
			AllowedTools: []string{
				"custom_tool",
				nodetools.ToolNameNodeShell,
				"custom_tool",
			},
		},
	})
	if err != nil {
		t.Fatalf("build run state: %v", err)
	}

	expected := []string{
		memorytools.ToolNameMemory,
		skillusetools.ToolNameSkillUse,
		skillmanagetools.ToolNameSkillManage,
		todotools.ToolNameTodo,
		nodetools.ToolNameNodeShell,
		nodetools.ToolNameNodeFileRead,
		nodetools.ToolNameNodeFileWrite,
		"custom_tool",
	}
	if got := strings.Join(state.toolBinding.AllowedTools, ","); got != strings.Join(expected, ",") {
		t.Fatalf("unexpected allowed tools:\nwant: %v\n got: %v", expected, state.toolBinding.AllowedTools)
	}
}

func TestAgentRunRealModel(t *testing.T) {
	logs.SetLevel(zapcore.DebugLevel)

	apiKey := firstNonEmptyEnv("LEROS_LLM_API_KEY")
	if apiKey == "" {
		t.Skip("set LEROS_LLM_API_KEY to run the real model agent test")
	}

	ctx, cancel := realModelTestContext(t)
	defer cancel()

	runtimeDeps, err := deps.New(ctx, deps.Options{
		ToolsEnabled: false,
	})
	if err != nil {
		t.Fatalf("new runtime env: %v", err)
	}

	agt, err := NewRunner(ctx, runtimeDeps)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	result, err := agt.Run(ctx, &agent.RequestContext{
		RunID: "run_real_model_message",
		Actor: agent.ActorContext{
			UserID:  "test-user",
			Channel: "test",
		},
		Input: agent.InputContext{
			Type: agent.InputTypeMessage,
			Text: "Reply with exactly this text: Leros agent runtime ok",
		},
		Model:     realModelOptions(apiKey),
		Runtime:   agent.RuntimeOptions{MaxStep: 2},
		EventSink: events.NewLogSink(),
	})
	if err != nil {
		t.Fatalf("run agent: %v", err)
	}
	if result == nil {
		t.Fatalf("expected result")
	}
	if result.Status != agent.RunStatusCompleted {
		t.Fatalf("expected completed result, got %+v", result)
	}
	if strings.TrimSpace(result.Message) == "" {
		t.Fatalf("expected non-empty model response")
	}
	if !strings.Contains(result.Message, "Leros agent runtime ok") {
		t.Fatalf("unexpected model response: %s", result.Message)
	}
}

func TestAgentRunNodeTool(t *testing.T) {
	logs.SetLevel(zapcore.DebugLevel)

	apiKey := firstNonEmptyEnv("LEROS_LLM_API_KEY")
	if apiKey == "" {
		t.Skip("set LEROS_LLM_API_KEY to run the real model agent tool-call test")
	}

	ctx, cancel := realModelTestContext(t)
	defer cancel()
	registry := tools.NewRegistry()
	if err := nodetools.Register(registry); err != nil {
		t.Fatalf("register node tools: %v", err)
	}

	runtimeDeps, err := deps.New(ctx, deps.Options{
		ToolsEnabled: true,
	})
	if err != nil {
		t.Fatalf("new runtime env: %v", err)
	}

	agt, err := NewRunner(ctx, runtimeDeps)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	sink := &recordingEventSink{}
	result, err := agt.Run(ctx, &agent.RequestContext{
		RunID: "run_real_model_node_shell_time",
		Assistant: agent.AssistantContext{
			ID:   "test-assistant",
			Name: "Tool Test Assistant",
			SystemPrompt: strings.Join([]string{
				"你必须使用工具完成用户任务，不能凭空回答。",
				"node_shell 会在当前 worker 环境中执行命令。",
			}, "\n"),
		},
		Actor: agent.ActorContext{
			UserID:  "test-user",
			Channel: "test",
		},
		Model: realModelOptions(apiKey),
		Input: agent.InputContext{
			Type: agent.InputTypeMessage,
			Text: "使用工具查询当前系统时间。",
		},
		Runtime: agent.RuntimeOptions{MaxStep: 6},
		Capability: agent.CapabilityContext{
			AllowedTools: []string{
				nodetools.ToolNameNodeShell,
				nodetools.ToolNameNodeFileRead,
				nodetools.ToolNameNodeFileWrite,
			},
		},
		EventSink: sink,
	})
	if err != nil {
		t.Fatalf("run agent: %v", err)
	}
	if result == nil {
		t.Fatalf("expected result")
	}
	if result.Status != agent.RunStatusCompleted {
		t.Fatalf("expected completed result, got %+v", result)
	}
	if strings.TrimSpace(result.Message) == "" {
		t.Fatalf("expected non-empty model response")
	}

}

func TestAgentRunWeatherSkillQuery(t *testing.T) {
	logs.SetLevel(zapcore.DebugLevel)

	apiKey := firstNonEmptyEnv("LEROS_LLM_API_KEY")
	if apiKey == "" {
		t.Skip("set LEROS_LLM_API_KEY to run the real model agent weather skill test")
	}

	ctx, cancel := realModelTestContext(t)
	defer cancel()
	catalog, skillDir := newBundledRuntimeSkillsCatalog(t)
	if _, err := catalog.Get("weather"); err != nil {
		t.Fatalf("weather skill must be available in %s: %v", skillDir, err)
	}

	registry := tools.NewRegistry()
	if err := skillusetools.Register(registry, catalog); err != nil {
		t.Fatalf("register skill tools: %v", err)
	}
	if err := nodetools.Register(registry); err != nil {
		t.Fatalf("register node tools: %v", err)
	}

	runtimeDeps, err := deps.New(ctx, deps.Options{
		ToolsEnabled: true,
	})
	if err != nil {
		t.Fatalf("new runtime env: %v", err)
	}

	agt, err := NewRunner(ctx, runtimeDeps)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	sink := &recordingEventSink{}
	result, err := agt.Run(ctx, &agent.RequestContext{
		RunID: "run_real_model_weather_skill_shanghai",
		Assistant: agent.AssistantContext{
			ID:   "test-weather-assistant",
			Name: "Weather Skill Test Assistant",
			SystemPrompt: strings.Join([]string{
				"你必须使用工具完成用户任务，不能凭空回答。",
				"node_shell 会在当前 worker 环境中执行命令。",
			}, "\n"),
		},
		Actor: agent.ActorContext{
			UserID:  "test-user",
			Channel: "test",
		},
		Model: realModelOptions(apiKey),
		Input: agent.InputContext{
			Type: agent.InputTypeTaskInstruction,
			Text: "使用 weather 这个 skill 来查询上海的天气。",
		},
		Runtime: agent.RuntimeOptions{MaxStep: 20},
		Capability: agent.CapabilityContext{
			AllowedTools: []string{
				skillusetools.ToolNameSkillUse,
				nodetools.ToolNameNodeShell,
			},
		},
		EventSink: sink,
	})
	if err != nil {
		t.Fatalf("run weather skill agent: %v", err)
	}
	if result == nil {
		t.Fatalf("expected result")
	}
	if result.Status != agent.RunStatusCompleted {
		t.Fatalf("expected completed result, got %+v", result)
	}
	if strings.TrimSpace(result.Message) == "" {
		t.Fatalf("expected non-empty model response")
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

func realModelOptions(apiKey string) agent.ModelOptions {
	return agent.ModelOptions{
		Provider: "openai",
		APIKey:   apiKey,
		Model:    firstNonEmptyEnv("LEROS_LLM_MODEL"),
		BaseURL:  firstNonEmptyEnv("LEROS_LLM_BASE_URL"),
	}
}

func newBundledRuntimeSkillsCatalog(t *testing.T) (*skillcatalog.Catalog, string) {
	t.Helper()

	_, currentFile, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatalf("resolve current test file")
	}

	skillsDir := filepath.Join(filepath.Dir(currentFile), "..", "skills")
	catalog, err := skillcatalog.NewCatalog(os.DirFS(skillsDir))
	if err != nil {
		t.Fatalf("load bundled skills catalog from %s: %v", skillsDir, err)
	}

	return catalog, skillsDir
}

func realModelTestContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()

	timeoutValue := strings.TrimSpace(os.Getenv("LEROS_TEST_TIMEOUT"))
	if timeoutValue == "" {
		timeoutValue = "3m"
	}
	if timeoutValue == "0" || strings.EqualFold(timeoutValue, "none") {
		return context.Background(), func() {}
	}

	timeout, err := time.ParseDuration(timeoutValue)
	if err != nil {
		t.Fatalf("parse LEROS_TEST_TIMEOUT: %v", err)
	}
	return context.WithTimeout(context.Background(), timeout)
}

type recordingEventSink struct {
	mu     sync.Mutex
	events []*events.Event
}

func (s *recordingEventSink) Emit(ctx context.Context, event *events.Event) error {
	if event == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	copied := *event
	logs.DebugContextf(ctx, "recordingEventSink event: type=%s run_id=%s seq=%d content=%s",
		copied.Type, copied.RunID, copied.Seq, copied.Content)
	s.events = append(s.events, &copied)
	return nil
}
