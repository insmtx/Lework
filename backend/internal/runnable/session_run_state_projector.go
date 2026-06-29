package runnable

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// StartSessionRunStateProjector 订阅 run.state lane，统一处理 session 运行状态投影。
//
// 消费 org.*.session.*.run.state，处理以下事件：
//   - run.started: 标记源用户消息为 processing，记录 replay start seq
//   - artifact.declared: 幂等持久化 artifact
//   - run.completed: 创建 completed assistant message
//   - run.failed / run.cancelled: 创建失败或取消 assistant message
//
// NOTE: 本 projector 只消费 run.state lane，不依赖 run.stream lane。
// SSE replay 目前仅订阅 run.stream lane（见 StreamSessionEvents）。
// 双 lane 回放待未来实现。
func StartSessionRunStateProjector(
	ictx context.Context,
	service contract.SessionService,
	eb eventbus.EventBus,
	db *gorm.DB,
) {
	ctx := logs.WithContextFields(ictx, "runnable", "session_run_state_projector")
	topic := messaging.RunEventStateWildcard()
	logs.InfoContextf(ctx, "starting session run state projector: %s", topic)

	persister := &declaredArtifactPersister{db: db}

	Run(ctx, "session_run_state_projector", func(ctx context.Context) {
		if err := eb.Subscribe(ctx, topic, messaging.SessionRunStateConsumer(), func(msg *nats.Msg) {
			handleRunStateMessage(ctx, service, persister, db, msg)
		}); err != nil {
			logs.ErrorContextf(ctx, "subscribe to %s failed: %v", topic, err)
		}
	})
}

func handleRunStateMessage(ctx context.Context, service contract.SessionService, persister *declaredArtifactPersister, db *gorm.DB, msg *nats.Msg) {
	var runEvent messaging.RunEvent
	if err := json.Unmarshal(msg.Data, &runEvent); err != nil {
		logs.WarnContextf(ctx, "unmarshal run state event: %v", err)
		return
	}
	if runEvent.Type != messaging.MessageTypeRunEvent {
		return
	}

	sessionID := runEvent.Route.SessionID
	if sessionID == "" {
		return
	}

	switch runEvent.Body.Event {
	case messaging.RunEventRunStarted:
		handleRunStartedEvent(ctx, service, msg, runEvent)

	case messaging.RunEventArtifactDeclared:
		handleArtifactDeclaredEvent(ctx, persister, runEvent)

	case messaging.RunEventRunCompleted:
		handleRunCompletedEvent(ctx, service, db, runEvent)

	case messaging.RunEventRunFailed:
		handleRunFailedEvent(ctx, service, runEvent)

	case messaging.RunEventRunCancelled:
		handleRunCancelledEvent(ctx, service, runEvent)

	default:
		logs.DebugContextf(ctx, "ignoring run state event: %s", runEvent.Body.Event)
	}
}

func handleRunStartedEvent(ctx context.Context, service contract.SessionService, msg *nats.Msg, runEvent messaging.RunEvent) {
	meta, err := msg.Metadata()
	if err != nil {
		logs.WarnContextf(ctx, "run started missing nats metadata: session_id=%s error=%v", runEvent.Route.SessionID, err)
		return
	}
	if err := service.HandleSessionRunStarted(ctx, &contract.SessionRunStartedRequest{
		SessionID:         runEvent.Route.SessionID,
		ReplyToMessageIDs: runEvent.Body.ReplyToMessageIDs,
		RequestID:         runEvent.Trace.RequestID,
		StreamStartSeq:    0, // set asynchronously by session_run_stream_projector
		StateStartSeq:     meta.Sequence.Stream,
	}); err != nil {
		logs.WarnContextf(ctx, "handle session run started failed: session_id=%s error=%v", runEvent.Route.SessionID, err)
	}
}

