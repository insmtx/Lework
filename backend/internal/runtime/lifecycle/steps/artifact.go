package steps

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
	"github.com/ygpkg/storage-go"

	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/internal/worker/client"
	"github.com/insmtx/Leros/backend/internal/worker/identity"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/types"
)

// ArtifactRecorder 记录已声明的产物并返回公开的事件负载。
type ArtifactRecorder interface {
	Record(ctx context.Context, req *agent.RequestContext) ([]events.ArtifactPayload, error)
}

// ArtifactStep 在终端运行事件发送前记录 manifest 中的产物。
type ArtifactStep struct {
	Recorder ArtifactRecorder
}

func (ArtifactStep) Name() string {
	return "artifact"
}

func (s ArtifactStep) Run(ctx context.Context, state *State) error {
	if state == nil || state.Err != nil || state.Journal == nil || s.Recorder == nil {
		return nil
	}
	artifacts, err := s.Recorder.Record(ctx, state.Request)
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.ArtifactID) == "" {
			return fmt.Errorf("artifact_id is required for artifact declaration")
		}
		if strings.TrimSpace(artifact.StorageKey) == "" {
			return fmt.Errorf("storage_key is required for artifact declaration")
		}
		if err := state.Journal.Append(ctx, events.NewArtifactDeclared(artifact)); err != nil {
			return err
		}
	}
	return nil
}

// WorkspaceArtifactRecorder 收集运行工作区 manifest 中声明的产物。
type WorkspaceArtifactRecorder struct{}

// NewWorkspaceArtifactRecorder 创建基于 manifest 的产物记录器。
func NewWorkspaceArtifactRecorder() *WorkspaceArtifactRecorder {
	return &WorkspaceArtifactRecorder{}
}

// Record 收集单次运行的最终 manifest 产物，并上传到 filestore。
func (r *WorkspaceArtifactRecorder) Record(ctx context.Context, req *agent.RequestContext) ([]events.ArtifactPayload, error) {
	if r == nil || req == nil {
		return nil, nil
	}
	plan, ok, err := agentworkspace.FromAgentRequest(req)
	if err != nil || !ok {
		return nil, err
	}
	records, err := agentworkspace.CollectFinalArtifacts(ctx, plan)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		logs.DebugContextf(ctx, "artifact: no final artifacts declared in manifest")
		return nil, nil
	}
	logs.DebugContextf(ctx, "artifact: collected %d final artifact(s) from manifest", len(records))
	payloads := make([]events.ArtifactPayload, 0, len(records))
	for _, record := range records {
		payload := artifactPayloadFromRecord(record)
		payloads = append(payloads, payload)
	}

	serverAddr := identity.ServerAddr()
	serverOrgID := identity.OrgID()
	projectPublicID := strings.TrimSpace(req.Workspace.ProjectID)
	logs.DebugContextf(ctx, "artifact: upload check, serverAddr=%q orgID=%d projectPublicID=%q", serverAddr, serverOrgID, projectPublicID)
	if serverAddr != "" && serverOrgID > 0 && projectPublicID != "" {
		srv := client.NewServerClient(serverAddr, identity.AppKey())

		storageCfg, cfgErr := srv.GetStorageConfig(ctx)
		if cfgErr != nil {
			logs.WarnContextf(ctx, "get storage config from server: %v", cfgErr)
			storageCfg = nil
		} else {
			logs.DebugContextf(ctx, "artifact: storage config scheme=%q bucket=%q", storageCfg.Scheme, storageCfg.Bucket)
		}

		for i, record := range records {
			logs.DebugContextf(ctx, "artifact: uploading record[%d] relativePath=%q filename=%q fileSize=%d", i, record.RelativePath, record.Filename, record.FileSize)
			storageURI, err := uploadArtifactToServer(ctx, srv, projectPublicID, record, storageCfg, plan.RepoDir)
			if err != nil {
				logs.WarnContextf(ctx, "upload artifact %s to server: %v", record.RelativePath, err)
				continue
			}
			payloads[i].StorageURI = storageURI
		}
	} else {
		logs.DebugContextf(ctx, "artifact: skip upload, conditions not met")
	}

	return payloads, nil
}

