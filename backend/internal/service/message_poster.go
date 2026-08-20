package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"code.gitea.io/sdk/gitea"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/internal/infra/git"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/llm"
	"github.com/insmtx/Leros/backend/internal/projectfile"
	skilltoken "github.com/insmtx/Leros/backend/internal/skill"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	skillstore "github.com/insmtx/Leros/backend/internal/skill/store"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
)

// MessagePoster 无状态的消息投递器，负责消息创建、统计更新、事件发布、Worker 任务投递。
// 多个 goroutine 可安全并发使用。
type MessagePoster struct {
	db              *gorm.DB
	perm            *PermissionService
	eventbus        eventbus.EventBus
	inferrer        AssistantInferrer
	giteaClient     *gitea.Client
	giteaCfg        *config.GiteaConfig
	env             string
	userRepo        account.UserRepository
	orgRepo         account.OrgRepository
	dispatchEnabled bool
}

// ErrRunDispatchUnavailable is returned when this Server cannot accept Worker work.
var ErrRunDispatchUnavailable = errors.New("run dispatch unavailable")

// NewMessagePoster 创建 MessagePoster 实例。
func NewMessagePoster(db *gorm.DB, perm *PermissionService, eb eventbus.EventBus, inferrer AssistantInferrer, giteaClient *gitea.Client, giteaCfg *config.GiteaConfig, env string, userRepo account.UserRepository, orgRepo account.OrgRepository, dispatchEnabled ...bool) *MessagePoster {
	enabled := true
	if len(dispatchEnabled) > 0 {
		enabled = dispatchEnabled[0]
	}
	return &MessagePoster{
		db:              db,
		perm:            perm,
		eventbus:        eb,
		inferrer:        inferrer,
		giteaClient:     giteaClient,
		giteaCfg:        giteaCfg,
		env:             env,
		userRepo:        userRepo,
		orgRepo:         orgRepo,
		dispatchEnabled: enabled,
	}
}

func (p *MessagePoster) resolveSenderNameFromCaller(ctx context.Context, database *gorm.DB, caller *types.Caller) string {
	if caller == nil || caller.Uin == 0 {
		return ""
	}

	var name string
	if caller.OrgID > 0 {
		if p.orgRepo != nil {
			orgMember, err := p.orgRepo.GetOrgMember(ctx, 0, caller.Uin)
			if err == nil && orgMember != nil && strings.TrimSpace(orgMember.UserName) != "" {
				return orgMember.UserName
			}
		}
		var relation types.UserOrg
		if err := database.WithContext(ctx).First(&relation, caller.Uin).Error; err != nil {
			return ""
		}
		var user types.User
		if err := database.WithContext(ctx).First(&user, relation.UserID).Error; err != nil {
			return ""
		}
		name = user.Name
	} else {
		if p.userRepo != nil {
			user, err := p.userRepo.GetUserByUin(ctx, caller.Uin)
			if err == nil && user != nil {
				return user.Name
			}
		}
		var user types.User
		if err := database.WithContext(ctx).First(&user, caller.Uin).Error; err != nil {
			return ""
		}
		name = user.Name
	}
	return name
}

// MessageRoutingOverride 消息级别的路由覆盖，用于在同个 session 内将消息发给不同的 assistant/worker。
// 决定消息发往哪个 worker 的唯一依据。RunCommandFactory 要求 routing 必须非 nil 且 WorkerID > 0。
type MessageRoutingOverride struct {
	AssistantID uint
	WorkerID    uint
}

// MessageExecutionOptions supplies source-independent execution policy for one persisted user message.
type MessageExecutionOptions struct {
	// QueueDeadline is the latest time the Worker may start this message.
	QueueDeadline *time.Time
	// Policy contains run-scoped execution restrictions supplied by the caller.
	Policy messaging.TaskPolicy
}

// runCommandID 决定运行命令的稳定 ID。所有消息入口都使用相同的 session+sequence 规则。
func runCommandID(session *types.Session, message *types.SessionMessage) string {
	return fmt.Sprintf("msg_%d_%d", session.ID, message.Sequence)
}

// runNotAfter 将调用方提供的 not_after 转为 RFC3339 字符串写入命令 payload。
func runNotAfter(opts *MessageExecutionOptions) string {
	if opts != nil && opts.QueueDeadline != nil {
		return opts.QueueDeadline.UTC().Format(time.RFC3339)
	}
	return ""
}

// PostMessage 在已有 session 上创建一条消息并完成后续投递（统计、EventBus、WorkerTask）。
// routing 为 nil 时使用 session 级别的 assistant/worker（默认行为）。
func (p *MessagePoster) PostMessage(
	ctx context.Context,
	session *types.Session,
	executionMode types.ExecutionMode,
	buildMessage func(sequence int64) *types.SessionMessage,
	routing *MessageRoutingOverride,
	postOpts ...*MessageExecutionOptions,
) (*types.SessionMessage, error) {
	if !p.dispatchEnabled {
		return nil, ErrRunDispatchUnavailable
	}
	var opts *MessageExecutionOptions
	if len(postOpts) > 0 && postOpts[0] != nil {
		opts = postOpts[0]
	}
	var message *types.SessionMessage
	var queued bool
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sequence, err := infradb.GetNextSequence(ctx, tx, session.ID)
		if err != nil {
			return err
		}
		message = buildMessage(sequence)
		message.SessionID = session.ID
		if message.MessageType == "" {
			message.MessageType = string(types.MessageTypeText)
		}

		// 群聊模式下为用户消息填充发送者身份（AI 队友回复由 CompleteSessionMessage 填充）
		if message.Role == string(types.MessageRoleUser) && message.SenderUin == nil {
			if caller, _ := auth.FromContext(ctx); caller != nil && caller.Uin > 0 {
				uid := caller.Uin
				message.SenderUin = &uid
				// 中文注释：事务内回读发送者必须复用 tx，避免单连接 SQLite 和真实数据库连接池发生自锁。
				message.SenderName = p.resolveSenderNameFromCaller(ctx, tx, caller)
			}
		}

		if err := infradb.CreateMessage(ctx, tx, message); err != nil {
			return fmt.Errorf("create message: %w", err)
		}
		now := time.Now().UTC()
		if err := infradb.IncrementMessageCount(ctx, tx, session.ID); err != nil {
			return err
		}
		if err := infradb.UpdateLastMessageAt(ctx, tx, session.ID, now); err != nil {
			return err
		}

		deadline := now.Add(30 * time.Minute)
		if opts != nil && opts.QueueDeadline != nil {
			deadline = opts.QueueDeadline.UTC()
		}
		// The transport record and Worker command must share the same start deadline:
		// publication can succeed while the Worker inbox is still waiting for a compute slot.
		effectiveOpts := &MessageExecutionOptions{QueueDeadline: &deadline}
		if opts != nil {
			effectiveOpts.Policy = opts.Policy
		}
		posterTx := *p
		posterTx.db = tx
		topic, command, err := posterTx.buildWorkerTask(ctx, session, message, executionMode, routing, effectiveOpts)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(command)
		if err != nil {
			return fmt.Errorf("marshal reliable task payload: %w", err)
		}
		if err := infradb.CreateReliableTask(ctx, tx, &types.ReliableTask{
			TaskID:        command.ID,
			Kind:          "worker.run",
			Destination:   topic,
			ContentType:   "application/json",
			Payload:       payload,
			SourceType:    "session_message",
			SourceID:      strconv.FormatUint(uint64(message.ID), 10),
			PartitionKey:  session.PublicID,
			Status:        types.ReliableTaskPending,
			NextAttemptAt: now,
			DeadlineAt:    deadline,
		}); err != nil {
			return fmt.Errorf("create reliable task for topic %s: %w", topic, err)
		}
		queued = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	logs.DebugContextf(ctx, "created message seq=%d in session=%s", message.Sequence, session.PublicID)

	logs.DebugContextf(ctx, "published message events for session=%s", session.PublicID)

	p.writeSkillInvokeResources(ctx, session, message)

	publishMessageCreatedEvent(ctx, p.db, p.eventbus, session, message)

	logs.InfoContextf(ctx, "published message.created (human): session_id=%s message_id=%d project_id=%v",
		session.PublicID, message.ID, session.ProjectID)

	if queued {
		logs.InfoContextf(ctx, "queued reliable task: session_id=%s message_id=%d", session.PublicID, message.ID)
	}
	return message, nil
}

