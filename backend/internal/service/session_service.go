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
	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/llm"
	"github.com/insmtx/Leros/backend/internal/modelrouter"
	"github.com/insmtx/Leros/backend/pkg/messaging"
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
	artifactVersionLookupAttempts  = 8
	artifactVersionLookupDelay     = 50 * time.Millisecond
)

// ErrNoReplyMessageIDs is returned when a run-started stream event lacks
// identifiable user messages to target.
var ErrNoReplyMessageIDs = errors.New("no reply message ids in stream event")

type sessionService struct {
	db           *gorm.DB
	perm         *PermissionService
	eventbus     eventbus.EventBus
	inferrer     AssistantInferrer
	giteaClient  *gitea.Client
	giteaCfg     *config.GiteaConfig
	env          string
	modelInvoker modelrouter.Invoker
	userRepo     account.UserRepository
	orgRepo      account.OrgRepository
}

func NewSessionService(db *gorm.DB, perm *PermissionService, eventbus eventbus.EventBus, inferrer AssistantInferrer, giteaClient *gitea.Client, giteaCfg *config.GiteaConfig, env string, modelInvoker modelrouter.Invoker, userRepo account.UserRepository, orgRepo account.OrgRepository) contract.SessionService {
	return &sessionService{
		db:           db,
		perm:         perm,
		eventbus:     eventbus,
		inferrer:     inferrer,
		giteaClient:  giteaClient,
		giteaCfg:     giteaCfg,
		env:          env,
		modelInvoker: modelInvoker,
		userRepo:     userRepo,
		orgRepo:      orgRepo,
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
	if (session.Type == types.SessionTypeTask || session.Type == types.SessionTypeProject) &&
		session.ProjectID != nil && *session.ProjectID > 0 {
		return session, caller, nil
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
		if caller.WorkerID == 0 {
			return nil, errors.New("permission denied")
		}
		if session.ProjectID == nil || *session.ProjectID == 0 {
			return nil, errors.New("permission denied")
		}
		ok, err := db.IsProjectAssistantBound(ctx, s.db, caller.OrgID, *session.ProjectID, caller.WorkerID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("permission denied")
		}
		return session, nil
	}
	// 群聊准入：task/project session 入口权限由 Handler PermGuardViaSession 保证。
	if (session.Type == types.SessionTypeTask || session.Type == types.SessionTypeProject) &&
		session.ProjectID != nil && *session.ProjectID > 0 {
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

	assistantID, err := resolveAssistantByPublicID(ctx, s.db, caller.OrgID, req.AssistantID)
	if err != nil {
		return nil, err
	}
	_, _, err = resolveRuntimeWorker(ctx, s.db, caller.OrgID, assistantID, s.inferrer)
	if err != nil {
		return nil, err
	}
	session := &types.Session{
		PublicID:     sessionID,
		Type:         types.SessionType(req.Type),
		Uin:          caller.Uin,
		OrgID:        caller.OrgID,
		Status:       string(types.SessionStatusActive),
		Title:        req.Title,
		MessageCount: 0,
		ExpiredAt:    req.ExpiredAt,
	}

	if req.Metadata != nil {
		session.Metadata = *req.Metadata
	}

	if err := db.CreateSession(ctx, s.db, session); err != nil {
		return nil, err
	}

	return convertToContractSession(ctx, session, s.db), nil
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

	result := convertToContractSession(ctx, session, s.db)
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

	return convertToContractSession(ctx, session, s.db), nil
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
	if req.AssistantID != nil && *req.AssistantID != "" {
		assistantID, err := resolveAssistantByPublicID(ctx, s.db, caller.OrgID, *req.AssistantID)
		if err != nil {
			return nil, err
		}
		if assistantID > 0 {
			opt.AddFilter("assistant_id", fmt.Sprintf("%d", assistantID))
		}
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
		items = append(items, *convertToContractSession(ctx, session, s.db))
	}

	return &contract.SessionList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
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
			return nil, fmt.Errorf("get project %d: %w", *session.ProjectID, err)
		}
		if project == nil {
			return nil, fmt.Errorf("project %d not found", *session.ProjectID)
		}
		if err := attachFilesToProject(ctx, s.db, session.OrgID, session.Uin, session.TaskID, project, req.Attachments); err != nil {
			return nil, fmt.Errorf("attach files to project: %w", err)
		}
	}

	// 解析消息级别的 assistant 路由覆盖
	var routing *MessageRoutingOverride
	if len(req.AssistantIDs) > 0 {
		assistantIDs, err := resolveAssistantIDsByPublicID(ctx, s.db, session.OrgID, req.AssistantIDs)
		if err != nil {
			return nil, fmt.Errorf("resolve assistant ids: %w", err)
		}
		if len(assistantIDs) > 0 {
			var projectID uint
			if session.ProjectID != nil {
				projectID = *session.ProjectID
			}
			assistantID, workerID, err := resolveProjectAssistantWorker(ctx, s.db, session.OrgID, projectID, assistantIDs, s.inferrer)
			if err != nil {
				return nil, fmt.Errorf("resolve assistant worker: %w", err)
			}
			routing = &MessageRoutingOverride{AssistantID: assistantID, WorkerID: workerID}
		}
	} else {
		projectID := uint(0)
		if session.ProjectID != nil {
			projectID = *session.ProjectID
		}
		assistantID, workerID, err := resolveDefaultBindingWorker(ctx, s.db, session.OrgID, projectID, s.inferrer)
		if err != nil {
			return nil, fmt.Errorf("resolve default assistant worker: %w", err)
		}
		routing = &MessageRoutingOverride{AssistantID: assistantID, WorkerID: workerID}
	}

	message, err := s.newMessagePoster().PostMessage(ctx, session, types.ExecutionMode(req.ExecutionMode), func(sequence int64) *types.SessionMessage {
		msg := s.buildMessage(req, sequence)
		if routing != nil {
			msg.AssistantID = routing.AssistantID
		}
		return msg
	}, routing)
	if err != nil {
		return nil, err
	}

	if session.ProjectID != nil && *session.ProjectID != 0 && req.Role == string(types.MessageRoleUser) {
		// 中文注释：只在用户主动发言时刷新项目活跃时间，避免助手流式输出把项目顺序不断顶来顶去。
		if err := db.TouchProjectUpdatedAt(ctx, s.db, *session.ProjectID, time.Now()); err != nil {
			logs.WarnContextf(ctx, "touch project updated_at after add message %s: %v", session.PublicID, err)
		}
	}

	return s.convertToContractSessionMessage(ctx, session.OrgID, message, session.PublicID), nil
}

