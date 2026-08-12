// 定义 Automation 数据访问层
package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// ListAutomationOptions 自动化列表查询参数
type ListAutomationOptions struct {
	// Keyword 名称/指令模糊搜索
	Keyword string
	// Enabled 启用状态筛选（nil 表示不过滤）
	Enabled *bool
	// ScheduleMode 调度模式筛选（calendar/interval）
	ScheduleMode string
	// Offset 分页偏移
	Offset int
	// Limit 每页数量
	Limit int
}

// CreateAutomation 创建自动化
func CreateAutomation(ctx context.Context, db *gorm.DB, automation *types.Automation) error {
	return db.WithContext(ctx).Create(automation).Error
}

// GetAutomationByPublicID 根据组织ID和PublicID获取自动化
func GetAutomationByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string) (*types.Automation, error) {
	var entity types.Automation
	err := db.WithContext(ctx).Where("org_id = ? AND public_id = ?", orgID, publicID).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// UpdateAutomation 更新自动化
func UpdateAutomation(ctx context.Context, db *gorm.DB, automation *types.Automation) error {
	return db.WithContext(ctx).Save(automation).Error
}

// UpdateAutomationProjectLink 仅更新自动化的关联项目（project_id 与 project_generation）。
//
// 用 map 做部分更新以支持把 project_id 置 NULL；generation 由调用方提供，避免清空关联时
// 丢失已创建专属项目的最大代数。
func UpdateAutomationProjectLink(ctx context.Context, db *gorm.DB, id uint, projectID *uint, generation int) error {
	return db.WithContext(ctx).Model(&types.Automation{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"project_id":         projectID,
			"project_generation": generation,
			"updated_at":         time.Now(),
		}).Error
}

// DeleteAutomationByPublicID 软删除自动化
func DeleteAutomationByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string) error {
	return db.WithContext(ctx).
		Where("org_id = ? AND public_id = ?", orgID, publicID).
		Delete(&types.Automation{}).Error
}

// ListAutomations 查询自动化列表
func ListAutomations(ctx context.Context, db *gorm.DB, orgID uint, ownerID uint, opts ListAutomationOptions) ([]*types.Automation, int64, error) {
	var entities []*types.Automation
	var total int64

	query := db.WithContext(ctx).Table(types.TableNameAutomation).
		Where("org_id = ? AND deleted_at IS NULL", orgID).
		Where("owner_id = ?", ownerID)

	if opts.Enabled != nil {
		query = query.Where("enabled = ?", *opts.Enabled)
	}
	if opts.ScheduleMode != "" {
		query = query.Where("schedule_mode = ?", opts.ScheduleMode)
	}
	if opts.Keyword != "" {
		keyword := "%" + strings.TrimSpace(opts.Keyword) + "%"
		query = query.Where("name LIKE ? OR instruction LIKE ?", keyword, keyword)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	query = query.Order("created_at DESC").
		Offset(opts.Offset)
	if opts.Limit > 0 {
		query = query.Limit(opts.Limit)
	}

	if err := query.Find(&entities).Error; err != nil {
		return nil, 0, err
	}
	return entities, total, nil
}

// GetAutomationProjectByGeneration 按 (automation_id, generation) 查询自动化的某代项目（优先恢复）。
func GetAutomationProjectByGeneration(ctx context.Context, db *gorm.DB, orgID, automationID uint, generation int) (*types.Project, error) {
	var entity types.Project
	err := db.WithContext(ctx).
		Where("org_id = ? AND automation_id = ? AND automation_generation = ? AND deleted_at IS NULL",
			orgID, automationID, generation).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// MaxAutomationProjectGeneration 返回自动化历史专属项目的最大代数。
// 使用 Unscoped 包含软删除项目：其 public_id 仍受唯一约束，下一代不能复用被删除项目的代数。
func MaxAutomationProjectGeneration(ctx context.Context, db *gorm.DB, orgID, automationID uint) (int, error) {
	var generation int
	err := db.WithContext(ctx).Unscoped().Model(&types.Project{}).
		Where("org_id = ? AND automation_id = ?", orgID, automationID).
		Select("COALESCE(MAX(automation_generation), 0)").Scan(&generation).Error
	return generation, err
}

// ListAutomationProjects 列出某自动化全部非删除项目（按代数升序）。
func ListAutomationProjects(ctx context.Context, db *gorm.DB, orgID, automationID uint) ([]*types.Project, error) {
	var entities []*types.Project
	err := db.WithContext(ctx).
		Where("org_id = ? AND automation_id = ? AND deleted_at IS NULL", orgID, automationID).
		Order("automation_generation ASC").
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// GetTaskByAutomationExecutionID 幂等：按执行记录主键查 cron 任务。
func GetTaskByAutomationExecutionID(ctx context.Context, db *gorm.DB, orgID, executionID uint) (*types.Task, error) {
	var entity types.Task
	err := db.WithContext(ctx).
		Where("org_id = ? AND automation_execution_id = ? AND deleted_at IS NULL", orgID, executionID).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetMessageByAutomationExecutionID 幂等：按执行记录主键查首条自动化指令消息。
func GetMessageByAutomationExecutionID(ctx context.Context, db *gorm.DB, orgID, executionID uint) (*types.SessionMessage, error) {
	var entity types.SessionMessage
	err := db.WithContext(ctx).
		Where("automation_execution_id = ? AND deleted_at IS NULL", executionID).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ListActiveAutomationExecutionsByAutomation 列出某自动化所有 queued/running 执行记录。
func ListActiveAutomationExecutionsByAutomation(ctx context.Context, db *gorm.DB, automationID uint) ([]*types.AutomationExecution, error) {
	var entities []*types.AutomationExecution
	err := db.WithContext(ctx).
		Where("automation_id = ? AND status IN ? AND deleted_at IS NULL",
			automationID, []string{string(types.AutomationExecutionQueued), string(types.AutomationExecutionRunning)}).
		Order("created_at ASC").
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}