// RunNewMessage 执行 NewMessage 完整编排：Project → Task → Session → Message 原子创建链。
func (p *MessagePoster) RunNewMessage(
	ctx context.Context,
	req *contract.NewMessageRequest,
	caller *types.Caller,
) (*contract.NewMessageResponse, error) {
	o := &newMessageOrchestrator{
		poster: p,
		ctx:    ctx,
		req:    req,
		caller: caller,
	}

	assistantIDs, err := resolveAssistantIDsByPublicID(ctx, p.db, caller.OrgID, req.AssistantIDs)
	if err != nil {
		return nil, err
	}
	o.assistantIDs = assistantIDs

	logs.DebugContextf(ctx, "NewMessage: caller=%d org=%d assistant=%v", caller.Uin, caller.OrgID, assistantIDs)

	if err := o.resolveOrCreateProject(); err != nil {
		logs.ErrorContextf(ctx, "NewMessage resolveOrCreateProject failed: %v", err)
		return nil, err
	}
	if skilltoken.HasInvokedSkills(req.Content) {
		result := bindInvokedSkillsToProject(
			ctx,
			p.db,
			o.project,
			o.caller,
			req.Content,
			func(c context.Context, tx *gorm.DB, caller *types.Caller, projectPublicID string, action types.ProjectActivityAction, payload types.ProjectActivityPayload) error {
				return recordUserRepoActivity(c, tx, p.userRepo, caller.Uin, projectPublicID, action, payload)
			},
		)
		logInvokedSkillBindingResult(ctx, o.project, result)
	}
	// 中文注释：先将请求携带的连接器关联到项目，再创建 Session/Task，保证 Worker 发布前绑定已落地。
	if len(o.req.ConnectorIDs) > 0 {
		if _, err := bindConnectorsToProject(
			ctx,
			p.db,
			p.perm,
			o.caller,
			o.project,
			o.req.ConnectorIDs,
			func(c context.Context, tx *gorm.DB, caller *types.Caller, projectPublicID string, action types.ProjectActivityAction, payload types.ProjectActivityPayload) error {
				return recordUserRepoActivity(ctx, tx, p.userRepo, caller.Uin, projectPublicID, action, payload)
			},
		); err != nil {
			logs.ErrorContextf(ctx, "NewMessage bind connectors failed: %v", err)
			return nil, fmt.Errorf("bind connectors to project: %w", err)
		}
	}
	if err := o.ensureProjectSession(); err != nil {
		logs.ErrorContextf(ctx, "NewMessage ensureProjectSession failed: %v", err)
		return nil, err
	}
	if err := o.resolveOrCreateTask(); err != nil {
		logs.ErrorContextf(ctx, "NewMessage resolveOrCreateTask failed: %v", err)
		return nil, err
	}
	if err := o.createTaskSession(); err != nil {
		logs.ErrorContextf(ctx, "NewMessage createTaskSession failed: %v", err)
		return nil, err
	}
	// 中文注释：附件必须在 Task 创建后再绑定，才能写入 uploads/_task/{taskPublicID}/ 隔离路径，避免跨任务同名文件夹冲突。
	if len(req.Attachments) > 0 {
		if err := attachFilesToProject(ctx, p.db, caller.OrgID, caller.Uin, &o.task.ID, o.project, req.Attachments); err != nil {
			return nil, fmt.Errorf("attach files to project: %w", err)
		}
	}

	// 先补齐附件的可访问 URL，再把附件写入用户消息，避免前端回显和后续上下文拿不到附件信息。
	resolveAttachmentURLs(ctx, p.db, caller.OrgID, req.Attachments)

	// 中文注释：content 为空时表示"召唤队友落地空对话"——仅创建 Project/Task/Session + 分配 worker，不发首条消息。
	// 后续用户在任务详情页发送的消息走 AddMessage 路径，persona 通过统一命令工厂自动注入。
	var messageID string
	if strings.TrimSpace(req.Content) != "" || len(req.Attachments) > 0 {
		message, err := p.PostMessage(ctx, o.taskSession, req.ExecutionMode, func(sequence int64) *types.SessionMessage {
			msgType := req.MessageType
			if msgType == "" {
				msgType = string(types.MessageTypeText)
			}
			msg := &types.SessionMessage{
				Role:        string(types.MessageRoleUser),
				Content:     req.Content,
				MessageType: msgType,
				Attachments: req.Attachments,
				Status:      string(types.MessageStatusPending),
				Sequence:    sequence,
				Timestamp:   time.Now().UnixMilli(),
			}
			if req.Metadata != nil {
				msg.Metadata = *req.Metadata
			}
			if scene := strings.TrimSpace(req.Scene); scene != "" {
				msg.Metadata.Scene = scene
			}
			if outputFormat := strings.TrimSpace(req.OutputFormat); outputFormat != "" {
				msg.Metadata.OutputFormat = outputFormat
			}
			if o.taskRoute != nil {
				msg.AssistantID = o.taskRoute.AssistantID
			}
			return msg
		}, o.taskRoute)
		if err != nil {
			logs.ErrorContextf(ctx, "NewMessage PostMessage failed: %v", err)
			return nil, err
		}
		messageID = fmt.Sprintf("%d", message.ID)
	} else {
		logs.InfoContextf(ctx, "NewMessage empty summon: project=%s task=%s session=%s (no first message)",
			o.project.PublicID, o.task.PublicID, o.taskSession.PublicID)
	}
	// 中文注释：项目页里通过 NewMessage 创建任务/首条消息后，要立即刷新项目活跃时间，供左侧列表排序使用。
	if err := infradb.TouchProjectUpdatedAt(ctx, p.db, o.project.ID, time.Now()); err != nil {
		logs.WarnContextf(ctx, "NewMessage touch project updated_at failed: %v", err)
	}

	assistantID := assistantIDToPublicID(o.ctx, o.poster.db, o.taskRoute.AssistantID)
	logs.InfoContextf(ctx, "NewMessage completed: project=%s task=%s session=%s message=%s assistant=%s",
		o.project.PublicID, o.task.PublicID, o.taskSession.PublicID, messageID, assistantID)

	return &contract.NewMessageResponse{
		ProjectID:   o.project.PublicID,
		TaskID:      o.task.PublicID,
		SessionID:   o.taskSession.PublicID,
		MessageID:   messageID,
		AssistantID: assistantID,
	}, nil
}

// newMessageOrchestrator 持有 NewMessage 编排过程中的临时状态。
// 仅在 RunNewMessage 调用期间存续，不可复用。
type newMessageOrchestrator struct {
	poster *MessagePoster
	ctx    context.Context
	req    *contract.NewMessageRequest
	caller *types.Caller

	project      *types.Project
	task         *types.Task
	taskSession  *types.Session
	assistantIDs []uint
	taskRoute    *MessageRoutingOverride
}

