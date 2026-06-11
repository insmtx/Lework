package service

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/types"
)

type artifactService struct {
	db *gorm.DB
}

const defaultArtifactWorkerID uint = 1

// NewArtifactService creates a service for generated artifacts.
func NewArtifactService(db *gorm.DB) contract.ArtifactService {
	return &artifactService{db: db}
}

func (s *artifactService) ListTaskArtifacts(ctx context.Context, taskPublicID string) ([]contract.Artifact, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(taskPublicID) == "" {
		return nil, errors.New("task_id is required")
	}
	task, err := db.GetTaskByPublicID(ctx, s.db, caller.OrgID, taskPublicID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, errors.New("task not found")
	}
	if err := verifyUserPermission(task.OwnerID, caller.Uin); err != nil {
		return nil, err
	}
	artifacts, err := db.ListTaskArtifacts(ctx, s.db, caller.OrgID, task.ID)
	if err != nil {
		return nil, err
	}
	result := make([]contract.Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		result = append(result, convertToContractArtifact(artifact))
	}
	return result, nil
}

func (s *artifactService) GetArtifactDownload(ctx context.Context, artifactPublicID string) (*contract.ArtifactDownload, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(artifactPublicID) == "" {
		return nil, errors.New("artifact_id is required")
	}
	artifact, err := db.GetArtifactByPublicID(ctx, s.db, caller.OrgID, artifactPublicID)
	if err != nil {
		return nil, err
	}
	if artifact == nil {
		return nil, errors.New("artifact not found")
	}
	if err := verifyUserPermission(artifact.OwnerID, caller.Uin); err != nil {
		return nil, err
	}
	file, err := agentworkspace.OpenArtifactStorageFile(artifact.OrgID, artifactWorkerID(artifact), artifact.StorageKey)
	if err != nil {
		return nil, err
	}
	return &contract.ArtifactDownload{
		FileName: artifactDownloadName(artifact),
		MimeType: artifact.MimeType,
		Size:     artifact.FileSize,
		Reader:   file,
	}, nil
}

func convertToContractArtifact(artifact *types.Artifact) contract.Artifact {
	if artifact == nil {
		return contract.Artifact{}
	}
	return contract.Artifact{
		ArtifactID:   artifact.PublicID,
		Title:        artifact.Title,
		Filename:     artifact.Filename,
		Description:  artifact.Description,
		ArtifactType: artifact.ArtifactType,
		MimeType:     artifact.MimeType,
		FileSize:     artifact.FileSize,
		Sha256:       artifact.Sha256,
	}
}

func artifactDownloadName(artifact *types.Artifact) string {
	if artifact == nil {
		return ""
	}
	if strings.TrimSpace(artifact.Filename) != "" {
		return strings.TrimSpace(artifact.Filename)
	}
	if strings.TrimSpace(artifact.Title) != "" {
		return strings.TrimSpace(artifact.Title)
	}
	return filepath.Base(strings.TrimSpace(artifact.RelativePath))
}

func artifactWorkerID(artifact *types.Artifact) uint {
	if artifact == nil || artifact.Metadata.Extra == nil {
		return defaultArtifactWorkerID
	}
	value, ok := artifact.Metadata.Extra["worker_id"]
	if !ok {
		return defaultArtifactWorkerID
	}
	switch typed := value.(type) {
	case uint:
		if typed != 0 {
			return typed
		}
	case int:
		if typed > 0 {
			return uint(typed)
		}
	case int64:
		if typed > 0 {
			return uint(typed)
		}
	case float64:
		if typed > 0 {
			return uint(typed)
		}
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		if err == nil && parsed > 0 {
			return uint(parsed)
		}
	}
	return defaultArtifactWorkerID
}

var _ contract.ArtifactService = (*artifactService)(nil)
