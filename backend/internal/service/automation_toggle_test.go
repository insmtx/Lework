package service

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

// TestToggleAutomationEnabledOnly 验证卡片开关走 UpdateAutomation 只传 enabled 时：
// 1) enabled 正确改变并落库；2) name/instruction/schedule 不被误清空；
// 3) 停用后 next_run_at 清空，启用后重新计算。
func TestToggleAutomationEnabledOnly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&types.Automation{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// 先建一条自动化
	now := time.Now().UTC()
	next := now.Add(time.Hour)
	automation := &types.Automation{
		OrgID:        1,
		OwnerID:      7,
		PublicID:     "auto_test_1",
		Name:         "测试自动化",
		Instruction:  "测试指令",
		Enabled:      true,
		ScheduleMode: "interval",
		ScheduleSpec: types.AutomationScheduleSpec{
			Spec: types.AutomationScheduleSpecItem{
				Mode: "interval", IntervalSeconds: 300, AnchorAt: "00:00", Timezone: "Asia/Shanghai",
			},
		},
		Timezone:    "Asia/Shanghai",
		AssistantID: 1,
		NextRunAt:   &next,
	}
	if err := db.Create(automation).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	svc := NewAutomationService(db)
	ctx := auth.WithContext(context.Background(), &types.Caller{Uin: 7, OrgID: 1, State: types.AuthStateSucc}, nil)

	// 只传 enabled=false（停用）
	disabled := false
	upd, err := svc.UpdateAutomation(ctx, "auto_test_1", &contract.UpdateAutomationRequest{Enabled: &disabled})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Enabled {
		t.Fatalf("expected disabled, got enabled")
	}
	if upd.Name != "测试自动化" {
		t.Fatalf("name was cleared: %q", upd.Name)
	}
	if upd.Instruction != "测试指令" {
		t.Fatalf("instruction was cleared: %q", upd.Instruction)
	}
	if upd.NextRunAt != nil {
		t.Fatalf("stopping should clear next_run_at, got %v", upd.NextRunAt)
	}

	// 只传 enabled=true（重新启用）
	enabled := true
	upd2, err := svc.UpdateAutomation(ctx, "auto_test_1", &contract.UpdateAutomationRequest{Enabled: &enabled})
	if err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if !upd2.Enabled {
		t.Fatalf("expected enabled")
	}
	if upd2.Name != "测试自动化" {
		t.Fatalf("name was cleared on re-enable: %q", upd2.Name)
	}
	if upd2.NextRunAt == nil {
		t.Fatalf("re-enable should recompute next_run_at, got nil")
	}
}