func (o *newMessageOrchestrator) resolveOrCreateProject() error {
	if o.req.ProjectID != "" {
		proj, err := infradb.GetProjectByPublicID(o.ctx, o.poster.db, o.caller.OrgID, o.req.ProjectID)
		if err != nil {
			return err
		}
		if proj == nil {
			return errors.New("project not found")
		}
		o.project = proj
		return nil
	}

	title := o.defaultTitle("新的队友对话")
	runes := []rune(skilltoken.DisplayText(strings.TrimSpace(o.req.Content)))
	if len(runes) > 0 && len(runes) <= 50 {
		title = string(runes)
	} else if len(runes) > 50 {
		title = string(runes[:50])
	}

	projectID := fmt.Sprintf("prj_%s", snowflake.GenerateIDBase58())
	o.project = &types.Project{
		PublicID:           projectID,
		OrgID:              o.caller.OrgID,
		OwnerID:            o.caller.Uin,
		Name:               title,
		Description:        "",
		Objective:          strings.TrimSpace(o.req.Objective),
		Status:             string(types.ProjectStatusActive),
		GiteaDefaultBranch: "main",
	}

	repoName := o.poster.buildRepoName(o.caller.OrgID, projectID)
	if o.poster.giteaClient != nil && o.poster.giteaCfg != nil && o.poster.giteaCfg.Enabled {
		repoInfo, err := git.CreateRepoWithRetry(o.ctx, o.poster.giteaClient, gitea.CreateRepoOption{
			Name:        repoName,
			Description: "",
			Private:     true,
			AutoInit:    false,
		})
		if err != nil {
			return fmt.Errorf("create gitea repo: %w", err)
		}
		if repoInfo == nil || repoInfo.FullName == "" {
			return fmt.Errorf("create gitea repo: incomplete response (project=%s repo=%s)", projectID, repoName)
		}
		o.project.GiteaRepoFullName = repoInfo.FullName
		o.project.GiteaRepoID = repoInfo.ID
	}
	if err := infradb.CreateProject(o.ctx, o.poster.db, o.project); err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	if err := o.recordProjectCreatedActivity(); err != nil {
		return err
	}

	if o.project.GiteaRepoFullName != "" {
		if err := git.InitRepoStructure(o.ctx, o.poster.giteaClient, o.project.GiteaRepoFullName); err != nil {
			logs.WarnContextf(o.ctx, "[message_poster] init repo structure: %v", err)
		}
		logs.InfoContextf(o.ctx, "created project=%s org=%d user=%d repo=%s", projectID, o.caller.OrgID, o.caller.Uin, o.project.GiteaRepoFullName)
	} else {
		logs.InfoContextf(o.ctx, "created project=%s org=%d user=%d (no gitea)", projectID, o.caller.OrgID, o.caller.Uin)
	}

	// 创建者的成员身份与权限来源统一为 leros_resource_binding：先建项目资源，再建 owner 绑定。
	resource := &types.Resource{
		OrgID: o.caller.OrgID,
		Uin:   o.caller.Uin,
		Type:  types.ResourceTypeProject,
		BizID: o.project.ID,
	}
	if err := infradb.CreateResource(o.ctx, o.poster.db, resource); err != nil {
		return fmt.Errorf("sync project resource: %w", err)
	}
	ownerUin := o.caller.Uin
	if err := infradb.CreateResourceBinding(o.ctx, o.poster.db, &types.ResourceBinding{
		OrgID:      o.caller.OrgID,
		Uin:        &ownerUin,
		ResourceID: resource.ID,
		Role:       types.ResourceRoleOwner,
	}); err != nil {
		return fmt.Errorf("bind project owner: %w", err)
	}

	if err := o.bindProjectAssistants(resource.ID); err != nil {
		return err
	}

	return nil
}

func (o *newMessageOrchestrator) recordProjectCreatedActivity() error {
	operatorID := ""
	if o.poster.userRepo != nil {
		if user, err := o.poster.userRepo.GetUserByUin(o.ctx, o.caller.Uin); err == nil && user != nil {
			operatorID = user.PublicID
		}
	}
	if operatorID == "" {
		return fmt.Errorf("resolve project activity operator: user %d not found", o.caller.Uin)
	}
	return infradb.CreateProjectActivity(o.ctx, o.poster.db, &types.ProjectActivity{
		ProjectID:  o.project.PublicID,
		OperatorID: operatorID,
		ActionType: types.ProjectActivityActionProjectCreated,
		Payload:    normalizeProjectActivityPayload(types.ProjectActivityPayload{}),
		Version:    1,
		CreatedAt:  time.Now(),
	})
}

func (o *newMessageOrchestrator) bindProjectAssistants(resourceID uint) error {
	defaultAssistantID, err := infradb.GetDefaultAssistantIDByOrg(o.ctx, o.poster.db, o.caller.OrgID)
	if err != nil {
		return fmt.Errorf("get default assistant: %w", err)
	}
	if defaultAssistantID == 0 {
		return ErrNoDefaultAssistantInOrg
	}
	boundID := defaultAssistantID
	if err := infradb.CreateResourceBinding(o.ctx, o.poster.db, &types.ResourceBinding{
		OrgID:       o.caller.OrgID,
		AssistantID: &boundID,
		ResourceID:  resourceID,
		Role:        types.ResourceRoleMember,
	}); err != nil {
		return fmt.Errorf("bind default project assistant %d: %w", defaultAssistantID, err)
	}

	for _, id := range o.assistantIDs {
		if id == 0 || id == defaultAssistantID {
			continue
		}
		extraID := id
		if err := infradb.CreateResourceBinding(o.ctx, o.poster.db, &types.ResourceBinding{
			OrgID:       o.caller.OrgID,
			AssistantID: &extraID,
			ResourceID:  resourceID,
			Role:        types.ResourceRoleMember,
		}); err != nil {
			return fmt.Errorf("bind project assistant %d: %w", id, err)
		}
	}
	return nil
}

func (o *newMessageOrchestrator) ensureProjectSession() error {
	projectSession, err := infradb.GetProjectSession(o.ctx, o.poster.db, o.project.ID)
	if err != nil {
		return fmt.Errorf("get project session: %w", err)
	}
	if projectSession != nil {
		return nil
	}

	_, _, err = resolveProjectAssistantWorker(o.ctx, o.poster.db, o.caller.OrgID, o.project.ID, o.assistantIDs, o.poster.inferrer)
	if err != nil {
		return err
	}
	projectSessionID := fmt.Sprintf("sess_%s", snowflake.GenerateIDBase58())
	projectSession = &types.Session{
		PublicID:  projectSessionID,
		Type:      types.SessionTypeProject,
		Uin:       o.caller.Uin,
		OrgID:     o.caller.OrgID,
		ProjectID: &o.project.ID,
		Status:    string(types.SessionStatusActive),
		Title:     "项目协作",
	}
	if err := infradb.CreateSession(o.ctx, o.poster.db, projectSession); err != nil {
		return fmt.Errorf("create project session: %w", err)
	}

	logs.InfoContextf(o.ctx, "created project session=%s for project=%s", projectSessionID, o.project.PublicID)
	return nil
}

