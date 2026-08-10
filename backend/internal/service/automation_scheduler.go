package service

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/ygpkg/yg-go/logs"
)

// schedulerWakeSignal 进程内的 wake 通道实现，供 Planner 通知 Dispatcher。
type schedulerWakeSignal struct {
	ch chan struct{}
}

func newSchedulerWakeSignal() *schedulerWakeSignal {
	return &schedulerWakeSignal{ch: make(chan struct{}, 1)}
}

func (w *schedulerWakeSignal) Wake() {
	select {
	case w.ch <- struct{}{}:
	default:
		// 非阻塞：channel 已有待消费信号时丢弃本次
	}
}

// StartAutomationScheduler 启动自动化 Planner/Dispatcher 后台调度循环。
//
// 仅在 server.automation_scheduler.enabled 为 true 时启动。失败返回 false，
// 调用方据此决定是否记录告警，不影响进程启动。
func StartAutomationScheduler(
	ctx context.Context,
	database *gorm.DB,
	cfg *config.AutomationSchedulerConfig,
	poster *MessagePoster,
) bool {
	if database == nil {
		return false
	}
	// 开关默认开启：未配置 automation_scheduler 或 enabled 未显式设为 false 时都启动调度。
	if cfg != nil && cfg.Enabled != nil && !*cfg.Enabled {
		logs.WarnContextf(ctx, "automation scheduler disabled by config")
		return false
	}
	// cfg 为 nil（未配置 automation_scheduler 块）时按默认开启处理，用默认配置避免后续 nil 解引用。
	if cfg == nil {
		cfg = &config.AutomationSchedulerConfig{}
	}
	if poster == nil {
		logs.WarnContextf(ctx, "automation scheduler disabled: poster is nil")
		return false
	}

	provisioner := NewAutomationProjectProvisioner(database, poster.giteaClient, poster.giteaCfg, poster.env)
	wake := newSchedulerWakeSignal()
	planner := NewAutomationPlanner(database, wake)
	dispatcher := NewAutomationDispatcher(database, poster, provisioner)

	plannerInterval := defaultAutomationPlannerInterval
	if cfg.PlannerInterval > 0 {
		plannerInterval = time.Duration(cfg.PlannerInterval) * time.Second
	}

	// Planner 每 interval 扫描 + 被 wake 唤醒
	go func() {
		ticker := time.NewTicker(plannerInterval)
		defer ticker.Stop()
		planner.Scan(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				planner.Scan(ctx)
			}
		}
	}()

	// Dispatcher 轮询 queued execution + 被 wake 唤醒
	go func() {
		ticker := time.NewTicker(defaultExecutionPollInterval)
		defer ticker.Stop()
		dispatcher.Dispatch(ctx, time.Now().UTC())
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake.ch:
				_ = dispatcher.Dispatch(ctx, time.Now().UTC())
			case <-ticker.C:
				_ = dispatcher.Dispatch(ctx, time.Now().UTC())
			}
		}
	}()

	logs.InfoContextf(ctx, "automation scheduler started: planner_interval=%s", plannerInterval)
	return true
}
