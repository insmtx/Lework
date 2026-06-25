package runnable

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/nats-io/nats.go"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// StartSessionCompleted subscribes to session completed events and dispatches to the service.
func StartSessionCompleted(ictx context.Context, service contract.SessionService, eb eventbus.EventBus, db *gorm.DB) {
	ctx := logs.WithContextFields(ictx, "runnable", "session_completed")
	topic := dm.SessionMessageCompletedWildcardSubject()
	logs.InfoContextf(ctx, "starting session completed runnable: %s", topic)

	Run(ctx, "session_completed", func(ctx context.Context) {
		if err := eb.Subscribe(ctx, topic, dm.SessionCompletedConsumer(), func(msg *nats.Msg) {
			handleSessionCompletedMessage(ctx, service, db, msg)
		}); err != nil {
			logs.ErrorContextf(ctx, "subscribe to %s failed: %v", topic, err)
		}
	})
}

func handleSessionCompletedMessage(ctx context.Context, service contract.SessionService, db *gorm.DB, msg *nats.Msg) {
	var streamMsg protocol.MessageStreamMessage
	if err := json.Unmarshal(msg.Data, &streamMsg); err != nil {
		logs.WarnContextf(ctx, "unmarshal session completed message: %v", err)
		return
	}

	sessionID := streamMsg.Route.SessionID
	if sessionID == "" {
		return
	}

	logs.InfoContextf(ctx, "[skill-record] handleSessionCompletedMessage: session=%s event=%s org=%d",
		sessionID, streamMsg.Body.Event, streamMsg.Route.OrgID)

	switch streamMsg.Body.Event {
	case protocol.StreamEventRunCompleted:
		completed := streamMsg.Body.RunCompleted
		if completed == nil {
			logs.WarnContextf(ctx, "run completed message missing run_completed payload: session_id=%s seq=%d", sessionID, streamMsg.Body.Seq)
			return
		}
		projectCompletedArtifacts(completed)
		replyToMessageIDs := replyToMessageIDsFromStream(streamMsg)
		req := &contract.CompleteSessionMessageRequest{
			SessionID:         sessionID,
			Content:           completed.Result.Message,
			ReplyToMessageIDs: replyToMessageIDs,
			Chunks:            runEventChunks(completed.Events),
			Artifacts:         messageArtifactsFromRunCompleted(completed.Artifacts),
			Metadata:          messageMetadataFromRunCompleted(completed),
			Usage:             messageUsageFromRuntime(completed.Usage),
			Seq:               streamMsg.Body.Seq,
			CreatedAt:         streamMsg.CreatedAt,
		}
		if err := service.CompleteSessionMessage(ctx, req); err != nil {
			logs.WarnContextf(ctx, "complete session message: %v", err)
		}
		recordSkillInvocations(ctx, db, streamMsg.Route.OrgID, streamMsg.Route.SessionID, completed.Events)

	case protocol.StreamEventRunFailed:
		errMsg := streamMsg.Body.Payload.Content
		status := string(types.MessageStatusFailed)
		completed := streamMsg.Body.RunCompleted
		if completed != nil && completed.Result.Message != "" {
			errMsg = completed.Result.Message
			if completed.Status == string(types.MessageStatusCancelled) {
				status = string(types.MessageStatusCancelled)
			}
		}
		if streamMsg.Body.Error != nil {
			errMsg = streamMsg.Body.Error.Message
		}
		projectCompletedArtifacts(completed)
		replyToMessageIDs := replyToMessageIDsFromStream(streamMsg)
		req := &contract.FailedSessionMessageRequest{
			SessionID:         sessionID,
			Content:           errMsg,
			ReplyToMessageIDs: replyToMessageIDs,
			ErrorMsg:          errMsg,
			Status:            status,
			Chunks:            runEventChunks(runCompletedEvents(completed)),
			Artifacts:         messageArtifactsFromRunCompleted(runCompletedArtifacts(completed)),
			Metadata:          messageMetadataFromRunCompleted(completed),
			Usage:             messageUsageFromRuntime(runCompletedUsage(completed)),
			Seq:               streamMsg.Body.Seq,
			CreatedAt:         streamMsg.CreatedAt,
		}
		if streamMsg.Body.Error != nil {
			req.ErrorCode = streamMsg.Body.Error.Code
		}
		if err := service.FailedSessionMessage(ctx, req); err != nil {
			logs.WarnContextf(ctx, "failed session message: %v", err)
		}

	default:
		logs.DebugContextf(ctx, "ignoring session completed event: %s", streamMsg.Body.Event)
	}
}

