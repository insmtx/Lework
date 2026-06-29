package todo

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	runtimetodo "github.com/insmtx/Leros/backend/agent/runtime/todo"
	"github.com/insmtx/Leros/backend/tools"
)

func TestToolSnapshotPublishesRuntimeTodos(t *testing.T) {
	var emitted []agent.Event
	reporter := runtimetodo.NewTracker(runtimetodo.Options{
		RunID: "run_todo",
		Sink: events.SinkFunc(func(_ context.Context, event *agent.Event) error {
			emitted = append(emitted, *event)
			return nil
		}),
	})
	ctx := runtimetodo.ContextWithReporter(context.Background(), reporter)

	output, err := NewTool().Execute(ctx, tools.JSONInput(map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"id":      "inspect",
				"content": "Inspect code",
				"status":  "running",
			},
		},
	}))

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(emitted) != 1 || emitted[0].Type != events.EventTodoSnapshot {
		t.Fatalf("expected todo snapshot event, got %#v", emitted)
	}

	var decoded struct {
		Todos []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
		Summary struct {
			Total      int `json:"total"`
			InProgress int `json:"in_progress"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(decoded.Todos) != 1 ||
		decoded.Todos[0].ID != "inspect" ||
		decoded.Todos[0].Content != "Inspect code" ||
		decoded.Todos[0].Status != string(runtimetodo.StatusInProgress) ||
		decoded.Summary.Total != 1 ||
		decoded.Summary.InProgress != 1 {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestToolUpdateMergesRuntimeTodos(t *testing.T) {
	reporter := runtimetodo.NewTracker(runtimetodo.Options{})
	ctx := runtimetodo.ContextWithReporter(context.Background(), reporter)
	tool := NewTool()

	if _, err := tool.Execute(ctx, tools.JSONInput(map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{"id": "a", "content": "A", "status": "pending"},
		},
	})); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	output, err := tool.Execute(ctx, tools.JSONInput(map[string]interface{}{
		"merge": true,
		"todos": []interface{}{
			map[string]interface{}{"id": "a", "content": "A", "status": "completed"},
			map[string]interface{}{"id": "b", "content": "B", "status": "pending"},
		},
	}))

	if err != nil {
		t.Fatalf("update: %v", err)
	}

	var decoded struct {
		Todos []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
		Summary struct {
			Total     int `json:"total"`
			Pending   int `json:"pending"`
			Completed int `json:"completed"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(decoded.Todos) != 2 ||
		decoded.Todos[0].Status != string(runtimetodo.StatusCompleted) ||
		decoded.Summary.Total != 2 ||
		decoded.Summary.Pending != 1 ||
		decoded.Summary.Completed != 1 {
		t.Fatalf("expected merged todos, got %#v", decoded.Todos)
	}
}

func TestToolReadReturnsCurrentTodosWithoutEmitting(t *testing.T) {
	var emitted []agent.Event
	reporter := runtimetodo.NewTracker(runtimetodo.Options{
		Sink: events.SinkFunc(func(_ context.Context, event *agent.Event) error {
			emitted = append(emitted, *event)
			return nil
		}),
	})
	if err := reporter.Snapshot(context.Background(), []events.RuntimeTodoItem{
		{ID: "a", Title: "A", Status: "pending"},
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	emitted = nil

	output, err := NewTool().Execute(runtimetodo.ContextWithReporter(context.Background(), reporter), tools.JSONInput(map[string]interface{}{}))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(emitted) != 0 {
		t.Fatalf("read should not emit events, got %#v", emitted)
	}

	var decoded struct {
		Todos []struct {
			Content string `json:"content"`
		} `json:"todos"`
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(decoded.Todos) != 1 || decoded.Todos[0].Content != "A" {
		t.Fatalf("unexpected read output: %s", output)
	}
}

func TestToolRequiresContentField(t *testing.T) {
	reporter := runtimetodo.NewTracker(runtimetodo.Options{})
	ctx := runtimetodo.ContextWithReporter(context.Background(), reporter)

	_, err := NewTool().Execute(ctx, tools.JSONInput(map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{"id": "legacy", "title": "Legacy title", "status": "pending"},
		},
	}))

	if err == nil {
		t.Fatalf("expected content validation error")
	}
}
