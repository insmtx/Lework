package native

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/runtime/deps"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	runtimetodo "github.com/insmtx/Leros/backend/internal/runtime/todo"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/pkg/leros"
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
				{Role: "user", Content: "remember this project uses Go"},
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
		"remember this project uses Go",
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
		toolAdapter: newToolAdapter(registry),
	}
	state, err := runner.buildRunState(&agent.RequestContext{
		RunID: "run_tools",
		Input: agent.InputContext{
			Type:     agent.InputTypeMessage,
			Messages: []agent.InputMessage{{Role: "user", Content: "hello"}},
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

func TestRunnerBuildRunStateUsesWorkspaceTempWhenWorkDirMissing(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	runner := &Runner{}
	expected := filepath.Join(workspaceRoot, "temp")
	state, err := runner.buildRunState(&agent.RequestContext{
		RunID: "run_temp",
		Input: agent.InputContext{
			Type:     agent.InputTypeMessage,
			Messages: []agent.InputMessage{{Role: "user", Content: "hello"}},
		},
		Runtime: agent.RuntimeOptions{WorkDir: expected},
	})
	if err != nil {
		t.Fatalf("build run state: %v", err)
	}
	if state.toolBinding.ToolContext.WorkDir != expected {
		t.Fatalf("tool work dir = %q, want %q", state.toolBinding.ToolContext.WorkDir, expected)
	}
	if state.req.Runtime.WorkDir != expected {
		t.Fatalf("request work dir = %q, want %q", state.req.Runtime.WorkDir, expected)
	}
}

func TestRunnerBuildRunStateUsesRequestWorkDir(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	projectDir := filepath.Join(workspaceRoot, "projects", "42", "project_1", "repo")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}

	runner := &Runner{}
	state, err := runner.buildRunState(&agent.RequestContext{
		RunID: "run_project",
		Input: agent.InputContext{
			Type:     agent.InputTypeMessage,
			Messages: []agent.InputMessage{{Role: "user", Content: "hello"}},
		},
		Runtime: agent.RuntimeOptions{WorkDir: projectDir},
	})
	if err != nil {
		t.Fatalf("build run state: %v", err)
	}
	if state.toolBinding.ToolContext.WorkDir != projectDir {
		t.Fatalf("tool work dir = %q, want %q", state.toolBinding.ToolContext.WorkDir, projectDir)
	}
}

func TestToolInvokerInjectsTodoReporter(t *testing.T) {
	registry := tools.NewRegistry()
	if err := todotools.Register(registry); err != nil {
		t.Fatalf("register todo tool: %v", err)
	}

	var emitted []events.Event
	reporter := runtimetodo.NewTracker(runtimetodo.Options{
		RunID: "run_adapter",
		Sink: events.SinkFunc(func(_ context.Context, event *events.Event) error {
			emitted = append(emitted, *event)
			return nil
		}),
	})

	adapter := newToolAdapter(registry)
	specs, invoker, err := adapter.EinoTools(toolBinding{
		TodoReporter: reporter,
		AllowedTools: []string{todotools.ToolNameTodo},
	}, events.SinkFunc(func(_ context.Context, event *events.Event) error {
		emitted = append(emitted, *event)
		return nil
	}))
	if err != nil {
		t.Fatalf("build tools: %v", err)
	}
	einoTools := buildEinoTools(specs, invoker)
	if len(einoTools) != 1 {
		t.Fatalf("expected one tool, got %d", len(einoTools))
	}

	runnable, ok := einoTools[0].(interface {
		InvokableRun(context.Context, string, ...einotool.Option) (string, error)
	})
	if !ok {
		t.Fatalf("expected invokable tool, got %T", einoTools[0])
	}

	output, err := runnable.InvokableRun(context.Background(), `{"todos":[{"content":"Plan","status":"pending"}]}`)
	if err != nil {
		t.Fatalf("run tool: %v", err)
	}
	if output == "" {
		t.Fatalf("expected tool output")
	}
	if len(emitted) != 1 || emitted[0].Type != events.EventTodoSnapshot {
		t.Fatalf("expected todo snapshot, got %#v", emitted)
	}
}

func TestToolInvokerEmitsToolEventsForNonTodoTool(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(&mockTool{
		BaseTool: tools.NewBaseTool(
			"regular_tool",
			"Regular test tool",
			tools.Schema{Type: "object"},
		),
	}); err != nil {
		t.Fatalf("register mock tool: %v", err)
	}

	var emitted []events.Event
	adapter := newToolAdapter(registry)
	specs, invoker, err := adapter.EinoTools(toolBinding{
		AllowedTools: []string{"regular_tool"},
	}, events.SinkFunc(func(_ context.Context, event *events.Event) error {
		emitted = append(emitted, *event)
		return nil
	}))
	if err != nil {
		t.Fatalf("build tools: %v", err)
	}
	einoTools := buildEinoTools(specs, invoker)
	runnable, ok := einoTools[0].(interface {
		InvokableRun(context.Context, string, ...einotool.Option) (string, error)
	})
	if !ok {
		t.Fatalf("expected invokable tool, got %T", einoTools[0])
	}

	if _, err := runnable.InvokableRun(context.Background(), `{}`); err != nil {
		t.Fatalf("run tool: %v", err)
	}
	if len(emitted) != 2 ||
		emitted[0].Type != events.EventToolCallStarted ||
		emitted[1].Type != events.EventToolCallCompleted {
		t.Fatalf("expected regular tool call events, got %#v", emitted)
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

	runtimeDeps, err := deps.New(ctx, deps.Options{})
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
			Type:     agent.InputTypeMessage,
			Messages: []agent.InputMessage{{Role: "user", Content: "Reply with exactly this text: Leros agent runtime ok"}},
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

	runtimeDeps, err := deps.New(ctx, deps.Options{})
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
				"You must use tools to complete the user task; do not answer without tool usage.",
				"node_shell executes commands in the current worker environment.",
			}, "\n"),
		},
		Actor: agent.ActorContext{
			UserID:  "test-user",
			Channel: "test",
		},
		Model: realModelOptions(apiKey),
		Input: agent.InputContext{
			Type:     agent.InputTypeMessage,
			Messages: []agent.InputMessage{{Role: "user", Content: "Use a tool to query the current system time."}},
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

	runtimeDeps, err := deps.New(ctx, deps.Options{})
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
				"You must use tools to complete the user task; do not answer without tool usage.",
				"node_shell executes commands in the current worker environment.",
			}, "\n"),
		},
		Actor: agent.ActorContext{
			UserID:  "test-user",
			Channel: "test",
		},
		Model: realModelOptions(apiKey),
		Input: agent.InputContext{
			Type:     agent.InputTypeTaskInstruction,
			Messages: []agent.InputMessage{{Role: "user", Content: "Use the weather skill to query the weather in Shanghai."}},
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

type mockTool struct {
	tools.BaseTool
}

func (m *mockTool) Validate(input map[string]interface{}) error {
	return nil
}

func (m *mockTool) Execute(ctx context.Context, input map[string]interface{}) (string, error) {
	return tools.JSONString(map[string]interface{}{
		"tool": m.Name(),
	})
}
