package service

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

// TestCreateAutomationPreservesDisabled verifies that an explicit false survives
// the service, ORM insert, and a fresh database read.
func TestCreateAutomationPreservesDisabled(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&types.Automation{}, &types.WorkerDeployment{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := database.Create(&types.WorkerDeployment{
		OrgID:              1,
		DigitalAssistantID: 1,
		WorkerID:           1,
		DeploymentName:     "worker-default",
		Status:             string(types.WorkerDeploymentStatusReady),
		Namespace:          "default",
		BootstrapTokenHash: "",
		WorkspacePath:      "",
	}).Error; err != nil {
		t.Fatalf("create worker deployment: %v", err)
	}

	caller := &types.Caller{Uin: 7, OrgID: 1, State: types.AuthStateSucc}
	ctx := auth.WithContext(context.Background(), caller, nil)
	disabled := false
	created, err := NewAutomationService(database, nil).CreateAutomation(ctx, &contract.CreateAutomationRequest{
		Name:         "停用自动化",
		Instruction:  "测试指令",
		Enabled:      &disabled,
		ScheduleMode: "interval",
		Schedule: &types.AutomationScheduleFormConfig{
			Mode:     "interval",
			Timezone: "Asia/Shanghai",
			Interval: &types.AutomationIntervalConfig{IntervalMinutes: 30},
		},
		Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("create automation: %v", err)
	}
	if created.Enabled {
		t.Fatalf("created automation should be disabled")
	}
	if created.NextRunAt != nil {
		t.Fatalf("disabled automation should not have next_run_at: %v", created.NextRunAt)
	}

	var persisted types.Automation
	if err := database.Where("public_id = ?", created.PublicID).First(&persisted).Error; err != nil {
		t.Fatalf("reload automation: %v", err)
	}
	if persisted.Enabled == nil || *persisted.Enabled {
		t.Fatalf("persisted automation should be disabled, got %#v", persisted.Enabled)
	}

	defaultCreated, err := NewAutomationService(database, nil).CreateAutomation(ctx, &contract.CreateAutomationRequest{
		Name:         "默认启用自动化",
		Instruction:  "测试默认状态",
		ScheduleMode: "interval",
		Schedule: &types.AutomationScheduleFormConfig{
			Mode:     "interval",
			Timezone: "Asia/Shanghai",
			Interval: &types.AutomationIntervalConfig{IntervalMinutes: 30},
		},
		Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("create default automation: %v", err)
	}
	if !defaultCreated.Enabled {
		t.Fatalf("omitted enabled should default to true")
	}
	if defaultCreated.NextRunAt == nil {
		t.Fatalf("enabled automation should have next_run_at")
	}
}
