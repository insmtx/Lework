package service

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/internal/infra/db"
	"github.com/insmtx/Lework/backend/types"
)

func projectFileResourceRef(pf *types.ProjectFile) (types.ResourceRef, error) {
	if pf == nil {
		return types.ResourceRef{}, fmt.Errorf("project file not found")
	}
	switch pf.ResourceType {
	case types.ProjectFileResourceTypeArtifact:
		return types.ResourceRef{Type: types.ResourceTypeArtifact, BizID: pf.ID}, nil
	case types.ProjectFileResourceTypeUserUpload, types.ProjectFileResourceTypePlan:
		return types.ResourceRef{Type: types.ResourceTypeFile, BizID: pf.ID}, nil
	default:
		return types.ResourceRef{}, fmt.Errorf("unknown project file resource type: %s", pf.ResourceType)
	}
}

func projectFileViewAction(pf *types.ProjectFile) types.Action {
	if pf != nil && pf.ResourceType == types.ProjectFileResourceTypeArtifact {
		return ActionArtifactView
	}
	return ActionFileView
}

func projectFileDownloadAction(pf *types.ProjectFile) types.Action {
	if pf != nil && pf.ResourceType == types.ProjectFileResourceTypeArtifact {
		return ActionArtifactDownload
	}
	return ActionFileDownload
}

func isProjectFileActionCompatible(pf *types.ProjectFile, action types.Action) bool {
	if pf.ResourceType == types.ProjectFileResourceTypeArtifact {
		return action == ActionArtifactView || action == ActionArtifactDownload
	}
	return action == ActionFileView || action == ActionFileDownload
}

// syncTaskResource 在任务创建后同步写入 lework_resource，挂到父项目资源树下。
func syncTaskResource(ctx context.Context, tx *gorm.DB, orgID, projectID, taskID, ownerUin uint) error {
	projectResource, err := db.GetResourceByBizID(ctx, tx, orgID, types.ResourceTypeProject, projectID)
	if err != nil {
		return fmt.Errorf("get project resource: %w", err)
	}
	if projectResource == nil {
		return fmt.Errorf("project resource not found for project %d", projectID)
	}

	parentID := projectResource.ID
	path := types.ResourcePathIDs{projectResource.ID}
	return db.CreateResource(ctx, tx, &types.Resource{
		OrgID:                 orgID,
		Uin:                   ownerUin,
		Type:                  types.ResourceTypeTask,
		BizID:                 taskID,
		ParentResourceID:      &parentID,
		ParentResourcePathIDs: path,
	})
}
