package db

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/types"
)

// GetWorkbenchRecentContext 获取用户最近明确使用的首页项目/任务上下文。
func GetWorkbenchRecentContext(ctx context.Context, db *gorm.DB, orgID, uin uint) (*types.WorkbenchRecentContext, error) {
	var entity types.WorkbenchRecentContext
	err := db.WithContext(ctx).
		Where("org_id = ? AND uin = ?", orgID, uin).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// UpsertWorkbenchRecentContext 保存用户最近明确使用的首页项目/任务上下文。
func UpsertWorkbenchRecentContext(ctx context.Context, db *gorm.DB, entity *types.WorkbenchRecentContext) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "org_id"}, {Name: "uin"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"project_id",
			"task_id",
			"used_at",
			"updated_at",
		}),
	}).Create(entity).Error
}
