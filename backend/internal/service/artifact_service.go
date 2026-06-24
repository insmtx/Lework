package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"code.gitea.io/sdk/gitea"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
	"gorm.io/gorm"
)

type artifactService struct {
	db          *gorm.DB
	giteaClient *gitea.Client
}

// NewArtifactService creates a service for generated artifacts.
func NewArtifactService(db *gorm.DB, giteaClient *gitea.Client) contract.ArtifactService {
	return &artifactService{db: db, giteaClient: giteaClient}
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
	if err := verifyUserPermission(task.OwnerID, caller.Uin); err != nil {
		return nil, err
	}
	artifacts, err := infradb.ListTaskArtifacts(ctx, s.db, caller.OrgID, task.ID)
	if err != nil {
		return nil, err
	}
	result := make([]contract.Artifact, 0, len(artifacts))
	for _, a := range artifacts {
		if converted := convertToContractArtifact(a); converted != nil {
			result = append(result, *converted)
		}
	}
	return result, nil
}

func (s *artifactService) GetArtifact(ctx context.Context, artifactPublicID string) (*contract.ArtifactDetail, error) {
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
	if err := verifyUserPermission(artifact.OwnerID, caller.Uin); err != nil {
		return nil, err
	}
	return convertToArtifactDetail(artifact), nil
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
	if err := verifyUserPermission(artifact.OwnerID, caller.Uin); err != nil {
		return nil, err
	}

	if strings.TrimSpace(artifact.RelativePath) == "" {
		return nil, errors.New("artifact has no relative path")
	}

	project, err := infradb.GetProjectByID(ctx, s.db, artifact.ProjectID)
	if err != nil {
		return nil, err
	}
	if project == nil || strings.TrimSpace(project.GiteaRepoFullName) == "" {
		return nil, errors.New("project not linked to gitea repo")
	}

	parts := strings.SplitN(project.GiteaRepoFullName, "/", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid gitea repo full name")
	}

	data, _, err := s.giteaClient.GetFile(parts[0], parts[1],
		project.GiteaDefaultBranch, artifact.RelativePath)
	if err != nil {
		return nil, fmt.Errorf("get gitea file: %w", err)
	}
	reader := io.NopCloser(bytes.NewReader(data))

	return &contract.ArtifactDownload{
		FileName: artifactDownloadName(artifact),
		MimeType: artifact.MimeType,
		Size:     artifact.FileSize,
		Reader:   reader,
	}, nil
}

func convertToContractArtifact(artifact *types.Artifact) *contract.Artifact {
	if artifact == nil {
		return nil
	}
	return &contract.Artifact{
		ArtifactID:   artifact.PublicID,
		Title:        artifact.Title,
		Filename:     artifact.Filename,
		Description:  artifact.Description,
		ArtifactType: artifact.ArtifactType,
		MimeType:     artifact.MimeType,
		FileSize:     artifact.FileSize,
		Sha256:       artifact.Sha256,
		CreatedAt:    artifact.CreatedAt,
	}
}

func convertToArtifactDetail(artifact *types.Artifact) *contract.ArtifactDetail {
	if artifact == nil {
		return nil
	}
	return &contract.ArtifactDetail{
		Artifact:     *convertToContractArtifact(artifact),
		RelativePath: artifact.RelativePath,
		FilePublicID: artifact.FilePublicID,
		Source:       artifact.Source,
		ExportFormat: artifact.ExportFormat,
		Version:      artifact.Version,
		Status:       artifact.Status,
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
