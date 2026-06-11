package db

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/types"
)

// CreateTask 创建任务
func CreateTask(ctx context.Context, db *gorm.DB, task *types.Task) error {
	return db.WithContext(ctx).Create(task).Error
}

// GetTaskByPublicID 根据组织ID和PublicID获取任务
func GetTaskByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string) (*types.Task, error) {
	var entity types.Task
	err := db.WithContext(ctx).Where("org_id = ? AND public_id = ?", orgID, publicID).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// UpdateTask 更新任务
func UpdateTask(ctx context.Context, db *gorm.DB, task *types.Task) error {
	return db.WithContext(ctx).Save(task).Error
}

// DeleteTask 删除任务（软删除）
func DeleteTask(ctx context.Context, db *gorm.DB, id uint) error {
	return db.WithContext(ctx).Delete(&types.Task{}, id).Error
}

// ListTasks 查询任务列表，使用 PageQuery 作为查询参数
func ListTasks(ctx context.Context, d *gorm.DB, opt *types.PageQuery) ([]*types.Task, int64, error) {
	var entities []*types.Task
	var total int64

	query := d.WithContext(ctx).Table(types.TableNameTask).
		Where("org_id = ? AND deleted_at IS NULL", opt.OrgID)
	if opt.Uin > 0 {
		query = query.Where("owner_id = ?", opt.Uin)
	}

	for _, filter := range opt.Filters {
		switch filter.Field {
		case "keyword":
			keyword := "%" + filter.Value[0] + "%"
			query = query.Where("title LIKE ? OR description LIKE ?", keyword, keyword)
		case "status":
			query = query.Where("status IN (?)", filter.Value)
		case "project_id":
			if filter.ExactMatch {
				query = query.Where("project_id IN (?)", filter.Value)
			} else {
				query = query.Where("project_id IN (?)", filter.Value)
			}
		case "task_type":
			query = query.Where("task_type IN (?)", filter.Value)
		case "assignee_id":
			query = query.Where("assignee_id IN (?)", filter.Value)
		case "title":
			if filter.ExactMatch {
				query = query.Where("title IN (?)", filter.Value)
			} else {
				query = query.Where("title LIKE ?", "%"+filter.Value[0]+"%")
			}
		default:
			logs.WarnContextf(ctx, "[task][ListTasks] invalid filter field: %s", filter.Field)
		}
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	if len(opt.OrderBy) > 0 {
		query = query.Order(strings.Join(opt.OrderBy, ","))
	} else {
		query = query.Order("created_at DESC")
	}

	query = query.Offset(opt.Offset)
	if !opt.ListAll && opt.Limit > 0 {
		query = query.Limit(opt.Limit)
	} else {
		query = query.Limit(150)
	}

	if err := query.Find(&entities).Error; err != nil {
		return nil, 0, err
	}
	return entities, total, nil
}

// ListTasksByProjectID 根据项目ID查询所有未删除的任务
func ListTasksByProjectID(ctx context.Context, db *gorm.DB, orgID, projectID uint) ([]*types.Task, error) {
	var entities []*types.Task
	err := db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND deleted_at IS NULL", orgID, projectID).
		Order("created_at DESC").
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}
