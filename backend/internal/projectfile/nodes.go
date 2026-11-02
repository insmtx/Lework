package projectfile

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/internal/consts"
	infradb "github.com/insmtx/Lework/backend/internal/infra/db"
	"github.com/insmtx/Lework/backend/internal/workspace"
	"github.com/insmtx/Lework/backend/types"
)

// NormalizeFolderRelativePath canonicalizes one folder path with a trailing slash.
func NormalizeFolderRelativePath(relativePath string) (string, error) {
	normalized, err := workspace.NormalizeRelativePath(relativePath)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(normalized, "/") {
		normalized += "/"
	}
	return normalized, nil
}

// BindUserUploadParams links one uploaded file into a project.
type BindUserUploadParams struct {
	OrgID        uint
	ProjectID    uint
	TaskID       *uint
	TaskPublicID string
	Uin          uint
	FileUpload   *types.FileUpload
	DisplayName  string
	RelativePath string
}

// BindUserUploadsToProject creates ProjectFile records for multiple uploads in a batch.
// The caller is responsible for wrapping the call in a transaction if atomicity is required.
func BindUserUploadsToProject(ctx context.Context, db *gorm.DB, params []BindUserUploadParams) ([]*types.ProjectFile, error) {
	if len(params) == 0 {
		return nil, nil
	}
	pfs := make([]*types.ProjectFile, 0, len(params))
	for _, p := range params {
		if p.FileUpload == nil {
			continue
		}
		relativePath := strings.TrimSpace(p.RelativePath)
		if relativePath == "" {
			relativePath = strings.TrimSpace(p.DisplayName)
		}
		if relativePath == "" {
			relativePath = strings.TrimSpace(p.FileUpload.OriginalName)
		}
		if relativePath == "" {
			continue
		}
		relativePath, err := workspace.NormalizeRelativePath(consts.RepoDirUploads + "/" + relativePath)
		if err != nil {
			return nil, fmt.Errorf("normalize relative path %q: %w", relativePath, err)
		}
		pf := &types.ProjectFile{
			FilePublicID:        p.FileUpload.PublicID,
			InitialFilePublicID: p.FileUpload.PublicID,
			VersionNo:           1,
			OrgID:               p.OrgID,
			ProjectID:           p.ProjectID,
			ResourceID:          p.FileUpload.ID,
			ResourceType:        types.ProjectFileResourceTypeUserUpload,
			Uin:                 p.Uin,
			RelativePath:        relativePath,
		}
		if p.TaskID != nil {
			pf.TaskID = *p.TaskID
		}
		pfs = append(pfs, pf)
	}
	if len(pfs) == 0 {
		return nil, fmt.Errorf("no valid project files to create")
	}
	if err := db.WithContext(ctx).Create(pfs).Error; err != nil {
		return nil, err
	}
	return pfs, nil
}

// BindUserUploadToProject creates a ProjectFile record for one upload.
func BindUserUploadToProject(ctx context.Context, db *gorm.DB, params BindUserUploadParams) (*types.ProjectFile, error) {
	if params.FileUpload == nil {
		return nil, fmt.Errorf("file upload is required")
	}

	relativePath := strings.TrimSpace(params.RelativePath)
	if relativePath == "" {
		relativePath = strings.TrimSpace(params.DisplayName)
	}
	if relativePath == "" {
		relativePath = strings.TrimSpace(params.FileUpload.OriginalName)
	}
	if relativePath == "" {
		return nil, fmt.Errorf("relative path is required")
	}
	relativePath, err := workspace.NormalizeRelativePath(consts.RepoDirUploads + "/" + relativePath)
	if err != nil {
		return nil, err
	}

	pf := &types.ProjectFile{
		FilePublicID:        params.FileUpload.PublicID,
		InitialFilePublicID: params.FileUpload.PublicID,
		VersionNo:           1,
		OrgID:               params.OrgID,
		ProjectID:           params.ProjectID,
		ResourceID:          params.FileUpload.ID,
		ResourceType:        types.ProjectFileResourceTypeUserUpload,
		Uin:                 params.Uin,
		RelativePath:        relativePath,
	}
	if params.TaskID != nil {
		pf.TaskID = *params.TaskID
	}
	if err := infradb.CreateProjectFile(ctx, db, pf); err != nil {
		return nil, err
	}
	return pf, nil
}
