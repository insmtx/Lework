package claude

import (
	"fmt"
	"github.com/bytedance/sonic"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
)

// ——— 工具调用事件 ———

func claudeToolCallStartedEvent(block streamContent, state *claudeStreamState) agent.Event {
	rememberClaudeToolName(block, state)
	return *events.NewToolCallStarted(block.ID, block.Name, events.MarshalRaw(block.Input))
}

func rememberClaudeToolName(block streamContent, state *claudeStreamState) {
	if state != nil && block.ID != "" && block.Name != "" {
		if state.toolNames == nil {
			state.toolNames = make(map[string]string)
		}
		state.toolNames[block.ID] = block.Name
	}
}

func claudeToolName(toolUseID string, state *claudeStreamState) string {
	if state == nil || state.toolNames == nil {
		return ""
	}
	return state.toolNames[toolUseID]
}

func claudeToolCallCompletedEvent(block streamContent, state *claudeStreamState) agent.Event {
	name := claudeToolName(block.ToolUseID, state)
	if block.IsError {
		return *events.NewToolCallFailed(block.ToolUseID, name, fmt.Sprintf("%v", block.Content), 0)
	}
	return *events.NewToolCallCompleted(block.ToolUseID, name, events.MarshalRaw(block.Content), 0)
}

// ——— Todo 工具检测 ———

func isClaudeTodoTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "TodoWrite", "TaskCreate", "TaskUpdate", "TaskList":
		return true
	default:
		return false
	}
}

// ——— Todo 事件（tool_use 阶段） ———

func claudeTodoEventsFromToolUse(block streamContent, state *claudeStreamState) []agent.Event {
	name := strings.TrimSpace(block.Name)
	switch name {
	case "TodoWrite":
		items := todoItemsFromClaudeTodos(block.Input["todos"])
		if len(items) == 0 {
			return nil
		}
		return []agent.Event{*events.NewTodoSnapshot(items)}
	case "TaskCreate":
		item := todoItemFromClaudeTaskCreate(block.Input)
		if item.Title == "" {
			return nil
		}
		if item.ID == "" {
			item.ID = strings.TrimSpace(block.ID)
		}
		if state != nil && block.ID != "" {
			if state.pendingTaskCreates == nil {
				state.pendingTaskCreates = make(map[string]events.RuntimeTodoItem)
			}
			state.pendingTaskCreates[block.ID] = item
		}
		return []agent.Event{*events.NewTodoUpdated([]events.RuntimeTodoItem{item})}
	case "TaskUpdate":
		item := todoItemFromClaudeTaskUpdate(block.Input)
		if item.ID == "" && item.Title == "" {
			return nil
		}
		return []agent.Event{*events.NewTodoUpdated([]events.RuntimeTodoItem{item})}
	}
	return nil
}

// ——— Todo 事件（tool_result 阶段） ———

func claudeTodoEventsFromToolResult(block streamContent, state *claudeStreamState) []agent.Event {
	name := ""
	if state != nil && state.toolNames != nil {
		name = state.toolNames[block.ToolUseID]
	}
	switch strings.TrimSpace(name) {
	case "TaskCreate":
		item, ok := claudeTaskItemFromResult(block.Content)
		if state != nil && state.pendingTaskCreates != nil {
			if pending, exists := state.pendingTaskCreates[block.ToolUseID]; exists {
				if item.ID == "" {
					item.ID = pending.ID
				}
				if item.Title == "" {
					item.Title = pending.Title
				}
				if item.Status == "" {
					item.Status = pending.Status
				}
				if item.Priority == "" {
					item.Priority = pending.Priority
				}
				delete(state.pendingTaskCreates, block.ToolUseID)
				ok = true
			}
		}
		if !ok || (item.ID == "" && item.Title == "") {
			return nil
		}
		return []agent.Event{*events.NewTodoUpdated([]events.RuntimeTodoItem{item})}
	case "TaskList":
		items := claudeTaskListFromResult(block.Content)
		if len(items) == 0 {
			return nil
		}
		return []agent.Event{*events.NewTodoSnapshot(items)}
	}
	return nil
}

