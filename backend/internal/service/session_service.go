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

	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
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

	exists, err := db.SessionIDExists(ctx, s.db, sessionID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errors.New("session with this session_id already exists")
	}

	session := &types.Session{
		SessionID:            sessionID,
		Type:                 req.Type,
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

	session, err := db.GetSessionBySessionID(ctx, s.db, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, errors.New("session not found")
	}

	return convertToContractSession(session), nil
}

func (s *sessionService) UpdateSession(ctx context.Context, sessionID string, req *contract.UpdateSessionRequest) (*contract.Session, error) {
	session, err := db.GetSessionBySessionID(ctx, s.db, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, errors.New("session not found")
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
	session, err := db.GetSessionBySessionID(ctx, s.db, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("session not found")
	}

	return db.DeleteSession(ctx, s.db, session.ID)
}

func (s *sessionService) ListSessions(ctx context.Context, req *contract.ListSessionsRequest) (*contract.SessionList, error) {
	caller, _ := auth.FromContext(ctx)

	var uin *uint
	var orgID *uint
	if caller != nil && caller.Uin > 0 {
		uin = &caller.Uin
		orgID = &caller.OrgID
	}

	sessions, total, err := db.ListSessions(
		ctx,
		s.db,
		req.Type,
		req.Status,
		uin,
		orgID,
		req.AssistantID,
		req.AssistantCode,
		req.Keyword,
		req.Offset,
		req.Limit,
	)
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
	session, err := db.GetSessionBySessionID(ctx, s.db, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("session not found")
	}

	if session.Status == string(types.SessionStatusEnded) {
		return errors.New("cannot activate from ended state")
	}

	return db.ActivateSession(ctx, s.db, session.ID)
}

func (s *sessionService) PauseSession(ctx context.Context, sessionID string) error {
	session, err := db.GetSessionBySessionID(ctx, s.db, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("session not found")
	}

	if session.Status == string(types.SessionStatusEnded) || session.Status == string(types.SessionStatusExpired) {
		return fmt.Errorf("cannot pause from %s state", session.Status)
	}

	return db.PauseSession(ctx, s.db, session.ID)
}

func (s *sessionService) EndSession(ctx context.Context, sessionID string) error {
	session, err := db.GetSessionBySessionID(ctx, s.db, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("session not found")
	}

	if session.Status == string(types.SessionStatusEnded) {
		return errors.New("session already ended")
	}

	return db.EndSession(ctx, s.db, session.ID)
}

func (s *sessionService) ResumeSession(ctx context.Context, sessionID string) error {
	session, err := db.GetSessionBySessionID(ctx, s.db, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("session not found")
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

	session, err := db.GetSessionBySessionID(ctx, s.db, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, errors.New("session not found")
	}

	sequence, err := db.GetNextSequence(ctx, s.db, session.SessionID)
	if err != nil {
		return nil, err
	}

	message := s.buildMessage(req, sequence)
	message.SessionID = session.SessionID

	if err := db.CreateMessage(ctx, s.db, message); err != nil {
		return nil, err
	}

	now := time.Now()
	if err := db.IncrementMessageCount(ctx, s.db, session.ID); err != nil {
		return nil, err
	}
	if err := db.UpdateLastMessageAt(ctx, s.db, session.ID, now); err != nil {
		return nil, err
	}

	if session.OrgID > 0 {
		topic, err := dm.SessionMessageRequestSubject(session.OrgID, session.SessionID)
		if err != nil {
			logs.WarnContextf(ctx, "failed to build message request subject: %v", err)
		} else {
			if err := s.eventbus.Publish(ctx, topic, message); err != nil {
				logs.WarnContextf(ctx, "failed to publish message to eventbus: %v", err)
			}
		}
	}

	if err := s.publishWorkerTask(ctx, session, message); err != nil {
		return nil, err
	}

	return convertToContractSessionMessage(message), nil
}

func (s *sessionService) buildMessage(req *contract.AddMessageRequest, sequence int64) *types.SessionMessage {
	message := &types.SessionMessage{
		SessionID:   "", // filled by caller
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
		message.Metadata = types.MessageMetadata{}
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
	recentMessages := s.buildRecentMessages(ctx, session.SessionID)

	title, err := prompts.Run(ctx, prompts.KeySessionTitle, map[string]any{
		"current_title":   session.Title,
		"recent_messages": recentMessages,
	})
	title = strings.TrimSpace(title)
	if err != nil {
		logs.WarnContextf(ctx, "LLM title generation failed, fallback: %v", err)
		if session.Title != "" && session.Title != "新会话" {
			return nil
		}
		latestMsg, _ := db.GetLatestMessage(ctx, s.db, session.SessionID)
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

func (s *sessionService) buildRecentMessages(ctx context.Context, sessionID string) string {
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
	session, err := db.GetSessionBySessionID(ctx, s.db, sessionID)
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

func (s *sessionService) publishWorkerTask(ctx context.Context, session *types.Session, message *types.SessionMessage) error {
	caller, _ := auth.FromContext(ctx)
	orgID := session.OrgID
	if orgID == 0 && caller != nil {
		orgID = caller.OrgID
	}

	if session.AssistantID == 0 && session.AllocatedAssistantID == 0 && s.inferrer != nil {
		assignedAssistantID := s.inferrer.InferAssignedAssistantID(ctx, orgID, session.Type)
		if assignedAssistantID > 0 {
			session.AllocatedAssistantID = assignedAssistantID
			if err := db.UpdateAllocatedAssistantID(ctx, s.db, session.ID, assignedAssistantID); err != nil {
				return fmt.Errorf("failed to update allocated_assistant_id: %w", err)
			}
		}
	}

	if session.AllocatedAssistantID == 0 {
		logs.DebugContextf(ctx, "Skipping task publish: no worker allocated for session %s", session.SessionID)
		return nil
	}

	topic, err := dm.WorkerTaskSubject(orgID, session.AllocatedAssistantID)
	if err != nil {
		return fmt.Errorf("failed to construct worker task topic: %w", err)
	}

	messagePayload := events.WorkerTaskMessage{
		ID:        fmt.Sprintf("msg_%d_%d", session.ID, message.Sequence),
		Type:      events.MessageTypeWorkerTask,
		CreatedAt: time.Now().UTC(),
		Trace: events.TraceContext{
			TraceID:   session.SessionID,
			RequestID: fmt.Sprintf("req_%d", message.ID),
			TaskID:    fmt.Sprintf("task_%d", message.ID),
		},
		Route: events.RouteContext{
			OrgID:     orgID,
			SessionID: session.SessionID,
			WorkerID:  session.AllocatedAssistantID,
		},
		Body: events.WorkerTaskBody{
			TaskType: events.TaskTypeAgentRun,
			Actor: events.ActorContext{
				UserID:      fmt.Sprintf("%d", session.Uin),
				DisplayName: "",
				Channel:     "session",
			},
			Input: events.TaskInput{
				Type: events.InputTypeMessage,
			},
		},
		Metadata: map[string]any{
			"session_id":   session.SessionID,
			"message_type": message.MessageType,
			"sequence":     message.Sequence,
			"timestamp":    message.Timestamp,
		},
	}

	if err := s.eventbus.Publish(ctx, topic, messagePayload); err != nil {
		logs.ErrorContextf(ctx, "Failed to publish message to assistant %d: %v", session.AllocatedAssistantID, err)
		return fmt.Errorf("failed to publish message to assistant: %w", err)
	}
	logs.DebugContextf(ctx, "Published message to topic %s: session_id=%s sequence=%d", topic, session.SessionID, message.Sequence)
	return nil
}

func (s *sessionService) GetSessionMessages(ctx context.Context, sessionID string, page, perPage int) (*contract.MessageList, error) {
	session, err := db.GetSessionBySessionID(ctx, s.db, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, errors.New("session not found")
	}

	messages, total, err := db.GetSessionMessages(ctx, s.db, session.SessionID, page, perPage)
	if err != nil {
		return nil, err
	}

	items := make([]contract.SessionMessage, 0, len(messages))
	for _, message := range messages {
		items = append(items, *convertToContractSessionMessage(message))
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

	if err := db.DeleteMessage(ctx, s.db, messageID); err != nil {
		return err
	}

	return nil
}

func (s *sessionService) ClearSessionMessages(ctx context.Context, sessionID string) error {
	session, err := db.GetSessionBySessionID(ctx, s.db, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("session not found")
	}

	if err := db.ClearSessionMessages(ctx, s.db, session.SessionID); err != nil {
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

func (s *sessionService) StreamSessionEvents(ctx context.Context, sessionID string, lastSequence int64, sink events.Sink) error {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		return errors.New("user not authenticated or org not set")
	}

	topic, err := dm.SessionResultStreamSubject(caller.OrgID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to construct session result stream topic: %w", err)
	}

	return s.eventbus.SubscribeFrom(ctx, topic, lastSequence, func(msg *nats.Msg) {
		var streamMsg events.MessageStreamMessage
		if err := json.Unmarshal(msg.Data, &streamMsg); err != nil {
			logs.WarnContextf(ctx, "failed to unmarshal to MessageStreamMessage: %v", err)
			return
		}
		// logs.DebugContextf(ctx, "received message from topic %s: session_id=%s event=%s seq=%d", topic, streamMsg.Route.SessionID, streamMsg.Body.Event, streamMsg.Body.Seq)

		if streamMsg.Body.Seq <= lastSequence {
			logs.DebugContextf(ctx, "skipping old message for session %s: seq=%d lastSequence=%d", sessionID, streamMsg.Body.Seq, lastSequence)
			return
		}

		se, ok := ProjectStreamMessage(streamMsg)
		if !ok {
			logs.WarnContextf(ctx, "unknown stream event type: %v", streamMsg.Body.Event)
			return
		}
		if err := sink.Emit(ctx, &events.Event{
			Type:    events.EventType(se.Type),
			Content: toJSONString(se),
		}); err != nil {
			logs.ErrorContextf(ctx, "failed to emit session event for session %s: %v", sessionID, err)
		}
	})
}

func convertToContractSession(session *types.Session) *contract.Session {
	result := &contract.Session{
		ID:                   session.ID,
		SessionID:            session.SessionID,
		Type:                 session.Type,
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

	if session.Metadata.Tags != nil || session.Metadata.Extra != nil || session.Metadata.UserAgent != "" || session.Metadata.IPAddress != "" {
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

func convertToContractSessionMessage(message *types.SessionMessage) *contract.SessionMessage {
	result := &contract.SessionMessage{
		ID:          fmt.Sprintf("%d", message.ID),
		SessionID:   message.SessionID,
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
			event, ok := ProjectRunEventRecord(message.SessionID, chunk)
			if !ok {
				logs.Warnf("skipping unknown or invalid session message chunk: session_id=%s message_id=%d type=%s seq=%d", message.SessionID, message.ID, chunk.Type, chunk.Seq)
				continue
			}
			result.Chunks = append(result.Chunks, *event)
		}
	}

	if message.Metadata.Model != "" || message.Metadata.Latency != 0 || message.Metadata.Extra != nil {
		result.Metadata = &message.Metadata
	}

	if hasMessageUsage(message.Usage) {
		result.Usage = &message.Usage
	}

	return result
}

func (s *sessionService) CompleteSessionMessage(ctx context.Context, req *contract.CompleteSessionMessageRequest) error {
	if req.SessionID == "" {
		return errors.New("session_id is required")
	}

	session, err := db.GetSessionBySessionID(ctx, s.db, req.SessionID)
	if err != nil {
		return fmt.Errorf("find session %s: %w", req.SessionID, err)
	}
	if session == nil {
		return fmt.Errorf("session %s not found", req.SessionID)
	}

	sequence, err := db.GetNextSequence(ctx, s.db, req.SessionID)
	if err != nil {
		return fmt.Errorf("get sequence for %s: %w", req.SessionID, err)
	}

	msgEntity := &types.SessionMessage{
		SessionID:   req.SessionID,
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

	if req.Metadata != nil {
		msgEntity.Metadata = *req.Metadata
	}
	if req.Usage != nil {
		msgEntity.Usage = *req.Usage
	}

	if err := db.CreateMessage(ctx, s.db, msgEntity); err != nil {
		return fmt.Errorf("create message for %s: %w", req.SessionID, err)
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

	session, err := db.GetSessionBySessionID(ctx, s.db, req.SessionID)
	if err != nil {
		return fmt.Errorf("find session %s: %w", req.SessionID, err)
	}
	if session == nil {
		return fmt.Errorf("session %s not found", req.SessionID)
	}

	sequence, err := db.GetNextSequence(ctx, s.db, req.SessionID)
	if err != nil {
		return fmt.Errorf("get sequence for %s: %w", req.SessionID, err)
	}

	status := req.Status
	if status == "" {
		status = string(types.MessageStatusFailed)
	}

	msgEntity := &types.SessionMessage{
		SessionID:   req.SessionID,
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

	if err := db.CreateMessage(ctx, s.db, msgEntity); err != nil {
		return fmt.Errorf("create message for %s: %w", req.SessionID, err)
	}

	now := time.Now()
	if err := db.UpdateLastMessageAt(ctx, s.db, session.ID, now); err != nil {
		logs.WarnContextf(ctx, "update last_message_at for %s: %v", req.SessionID, err)
	}

	logs.DebugContextf(ctx, "persisted failed session message: session_id=%s seq=%d", req.SessionID, sequence)
	return nil
}
