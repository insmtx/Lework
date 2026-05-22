package eino

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	runtimetodo "github.com/insmtx/Leros/backend/internal/agent/runtime/todo"
	"github.com/insmtx/Leros/backend/tools"
	todotools "github.com/insmtx/Leros/backend/tools/todo"
)

func TestInvokableToolInjectsTodoReporter(t *testing.T) {
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

	adapter := NewToolAdapter(registry)
	einoTools, err := adapter.EinoTools(ToolBinding{
		TodoReporter: reporter,
		AllowedTools: []string{todotools.ToolNameTodo},
	}, events.SinkFunc(func(_ context.Context, event *events.Event) error {
		emitted = append(emitted, *event)
		return nil
	}))
	if err != nil {
		t.Fatalf("build tools: %v", err)
	}
	if len(einoTools) != 1 {
		t.Fatalf("expected one tool, got %d", len(einoTools))
	}

	runnable, ok := einoTools[0].(*invokableTool)
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

func TestInvokableToolStillEmitsToolEventsForNonTodoTool(t *testing.T) {
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
	adapter := NewToolAdapter(registry)
	einoTools, err := adapter.EinoTools(ToolBinding{
		AllowedTools: []string{"regular_tool"},
	}, events.SinkFunc(func(_ context.Context, event *events.Event) error {
		emitted = append(emitted, *event)
		return nil
	}))
	if err != nil {
		t.Fatalf("build tools: %v", err)
	}
	runnable, ok := einoTools[0].(*invokableTool)
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
