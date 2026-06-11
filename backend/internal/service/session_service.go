package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/insmtx/Leros/backend/prompts"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
)

var _ contract.SessionService = (*sessionService)(nil)

type sessionService struct {
	db       *gorm.DB
	eventbus eventbus.EventBus
	inferrer AssistantInferrer
}

func NewSessionService(db *gorm.DB, eventbus eventbus.EventBus, inferrer AssistantInferrer) contract.SessionService {
	return &sessionService{
		db:       db,
		eventbus: eventbus,
		inferrer: inferrer,
	}
}

func (s *sessionService) getSessionForCaller(ctx context.Context, sessionID string) (*types.Session, *types.Caller, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, nil, err
	}
	session, err := db.GetSessionByPublicID(ctx, s.db, sessionID)
	if err != nil {
		return nil, nil, err
	}
	if session == nil {
		return nil, nil, errors.New("session not found")
	}
	if session.OrgID != caller.OrgID {
		return nil, nil, errors.New("permission denied")
	}
	if err := verifyUserPermission(session.Uin, caller.Uin); err != nil {
		return nil, nil, err
	}
	return session, caller, nil
}

func (s *sessionService) CreateSession(ctx context.Context, req *contract.CreateSessionRequest) (*contract.Session, error) {
	if req.Type == "" {
		return nil, errors.New("type is required")
	}

	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 || caller.OrgID == 0 {
		return nil, errors.New("user not authenticated or org not set")
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_%s", snowflake.GenerateIDBase58())
	}

	exists, err := db.PublicIDExists(ctx, s.db, sessionID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errors.New("session with this public_id already exists")
	}

	session := &types.Session{
		PublicID:             sessionID,
		Type:                 types.SessionType(req.Type),
		Uin:                  caller.Uin,
		OrgID:                caller.OrgID,
		AssistantID:          req.AssistantID,
		AllocatedAssistantID: req.AssistantID,
		Status:               string(types.SessionStatusActive),
		Title:                req.Title,
		MessageCount:         0,
		ExpiredAt:            req.ExpiredAt,
	}

	if req.Metadata != nil {
		session.Metadata = *req.Metadata
	}

	if err := db.CreateSession(ctx, s.db, session); err != nil {
		return nil, err
	}

	return convertToContractSession(session), nil
}

