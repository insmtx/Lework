package runnable

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

func setupTaskProjectorDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano())
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.Task{}, &types.ProjectionReceipt{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	return database
}

func seedProjectorTask(t *testing.T, database *gorm.DB, publicID string, taskType types.TaskType, status types.TaskStatus) *types.Task {
	t.Helper()
	task := &types.Task{
		PublicID:  publicID,
		OrgID:     1,
		OwnerID:   1,
		ProjectID: 10,
		TaskType:  taskType,
		Title:     "projector task",
		Status:    string(status),
	}
	if err := database.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func taskRunEvent(event messaging.RunEventType, taskID, eventID string) messaging.RunEvent {
	return messaging.RunEvent{
		ID:   eventID,
		Type: messaging.MessageTypeRunEvent,
		Route: messaging.RouteContext{
			OrgID:     1,
			SessionID: "sess_projector",
		},
		Trace: messaging.TraceContext{
			TaskID: taskID,
			RunID:  "run_projector",
		},
		Body: messaging.RunEventBody{Event: event},
	}
}

func TestApplyTaskRunState(t *testing.T) {
	tests := []struct {
		name     string
		event    messaging.RunEventType
		taskType types.TaskType
		from     types.TaskStatus
		want     types.TaskStatus
	}{
		{name: "started moves created forward", event: messaging.RunEventRunStarted, taskType: types.TaskTypeGeneral, from: types.TaskStatusCreated, want: types.TaskStatusInProgress},
		{name: "started does not move pending acceptance backward", event: messaging.RunEventRunStarted, taskType: types.TaskTypeGeneral, from: types.TaskStatusPendingAcceptance, want: types.TaskStatusPendingAcceptance},
		{name: "completed general waits for acceptance", event: messaging.RunEventRunCompleted, taskType: types.TaskTypeGeneral, from: types.TaskStatusInProgress, want: types.TaskStatusPendingAcceptance},
		{name: "completed cron stays completed", event: messaging.RunEventRunCompleted, taskType: types.TaskTypeCron, from: types.TaskStatusInProgress, want: types.TaskStatusCompleted},
		{name: "completed does not overwrite terminal", event: messaging.RunEventRunCompleted, taskType: types.TaskTypeGeneral, from: types.TaskStatusCompleted, want: types.TaskStatusCompleted},
		{name: "failed marks active task failed", event: messaging.RunEventRunFailed, taskType: types.TaskTypeGeneral, from: types.TaskStatusInProgress, want: types.TaskStatusFailed},
		{name: "failed does not overwrite completed", event: messaging.RunEventRunFailed, taskType: types.TaskTypeGeneral, from: types.TaskStatusCompleted, want: types.TaskStatusCompleted},
		{name: "failed does not overwrite pending acceptance", event: messaging.RunEventRunFailed, taskType: types.TaskTypeGeneral, from: types.TaskStatusPendingAcceptance, want: types.TaskStatusPendingAcceptance},
		{name: "cancelled marks active task cancelled", event: messaging.RunEventRunCancelled, taskType: types.TaskTypeGeneral, from: types.TaskStatusInProgress, want: types.TaskStatusCancelled},
		{name: "cancelled does not overwrite completed", event: messaging.RunEventRunCancelled, taskType: types.TaskTypeGeneral, from: types.TaskStatusCompleted, want: types.TaskStatusCompleted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := setupTaskProjectorDB(t)
			task := seedProjectorTask(t, database, "task_apply", tt.taskType, tt.from)
			event := taskRunEvent(tt.event, task.PublicID, "evt_apply")
			if err := applyTaskRunState(context.Background(), database, event); err != nil {
				t.Fatalf("apply: %v", err)
			}
			var got types.Task
			if err := database.First(&got, task.ID).Error; err != nil {
				t.Fatalf("load task: %v", err)
			}
			if types.TaskStatus(got.Status) != tt.want {
				t.Fatalf("status = %s, want %s", got.Status, tt.want)
			}
		})
	}
}

func TestProjectTaskRunEventStoresResultSummary(t *testing.T) {
	database := setupTaskProjectorDB(t)
	task := seedProjectorTask(t, database, "task_summary", types.TaskTypeGeneral, types.TaskStatusInProgress)
	event := taskRunEvent(messaging.RunEventRunCompleted, task.PublicID, "evt_summary")
	event.Body.RunCompleted = &messaging.RunCompletedPayload{
		Result: messaging.RunResultPayload{Message: "已修复并补充测试"},
	}
	applied, err := projectTaskRunEvent(context.Background(), database, event)
	if err != nil || !applied {
		t.Fatalf("project: applied=%v err=%v", applied, err)
	}
	var got types.Task
	if err := database.First(&got, task.ID).Error; err != nil {
		t.Fatalf("load task: %v", err)
	}
	if got.Status != string(types.TaskStatusPendingAcceptance) {
		t.Fatalf("status = %s", got.Status)
	}
	if got.ResultSummary != "已修复并补充测试" {
		t.Fatalf("result_summary = %q", got.ResultSummary)
	}
}

func TestProjectTaskRunEventIsIdempotent(t *testing.T) {
	database := setupTaskProjectorDB(t)
	task := seedProjectorTask(t, database, "task_idempotent", types.TaskTypeGeneral, types.TaskStatusInProgress)
	event := taskRunEvent(messaging.RunEventRunCompleted, task.PublicID, "evt_same")

	applied, err := projectTaskRunEvent(context.Background(), database, event)
	if err != nil || !applied {
		t.Fatalf("first project: applied=%v err=%v", applied, err)
	}
	if err := database.Model(&types.Task{}).Where("id = ?", task.ID).Update("status", string(types.TaskStatusCompleted)).Error; err != nil {
		t.Fatalf("force completed: %v", err)
	}
	applied, err = projectTaskRunEvent(context.Background(), database, event)
	if err != nil {
		t.Fatalf("second project: %v", err)
	}
	if applied {
		t.Fatal("duplicate event must not be applied")
	}
	var got types.Task
	if err := database.First(&got, task.ID).Error; err != nil {
		t.Fatalf("load task: %v", err)
	}
	if got.Status != string(types.TaskStatusCompleted) {
		t.Fatalf("duplicate event changed status to %s", got.Status)
	}
}