func (s *sessionService) newMessagePoster() *MessagePoster {
	return NewMessagePoster(s.db, s.perm, s.eventbus, s.inferrer, s.giteaClient, s.giteaCfg, s.env, s.userRepo, s.orgRepo)
}

func (s *sessionService) CreateInitialMessage(ctx context.Context, req *contract.NewMessageRequest) (*contract.NewMessageResponse, error) {
	if strings.TrimSpace(req.Content) == "" && len(req.AssistantIDs) == 0 && len(req.Attachments) == 0 {
		return nil, errors.New("content is required")
	}

	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 || caller.OrgID == 0 {
		return nil, errors.New("user not authenticated or org not set")
	}

	return s.newMessagePoster().RunNewMessage(ctx, req, caller)
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
	message.Usage = normalizeMessageUsage(req.Usage)

	if message.MessageType == "" {
		message.MessageType = string(types.MessageTypeText)
	}

	return message
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

func (s *sessionService) SubmitApproval(ctx context.Context, req *contract.SubmitApprovalRequest) error {
	_, caller, err := s.getSessionForCaller(ctx, req.SessionID)
	if err != nil {
		return err
	}
	req.OrgID = caller.OrgID

	workerID, err := db.GetWorkerIDByAssistantPublicID(ctx, s.db, req.AssistantID)
	if err != nil {
		return fmt.Errorf("resolve worker by assistant: %w", err)
	}

	topic, err := messaging.WorkerCommandSubject(req.OrgID, workerID, messaging.LaneInteraction)
	if err != nil {
		return fmt.Errorf("build approval topic: %w", err)
	}

	cmd := withRequestTrace(ctx, messaging.NewApprovalResolveCommand(
		fmt.Sprintf("approval_%s", snowflake.GenerateIDBase58()),
		messaging.RouteContext{
			OrgID:     req.OrgID,
			WorkerID:  workerID,
			SessionID: req.SessionID,
			ClientIP:  llm.GetCtxString(ctx, llm.CtxClientIP),
		},
		messaging.ApprovalResolveCommandPayload{
			Action: req.Action,
			Reason: req.Reason,
		},
		req.RequestID,
	))
	return s.eventbus.Publish(ctx, topic, cmd)
}

func (s *sessionService) SubmitQuestionAnswer(ctx context.Context, req *contract.SubmitQuestionAnswerRequest) error {
	_, caller, err := s.getSessionForCaller(ctx, req.SessionID)
	if err != nil {
		return err
	}
	req.OrgID = caller.OrgID

	workerID, err := db.GetWorkerIDByAssistantPublicID(ctx, s.db, req.AssistantID)
	if err != nil {
		return fmt.Errorf("resolve worker by assistant: %w", err)
	}

	topic, err := messaging.WorkerCommandSubject(req.OrgID, workerID, messaging.LaneInteraction)
	if err != nil {
		return fmt.Errorf("build question answer topic: %w", err)
	}

	cmd := withRequestTrace(ctx, messaging.NewQuestionAnswerCommand(
		fmt.Sprintf("question_%s", snowflake.GenerateIDBase58()),
		messaging.RouteContext{
			OrgID:     req.OrgID,
			WorkerID:  workerID,
			SessionID: req.SessionID,
			ClientIP:  llm.GetCtxString(ctx, llm.CtxClientIP),
		},
		messaging.QuestionAnswerCommandPayload{
			Answers: req.Answers,
		},
		req.RequestID,
	))
	return s.eventbus.Publish(ctx, topic, cmd)
}

func lookupSessionRuntimeStatus(ctx context.Context, gdb *gorm.DB, sessionID uint) string {
	messages, err := db.GetRecentProcessingUserMessages(ctx, gdb, sessionID, time.Now().Add(-sessionProcessingWindow))
	if err != nil {
		logs.WarnContextf(ctx, "get session runtime status failed: session=%d error=%v", sessionID, err)
		return sessionRuntimeStatusIdle
	}
	if len(messages) > 0 {
		return sessionRuntimeStatusResponding
	}
	return sessionRuntimeStatusIdle
}

func (s *sessionService) sessionRuntimeStatus(ctx context.Context, sessionID uint) string {
	return lookupSessionRuntimeStatus(ctx, s.db, sessionID)
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

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	}); err != nil {
		return err
	}

	publishAssistantReplyStartedEvent(ctx, s.db, s.eventbus, session, req.RunID, req.AssistantID)

	logs.InfoContextf(ctx, "handled session run started: session_id=%s run_id=%s state_start_seq=%d reply_ids=%v",
		req.SessionID, req.RunID, req.StateStartSeq, req.ReplyToMessageIDs)

	return nil
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
		items = append(items, *s.convertToContractSessionMessage(ctx, session.OrgID, message, session.PublicID))
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

