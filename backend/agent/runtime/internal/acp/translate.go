package acp

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/insmtx/Leros/backend/agent"
)

const (
	// MaxToolResultBytes 与 v1 一致（events.go:20），超长工具输出在此截断。
	MaxToolResultBytes = 32 * 1024
	// childSessionMetaKey 是上游压平子会话时附加的 _meta 键（§5.6）。
	childSessionMetaKey  = "opencode/child-session"
	reasoningIDSeparator = ":reasoning:"
)

// filteredToolNames 沿用 v1 的过滤名单，但移除 v2 已不存在的 plan_exit。
var filteredToolNames = map[string]struct{}{
	"question":         {},
	"todowrite":        {},
	"artifact_declare": {},
}

// ToolNameFromUpdate 从 ACP 帧推导 v2 规范工具名（§5.5）：
// execute 走 Kind，其余按 Title；子会话标题前缀 `<childTitle>: ` 先剥掉。
func ToolNameFromUpdate(kind, title string) string {
	if kind == string(acpsdk.ToolKindExecute) {
		return "shell"
	}
	title = strings.TrimSpace(title)
	if idx := strings.LastIndex(title, ": "); idx >= 0 {
		title = strings.TrimSpace(title[idx+2:])
	}
	return title
}

// displayToolNames 把 v2 规范名反向映射为前端已认识的 v1 名（§5.5）。
var displayToolNames = map[string]string{
	"shell":    "bash",
	"subagent": "task",
	"patch":    "apply_patch",
}

// DisplayToolName 保持工具卡标题与 v1 一致。
func DisplayToolName(name string) string {
	if display, ok := displayToolNames[name]; ok {
		return display
	}
	return name
}

// IsFilteredTool 判断工具是否属于「只走交互事件、不产工具卡」的名单。
func IsFilteredTool(name string) bool {
	_, ok := filteredToolNames[name]
	return ok
}

// splitReasoningMessageID 拆出 `:reasoning:<ordinal>` 后缀（§5.3）。
func splitReasoningMessageID(messageID string) (base, ordinal string) {
	if idx := strings.LastIndex(messageID, reasoningIDSeparator); idx >= 0 {
		return messageID[:idx], messageID[idx+len(reasoningIDSeparator):]
	}
	return messageID, "0"
}

// toolContentText 只取 text 段（图片/终端/diff 丢弃），避免单帧接近 10 MiB。
func toolContentText(contents []acpsdk.ToolCallContent) string {
	var builder strings.Builder
	for _, content := range contents {
		if content.Content == nil || content.Content.Content.Text == nil {
			continue
		}
		builder.WriteString(content.Content.Content.Text.Text)
	}
	return builder.String()
}

// TruncateToolOutput 沿用 v1 的 32 KiB + UTF-8 边界修正 + 尾部标记。
func TruncateToolOutput(output string) string {
	if len(output) <= MaxToolResultBytes {
		return output
	}
	truncated := output[:MaxToolResultBytes]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated + fmt.Sprintf("\n...[truncated %d bytes]", len(output)-len(truncated))
}

// ParseTodos 从 todowrite 的 RawInput 解析待办列表。
// 上游字段是 content/status/priority（不是 title）；ID 按序号生成 todo_<n>。
func ParseTodos(raw any) []agent.RuntimeTodoItem {
	object, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	list, ok := object["todos"].([]any)
	if !ok || len(list) == 0 {
		return nil
	}
	items := make([]agent.RuntimeTodoItem, 0, len(list))
	for index, entry := range list {
		todo, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		title := firstStringField(todo, "content", "title", "text")
		if title == "" {
			continue
		}
		items = append(items, agent.RuntimeTodoItem{
			ID:       fmt.Sprintf("todo_%d", index),
			Title:    title,
			Status:   normalizeTodoStatus(firstStringField(todo, "status")),
			Priority: firstStringField(todo, "priority"),
		})
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func firstStringField(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeTodoStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "in_progress", "in-progress", "running", "active", "started":
		return "in_progress"
	case "completed", "complete", "done", "success":
		return "completed"
	case "cancelled", "canceled", "deleted", "declined":
		return "cancelled"
	default:
		return "pending"
	}
}

// marshalArguments 把 RawInput 原样编码为 ToolExecutionStartPayload.Arguments。
func marshalArguments(raw any) json.RawMessage {
	if raw == nil {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	return encoded
}

// marshalResult 把工具输出编码为前端期望的 JSON 字符串。
func marshalResult(text string) json.RawMessage {
	encoded, err := json.Marshal(text)
	if err != nil {
		return nil
	}
	return encoded
}
