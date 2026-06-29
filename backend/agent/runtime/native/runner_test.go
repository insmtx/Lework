package native

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	runtimetodo "github.com/insmtx/Leros/backend/agent/runtime/todo"
	"github.com/insmtx/Leros/backend/internal/assistant"
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

// noopEngineSink implements engineSink by discarding events.
type noopEngineSink struct{}

func (noopEngineSink) Emit(context.Context, *agent.Event) error { return nil }

// eventsSinkAdapter adapts a *recordingEngineSink to events.Sink for the todo tracker.
type eventsSinkAdapter struct {
	sink *recordingEngineSink
}

func (a eventsSinkAdapter) Emit(ctx context.Context, evt *agent.Event) error {
	return a.sink.Emit(ctx, evt)
}

// recordingEngineSink records engine events emitted through the sink.
type recordingEngineSink struct {
	mu        sync.Mutex
	events    []agent.Event
	eventsPtr *[]agent.Event // old-style events captured for assertions
}

func newRecordingEngineSink() *recordingEngineSink {
	return &recordingEngineSink{}
}

func (s *recordingEngineSink) Emit(ctx context.Context, event *agent.Event) error {
	if event == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, *event)
	// Also record as agent.Event for backward compatibility in assertions.
	if s.eventsPtr != nil {
		*s.eventsPtr = append(*s.eventsPtr, *event)
	}
	return nil
}

func TestRunnerBuildToolBindingMergesDefaultTools(t *testing.T) {
	registry := tools.NewRegistry()
	if err := memorytools.Register(registry); err != nil {
		t.Fatalf("register memory tools: %v", err)
	}
	if err := skillusetools.Register(registry); err != nil {
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

	runner := &Runner{}
	binding := runner.buildToolBinding(agent.ExecutionRequest{
		ExecutionID: "run_tools",
		Prompt:      "hello",
		Tools:       adaptRegistry(registry),
	}, noopEngineSink{})

	expected := []string{
		memorytools.ToolNameMemory,
		nodetools.ToolNameNodeFileRead,
		skillmanagetools.ToolNameSkillManage,
		skillusetools.ToolNameSkillUse,
		nodetools.ToolNameNodeShell,
		todotools.ToolNameTodo,
		nodetools.ToolNameNodeFileWrite,
	}
	var actual []string
	for _, tool := range binding.Tools {
		actual = append(actual, tool.Definition().Name)
	}
	if got := strings.Join(actual, ","); got != strings.Join(expected, ",") {
		t.Fatalf("unexpected tools:\nwant: %v\n got: %v", expected, actual)
	}
}

func TestToolInvokerInjectsTodoReporter(t *testing.T) {
	registry := tools.NewRegistry()
	if err := todotools.Register(registry); err != nil {
		t.Fatalf("register todo tool: %v", err)
	}

	var emitted []agent.Event
	sink := newRecordingEngineSink()
	sink.eventsPtr = &emitted

	specs, invoker, err := buildRuntimeTools(toolBinding{
		Tools:        adaptRegistry(registry),
		AllowedTools: []string{todotools.ToolNameTodo},
		TodoReporter: runtimetodo.NewTracker(runtimetodo.Options{
			RunID: "run_adapter",
			Sink:  eventsSinkAdapter{sink},
		}),
	}, sink)
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

	var emitted []agent.Event
	sink := newRecordingEngineSink()
	sink.eventsPtr = &emitted

	specs, invoker, err := buildRuntimeTools(toolBinding{
		Tools:        adaptRegistry(registry),
		AllowedTools: []string{"regular_tool"},
	}, sink)
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

	agt, err := NewRunner(ctx)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	result, err := agt.Execute(ctx, agent.ExecutionRequest{
		ExecutionID: "run_real_model_message",
		Prompt:      "Reply with exactly this text: Leros agent runtime ok",
		Model:       realModelOptions(apiKey),
	}, nil)
	if err != nil {
		t.Fatalf("run agent: %v", err)
	}
	message := result.Message
	if strings.TrimSpace(message) == "" {
		t.Fatalf("expected non-empty model response")
	}
	if !strings.Contains(message, "Leros agent runtime ok") {
		t.Fatalf("unexpected model response: %s", message)
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

	agt, err := NewRunner(ctx)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	result, err := agt.Execute(ctx, agent.ExecutionRequest{
		ExecutionID: "run_real_model_node_shell_time",
		SystemPrompt: strings.Join([]string{
			"You must use tools to complete the user task; do not answer without tool usage.",
			"node_shell executes commands in the current worker environment.",
		}, "\n"),
		Prompt: "Use a tool to query the current system time.",
		Model:  realModelOptions(apiKey),
		Tools:  adaptRegistry(registry),
	}, nil)
	if err != nil {
		t.Fatalf("run agent: %v", err)
	}
	message := result.Message
	if strings.TrimSpace(message) == "" {
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
	skillDir := newBundledRuntimeSkillsCatalog(t)
	if _, err := skillcatalog.Get("weather"); err != nil {
		t.Fatalf("weather skill must be available in %s: %v", skillDir, err)
	}

	registry := tools.NewRegistry()
	if err := skillusetools.Register(registry); err != nil {
		t.Fatalf("register skill tools: %v", err)
	}
	if err := nodetools.Register(registry); err != nil {
		t.Fatalf("register node tools: %v", err)
	}

	agt, err := NewRunner(ctx)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	result, err := agt.Execute(ctx, agent.ExecutionRequest{
		ExecutionID: "run_real_model_weather_skill_shanghai",
		SystemPrompt: strings.Join([]string{
			"You must use tools to complete the user task; do not answer without tool usage.",
			"node_shell executes commands in the current worker environment.",
		}, "\n"),
		Prompt: "Use the weather skill to query the weather in Shanghai.",
		Model:  realModelOptions(apiKey),
		Tools:  adaptRegistry(registry),
	}, nil)
	if err != nil {
		t.Fatalf("run weather skill agent: %v", err)
	}
	message := result.Message
	if strings.TrimSpace(message) == "" {
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

func realModelOptions(apiKey string) agent.ModelConfig {
	return agent.ModelConfig{
		Provider: "openai",
		APIKey:   apiKey,
		Model:    firstNonEmptyEnv("LEROS_LLM_MODEL"),
		BaseURL:  firstNonEmptyEnv("LEROS_LLM_BASE_URL"),
	}
}

func newBundledRuntimeSkillsCatalog(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatalf("resolve current test file")
	}

	skillsDir := filepath.Join(filepath.Dir(currentFile), "..", "skills")
	workspaceRoot := filepath.Dir(filepath.Dir(skillsDir))
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	return skillsDir
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

type mockTool struct {
	tools.BaseTool
}

func adaptRegistry(registry *tools.Registry) []agent.Tool {
	if registry == nil {
		return nil
	}
	legacy := registry.List()
	result := make([]agent.Tool, 0, len(legacy))
	for _, tool := range legacy {
		result = append(result, assistant.Adapt(tool, tools.ToolContext{}))
	}
	return result
}

func (m *mockTool) Validate(json.RawMessage) error {
	return nil
}

func (m *mockTool) Execute(context.Context, json.RawMessage) (string, error) {
	return tools.JSONString(map[string]interface{}{
		"tool": m.Name(),
	})
}
