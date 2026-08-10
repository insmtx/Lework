// 自动化 Run 状态投影到 AutomationExecution 与 cron Task。
//
// 自动化执行复用了统一的 run 状态事件；当 run 对应的 Task 携带 AutomationExecutionID
// 时，把执行生命周期（queued→running→succeeded/failed/cancelled）回写到 execution。
package runnable

import (
	"context"
	"time"

	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// RunTerminalState 描述 run 到达终态时对 automation 的落库状态。
type runTerminalState struct {
	Status     types.AutomationExecutionStatus
	TaskStatus string
	ErrorCode  string
	ErrorMsg   string
}

var runTerminalMap = map[messaging.RunEventType]runTerminalState{
	messaging.RunEventRunStarted:   {Status: types.AutomationExecutionRunning, TaskStatus: string(types.TaskStatusInProgress)},
	messaging.RunEventRunCompleted: {Status: types.AutomationExecutionSucceeded, TaskStatus: string(types.TaskStatusCompleted)},
	messaging.RunEventRunFailed:    {Status: types.AutomationExecutionFailed, TaskStatus: string(types.TaskStatusFailed)},
	messaging.RunEventRunCancelled: {Status: types.AutomationExecutionFailed, TaskStatus: string(types.TaskStatusCancelled)},
}

// handleAutomationRunEvent 按 run 事件更新对应 AutomationExecution 与 cron Task。
//
// 通过 runEvent.Trace.TaskID（任务 public_id）反查；仅当 Task 携带 AutomationExecutionID 时生效。
// 普通任务不受影响。
func handleAutomationRunEvent(ctx context.Context, database *gorm.DB, runEvent messaging.RunEvent) {
	if database == nil {
		return
	}
	taskPublicID := runEvent.Trace.TaskID
	if taskPublicID == "" {
		return
	}
	state, ok := runTerminalMap[runEvent.Body.Event]
	if !ok {
		return
	}

	task, err := infradb.GetTaskByPublicID(ctx, database, runEvent.Route.OrgID, taskPublicID)
	if err != nil {
		logs.WarnContextf(ctx, "automation projection get task %s failed: %v", taskPublicID, err)
		return
	}
	if task == nil || task.AutomationExecutionID == nil {
		return
	}

	execID := *task.AutomationExecutionID
	execution, err := infradb.GetAutomationExecutionByID(ctx, database, execID)
	if err != nil || execution == nil {
		logs.WarnContextf(ctx, "automation projection get execution %d failed: task=%s", execID, taskPublicID)
		return
	}

	now := time.Now().UTC()

	// 终态保护：
	//   - 执行已进入终态（succeeded/failed/skipped）后，忽略迟到的 run 事件，不可逆转。
	//   - 已标记失败的执行不允许被迟到的 run.started 恢复为 running。
	if isExecutionTerminal(execution.Status) {
		logs.DebugContextf(ctx, "automation projection ignore late run event: execution=%d status=%s event=%s",
			execID, execution.Status, runEvent.Body.Event)
		return
	}

	// Start 保护：StartedAt 只在第一次 run.started 时写入。
	switch state.Status {
	case types.AutomationExecutionRunning:
		if execution.StartedAt == nil {
			t := now
			execution.StartedAt = &t
		}
		if execution.RunID == "" {
			execution.RunID = runEvent.Trace.RunID
		}
		execution.Status = types.AutomationExecutionRunning
	case types.AutomationExecutionSucceeded:
		execution.Status = types.AutomationExecutionSucceeded
		t := now
		execution.FinishedAt = &t
		execution.ErrorCode = ""
		execution.ErrorMsg = ""
	case types.AutomationExecutionFailed:
		execution.Status = types.AutomationExecutionFailed
		t := now
		execution.FinishedAt = &t
		if runEvent.Body.Event == messaging.RunEventRunCancelled {
			execution.ErrorCode = "run_cancelled"
			execution.ErrorMsg = "执行已取消"
		} else if runEvent.Body.Error != nil {
			execution.ErrorCode = runEvent.Body.Error.Code
			execution.ErrorMsg = runEvent.Body.Error.Message
		}
	}

	if err := infradb.UpdateAutomationExecution(ctx, database, execution); err != nil {
		logs.WarnContextf(ctx, "automation projection update execution %d failed: %v", execID, err)
		return
	}

	// 更新 cron Task 状态
	if err := database.WithContext(ctx).Model(&types.Task{}).Where("id = ?", task.ID).
		Update("status", state.TaskStatus).Error; err != nil {
		logs.WarnContextf(ctx, "automation projection update task %d failed: %v", task.ID, err)
	}
}

// isExecutionTerminal 判断执行状态是否为终态。
func isExecutionTerminal(status types.AutomationExecutionStatus) bool {
	return status == types.AutomationExecutionSucceeded ||
		status == types.AutomationExecutionFailed ||
		status == types.AutomationExecutionSkipped
}
