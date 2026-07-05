package db

import (
	"context"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func CreateProjectFile(ctx context.Context, db *gorm.DB, file *types.ProjectFile) error {
	return db.WithContext(ctx).Create(file).Error
}

func GetProjectFileByFilePublicID(ctx context.Context, db *gorm.DB, orgID uint, filePublicID string) (*types.ProjectFile, error) {
	var file types.ProjectFile
	err := db.WithContext(ctx).Where("file_public_id = ? AND org_id = ?", filePublicID, orgID).First(&file).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &file, nil
}

func ListProjectFiles(ctx context.Context, db *gorm.DB, orgID uint, projectID uint, resourceType string) ([]types.ProjectFile, error) {
	var files []types.ProjectFile
	query := db.WithContext(ctx).Model(&types.ProjectFile{}).
		Where("org_id = ? AND project_id = ?", orgID, projectID)
	if resourceType != "" {
		query = query.Where("resource_type = ?", resourceType)
	} else {
		query = query.Where("resource_type != ?", types.ProjectFileResourceTypePlan)
	}
	if err := query.Order("created_at DESC").Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func DeleteProjectFile(ctx context.Context, db *gorm.DB, filePublicID string) error {
	return db.WithContext(ctx).Where("file_public_id = ?", filePublicID).Delete(&types.ProjectFile{}).Error
}

func DeleteProjectFilesByResourceID(ctx context.Context, db *gorm.DB, resourceID uint, resourceType string) error {
	return db.WithContext(ctx).Where("resource_id = ? AND resource_type = ?", resourceID, resourceType).Delete(&types.ProjectFile{}).Error
}

// ListProjectFilesByTask returns ProjectFile records filtered by task.
func ListProjectFilesByTask(ctx context.Context, db *gorm.DB, orgID uint, projectID uint, taskID uint, resourceType string) ([]types.ProjectFile, error) {
	var files []types.ProjectFile
	query := db.WithContext(ctx).Model(&types.ProjectFile{}).
		Where("org_id = ? AND project_id = ? AND task_id = ?", orgID, projectID, taskID)
	if resourceType != "" {
		query = query.Where("resource_type = ?", resourceType)
	} else {
		query = query.Where("resource_type != ?", types.ProjectFileResourceTypePlan)
	}
	if err := query.Order("created_at DESC").Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}