// ——— Todo 解析辅助函数 ———

func todoItemsFromClaudeTodos(value any) []events.RuntimeTodoItem {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]events.RuntimeTodoItem, 0, len(list))
	for index, raw := range list {
		obj, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title := firstNonEmptyString(anyString(obj["content"]), anyString(obj["title"]), anyString(obj["description"]))
		if title == "" {
			continue
		}
		id := firstNonEmptyString(anyString(obj["id"]), fmt.Sprintf("todo_%d", index+1))
		items = append(items, events.RuntimeTodoItem{
			ID:       id,
			Title:    title,
			Status:   anyString(obj["status"]),
			Priority: anyString(obj["priority"]),
		})
	}
	return items
}

func todoItemFromClaudeTaskCreate(input map[string]any) events.RuntimeTodoItem {
	return events.RuntimeTodoItem{
		ID:       anyString(input["id"]),
		Title:    firstNonEmptyString(anyString(input["title"]), anyString(input["subject"]), anyString(input["description"]), anyString(input["content"])),
		Status:   firstNonEmptyString(anyString(input["status"]), "pending"),
		Priority: anyString(input["priority"]),
	}
}

func todoItemFromClaudeTaskUpdate(input map[string]any) events.RuntimeTodoItem {
	return events.RuntimeTodoItem{
		ID:       firstNonEmptyString(anyString(input["id"]), anyString(input["task_id"]), anyString(input["taskId"])),
		Title:    firstNonEmptyString(anyString(input["title"]), anyString(input["subject"]), anyString(input["description"]), anyString(input["content"])),
		Status:   anyString(input["status"]),
		Priority: anyString(input["priority"]),
	}
}

func claudeTaskItemFromResult(content any) (events.RuntimeTodoItem, bool) {
	value, ok := decodedJSONValue(content)
	if !ok {
		return events.RuntimeTodoItem{}, false
	}
	if obj, ok := value.(map[string]any); ok {
		if task, ok := obj["task"].(map[string]any); ok {
			obj = task
		}
		item := events.RuntimeTodoItem{
			ID:       firstNonEmptyString(anyString(obj["id"]), anyString(obj["task_id"]), anyString(obj["taskId"])),
			Title:    firstNonEmptyString(anyString(obj["title"]), anyString(obj["subject"]), anyString(obj["description"]), anyString(obj["content"])),
			Status:   anyString(obj["status"]),
			Priority: anyString(obj["priority"]),
		}
		return item, item.ID != "" || item.Title != ""
	}
	return events.RuntimeTodoItem{}, false
}

func claudeTaskListFromResult(content any) []events.RuntimeTodoItem {
	value, ok := decodedJSONValue(content)
	if !ok {
		return nil
	}
	var rawTasks []any
	switch typed := value.(type) {
	case []any:
		rawTasks = typed
	case map[string]any:
		for _, key := range []string{"tasks", "todos", "items"} {
			if list, ok := typed[key].([]any); ok {
				rawTasks = list
				break
			}
		}
	}
	if len(rawTasks) == 0 {
		return nil
	}
	items := make([]events.RuntimeTodoItem, 0, len(rawTasks))
	for index, raw := range rawTasks {
		obj, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title := firstNonEmptyString(anyString(obj["title"]), anyString(obj["subject"]), anyString(obj["description"]), anyString(obj["content"]))
		if title == "" {
			continue
		}
		items = append(items, events.RuntimeTodoItem{
			ID:       firstNonEmptyString(anyString(obj["id"]), anyString(obj["task_id"]), fmt.Sprintf("task_%d", index+1)),
			Title:    title,
			Status:   anyString(obj["status"]),
			Priority: anyString(obj["priority"]),
		})
	}
	return items
}

// ——— JSON 值解码 ———

func decodedJSONValue(content any) (any, bool) {
	switch typed := content.(type) {
	case nil:
		return nil, false
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil, false
		}
		var value any
		if err := sonic.Unmarshal([]byte(text), &value); err != nil {
			return nil, false
		}
		return value, true
	default:
		return typed, true
	}
}

// ——— 通用字符串工具 ———

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