func (s *sessionService) GetSession(ctx context.Context, sessionID string) (*contract.Session, error) {
	if sessionID == "" {
		return nil, errors.New("session_id is required")
	}

	session, _, err := s.getSessionForCaller(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return convertToContractSession(session), nil
}

func (s *sessionService) UpdateSession(ctx context.Context, sessionID string, req *contract.UpdateSessionRequest) (*contract.Session, error) {
	session, _, err := s.getSessionForCaller(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if req.Title != "" {
		session.TitleManuallySet = true
		session.Title = req.Title
	}
	if req.Metadata != nil {
		session.Metadata = *req.Metadata
	}
	if req.ExpiredAt != nil {
		session.ExpiredAt = req.ExpiredAt
	}

	session.UpdatedAt = time.Now()

	if err := db.UpdateSession(ctx, s.db, session); err != nil {
		return nil, err
	}

	return convertToContractSession(session), nil
}

func (s *sessionService) DeleteSession(ctx context.Context, sessionID string) error {
	session, _, err := s.getSessionForCaller(ctx, sessionID)
	if err != nil {
		return err
	}

	return db.DeleteSession(ctx, s.db, session.ID)
}

func (s *sessionService) ListSessions(ctx context.Context, req *contract.ListSessionsRequest) (*contract.SessionList, error) {
	caller, _ := auth.FromContext(ctx)

	var pqCaller types.Caller
	if caller != nil {
		pqCaller = *caller
	}

	sessionType := (*types.SessionType)(req.Type)
	opt := types.NewPageQuery(pqCaller, req.Offset, req.Limit)
	if sessionType != nil && *sessionType != "" {
		opt.AddExactFilter("type", string(*sessionType))
	}
	if req.Status != nil && *req.Status != "" {
		opt.AddFilter("status", *req.Status)
	}
	if req.AssistantID != nil && *req.AssistantID > 0 {
		opt.AddFilter("assistant_id", fmt.Sprintf("%d", *req.AssistantID))
	}
	if req.AssistantCode != nil && *req.AssistantCode != "" {
		opt.AddFilter("assistant_code", *req.AssistantCode)
	}
	if req.Keyword != nil && *req.Keyword != "" {
		opt.AddFilter("keyword", *req.Keyword)
	}

	sessions, total, err := db.ListSessions(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}

	items := make([]contract.Session, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, *convertToContractSession(session))
	}

	return &contract.SessionList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func (s *sessionService) ActivateSession(ctx context.Context, sessionID string) error {
	session, _, err := s.getSessionForCaller(ctx, sessionID)
	if err != nil {
		return err
	}

	if session.Status == string(types.SessionStatusEnded) {
		return errors.New("cannot activate from ended state")
	}

	return db.ActivateSession(ctx, s.db, session.ID)
}

func (s *sessionService) PauseSession(ctx context.Context, sessionID string) error {
	session, _, err := s.getSessionForCaller(ctx, sessionID)
	if err != nil {
		return err
	}

	if session.Status == string(types.SessionStatusEnded) || session.Status == string(types.SessionStatusExpired) {
		return fmt.Errorf("cannot pause from %s state", session.Status)
	}

	return db.PauseSession(ctx, s.db, session.ID)
}

func (s *sessionService) EndSession(ctx context.Context, sessionID string) error {
	session, _, err := s.getSessionForCaller(ctx, sessionID)
	if err != nil {
		return err
	}

	if session.Status == string(types.SessionStatusEnded) {
		return errors.New("session already ended")
	}

	return db.EndSession(ctx, s.db, session.ID)
}

func (s *sessionService) ResumeSession(ctx context.Context, sessionID string) error {
	session, _, err := s.getSessionForCaller(ctx, sessionID)
	if err != nil {
		return err
	}

	if session.Status != string(types.SessionStatusPaused) {
		return errors.New("can only resume from paused state")
	}

	return db.ResumeSession(ctx, s.db, session.ID)
}

func (s *sessionService) AddMessage(ctx context.Context, sessionID string, req *contract.AddMessageRequest) (*contract.SessionMessage, error) {
	if req.Role == "" {
		return nil, errors.New("role is required")
	}
	if req.Content == "" {
		return nil, errors.New("content is required")
	}

	session, _, err := s.getSessionForCaller(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	mp := NewMessagePoster(s.db, s.eventbus, s.inferrer)
	message, err := mp.PostMessage(ctx, session, func(sequence int64) *types.SessionMessage {
		return s.buildMessage(req, sequence)
	})
	if err != nil {
		return nil, err
	}

	return convertToContractSessionMessage(message, session.PublicID), nil
}

func (s *sessionService) buildMessage(req *contract.AddMessageRequest, sequence int64) *types.SessionMessage {
	message := &types.SessionMessage{
		SessionID:   0, // filled by caller
		Role:        req.Role,
		Content:     req.Content,
		MessageType: req.MessageType,
		Status:      string(types.MessageStatusPending),
		Sequence:    sequence,
		Timestamp:   time.Now().UnixMilli(),
	}

	if req.Chunks != nil && len(req.Chunks) > 0 {
		message.Chunks = req.Chunks
	}

	if req.Metadata != nil {
		message.Metadata = *req.Metadata
	} else {
		message.Metadata = types.ObjectMetadata{}
	}
	if req.Usage != nil {
		message.Usage = *req.Usage
	}

	if message.MessageType == "" {
		message.MessageType = string(types.MessageTypeText)
	}

	return message
}

func (s *sessionService) tryAutoUpdateTitle(ctx context.Context, session *types.Session) {
	if session.TitleManuallySet {
		return
	}
	if session.MessageCount >= 3 {
		return
	}

	if err := s.renameSession(ctx, session); err != nil {
		logs.WarnContextf(ctx, "failed to auto-update session title: %v", err)
	}
}

func (s *sessionService) renameSession(ctx context.Context, session *types.Session) error {
	recentMessages := s.buildRecentMessages(ctx, session.ID)

	title, err := prompts.Run(ctx, prompts.KeySessionTitle, map[string]any{
		"current_title":   session.Title,
		"recent_messages": recentMessages,
	})
	title = strings.TrimSpace(title)
	if err != nil {
		logs.WarnContextf(ctx, "LLM title generation failed, fallback: %v", err)
		if session.Title != "" && session.Title != "New Session" {
			return nil
		}
		latestMsg, _ := db.GetLatestMessage(ctx, s.db, session.ID)
		if latestMsg != nil {
			runes := []rune(latestMsg.Content)
			if len(runes) > 100 {
				title = string(runes[:100])
			} else {
				title = latestMsg.Content
			}
		}
		if title == "" {
			return nil
		}
	} else if title == "KEEP" {
		return nil
	}
	logs.InfoContextf(ctx, "auto-updating session title to: %s, old title: %s", title, session.Title)
	session.Title = title
	session.UpdatedAt = time.Now()
	return db.UpdateSession(ctx, s.db, session)
}

func (s *sessionService) buildRecentMessages(ctx context.Context, sessionID uint) string {
	const maxMessages = 10
	messages, err := db.GetRecentSessionMessages(ctx, s.db, sessionID, maxMessages)
	if err != nil || len(messages) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("%s: %s\n", msg.Role, msg.Content))
	}
	return sb.String()
}

func (s *sessionService) HandleSessionTitleRequest(ctx context.Context, sessionID string) error {
	session, err := db.GetSessionByPublicID(ctx, s.db, sessionID)
	if err != nil {
		return fmt.Errorf("get session %s: %w", sessionID, err)
	}
	if session == nil {
		return nil
	}

	logs.DebugContextf(ctx, "handling session title request for session %s", sessionID)
	s.tryAutoUpdateTitle(ctx, session)
	return nil
}

func (s *sessionService) SubmitApproval(ctx context.Context, req *contract.SubmitApprovalRequest) error {
	session, caller, err := s.getSessionForCaller(ctx, req.SessionID)
	if err != nil {
		return err
	}
	req.OrgID = caller.OrgID
	if req.WorkerID == 0 {
		req.WorkerID = session.AllocatedAssistantID
	}
	if req.WorkerID == 0 {
		req.WorkerID = 1
	}
	topic, err := dm.WorkerApprovalSubject(req.OrgID, req.WorkerID)
	if err != nil {
		return fmt.Errorf("build approval topic: %w", err)
	}
	return s.eventbus.Publish(ctx, topic, req)
}
func (s *sessionService) GetSessionMessages(ctx context.Context, sessionID string, page, perPage int) (*contract.MessageList, error) {
	session, _, err := s.getSessionForCaller(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	messages, total, err := db.GetSessionMessages(ctx, s.db, session.ID, page, perPage)
	if err != nil {
		return nil, err
	}

	items := make([]contract.SessionMessage, 0, len(messages))
	for _, message := range messages {
		items = append(items, *convertToContractSessionMessage(message, session.PublicID))
	}

	return &contract.MessageList{
		Total: total,
		Page:  page,
		Items: items,
	}, nil
}

func (s *sessionService) DeleteMessage(ctx context.Context, messageID uint) error {
	message, err := db.GetMessageByID(ctx, s.db, messageID)
	if err != nil {
		return err
	}
	if message == nil {
		return errors.New("message not found")
	}
	session, err := db.GetSessionByID(ctx, s.db, message.SessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("session not found")
	}
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return err
	}
	if session.OrgID != caller.OrgID {
		return errors.New("permission denied")
	}
	if err := verifyUserPermission(session.Uin, caller.Uin); err != nil {
		return err
	}

	if err := db.DeleteMessage(ctx, s.db, messageID); err != nil {
		return err
	}

	return nil
}

func (s *sessionService) ClearSessionMessages(ctx context.Context, sessionID string) error {
	session, _, err := s.getSessionForCaller(ctx, sessionID)
	if err != nil {
		return err
	}

	if err := db.ClearSessionMessages(ctx, s.db, session.ID); err != nil {
		return err
	}

	session.MessageCount = 0
	session.LastMessageAt = nil
	session.UpdatedAt = time.Now()

	return db.UpdateSession(ctx, s.db, session)
}

func toJSONString(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *sessionService) StreamSessionEvents(ctx context.Context, sessionPID string, lastSequence int64, sink events.Sink) error {
	_, caller, err := s.getSessionForCaller(ctx, sessionPID)
	if err != nil {
		return err
	}

	topic, err := dm.SessionResultStreamSubject(caller.OrgID, sessionPID)
	if err != nil {
		return fmt.Errorf("failed to construct session result stream topic: %w", err)
	}

	return s.eventbus.SubscribeFrom(ctx, topic, lastSequence, func(msg *nats.Msg) {
		var streamMsg protocol.MessageStreamMessage
		if err := json.Unmarshal(msg.Data, &streamMsg); err != nil {
			logs.WarnContextf(ctx, "failed to unmarshal to MessageStreamMessage: %v", err)
			return
		}
		// logs.DebugContextf(ctx, "received message from topic %s: session_id=%s event=%s seq=%d", topic, streamMsg.Route.SessionID, streamMsg.Body.Event, streamMsg.Body.Seq)

		if streamMsg.Body.Seq <= lastSequence {
			logs.DebugContextf(ctx, "skipping old message for session %s: seq=%d lastSequence=%d", sessionPID, streamMsg.Body.Seq, lastSequence)
			return
		}

		se, ok := ProjectStreamMessage(streamMsg)
		if !ok {
			logs.WarnContextf(ctx, "unknown stream event type: %v", streamMsg.Body.Event)
			return
		}
		if err := sink.Emit(ctx, &events.Event{
			Type:    se.Type,
			Content: toJSONString(se),
		}); err != nil {
			logs.ErrorContextf(ctx, "failed to emit session event for session %s: %v", sessionPID, err)
		}
	})
}

func convertToContractSession(session *types.Session) *contract.Session {
	result := &contract.Session{
		SessionID:            session.PublicID,
		Type:                 string(session.Type),
		Uin:                  session.Uin,
		OrgID:                session.OrgID,
		AssistantID:          session.AssistantID,
		AllocatedAssistantID: session.AllocatedAssistantID,
		Status:               session.Status,
		Title:                session.Title,
		TitleManuallySet:     session.TitleManuallySet,
		MessageCount:         session.MessageCount,
		CreatedAt:            session.CreatedAt,
		UpdatedAt:            session.UpdatedAt,
	}

	if session.Metadata.Tags != nil || session.Metadata.Extra != nil {
		result.Metadata = &session.Metadata
	}
	if session.LastMessageAt != nil {
		result.LastMessageAt = session.LastMessageAt
	}
	if session.ExpiredAt != nil {
		result.ExpiredAt = session.ExpiredAt
	}

	return result
}

func hasMessageUsage(usage types.MessageUsage) bool {
	return usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.TotalTokens != 0
}

func convertToContractSessionMessage(message *types.SessionMessage, publicID string) *contract.SessionMessage {
	result := &contract.SessionMessage{
		ID:          fmt.Sprintf("%d", message.ID),
		SessionID:   publicID,
		Role:        message.Role,
		Content:     message.Content,
		MessageType: message.MessageType,
		Timestamp:   message.Timestamp,
		Sequence:    message.Sequence,
		CreatedAt:   message.CreatedAt,
	}

	if message.Chunks != nil && len(message.Chunks) > 0 {
		result.Chunks = make([]contract.SessionEvent, 0, len(message.Chunks))
		for _, chunk := range message.Chunks {
			if isHiddenSessionHistoryChunk(chunk.Type) {
				continue
			}
			event, ok := ProjectRunEventRecord(publicID, chunk)
			if !ok {
				logs.Warnf("skipping unknown or invalid session message chunk: public_id=%s message_id=%d type=%s seq=%d", publicID, message.ID, chunk.Type, chunk.Seq)
				continue
			}
			result.Chunks = append(result.Chunks, *event)
		}
	}
	if len(message.Artifacts) > 0 {
		result.Artifacts = append([]types.MessageArtifact{}, message.Artifacts...)
	}

	if message.Metadata.Extra != nil {
		result.Metadata = &message.Metadata
	}

	if hasMessageUsage(message.Usage) {
		result.Usage = &message.Usage
	}

	return result
}

func isHiddenSessionHistoryChunk(eventType string) bool {
	switch events.EventType(eventType) {
	case events.EventTodoSnapshot, events.EventTodoUpdated:
		return true
	default:
		return false
	}
}

func (s *sessionService) CompleteSessionMessage(ctx context.Context, req *contract.CompleteSessionMessageRequest) error {
	if req.SessionID == "" {
		return errors.New("session_id is required")
	}

	session, err := db.GetSessionByPublicID(ctx, s.db, req.SessionID)
	if err != nil {
		return fmt.Errorf("find session %s: %w", req.SessionID, err)
	}
	if session == nil {
		return fmt.Errorf("session %s not found", req.SessionID)
	}

	sequence, err := db.GetNextSequence(ctx, s.db, session.ID)
	if err != nil {
		return fmt.Errorf("get sequence for %s: %w", req.SessionID, err)
	}

	msgEntity := &types.SessionMessage{
		SessionID:   session.ID,
		Role:        string(types.MessageRoleAssistant),
		Content:     req.Content,
		MessageType: string(types.MessageTypeText),
		Status:      string(types.MessageStatusCompleted),
		Sequence:    sequence,
		Timestamp:   req.CreatedAt.UnixMilli(),
	}

	if req.Chunks != nil && len(req.Chunks) > 0 {
		msgEntity.Chunks = req.Chunks
	}
	if len(req.Artifacts) > 0 {
		msgEntity.Artifacts = req.Artifacts
	}

	if req.Metadata != nil {
		msgEntity.Metadata = *req.Metadata
	}
	if req.Usage != nil {
		msgEntity.Usage = *req.Usage
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := db.CreateMessage(ctx, tx, msgEntity); err != nil {
			return fmt.Errorf("create message for %s: %w", req.SessionID, err)
		}
		// 不再绑定 artifact 与 message 的关联关系，artifact 通过 session_id 关联查询
		// bindDeclaredArtifacts(ctx, tx, req.Artifacts, session, msgEntity)
		return nil
	}); err != nil {
		return err
	}

	now := time.Now()
	if err := db.UpdateLastMessageAt(ctx, s.db, session.ID, now); err != nil {
		logs.WarnContextf(ctx, "update last_message_at for %s: %v", req.SessionID, err)
	}

	logs.DebugContextf(ctx, "persisted completed session message: session_id=%s seq=%d", req.SessionID, sequence)
	return nil
}

