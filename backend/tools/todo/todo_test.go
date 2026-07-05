package todo

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/tools"
)

type testTodoReporter struct {
	items []agent.RuntimeTodoItem
}

func (r *testTodoReporter) Snapshot(
	_ context.Context,
	items []agent.RuntimeTodoItem,
) error {
	r.items = append([]agent.RuntimeTodoItem(nil), items...)
	return nil
}

func (r *testTodoReporter) Update(
	_ context.Context,
	items []agent.RuntimeTodoItem,
	merge bool,
) error {
	if !merge {
		r.items = append([]agent.RuntimeTodoItem(nil), items...)
		return nil
	}
	byID := make(map[string]int, len(r.items))
	for index, item := range r.items {
		byID[item.ID] = index
	}
	for _, item := range items {
		if index, ok := byID[item.ID]; ok {
			r.items[index] = item
		} else {
			r.items = append(r.items, item)
		}
	}
	return nil
}

func (r *testTodoReporter) List() []agent.RuntimeTodoItem {
	return append([]agent.RuntimeTodoItem(nil), r.items...)
}

func TestToolSnapshotReturnsRuntimeTodos(t *testing.T) {
	reporter := &testTodoReporter{}
	ctx := agent.ContextWithTodoReporter(context.Background(), reporter)
	output, err := NewTool().Execute(ctx, tools.JSONInput(map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"id": "inspect", "content": "Inspect code", "status": "in_progress",
			},
		},
	}))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var decoded struct {
		Todos   []todoResultItem `json:"todos"`
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
		decoded.Todos[0].Status != "in_progress" ||
		decoded.Summary.Total != 1 ||
		decoded.Summary.InProgress != 1 {
		t.Fatalf("unexpected output: %#v", decoded)
	}
}

func TestToolUpdateMergesTodos(t *testing.T) {
	reporter := &testTodoReporter{}
	ctx := agent.ContextWithTodoReporter(context.Background(), reporter)
	if _, err := NewTool().Execute(ctx, tools.JSONInput(map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{"id": "a", "content": "Task A", "status": "pending"},
			map[string]interface{}{"id": "b", "content": "Task B", "status": "pending"},
		},
	})); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	if _, err := NewTool().Execute(ctx, tools.JSONInput(map[string]interface{}{
		"merge": true,
		"todos": []interface{}{
			map[string]interface{}{"id": "a", "content": "Task A", "status": "completed"},
		},
	})); err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(reporter.items) != 2 || reporter.items[0].Status != "completed" {
		t.Fatalf("merged items = %#v", reporter.items)
	}
}

func TestToolReadReturnsCurrentTodos(t *testing.T) {
	reporter := &testTodoReporter{
		items: []agent.RuntimeTodoItem{{ID: "a", Title: "A", Status: "pending"}},
	}
	output, err := NewTool().Execute(
		agent.ContextWithTodoReporter(context.Background(), reporter),
		tools.JSONInput(map[string]interface{}{}),
	)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var decoded struct {
		Todos []todoResultItem `json:"todos"`
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(decoded.Todos) != 1 || decoded.Todos[0].Content != "A" {
		t.Fatalf("unexpected read output: %s", output)
	}
}
