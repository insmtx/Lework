package todo

import "github.com/insmtx/Leros/backend/agent/runtime/events"

// ToolName is the canonical name used by the planning tool.
const ToolName = "todo"

// Status 表示待办事项的状态。
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusCancelled  Status = "cancelled"
)

// Mode 描述解析后的待办输出是快照还是更新。
type Mode string

const (
	ModeSnapshot Mode = "snapshot"
	ModeUpdate   Mode = "update"
)

// ParseResult 是运行时待办更新的标准化解析结果。
type ParseResult struct {
	Items []events.RuntimeTodoItem
	Mode  Mode
	Merge bool
}