func uploadArtifactToServer(ctx context.Context, srv *client.ServerClient, projectPublicID string, record agentworkspace.ArtifactRecord, storageCfg *client.StorageConfig, repoDir string) (string, error) {
	absolute, err := agentworkspace.SafeJoin(repoDir, record.RelativePath)
	if err != nil {
		return "", fmt.Errorf("safe join %q: %w", record.RelativePath, err)
	}
	logs.DebugContextf(ctx, "artifact: resolved absolute path %q for relative path %q", absolute, record.RelativePath)
	data, err := os.ReadFile(absolute)
	if err != nil {
		return "", fmt.Errorf("read artifact file: %w", err)
	}
	logs.DebugContextf(ctx, "artifact: read file %q, %d bytes", absolute, len(data))

	randomID := snowflake.GenerateIDBase58()
	orgID := identity.OrgID()
	ext := filepath.Ext(record.OriginalName)
	storageFilename := randomID + ext
	key := fmt.Sprintf("projects/%d/%s/artifacts/%s", orgID, projectPublicID, storageFilename)

	bucket := ""
	scheme := "s3"
	if storageCfg != nil {
		bucket = storageCfg.Bucket
		scheme = storageCfg.Scheme
	}

	storageURI := ""
	if bucket != "" {
		uri, err := storage.BuildURI(scheme, bucket, key)
		if err != nil {
			return "", fmt.Errorf("build storage uri: %w", err)
		}
		storageURI = uri
	}

	logs.DebugContextf(ctx, "artifact: requesting presign upload URL, bucket=%q key=%q", bucket, key)
	uploadURL, err := srv.GetPresignUploadURL(ctx, bucket, key)
	if err != nil {
		return "", fmt.Errorf("get presign upload url: %w", err)
	}
	logs.DebugContextf(ctx, "artifact: got presign upload URL for key=%q", key)

	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	putReq.Header.Set("Content-Type", record.MimeType)
	putReq.ContentLength = record.FileSize

	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		return "", fmt.Errorf("upload artifact file: %w", err)
	}
	defer putResp.Body.Close()

	if putResp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(putResp.Body, 4096))
		return "", fmt.Errorf("upload artifact file returned %d: %s", putResp.StatusCode, strings.TrimSpace(string(body)))
	}

	logs.DebugContextf(ctx, "artifact: uploaded %q to storageURI=%q, status=%d", record.Filename, storageURI, putResp.StatusCode)
	return storageURI, nil
}

func artifactPayloadFromRecord(record agentworkspace.ArtifactRecord) events.ArtifactPayload {
	return events.ArtifactPayload{
		ArtifactID:    newArtifactID(),
		Title:         artifactTitle(record),
		Filename:      artifactFilename(record),
		OriginalName:  strings.TrimSpace(record.OriginalName),
		Description:   strings.TrimSpace(record.Description),
		MimeType:     strings.TrimSpace(record.MimeType),
		ArtifactType: artifactType(record.ArtifactType),
		FileSize:     record.FileSize,
		RelativePath: strings.TrimSpace(record.RelativePath),
		StorageKey:   strings.TrimSpace(record.StorageKey),
		StorageURI:   strings.TrimSpace(record.StorageURI),
		Sha256:       record.Sha256,
		Source:       artifactSource(record.Source),
		Status:       artifactStatus(record.Status),
		CreatedAt:    time.Now().UTC(),
	}
}

func newArtifactID() string {
	return "art_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func artifactTitle(record agentworkspace.ArtifactRecord) string {
	title := strings.TrimSpace(record.Title)
	if title != "" {
		return title
	}
	return strings.TrimSpace(record.RelativePath)
}

func artifactFilename(record agentworkspace.ArtifactRecord) string {
	filename := strings.TrimSpace(record.Filename)
	if filename != "" {
		return filename
	}
	return strings.TrimSpace(record.RelativePath)
}

func artifactType(value string) string {
	if strings.TrimSpace(value) == "" {
		return string(types.ArtifactTypeFile)
	}
	return strings.TrimSpace(value)
}

func artifactSource(value string) string {
	if strings.TrimSpace(value) == "" {
		return string(types.ArtifactSourceAgentDeclared)
	}
	return strings.TrimSpace(value)
}

func artifactStatus(value string) string {
	if strings.TrimSpace(value) == "" {
		return string(types.ArtifactStatusCompleted)
	}
	return strings.TrimSpace(value)
}

var _ ArtifactRecorder = (*WorkspaceArtifactRecorder)(nil)
