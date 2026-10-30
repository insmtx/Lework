// 任务状态投影：把 run.state 事件回写到普通任务。
//
// 自动化任务继续由 automation_run_projection 负责 cron 语义；本投影器只处理
// 任务自己的生命周期，让手动任务在 run 结束后进入 pending_acceptance。
package runnable

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// StartTaskRunStateProjector 订阅 run.state lane，把 run 终态投影到任务表。
func StartTaskRunStateProjector(ictx context.Context, eb eventbus.EventBus, database *gorm.DB) {
	ctx := logs.WithContextFields(ictx, "runnable", "task_run_state_projector")
	topic := messaging.RunEventStateWildcard()
	logs.InfoContextf(ctx, "starting task run state projector: %s", topic)

	Run(ctx, "task_run_state_projector", func(ctx context.Context) {
		if err := eb.SubscribeManualDurable(ctx, topic, messaging.TaskRunStateConsumer(), func(msg *nats.Msg) {
			handleTaskRunStateDelivery(ctx, database, msg)
		}); err != nil {
			logs.ErrorContextf(ctx, "subscribe to %s failed: %v", topic, err)
		}
	})
}

func handleTaskRunStateDelivery(ctx context.Context, database *gorm.DB, msg *nats.Msg) {
	delivery := eventbus.NewManualDelivery(msg)
	var event messaging.RunEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil ||
		event.Type != messaging.MessageTypeRunEvent ||
		event.ID == "" ||
		strings.TrimSpace(event.Trace.TaskID) == "" {
		logs.WarnContextf(ctx, "discard malformed task run state event: %v", err)
		_ = delivery.Term()
		return
	}

	applied, err := projectTaskRunEvent(ctx, database, event)
	if err != nil {
		logs.WarnContextf(ctx, "project task run event id=%s task=%s: %v", event.ID, event.Trace.TaskID, err)
		_ = delivery.NakWithDelay(5 * time.Second)
		return
	}
	if applied {
		logs.InfoContextf(ctx, "projected task run event id=%s task=%s event=%s", event.ID, event.Trace.TaskID, event.Body.Event)
	}
	if err := delivery.Ack(); err != nil {
		logs.WarnContextf(ctx, "ack task run event id=%s: %v", event.ID, err)
	}
}

// projectTaskRunEvent 在同一个事务内写 receipt 和任务状态，保证重复投递只生效一次。
func projectTaskRunEvent(ctx context.Context, database *gorm.DB, event messaging.RunEvent) (bool, error) {
	taskID := strings.TrimSpace(event.Trace.TaskID)
	if database == nil || taskID == "" {
		return false, nil
	}
	sessionID := strings.TrimSpace(event.Route.SessionID)
	if sessionID == "" {
		sessionID = "task:" + taskID
	}

	var applied bool
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		inserted, err := infradb.CreateProjectionReceipt(ctx, tx, &types.ProjectionReceipt{
			Consumer:  messaging.TaskRunStateConsumer(),
			EventID:   event.ID,
			RunID:     event.Trace.RunID,
			SessionID: sessionID,
			EventType: string(event.Body.Event),
		})
		if err != nil || !inserted {
			return err
		}
		applied = true
		return applyTaskRunState(ctx, tx, event)
	})
	return applied, err
}

func applyTaskRunState(ctx context.Context, database *gorm.DB, event messaging.RunEvent) error {
	taskID := strings.TrimSpace(event.Trace.TaskID)
	if taskID == "" {
		return nil
	}
	task, err := infradb.GetTaskByPublicID(ctx, database, event.Route.OrgID, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return nil
	}

	desired, ok := projectedTaskStatus(task, event.Body.Event)
	if !ok || desired == "" || task.Status == string(desired) {
		return nil
	}
	if !taskStatusAllowsRunProjection(types.TaskStatus(task.Status)) {
		return nil
	}

	updates := map[string]interface{}{"status": string(desired)}
	if event.Body.Event == messaging.RunEventRunCompleted && event.Body.RunCompleted != nil {
		if summary := strings.TrimSpace(event.Body.RunCompleted.Result.Message); summary != "" {
			updates["result_summary"] = truncateRunes(summary, 4000)
		}
	}
	return database.WithContext(ctx).
		Model(&types.Task{}).
		Where("id = ?", task.ID).
		Updates(updates).Error
}

func projectedTaskStatus(task *types.Task, event messaging.RunEventType) (types.TaskStatus, bool) {
	switch event {
	case messaging.RunEventRunStarted:
		return types.TaskStatusInProgress, true
	case messaging.RunEventRunCompleted:
		if task.TaskType == types.TaskTypeCron {
			return types.TaskStatusCompleted, true
		}
		return types.TaskStatusPendingAcceptance, true
	case messaging.RunEventRunFailed:
		return types.TaskStatusFailed, true
	case messaging.RunEventRunCancelled:
		return types.TaskStatusCancelled, true
	default:
		return "", false
	}
}

// taskStatusAllowsRunProjection 只允许活跃状态被 run 事件推进。
// pending_acceptance 与终态不会被迟到的 started/failed 事件回退。
func taskStatusAllowsRunProjection(status types.TaskStatus) bool {
	switch status {
	case types.TaskStatusCreated,
		types.TaskStatusPendingAssignment,
		types.TaskStatusInProgress,
		types.TaskStatusPendingApproval,
		types.TaskStatusPaused:
		return true
	default:
		return false
	}
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
