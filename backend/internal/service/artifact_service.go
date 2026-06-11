package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/types"
	"gorm.io/gorm"
)

type artifactService struct {
	db *gorm.DB
}

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
	task, err := infradb.GetTaskByPublicID(ctx, s.db, caller.OrgID, taskPublicID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, errors.New("task not found")
	}
	artifacts, err := infradb.ListTaskArtifacts(ctx, s.db, caller.OrgID, task.ID)
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
	artifact, err := infradb.GetArtifactByPublicID(ctx, s.db, caller.OrgID, artifactPublicID)
	if err != nil {
		return nil, err
	}
	if artifact == nil {
		return nil, errors.New("artifact not found")
	}

	reader, _, err := filestore.OpenFileByPublicID(ctx, s.db, artifact.OrgID, artifact.StorageKey)
	if err != nil {
		return nil, err
	}
	return &contract.ArtifactDownload{
		FileName: artifactDownloadName(artifact),
		MimeType: artifact.MimeType,
		Size:     artifact.FileSize,
		Reader:   reader,
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

var _ contract.ArtifactService = (*artifactService)(nil)