func (s *sessionService) FailedSessionMessage(ctx context.Context, req *contract.FailedSessionMessageRequest) error {
	if req.SessionID == "" {
		return errors.New("session_id is required")
	}

	session, err := db.GetSessionByPublicID(ctx, s.db, req.SessionID)
	if err != nil {
		return fmt.Errorf("find session %s: %w", req.SessionID, err)
	}
	if session == nil {
		return fmt.Errorf("session %s not found", req.SessionID)
	}

	sequence, err := db.GetNextSequence(ctx, s.db, session.ID)
	if err != nil {
		return fmt.Errorf("get sequence for %s: %w", req.SessionID, err)
	}

	status := req.Status
	if status == "" {
		status = string(types.MessageStatusFailed)
	}

	msgEntity := &types.SessionMessage{
		SessionID:   session.ID,
		Role:        string(types.MessageRoleAssistant),
		Content:     req.ErrorMsg,
		MessageType: string(types.MessageTypeText),
		Status:      status,
		Sequence:    sequence,
		Timestamp:   req.CreatedAt.UnixMilli(),
	}
	if req.Chunks != nil && len(req.Chunks) > 0 {
		msgEntity.Chunks = req.Chunks
	}
	if len(req.Artifacts) > 0 {
		msgEntity.Artifacts = req.Artifacts
	}
	if req.Metadata != nil {
		msgEntity.Metadata = *req.Metadata
	}
	if req.Usage != nil {
		msgEntity.Usage = *req.Usage
	}
	if req.ErrorCode != "" {
		if msgEntity.Metadata.Extra == nil {
			msgEntity.Metadata.Extra = map[string]interface{}{}
		}
		msgEntity.Metadata.Extra["error_code"] = req.ErrorCode
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := db.CreateMessage(ctx, tx, msgEntity); err != nil {
			return fmt.Errorf("create message for %s: %w", req.SessionID, err)
		}
		// 不再绑定 artifact 与 message 的关联关系，artifact 通过 session_id 关联查询
		// bindDeclaredArtifacts(ctx, tx, req.Artifacts, session, msgEntity)
		return nil
	}); err != nil {
		return err
	}

	now := time.Now()
	if err := db.UpdateLastMessageAt(ctx, s.db, session.ID, now); err != nil {
		logs.WarnContextf(ctx, "update last_message_at for %s: %v", req.SessionID, err)
	}

	logs.DebugContextf(ctx, "persisted failed session message: session_id=%s seq=%d", req.SessionID, sequence)
	return nil
}