func handleArtifactDeclaredEvent(ctx context.Context, persister *declaredArtifactPersister, runEvent messaging.RunEvent) {
	if runEvent.Body.Payload.Artifact == nil {
		logs.WarnContextf(ctx, "artifact declared missing payload: session_id=%s seq=%d", runEvent.Route.SessionID, runEvent.Body.Seq)
		return
	}
	art := runEvent.Body.Payload.Artifact
	logs.InfoContextf(ctx, "persisting declared artifact: session_id=%s artifact_id=%s storage_key=%s",
		runEvent.Route.SessionID, art.ArtifactID, art.StorageKey)

	legacyArtifact := events.ArtifactPayload{
		ArtifactID:   art.ArtifactID,
		Title:        art.Title,
		Filename:     art.Filename,
		OriginalName: art.OriginalName,
		Description:  art.Description,
		MimeType:     art.MimeType,
		ArtifactType: art.ArtifactType,
		FileSize:     art.FileSize,
		RelativePath: art.RelativePath,
		StorageKey:   art.StorageKey,
		StorageURI:   art.StorageURI,
		Sha256:       art.Sha256,
		Source:       art.Source,
		Status:       art.Status,
	}

	if err := persister.PersistDeclaredArtifact(ctx, messaging.RouteContext{
		OrgID:     runEvent.Route.OrgID,
		SessionID: runEvent.Route.SessionID,
		WorkerID:  runEvent.Route.WorkerID,
	}, legacyArtifact); err != nil {
		logs.WarnContextf(ctx, "persist declared artifact failed: session_id=%s artifact_id=%s err=%v",
			runEvent.Route.SessionID, art.ArtifactID, err)
	}
}

func handleRunCompletedEvent(ctx context.Context, service contract.SessionService, db *gorm.DB, runEvent messaging.RunEvent) {
	completed := runEvent.Body.RunCompleted
	if completed == nil {
		logs.WarnContextf(ctx, "run completed missing run_completed payload: session_id=%s seq=%d", runEvent.Route.SessionID, runEvent.Body.Seq)
		return
	}
	req := &contract.CompleteSessionMessageRequest{
		SessionID:         runEvent.Route.SessionID,
		Content:           completed.Result.Message,
		ReplyToMessageIDs: runEvent.Body.ReplyToMessageIDs,
		Chunks:            messagingEventsToChunks(completed.Events),
		Artifacts:         messagingArtifactsToMessageArtifacts(completed.Artifacts),
		Metadata:          completedMetadataToObject(completed),
		Usage:             messagingUsageToMessageUsage(completed.Usage),
		Seq:               runEvent.Body.Seq,
		CreatedAt:         runEvent.CreatedAt,
	}
	if err := service.CompleteSessionMessage(ctx, req); err != nil {
		logs.WarnContextf(ctx, "complete session message: %v", err)
	}
	recordSkillInvocationsFromMessaging(ctx, db, runEvent.Route.OrgID, runEvent.Route.SessionID, completed.Events)
}

func handleRunFailedEvent(ctx context.Context, service contract.SessionService, runEvent messaging.RunEvent) {
	content := runEvent.Body.Payload.Content
	errMsg := runEvent.Body.Payload.Content
	status := string(types.MessageStatusFailed)
	completed := runEvent.Body.RunCompleted
	if completed != nil && completed.Result.Message != "" {
		content = completed.Result.Message
		errMsg = completed.Result.Message
		if completed.Status == string(types.MessageStatusCancelled) {
			status = string(types.MessageStatusCancelled)
			content = "已取消"
		}
	}
	if runEvent.Body.Error != nil {
		errMsg = runEvent.Body.Error.Message
	}
	req := &contract.FailedSessionMessageRequest{
		SessionID:         runEvent.Route.SessionID,
		Content:           content,
		ReplyToMessageIDs: runEvent.Body.ReplyToMessageIDs,
		ErrorMsg:          errMsg,
		Status:            status,
		Chunks:            messagingEventsToChunks(messagingCompletedEvents(completed)),
		Artifacts:         messagingArtifactsToMessageArtifacts(messagingCompletedArtifacts(completed)),
		Metadata:          completedMetadataToObject(completed),
		Usage:             messagingUsageToMessageUsage(messagingCompletedUsage(completed)),
		Seq:               runEvent.Body.Seq,
		CreatedAt:         runEvent.CreatedAt,
	}
	if runEvent.Body.Error != nil {
		req.ErrorCode = runEvent.Body.Error.Code
	}
	if err := service.FailedSessionMessage(ctx, req); err != nil {
		logs.WarnContextf(ctx, "failed session message: %v", err)
	}
}