// StreamSessionEvents 同时订阅 run.stream 和 run.state lane，按 run 内 Seq 去重后推送 SSE 事件。
//
//   - run.stream: delta, tool_call, todo 等高频事件
//   - run.state:  terminal, approval, question 等关键状态事件
//
// 两个 lane 的事件互不重复，Seq 去重仅作保底。
func (s *sessionService) StreamSessionEvents(ctx context.Context, sessionPID string, replay bool, assistantPublicID string, sink contract.SessionEventSink) error {
	session, caller, err := s.getSessionForCaller(ctx, sessionPID)
	if err != nil {
		return err
	}

	var filterAssistantID string
	var filterWorkerID uint
	if assistantPublicID != "" {
		filterAssistantID = assistantPublicID
		assistantID, err := resolveAssistantByPublicID(ctx, s.db, caller.OrgID, assistantPublicID)
		if err != nil {
			return err
		}
		if assistantID == 0 {
			return fmt.Errorf("digital assistant not found: %s", assistantPublicID)
		}
		deployment, err := db.GetWorkerDeploymentByAssistantID(ctx, s.db, assistantID)
		if err != nil {
			return fmt.Errorf("resolve assistant worker deployment: %w", err)
		}
		if deployment == nil {
			return fmt.Errorf("worker deployment not found for assistant %s", assistantPublicID)
		}
		if deployment.OrgID != caller.OrgID {
			return errors.New("permission denied: assistant belongs to different org")
		}
		filterWorkerID = deployment.WorkerID
	}

	replayState := sessionReplayState{}
	streamStartSeq := int64(0)
	stateStartSeq := int64(0)
	if replay {
		replayState, err = s.getSessionReplayState(ctx, session.ID)
		if err != nil {
			return err
		}
		// 优先使用 state_start_seq，旧消息没有该值时回退到 response_stream_start_seq。
		if replayState.StateStartSeq > 0 && replayState.StateStartSeq <= math.MaxInt64 {
			stateStartSeq = int64(replayState.StateStartSeq)
		} else if replayState.StreamStartSeq > 0 && replayState.StreamStartSeq <= math.MaxInt64 {
			stateStartSeq = int64(replayState.StreamStartSeq)
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
		if filterAssistantID != "" && runEvent.Route.AssistantID != "" &&
			runEvent.Route.AssistantID != filterAssistantID {
			return
		}
		if filterWorkerID > 0 && runEvent.Route.AssistantID == "" &&
			runEvent.Route.WorkerID != filterWorkerID {
			return
		}
		if replay && !runEventMatchesReplyIDs(runEvent, replayState.MessageIDs) {
			return
		}
		if !dedup.mark(runEvent.Body.Seq) {
			return
		}
		se, ok := s.projectSessionRunEvent(ctx, caller.OrgID, runEvent)
		if !ok {
			logs.WarnContextf(ctx, "unknown run event type: %v", runEvent.Body.Event)
			return
		}
		if err := sink.EmitSessionEvent(ctx, se); err != nil {
			logs.ErrorContextf(ctx, "failed to emit session event for session %s: %v", sessionPID, err)
		}
		// 收到终端事件后，服务端主动结束 SSE 流
		switch runEvent.Body.Event {
		case messaging.RunEventRunCompleted,
			messaging.RunEventRunFailed,
			messaging.RunEventRunCancelled:
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

func (s *sessionService) projectSessionRunEvent(
	ctx context.Context,
	orgID uint,
	runEvent messaging.RunEvent,
) (*contract.SessionEvent, bool) {
	if runEvent.Body.Event == messaging.RunEventArtifactDeclared && runEvent.Body.Payload.Artifact != nil {
		artifact := runEvent.Body.Payload.Artifact
		artifact.VersionNo = s.lookupArtifactVersion(ctx, orgID, artifact.ArtifactID)
	}
	return ProjectRunEvent(runEvent)
}

func (s *sessionService) lookupArtifactVersion(ctx context.Context, orgID uint, artifactID string) int {
	for attempt := 0; attempt < artifactVersionLookupAttempts; attempt++ {
		projectFile, err := db.GetProjectFileByFilePublicID(ctx, s.db, orgID, artifactID)
		if err != nil {
			logs.WarnContextf(ctx, "resolve artifact version: artifact_id=%s err=%v", artifactID, err)
			return 0
		}
		if projectFile != nil {
			return projectFile.VersionNo
		}
		if attempt == artifactVersionLookupAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(artifactVersionLookupDelay):
		}
	}
	return 0
}

// StreamGlobalEvents 为调用方订阅其所属所有 project 的全局通知事件
// （message.created 等），通过 ch 推送。调用阻塞直到 ctx 取消。
//
// 实现：使用 org.{org}.project.*.notify wildcard 订阅，覆盖用户所属 org
// 下所有 project（含连接建立后新增的 project）。handler 内通过
// IsProjectUserMember 做权限过滤，仅转发用户所属 project 的事件。
func (s *sessionService) StreamGlobalEvents(ctx context.Context, orgID, userID uint, replaySinceSeq uint64, ch chan<- *messaging.GlobalEventPayload) error {
	if orgID == 0 || userID == 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	startSeq := int64(replaySinceSeq)
	wildcardSubject := "org." + strconv.FormatUint(uint64(orgID), 10) + ".project.*.notify"

	handler := func(msg *nats.Msg) {
		var payload messaging.GlobalEventPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			logs.WarnContextf(ctx, "global events: unmarshal payload: %v", err)
			return
		}
		if meta, err := msg.Metadata(); err == nil {
			payload.Seq = meta.Sequence.Stream
		}
		// 权限过滤：仅转发用户所属 project 的事件，支持动态新增 project
		if member, err := db.IsProjectUserMember(ctx, s.db, orgID, userID, payload.ProjectID); err != nil || !member {
			return
		}
		select {
		case ch <- &payload:
		default:
			logs.WarnContextf(ctx, "global events: channel full, dropping event type=%s seq=%d", payload.Type, payload.Seq)
		}
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := s.eventbus.SubscribeFrom(gctx, wildcardSubject, startSeq, handler); err != nil {
			logs.WarnContextf(ctx, "global events: subscribe %s failed: %v", wildcardSubject, err)
		}
		return nil
	})

	<-ctx.Done()
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

func convertToContractSession(ctx context.Context, session *types.Session, db *gorm.DB) *contract.Session {
	result := &contract.Session{
		SessionID:        session.PublicID,
		Type:             string(session.Type),
		Uin:              session.Uin,
		OrgID:            session.OrgID,
		Status:           session.Status,
		Title:            session.Title,
		TitleManuallySet: session.TitleManuallySet,
		MessageCount:     session.MessageCount,
		CreatedAt:        session.CreatedAt,
		UpdatedAt:        session.UpdatedAt,
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

func publishAssistantReplyStartedEvent(
	ctx context.Context,
	gdb *gorm.DB,
	eb eventbus.EventBus,
	session *types.Session,
	runID string,
	assistantID uint,
) {
	if session == nil || eb == nil || gdb == nil {
		return
	}
	if session.ProjectID == nil || *session.ProjectID == 0 || session.OrgID == 0 {
		logs.WarnContextf(ctx, "skip assistant reply event: session_id=%s project_id=%v org_id=%d", session.PublicID, session.ProjectID, session.OrgID)
		return
	}

	data := messaging.MessageCreatedData{
		SenderType: messaging.SenderTypeAssistant,
		RunID:      runID,
	}
	if assistantID > 0 {
		publicID := assistantIDToPublicID(ctx, gdb, assistantID)
		data.AssistantID = &publicID
		if da, err := db.GetDigitalAssistantByID(ctx, gdb, assistantID); err == nil && da != nil {
			data.AssistantName = da.Name
		} else if err != nil {
			logs.WarnContextf(ctx, "publishAssistantReplyStartedEvent: get assistant %d: %v", assistantID, err)
		}
	}

	rawData, err := json.Marshal(data)
	if err != nil {
		logs.WarnContextf(ctx, "publishAssistantReplyStartedEvent: marshal data: %v", err)
		return
	}

	subject, err := messaging.ProjectNotifySubject(session.OrgID, *session.ProjectID)
	if err != nil {
		logs.WarnContextf(ctx, "publishAssistantReplyStartedEvent: build subject: %v", err)
		return
	}

	payload := messaging.GlobalEventPayload{
		Type:      messaging.GlobalEventMessageCreated,
		ProjectID: *session.ProjectID,
		SessionID: session.PublicID,
		Timestamp: time.Now().UnixMilli(),
		Data:      rawData,
	}

	if err := eb.Publish(ctx, subject, payload); err != nil {
		logs.WarnContextf(ctx, "publishAssistantReplyStartedEvent: publish to %s: %v", subject, err)
	}
	logs.InfoContextf(ctx, "published message.created (assistant): session_id=%s project_id=%d run_id=%s assistant_id=%d subject=%s",
		session.PublicID, *session.ProjectID, runID, assistantID, subject)
}

func normalizeMessageUsage(usage *types.MessageUsage) types.MessageUsage {
	if usage == nil {
		return types.MessageUsage{}
	}
	normalized := *usage
	normalized.TotalTokens = normalized.InputTokens + normalized.OutputTokens
	return normalized
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
		SenderUin:   message.SenderUin,
		SenderName:  message.SenderName,
		RunID:       message.RunID,
	}

	if message.Chunks != nil && len(message.Chunks) > 0 {
		result.Chunks = make([]contract.SessionEvent, 0, len(message.Chunks))
		for _, chunk := range message.Chunks {
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

	usage := normalizeMessageUsage(&message.Usage)
	result.Usage = &usage

	return result
}

func (s *sessionService) convertToContractSessionMessage(
	ctx context.Context,
	orgID uint,
	message *types.SessionMessage,
	publicID string,
) *contract.SessionMessage {
	result := convertToContractSessionMessage(message, publicID)
	if message == nil {
		return result
	}
	if len(message.Chunks) > 0 {
		result.Chunks = result.Chunks[:0]
		for _, chunk := range message.Chunks {
			runEvent, ok := runEventFromRecord(publicID, chunk)
			if !ok {
				continue
			}
			event, ok := s.projectSessionRunEvent(ctx, orgID, runEvent)
			if ok {
				result.Chunks = append(result.Chunks, *event)
			}
		}
	}
	for index := range result.Artifacts {
		artifact := &result.Artifacts[index]
		artifact.VersionNo = s.lookupArtifactVersion(ctx, orgID, artifact.ArtifactID)
	}
	return result
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

	if req.AssistantID != "" {
		workerID, err := db.GetWorkerIDByAssistantPublicID(ctx, s.db, req.AssistantID)
		if err != nil {
			return nil, fmt.Errorf("resolve worker by assistant: %w", err)
		}

		topic, err := messaging.WorkerCommandSubject(caller.OrgID, workerID, messaging.LaneControl)
		if err != nil {
			return nil, fmt.Errorf("build control topic: %w", err)
		}

		cmd := withRequestTrace(ctx, messaging.NewCancelRunCommand(
			fmt.Sprintf("ctrl_%s", snowflake.GenerateIDBase58()),
			messaging.RouteContext{
				OrgID:     caller.OrgID,
				WorkerID:  workerID,
				SessionID: sessionID,
				ClientIP:  llm.GetCtxString(ctx, llm.CtxClientIP),
			},
			messaging.CancelRunCommandPayload{
				RunID:  req.RunID,
				Reason: req.Reason,
			},
			req.RunID,
		))

		if err := s.eventbus.Publish(ctx, topic, cmd); err != nil {
			return nil, fmt.Errorf("publish cancel control: %w", err)
		}

		logs.InfoContextf(ctx, "CancelSessionRun: session=%s worker=%d run=%s assistant=%s",
			sessionID, workerID, req.RunID, req.AssistantID)

		return &contract.CancelSessionRunResponse{
			SessionID: sessionID,
			Status:    "cancelled",
		}, nil
	}

	// 未指定 assistant_id：取消 session 关联的所有活跃 worker 的 run
	if session.ProjectID != nil && *session.ProjectID > 0 {
		resource, err := db.GetResourceByBizID(ctx, s.db, caller.OrgID, types.ResourceTypeProject, *session.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("find project resource: %w", err)
		}
		if resource != nil {
			bindings, err := db.ListResourceBindingsByResourceID(ctx, s.db, resource.ID)
			if err != nil {
				return nil, fmt.Errorf("list project bindings: %w", err)
			}
			var lastErr error
			for _, binding := range bindings {
				if binding.AssistantID == nil || *binding.AssistantID == 0 {
					continue
				}
				_, workerID, err := resolveRuntimeWorker(ctx, s.db, caller.OrgID, *binding.AssistantID, s.inferrer)
				if err != nil {
					logs.WarnContextf(ctx, "CancelSessionRun: resolve worker for assistant %d failed: %v", *binding.AssistantID, err)
					lastErr = err
					continue
				}
				topic, err := messaging.WorkerCommandSubject(caller.OrgID, workerID, messaging.LaneControl)
				if err != nil {
					lastErr = err
					continue
				}
				cmd := withRequestTrace(ctx, messaging.NewCancelRunCommand(
					fmt.Sprintf("ctrl_%s", snowflake.GenerateIDBase58()),
					messaging.RouteContext{
						OrgID:     caller.OrgID,
						WorkerID:  workerID,
						SessionID: sessionID,
						ClientIP:  llm.GetCtxString(ctx, llm.CtxClientIP),
					},
					messaging.CancelRunCommandPayload{
						RunID:  req.RunID,
						Reason: req.Reason,
					},
					req.RunID,
				))
				if err := s.eventbus.Publish(ctx, topic, cmd); err != nil {
					logs.WarnContextf(ctx, "CancelSessionRun: publish cancel to worker %d failed: %v", workerID, err)
					lastErr = err
				}
				logs.InfoContextf(ctx, "CancelSessionRun: session=%s worker=%d run=%s assistant=%d",
					sessionID, workerID, req.RunID, *binding.AssistantID)
			}
			if lastErr != nil {
				return nil, lastErr
			}
			return &contract.CancelSessionRunResponse{
				SessionID: sessionID,
				Status:    "cancelled",
			}, nil
		}
	}

	return &contract.CancelSessionRunResponse{
		SessionID: sessionID,
		Status:    "no_active_run",
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

	// 群聊模式下为 AI 回复填充发送者名称（反查 DigitalAssistant.Name）
	assistantName := ""
	if req.AssistantID > 0 {
		if da, err := db.GetDigitalAssistantByID(ctx, s.db, req.AssistantID); err == nil && da != nil {
			assistantName = da.Name
		} else if err != nil {
			logs.WarnContextf(ctx, "complete session message: get assistant %d: %v", req.AssistantID, err)
		}
	}

	msgEntity := &types.SessionMessage{
		SessionID:   session.ID,
		Role:        string(types.MessageRoleAssistant),
		Content:     req.Content,
		MessageType: string(types.MessageTypeText),
		Status:      string(types.MessageStatusCompleted),
		Sequence:    sequence,
		Timestamp:   req.CreatedAt.UnixMilli(),
		SenderName:  assistantName,
		RunID:       req.RunID,
		AssistantID: req.AssistantID,
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
	msgEntity.Usage = normalizeMessageUsage(req.Usage)

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
	s.scheduleFirstTurnWorkTitleUpdate(ctx, session, msgEntity, true)

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

	// 群聊模式下为 AI 回复填充发送者名称（反查 DigitalAssistant.Name）
	assistantName := ""
	if req.AssistantID > 0 {
		if da, err := db.GetDigitalAssistantByID(ctx, s.db, req.AssistantID); err == nil && da != nil {
			assistantName = da.Name
		} else if err != nil {
			logs.WarnContextf(ctx, "failed session message: get assistant %d: %v", req.AssistantID, err)
		}
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
		SenderName:  assistantName,
		RunID:       req.RunID,
		AssistantID: req.AssistantID,
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
	msgEntity.Usage = normalizeMessageUsage(req.Usage)
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
	s.scheduleFirstTurnWorkTitleUpdate(ctx, session, msgEntity, false)

	return nil
}

func (s *sessionService) scheduleFirstTurnWorkTitleUpdate(
	ctx context.Context,
	session *types.Session,
	message *types.SessionMessage,
	includeAssistantMessage bool,
) {
	if s == nil || session == nil || message == nil || session.OrgID == 0 {
		logs.DebugContextf(ctx, "work title: skip schedule due to missing service/session/message/org")
		return
	}
	if session.Type != types.SessionTypeTask || session.ProjectID == nil || session.TaskID == nil {
		logs.DebugContextf(ctx, "work title: skip schedule for non-task session=%s type=%s", session.PublicID, session.Type)
		return
	}

	sessionID := session.PublicID
	assistantMessage := ""
	if includeAssistantMessage {
		assistantMessage = message.Content
	}

	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		caller = &types.Caller{Uin: session.Uin, OrgID: session.OrgID}
	}

	logs.InfoContextf(ctx, "work title: scheduled first-turn update session=%s include_assistant=%t", sessionID, includeAssistantMessage)
	go func() {
		titleCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, trace := auth.FromContext(ctx)
		titleCtx = auth.WithContext(titleCtx, caller, trace)
		if ip := llm.GetCtxString(ctx, llm.CtxClientIP); ip != "" {
			titleCtx = llm.WithCtxString(titleCtx, llm.CtxClientIP, ip)
		}

		updater := NewWorkTitleUpdater(s.db, s.eventbus, s.modelInvoker)
		if err := updater.UpdateAfterFirstTurn(titleCtx, sessionID, assistantMessage); err != nil {
			logs.WarnContextf(titleCtx, "first-turn work title update failed for session %s: %v", sessionID, err)
			return
		}
		logs.DebugContextf(titleCtx, "work title: first-turn update finished session=%s", sessionID)
	}()
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

func resolveAssistantByPublicID(ctx context.Context, database *gorm.DB, orgID uint, publicID string) (uint, error) {
	if publicID == "" || publicID == "0" {
		return 0, nil
	}
	da, err := db.GetDigitalAssistantByPublicID(ctx, database, publicID)
	if err != nil {
		return 0, err
	}
	if da == nil {
		return 0, nil
	}
	if da.OrgID != orgID {
		return 0, errors.New("digital assistant organization mismatch")
	}
	return da.ID, nil
}

func assistantIDToPublicID(ctx context.Context, database *gorm.DB, assistantID uint) string {
	if database == nil || assistantID == 0 {
		return ""
	}
	da, err := db.GetDigitalAssistantByID(ctx, database, assistantID)
	if err != nil || da == nil {
		return ""
	}
	return da.PublicID
}

func workerIDToPublicID(ctx context.Context, database *gorm.DB, orgID uint, workerID uint) string {
	if database == nil || orgID == 0 || workerID == 0 {
		return ""
	}
	deployment, err := db.GetWorkerDeploymentByOrgWorkerID(ctx, database, orgID, workerID)
	if err != nil || deployment == nil {
		return ""
	}
	return deployment.PublicID
}