func (o *newMessageOrchestrator) resolveOrCreateTask() error {
	if o.req.TaskID != "" {
		t, err := infradb.GetTaskByPublicID(o.ctx, o.poster.db, o.caller.OrgID, o.req.TaskID)
		if err != nil {
			return err
		}
		if t == nil {
			return errors.New("task not found")
		}
		o.task = t
		return nil
	}

	taskTitle := o.defaultTitle("新的队友任务")
	runes := []rune(skilltoken.DisplayText(strings.TrimSpace(o.req.Content)))
	if len(runes) > 0 && len(runes) <= 50 {
		taskTitle = string(runes)
	} else if len(runes) > 50 {
		taskTitle = string(runes[:50])
	}

	taskID := fmt.Sprintf("task_%s", snowflake.GenerateIDBase58())
	o.task = &types.Task{
		PublicID:    taskID,
		OrgID:       o.caller.OrgID,
		OwnerID:     o.caller.Uin,
		ProjectID:   o.project.ID,
		TaskType:    types.TaskTypeGeneral,
		Title:       taskTitle,
		Description: o.req.Content,
		Status:      string(types.TaskStatusCreated),
	}
	if err := infradb.CreateTask(o.ctx, o.poster.db, o.task); err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	if err := syncTaskResource(o.ctx, o.poster.db, o.caller.OrgID, o.project.ID, o.task.ID, o.caller.Uin); err != nil {
		return fmt.Errorf("sync task resource: %w", err)
	}

	logs.InfoContextf(o.ctx, "created task=%s in project=%s", taskID, o.project.PublicID)
	return nil
}

func (o *newMessageOrchestrator) defaultTitle(fallback string) string {
	if o == nil || o.poster == nil || o.poster.db == nil || o.req == nil || len(o.assistantIDs) == 0 {
		return fallback
	}
	da, err := infradb.GetDigitalAssistantByID(o.ctx, o.poster.db, o.assistantIDs[0])
	if err != nil || da == nil || strings.TrimSpace(da.Name) == "" {
		return fallback
	}
	return fmt.Sprintf("与%s对话", strings.TrimSpace(da.Name))
}

func (o *newMessageOrchestrator) createTaskSession() error {
	assistantID, workerID, err := resolveProjectAssistantWorker(o.ctx, o.poster.db, o.caller.OrgID, o.project.ID, o.assistantIDs, o.poster.inferrer)
	if err != nil {
		return err
	}
	o.taskRoute = &MessageRoutingOverride{AssistantID: assistantID, WorkerID: workerID}
	taskSessionID := fmt.Sprintf("sess_%s", snowflake.GenerateIDBase58())
	o.taskSession = &types.Session{
		PublicID:  taskSessionID,
		Type:      types.SessionTypeTask,
		Uin:       o.caller.Uin,
		OrgID:     o.caller.OrgID,
		ProjectID: &o.project.ID,
		TaskID:    &o.task.ID,
		Status:    string(types.SessionStatusActive),
		Title:     o.task.Title,
	}
	if err := infradb.CreateSession(o.ctx, o.poster.db, o.taskSession); err != nil {
		return fmt.Errorf("create task session: %w", err)
	}

	o.task.SessionID = &o.taskSession.ID
	if err := o.poster.db.WithContext(o.ctx).Model(o.task).Update("session_id", o.taskSession.ID).Error; err != nil {
		logs.WarnContextf(o.ctx, "update task session_id failed: %v", err)
	}

	logs.InfoContextf(o.ctx, "created task session=%s for task=%s", taskSessionID, o.task.PublicID)
	return nil
}

func (p *MessagePoster) resolveRuntimeWorker(ctx context.Context, orgID, assistantID uint) (uint, uint, error) {
	if p == nil {
		return assistantID, assistantID, nil
	}
	return resolveRuntimeWorker(ctx, p.db, orgID, assistantID, p.inferrer)
}

type assistantEvolutionContext struct {
	promptBlockIDs  []string
	memoryIDs       []string
	promptExtension string
}

// buildExecutionTarget 根据 assistantID 构造 ExecutionTarget。
// 查询失败或 assistantID 为 0 时返回零值，不阻塞 run（降级为通用 lework 身份）。
func (p *MessagePoster) buildExecutionTarget(ctx context.Context, session *types.Session, assistantID uint, message *types.SessionMessage) messaging.ExecutionTarget {
	if p == nil || p.db == nil || assistantID == 0 {
		return messaging.ExecutionTarget{}
	}
	da, err := infradb.GetDigitalAssistantByID(ctx, p.db, assistantID)
	if err != nil || da == nil {
		logs.WarnContextf(ctx, "buildExecutionTarget: assistant %d not found, fallback to default identity: %v", assistantID, err)
		return messaging.ExecutionTarget{AssistantID: assistantID, AssistantPublicID: strconv.FormatUint(uint64(assistantID), 10)}
	}
	systemPrompt := da.SystemPrompt
	if message != nil {
		evolution, err := p.buildAssistantEvolutionContext(ctx, assistantID, skilltoken.DisplayText(message.Content))
		if err != nil {
			logs.WarnContextf(ctx, "buildExecutionTarget: assistant %d evolution context skipped: %v", assistantID, err)
		} else if evolution.promptExtension != "" {
			systemPrompt = strings.TrimSpace(strings.Join(filterEmptyStrings(systemPrompt, evolution.promptExtension), "\n\n"))
			p.writeAssistantPromptTrace(ctx, session, message, assistantID, systemPrompt, evolution)
		}
	}

	return messaging.ExecutionTarget{
		AssistantID:       da.ID,
		AssistantPublicID: da.PublicID,
		AssistantName:     da.Name,
		AssistantDesc:     da.Description,
		SystemPrompt:      systemPrompt,
	}
}

func (p *MessagePoster) buildAssistantEvolutionContext(ctx context.Context, assistantID uint, query string) (*assistantEvolutionContext, error) {
	blocks, err := infradb.ListDigitalAssistantPromptBlocks(ctx, p.db, assistantID, query, 6)
	if err != nil {
		return nil, err
	}
	memories, err := infradb.ListRelevantDigitalAssistantMemories(ctx, p.db, assistantID, query, 5)
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 && len(memories) == 0 {
		return &assistantEvolutionContext{}, nil
	}

	var sb strings.Builder
	sb.WriteString("<teammate_evolution_context>\n")
	sb.WriteString("以下内容来自该 AI 队友的分层提示词和长期记忆，用于增强当前回答；若与当前用户明确要求冲突，以用户要求和核心身份边界为准。\n")
	if len(blocks) > 0 {
		sb.WriteString("\n## 动态能力片段\n")
		for _, block := range blocks {
			if block == nil {
				continue
			}
			sb.WriteString("- [")
			sb.WriteString(block.BlockType)
			sb.WriteString("] ")
			if strings.TrimSpace(block.Title) != "" {
				sb.WriteString(block.Title)
				sb.WriteString(": ")
			}
			sb.WriteString(truncateEvolutionPromptText(block.Content, 1200))
			sb.WriteString("\n")
		}
	}
	if len(memories) > 0 {
		sb.WriteString("\n## 长期记忆\n")
		for _, memory := range memories {
			if memory == nil {
				continue
			}
			sb.WriteString("- [")
			sb.WriteString(memory.MemoryType)
			if memory.SourceType != "" {
				sb.WriteString("/")
				sb.WriteString(memory.SourceType)
			}
			sb.WriteString("] ")
			sb.WriteString(truncateEvolutionPromptText(memory.Content, 1000))
			sb.WriteString("\n")
		}
	}
	sb.WriteString("</teammate_evolution_context>")

	return &assistantEvolutionContext{
		promptBlockIDs:  promptBlockIDs(blocks),
		memoryIDs:       memoryIDs(memories),
		promptExtension: strings.TrimSpace(sb.String()),
	}, nil
}