func handleRunCancelledEvent(ctx context.Context, service contract.SessionService, runEvent messaging.RunEvent) {
	completed := runEvent.Body.RunCompleted
	content := "已取消"
	if completed != nil && completed.Result.Message != "" {
		content = completed.Result.Message
	}
	req := &contract.FailedSessionMessageRequest{
		SessionID:         runEvent.Route.SessionID,
		Content:           content,
		ReplyToMessageIDs: runEvent.Body.ReplyToMessageIDs,
		ErrorMsg:          cancellationError(runEvent),
		Status:            string(types.MessageStatusCancelled),
		Chunks:            messagingEventsToChunks(messagingCompletedEvents(completed)),
		Artifacts:         messagingArtifactsToMessageArtifacts(messagingCompletedArtifacts(completed)),
		Metadata:          completedMetadataToObject(completed),
		Usage:             messagingUsageToMessageUsage(messagingCompletedUsage(completed)),
		Seq:               runEvent.Body.Seq,
		CreatedAt:         runEvent.CreatedAt,
	}
	if err := service.FailedSessionMessage(ctx, req); err != nil {
		logs.WarnContextf(ctx, "cancelled session message: %v", err)
	}
}

func cancellationError(runEvent messaging.RunEvent) string {
	if runEvent.Body.Error != nil && strings.TrimSpace(runEvent.Body.Error.Message) != "" {
		return runEvent.Body.Error.Message
	}
	return "run cancelled"
}

// ---- type conversion helpers ----

func messagingEventsToChunks(records []messaging.RunEventRecord) []types.MessageChunk {
	if len(records) == 0 {
		return nil
	}
	chunks := make([]types.MessageChunk, 0, len(records))
	for _, record := range records {
		chunks = append(chunks, types.MessageChunk{
			Seq:       record.Seq,
			LastSeq:   record.LastSeq,
			Type:      record.Type,
			Timestamp: record.Timestamp,
			Payload:   json.RawMessage(record.Payload),
		})
	}
	return chunks
}

func messagingArtifactsToMessageArtifacts(artifacts []messaging.ArtifactPayload) []types.MessageArtifact {
	if len(artifacts) == 0 {
		return nil
	}
	result := make([]types.MessageArtifact, 0, len(artifacts))
	for _, a := range artifacts {
		result = append(result, types.MessageArtifact{
			ArtifactID:   a.ArtifactID,
			Title:        a.Title,
			Filename:     a.Filename,
			Description:  a.Description,
			MimeType:     a.MimeType,
			ArtifactType: a.ArtifactType,
			FileSize:     a.FileSize,
			StorageURI:   a.StorageURI,
			Sha256:       a.Sha256,
			CreatedAt:    time.Time{},
		})
	}
	return result
}

func messagingUsageToMessageUsage(usage *messaging.UsagePayload) *types.MessageUsage {
	if usage == nil {
		return nil
	}
	return &types.MessageUsage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
	}
}

func completedMetadataToObject(completed *messaging.RunCompletedPayload) *types.ObjectMetadata {
	if completed == nil || completed.Metadata == nil {
		return nil
	}
	extra := map[string]any{}
	if completed.Metadata.Runtime != "" {
		extra["runtime"] = completed.Metadata.Runtime
	}
	if completed.Metadata.WorkDir != "" {
		extra["work_dir"] = completed.Metadata.WorkDir
	}
	if completed.Metadata.ProviderID != "" {
		extra["provider_id"] = completed.Metadata.ProviderID
	}
	if completed.Metadata.SessionID != "" {
		extra["session_id"] = completed.Metadata.SessionID
	}
	if completed.Metadata.Phase != "" {
		extra["phase"] = completed.Metadata.Phase
	}
	if completed.Metadata.Resume {
		extra["resume"] = true
	}
	if completed.Usage != nil && completed.Usage.TotalTokens > 0 {
		extra["tokens"] = completed.Usage.TotalTokens
	}
	if len(extra) == 0 {
		return nil
	}
	return &types.ObjectMetadata{Extra: extra}
}

func messagingCompletedEvents(completed *messaging.RunCompletedPayload) []messaging.RunEventRecord {
	if completed == nil {
		return nil
	}
	return completed.Events
}

func messagingCompletedArtifacts(completed *messaging.RunCompletedPayload) []messaging.ArtifactPayload {
	if completed == nil {
		return nil
	}
	return completed.Artifacts
}

func messagingCompletedUsage(completed *messaging.RunCompletedPayload) *messaging.UsagePayload {
	if completed == nil {
		return nil
	}
	return completed.Usage
}

