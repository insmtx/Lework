package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"code.gitea.io/sdk/gitea"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/internal/infra/git"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	skilltoken "github.com/insmtx/Leros/backend/internal/skill"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	skillstore "github.com/insmtx/Leros/backend/internal/skill/store"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
)

// TitleUpdater handles session title generation.
type TitleUpdater interface {
	HandleSessionTitleRequest(ctx context.Context, sessionPID string) error
}

// MessagePoster 无状态的消息投递器，负责消息创建、统计更新、事件发布、Worker 任务投递。
// 多个 goroutine 可安全并发使用。
type MessagePoster struct {
	db           *gorm.DB
	eventbus     eventbus.EventBus
	inferrer     AssistantInferrer
	giteaClient  *gitea.Client
	giteaCfg     *config.GiteaConfig
	env          string
	titleUpdater TitleUpdater
}

// NewMessagePoster 创建 MessagePoster 实例。
func NewMessagePoster(db *gorm.DB, eb eventbus.EventBus, inferrer AssistantInferrer, giteaClient *gitea.Client, giteaCfg *config.GiteaConfig, env string, titleUpdater TitleUpdater) *MessagePoster {
	return &MessagePoster{
		db:           db,
		eventbus:     eb,
		inferrer:     inferrer,
		giteaClient:  giteaClient,
		giteaCfg:     giteaCfg,
		env:          env,
		titleUpdater: titleUpdater,
	}
}

