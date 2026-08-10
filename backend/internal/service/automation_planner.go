package service

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

const (
	// defaultAutomationPlannerInterval Planner 扫描间隔
	defaultAutomationPlannerInterval = 30 * time.Second
	// defaultAutomationBatchSize 单次扫描处理的到期计划上限
	defaultAutomationBatchSize = 100
	// defaultDispatchTimeout 命令投递超时时间
	defaultDispatchTimeout = 30 * time.Minute
)

// WakeSignal 用于 Planner 通知 Dispatcher 有新的 queued execution。
type WakeSignal interface {
	Wake()
}

// AutomationPlanner 扫描到期自动化并生成 execution。
//
// 不依赖进程锁：CAS 推进 next_run_at、occurrence 唯一索引、活动执行部分唯一
// 约束共同构成数据库防重边界。多实例同时扫描只会有一个实例领取成功。
type AutomationPlanner struct {
	db   *gorm.DB
	wake WakeSignal
}

// NewAutomationPlanner 构造 Planner
func NewAutomationPlanner(db *gorm.DB, wake WakeSignal) *AutomationPlanner {
	return &AutomationPlanner{db: db, wake: wake}
}

// Scan 执行一次扫描：领取到期计划并生成 queued/skipped execution。
func (p *AutomationPlanner) Scan(ctx context.Context) {
	now := time.Now().UTC()
	due, err := db.ListDueAutomations(ctx, p.db, now, defaultAutomationBatchSize)
	if err != nil {
		logs.WarnContextf(ctx, "planner list due automations failed: %v", err)
		return
	}
	if len(due) == 0 {
		return
	}

	created := 0
	for _, a := range due {
		if ctx.Err() != nil {
			break
		}
		if ok := p.processDue(ctx, a, now); ok {
			created++
		}
	}
	if created > 0 && p.wake != nil {
		p.wake.Wake()
	}
}

// processDue 处理单个到期计划，返回是否生成了新 execution（从而需要唤醒 Dispatcher）。
// processDue 处理单个到期计划，返回是否生成了 queued execution（从而需要唤醒 Dispatcher）。
//
// 游标推进与 execution 插入**分离**：
//   - 先 CAS 推进 next_run_at（独立、不因插入失败回滚）——保证即使本次周期因脏数据/冲突插入失败，
//     定时任务仍能推进到下一个可用周期，后续继续执行；
//   - 再单独插入 execution（含活跃执行判断）。
//
// 多实例防重用两层保证：① CAS 推进 next_run_at（只有更新成功的实例获得本次 occurrence）；
// ② (automation_id, occurrence_key) 唯一索引（阻止同周期重复插入）。
func (p *AutomationPlanner) processDue(ctx context.Context, a *types.Automation, now time.Time) bool {
	if a.NextRunAt == nil {
		return false
	}
	oldNext := *a.NextRunAt

	// 计算遗漏窗口：最近一次遗漏、遗漏数、未来 next
	window, err := ComputeOccurrenceWindow(&a.ScheduleSpec, oldNext, now)
	if err != nil {
		logs.WarnContextf(ctx, "planner compute window failed for automation %d: %v", a.ID, err)
		return false
	}

	// ① 先推进 next_run_at（独立事务；插入失败也不影响游标推进）
	generatedAt := time.Now().UTC()
	advanced, advErr := db.AdvanceAutomationNextRun(ctx, p.db, a.ID, oldNext, &window.Next, generatedAt)
	if advErr != nil {
		logs.WarnContextf(ctx, "planner advance next_run failed for automation %d: %v", a.ID, advErr)
		return false
	}
	if !advanced {
		// 已被其它实例处理本次 occurrence（CAS 未命中）
		return false
	}

	// ② 再插入 execution（单独调用；失败只影响本次周期，不阻断后续）
	queued, exErr := createAutomationExecution(ctx, p.db, a, window, now)
	if exErr != nil {
		logs.WarnContextf(ctx, "planner create execution failed for automation %d（本次周期跳过，next_run_at 已推进）: %v", a.ID, exErr)
		return false
	}
	return queued
}

// createAutomationExecution 完成「活跃执行判断 + 插入 execution」。
//
// scheduled_at / occurrence_key 用 window.LatestDue（最近一次理论 occurrence）；
// not_after 用调度生成时刻 + 投递宽限（避免恢复较晚时刚创建就过期）；
// last_run_at 由调用方在推进 next_run_at 时写入生成时刻。
// 多实例防重由 (automation_id, occurrence_key) 唯一索引兜底。
func createAutomationExecution(ctx context.Context, database *gorm.DB, a *types.Automation, window *OccurrenceWindow, now time.Time) (bool, error) {
	active, err := db.HasActiveExecution(ctx, database, a.ID)
	if err != nil {
		return false, err
	}
	status := types.AutomationExecutionQueued
	if active {
		status = types.AutomationExecutionSkipped
	}

	// LatestDue 由 ComputeOccurrenceWindow 保证为真实计划时间（非零）；直接使用。
	// 不在其为零值时回退到扫描时刻——那正是"提前执行"的根因。
	latestDue := window.LatestDue.UTC()
	notAfter := now.UTC().Add(defaultDispatchTimeout)

	execution := &types.AutomationExecution{
		OrgID:               a.OrgID,
		AutomationID:        a.ID,
		OwnerID:             a.OwnerID,
		PublicID:            "autoexec_" + newAutoExecPublicID(),
		TriggerType:         types.AutomationTriggerScheduled,
		OccurrenceKey:       latestDue.Format(time.RFC3339Nano),
		Status:              status,
		ScheduledAt:         latestDue,
		NotAfter:            &notAfter,
		NameSnapshot:        a.Name,
		InstructionSnapshot: a.Instruction,
		AssistantIDSnapshot: a.AssistantID,
		MissedCount:         window.MissedCount,
	}
	if err := db.CreateAutomationExecution(ctx, database, execution); err != nil {
		return false, err
	}
	return status == types.AutomationExecutionQueued, nil
}
