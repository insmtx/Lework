package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ygpkg/storage-go"

	"github.com/insmtx/Leros/backend/agent"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	"github.com/insmtx/Leros/backend/internal/worker/client"
	"github.com/insmtx/Leros/backend/internal/worker/identity"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
)

// finalizer is the concrete RunFinalizer implementation.
type finalizer struct{}

// NewFinalizer creates a new RunFinalizer.
func NewFinalizer() Finalizer {
	return &finalizer{}
}

// FinalizeRequired performs the required finalization steps in order:
//
//  1. Reconcile workspace changes
//  2. Collect artifact facts
//  3. Stage/commit/push workspace
//  4. Build final RunResult
func (f *finalizer) FinalizeRequired(
	ctx context.Context,
	run *PreparedRun,
	runtimeResult *agent.ExecutionResult,
	snapshot JournalSnapshot,
) (*Finalization, error) {
	if run == nil || runtimeResult == nil {
		return nil, fmt.Errorf("prepared run and runtime result are required")
	}

	req := run.Request
	if req == nil {
		return nil, fmt.Errorf("prepared run request is required")
	}

	// 1. Reconcile workspace (detect auto-generated artifacts).
	if err := reconcileWorkspace(ctx, run.Workspace); err != nil {
		return nil, fmt.Errorf("reconcile workspace: %w", err)
	}

	// 2. Collect artifact facts.
	artifactEvents, artifacts, err := collectArtifacts(ctx, run.Workspace, req.Workspace.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("collect artifacts: %w", err)
	}

	// 3. Stage/commit/push workspace.
	if err := pushWorkspace(ctx, run.Workspace); err != nil {
		return nil, fmt.Errorf("push workspace: %w", err)
	}

	// 4. Build final RunResult.
	result := &assistantdomain.RunResult{
		RunID:       req.RunID,
		TraceID:     req.TraceID,
		Status:      assistantdomain.RunStatusCompleted,
		Message:     runtimeResult.Message,
		Usage:       runtimeResult.Usage,
		ToolCalls:   runtimeResult.ToolCalls,
		Artifacts:   artifacts,
		CompletedAt: time.Now().UTC(),
		Metadata: &assistantdomain.RunMetadata{
			Runtime:    run.Execution.Runtime,
			WorkDir:    run.Workspace.WorkDir,
			ProviderID: runtimeResult.ProviderConversationID,
		},
	}

	return &Finalization{
		Result: result,
		Events: artifactEvents,
	}, nil
}

// PostRunBestEffort executes best-effort tasks after the terminal event.
// Currently performs no operations — learning is deferred to a separate phase.
func (f *finalizer) PostRunBestEffort(
	ctx context.Context,
	run *PreparedRun,
	result *assistantdomain.RunResult,
	snapshot JournalSnapshot,
) {
	// Learning, metrics, and diagnostics will be wired here in a follow-up.
	// Errors must not modify the run result.
	_ = ctx
	_ = run
	_ = result
}

// reconcileWorkspace compares repo state against baseline and auto-detects changes.
func reconcileWorkspace(ctx context.Context, workspace WorkspacePreparation) error {
	plan, ok := workspacePlan(workspace)
	if !ok {
		return nil
	}
	if _, err := os.Stat(plan.RepoDir); os.IsNotExist(err) {
		return nil
	}
	return agentworkspace.ReconcileArtifacts(ctx, plan)
}

// collectArtifacts reads the final artifact manifest and produces events.
func collectArtifacts(
	ctx context.Context,
	workspace WorkspacePreparation,
	projectPublicID string,
) ([]*agent.Event, []assistantdomain.ArtifactRecord, error) {
	plan, ok := workspacePlan(workspace)
	if !ok {
		return nil, nil, nil
	}
	records, err := agentworkspace.CollectFinalArtifacts(ctx, plan)
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, nil
	}

	uploadArtifacts(ctx, records, plan.RepoDir, projectPublicID)

	events := make([]*agent.Event, 0, len(records))
	artifacts := make([]assistantdomain.ArtifactRecord, 0, len(records))
	for _, record := range records {
		payload := artifactPayloadFromRecord(record)
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal artifact payload: %w", err)
		}
		events = append(events, &agent.Event{
			Type:    agent.EventType("artifact.declared"),
			Payload: json.RawMessage(raw),
			Content: payload.Title,
		})
		artifacts = append(artifacts, assistantdomain.ArtifactRecord{
			ArtifactID:   payload.ArtifactID,
			Title:        payload.Title,
			Filename:     payload.Filename,
			OriginalName: payload.OriginalName,
			Description:  payload.Description,
			MimeType:     payload.MimeType,
			ArtifactType: payload.ArtifactType,
			FileSize:     payload.FileSize,
			RelativePath: payload.RelativePath,
			StorageKey:   payload.StorageKey,
			StorageURI:   payload.StorageURI,
			Sha256:       payload.Sha256,
			Source:       payload.Source,
			Status:       payload.Status,
		})
	}
	return events, artifacts, nil
}