// PostMessage 在已有 session 上创建一条消息并完成后续投递（统计、EventBus、WorkerTask）。
func (p *MessagePoster) PostMessage(
	ctx context.Context,
	session *types.Session,
	executionMode agent.ExecutionMode,
	buildMessage func(sequence int64) *types.SessionMessage,
) (*types.SessionMessage, error) {
	sequence, err := infradb.GetNextSequence(ctx, p.db, session.ID)
	if err != nil {
		return nil, err
	}

	message := buildMessage(sequence)
	message.SessionID = session.ID
	if message.MessageType == "" {
		message.MessageType = string(types.MessageTypeText)
	}

	if err := infradb.CreateMessage(ctx, p.db, message); err != nil {
		return nil, fmt.Errorf("create message: %w", err)
	}

	logs.DebugContextf(ctx, "created message seq=%d in session=%s", sequence, session.PublicID)

	now := time.Now()
	if err := infradb.IncrementMessageCount(ctx, p.db, session.ID); err != nil {
		return nil, err
	}
	if err := infradb.UpdateLastMessageAt(ctx, p.db, session.ID, now); err != nil {
		return nil, err
	}

	// Trigger title update asynchronously via local call.
	if session.OrgID > 0 {
		go func() {
			if p.titleUpdater == nil {
				return
			}
			titleCtx := context.Background()
			if caller, _ := auth.FromContext(ctx); caller != nil && caller.OrgID > 0 {
				titleCtx = auth.WithContext(titleCtx, caller, nil)
			} else {
				titleCtx = auth.WithContext(titleCtx, &types.Caller{
					Uin:   session.Uin,
					OrgID: session.OrgID,
				}, nil)
			}
			if err := p.titleUpdater.HandleSessionTitleRequest(titleCtx, session.PublicID); err != nil {
				logs.Warnf("title update failed for session %s: %v", session.PublicID, err)
			}
		}()
	}

	logs.DebugContextf(ctx, "published message events for session=%s", session.PublicID)

	p.writeSkillInvokeResources(ctx, session, message)

	if err := p.publishWorkerTask(ctx, session, message, executionMode); err != nil {
		return nil, err
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

	logs.DebugContextf(ctx, "NewMessage: caller=%d org=%d assistant=%d", caller.Uin, caller.OrgID, req.AssistantID)

	if err := o.resolveOrCreateProject(); err != nil {
		logs.ErrorContextf(ctx, "NewMessage resolveOrCreateProject failed: %v", err)
		return nil, err
	}
	// 无项目预上传的附件，需要在项目创建完成后回填项目归属，确保后续文件树可见。
	attachFilesToProject(ctx, p.db, caller.OrgID, caller.Uin, nil, o.project, req.Attachments)
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

	// 先补齐附件的可访问 URL，再把附件写入用户消息，避免前端回显和后续上下文拿不到附件信息。
	resolveAttachmentURLs(ctx, p.db, caller.OrgID, req.Attachments)

	// 中文注释：content 为空时表示"召唤队友落地空对话"——仅创建 Project/Task/Session + 分配 worker，不发首条消息。
	// 后续用户在任务详情页发送的消息走 AddMessage 路径，persona 通过 publishWorkerTask 自动注入。
	var messageID string
	if strings.TrimSpace(req.Content) != "" || len(req.Attachments) > 0 {
		message, err := p.PostMessage(ctx, o.taskSession, req.ExecutionMode, func(sequence int64) *types.SessionMessage {
			msgType := req.MessageType
			if msgType == "" {
				msgType = string(types.MessageTypeText)
			}
			return &types.SessionMessage{
				Role:        string(types.MessageRoleUser),
				Content:     req.Content,
				MessageType: msgType,
				Attachments: req.Attachments,
				Status:      string(types.MessageStatusPending),
				Sequence:    sequence,
				Timestamp:   time.Now().UnixMilli(),
			}
		})
		if err != nil {
			logs.ErrorContextf(ctx, "NewMessage PostMessage failed: %v", err)
			return nil, err
		}
		messageID = fmt.Sprintf("%d", message.ID)
	} else {
		logs.InfoContextf(ctx, "NewMessage empty summon: project=%s task=%s session=%s assistant=%d (no first message)",
			o.project.PublicID, o.task.PublicID, o.taskSession.PublicID, o.taskSession.AllocatedAssistantID)
	}
	// 中文注释：项目页里通过 NewMessage 创建任务/首条消息后，要立即刷新项目活跃时间，供左侧列表排序使用。
	if err := infradb.TouchProjectUpdatedAt(ctx, p.db, o.project.ID, time.Now()); err != nil {
		logs.WarnContextf(ctx, "NewMessage touch project updated_at failed: %v", err)
	}

	logs.InfoContextf(ctx, "NewMessage completed: project=%s task=%s session=%s message=%s assistant=%d",
		o.project.PublicID, o.task.PublicID, o.taskSession.PublicID, messageID, o.taskSession.AllocatedAssistantID)

	return &contract.NewMessageResponse{
		ProjectID:   o.project.PublicID,
		TaskID:      o.task.PublicID,
		SessionID:   o.taskSession.PublicID,
		MessageID:   messageID,
		AssistantID: o.taskSession.AssistantID,
	}, nil
}

// newMessageOrchestrator 持有 NewMessage 编排过程中的临时状态。
// 仅在 RunNewMessage 调用期间存续，不可复用。
type newMessageOrchestrator struct {
	poster *MessagePoster
	ctx    context.Context
	req    *contract.NewMessageRequest
	caller *types.Caller

	project     *types.Project
	task        *types.Task
	taskSession *types.Session
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
		if err := verifyUserPermission(proj.OwnerID, o.caller.Uin); err != nil {
			return err
		}
		o.project = proj
		return nil
	}

	title := o.defaultTitle("新的队友对话")
	runes := []rune(strings.TrimSpace(o.req.Content))
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
		repoInfo, _, err := o.poster.giteaClient.CreateRepo(gitea.CreateRepoOption{
			Name:        repoName,
			Description: "",
			Private:     true,
			AutoInit:    false,
		})
		if err != nil {
			return fmt.Errorf("create gitea repo: %w", err)
		}
		o.project.GiteaRepoFullName = repoInfo.FullName
		o.project.GiteaRepoID = repoInfo.ID
	}

	if err := infradb.CreateProject(o.ctx, o.poster.db, o.project); err != nil {
		return fmt.Errorf("create project: %w", err)
	}

	if o.project.GiteaRepoFullName != "" {
		if err := git.InitRepoStructure(o.ctx, o.poster.giteaClient, o.project.GiteaRepoFullName); err != nil {
			logs.WarnContextf(o.ctx, "[message_poster] init repo structure: %v", err)
		}
		logs.InfoContextf(o.ctx, "created project=%s org=%d user=%d repo=%s", projectID, o.caller.OrgID, o.caller.Uin, o.project.GiteaRepoFullName)
	} else {
		logs.InfoContextf(o.ctx, "created project=%s org=%d user=%d (no gitea)", projectID, o.caller.OrgID, o.caller.Uin)
	}

	if err := infradb.CreateProjectMember(o.ctx, o.poster.db, &types.ProjectMember{
		ProjectID:  o.project.ID,
		MemberID:   o.caller.Uin,
		MemberType: types.MemberTypeUser,
		MemberRole: types.MemberRoleOwner,
	}); err != nil {
		logs.WarnContextf(o.ctx, "create project member failed: %v", err)
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

	assistantID, workerID, err := o.poster.resolveRuntimeWorker(o.ctx, o.caller.OrgID, o.req.AssistantID)
	if err != nil {
		return err
	}
	projectSessionID := fmt.Sprintf("sess_%s", snowflake.GenerateIDBase58())
	projectSession = &types.Session{
		PublicID:             projectSessionID,
		Type:                 types.SessionTypeProject,
		Uin:                  o.caller.Uin,
		OrgID:                o.caller.OrgID,
		AssistantID:          assistantID,
		AllocatedAssistantID: workerID,
		ProjectID:            &o.project.ID,
		Status:               string(types.SessionStatusActive),
		Title:                "项目协作",
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
		if err := verifyUserPermission(t.OwnerID, o.caller.Uin); err != nil {
			return err
		}
		o.task = t
		return nil
	}

	taskTitle := o.defaultTitle("新的队友任务")
	runes := []rune(strings.TrimSpace(o.req.Content))
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

	logs.InfoContextf(o.ctx, "created task=%s in project=%s", taskID, o.project.PublicID)
	return nil
}

