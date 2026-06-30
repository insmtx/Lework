package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"golang.org/x/sync/errgroup"

	"gorm.io/gorm"

	"code.gitea.io/sdk/gitea"
	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/prompts"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
)

var _ contract.SessionService = (*sessionService)(nil)

const (
	sessionRuntimeStatusIdle       = "idle"
	sessionRuntimeStatusResponding = "responding"
	responseStreamStartSeqKey      = "response_stream_start_seq"
	stateStartSeqKey               = "state_start_seq"
	replyToMessageIDsKey           = "reply_to_message_ids"
	sessionProcessingWindow        = 30 * time.Minute
	workTitleMaxRunes              = 50
)

// ErrNoReplyMessageIDs is returned when a run-started stream event lacks
// identifiable user messages to target.
var ErrNoReplyMessageIDs = errors.New("no reply message ids in stream event")

type sessionService struct {
	db          *gorm.DB
	eventbus    eventbus.EventBus
	inferrer    AssistantInferrer
	giteaClient *gitea.Client
	giteaCfg    *config.GiteaConfig
	env         string
}

func NewSessionService(db *gorm.DB, eventbus eventbus.EventBus, inferrer AssistantInferrer, giteaClient *gitea.Client, giteaCfg *config.GiteaConfig, env string) contract.SessionService {
	return &sessionService{
		db:          db,
		eventbus:    eventbus,
		inferrer:    inferrer,
		giteaClient: giteaClient,
		giteaCfg:    giteaCfg,
		env:         env,
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

func (s *sessionService) getSessionMessagesForCaller(ctx context.Context, sessionID string) (*types.Session, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	session, err := db.GetSessionByPublicID(ctx, s.db, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, errors.New("session not found")
	}
	if session.OrgID != caller.OrgID {
		return nil, errors.New("permission denied")
	}
	if caller.Kind == types.CallerKindWorker {
		if caller.WorkerID == 0 || session.AllocatedAssistantID != caller.WorkerID {
			return nil, errors.New("permission denied")
		}
		return session, nil
	}
	if err := verifyUserPermission(session.Uin, caller.Uin); err != nil {
		return nil, err
	}
	return session, nil
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

	assistantID, workerID, err := s.resolveRuntimeWorker(ctx, caller.OrgID, req.AssistantID)
	if err != nil {
		return nil, err
	}
	session := &types.Session{
		PublicID:             sessionID,
		Type:                 types.SessionType(req.Type),
		Uin:                  caller.Uin,
		OrgID:                caller.OrgID,
		AssistantID:          assistantID,
		AllocatedAssistantID: workerID,
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

func (s *sessionService) resolveRuntimeWorker(ctx context.Context, orgID, assistantID uint) (uint, uint, error) {
	if s == nil {
		return assistantID, assistantID, nil
	}
	return resolveRuntimeWorker(ctx, s.db, orgID, assistantID, s.inferrer)
}

func (s *sessionService) GetSession(ctx context.Context, sessionID string) (*contract.Session, error) {
	if sessionID == "" {
		return nil, errors.New("session_id is required")
	}

	session, _, err := s.getSessionForCaller(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	result := convertToContractSession(session)
	result.RuntimeStatus = s.sessionRuntimeStatus(ctx, session.ID)
	return result, nil
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

	resolveAttachmentURLs(ctx, s.db, session.OrgID, req.Attachments)

	if session.ProjectID != nil && *session.ProjectID != 0 && len(req.Attachments) > 0 {
		project, err := db.GetProjectByID(ctx, s.db, *session.ProjectID)
		if err != nil {
			logs.WarnContextf(ctx, "addMessage get project %d: %v", *session.ProjectID, err)
		} else if project != nil {
			attachFilesToProject(ctx, s.db, session.OrgID, session.Uin, session.TaskID, project, req.Attachments)
		}
	}

	mp := NewMessagePoster(s.db, s.eventbus, s.inferrer, s.giteaClient, s.giteaCfg, s.env, s)
	message, err := mp.PostMessage(ctx, session, req.ExecutionMode, func(sequence int64) *types.SessionMessage {
		return s.buildMessage(req, sequence)
	})
	if err != nil {
		return nil, err
	}

	if session.ProjectID != nil && *session.ProjectID != 0 && req.Role == string(types.MessageRoleUser) {
		// 中文注释：只在用户主动发言时刷新项目活跃时间，避免助手流式输出把项目顺序不断顶来顶去。
		if err := db.TouchProjectUpdatedAt(ctx, s.db, *session.ProjectID, time.Now()); err != nil {
			logs.WarnContextf(ctx, "touch project updated_at after add message %s: %v", session.PublicID, err)
		}
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

	if req.Attachments != nil && len(req.Attachments) > 0 {
		message.Attachments = req.Attachments
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
	s.tryAutoUpdateWorkTitle(ctx, session)
	return nil
}

func (s *sessionService) tryAutoUpdateWorkTitle(ctx context.Context, session *types.Session) {
	if session == nil || session.Type != types.SessionTypeTask {
		return
	}
	if session.ProjectID == nil || session.TaskID == nil {
		return
	}
	if session.MessageCount >= 3 {
		return
	}

	project, err := db.GetProjectByID(ctx, s.db, *session.ProjectID)
	if err != nil || project == nil {
		return
	}
	var task types.Task
	if err := s.db.WithContext(ctx).First(&task, *session.TaskID).Error; err != nil {
		return
	}

	firstMsg, err := s.firstUserMessage(ctx, session.ID)
	if err != nil || firstMsg == nil {
		return
	}
	fallbackTitle := fallbackWorkTitle(firstMsg.Content)
	if project.Name != fallbackTitle && task.Title != fallbackTitle {
		return
	}

	title, err := prompts.Run(ctx, prompts.KeyWorkTitle, map[string]any{
		"user_message": firstMsg.Content,
	})
	if err != nil {
		logs.WarnContextf(ctx, "work title: LLM generation failed for session %s: %v", session.PublicID, err)
		return
	}
	title = sanitizeGeneratedWorkTitle(title)
	if title == "" {
		return
	}

	projectUpdated := false
	if project.Name == fallbackTitle {
		project.Name = title
		project.UpdatedAt = time.Now()
		if err := db.UpdateProject(ctx, s.db, project); err != nil {
			logs.WarnContextf(ctx, "work title: update project %s: %v", project.PublicID, err)
		} else {
			projectUpdated = true
		}
	}
	taskUpdated := false
	if task.Title == fallbackTitle {
		task.Title = title
		task.UpdatedAt = time.Now()
		if err := db.UpdateTask(ctx, s.db, &task); err != nil {
			logs.WarnContextf(ctx, "work title: update task %s: %v", task.PublicID, err)
		} else {
			taskUpdated = true
		}
	}
	if !projectUpdated && !taskUpdated {
		return
	}

	if session.Title == fallbackTitle {
		session.Title = title
		session.UpdatedAt = time.Now()
		if err := db.UpdateSession(ctx, s.db, session); err != nil {
			logs.WarnContextf(ctx, "work title: update session %s: %v", session.PublicID, err)
		}
	}

	if err := s.publishWorkTitleUpdated(ctx, session, project, &task); err != nil {
		logs.WarnContextf(ctx, "work title: publish stream event for session %s: %v", session.PublicID, err)
	}
}

func (s *sessionService) firstUserMessage(ctx context.Context, sessionID uint) (*types.SessionMessage, error) {
	var message types.SessionMessage
	err := s.db.WithContext(ctx).
		Where("session_id = ? AND role = ?", sessionID, string(types.MessageRoleUser)).
		Order("sequence ASC").
		First(&message).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &message, nil
}

func fallbackWorkTitle(content string) string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) > workTitleMaxRunes {
		return string(runes[:workTitleMaxRunes])
	}
	return string(runes)
}

func sanitizeGeneratedWorkTitle(title string) string {
	title = strings.TrimSpace(title)
	title = strings.Trim(title, "\"'`“”‘’「」『』")
	title = strings.TrimSpace(title)
	runes := []rune(title)
	if len(runes) > workTitleMaxRunes {
		return string(runes[:workTitleMaxRunes])
	}
	return title
}

func (s *sessionService) publishWorkTitleUpdated(
	ctx context.Context,
	session *types.Session,
	project *types.Project,
	task *types.Task,
) error {
	if s == nil || s.eventbus == nil || session == nil || project == nil || task == nil {
		return nil
	}
	if session.OrgID == 0 || session.PublicID == "" {
		return nil
	}

	workTitle := messaging.WorkTitleUpdatedPayload{
		ProjectID:    project.PublicID,
		ProjectName:  project.Name,
		TaskID:       task.PublicID,
		TaskTitle:    task.Title,
		SessionID:    session.PublicID,
		SessionTitle: session.Title,
	}
	topic, err := messaging.RunEventSubject(session.OrgID, session.PublicID, messaging.RunEventLaneState)
	if err != nil {
		return err
	}

	msg := messaging.RunEvent{
		ID:        fmt.Sprintf("work-title:%s:%d", session.PublicID, time.Now().UnixMilli()),
		Type:      messaging.MessageTypeRunEvent,
		CreatedAt: time.Now().UTC(),
		Route: messaging.RouteContext{
			OrgID:     session.OrgID,
			SessionID: session.PublicID,
		},
		Body: messaging.RunEventBody{
			Seq:   time.Now().UnixMilli(),
			Event: messaging.RunEventWorkTitleUpdated,
			Payload: messaging.RunEventPayload{
				WorkTitle: &workTitle,
			},
		},
	}
	return s.eventbus.Publish(ctx, topic, msg)
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
		_, workerID, err := resolveDefaultRuntimeWorker(ctx, s.db, caller.OrgID, s.inferrer)
		if err != nil {
			return err
		}
		req.WorkerID = workerID
	}
	topic, err := messaging.WorkerCommandSubject(req.OrgID, req.WorkerID, messaging.LaneInteraction)
	if err != nil {
		return fmt.Errorf("build approval topic: %w", err)
	}

	cmd := messaging.NewApprovalResolveCommand(
		fmt.Sprintf("approval_%s", snowflake.GenerateIDBase58()),
		messaging.RouteContext{
			OrgID:     req.OrgID,
			WorkerID:  req.WorkerID,
			SessionID: req.SessionID,
		},
		messaging.ApprovalResolveCommandPayload{
			Action: req.Action,
			Reason: req.Reason,
		},
		req.RequestID,
	)
	return s.eventbus.Publish(ctx, topic, cmd)
}

func (s *sessionService) SubmitQuestionAnswer(ctx context.Context, req *contract.SubmitQuestionAnswerRequest) error {
	session, caller, err := s.getSessionForCaller(ctx, req.SessionID)
	if err != nil {
		return err
	}
	req.OrgID = caller.OrgID
	if req.WorkerID == 0 {
		req.WorkerID = session.AllocatedAssistantID
	}
	if req.WorkerID == 0 {
		_, workerID, err := resolveDefaultRuntimeWorker(ctx, s.db, caller.OrgID, s.inferrer)
		if err != nil {
			return err
		}
		req.WorkerID = workerID
	}
	topic, err := messaging.WorkerCommandSubject(req.OrgID, req.WorkerID, messaging.LaneInteraction)
	if err != nil {
		return fmt.Errorf("build question answer topic: %w", err)
	}

	cmd := messaging.NewQuestionAnswerCommand(
		fmt.Sprintf("question_%s", snowflake.GenerateIDBase58()),
		messaging.RouteContext{
			OrgID:     req.OrgID,
			WorkerID:  req.WorkerID,
			SessionID: req.SessionID,
		},
		messaging.QuestionAnswerCommandPayload{
			Answers: req.Answers,
		},
		req.RequestID,
	)
	return s.eventbus.Publish(ctx, topic, cmd)
}

func (s *sessionService) sessionRuntimeStatus(ctx context.Context, sessionID uint) string {
	messages, err := db.GetRecentProcessingUserMessages(ctx, s.db, sessionID, time.Now().Add(-sessionProcessingWindow))
	if err != nil {
		logs.WarnContextf(ctx, "get session runtime status failed: session=%d error=%v", sessionID, err)
		return sessionRuntimeStatusIdle
	}
	if len(messages) > 0 {
		return sessionRuntimeStatusResponding
	}
	return sessionRuntimeStatusIdle
}

func (s *sessionService) HandleSessionRunStarted(ctx context.Context, req *contract.SessionRunStartedRequest) error {
	if req == nil {
		return errors.New("request is required")
	}
	if req.SessionID == "" {
		return errors.New("session_id is required")
	}
	// StreamStartSeq is optional; stream projector sets it asynchronously
	// when the first run.stream event arrives.
	if req.StateStartSeq == 0 {
		return errors.New("state_start_seq is required")
	}

	session, err := db.GetSessionByPublicID(ctx, s.db, req.SessionID)
	if err != nil {
		return fmt.Errorf("find session %s: %w", req.SessionID, err)
	}
	if session == nil {
		return fmt.Errorf("session %s not found", req.SessionID)
	}

	messageIDs := replyMessageIDs(req.ReplyToMessageIDs, req.RequestID)
	if len(messageIDs) == 0 {
		logs.WarnContextf(ctx, "run started without reply message ids: session_id=%s request_id=%s", req.SessionID, req.RequestID)
		return ErrNoReplyMessageIDs
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		messages, err := db.GetSessionMessagesByIDs(ctx, tx, session.ID, messageIDs)
		if err != nil {
			return err
		}
		for _, message := range messages {
			if message.Role != string(types.MessageRoleUser) || message.Status != string(types.MessageStatusPending) {
				continue
			}
			message.Status = string(types.MessageStatusProcessing)
			if req.StreamStartSeq > 0 {
				setResponseStreamStartSeq(&message.Metadata, req.StreamStartSeq)
			}
			if req.StateStartSeq > 0 {
				setStateStartSeq(&message.Metadata, req.StateStartSeq)
			}
			if err := tx.Save(message).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// SetSessionStreamStartSeq records the NATS stream sequence for the first
// run.stream event of a session, used by the stream projector for SSE replay.
func (s *sessionService) SetSessionStreamStartSeq(ctx context.Context, sessionID string, streamSeq uint64) error {
	session, err := db.GetSessionByPublicID(ctx, s.db, sessionID)
	if err != nil {
		return fmt.Errorf("find session %s: %w", sessionID, err)
	}
	if session == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	messages, err := db.GetRecentProcessingUserMessages(ctx, s.db, session.ID, time.Now().Add(-sessionProcessingWindow))
	if err != nil {
		return fmt.Errorf("get processing messages for session %d: %w", session.ID, err)
	}
	if len(messages) == 0 {
		logs.DebugContextf(ctx, "SetSessionStreamStartSeq: no processing messages for session %s, skipping", sessionID)
		return nil
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids := make([]uint, len(messages))
		for i, m := range messages {
			ids[i] = m.ID
		}
		dbMsgs, err := db.GetSessionMessagesByIDs(ctx, tx, session.ID, ids)
		if err != nil {
			return err
		}
		for _, message := range dbMsgs {
			if message.Status != string(types.MessageStatusProcessing) {
				continue
			}
			// Only set if not already present (idempotent).
			if _, ok := responseStreamStartSeq(message.Metadata); ok {
				continue
			}
			setResponseStreamStartSeq(&message.Metadata, streamSeq)
			if err := tx.Save(message).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *sessionService) GetSessionMessages(ctx context.Context, sessionID string, page, perPage int) (*contract.MessageList, error) {
	session, err := s.getSessionMessagesForCaller(ctx, sessionID)
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

func (s *sessionService) updateReplyMessageStatus(ctx context.Context, tx *gorm.DB, sessionID uint, rawIDs []string, status string) error {
	messageIDs := replyMessageIDs(rawIDs, "")
	if len(messageIDs) == 0 {
		return nil
	}
	messages, err := db.GetSessionMessagesByIDs(ctx, tx, sessionID, messageIDs)
	if err != nil {
		return err
	}
	for _, message := range messages {
		if message.Role != string(types.MessageRoleUser) {
			continue
		}
		message.Status = status
		if err := tx.Save(message).Error; err != nil {
			return err
		}
	}
	return nil
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

// StreamSessionEvents 同时订阅 run.stream 和 run.state lane，按 run 内 Seq 去重后推送 SSE 事件。
//
//   - run.stream: delta, tool_call, todo 等高频事件
//   - run.state:  terminal, approval, question 等关键状态事件
//
// 两个 lane 的事件互不重复，Seq 去重仅作保底。
func (s *sessionService) StreamSessionEvents(ctx context.Context, sessionPID string, replay bool, sink events.Sink) error {
	session, caller, err := s.getSessionForCaller(ctx, sessionPID)
	if err != nil {
		return err
	}

	replayState := sessionReplayState{}
	streamStartSeq := int64(0)
	stateStartSeq := int64(0)
	if replay {
		replayState, err = s.getSessionReplayState(ctx, session.ID)
		if err != nil {
			return err
		}
		if replayState.StateStartSeq > 0 && replayState.StateStartSeq <= math.MaxInt64 {
			stateStartSeq = int64(replayState.StateStartSeq)
		}
		// 两条 lane 在同一个 NATS stream 中，共享全局 Sequence.Stream 序号。
		// state_start_seq（run.started 事件到达时记录）必然早于所有 run.stream 事件，
		// 因此两条 lane 都使用 stateStartSeq 即可覆盖所有需要回放的内容。
		if stateStartSeq > 0 {
			streamStartSeq = stateStartSeq
		}
	}

	streamTopic, err := messaging.RunEventSubject(caller.OrgID, sessionPID, messaging.RunEventLaneStream)
	if err != nil {
		return fmt.Errorf("failed to construct run stream topic: %w", err)
	}
	stateTopic, err := messaging.RunEventSubject(caller.OrgID, sessionPID, messaging.RunEventLaneState)
	if err != nil {
		return fmt.Errorf("failed to construct run state topic: %w", err)
	}

	// Dedup by run-level Seq across lanes (belt-and-suspenders — events on
	// different lanes should not overlap, but we dedup anyway).
	dedup := &runEventDedup{}

	// innerCtx is cancelled by the first terminal event (completed/failed/cancelled)
	// so the server actively closes the SSE stream instead of waiting for the client.
	innerCtx, innerCancel := context.WithCancel(ctx)
	defer innerCancel()

	emitEvent := func(runEvent messaging.RunEvent) {
		if runEvent.Body.Seq == 0 {
			return
		}
		if replay && !runEventMatchesReplyIDs(runEvent, replayState.MessageIDs) {
			return
		}
		if !dedup.mark(runEvent.Body.Seq) {
			return
		}
		se, ok := ProjectRunEvent(runEvent)
		if !ok {
			logs.WarnContextf(ctx, "unknown run event type: %v", runEvent.Body.Event)
			return
		}
		if err := sink.Emit(ctx, &agent.Event{
			Type:    se.Type,
			Content: toJSONString(se),
		}); err != nil {
			logs.ErrorContextf(ctx, "failed to emit session event for session %s: %v", sessionPID, err)
		}
		// 收到终端事件后，服务端主动结束 SSE 流
		switch se.Type {
		case events.EventCompleted, events.EventFailed, events.EventCancelled:
			innerCancel()
		}
	}

	handler := func(msg *nats.Msg) {
		var runEvent messaging.RunEvent
		if err := json.Unmarshal(msg.Data, &runEvent); err != nil {
			logs.WarnContextf(ctx, "failed to unmarshal to RunEvent: %v", err)
			return
		}
		emitEvent(runEvent)
	}

	// Subscribe to run.stream and run.state lanes concurrently, since
	// SubscribeFrom blocks until ctx is done.
	g, gctx := errgroup.WithContext(innerCtx)

	// Subscribe to run.stream lane.
	g.Go(func() error {
		return s.eventbus.SubscribeFrom(gctx, streamTopic, streamStartSeq, handler)
	})

	// Subscribe to run.state lane (non-fatal if it fails; stream lane still delivers).
	g.Go(func() error {
		if err := s.eventbus.SubscribeFrom(gctx, stateTopic, stateStartSeq, handler); err != nil {
			logs.WarnContextf(ctx, "subscribe to run.state failed: %v (stream lane still active)", err)
		}
		return nil
	})

	// Block until innerCtx is done (cancelled by terminal event or parent ctx).
	<-innerCtx.Done()

	// Wait for goroutines to clean up (they'll exit when innerCtx is Done).
	_ = g.Wait()
	return nil
}

// runEventDedup tracks the highest Seq seen and deduplicates across run lanes.
type runEventDedup struct {
	mu      sync.Mutex
	highest int64
	seen    map[int64]struct{}
}

func (d *runEventDedup) mark(seq int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen == nil {
		d.seen = make(map[int64]struct{})
	}
	if _, ok := d.seen[seq]; ok {
		return false
	}
	d.seen[seq] = struct{}{}
	if seq > d.highest {
		d.highest = seq
	}
	return true
}

type sessionReplayState struct {
	StreamStartSeq uint64
	StateStartSeq  uint64
	MessageIDs     map[string]struct{}
}

func (s *sessionService) getSessionReplayState(ctx context.Context, sessionID uint) (sessionReplayState, error) {
	messages, err := db.GetRecentProcessingUserMessages(ctx, s.db, sessionID, time.Now().Add(-sessionProcessingWindow))
	if err != nil {
		return sessionReplayState{}, err
	}
	state := sessionReplayState{MessageIDs: map[string]struct{}{}}
	for _, message := range messages {
		id := strconv.FormatUint(uint64(message.ID), 10)
		state.MessageIDs[id] = struct{}{}

		// Stream start seq — uses response_stream_start_seq (temporary compat field).
		streamSeq, ok := responseStreamStartSeq(message.Metadata)
		if ok && streamSeq > 0 {
			if state.StreamStartSeq == 0 || streamSeq < state.StreamStartSeq {
				state.StreamStartSeq = streamSeq
			}
		}

		// State start seq — new field for run.state lane.
		stateSeq, ok := stateStartSeq(message.Metadata)
		if ok && stateSeq > 0 {
			if state.StateStartSeq == 0 || stateSeq < state.StateStartSeq {
				state.StateStartSeq = stateSeq
			}
		}
	}
	return state, nil
}

func runEventMatchesReplyIDs(runEvent messaging.RunEvent, ids map[string]struct{}) bool {
	if len(ids) == 0 {
		return false
	}
	for _, id := range runEvent.Body.ReplyToMessageIDs {
		if _, ok := ids[id]; ok {
			return true
		}
	}
	return false
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
		ErrorMsg:    message.ErrorMsg,
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

	if len(message.Attachments) > 0 {
		result.Attachments = append([]types.MessageAttachment{}, message.Attachments...)
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
	switch agent.EventType(eventType) {
	case events.EventTodoSnapshot, events.EventTodoUpdated:
		return true
	default:
		return false
	}
}

func setResponseStreamStartSeq(metadata *types.ObjectMetadata, seq uint64) {
	if metadata.Extra == nil {
		metadata.Extra = map[string]interface{}{}
	}
	metadata.Extra[responseStreamStartSeqKey] = seq
}

func setStateStartSeq(metadata *types.ObjectMetadata, seq uint64) {
	if metadata.Extra == nil {
		metadata.Extra = map[string]interface{}{}
	}
	metadata.Extra[stateStartSeqKey] = seq
}

func responseStreamStartSeq(metadata types.ObjectMetadata) (uint64, bool) {
	if metadata.Extra == nil {
		return 0, false
	}
	value, ok := metadata.Extra[responseStreamStartSeqKey]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case uint64:
		return v, true
	case uint:
		return uint64(v), true
	case int64:
		if v <= 0 {
			return 0, false
		}
		return uint64(v), true
	case int:
		if v <= 0 {
			return 0, false
		}
		return uint64(v), true
	case float64:
		if v <= 0 {
			return 0, false
		}
		return uint64(v), true
	default:
		return 0, false
	}
}

func attachReplyToMessageIDs(metadata *types.ObjectMetadata, ids []string) {
	normalized := normalizedReplyIDStrings(ids)
	if len(normalized) == 0 {
		return
	}
	if metadata.Extra == nil {
		metadata.Extra = map[string]interface{}{}
	}
	metadata.Extra[replyToMessageIDsKey] = normalized
}

func replyMessageIDs(rawIDs []string, fallbackRequestID string) []uint {
	seen := map[uint]struct{}{}
	result := make([]uint, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
		if err != nil || id == 0 {
			continue
		}
		value := uint(id)
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) == 0 {
		if id, ok := messageIDFromRequestID(fallbackRequestID); ok {
			result = append(result, id)
		}
	}
	return result
}

func normalizedReplyIDStrings(rawIDs []string) []string {
	ids := replyMessageIDs(rawIDs, "")
	if len(ids) == 0 {
		return nil
	}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, strconv.FormatUint(uint64(id), 10))
	}
	return result
}

func messageIDFromRequestID(requestID string) (uint, bool) {
	value := strings.TrimSpace(requestID)
	if !strings.HasPrefix(value, "req_") {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(value, "req_"), 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint(id), true
}

func (s *sessionService) CancelSessionRun(ctx context.Context, sessionID string, req *contract.CancelSessionRunRequest) (*contract.CancelSessionRunResponse, error) {
	session, caller, err := s.getSessionForCaller(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	workerID := session.AllocatedAssistantID
	if workerID == 0 {
		return &contract.CancelSessionRunResponse{
			SessionID: sessionID,
			Status:    "no_active_run",
		}, nil
	}

	topic, err := messaging.WorkerCommandSubject(caller.OrgID, workerID, messaging.LaneControl)
	if err != nil {
		return nil, fmt.Errorf("build control topic: %w", err)
	}

	cmd := messaging.NewCancelRunCommand(
		fmt.Sprintf("ctrl_%s", snowflake.GenerateIDBase58()),
		messaging.RouteContext{
			OrgID:     caller.OrgID,
			WorkerID:  workerID,
			SessionID: sessionID,
		},
		messaging.CancelRunCommandPayload{
			RunID:  req.RunID,
			Reason: req.Reason,
		},
		req.RunID,
	)

	if err := s.eventbus.Publish(ctx, topic, cmd); err != nil {
		return nil, fmt.Errorf("publish cancel control: %w", err)
	}

	logs.InfoContextf(ctx, "CancelSessionRun: session=%s worker=%d run=%s",
		sessionID, workerID, req.RunID)

	return &contract.CancelSessionRunResponse{
		SessionID: sessionID,
		Status:    "cancelled",
	}, nil
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
	attachReplyToMessageIDs(&msgEntity.Metadata, req.ReplyToMessageIDs)
	if req.Usage != nil {
		msgEntity.Usage = *req.Usage
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := db.CreateMessage(ctx, tx, msgEntity); err != nil {
			return fmt.Errorf("create message for %s: %w", req.SessionID, err)
		}
		if err := s.updateReplyMessageStatus(ctx, tx, session.ID, req.ReplyToMessageIDs, string(types.MessageStatusCompleted)); err != nil {
			return err
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
	if err := db.IncrementMessageCount(ctx, s.db, session.ID); err != nil {
		logs.WarnContextf(ctx, "increment message count for %s: %v", req.SessionID, err)
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
		Content:     req.Content,
		ErrorMsg:    req.ErrorMsg,
		MessageType: string(types.MessageTypeText),
		Status:      status,
		Sequence:    sequence,
		Timestamp:   req.CreatedAt.UnixMilli(),
	}
	if msgEntity.Content == "" {
		msgEntity.Content = req.ErrorMsg
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
	attachReplyToMessageIDs(&msgEntity.Metadata, req.ReplyToMessageIDs)
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
		if err := s.updateReplyMessageStatus(ctx, tx, session.ID, req.ReplyToMessageIDs, status); err != nil {
			return err
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

func stateStartSeq(metadata types.ObjectMetadata) (uint64, bool) {
	if metadata.Extra == nil {
		return 0, false
	}
	value, ok := metadata.Extra[stateStartSeqKey]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case uint64:
		return v, true
	case uint:
		return uint64(v), true
	case int64:
		if v <= 0 {
			return 0, false
		}
		return uint64(v), true
	case int:
		if v <= 0 {
			return 0, false
		}
		return uint64(v), true
	case float64:
		if v <= 0 {
			return 0, false
		}
		return uint64(v), true
	default:
		return 0, false
	}
}
