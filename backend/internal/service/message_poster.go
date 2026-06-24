package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"code.gitea.io/sdk/gitea"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
)

// MessagePoster 无状态的消息投递器，负责消息创建、统计更新、事件发布、Worker 任务投递。
// 多个 goroutine 可安全并发使用。
type MessagePoster struct {
	db          *gorm.DB
	eventbus    eventbus.EventBus
	inferrer    AssistantInferrer
	giteaClient *gitea.Client
	giteaCfg    *config.GiteaConfig
	env         string
}

// NewMessagePoster 创建 MessagePoster 实例。
func NewMessagePoster(db *gorm.DB, eb eventbus.EventBus, inferrer AssistantInferrer, giteaClient *gitea.Client, giteaCfg *config.GiteaConfig, env string) *MessagePoster {
	return &MessagePoster{
		db:          db,
		eventbus:    eb,
		inferrer:    inferrer,
		giteaClient: giteaClient,
		giteaCfg:    giteaCfg,
		env:         env,
	}
}

// PostMessage 在已有 session 上创建一条消息并完成后续投递（统计、EventBus、WorkerTask）。
func (p *MessagePoster) PostMessage(
	ctx context.Context,
	session *types.Session,
	buildMessage func(sequence int64) *types.SessionMessage,
) (*types.SessionMessage, error) {
	sequence, err := db.GetNextSequence(ctx, p.db, session.ID)
	if err != nil {
		return nil, err
	}

	message := buildMessage(sequence)
	message.SessionID = session.ID
	if message.MessageType == "" {
		message.MessageType = string(types.MessageTypeText)
	}

	if err := db.CreateMessage(ctx, p.db, message); err != nil {
		return nil, fmt.Errorf("create message: %w", err)
	}

	logs.DebugContextf(ctx, "created message seq=%d in session=%s", sequence, session.PublicID)

	now := time.Now()
	if err := db.IncrementMessageCount(ctx, p.db, session.ID); err != nil {
		return nil, err
	}
	if err := db.UpdateLastMessageAt(ctx, p.db, session.ID, now); err != nil {
		return nil, err
	}

	if session.OrgID > 0 {
		topic, err := dm.SessionMessageRequestSubject(session.OrgID, session.PublicID)
		if err != nil {
			logs.WarnContextf(ctx, "failed to build message request subject: %v", err)
		} else {
			if err := p.eventbus.Publish(ctx, topic, message); err != nil {
				logs.WarnContextf(ctx, "failed to publish message to eventbus: %v", err)
			}
		}
	}

	logs.DebugContextf(ctx, "published message events for session=%s", session.PublicID)

	if err := p.publishWorkerTask(ctx, session, message); err != nil {
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
	p.attachFilesToProject(ctx, caller.OrgID, o.project.PublicID, req.Attachments)
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
	p.resolveAttachmentURLs(ctx, caller.OrgID, req.Attachments)

	message, err := p.PostMessage(ctx, o.taskSession, func(sequence int64) *types.SessionMessage {
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

	logs.InfoContextf(ctx, "NewMessage completed: project=%s task=%s session=%s message=%d assistant=%d",
		o.project.PublicID, o.task.PublicID, o.taskSession.PublicID, message.ID, o.taskSession.AllocatedAssistantID)

	return &contract.NewMessageResponse{
		ProjectID:   o.project.PublicID,
		TaskID:      o.task.PublicID,
		SessionID:   o.taskSession.PublicID,
		MessageID:   fmt.Sprintf("%d", message.ID),
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
		proj, err := db.GetProjectByPublicID(o.ctx, o.poster.db, o.caller.OrgID, o.req.ProjectID)
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

	runes := []rune(o.req.Content)
	title := string(runes)
	if len(runes) > 50 {
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

	if err := db.CreateProject(o.ctx, o.poster.db, o.project); err != nil {
		return fmt.Errorf("create project: %w", err)
	}

	o.poster.initRepoStructure(o.ctx, repoInfo.FullName)

	logs.InfoContextf(o.ctx, "created project=%s org=%d user=%d repo=%s", projectID, o.caller.OrgID, o.caller.Uin, repoInfo.FullName)

	if err := db.CreateProjectMember(o.ctx, o.poster.db, &types.ProjectMember{
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
	projectSession, err := db.GetProjectSession(o.ctx, o.poster.db, o.project.ID)
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
	if err := db.CreateSession(o.ctx, o.poster.db, projectSession); err != nil {
		return fmt.Errorf("create project session: %w", err)
	}

	logs.InfoContextf(o.ctx, "created project session=%s for project=%s", projectSessionID, o.project.PublicID)
	return nil
}

func (o *newMessageOrchestrator) resolveOrCreateTask() error {
	if o.req.TaskID != "" {
		t, err := db.GetTaskByPublicID(o.ctx, o.poster.db, o.caller.OrgID, o.req.TaskID)
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

	runes := []rune(o.req.Content)
	taskTitle := string(runes)
	if len(runes) > 50 {
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
	if err := db.CreateTask(o.ctx, o.poster.db, o.task); err != nil {
		return fmt.Errorf("create task: %w", err)
	}

	logs.InfoContextf(o.ctx, "created task=%s in project=%s", taskID, o.project.PublicID)
	return nil
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
	if err := db.CreateSession(o.ctx, o.poster.db, o.taskSession); err != nil {
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

func (p *MessagePoster) publishWorkerTask(ctx context.Context, session *types.Session, message *types.SessionMessage) error {
	caller, _ := auth.FromContext(ctx)
	orgID := session.OrgID
	if orgID == 0 && caller != nil {
		orgID = caller.OrgID
	}

	if session.AssistantID == 0 && session.AllocatedAssistantID == 0 && p.inferrer != nil {
		assignedAssistantID := p.inferrer.InferAssignedAssistantID(ctx, orgID, string(session.Type))
		if assignedAssistantID > 0 {
			session.AllocatedAssistantID = assignedAssistantID
			if err := db.UpdateAllocatedAssistantID(ctx, p.db, session.ID, assignedAssistantID); err != nil {
				return fmt.Errorf("failed to update allocated_assistant_id: %w", err)
			}
		}
	}

	if session.AllocatedAssistantID == 0 {
		logs.DebugContextf(ctx, "Skipping task publish: no worker allocated for session %s", session.PublicID)
		return nil
	}

	topic, err := dm.WorkerTaskSubject(orgID, session.AllocatedAssistantID)
	if err != nil {
		return fmt.Errorf("failed to construct worker task topic: %w", err)
	}

	projectPublicID, taskPublicID, err := p.resolveWorkspaceIDs(ctx, session)
	if err != nil {
		return err
	}
	if taskPublicID == "" {
		taskPublicID = fmt.Sprintf("task_%d", message.ID)
	}
	requestID := fmt.Sprintf("req_%d", message.ID)
	modelOptions, err := p.resolveWorkerTaskModel(ctx, orgID)
	if err != nil {
		return err
	}

	messagePayload := protocol.WorkerTaskMessage{
		ID:        fmt.Sprintf("msg_%d_%d", session.ID, message.Sequence),
		Type:      protocol.MessageTypeWorkerTask,
		CreatedAt: time.Now().UTC(),
		Trace: protocol.TraceContext{
			TraceID:   session.PublicID,
			RequestID: requestID,
			TaskID:    taskPublicID,
			RunID:     requestID,
		},
		Route: protocol.RouteContext{
			OrgID:     orgID,
			SessionID: session.PublicID,
			WorkerID:  session.AllocatedAssistantID,
		},
		Body: protocol.WorkerTaskBody{
			TaskType: protocol.TaskTypeAgentRun,
			Actor: protocol.ActorContext{
				UserID:      fmt.Sprintf("%d", session.Uin),
				DisplayName: "",
				Channel:     "session",
			},
			Workspace: protocol.WorkspaceOptions{
				ProjectID: projectPublicID,
				TaskID:    taskPublicID,
			},
			Input: protocol.TaskInput{
				Type: protocol.InputTypeMessage,
				Messages: []protocol.ChatMessage{
					{ID: fmt.Sprintf("%d", message.ID), Role: protocol.MessageRoleUser, Content: message.Content},
				},
				Attachments: convertMessageToProtocolAttachments(message.Attachments),
			},
			Model: modelOptions,
		},
		Metadata: map[string]any{
			"session_id":   session.PublicID,
			"message_type": message.MessageType,
			"sequence":     message.Sequence,
		},
	}

	if err := p.eventbus.Publish(ctx, topic, messagePayload); err != nil {
		logs.ErrorContextf(ctx, "Failed to publish message to assistant %d: %v", session.AllocatedAssistantID, err)
		return fmt.Errorf("failed to publish message to assistant: %w", err)
	}
	logs.DebugContextf(ctx, "Published message to topic %s: session_id=%s sequence=%d", topic, session.PublicID, message.Sequence)
	return nil
}

func (p *MessagePoster) resolveWorkerTaskModel(ctx context.Context, orgID uint) (protocol.ModelOptions, error) {
	if p == nil || p.db == nil {
		return protocol.ModelOptions{}, errors.New("database is required to resolve worker task llm model")
	}
	model, err := db.GetDefaultLLMModel(ctx, p.db, orgID)
	if err != nil {
		return protocol.ModelOptions{}, fmt.Errorf("get default llm model: %w", err)
	}
	if model == nil {
		return protocol.ModelOptions{}, errors.New("default llm model not found")
	}
	if strings.TrimSpace(model.Provider) == "" || strings.TrimSpace(model.ModelName) == "" || strings.TrimSpace(model.APIKeyEncrypted) == "" {
		return protocol.ModelOptions{}, errors.New("default llm model config is incomplete")
	}
	return protocol.ModelOptions{
		Provider:     model.Provider,
		Model:        model.ModelName,
		BaseURL:      model.BaseURL,
		BaseURLHasV1: model.BaseURLHasV1,
		APIKey:       model.APIKeyEncrypted,
	}, nil
}

func convertMessageToProtocolAttachments(attachments types.MessageAttachmentSlice) []protocol.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	result := make([]protocol.Attachment, 0, len(attachments))
	for _, a := range attachments {
		result = append(result, protocol.Attachment{
			ID:       a.FileUploadID,
			Name:     a.Name,
			MimeType: a.MimeType,
			URL:      a.PublicURL,
		})
	}
	return result
}

func (p *MessagePoster) resolveAttachmentURLs(
	ctx context.Context,
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
		fileUpload, err := db.GetFileUploadByPublicID(ctx, p.db, orgID, attachments[i].FileUploadID)
		if err != nil {
			logs.WarnContextf(ctx, "resolve attachment file %s: %v", attachments[i].FileUploadID, err)
			continue
		}
		if fileUpload == nil {
			logs.WarnContextf(ctx, "resolve attachment file %s: not found", attachments[i].FileUploadID)
			continue
		}
		publicURL, err := filestore.ResolvePublicURL(ctx, fileUpload.StoragePath)
		if err != nil {
			logs.WarnContextf(ctx, "resolve attachment public url for %s: %v", attachments[i].FileUploadID, err)
			continue
		}
		attachments[i].PublicURL = publicURL
	}
}

func (p *MessagePoster) attachFilesToProject(
	ctx context.Context,
	orgID uint,
	projectPublicID string,
	attachments []types.MessageAttachment,
) {
	if strings.TrimSpace(projectPublicID) == "" || len(attachments) == 0 {
		return
	}
	for i := range attachments {
		if attachments[i].FileUploadID == "" {
			continue
		}
		fileUpload, err := db.GetFileUploadByPublicID(ctx, p.db, orgID, attachments[i].FileUploadID)
		if err != nil {
			logs.WarnContextf(ctx, "attach file %s to project %s failed: %v", attachments[i].FileUploadID, projectPublicID, err)
			continue
		}
		if fileUpload == nil {
			continue
		}
		if fileUpload.Metadata.Extra == nil {
			fileUpload.Metadata.Extra = make(map[string]interface{})
		}
		fileUpload.Metadata.Extra["project_public_id"] = projectPublicID
		if err := db.UpdateFileUpload(ctx, p.db, fileUpload); err != nil {
			logs.WarnContextf(ctx, "persist file %s project_public_id failed: %v", attachments[i].FileUploadID, err)
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

func (p *MessagePoster) initRepoStructure(ctx context.Context, fullName string) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return
	}
	owner, repo := parts[0], parts[1]

	emptyContent := base64.StdEncoding.EncodeToString([]byte(""))

	gitignore := `# Leros runtime
.leros/
!.leros/memory/

# Dependency directories
node_modules/
vendor/

# Build/cache outputs
dist/
build/
target/
.cache/
.cache*/
tmp/
temp/
logs/
log/

# OS/editor noise
.DS_Store
Thumbs.db
*.swp
*.swo

# Runtime logs
*.log

# Environment/secrets
.env
.env.*
!.env.example
`
	gitignoreContent := base64.StdEncoding.EncodeToString([]byte(gitignore))

	initFiles := []struct {
		path    string
		content string
		msg     string
	}{
		{".gitignore", gitignoreContent, "chore: init .gitignore"},
		{".leros/memory/.gitkeep", emptyContent, "chore: init .leros/memory/"},
		{"artifacts/.gitkeep", emptyContent, "chore: init artifacts/"},
		{"README.md", base64.StdEncoding.EncodeToString([]byte("# " + repo + "\n")), "chore: init README"},
	}

	for _, f := range initFiles {
		if _, _, err := p.giteaClient.CreateFile(owner, repo, f.path, gitea.CreateFileOptions{
			FileOptions: gitea.FileOptions{
				Message: f.msg,
			},
			Content: f.content,
		}); err != nil {
			logs.WarnContextf(ctx, "[message_poster] init gitea file %s failed: %v", f.path, err)
		}
	}
}
