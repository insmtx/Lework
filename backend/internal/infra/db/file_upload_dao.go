package db

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/types"
)

// ErrSystemArtifactAlreadyExists indicates that a content-addressed system artifact was reused.
var ErrSystemArtifactAlreadyExists = errors.New("system artifact already exists")

func CreateFileUpload(ctx context.Context, db *gorm.DB, file *types.FileUpload) error {
	file.OwnerScope = types.NormalizeOwnerScope(file.OwnerScope)
	if !types.ValidateOwnerScope(file.OwnerScope, file.OrgID) {
		return fmt.Errorf("invalid file upload owner scope %q for org_id %d", file.OwnerScope, file.OrgID)
	}
	if file.OwnerScope == types.OwnerScopeOrganization && file.OwnerID == 0 {
		return fmt.Errorf("organization file upload owner_id is required")
	}
	if file.OwnerScope == types.OwnerScopeSystem && file.OwnerID != 0 {
		return fmt.Errorf("system file upload owner_id must be zero")
	}
	if file.OwnerScope == types.OwnerScopeSystem && file.Purpose == "artifact" {
		result := db.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(file)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrSystemArtifactAlreadyExists
		}
		return nil
	}
	return db.WithContext(ctx).Create(file).Error
}

func GetFileUploadsByPublicIDs(ctx context.Context, db *gorm.DB, orgID uint, publicIDs []string) ([]types.FileUpload, error) {
	if len(publicIDs) == 0 {
		return nil, nil
	}
	var files []types.FileUpload
	query := db.WithContext(ctx).
		Where("public_id IN ? AND owner_scope = ?", publicIDs, types.OwnerScopeOrganization)
	if orgID != 0 {
		query = query.Where("org_id = ?", orgID)
	}
	err := query.Find(&files).Error
	if err != nil {
		return nil, err
	}
	return files, nil
}

func GetFileUploadByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string) (*types.FileUpload, error) {
	var file types.FileUpload
	query := db.WithContext(ctx).
		Where("public_id = ? AND owner_scope = ?", publicID, types.OwnerScopeOrganization)
	if orgID != 0 {
		query = query.Where("org_id = ?", orgID)
	}
	err := query.First(&file).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &file, nil
}

// GetFileUploadByStorageURI resolves an organization-owned upload by its
// canonical storage identity.
func GetFileUploadByStorageURI(
	ctx context.Context,
	db *gorm.DB,
	orgID uint,
	storageURI string,
) (*types.FileUpload, error) {
	var file types.FileUpload
	err := db.WithContext(ctx).
		Where(
			"storage_uri = ? AND owner_scope = ? AND org_id = ?",
			storageURI,
			types.OwnerScopeOrganization,
			orgID,
		).
		Order("id ASC").
		First(&file).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &file, nil
}

// GetSystemFileUploadByPublicID resolves one system-owned file identity.
// Callers must establish authorization through a system Plugin revision before using it.
func GetSystemFileUploadByPublicID(ctx context.Context, db *gorm.DB, publicID string) (*types.FileUpload, error) {
	var file types.FileUpload
	err := db.WithContext(ctx).
		Where("public_id = ? AND owner_scope = ? AND org_id = 0", publicID, types.OwnerScopeSystem).
		First(&file).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &file, nil
}

// GetSystemFileUploadBySHA256 resolves one content-addressed system artifact.
func GetSystemFileUploadBySHA256(ctx context.Context, db *gorm.DB, sha256 string) (*types.FileUpload, error) {
	var file types.FileUpload
	err := db.WithContext(ctx).
		Where(
			"owner_scope = ? AND org_id = 0 AND owner_id = 0 AND purpose = ? AND sha256 = ?",
			types.OwnerScopeSystem,
			"artifact",
			sha256,
		).
		First(&file).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &file, nil
}

func UpdateFileUpload(ctx context.Context, db *gorm.DB, file *types.FileUpload) error {
	return db.WithContext(ctx).Save(file).Error
}

func ListFileUploads(ctx context.Context, db *gorm.DB, orgID uint, purpose string, offset, limit int) ([]types.FileUpload, int64, error) {
	var files []types.FileUpload
	query := db.WithContext(ctx).Model(&types.FileUpload{}).
		Where("owner_scope = ? AND org_id = ?", types.OwnerScopeOrganization, orgID)
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
		Where(
			"owner_scope = ? AND org_id = ? AND metadata->'extra'->>'project_public_id' = ?",
			types.OwnerScopeOrganization,
			orgID,
			projectPublicID,
		).
		Order("created_at DESC").
		Find(&files).Error
	if err != nil {
		return nil, err
	}
	return files, nil
}
