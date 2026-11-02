package db

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Lework/backend/types"
)

func CreateProjectFile(ctx context.Context, db *gorm.DB, file *types.ProjectFile) error {
	return db.WithContext(ctx).Create(file).Error
}

// CreateProjectFileVersion allocates version metadata from the latest record at the same project path.
// The caller must execute this function inside the transaction that creates the associated FileUpload.
func CreateProjectFileVersion(ctx context.Context, db *gorm.DB, file *types.ProjectFile) error {
	return CreateProjectFileVersionFromPreviousPath(ctx, db, file, "")
}

// CreateProjectFileVersionFromPreviousPath keeps a renamed file in its existing version chain.
// previousRelativePath is transient rename metadata and is not persisted.
func CreateProjectFileVersionFromPreviousPath(
	ctx context.Context,
	db *gorm.DB,
	file *types.ProjectFile,
	previousRelativePath string,
) error {
	if file == nil {
		return fmt.Errorf("project file is required")
	}
	file.RelativePath = strings.TrimSpace(file.RelativePath)
	if file.RelativePath == "" {
		return fmt.Errorf("project file relative_path is required")
	}

	latest, err := GetLatestProjectFileByRelativePathForUpdate(
		ctx,
		db,
		file.OrgID,
		file.ProjectID,
		file.ResourceType,
		file.RelativePath,
	)
	if err != nil {
		return err
	}
	previousRelativePath = strings.TrimSpace(previousRelativePath)
	if latest == nil && previousRelativePath != "" && previousRelativePath != file.RelativePath {
		latest, err = GetLatestProjectFileByRelativePathForUpdate(
			ctx,
			db,
			file.OrgID,
			file.ProjectID,
			file.ResourceType,
			previousRelativePath,
		)
		if err != nil {
			return err
		}
	}
	file.InitialFilePublicID = file.FilePublicID
	file.VersionNo = 1
	if latest != nil {
		file.InitialFilePublicID = latest.InitialFilePublicID
		if file.InitialFilePublicID == "" {
			file.InitialFilePublicID = latest.FilePublicID
		}
		file.VersionNo = latest.VersionNo + 1
	}
	return CreateProjectFile(ctx, db, file)
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

// GetProjectFileByProjectAndFilePublicID returns one concrete project file version.
func GetProjectFileByProjectAndFilePublicID(
	ctx context.Context,
	db *gorm.DB,
	orgID uint,
	projectID uint,
	filePublicID string,
) (*types.ProjectFile, error) {
	var file types.ProjectFile
	err := db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND file_public_id = ?", orgID, projectID, filePublicID).
		First(&file).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &file, nil
}

// GetLatestProjectFileByRelativePath returns the greatest version for one project path.
func GetLatestProjectFileByRelativePath(
	ctx context.Context,
	db *gorm.DB,
	orgID uint,
	projectID uint,
	resourceType types.ProjectFileResourceType,
	relativePath string,
) (*types.ProjectFile, error) {
	return getLatestProjectFileByRelativePath(ctx, db, orgID, projectID, resourceType, relativePath, false)
}

// GetLatestProjectFileByRelativePathForUpdate locks the greatest path version for version allocation.
func GetLatestProjectFileByRelativePathForUpdate(
	ctx context.Context,
	db *gorm.DB,
	orgID uint,
	projectID uint,
	resourceType types.ProjectFileResourceType,
	relativePath string,
) (*types.ProjectFile, error) {
	return getLatestProjectFileByRelativePath(ctx, db, orgID, projectID, resourceType, relativePath, true)
}

func getLatestProjectFileByRelativePath(
	ctx context.Context,
	db *gorm.DB,
	orgID uint,
	projectID uint,
	resourceType types.ProjectFileResourceType,
	relativePath string,
	forUpdate bool,
) (*types.ProjectFile, error) {
	var file types.ProjectFile
	query := db.WithContext(ctx).
		Where(
			"org_id = ? AND project_id = ? AND resource_type = ? AND relative_path = ?",
			orgID,
			projectID,
			resourceType,
			relativePath,
		).
		Order("version_no DESC, created_at DESC, id DESC")
	if forUpdate {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&file).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &file, nil
}

func ListProjectFiles(ctx context.Context, db *gorm.DB, orgID uint, projectID uint, resourceType string) ([]types.ProjectFile, error) {
	return ListProjectFilesFiltered(ctx, db, orgID, projectID, ProjectFileListFilter{
		ResourceType: resourceType,
	})
}

func DeleteProjectFile(ctx context.Context, db *gorm.DB, filePublicID string) error {
	return db.WithContext(ctx).Where("file_public_id = ?", filePublicID).Delete(&types.ProjectFile{}).Error
}

func DeleteProjectFilesByResourceID(ctx context.Context, db *gorm.DB, resourceID uint, resourceType string) error {
	return db.WithContext(ctx).Where("resource_id = ? AND resource_type = ?", resourceID, resourceType).Delete(&types.ProjectFile{}).Error
}

// ListProjectFilesByTask returns ProjectFile records filtered by task.
func ListProjectFilesByTask(ctx context.Context, db *gorm.DB, orgID uint, projectID uint, taskID uint, resourceType string) ([]types.ProjectFile, error) {
	return ListProjectFilesFiltered(ctx, db, orgID, projectID, ProjectFileListFilter{
		ResourceType: resourceType,
		TaskID:       taskID,
	})
}

// ListProjectFileVersions returns all versions in one initial-file chain.
func ListProjectFileVersions(
	ctx context.Context,
	db *gorm.DB,
	orgID uint,
	projectID uint,
	initialFilePublicID string,
) ([]types.ProjectFile, error) {
	var files []types.ProjectFile
	err := db.WithContext(ctx).
		Where(
			"org_id = ? AND project_id = ? AND initial_file_public_id = ?",
			orgID,
			projectID,
			initialFilePublicID,
		).
		Order("version_no DESC, created_at DESC, id DESC").
		Find(&files).Error
	if err != nil {
		return nil, err
	}
	return files, nil
}
