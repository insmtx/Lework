package service

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
)

func newSchedulerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	return db
}

// TestStartAutomationSchedulerNilConfig 验证未配置 automation_scheduler（cfg==nil）时：
// 默认开启且不触发 nil 指针 panic（回归：曾因 cfg.PlannerInterval 在 nil 时被访问而崩溃）。
func TestStartAutomationSchedulerNilConfig(t *testing.T) {
	poster := NewMessagePoster(newSchedulerTestDB(t), nil, nil, nil, nil, nil, "test", nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 已取消：调度 goroutine 启动即退出，不泄漏

	started := StartAutomationScheduler(ctx, newSchedulerTestDB(t), nil, poster)
	if !started {
		t.Fatalf("expected scheduler to start by default when cfg is nil")
	}
}

// TestStartAutomationSchedulerExplicitDisabled 验证显式 enabled=false 时返回 false（关闭）。
func TestStartAutomationSchedulerExplicitDisabled(t *testing.T) {
	poster := NewMessagePoster(newSchedulerTestDB(t), nil, nil, nil, nil, nil, "test", nil, nil)
	disabled := false
	cfg := &config.AutomationSchedulerConfig{Enabled: &disabled}

	started := StartAutomationScheduler(context.Background(), newSchedulerTestDB(t), cfg, poster)
	if started {
		t.Fatalf("expected disabled when enabled=false")
	}
}
