package runnable

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/insmtx/Leros/backend/types"
	"github.com/nats-io/nats.go"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"
)

func StartSessionArtifactDeclared(ictx context.Context, eb eventbus.EventBus, db *gorm.DB, _ interface{}) {
	ctx := logs.WithContextFields(ictx, "runnable", "session_artifact_declared")
	topic := dm.SessionResultStreamWildcardSubject()
	persister := &declaredArtifactPersister{db: db}
	logs.InfoContextf(ctx, "starting session artifact declared runnable: %s", topic)

	Run(ctx, "session_artifact_declared", func(ctx context.Context) {
		if err := eb.Subscribe(ctx, topic, dm.SessionArtifactDeclaredConsumer(), func(msg *nats.Msg) {
			handleSessionArtifactDeclaredMessage(ctx, persister, msg)
		}); err != nil {
			logs.ErrorContextf(ctx, "subscribe to %s failed: %v", topic, err)
		}
	})
}

type declaredArtifactPersister struct {
	db *gorm.DB
}

func handleSessionArtifactDeclaredMessage(ctx context.Context, persister *declaredArtifactPersister, msg *nats.Msg) {
	var streamMsg protocol.MessageStreamMessage
	if err := json.Unmarshal(msg.Data, &streamMsg); err != nil {
		logs.WarnContextf(ctx, "unmarshal session artifact declared message: %v", err)
		return
	}
	if streamMsg.Body.Event != protocol.StreamEventArtifactDeclared {
		return
	}
	if streamMsg.Body.Payload.Artifact == nil {
		logs.WarnContextf(ctx, "artifact declared message missing payload: session_id=%s seq=%d", streamMsg.Route.SessionID, streamMsg.Body.Seq)
		return
	}
	artifact := streamMsg.Body.Payload.Artifact
	logs.InfoContextf(ctx, "persisting declared artifact: session_id=%s artifact_id=%s storage_key=%s",
		streamMsg.Route.SessionID, artifact.ArtifactID, artifact.StorageKey)
	if err := persister.PersistDeclaredArtifact(ctx, streamMsg.Route, *artifact); err != nil {
		logs.WarnContextf(ctx, "persist declared artifact failed: session_id=%s artifact_id=%s err=%v",
			streamMsg.Route.SessionID, artifact.ArtifactID, err)
	} else {
		logs.InfoContextf(ctx, "persist declared artifact success: session_id=%s artifact_id=%s",
			streamMsg.Route.SessionID, artifact.ArtifactID)
	}
}