func uploadArtifacts(
	ctx context.Context,
	records []agentworkspace.ArtifactRecord,
	repoDir string,
	projectPublicID string,
) {
	serverAddr := strings.TrimSpace(identity.ServerAddr())
	projectPublicID = strings.TrimSpace(projectPublicID)
	if serverAddr == "" || identity.OrgID() == 0 || projectPublicID == "" {
		return
	}

	serverClient := client.NewServerClient(serverAddr, identity.AppKey())
	storageConfig, err := serverClient.GetStorageConfig(ctx)
	if err != nil {
		logs.WarnContextf(ctx, "get storage config from server: %v", err)
		storageConfig = nil
	}

	for i := range records {
		storageURI, err := uploadArtifact(
			ctx,
			serverClient,
			storageConfig,
			repoDir,
			projectPublicID,
			records[i],
		)
		if err != nil {
			logs.WarnContextf(ctx, "upload artifact %s to server: %v", records[i].RelativePath, err)
			continue
		}
		records[i].StorageURI = storageURI
	}
}

func uploadArtifact(
	ctx context.Context,
	serverClient *client.ServerClient,
	storageConfig *client.StorageConfig,
	repoDir string,
	projectPublicID string,
	record agentworkspace.ArtifactRecord,
) (string, error) {
	absolutePath, err := agentworkspace.SafeJoin(repoDir, record.RelativePath)
	if err != nil {
		return "", fmt.Errorf("resolve artifact path %q: %w", record.RelativePath, err)
	}
	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return "", fmt.Errorf("read artifact file: %w", err)
	}

	storageFilename := snowflake.GenerateIDBase58() + filepath.Ext(record.OriginalName)
	key := fmt.Sprintf(
		"projects/%d/%s/artifacts/%s",
		identity.OrgID(),
		projectPublicID,
		storageFilename,
	)

	bucket := ""
	scheme := "s3"
	if storageConfig != nil {
		bucket = storageConfig.Bucket
		scheme = storageConfig.Scheme
	}

	storageURI := ""
	if bucket != "" {
		storageURI, err = storage.BuildURI(scheme, bucket, key)
		if err != nil {
			return "", fmt.Errorf("build storage uri: %w", err)
		}
	}

	uploadURL, err := serverClient.GetPresignUploadURL(ctx, bucket, key)
	if err != nil {
		return "", fmt.Errorf("get presign upload url: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create artifact upload request: %w", err)
	}
	request.Header.Set("Content-Type", record.MimeType)
	request.ContentLength = record.FileSize

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("upload artifact file: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf(
			"upload artifact file returned %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}
	return storageURI, nil
}

// pushWorkspace stages, commits, and pushes workspace changes.
func pushWorkspace(ctx context.Context, workspace WorkspacePreparation) error {
	repoDir := strings.TrimSpace(workspace.RepoDir)
	if repoDir == "" {
		return nil
	}
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return nil
	}

	addCmd := exec.CommandContext(ctx, "git", "add", ".")
	addCmd.Dir = repoDir
	if output, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w: %s", err, strings.TrimSpace(string(output)))
	}

	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", "task: agent run artifacts")
	commitCmd.Dir = repoDir
	commitCmd.Env = identity.GitAuthorEnv()
	if output, err := commitCmd.CombinedOutput(); err != nil {
		logs.ErrorContextf(ctx, "git commit artifacts: %v: %s", err, strings.TrimSpace(string(output)))
		return nil // clean working tree is not an error
	}

	// 本地 git init 的仓库没有 origin，跳过 push
	hasRemote := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	hasRemote.Dir = repoDir
	if hasRemote.Run() != nil {
		logs.InfoContextf(ctx, "push_workspace skipped: no origin remote (local repo)")
		return nil
	}
	pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
	pushCmd.Dir = repoDir
	if output, err := pushCmd.CombinedOutput(); err != nil {
		logs.ErrorContextf(ctx, "git push failed: %v: %s", err, strings.TrimSpace(string(output)))
		return fmt.Errorf("git push: %w: %s", err, strings.TrimSpace(string(output)))
	}
	logs.InfoContextf(ctx, "push_workspace completed: repo_dir=%s", repoDir)
	return nil
}

