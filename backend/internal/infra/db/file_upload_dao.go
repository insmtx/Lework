package db

import (
	"context"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func CreateFileUpload(ctx context.Context, db *gorm.DB, file *types.FileUpload) error {
	return db.WithContext(ctx).Create(file).Error
}

func GetFileUploadByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string) (*types.FileUpload, error) {
	var file types.FileUpload
	err := db.WithContext(ctx).Where("public_id = ? AND org_id = ?", publicID, orgID).First(&file).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &file, nil
}

func UpdateFileUpload(ctx context.Context, db *gorm.DB, file *types.FileUpload) error {
	return db.WithContext(ctx).Save(file).Error
}

func ListFileUploads(ctx context.Context, db *gorm.DB, orgID uint, purpose string, offset, limit int) ([]types.FileUpload, int64, error) {
	var files []types.FileUpload
	query := db.WithContext(ctx).Model(&types.FileUpload{}).Where("org_id = ?", orgID)
	if purpose != "" {
		query = query.Where("purpose = ?", purpose)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&files).Error; err != nil {
		return nil, 0, err
	}
	return files, total, nil
}

// ListProjectFileUploads 查询关联到指定项目的已上传文件列表。
// 文件关联通过 FileUpload.Metadata.Extra["project_id"] 标记。
func ListProjectFileUploads(ctx context.Context, db *gorm.DB, orgID uint, projectPublicID string) ([]types.FileUpload, error) {
	var files []types.FileUpload
	err := db.WithContext(ctx).Model(&types.FileUpload{}).
		Where("org_id = ? AND metadata->'extra'->>'project_public_id' = ?", orgID, projectPublicID).
		Order("created_at DESC").
		Find(&files).Error
	if err != nil {
		return nil, err
	}
	return files, nil
}