func (o *newMessageOrchestrator) defaultTitle(fallback string) string {
	if o == nil || o.poster == nil || o.poster.db == nil || o.req == nil || o.req.AssistantID == 0 {
		return fallback
	}
	da, err := infradb.GetDigitalAssistantByID(o.ctx, o.poster.db, o.req.AssistantID)
	if err != nil || da == nil || strings.TrimSpace(da.Name) == "" {
		return fallback
	}
	return fmt.Sprintf("与%s对话", strings.TrimSpace(da.Name))
}

func (o *newMessageOrchestrator) createTaskSession() error {
	assistantID, workerID, err := o.poster.resolveRuntimeWorker(o.ctx, o.caller.OrgID, o.req.AssistantID)
	if err != nil {
		return err
	}
	taskSessionID := fmt.Sprintf("sess_%s", snowflake.GenerateIDBase58())
	o.taskSession = &types.Session{
		PublicID:             taskSessionID,
		Type:                 types.SessionTypeTask,
		Uin:                  o.caller.Uin,
		OrgID:                o.caller.OrgID,
		AssistantID:          assistantID,
		AllocatedAssistantID: workerID,
		ProjectID:            &o.project.ID,
		TaskID:               &o.task.ID,
		Status:               string(types.SessionStatusActive),
		Title:                o.task.Title,
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

// buildExecutionTarget 根据会话绑定的 DigitalAssistant 构造 ExecutionTarget。
// 查询失败或无 assistant_id 时返回零值，不阻塞 run（降级为通用 lework 身份）。
func (p *MessagePoster) buildExecutionTarget(ctx context.Context, session *types.Session, message *types.SessionMessage) messaging.ExecutionTarget {
	if session == nil {
		return messaging.ExecutionTarget{}
	}
	assistantID := session.AssistantID
	if p == nil || p.db == nil || assistantID == 0 {
		return messaging.ExecutionTarget{}
	}
	da, err := infradb.GetDigitalAssistantByID(ctx, p.db, assistantID)
	if err != nil || da == nil {
		logs.WarnContextf(ctx, "buildExecutionTarget: assistant %d not found, fallback to default identity: %v", assistantID, err)
		return messaging.ExecutionTarget{AssistantID: strconv.FormatUint(uint64(assistantID), 10)}
	}
	systemPrompt := da.SystemPrompt
	if message != nil {
		evolution, err := p.buildAssistantEvolutionContext(ctx, assistantID, message.Content)
		if err != nil {
			logs.WarnContextf(ctx, "buildExecutionTarget: assistant %d evolution context skipped: %v", assistantID, err)
		} else if evolution.promptExtension != "" {
			systemPrompt = strings.TrimSpace(strings.Join(filterEmptyStrings(systemPrompt, evolution.promptExtension), "\n\n"))
			p.writeAssistantPromptTrace(ctx, session, message, assistantID, systemPrompt, evolution)
		}
	}

	return messaging.ExecutionTarget{
		AssistantID:   strconv.FormatUint(uint64(assistantID), 10),
		AssistantName: da.Name,
		SystemPrompt:  systemPrompt,
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

func (p *MessagePoster) publishWorkerTask(
	ctx context.Context,
	session *types.Session,
	message *types.SessionMessage,
	executionMode agent.ExecutionMode,
) error {
	caller, _ := auth.FromContext(ctx)
	orgID := session.OrgID
	if orgID == 0 && caller != nil {
		orgID = caller.OrgID
	}

	if session.AssistantID == 0 && session.AllocatedAssistantID == 0 && p.inferrer != nil {
		assignedAssistantID := p.inferrer.InferAssignedAssistantID(ctx, orgID, string(session.Type))
		if assignedAssistantID > 0 {
			session.AllocatedAssistantID = assignedAssistantID
			if err := infradb.UpdateAllocatedAssistantID(ctx, p.db, session.ID, assignedAssistantID); err != nil {
				return fmt.Errorf("failed to update allocated_assistant_id: %w", err)
			}
		}
	}

	if session.AllocatedAssistantID == 0 {
		logs.DebugContextf(ctx, "Skipping task publish: no worker allocated for session %s", session.PublicID)
		return nil
	}

	topic, err := messaging.WorkerCommandSubject(orgID, session.AllocatedAssistantID, messaging.LaneRun)
	if err != nil {
		return fmt.Errorf("failed to construct worker command topic: %w", err)
	}

	projectPublicID, taskPublicID, err := p.resolveWorkspaceIDs(ctx, session)
	if err != nil {
		return err
	}
	if taskPublicID == "" {
		taskPublicID = fmt.Sprintf("task_%d", message.ID)
	}
	syncOrgSkillsToWorker(ctx, p.db, p.eventbus, orgID, session.AllocatedAssistantID)
	requestID := fmt.Sprintf("req_%d", message.ID)
	modelOptions, err := p.resolveWorkerTaskModel(ctx, orgID)
	if err != nil {
		return err
	}

	executionTarget := p.buildExecutionTarget(ctx, session, message)

	cmd := messaging.NewRunCommand(
		fmt.Sprintf("msg_%d_%d", session.ID, message.Sequence),
		messaging.RouteContext{
			OrgID:     orgID,
			SessionID: session.PublicID,
			WorkerID:  session.AllocatedAssistantID,
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
				Type: messaging.InputTypeMessage,
				Messages: []messaging.ChatMessage{
					{ID: fmt.Sprintf("%d", message.ID), Role: messaging.MessageRoleUser, Content: message.Content},
				},
				Attachments: convertMessageToMessagingAttachments(message.Attachments),
			},
			Model:     modelOptions,
			Execution: executionTarget,
		},
		&messaging.RunCommandMetadata{
			SessionID:   session.PublicID,
			MessageType: message.MessageType,
			Sequence:    message.Sequence,
		},
	)

	if err := p.eventbus.Publish(ctx, topic, cmd); err != nil {
		logs.ErrorContextf(ctx, "Failed to publish message to assistant %d: %v", session.AllocatedAssistantID, err)
		return fmt.Errorf("failed to publish message to assistant: %w", err)
	}
	logs.DebugContextf(ctx, "Published message to topic %s: session_id=%s sequence=%d", topic, session.PublicID, message.Sequence)
	return nil
}

func normalizeExecutionMode(mode agent.ExecutionMode) agent.ExecutionMode {
	if mode == agent.ExecutionModePlan {
		return agent.ExecutionModePlan
	}
	return agent.ExecutionModeDefault
}

func (p *MessagePoster) resolveWorkerTaskModel(ctx context.Context, orgID uint) (messaging.ModelOptions, error) {
	if p == nil || p.db == nil {
		return messaging.ModelOptions{}, errors.New("database is required to resolve worker task llm model")
	}
	model, err := infradb.GetDefaultLLMModel(ctx, p.db, orgID)
	if err != nil {
		return messaging.ModelOptions{}, fmt.Errorf("get default llm model: %w", err)
	}
	if model == nil {
		return messaging.ModelOptions{}, errors.New("default llm model not found")
	}
	if strings.TrimSpace(model.Provider) == "" || strings.TrimSpace(model.ModelName) == "" || strings.TrimSpace(model.APIKeyEncrypted) == "" {
		return messaging.ModelOptions{}, errors.New("default llm model config is incomplete")
	}
	return messaging.ModelOptions{
		Provider:     model.Provider,
		Model:        model.ModelName,
		BaseURL:      model.BaseURL,
		BaseURLHasV1: model.BaseURLHasV1,
		APIKey:       model.APIKeyEncrypted,
	}, nil
}

func convertMessageToMessagingAttachments(attachments types.MessageAttachmentSlice) []messaging.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	result := make([]messaging.Attachment, 0, len(attachments))
	for _, a := range attachments {
		result = append(result, messaging.Attachment{
			ID:       a.FileUploadID,
			Name:     a.Name,
			MimeType: a.MimeType,
			URL:      a.PublicURL,
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
	db *gorm.DB,
	orgID uint,
	uin uint,
	taskID *uint,
	project *types.Project,
	attachments []types.MessageAttachment,
) {
	if project == nil || project.ID == 0 || len(attachments) == 0 {
		return
	}
	for i := range attachments {
		if attachments[i].FileUploadID == "" {
			continue
		}
		fileUpload, err := infradb.GetFileUploadByPublicID(ctx, db, orgID, attachments[i].FileUploadID)
		if err != nil {
			logs.WarnContextf(ctx, "attach file %s to project %s failed: %v", attachments[i].FileUploadID, project.PublicID, err)
			continue
		}
		if fileUpload == nil {
			continue
		}

		exists, _ := infradb.GetProjectFileByFilePublicID(ctx, db, orgID, fileUpload.PublicID)
		if exists == nil {
			pf := &types.ProjectFile{
				FilePublicID: fileUpload.PublicID,
				OrgID:        orgID,
				ProjectID:    project.ID,
				ResourceID:   fileUpload.ID,
				ResourceType: types.ProjectFileResourceTypeUserUpload,
				Uin:          uin,
			}
			if taskID != nil {
				pf.TaskID = *taskID
			}
			if err := infradb.CreateProjectFile(ctx, db, pf); err != nil {
				logs.WarnContextf(ctx, "create project_file record for attachment %s: %v", attachments[i].FileUploadID, err)
			}
		}
	}
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

// writeSkillInvokeResources parses /skill tokens from message content and writes
// message_resource records so that skill invocations are tracked at the service layer
// before the worker task is published.
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
		source, skillID, resourceID := p.resolveSkillMarketplace(ctx, name)
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

// resolveSkillMarketplace looks up the marketplace record for a local skill
// name. Returns (source, skill_id, db_primary_key_as_string). When no record is
// found, source and skillID fall back to the name itself and resourceID is empty.
func (p *MessagePoster) resolveSkillMarketplace(ctx context.Context, name string) (source, skillID, resourceID string) {
	if item, err := infradb.GetBuiltinSkillByID(ctx, p.db, name); err == nil && item != nil {
		return "Leros", item.SkillID, fmt.Sprintf("%d", item.ID)
	}
	query := p.db.WithContext(ctx).Model(&types.SkillMarketplaceItem{}).
		Where("name = ?", name).
		Select("id, source, skill_id")
	type row struct {
		ID      uint   `gorm:"column:id"`
		Source  string `gorm:"column:source"`
		SkillID string `gorm:"column:skill_id"`
	}
	var r row
	if err := query.First(&r).Error; err == nil && r.Source != "" {
		return r.Source, r.SkillID, fmt.Sprintf("%d", r.ID)
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

// resolveSkillEntries resolves skill tokens to manifest names, deduplicating
// case-insensitively and keeping only valid skill names in the catalog.
func resolveSkillEntries(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(tokens))
	result := make([]string, 0, len(tokens))
	for _, name := range tokens {
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := skillcatalog.Get(name); err != nil {
			continue
		}
		result = append(result, name)
	}
	return result
}
