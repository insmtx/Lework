package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
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

// Record 收集单次运行的最终 manifest 产物。
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
		return nil, nil
	}
	payloads := make([]events.ArtifactPayload, 0, len(records))
	for _, record := range records {
		payloads = append(payloads, artifactPayloadFromRecord(record))
	}
	return payloads, nil
}

func artifactPayloadFromRecord(record agentworkspace.ArtifactRecord) events.ArtifactPayload {
	return events.ArtifactPayload{
		ArtifactID:   newArtifactID(),
		Title:        artifactTitle(record),
		Filename:     artifactFilename(record),
		Description:  strings.TrimSpace(record.Description),
		MimeType:     strings.TrimSpace(record.MimeType),
		ArtifactType: artifactType(record.ArtifactType),
		FileSize:     record.FileSize,
		RelativePath: strings.TrimSpace(record.RelativePath),
		StorageKey:   strings.TrimSpace(record.StorageKey),
		Sha256:       record.Sha256,
		Source:       artifactSource(record.Source),
		Status:       artifactStatus(record.Status),
		// 中文注释：产物在运行期声明时就附带时间，供历史消息直接展示，无需再依赖任务接口二次补齐。
		CreatedAt: time.Now().UTC(),
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
