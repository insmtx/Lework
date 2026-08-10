// 定义 AutomationExecution 及 Planner/Dispatcher 所需的数据访问层
package db

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// CreateAutomationExecution 创建执行记录
func CreateAutomationExecution(ctx context.Context, db *gorm.DB, ex *types.AutomationExecution) error {
	return db.WithContext(ctx).Create(ex).Error
}

// GetAutomationExecutionByID 按主键查询执行记录
func GetAutomationExecutionByID(ctx context.Context, db *gorm.DB, id uint) (*types.AutomationExecution, error) {
	var entity types.AutomationExecution
	err := db.WithContext(ctx).First(&entity, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetAutomationExecutionByPublicID 按对外 ID 查询执行记录
func GetAutomationExecutionByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string) (*types.AutomationExecution, error) {
	var entity types.AutomationExecution
	err := db.WithContext(ctx).Where("org_id = ? AND public_id = ?", orgID, publicID).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// UpdateAutomationExecution 全量更新执行记录
func UpdateAutomationExecution(ctx context.Context, db *gorm.DB, ex *types.AutomationExecution) error {
	return db.WithContext(ctx).Save(ex).Error
}

// ListDueAutomations 返回所有已到期且启用的自动化（Planner 扫描候选）。
func ListDueAutomations(ctx context.Context, db *gorm.DB, now time.Time, limit int) ([]*types.Automation, error) {
	var entities []*types.Automation
	err := db.WithContext(ctx).
		Where("enabled = true AND next_run_at IS NOT NULL AND next_run_at <= ? AND deleted_at IS NULL", now).
		Order("next_run_at ASC").
		Limit(limit).
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// AdvanceAutomationNextRun 以 CAS 方式推进自动化的 next_run_at。
//
// 条件：id 匹配、enabled 仍为 true、next_run_at 仍等于预期旧值。
// 只有一个并发实例能更新成功；返回是否成功（其它实例视为未领取到本次 occurrence）。
func AdvanceAutomationNextRun(ctx context.Context, db *gorm.DB,
	id uint, expectedNextRun time.Time, newNextRun *time.Time, lastRunAt time.Time) (bool, error) {
	res := db.WithContext(ctx).
		Model(&types.Automation{}).
		Where("id = ? AND enabled = true AND deleted_at IS NULL AND next_run_at = ?", id, expectedNextRun).
		Updates(map[string]interface{}{
			"next_run_at": newNextRun,
			"last_run_at": lastRunAt,
			"updated_at":  time.Now(),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// HasActiveExecution 判断同一 automation 是否存在 queued/running 的执行记录。
func HasActiveExecution(ctx context.Context, db *gorm.DB, automationID uint) (bool, error) {
	var count int64
	err := db.WithContext(ctx).
		Model(&types.AutomationExecution{}).
		Where("automation_id = ? AND status IN ? AND deleted_at IS NULL",
			automationID, []string{string(types.AutomationExecutionQueued), string(types.AutomationExecutionRunning)}).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// AcquireExecutionLease 尝试以租约方式领取一个 queued 执行记录。
//
// 条件：id 匹配、状态为 queued、租约已过期（lease_until IS NULL 或 <= now）、未投递（dispatched_at IS NULL）。
// 成功后 lease_owner 写为 owner，lease_until 写为 now+window。
func AcquireExecutionLease(ctx context.Context, db *gorm.DB, id uint, owner string, leaseFor time.Duration, now time.Time) (bool, error) {
	leaseUntil := now.Add(leaseFor)
	res := db.WithContext(ctx).
		Model(&types.AutomationExecution{}).
		Where("id = ? AND status = ? AND (lease_until IS NULL OR lease_until <= ?) AND dispatched_at IS NULL AND deleted_at IS NULL",
			id, string(types.AutomationExecutionQueued), now).
		Updates(map[string]interface{}{
			"lease_owner": owner,
			"lease_until": leaseUntil,
			"updated_at":  time.Now(),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// ListLeasableQueuedExecutions 返回所有可被派发的 queued 执行（Planner/Dispatcher 轮询候选）。
func ListLeasableQueuedExecutions(ctx context.Context, db *gorm.DB, now time.Time, limit int) ([]*types.AutomationExecution, error) {
	var entities []*types.AutomationExecution
	err := db.WithContext(ctx).
		Where("status = ? AND (lease_until IS NULL OR lease_until <= ?) AND deleted_at IS NULL",
			string(types.AutomationExecutionQueued), now).
		Order("created_at ASC").
		Limit(limit).
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// GetExecutionByOccurrenceKey 幂等防重：按 (automation_id, occurrence_key) 查询执行记录。
func GetExecutionByOccurrenceKey(ctx context.Context, db *gorm.DB, automationID uint, occurrenceKey string) (*types.AutomationExecution, error) {
	var entity types.AutomationExecution
	err := db.WithContext(ctx).
		Where("automation_id = ? AND occurrence_key = ?", automationID, occurrenceKey).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ListAutomationExecutions 分页查询某自动化的执行历史（可按状态筛选）。
func ListAutomationExecutions(ctx context.Context, db *gorm.DB, automationID uint, status *string, offset, limit int) ([]*types.AutomationExecution, int64, error) {
	var entities []*types.AutomationExecution
	var total int64

	query := db.WithContext(ctx).
		Model(&types.AutomationExecution{}).
		Where("automation_id = ? AND deleted_at IS NULL", automationID)
	if status != nil && *status != "" {
		query = query.Where("status = ?", *status)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&entities).Error
	if err != nil {
		return nil, 0, err
	}
	return entities, total, nil
}

// SetExecutionRetryAt 记录一次临时失败后的重试时间（复用 lease_until 作为退避门控）。
func SetExecutionRetryAt(ctx context.Context, db *gorm.DB, id uint, retryAt time.Time, attemptCount int) error {
	return db.WithContext(ctx).
		Model(&types.AutomationExecution{}).
		Where("id = ? AND status = ? AND deleted_at IS NULL", id, string(types.AutomationExecutionQueued)).
		Updates(map[string]interface{}{
			"lease_until":   retryAt,
			"attempt_count": attemptCount,
			"updated_at":    time.Now(),
		}).Error
}

// ListDispatchedExpiredExecutions 返回已投递（dispatched_at 非空）但超过 not_after、
// 仍未收到 run.started（仍 queued）的执行记录，供 Server 过期扫描标记失败。
func ListDispatchedExpiredExecutions(ctx context.Context, db *gorm.DB, now time.Time, limit int) ([]*types.AutomationExecution, error) {
	var entities []*types.AutomationExecution
	err := db.WithContext(ctx).
		Where("status = ? AND dispatched_at IS NOT NULL AND not_after IS NOT NULL AND not_after <= ? AND deleted_at IS NULL",
			string(types.AutomationExecutionQueued), now).
		Order("created_at ASC").
		Limit(limit).
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// MarkExecutionDispatchExpired 将已投递但未开始的过期执行标记为失败。
func MarkExecutionDispatchExpired(ctx context.Context, db *gorm.DB, id uint) error {
	now := time.Now().UTC()
	return db.WithContext(ctx).
		Model(&types.AutomationExecution{}).
		Where("id = ? AND status = ? AND deleted_at IS NULL", id, string(types.AutomationExecutionQueued)).
		Updates(map[string]interface{}{
			"status":      string(types.AutomationExecutionFailed),
			"finished_at": now,
			"error_code":  "automation_dispatch_expired",
			"error_msg":   "已投递但未收到开始事件，命令已过期",
			"updated_at":  now,
		}).Error
}

// GetLatestExecutionByAutomation 返回某自动化最近一条执行记录（用于列表聚合最近状态）。
func GetLatestExecutionByAutomation(ctx context.Context, db *gorm.DB, automationID uint) (*types.AutomationExecution, error) {
	var entity types.AutomationExecution
	err := db.WithContext(ctx).
		Where("automation_id = ? AND deleted_at IS NULL", automationID).
		Order("created_at DESC").
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ListRunningTimeoutExecutions 返回 running 状态、但超过各自 not_after（命令截止时间）仍未到终态
// 的执行记录，供 Dispatcher 兜底标记失败，释放该自动化的 active 占位。
// 仅用于"僵尸 running"兜底，避免后续周期被无限 skipped。not_after 即触发时 +30min 宽限，
// 是每执行独立的截止点，不会误杀仍在正常运行的 AI 任务。
func ListRunningTimeoutExecutions(ctx context.Context, db *gorm.DB, now time.Time, limit int) ([]*types.AutomationExecution, error) {
	var entities []*types.AutomationExecution
	err := db.WithContext(ctx).
		Where("status = ? AND not_after IS NOT NULL AND not_after <= ? AND deleted_at IS NULL",
			string(types.AutomationExecutionRunning), now).
		Order("started_at ASC").
		Limit(limit).
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// MarkExecutionRunTimeout 将超时的 running 执行标记为失败，并清空/释放占位。
func MarkExecutionRunTimeout(ctx context.Context, db *gorm.DB, id uint) error {
	now := time.Now().UTC()
	return db.WithContext(ctx).
		Model(&types.AutomationExecution{}).
		Where("id = ? AND status = ? AND deleted_at IS NULL", id, string(types.AutomationExecutionRunning)).
		Updates(map[string]interface{}{
			"status":      string(types.AutomationExecutionFailed),
			"finished_at": now,
			"error_code":  "automation_run_timeout",
			"error_msg":   "执行运行超时，已兜底终止",
			"updated_at":  now,
		}).Error
}
