package runnable

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"gorm.io/gorm"

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

	case messaging.RunEventPlanPublished:
		handlePlanPublishedEvent(ctx, persister, runEvent)

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

	artifact := messaging.ArtifactPayload{
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
	}, artifact); err != nil {
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

func handlePlanPublishedEvent(ctx context.Context, persister *declaredArtifactPersister, runEvent messaging.RunEvent) {
	if runEvent.Body.Payload.PlanPublished == nil {
		logs.WarnContextf(ctx, "plan published missing payload: session_id=%s seq=%d", runEvent.Route.SessionID, runEvent.Body.Seq)
		return
	}
	pp := runEvent.Body.Payload.PlanPublished
	logs.InfoContextf(ctx, "persisting published plan: session_id=%s file_id=%s", runEvent.Route.SessionID, pp.FileID)

	if err := persister.PersistPublishedPlan(ctx, messaging.RouteContext{
		OrgID:     runEvent.Route.OrgID,
		SessionID: runEvent.Route.SessionID,
		WorkerID:  runEvent.Route.WorkerID,
	}, pp); err != nil {
		logs.WarnContextf(ctx, "persist published plan failed: session_id=%s file_id=%s err=%v",
			runEvent.Route.SessionID, pp.FileID, err)
	}
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
		usage = &messaging.UsagePayload{}
	}
	return &types.MessageUsage{
		TotalTokens:       usage.InputTokens + usage.OutputTokens,
		InputTokens:       usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		CacheInputTokens:  usage.CacheInputTokens,
		CacheOutputTokens: usage.CacheOutputTokens,
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
	if completed.Usage != nil {
		totalTokens := completed.Usage.InputTokens + completed.Usage.OutputTokens
		if totalTokens > 0 {
			extra["tokens"] = totalTokens
		}
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
		if evt.Type != string(messaging.RunEventToolCallStarted) {
			continue
		}
		payloadBytes, _ := json.Marshal(evt.Payload)
		var payload messaging.ToolCallPayload
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

// PersistDeclaredArtifact persists a declared artifact as FileUpload + ProjectFile in a transaction.
// Uses ProjectFile.FilePublicID unique index for event replay idempotency.
// Skips persistence when storage_uri is missing (ProjectFile cannot point to an inaccessible file).
func (p *declaredArtifactPersister) PersistDeclaredArtifact(
	ctx context.Context,
	route messaging.RouteContext,
	item messaging.ArtifactPayload,
) error {
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
	storageURI := strings.TrimSpace(item.StorageURI)
	if storageURI == "" {
		logs.InfoContextf(ctx, "persist declared artifact: storage_uri is empty, skipping persistence artifact_id=%s session_id=%s", artifactID, sessionID)
		return nil
	}

	// Check idempotency via ProjectFile.FilePublicID unique index.
	existingPF, err := infradb.GetProjectFileByFilePublicID(ctx, p.db, route.OrgID, artifactID)
	if err != nil {
		return err
	}
	if existingPF != nil {
		logs.InfoContextf(ctx, "persist declared artifact: already exists (project_file), artifact_id=%s session_id=%s", artifactID, sessionID)
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

	filename := strings.TrimSpace(item.Filename)
	if filename == "" {
		filename = item.Title
	}
	originalName := strings.TrimSpace(item.OriginalName)
	if originalName == "" {
		originalName = filename
	}

	// Use transaction to ensure FileUpload and ProjectFile are created atomically.
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fileUpload, err := filestore.RecordUpload(ctx, tx, filestore.RecordUploadParams{
			StorageURI:   storageURI,
			Filename:     filename,
			OriginalName: originalName,
			MimeType:     strings.TrimSpace(item.MimeType),
			OrgID:        session.OrgID,
			OwnerID:      session.Uin,
			FileSize:     item.FileSize,
			Sha256:       item.Sha256,
			Purpose:      filestore.PurposeArtifact,
			PublicID:     artifactID,
		})
		if err != nil {
			return fmt.Errorf("record artifact upload: %w", err)
		}

		projectFile := &types.ProjectFile{
			FilePublicID: fileUpload.PublicID,
			OrgID:        session.OrgID,
			ProjectID:    *session.ProjectID,
			TaskID:       *session.TaskID,
			ResourceID:   fileUpload.ID,
			ResourceType: types.ProjectFileResourceTypeArtifact,
			Uin:          session.Uin,
		}
		if err := infradb.CreateProjectFile(ctx, tx, projectFile); err != nil {
			return fmt.Errorf("create artifact project file: %w", err)
		}
		return nil
	})
	if err != nil {
		logs.WarnContextf(ctx, "persist declared artifact: transaction failed, artifact_id=%s err=%v", artifactID, err)
		return err
	}

	logs.InfoContextf(ctx, "persist declared artifact: success, artifact_id=%s session_id=%s", artifactID, sessionID)
	return nil
}

// PersistPublishedPlan persists a published plan as FileUpload + ProjectFile in a transaction.
func (p *declaredArtifactPersister) PersistPublishedPlan(ctx context.Context, route messaging.RouteContext, pp *messaging.PlanPublishedPayload) error {
	if p == nil || p.db == nil {
		return nil
	}
	if pp == nil {
		return fmt.Errorf("plan published payload is nil")
	}
	fileID := strings.TrimSpace(pp.FileID)
	if fileID == "" {
		return fmt.Errorf("file_id is required")
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
	storageURI := strings.TrimSpace(pp.StorageURI)
	if storageURI == "" {
		logs.InfoContextf(ctx, "persist published plan: storage_uri is empty, skipping file_id=%s session_id=%s", fileID, sessionID)
		return nil
	}

	// Check idempotency via ProjectFile.FilePublicID unique index.
	existingPF, err := infradb.GetProjectFileByFilePublicID(ctx, p.db, route.OrgID, fileID)
	if err != nil {
		return err
	}
	if existingPF != nil {
		logs.InfoContextf(ctx, "persist published plan: already exists, file_id=%s session_id=%s", fileID, sessionID)
		return nil
	}

	session, err := infradb.GetSessionByPublicID(ctx, p.db, sessionID)
	if err != nil {
		return fmt.Errorf("find session %s: %w", sessionID, err)
	}
	if session == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}
	if session.OrgID != route.OrgID {
		return fmt.Errorf("session %s does not belong to org %d", sessionID, route.OrgID)
	}
	if session.ProjectID == nil || *session.ProjectID == 0 {
		return fmt.Errorf("session project_id is required")
	}
	if session.TaskID == nil || *session.TaskID == 0 {
		return fmt.Errorf("session task_id is required")
	}

	filename := strings.TrimSpace(pp.Filename)
	if filename == "" {
		filename = "plan.md"
	}
	originalName := strings.TrimSpace(pp.OriginalName)
	if originalName == "" {
		originalName = filename
	}
	mimeType := strings.TrimSpace(pp.MimeType)
	if mimeType == "" {
		mimeType = "text/markdown"
	}

	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fileUpload, err := filestore.RecordUpload(ctx, tx, filestore.RecordUploadParams{
			StorageURI:   storageURI,
			Filename:     filename,
			OriginalName: originalName,
			MimeType:     mimeType,
			OrgID:        session.OrgID,
			OwnerID:      session.Uin,
			FileSize:     pp.FileSize,
			Sha256:       pp.Sha256,
			Purpose:      filestore.PurposePlan,
			PublicID:     fileID,
		})
		if err != nil {
			return fmt.Errorf("record plan upload: %w", err)
		}

		projectFile := &types.ProjectFile{
			FilePublicID: fileUpload.PublicID,
			OrgID:        session.OrgID,
			ProjectID:    *session.ProjectID,
			TaskID:       *session.TaskID,
			ResourceID:   fileUpload.ID,
			ResourceType: types.ProjectFileResourceTypePlan,
			Uin:          session.Uin,
		}
		if err := infradb.CreateProjectFile(ctx, tx, projectFile); err != nil {
			return fmt.Errorf("create plan project file: %w", err)
		}
		return nil
	})
	if err != nil {
		logs.WarnContextf(ctx, "persist published plan: transaction failed, file_id=%s err=%v", fileID, err)
		return err
	}

	logs.InfoContextf(ctx, "persist published plan: success, file_id=%s session_id=%s", fileID, sessionID)
	return nil
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
