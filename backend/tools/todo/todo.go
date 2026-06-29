// Package todo exposes the run-scoped runtime todo list as a Leros tool.
package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/agent/runtime/events"
	runtimetodo "github.com/insmtx/Leros/backend/agent/runtime/todo"
	"github.com/insmtx/Leros/backend/tools"
)

const (
	// ToolNameTodo is the stable runtime tool name for updating run progress.
	ToolNameTodo    = "todo"
	ToolDescription = `Create and maintain a structured todo list for the current coding run. Tracks progress, organizes multi-step work, and surfaces status to the user.

When to use:
- Use proactively for non-trivial work that benefits from planning.
- Use when the task requires 3+ distinct steps or actions, not merely 3 tool calls for one conceptual step.
- Use when the user provides multiple tasks, new instructions arrive, or the user explicitly asks for a todo list.
- Before starting work on an item, mark it in_progress. Keep exactly one item in_progress while work remains.
- As soon as an item is actually done, including required verification, mark it completed. Do not batch completions.
- If an item becomes blocked, obsolete, or cannot be completed as written, mark it cancelled and add a concrete follow-up todo when useful.

When not to use:
- Skip for a single straightforward task, a purely informational answer, casual conversation, or when tracking adds no organizational value.

Rules:
- Allowed statuses are only pending, in_progress, completed, and cancelled.
- Todo items should be specific, actionable, and ordered by priority.
- Preserve user-provided commands verbatim, including flags, arguments, and order.
- Omit todos to read the current list. Provide todos to write the list. Use merge=true to update existing items by id and append new items; merge=false replaces the whole list.
- Always returns the full current list. When in doubt, use it.`
)

// Tool lets an internal Leros agent publish its current plan and progress.
type Tool struct {
	tools.BaseTool
}

// NewTool creates the runtime todo tool.
func NewTool() *Tool {
	return &Tool{
		BaseTool: tools.NewBaseTool(
			ToolNameTodo,
			ToolDescription,
			tools.Schema{
				Type: "object",
				Properties: map[string]*tools.Property{
					"todos": {
						Type:        "array",
						Description: "Todo items to write. Omit this field to read the current list. Each item supports id, content, status, and optional priority. Status must be pending, in_progress, completed, or cancelled.",
						Items: &tools.Property{
							Type: "object",
						},
					},
					"merge": {
						Type:        "boolean",
						Description: "true updates existing items by id and appends new items. false, the default, replaces the whole list.",
					},
				},
			},
		),
	}
}

// Validate checks todo tool input before execution.
func (t *Tool) Validate(rawInput json.RawMessage) error {
	input, err := tools.DecodeInput(rawInput)
	if err != nil {
		return err
	}
	return validateInput(input)
}

func validateInput(input map[string]any) error {
	if input == nil {
		return nil
	}
	raw, exists := input["todos"]
	if !exists || raw == nil {
		return nil
	}
	_, err := parseItems(raw)
	return err
}

// Execute updates the run-scoped todo reporter.
func (t *Tool) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
	input, err := tools.DecodeInput(rawInput)
	if err != nil {
		return "", err
	}
	if err := validateInput(input); err != nil {
		return "", err
	}

	reporter, ok := runtimetodo.ReporterFrom(ctx)
	if !ok {
		return "", fmt.Errorf("todo reporter is required")
	}

	if input != nil {
		raw, exists := input["todos"]
		if exists && raw != nil {
			items, err := parseItems(raw)
			if err != nil {
				return "", err
			}
			if boolValue(input["merge"], false) {
				if err := reporter.Update(ctx, items, true); err != nil {
					return "", err
				}
			} else if err := reporter.Snapshot(ctx, items); err != nil {
				return "", err
			}
		}
	}

	return tools.JSONString(todoOutput(reporter.List()))
}

func todoOutput(items []events.RuntimeTodoItem) struct {
	Todos   []todoResultItem `json:"todos"`
	Summary todoSummary      `json:"summary"`
} {
	todos := make([]todoResultItem, 0, len(items))
	summary := todoSummary{Total: len(items)}
	for _, item := range items {
		todos = append(todos, todoResultItem{
			ID:       item.ID,
			Content:  item.Title,
			Status:   item.Status,
			Priority: item.Priority,
		})
		switch item.Status {
		case string(runtimetodo.StatusInProgress):
			summary.InProgress++
		case string(runtimetodo.StatusCompleted):
			summary.Completed++
		case string(runtimetodo.StatusCancelled):
			summary.Cancelled++
		default:
			summary.Pending++
		}
	}
	return struct {
		Todos   []todoResultItem `json:"todos"`
		Summary todoSummary      `json:"summary"`
	}{
		Todos:   todos,
		Summary: summary,
	}
}

type todoResultItem struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority,omitempty"`
}

type todoSummary struct {
	Total      int `json:"total"`
	Pending    int `json:"pending"`
	InProgress int `json:"in_progress"`
	Completed  int `json:"completed"`
	Cancelled  int `json:"cancelled"`
}

func parseItems(value interface{}) ([]events.RuntimeTodoItem, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case []interface{}:
		items := make([]events.RuntimeTodoItem, 0, len(typed))
		for index, raw := range typed {
			item, err := parseItem(raw)
			if err != nil {
				return nil, fmt.Errorf("todos[%d]: %w", index, err)
			}
			items = append(items, item)
		}
		return items, nil
	case []map[string]interface{}:
		items := make([]events.RuntimeTodoItem, 0, len(typed))
		for index, raw := range typed {
			item, err := parseItem(raw)
			if err != nil {
				return nil, fmt.Errorf("todos[%d]: %w", index, err)
			}
			items = append(items, item)
		}
		return items, nil
	case []events.RuntimeTodoItem:
		return typed, nil
	default:
		return nil, fmt.Errorf("todos must be an array")
	}
}

func parseItem(value interface{}) (events.RuntimeTodoItem, error) {
	raw, ok := value.(map[string]interface{})
	if !ok {
		return events.RuntimeTodoItem{}, fmt.Errorf("item must be an object")
	}
	content := strings.TrimSpace(stringValue(raw, "content"))
	if content == "" {
		return events.RuntimeTodoItem{}, fmt.Errorf("content is required")
	}
	return events.RuntimeTodoItem{
		ID:       strings.TrimSpace(stringValue(raw, "id")),
		Title:    content,
		Status:   strings.TrimSpace(stringValue(raw, "status")),
		Priority: strings.TrimSpace(stringValue(raw, "priority")),
	}, nil
}

func stringValue(input map[string]interface{}, key string) string {
	if input == nil {
		return ""
	}
	value := input[key]
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func boolValue(value interface{}, defaultValue bool) bool {
	switch typed := value.(type) {
	case nil:
		return defaultValue
	case bool:
		return typed
	default:
		return defaultValue
	}
}
