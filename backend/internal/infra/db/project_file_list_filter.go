package db

import (
	"context"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// ProjectFileListFilter scopes project file list queries.
type ProjectFileListFilter struct {
	ResourceType string
	TaskID       uint
	FileExt      string
}

// ValidProjectFileExtFilter reports whether file_ext is a supported filter key.
func ValidProjectFileExtFilter(fileExt string) bool {
	switch strings.TrimSpace(fileExt) {
	case "pdf", "docx", "xlsx", "pptx", "md", "image", "video", "text":
		return true
	default:
		return false
	}
}

// ListProjectFilesFiltered returns project files for one filter scope.
func ListProjectFilesFiltered(
	ctx context.Context,
	db *gorm.DB,
	orgID uint,
	projectID uint,
	filter ProjectFileListFilter,
) ([]types.ProjectFile, error) {
	fileExt := strings.TrimSpace(filter.FileExt)

	files, err := listLatestProjectFileRecords(ctx, db, orgID, projectID, filter.TaskID, filter.ResourceType)
	if err != nil {
		return nil, err
	}
	if fileExt != "" {
		files = filterProjectFilesByExtGroup(files, fileExt)
	}
	return files, nil
}

func listLatestProjectFileRecords(
	ctx context.Context,
	db *gorm.DB,
	orgID uint,
	projectID uint,
	taskID uint,
	resourceType string,
) ([]types.ProjectFile, error) {
	var files []types.ProjectFile
	base := db.WithContext(ctx).Model(&types.ProjectFile{}).
		Where("org_id = ? AND project_id = ?", orgID, projectID)
	if taskID != 0 {
		base = base.Where("task_id = ?", taskID)
	}
	if resourceType != "" {
		base = base.Where("resource_type = ?", resourceType)
	} else {
		base = base.Where("resource_type != ?", types.ProjectFileResourceTypePlan)
	}

	latest := base.Session(&gorm.Session{}).
		Select("initial_file_public_id, MAX(version_no) AS version_no").
		Group("initial_file_public_id")
	table := types.TableNameProjectFile
	query := base.
		Joins(
			"JOIN (?) AS latest ON latest.initial_file_public_id = "+table+".initial_file_public_id AND latest.version_no = "+table+".version_no",
			latest,
		).
		Order(table + ".created_at DESC, " + table + ".id DESC")
	if err := query.Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func filterProjectFilesByExtGroup(files []types.ProjectFile, fileExt string) []types.ProjectFile {
	filtered := make([]types.ProjectFile, 0, len(files))
	for _, file := range files {
		if matchesProjectFileExtGroup(file.RelativePath, fileExt) {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

func matchesProjectFileExtGroup(relativePath, fileExt string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(relativePath)))
	switch strings.TrimSpace(fileExt) {
	case "pdf":
		return ext == ".pdf"
	case "docx":
		return ext == ".docx" || ext == ".doc"
	case "xlsx":
		return ext == ".xlsx" || ext == ".xls" || ext == ".csv"
	case "pptx":
		return ext == ".pptx" || ext == ".ppt"
	case "md":
		return ext == ".md" || ext == ".markdown"
	case "image":
		return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" ||
			ext == ".bmp" || ext == ".webp" || ext == ".svg"
	case "video":
		return ext == ".mp4" || ext == ".mov" || ext == ".avi"
	case "text":
		return ext == ".txt" || ext == ".json" || ext == ".yaml" || ext == ".yml" ||
			ext == ".log" || ext == ".html" || ext == ".htm"
	default:
		return true
	}
}