func (p *MessagePoster) writeAssistantPromptTrace(ctx context.Context, session *types.Session, message *types.SessionMessage, assistantID uint, systemPrompt string, evolution *assistantEvolutionContext) {
	if p == nil || p.db == nil || session == nil || message == nil || evolution == nil {
		return
	}
	if len(evolution.promptBlockIDs) == 0 && len(evolution.memoryIDs) == 0 {
		return
	}
	hash := sha256.Sum256([]byte(systemPrompt))
	trace := &types.AssistantPromptTrace{
		SessionID:         session.ID,
		MessageID:         message.ID,
		AssistantID:       assistantID,
		CorePromptVersion: 1,
		InjectedBlockIDs:  types.SkillStringList(evolution.promptBlockIDs),
		InjectedMemoryIDs: types.SkillStringList(evolution.memoryIDs),
		PromptHash:        fmt.Sprintf("%x", hash[:]),
	}
	if err := infradb.CreateAssistantPromptTrace(ctx, p.db, trace); err != nil {
		logs.WarnContextf(ctx, "assistant prompt trace write failed: session=%d message=%d assistant=%d error=%v",
			session.ID, message.ID, assistantID, err)
	}
}

func promptBlockIDs(blocks []*types.DigitalAssistantPromptBlock) []string {
	ids := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block == nil || block.ID == 0 {
			continue
		}
		ids = append(ids, strconv.FormatUint(uint64(block.ID), 10))
	}
	return ids
}

func memoryIDs(memories []*types.DigitalAssistantMemory) []string {
	ids := make([]string, 0, len(memories))
	for _, memory := range memories {
		if memory == nil || memory.ID == 0 {
			continue
		}
		ids = append(ids, strconv.FormatUint(uint64(memory.ID), 10))
	}
	return ids
}

// buildProjectContext queries project business context and member list for the worker.
// assistantID 用于标记 IsCurrentExec。
// Returns zero value when session has no project (e.g. chat sessions) or query fails.
func (p *MessagePoster) buildProjectContext(ctx context.Context, session *types.Session, currentAssistantID uint) messaging.ProjectContext {
	if session == nil || session.ProjectID == nil || p == nil || p.db == nil {
		return messaging.ProjectContext{}
	}
	project, err := infradb.GetProjectByID(ctx, p.db, *session.ProjectID)
	if err != nil || project == nil {
		logs.WarnContextf(ctx, "buildProjectContext: project %d not found: %v", *session.ProjectID, err)
		return messaging.ProjectContext{}
	}
	ctx2 := messaging.ProjectContext{
		Name:        project.Name,
		Description: project.Description,
		Objective:   project.Objective,
	}
	resource, err := infradb.GetResourceByBizID(ctx, p.db, project.OrgID, types.ResourceTypeProject, project.ID)
	if err != nil || resource == nil {
		if err != nil {
			logs.WarnContextf(ctx, "buildProjectContext: get project resource failed: %v", err)
		}
		return ctx2
	}
	bindings, err := infradb.ListResourceBindingsByResourceID(ctx, p.db, resource.ID)
	if err != nil {
		logs.WarnContextf(ctx, "buildProjectContext: list bindings failed: %v", err)
		return ctx2
	}
	defaultAssistantID, _ := infradb.GetDefaultAssistantIDByOrg(ctx, p.db, project.OrgID)
	userIDs, assistantIDs := collectBindingMemberIDs(bindings)
	userMap := make(map[uint]string)
	if len(userIDs) > 0 && p.userRepo != nil {
		if users, err := p.userRepo.GetUsersByUins(ctx, userIDs); err == nil {
			for uin, user := range users {
				if user != nil {
					userMap[uin] = user.Name
				}
			}
		}
	}
	// The organization member directory is the canonical display-name source
	// for both project members and message senders. Keep the user repository as
	// a fallback for environments where the directory is unavailable.
	if len(userIDs) > 0 && p.orgRepo != nil {
		for _, uin := range userIDs {
			member, err := p.orgRepo.GetOrgMember(ctx, 0, uin)
			if err == nil && member != nil && strings.TrimSpace(member.UserName) != "" {
				userMap[uin] = member.UserName
			}
		}
	}
	assistantMap := make(map[uint]string)
	if len(assistantIDs) > 0 {
		if assistants, err := infradb.GetAssistantsByIDs(ctx, p.db, assistantIDs); err == nil {
			for _, a := range assistants {
				if a != nil {
					assistantMap[a.ID] = a.Name
				}
			}
		}
	}
	briefs := make([]messaging.MemberBrief, 0, len(bindings))
	for _, b := range bindings {
		if b == nil {
			continue
		}
		if b.AssistantID != nil && *b.AssistantID != 0 {
			assistantID := *b.AssistantID
			brief := messaging.MemberBrief{
				MemberID:   assistantID,
				MemberType: string(types.MemberTypeAssistant),
				MemberRole: string(b.Role),
				IsDefault:  defaultAssistantID > 0 && assistantID == defaultAssistantID,
				Name:       assistantMap[assistantID],
			}
			if assistantID == currentAssistantID {
				brief.IsCurrentExec = true
			}
			briefs = append(briefs, brief)
			continue
		}
		if b.Uin == nil || *b.Uin == 0 {
			continue
		}
		uin := *b.Uin
		brief := messaging.MemberBrief{
			MemberID:   uin,
			MemberType: string(types.MemberTypeUser),
			MemberRole: string(b.Role),
			Name:       userMap[uin],
		}
		if uin == session.Uin {
			brief.IsCurrentUser = true
		}
		briefs = append(briefs, brief)
	}
	ctx2.Members = briefs
	return ctx2
}

func collectBindingMemberIDs(bindings []*types.ResourceBinding) (userIDs, assistantIDs []uint) {
	for _, b := range bindings {
		if b == nil {
			continue
		}
		if b.Uin != nil && *b.Uin != 0 {
			userIDs = append(userIDs, *b.Uin)
			continue
		}
		if b.AssistantID != nil && *b.AssistantID != 0 {
			assistantIDs = append(assistantIDs, *b.AssistantID)
		}
	}
	return
}

func truncateEvolutionPromptText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "...(truncated)"
}

func filterEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// buildWorkerTask constructs a stable command without publishing it, so the Server can persist it atomically.
func (p *MessagePoster) buildWorkerTask(
	ctx context.Context,
	session *types.Session,
	message *types.SessionMessage,
	executionMode types.ExecutionMode,
	routing *MessageRoutingOverride,
	postOpts ...*MessageExecutionOptions,
) (string, messaging.WorkerCommand, error) {
	var opts *MessageExecutionOptions
	if len(postOpts) > 0 && postOpts[0] != nil {
		opts = postOpts[0]
	}
	policy := messaging.TaskPolicy{}
	if opts != nil {
		policy = opts.Policy
	}
	caller, _ := auth.FromContext(ctx)
	orgID := session.OrgID
	if orgID == 0 && caller != nil {
		orgID = caller.OrgID
	}

	if routing == nil || routing.WorkerID == 0 {
		return "", messaging.WorkerCommand{}, fmt.Errorf("no assistant routing provided for worker task: session %s", session.PublicID)
	}
	effectiveAssistantID := routing.AssistantID
	effectiveWorkerID := routing.WorkerID

	topic, err := messaging.WorkerCommandSubject(orgID, effectiveWorkerID, messaging.LaneRun)
	if err != nil {
		return "", messaging.WorkerCommand{}, fmt.Errorf("failed to construct worker command topic: %w", err)
	}

	projectPublicID, taskPublicID, err := p.resolveWorkspaceIDs(ctx, session)
	if err != nil {
		return "", messaging.WorkerCommand{}, err
	}
	if taskPublicID == "" {
		taskPublicID = fmt.Sprintf("task_%d", message.ID)
	}
	requestID := fmt.Sprintf("req_%d", message.ID)
	modelOptions, err := p.resolveWorkerTaskModel(ctx, orgID)
	if err != nil {
		return "", messaging.WorkerCommand{}, err
	}

	var inputMessages []messaging.ChatMessage
	// 群聊历史上下文注入：以当前 AI 队友上一条回复为起点，增量获取时间窗口内的 user/assistant 消息。
	// 用 effectiveAssistantID（DigitalAssistant PK）查历史回复，而非 session.AllocatedAssistantID
	// （WorkerID），避免 AssistantID 与 WorkerID 错位导致全量历史注入。
	if session.Type == types.SessionTypeTask || session.Type == types.SessionTypeProject {
		lastTime, err := infradb.GetLastAssistantMessageCreatedAt(ctx, p.db, session.ID, effectiveAssistantID)
		if err != nil {
			logs.WarnContextf(ctx, "buildWorkerTask: get last assistant message time: %v", err)
		}
		if lastTime != nil {
			incremental, err := infradb.GetSessionMessagesInRange(ctx, p.db, session.ID, *lastTime)
			if err != nil {
				logs.WarnContextf(ctx, "buildWorkerTask: get session messages in range: %v", err)
			} else {
				seen := make(map[uint]bool)
				for _, hm := range incremental {
					if hm.ID == message.ID {
						continue
					}
					if hm.Role != string(types.MessageRoleUser) && hm.Role != string(types.MessageRoleAssistant) {
						continue
					}
					if seen[hm.ID] {
						continue
					}
					seen[hm.ID] = true
					inputMessages = append(inputMessages, messaging.ChatMessage{
						ID:           fmt.Sprintf("%d", hm.ID),
						Role:         messaging.MessageRole(hm.Role),
						Content:      hm.Content,
						SenderUserID: hm.SenderUin,
						SenderName:   hm.SenderName,
					})
				}
			}
		}
		// 该 AI 队友在 session 中无历史消息，不查询历史记录，inputMessages 保持空
	}
	// 当前新消息追加末尾，携带发言者身份
	inputMessages = append(inputMessages, messaging.ChatMessage{
		ID:           fmt.Sprintf("%d", message.ID),
		Role:         messaging.MessageRoleUser,
		Content:      message.Content,
		SenderUserID: message.SenderUin,
		SenderName:   message.SenderName,
	})
	executionTarget := p.buildExecutionTarget(ctx, session, effectiveAssistantID, message)
	projectContext := p.buildProjectContext(ctx, session, effectiveAssistantID)
	projectID := coalesceUintPtr(session.ProjectID)
	disableProjectMCP := p.shouldDisableProjectMCP(ctx, orgID, projectID)
	pluginSnapshots, err := p.resolveProjectPluginSnapshots(ctx, orgID, projectID, disableProjectMCP)
	if err != nil {
		return "", messaging.WorkerCommand{}, fmt.Errorf("resolve project plugin snapshots: %w", err)
	}

	cmd := withRequestTrace(ctx, messaging.NewRunCommand(
		runCommandID(session, message),
		messaging.RouteContext{
			OrgID:             orgID,
			SessionID:         session.PublicID,
			WorkerID:          effectiveWorkerID,
			WorkerPublicID:    workerIDToPublicID(ctx, p.db, orgID, effectiveWorkerID),
			AssistantID:       effectiveAssistantID,
			AssistantPublicID: assistantIDToPublicID(ctx, p.db, effectiveAssistantID),
			ClientIP:          llm.GetCtxString(ctx, llm.CtxClientIP),
		},
		messaging.TraceContext{
			TraceID:   session.PublicID,
			RequestID: requestID,
			TaskID:    taskPublicID,
			RunID:     requestID,
		},
		messaging.RunCommandPayload{
			TaskType:      messaging.TaskTypeAgentRun,
			ExecutionMode: string(normalizeExecutionMode(executionMode)),
			Policy:        policy,
			Actor: messaging.ActorContext{
				UserID:      fmt.Sprintf("%d", session.Uin),
				DisplayName: "",
				Channel:     "session",
			},
			Workspace: messaging.WorkspaceOptions{
				ProjectID: projectPublicID,
				TaskID:    taskPublicID,
			},
			Input: messaging.TaskInput{
				Type:         messaging.InputTypeMessage,
				Scene:        strings.TrimSpace(message.Metadata.Scene),
				OutputFormat: strings.TrimSpace(message.Metadata.OutputFormat),
				Messages:     inputMessages,
				Attachments:  convertMessageToMessagingAttachments(message.Attachments),
			},
			Model:             modelOptions,
			Execution:         executionTarget,
			Project:           projectContext,
			Plugins:           pluginSnapshots,
			ProjectID:         coalesceUintPtr(session.ProjectID),
			SessionID:         session.ID,
			MessageID:         message.ID,
			AssistantID:       routing.AssistantID,
			AssistantPublicID: assistantIDToPublicID(ctx, p.db, routing.AssistantID),
			Uin:               session.Uin,
			NotAfter:          runNotAfter(opts),
		},
		&messaging.RunCommandMetadata{
			SessionID:   session.PublicID,
			MessageType: message.MessageType,
			Sequence:    message.Sequence,
		},
	))

	return topic, cmd, nil
}

func (p *MessagePoster) shouldDisableProjectMCP(ctx context.Context, orgID, projectID uint) bool {
	if projectID == 0 {
		return false
	}
	resource, err := infradb.GetResourceByBizID(ctx, p.db, orgID, types.ResourceTypeProject, projectID)
	if err != nil {
		logs.WarnContextf(
			ctx,
			"get project resource for MCP collaboration policy failed; MCP disabled: project_id=%d error=%v",
			projectID,
			err,
		)
		return true
	}
	if resource == nil {
		logs.WarnContextf(
			ctx,
			"project resource for MCP collaboration policy not found; MCP disabled: project_id=%d",
			projectID,
		)
		return true
	}
	humanCount, err := infradb.CountResourceUserBindings(ctx, p.db, resource.ID)
	if err != nil {
		logs.WarnContextf(
			ctx,
			"count project human members for MCP collaboration policy failed; MCP disabled: project_id=%d error=%v",
			projectID,
			err,
		)
		return true
	}
	return humanCount >= 2
}

