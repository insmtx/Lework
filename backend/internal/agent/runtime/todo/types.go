package todo

import "github.com/insmtx/Leros/backend/internal/agent/runtime/events"

// RuntimeTodoItem 是运行期 todo 的标准条目结构。
type RuntimeTodoItem = events.RuntimeTodoItem

// Status 表示运行期 todo 条目的统一状态。
type Status string

const (
	// StatusPending 表示条目尚未开始。
	StatusPending Status = "pending"
	// StatusInProgress 表示条目正在执行。
	StatusInProgress Status = "in_progress"
	// StatusCompleted 表示条目已经完成。
	StatusCompleted Status = "completed"
	// StatusCancelled 表示条目已取消。
	StatusCancelled Status = "cancelled"
)

// Mode 表示 parser 输出的是完整快照还是增量更新。
type Mode string

const (
	// ModeSnapshot 表示用完整列表替换当前 todo。
	ModeSnapshot Mode = "snapshot"
	// ModeUpdate 表示将条目更新到当前 todo。
	ModeUpdate Mode = "update"
)

// ParseResult 是 CLI provider parser 输出给 runtime todo 层的统一结果。
type ParseResult struct {
	Items []RuntimeTodoItem
	Mode  Mode
	Merge bool
}