func replyToMessageIDsFromStream(streamMsg protocol.MessageStreamMessage) []string {
	if len(streamMsg.Body.ReplyToMessageIDs) > 0 {
		return deduplicateTrimmedIDs(streamMsg.Body.ReplyToMessageIDs)
	}
	requestID := strings.TrimSpace(streamMsg.Trace.RequestID)
	if !strings.HasPrefix(requestID, "req_") {
		return nil
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(requestID, "req_"), 10, 64)
	if err != nil || id == 0 {
		return nil
	}
	return []string{strconv.FormatUint(id, 10)}
}

// deduplicateTrimmedIDs normalizes and deduplicates string message IDs.
func deduplicateTrimmedIDs(rawIDs []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func projectCompletedArtifacts(completed *events.RunCompletedPayload) {
	if completed == nil {
		return
	}
	completed.Artifacts = publicArtifactPayloads(completed.Artifacts)
	updateRunArtifactEventRecords(completed.Events, completed.Artifacts)
}

func updateRunArtifactEventRecords(records []events.RunEventRecord, artifacts []events.ArtifactPayload) {
	if len(records) == 0 || len(artifacts) == 0 {
		return
	}
	next := 0
	for i := range records {
		if records[i].Type != events.EventArtifactDeclared {
			continue
		}
		if next >= len(artifacts) {
			return
		}
		payload, err := json.Marshal(artifacts[next])
		if err != nil {
			continue
		}
		records[i].Payload = payload
		next++
	}
}

func publicArtifactPayloads(artifacts []events.ArtifactPayload) []events.ArtifactPayload {
	if len(artifacts) == 0 {
		return nil
	}
	result := make([]events.ArtifactPayload, 0, len(artifacts))
	for _, artifact := range artifacts {
		result = append(result, publicArtifactPayload(artifact))
	}
	return result
}

func publicArtifactPayload(artifact events.ArtifactPayload) events.ArtifactPayload {
	return events.ArtifactPayload{
		ArtifactID:   strings.TrimSpace(artifact.ArtifactID),
		Title:        strings.TrimSpace(artifact.Title),
		Filename:     artifactFilename(artifact),
		MimeType:     strings.TrimSpace(artifact.MimeType),
		ArtifactType: artifactType(artifact.ArtifactType),
		CreatedAt:    artifact.CreatedAt,
	}
}

func runCompletedEvents(completed *events.RunCompletedPayload) []events.RunEventRecord {
	if completed == nil {
		return nil
	}
	return completed.Events
}

func runCompletedArtifacts(completed *events.RunCompletedPayload) []events.ArtifactPayload {
	if completed == nil {
		return nil
	}
	return completed.Artifacts
}

func runCompletedUsage(completed *events.RunCompletedPayload) *events.UsagePayload {
	if completed == nil {
		return nil
	}
	return completed.Usage
}

func artifactTitle(item events.ArtifactPayload) string {
	if strings.TrimSpace(item.Title) != "" {
		return strings.TrimSpace(item.Title)
	}
	return strings.TrimSpace(item.RelativePath)
}

func artifactFilename(item events.ArtifactPayload) string {
	if strings.TrimSpace(item.Filename) != "" {
		return strings.TrimSpace(item.Filename)
	}
	return strings.TrimSpace(item.RelativePath)
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

func runEventChunks(records []events.RunEventRecord) []types.MessageChunk {
	if len(records) == 0 {
		return nil
	}
	chunks := make([]types.MessageChunk, 0, len(records))
	for _, record := range records {
		chunks = append(chunks, types.MessageChunk{
			Seq:       record.Seq,
			LastSeq:   record.LastSeq,
			Type:      string(record.Type),
			Timestamp: record.Timestamp,
			Payload:   append([]byte(nil), record.Payload...),
		})
	}
	return chunks
}

func messageArtifactsFromRunCompleted(artifacts []events.ArtifactPayload) []types.MessageArtifact {
	if len(artifacts) == 0 {
		return nil
	}
	result := make([]types.MessageArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		result = append(result, types.MessageArtifact{
			ArtifactID:   artifact.ArtifactID,
			Title:        artifact.Title,
			Filename:     artifact.Filename,
			MimeType:     artifact.MimeType,
			ArtifactType: artifact.ArtifactType,
			CreatedAt:    artifact.CreatedAt,
		})
	}
	return result
}

func messageUsageFromRuntime(usage *events.UsagePayload) *types.MessageUsage {
	if usage == nil {
		return nil
	}
	return &types.MessageUsage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
	}
}

func messageMetadataFromRunCompleted(completed *events.RunCompletedPayload) *types.ObjectMetadata {
	if completed == nil {
		return nil
	}

	msgMetadata := &types.ObjectMetadata{}
	extra := map[string]any{}

	if completed.Metadata != nil {
		data, err := json.Marshal(completed.Metadata)
		if err != nil {
			return nil
		}
		if err := json.Unmarshal(data, msgMetadata); err != nil {
			return nil
		}

		var flat map[string]any
		if err := json.Unmarshal(data, &flat); err != nil {
			return nil
		}

		knownKeys := map[string]bool{"tags": true, "type": true, "bucket": true, "key": true}
		for k, v := range flat {
			if knownKeys[k] {
				continue
			}
			extra[k] = v
		}
	}

	// 写入前端消息 footer 使用的标准展示字段（model / tokens / latency）。
	enrichMessageDisplayMetadata(extra, completed)
	if len(extra) > 0 {
		msgMetadata.Extra = extra
	}

	if isEmptyObjectMetadata(msgMetadata) {
		return nil
	}
	return msgMetadata
}

// enrichMessageDisplayMetadata 将单次 run 的模型、token 与耗时写入 metadata.extra，供前端按消息展示。
func enrichMessageDisplayMetadata(extra map[string]any, completed *events.RunCompletedPayload) {
	if extra == nil || completed == nil {
		return
	}
	if model := messageDisplayModel(extra, completed.Metadata); model != "" {
		extra["model"] = model
	}
	if completed.Usage != nil && completed.Usage.TotalTokens > 0 {
		extra["tokens"] = completed.Usage.TotalTokens
	}
	if latencyMS := runCompletedLatencyMS(completed); latencyMS > 0 {
		extra["latency"] = latencyMS
	}
}

func messageDisplayModel(extra map[string]any, src map[string]any) string {
	if model := metadataStringValue(extra["model"]); model != "" {
		return model
	}
	if model := metadataStringValue(extra["model_name"]); model != "" {
		return model
	}
	if src == nil {
		return ""
	}
	if model := metadataStringValue(src["model"]); model != "" {
		return model
	}
	return metadataStringValue(src["model_name"])
}

func runCompletedLatencyMS(completed *events.RunCompletedPayload) int64 {
	if completed == nil || completed.StartedAt.IsZero() || completed.CompletedAt.IsZero() {
		return 0
	}
	if completed.CompletedAt.Before(completed.StartedAt) {
		return 0
	}
	return completed.CompletedAt.Sub(completed.StartedAt).Milliseconds()
}

func metadataStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func isEmptyObjectMetadata(metadata *types.ObjectMetadata) bool {
	if metadata == nil {
		return true
	}
	if len(metadata.Tags) > 0 || metadata.Type != "" || metadata.Bucket != "" || metadata.Key != "" {
		return false
	}
	return len(metadata.Extra) == 0
}

func recordSkillInvocations(ctx context.Context, db *gorm.DB, orgID uint, sessionID string, runEvents []events.RunEventRecord) {
	logs.InfoContextf(ctx, "[skill-record] recordSkillInvocations called: db=%v runEvents=%d session=%s",
		db != nil, len(runEvents), sessionID)
	if db == nil || len(runEvents) == 0 {
		if db == nil {
			logs.WarnContextf(ctx, "[skill-record] db is nil, skip")
		}
		if len(runEvents) == 0 {
			logs.WarnContextf(ctx, "[skill-record] runEvents is empty, skip")
		}
		return
	}

	// print event types for diagnosis
	for i, evt := range runEvents {
		logs.InfoContextf(ctx, "[skill-record] event[%d] type=%s payloadLen=%d", i, evt.Type, len(evt.Payload))
	}

	var session types.Session
	if err := db.WithContext(ctx).Where("public_id = ?", sessionID).First(&session).Error; err != nil {
		logs.WarnContextf(ctx, "recordSkillInvocations: session not found: %s err=%v", sessionID, err)
		return
	}

	seen := make(map[string]bool)
	var records []*types.MessageResource
	for _, evt := range runEvents {
		if evt.Type != events.EventToolCallStarted {
			continue
		}

		var payload events.ToolCallPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			logs.WarnContextf(ctx, "[skill-record] unmarshal ToolCallPayload failed: type=%s payload=%s err=%v",
				evt.Type, string(evt.Payload), err)
			continue
		}

		skillName := extractSkillName(payload.Name, payload.Arguments)
		logs.InfoContextf(ctx, "[skill-record] tool_call.started: tool=%s args=%v => skillName=%s",
			payload.Name, payload.Arguments, skillName)
		if skillName == "" {
			continue
		}

		if seen[skillName] {
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

	logs.InfoContextf(ctx, "[skill-record] records collected: count=%d", len(records))
	if len(records) > 0 {
		if err := infradb.BatchCreateMessageResources(ctx, db, records); err != nil {
			logs.WarnContextf(ctx, "recordSkillInvocations: batch create failed: %v", err)
		}
	}
}

var skillToolNames = map[string]bool{
	"skill_use": true, // native engine
	"skill":     true, // opencode engine
}

func extractSkillName(toolName string, arguments map[string]any) string {
	if !skillToolNames[toolName] {
		return ""
	}
	if name, ok := arguments["name"].(string); ok && name != "" {
		return name
	}
	return ""
}
