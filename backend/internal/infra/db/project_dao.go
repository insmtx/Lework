package db

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// CreateProject 创建项目
func CreateProject(ctx context.Context, db *gorm.DB, project *types.Project) error {
	return db.WithContext(ctx).Create(project).Error
}

// GetProjectByPublicID 根据组织ID和PublicID获取项目
func GetProjectByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string) (*types.Project, error) {
	var entity types.Project
	err := db.WithContext(ctx).Where("org_id = ? AND public_id = ?", orgID, publicID).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// UpdateProject 更新项目
func UpdateProject(ctx context.Context, db *gorm.DB, project *types.Project) error {
	return db.WithContext(ctx).Save(project).Error
}

// DeleteProject 删除项目（软删除）
func DeleteProject(ctx context.Context, db *gorm.DB, id uint) error {
	return db.WithContext(ctx).Delete(&types.Project{}, id).Error
}

// ListProjects 分页查询项目列表
func ListProjects(ctx context.Context, db *gorm.DB, orgID uint, keyword, status *string, offset, limit int) ([]*types.Project, int64, error) {
	var entities []*types.Project
	var total int64

	query := db.WithContext(ctx).Model(&types.Project{}).Where("org_id = ?", orgID)

	if keyword != nil && *keyword != "" {
		query = query.Where("name LIKE ? OR description LIKE ?",
			"%"+*keyword+"%", "%"+*keyword+"%")
	}
	if status != nil && *status != "" {
		query = query.Where("status = ?", *status)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&entities).Error
	if err != nil {
		return nil, 0, err
	}

	return entities, total, nil
}