func workspacePlan(workspace WorkspacePreparation) (*agentworkspace.TaskWorkspace, bool) {
	repoDir := strings.TrimSpace(workspace.RepoDir)
	manifestPath := strings.TrimSpace(workspace.ArtifactManifestPath)
	if repoDir == "" || manifestPath == "" {
		return nil, false
	}
	baselinePath := strings.TrimSpace(workspace.BaselinePath)
	if baselinePath == "" {
		baselinePath = filepath.Join(filepath.Dir(manifestPath), "baseline.jsonl")
	}
	return &agentworkspace.TaskWorkspace{
		RepoDir:              repoDir,
		TaskDir:              strings.TrimSpace(workspace.TaskDir),
		ArtifactManifestPath: manifestPath,
		BaselinePath:         baselinePath,
		EffectiveWorkDir:     strings.TrimSpace(workspace.WorkDir),
	}, true
}

// artifactPayloadFromRecord converts a workspace artifact record to an event payload.
func artifactPayloadFromRecord(record agentworkspace.ArtifactRecord) ArtifactPayload {
	return ArtifactPayload{
		ArtifactID:   newArtifactID(),
		Title:        artifactTitle(record),
		Filename:     artifactFilename(record),
		OriginalName: strings.TrimSpace(record.OriginalName),
		Description:  strings.TrimSpace(record.Description),
		MimeType:     strings.TrimSpace(record.MimeType),
		ArtifactType: artifactTypeValue(record.ArtifactType),
		FileSize:     record.FileSize,
		RelativePath: strings.TrimSpace(record.RelativePath),
		StorageKey:   strings.TrimSpace(record.StorageKey),
		StorageURI:   strings.TrimSpace(record.StorageURI),
		Sha256:       record.Sha256,
		Source:       artifactSourceValue(record.Source),
		Status:       artifactStatusValue(record.Status),
	}
}

// ArtifactPayload is a local copy to avoid depending on the events package.
type ArtifactPayload struct {
	ArtifactID   string `json:"artifact_id,omitempty"`
	Title        string `json:"title,omitempty"`
	Filename     string `json:"filename,omitempty"`
	OriginalName string `json:"original_name,omitempty"`
	Description  string `json:"description,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
	ArtifactType string `json:"artifact_type,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
	RelativePath string `json:"relative_path,omitempty"`
	StorageKey   string `json:"storage_key,omitempty"`
	StorageURI   string `json:"storage_uri,omitempty"`
	Sha256       string `json:"sha256,omitempty"`
	Source       string `json:"source,omitempty"`
	Status       string `json:"status,omitempty"`
}

func newArtifactID() string {
	return "art_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func artifactTitle(record agentworkspace.ArtifactRecord) string {
	if title := strings.TrimSpace(record.Title); title != "" {
		return title
	}
	return strings.TrimSpace(record.RelativePath)
}

func artifactFilename(record agentworkspace.ArtifactRecord) string {
	if filename := strings.TrimSpace(record.Filename); filename != "" {
		return filename
	}
	return strings.TrimSpace(record.RelativePath)
}

func artifactTypeValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return string(types.ArtifactTypeFile)
	}
	return strings.TrimSpace(value)
}

func artifactSourceValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return string(types.ArtifactSourceAgentDeclared)
	}
	return strings.TrimSpace(value)
}

func artifactStatusValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return string(types.ArtifactStatusCompleted)
	}
	return strings.TrimSpace(value)
}