func recordSkillInvocationsFromMessaging(ctx context.Context, db *gorm.DB, orgID uint, sessionID string, runEvents []messaging.RunEventRecord) {
	if db == nil || len(runEvents) == 0 {
		return
	}

	var session types.Session
	if err := db.WithContext(ctx).Where("public_id = ?", sessionID).First(&session).Error; err != nil {
		return
	}

	seen := make(map[string]bool)
	var records []*types.MessageResource
	for _, evt := range runEvents {
		if evt.Type != string(events.EventToolCallStarted) {
			continue
		}
		payloadBytes, _ := json.Marshal(evt.Payload)
		var payload events.ToolCallPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			continue
		}
		skillName := extractSkillName(payload.Name, payload.Arguments)
		if skillName == "" || seen[skillName] {
			continue
		}
		seen[skillName] = true

		source, skillID := "Leros", skillName
		resourceID := ""
		if item, err := infradb.GetBuiltinSkillByID(ctx, db, skillName); err == nil && item != nil {
			source = "Leros"
			skillID = item.SkillID
			resourceID = fmt.Sprintf("%d", item.ID)
		}
		records = append(records, &types.MessageResource{
			ResourceID:   resourceID,
			ResourceKey:  source + ":" + skillID,
			OrgID:        orgID,
			Uin:          session.Uin,
			SessionID:    session.ID,
			ResourceType: "skill",
			ResourceName: skillName,
			InvokeType:   "tool_call",
		})
	}
	if len(records) > 0 {
		if err := infradb.BatchCreateMessageResources(ctx, db, records); err != nil {
			logs.WarnContextf(ctx, "recordSkillInvocations: batch create failed: %v", err)
		}
	}
}

// declaredArtifactPersister persists declared artifacts to the database.
type declaredArtifactPersister struct {
	db *gorm.DB
}

// PersistDeclaredArtifact persists a declared artifact to the database.
func (p *declaredArtifactPersister) PersistDeclaredArtifact(ctx context.Context, route messaging.RouteContext, item events.ArtifactPayload) error {
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
	storageURI := strings.TrimSpace(item.StorageURI)
	fileURL := storageURI
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
		RelativePath: item.RelativePath,
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

	if storageURI == "" {
		return nil
	}
	fileUpload, err := filestore.RecordUpload(ctx, p.db, filestore.RecordUploadParams{
		StorageURI:   storageURI,
		Filename:     filename,
		OriginalName: originalName,
		MimeType:     strings.TrimSpace(item.MimeType),
		OrgID:        session.OrgID,
		OwnerID:      session.Uin,
		FileSize:     item.FileSize,
		Sha256:       item.Sha256,
		Purpose:      filestore.PurposeArtifact,
	})
	if err != nil {
		return fmt.Errorf("record artifact upload: %w", err)
	}
	projectFile := &types.ProjectFile{
		FilePublicID: fileUpload.PublicID,
		OrgID:        session.OrgID,
		ProjectID:    *session.ProjectID,
		TaskID:       *session.TaskID,
		ResourceID:   artifact.ID,
		ResourceType: types.ProjectFileResourceTypeArtifact,
		Uin:          session.Uin,
	}
	if err := infradb.CreateProjectFile(ctx, p.db, projectFile); err != nil {
		return fmt.Errorf("create artifact project file: %w", err)
	}
	return nil
}

func artifactTitle(item events.ArtifactPayload) string {
	if t := strings.TrimSpace(item.Title); t != "" {
		return t
	}
	return strings.TrimSpace(item.Filename)
}

func artifactType(value string) string {
	if v := strings.TrimSpace(value); v != "" {
		return v
	}
	return "unknown"
}

func artifactSource(value string) string {
	if v := strings.TrimSpace(value); v != "" {
		return v
	}
	return "unknown"
}

func artifactStatus(value string) string {
	if v := strings.TrimSpace(value); v != "" {
		return v
	}
	return "pending"
}

func extractSkillName(toolName string, arguments json.RawMessage) string {
	if toolName == "" {
		return ""
	}
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "use_skill" || name == "invoke_skill" || name == "run_skill" {
		var input struct {
			Skill     string `json:"skill"`
			SkillName string `json:"skill_name"`
		}
		if json.Unmarshal(arguments, &input) == nil {
			if value := strings.TrimSpace(input.Skill); value != "" {
				return value
			}
			return strings.TrimSpace(input.SkillName)
		}
	}
	return ""
}
