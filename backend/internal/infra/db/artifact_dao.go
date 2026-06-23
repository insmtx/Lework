package db

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// CreateArtifacts persists artifact records in order.
func CreateArtifacts(ctx context.Context, db *gorm.DB, artifacts []*types.Artifact) error {
	if len(artifacts) == 0 {
		return nil
	}
	return db.WithContext(ctx).Create(&artifacts).Error
}

// CreateArtifact persists one artifact record.
func CreateArtifact(ctx context.Context, db *gorm.DB, artifact *types.Artifact) error {
	if artifact == nil {
		return nil
	}
	return db.WithContext(ctx).Create(artifact).Error
}

// GetArtifactByPublicID returns one artifact in an organization.
func GetArtifactByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string) (*types.Artifact, error) {
	var entity types.Artifact
	err := db.WithContext(ctx).
		Where("org_id = ? AND public_id = ?", orgID, publicID).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ListTaskArtifacts returns completed artifacts for a task.
func ListTaskArtifacts(ctx context.Context, db *gorm.DB, orgID uint, taskID uint) ([]*types.Artifact, error) {
	var entities []*types.Artifact
	err := db.WithContext(ctx).
		Where("org_id = ? AND task_id = ? AND status = ?", orgID, taskID, string(types.ArtifactStatusCompleted)).
		// 中文注释：任务产物列表按最新生成优先返回，方便前端统一展示“新产物在最上面”。
		Order("created_at DESC").
		Find(&entities).Error
	return entities, err
}

// ListArtifactsByProjectID returns completed artifacts for a project.
func ListArtifactsByProjectID(ctx context.Context, db *gorm.DB, orgID, projectID uint) ([]*types.Artifact, error) {
	var entities []*types.Artifact
	err := db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND status = ?", orgID, projectID, string(types.ArtifactStatusCompleted)).
		Order("created_at DESC").
		Find(&entities).Error
	return entities, err
}