func (p *MessagePoster) resolveProjectPluginSnapshots(
	ctx context.Context,
	orgID, projectID uint,
	disableMCP bool,
) ([]messaging.PluginSnapshot, error) {
	if projectID == 0 {
		return nil, nil
	}
	rows, err := infradb.ListProjectPluginSnapshots(ctx, p.db, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if disableMCP {
		filtered := rows[:0]
		for _, row := range rows {
			if strings.EqualFold(row.Kind, "mcp") {
				continue
			}
			filtered = append(filtered, row)
		}
		rows = filtered
	}
	oauthService := &pluginService{db: p.db, oauth: newConnectorOAuthManager()}
	refreshUsable := make(map[string]bool)
	refreshChanged := false
	for _, row := range rows {
		if !strings.EqualFold(row.Kind, "mcp") {
			continue
		}
		connector, connectorErr := ConnectorFromDefinition(row.Definition)
		if connectorErr != nil || connector == nil || connector.Auth.OAuth == nil {
			continue
		}
		usable, changed, refreshErr := oauthService.refreshMCPPlatformOAuth(ctx, orgID, row.PluginPublicID)
		refreshUsable[row.PluginPublicID] = usable
		refreshChanged = refreshChanged || changed
		if refreshErr != nil {
			logs.WarnContextf(
				ctx,
				"OAuth connector refresh failed; usable=%t plugin_id=%s code=%s error=%v",
				usable,
				row.PluginPublicID,
				row.Code,
				refreshErr,
			)
		}
	}
	if refreshChanged {
		rows, err = infradb.ListProjectPluginSnapshots(ctx, p.db, orgID, projectID)
		if err != nil {
			return nil, err
		}
	}
	result := make([]messaging.PluginSnapshot, 0, len(rows))
	for _, row := range rows {
		connector, connectorErr := ConnectorFromDefinition(row.Definition)
		if connectorErr == nil && connector != nil && connector.Auth.OAuth != nil {
			usable, checked := refreshUsable[row.PluginPublicID]
			if connector.Auth.OAuth.Status != ConnectorOAuthActive || (checked && !usable) {
				logs.WarnContextf(
					ctx,
					"skip unavailable OAuth connector snapshot: plugin_id=%s code=%s status=%s",
					row.PluginPublicID,
					row.Code,
					connector.Auth.OAuth.Status,
				)
				continue
			}
		}
		if err := ValidatePluginDefinition(row.Kind, row.Definition); err != nil {
			return nil, fmt.Errorf("plugin %s revision %d: %w", row.PluginPublicID, row.Revision, err)
		}
		result = append(result, messaging.PluginSnapshot{PluginID: row.PluginPublicID, Code: row.Code, Kind: row.Kind, Revision: row.Revision, Definition: append([]byte(nil), row.Definition...)})
	}
	return result, nil
}

func normalizeExecutionMode(mode types.ExecutionMode) types.ExecutionMode {
	if mode == types.ExecutionModePlan {
		return types.ExecutionModePlan
	}
	return types.ExecutionModeDefault
}

func (p *MessagePoster) resolveWorkerTaskModel(ctx context.Context, orgID uint) (messaging.ModelOptions, error) {
	if p == nil || p.db == nil {
		return messaging.ModelOptions{}, errors.New("database is required to resolve worker task llm model")
	}
	model, err := llm.ResolveDefaultLLMModel(ctx, p.db, orgID)
	if err != nil {
		return messaging.ModelOptions{}, fmt.Errorf("get default llm model: %w", err)
	}
	if model == nil {
		model, err = infradb.GetAnyActiveLLMModel(ctx, p.db, orgID)
		if err != nil {
			return messaging.ModelOptions{}, fmt.Errorf("get any active llm model: %w", err)
		}
	}
	if model == nil {
		return messaging.ModelOptions{}, errors.New("default llm model not found")
	}
	if strings.TrimSpace(model.Provider) == "" || strings.TrimSpace(model.ModelName) == "" || strings.TrimSpace(model.APIKeyEncrypted) == "" {
		return messaging.ModelOptions{}, errors.New("default llm model config is incomplete")
	}
	sampling := types.SamplingParamsFromConfig(model.Config)
	return messaging.ModelOptions{
		ModelID:          model.ID,
		Provider:         model.Provider,
		Model:            model.ModelName,
		BaseURL:          model.BaseURL,
		BaseURLHasV1:     model.BaseURLHasV1,
		APIKey:           model.APIKeyEncrypted,
		Vision:           llm.VisionFromConfig(model.Config),
		Temperature:      model.Temperature,
		MaxTokens:        model.MaxTokens,
		TopP:             sampling.TopP,
		FrequencyPenalty: sampling.FrequencyPenalty,
		PresencePenalty:  sampling.PresencePenalty,
		ContextLimit:     samplingLimitContext(sampling.Limit),
		OutputLimit:      samplingLimitOutput(sampling.Limit),
	}, nil
}

func samplingLimitContext(l *types.LLMLimitFields) int {
	if l == nil {
		return 0
	}
	return l.Context
}

func samplingLimitOutput(l *types.LLMLimitFields) int {
	if l == nil {
		return 0
	}
	return l.Output
}

func convertMessageToMessagingAttachments(attachments types.MessageAttachmentSlice) []messaging.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	result := make([]messaging.Attachment, 0, len(attachments))
	for _, a := range attachments {
		result = append(result, messaging.Attachment{
			ID:             a.FileUploadID,
			Name:           a.Name,
			MimeType:       a.MimeType,
			URL:            a.PublicURL,
			AttachmentRole: strings.TrimSpace(a.AttachmentRole),
		})
	}
	return result
}

func resolveAttachmentURLs(
	ctx context.Context,
	db *gorm.DB,
	orgID uint,
	attachments []types.MessageAttachment,
) {
	if len(attachments) == 0 {
		return
	}
	for i := range attachments {
		if attachments[i].FileUploadID == "" {
			continue
		}
		fileUpload, err := infradb.GetFileUploadByPublicID(ctx, db, orgID, attachments[i].FileUploadID)
		if err != nil {
			logs.WarnContextf(ctx, "resolve attachment file %s: %v", attachments[i].FileUploadID, err)
			continue
		}
		if fileUpload == nil {
			logs.WarnContextf(ctx, "resolve attachment file %s: not found", attachments[i].FileUploadID)
			continue
		}
		publicURL, err := filestore.ResolvePublicURL(ctx, fileUpload.StorageURI)
		if err != nil {
			logs.WarnContextf(ctx, "resolve attachment public url for %s: %v", attachments[i].FileUploadID, err)
			continue
		}
		attachments[i].PublicURL = publicURL
	}
}