func (p *declaredArtifactPersister) PersistDeclaredArtifact(ctx context.Context, route protocol.RouteContext, item events.ArtifactPayload) error {
	if p == nil || p.db == nil {
		return nil
	}
	artifactID := strings.TrimSpace(item.ArtifactID)
	if artifactID == "" {
		return fmt.Errorf("artifact_id is required")
	}
	if route.OrgID == 0 {
		return fmt.Errorf("org_id is required")
	}
	if route.WorkerID == 0 {
		return fmt.Errorf("worker_id is required")
	}
	sessionID := strings.TrimSpace(route.SessionID)
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	storageKey := strings.TrimSpace(item.StorageKey)
	if storageKey == "" {
		logs.WarnContextf(ctx, "persist declared artifact: storage_key is empty, artifact_id=%s session_id=%s", artifactID, sessionID)
		return fmt.Errorf("storage_key is required")
	}

	existing, err := infradb.GetArtifactByPublicID(ctx, p.db, route.OrgID, artifactID)
	if err != nil {
		return err
	}
	if existing != nil {
		logs.InfoContextf(ctx, "persist declared artifact: already exists, artifact_id=%s session_id=%s", artifactID, sessionID)
		return nil
	}

	session, err := infradb.GetSessionByPublicID(ctx, p.db, sessionID)
	if err != nil {
		return fmt.Errorf("find session %s: %w", sessionID, err)
	}
	if session == nil {
		logs.WarnContextf(ctx, "persist declared artifact: session not found, artifact_id=%s session_id=%s", artifactID, sessionID)
		return fmt.Errorf("session %s not found", sessionID)
	}
	if session.OrgID != route.OrgID {
		logs.WarnContextf(ctx, "persist declared artifact: org mismatch, artifact_id=%s session_org=%d route_org=%d",
			artifactID, session.OrgID, route.OrgID)
		return fmt.Errorf("session %s does not belong to org %d", sessionID, route.OrgID)
	}
	if session.ProjectID == nil || *session.ProjectID == 0 {
		logs.WarnContextf(ctx, "persist declared artifact: session has no project_id, artifact_id=%s session_id=%s",
			artifactID, sessionID)
		return fmt.Errorf("session project_id is required for artifact persistence")
	}
	if session.TaskID == nil || *session.TaskID == 0 {
		logs.WarnContextf(ctx, "persist declared artifact: session has no task_id, artifact_id=%s session_id=%s",
			artifactID, sessionID)
		return fmt.Errorf("session task_id is required for artifact persistence")
	}

	projects, err := infradb.GetProjectsByIDs(ctx, p.db, []uint{*session.ProjectID})
	if err != nil {
		return fmt.Errorf("find project %d: %w", *session.ProjectID, err)
	}
	if len(projects) == 0 {
		return fmt.Errorf("project %d not found", *session.ProjectID)
	}
	project := projects[0]
	projectPublicID := project.PublicID

	filename := strings.TrimSpace(item.Filename)
	if filename == "" {
		filename = item.Title
	}
	originalName := strings.TrimSpace(item.OriginalName)
	if originalName == "" {
		originalName = filename
	}

	storagePathURI := strings.TrimSpace(item.StoragePathURI)
	fileURL := storagePathURI
	if fileURL == "" {
		fileURL = projectPublicID + "/" + storageKey
	}

	artifact := &types.Artifact{
		PublicID:     artifactID,
		OrgID:        session.OrgID,
		OwnerID:      session.Uin,
		TaskID:       *session.TaskID,
		ProjectID:    *session.ProjectID,
		SessionID:    &session.ID,
		Title:        artifactTitle(item),
		Filename:     filename,
		Description:  strings.TrimSpace(item.Description),
		ArtifactType: artifactType(item.ArtifactType),
		FileURL:      fileURL,
		FileSize:     item.FileSize,
		RelativePath:  item.RelativePath,
		StorageKey:   storageKey,
		MimeType:     item.MimeType,
		Sha256:       item.Sha256,
		Source:       artifactSource(item.Source),
		Status:       artifactStatus(item.Status),
		Metadata: types.ObjectMetadata{
			Extra: map[string]interface{}{
				"worker_id":         route.WorkerID,
				"project_public_id": projectPublicID,
			},
		},
	}
	if artifact.Title == "" {
		artifact.Title = filename
	}
	if err := infradb.CreateArtifact(ctx, p.db, artifact); err != nil {
		logs.WarnContextf(ctx, "persist declared artifact: create artifact record failed, artifact_id=%s err=%v", artifactID, err)
		existing, findErr := infradb.GetArtifactByPublicID(ctx, p.db, route.OrgID, artifactID)
		if findErr == nil && existing != nil {
			return nil
		}
		return err
	}

	if storagePathURI != "" {
		mimeType := strings.TrimSpace(item.MimeType)
		fileUpload, err := filestore.RecordUpload(ctx, p.db, filestore.RecordUploadParams{
			StorageURI:   storagePathURI,
			Filename:     filename,
			OriginalName: originalName,
			MimeType:     mimeType,
			OrgID:        session.OrgID,
			OwnerID:      session.Uin,
			FileSize:     item.FileSize,
			Sha256:       item.Sha256,
			Purpose:      filestore.PurposeArtifact,
			Metadata: map[string]interface{}{
				"worker_id":         route.WorkerID,
				"project_public_id": projectPublicID,
			},
		})
		pfStoragePath := storagePathURI
		if err != nil {
			logs.WarnContextf(ctx, "persist declared artifact: record upload failed: %v", err)
		} else {
			pfStoragePath = fileUpload.StoragePath
		}

		pf := &types.ProjectFile{
			PublicID:        fmt.Sprintf("file_%s", snowflake.GenerateIDBase58()),
			OrgID:           session.OrgID,
			ProjectID:       *session.ProjectID,
			ProjectPublicID: projectPublicID,
			Filename:        filename,
			OriginalName:    originalName,
			MimeType:        mimeType,
			FileSize:        item.FileSize,
			StoragePath:     pfStoragePath,
			Sha256:          item.Sha256,
			Source:          "worker_artifact",
			ArtifactID:      &artifact.ID,
		}
		if err := infradb.CreateProjectFile(ctx, p.db, pf); err != nil {
			logs.WarnContextf(ctx, "persist declared artifact: create project file record failed: %v", err)
		}
	}

	return nil
}