func attachFilesToProject(
	ctx context.Context,
	dbParam *gorm.DB,
	orgID uint,
	uin uint,
	taskID *uint,
	project *types.Project,
	attachments []types.MessageAttachment,
) error {
	if project == nil || project.ID == 0 || len(attachments) == 0 {
		return nil
	}

	var taskPublicID string
	if taskID != nil && *taskID != 0 {
		task, taskErr := infradb.GetTaskByID(ctx, dbParam, orgID, *taskID)
		if taskErr != nil {
			logs.WarnContextf(ctx, "attach files resolve task %d failed: %v", *taskID, taskErr)
		} else if task != nil {
			taskPublicID = task.PublicID
		}
	}

	publicIDs := make([]string, 0, len(attachments))
	for _, a := range attachments {
		if a.FileUploadID != "" {
			publicIDs = append(publicIDs, a.FileUploadID)
		}
	}
	if len(publicIDs) == 0 {
		return nil
	}

	uploads, err := infradb.GetFileUploadsByPublicIDs(ctx, dbParam, orgID, publicIDs)
	if err != nil {
		return fmt.Errorf("batch get file uploads: %w", err)
	}
	uploadByPublicID := make(map[string]*types.FileUpload, len(uploads))
	for i := range uploads {
		uploadByPublicID[uploads[i].PublicID] = &uploads[i]
	}

	projResource, rerr := infradb.GetResourceByBizID(ctx, dbParam, orgID, types.ResourceTypeProject, project.ID)
	if rerr != nil {
		return fmt.Errorf("get project resource: %w", rerr)
	}

	if err := dbParam.Transaction(func(tx *gorm.DB) error {
		var params []projectfile.BindUserUploadParams
		for i := range attachments {
			upload, ok := uploadByPublicID[attachments[i].FileUploadID]
			if !ok || upload == nil {
				continue
			}
			params = append(params, projectfile.BindUserUploadParams{
				OrgID:        orgID,
				ProjectID:    project.ID,
				TaskID:       taskID,
				TaskPublicID: taskPublicID,
				Uin:          uin,
				FileUpload:   upload,
				DisplayName:  attachments[i].Name,
				RelativePath: attachments[i].RelativePath,
			})
		}
		pfs, bindErr := projectfile.BindUserUploadsToProject(ctx, tx, params)
		if bindErr != nil {
			return bindErr
		}
		if projResource == nil {
			return nil
		}
		for _, pf := range pfs {
			fr := &types.Resource{
				OrgID:                 orgID,
				Uin:                   uin,
				Type:                  types.ResourceTypeFile,
				BizID:                 pf.ID,
				ParentResourceID:      &projResource.ID,
				ParentResourcePathIDs: types.ResourcePathIDs{projResource.ID},
			}
			if err := infradb.CreateResource(ctx, tx, fr); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("batch create project_file records (org=%d project=%d files=%d): %w",
			orgID, project.ID, len(publicIDs), err)
	}
	return nil
}

func (p *MessagePoster) resolveWorkspaceIDs(ctx context.Context, session *types.Session) (string, string, error) {
	var projectPublicID string
	var taskPublicID string
	if session.ProjectID != nil && *session.ProjectID > 0 {
		var project types.Project
		if err := p.db.WithContext(ctx).Select("public_id").First(&project, *session.ProjectID).Error; err != nil {
			return "", "", fmt.Errorf("resolve session project: %w", err)
		}
		projectPublicID = project.PublicID
	}
	if session.TaskID != nil && *session.TaskID > 0 {
		var task types.Task
		if err := p.db.WithContext(ctx).Select("public_id").First(&task, *session.TaskID).Error; err != nil {
			return "", "", fmt.Errorf("resolve session task: %w", err)
		}
		taskPublicID = task.PublicID
	}
	return projectPublicID, taskPublicID, nil
}

func (p *MessagePoster) buildRepoName(orgID uint, projectPublicID string) string {
	return fmt.Sprintf("%s-%d-%s", p.env, orgID, projectPublicID)
}

// writeSkillInvokeResources writes message_resource records from skill chips in content.
func (p *MessagePoster) writeSkillInvokeResources(ctx context.Context, session *types.Session, message *types.SessionMessage) {
	if p.db == nil || message == nil || session == nil {
		return
	}
	tokens := skilltoken.ParseTokensOnly(message.Content)
	if len(tokens) == 0 {
		return
	}
	entries := resolveSkillEntries(tokens)
	if len(entries) == 0 {
		return
	}
	records := make([]*types.MessageResource, 0, len(entries))
	for seq, name := range entries {
		source, skillID, resourceID := p.resolveSkillMarketplace(ctx, session.OrgID, name)
		records = append(records, &types.MessageResource{
			ResourceID:   resourceID,
			ResourceKey:  source + ":" + skillID,
			MessageID:    message.ID,
			SessionID:    session.ID,
			OrgID:        session.OrgID,
			Uin:          session.Uin,
			ResourceType: "skill",
			ResourceName: name,
			InvokeType:   "slash_command",
			Seq:          seq,
		})
	}
	if err := infradb.BatchCreateMessageResources(ctx, p.db, records); err != nil {
		logs.WarnContextf(ctx, "write skill invoke message_resource failed: count=%d error=%v", len(records), err)
	} else {
		logs.InfoContextf(ctx, "Skill invoke message_resource written: count=%d", len(records))
	}
}

// resolveSkillMarketplace looks up a skill by name in one organization's Plugin list.
// Returns (source, skill_id, resourceID). When no record is found, source and skillID
// fall back to the name itself and resourceID is empty.
func (p *MessagePoster) resolveSkillMarketplace(ctx context.Context, orgID uint, name string) (source, skillID, resourceID string) {
	var plugin types.Plugin
	if err := p.db.WithContext(ctx).
		Where(
			"owner_scope = ? AND org_id = ? AND code = ? AND kind = ? AND deleted_at IS NULL",
			types.OwnerScopeOrganization,
			orgID,
			name,
			"skill",
		).
		First(&plugin).Error; err == nil && plugin.ID != 0 {
		return "organization", plugin.Code, plugin.PublicID
	}
	// Fall back to local .skill-metadata file
	if meta := p.readLocalSkillMetadata(ctx, name); meta != nil {
		return meta.Source, meta.SkillID, ""
	}
	// Fall back to catalog Manifest.Metadata.Source
	if entry, err := skillcatalog.Get(name); err == nil && entry != nil {
		src := entry.Manifest.Metadata.Source
		if src != "" {
			return src, entry.Manifest.Name, ""
		}
	}
	return name, name, ""
}

func (p *MessagePoster) readLocalSkillMetadata(ctx context.Context, name string) *skillstore.SkillMetadata {
	skillsDir, err := leros.SkillsDir()
	if err != nil {
		return nil
	}
	m, err := skillstore.ReadSkillMetadata(filepath.Join(skillsDir, name))
	if err != nil {
		return nil
	}
	return m
}

// resolveSkillEntries normalizes and deduplicates parsed Skill tokens.
// Organization Skills need not exist in the worker's local catalog.
func resolveSkillEntries(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(tokens))
	result := make([]string, 0, len(tokens))
	for _, name := range tokens {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, name)
	}
	return result
}

// publishMessageCreatedEvent 在用户消息持久化成功后发布 message.created 事件到
// org.{org_id}.project.{project_id}.notify subject，通知群聊成员有新用户消息。
//
// 携带完整 content（前端可直接渲染）。纯 chat session（无 ProjectID）不发布事件。
// 发布失败仅记录日志，不影响主流程。
func publishMessageCreatedEvent(
	ctx context.Context,
	gdb *gorm.DB,
	eb eventbus.EventBus,
	session *types.Session,
	message *types.SessionMessage,
) {
	if session == nil || message == nil || eb == nil || gdb == nil {
		return
	}
	if session.ProjectID == nil || *session.ProjectID == 0 || session.OrgID == 0 {
		return
	}

	data := messaging.MessageCreatedData{
		SenderType: messaging.SenderTypeHuman,
		SenderUin:  message.SenderUin,
		SenderName: message.SenderName,
		RunID:      message.RunID,
		Content:    message.Content,
		MessageID:  message.ID,
		Sequence:   message.Sequence,

		MessageType: message.MessageType,
	}
	if len(message.Attachments) > 0 {
		data.Attachments = message.Attachments
	}
	if !message.Metadata.IsZero() {
		data.Metadata = &message.Metadata
	}

	rawData, err := json.Marshal(data)
	if err != nil {
		logs.WarnContextf(ctx, "publishMessageCreatedEvent: marshal data: %v", err)
		return
	}

	subject, err := messaging.ProjectNotifySubject(session.OrgID, *session.ProjectID)
	if err != nil {
		logs.WarnContextf(ctx, "publishMessageCreatedEvent: build subject: %v", err)
		return
	}

	payload := messaging.GlobalEventPayload{
		Type:      messaging.GlobalEventMessageCreated,
		ProjectID: *session.ProjectID,
		SessionID: session.PublicID,
		Timestamp: message.Timestamp,
		Data:      rawData,
	}

	if err := eb.Publish(ctx, subject, payload); err != nil {
		logs.WarnContextf(ctx, "publishMessageCreatedEvent: publish to %s: %v", subject, err)
	}
}

func coalesceUintPtr(p *uint) uint {
	if p == nil {
		return 0
	}
	return *p
}
