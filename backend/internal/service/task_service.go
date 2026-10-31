package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/internal/api/contract"
	"github.com/insmtx/Lework/backend/internal/infra/db"
	"github.com/insmtx/Lework/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
)

type taskService struct {
	db       *gorm.DB
	perm     *PermissionService
	sessions contract.SessionService
}

type projectTaskBrief struct {
	PublicID string
	Name     string
}

// ErrInvalidTaskStatusTransition 表示任务状态迁移不符合生命周期约束。
var ErrInvalidTaskStatusTransition = errors.New("invalid task status transition")

// allowedTaskStatusTransitions 约束任务状态只能沿闭环方向推进。
// completed / cancelled 为终态；驳回路径从 pending_acceptance 回到 in_progress。
var allowedTaskStatusTransitions = map[types.TaskStatus]map[types.TaskStatus]bool{
	types.TaskStatusCreated: {
		types.TaskStatusPendingAssignment: true,
		types.TaskStatusInProgress:        true,
		types.TaskStatusCancelled:         true,
	},
	types.TaskStatusPendingAssignment: {
		types.TaskStatusInProgress: true,
		types.TaskStatusCancelled:  true,
	},
	types.TaskStatusInProgress: {
		types.TaskStatusPendingApproval:   true,
		types.TaskStatusPendingAcceptance: true,
		types.TaskStatusCompleted:         true,
		types.TaskStatusFailed:            true,
		types.TaskStatusCancelled:         true,
		types.TaskStatusPaused:            true,
	},
	types.TaskStatusPendingApproval: {
		types.TaskStatusInProgress:        true,
		types.TaskStatusPendingAcceptance: true,
		types.TaskStatusCompleted:         true,
		types.TaskStatusFailed:            true,
		types.TaskStatusCancelled:         true,
	},
	types.TaskStatusPendingAcceptance: {
		types.TaskStatusInProgress: true,
		types.TaskStatusCompleted:  true,
		types.TaskStatusCancelled:  true,
	},
	types.TaskStatusPaused: {
		types.TaskStatusInProgress: true,
		types.TaskStatusCancelled:  true,
	},
	types.TaskStatusFailed: {
		types.TaskStatusInProgress: true,
		types.TaskStatusCancelled:  true,
	},
}

// ValidateTaskStatusTransition 校验任务状态迁移是否合法，供 UpdateTask 与投影器共用。
func ValidateTaskStatusTransition(from, to types.TaskStatus) error {
	if from == to {
		return nil
	}
	allowed, ok := allowedTaskStatusTransitions[from]
	if !ok || !allowed[to] {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTaskStatusTransition, from, to)
	}
	return nil
}

func NewTaskService(db *gorm.DB, perm *PermissionService) contract.TaskService {
	return NewTaskServiceWithSession(db, perm, nil)
}

// NewTaskServiceWithSession 注入会话服务，使驳回后能通过正常消息链路触发返工。
func NewTaskServiceWithSession(db *gorm.DB, perm *PermissionService, sessions contract.SessionService) contract.TaskService {
	return &taskService{
		db:       db,
		perm:     perm,
		sessions: sessions,
	}
}

// ErrTaskNotPendingAcceptance 表示任务当前不在待验收状态，不能执行通过/驳回。
var ErrTaskNotPendingAcceptance = errors.New("task is not pending acceptance")

func (s *taskService) permWithDB(db *gorm.DB) *PermissionService {
	return PermissionForDB(db, s.perm)
}

// bindTaskAssignee 校验指派队友并保证其已绑定到项目资源。
// 任务详情页会按 assignee 路由到对应 Worker，因此绑定必须与任务写入处于同一事务。
func bindTaskAssignee(
	ctx context.Context,
	tx *gorm.DB,
	caller *types.Caller,
	project *types.Project,
	assigneeID uint,
) error {
	if assigneeID == 0 {
		return nil
	}
	assistant, err := db.GetDigitalAssistantByID(ctx, tx, assigneeID)
	if err != nil {
		return fmt.Errorf("get assignee: %w", err)
	}
	if assistant == nil || assistant.OrgID != caller.OrgID {
		return errDigitalAssistantNotFound
	}
	if _, err := newDigitalAssistantAccessManager(tx).require(
		ctx, caller.OrgID, caller.Uin, assistant, types.ActionAssistantUse,
	); err != nil {
		return err
	}

	resource, err := db.GetResourceByBizID(ctx, tx, caller.OrgID, types.ResourceTypeProject, project.ID)
	if err != nil {
		return fmt.Errorf("get project resource: %w", err)
	}
	if resource == nil {
		return errors.New("project resource not found")
	}
	binding, err := db.GetResourceBindingByAssistantID(ctx, tx, resource.ID, assigneeID)
	if err != nil {
		return fmt.Errorf("get assignee binding: %w", err)
	}
	if binding != nil {
		return nil
	}
	boundID := assigneeID
	if err := db.CreateResourceBinding(ctx, tx, &types.ResourceBinding{
		OrgID:       caller.OrgID,
		AssistantID: &boundID,
		ResourceID:  resource.ID,
		Role:        types.ResourceRoleMember,
	}); err != nil {
		return fmt.Errorf("bind assignee to project: %w", err)
	}
	return nil
}

func (s *taskService) CreateTask(ctx context.Context, req *contract.CreateTaskRequest) (*contract.Task, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Title) == "" {
		return nil, errors.New("title is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}

	publicID := generateTaskPublicID()

	taskType := string(types.TaskTypeGeneral)
	if req.TaskType != nil && *req.TaskType != "" {
		taskType = *req.TaskType
	}

	var assigneeID uint
	if req.AssigneeID != nil {
		assigneeID = *req.AssigneeID
	}
	task := &types.Task{
		OrgID:              caller.OrgID,
		PublicID:           publicID,
		OwnerID:            caller.Uin,
		ProjectID:          project.ID,
		TaskType:           types.TaskType(taskType),
		Title:              strings.TrimSpace(req.Title),
		Description:        strings.TrimSpace(req.Description),
		ExpectedOutput:     strings.TrimSpace(req.ExpectedOutput),
		AcceptanceCriteria: strings.TrimSpace(req.AcceptanceCriteria),
		Status:             string(types.TaskStatusCreated),
		Deadline:           req.Deadline,
	}
	if req.Metadata != nil {
		task.Metadata = types.ObjectMetadata{}
		if tags, ok := req.Metadata["tags"].([]interface{}); ok {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					task.Metadata.Tags = append(task.Metadata.Tags, s)
				}
			}
		}
		if t, ok := req.Metadata["type"].(string); ok {
			task.Metadata.Type = t
		}
		if extra, ok := req.Metadata["extra"].(map[string]interface{}); ok {
			task.Metadata.Extra = extra
		}
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if assigneeID > 0 {
			if err := bindTaskAssignee(ctx, tx, caller, project, assigneeID); err != nil {
				return err
			}
			task.AssigneeID = &assigneeID
		}
		if err := db.CreateTask(ctx, tx, task); err != nil {
			return err
		}
		return syncTaskResource(ctx, tx, caller.OrgID, project.ID, task.ID, caller.Uin)
	}); err != nil {
		return nil, err
	}
	// 中文注释：在项目内手动创建任务属于项目活跃行为，需要同步刷新项目排序时间。
	if err := db.TouchProjectUpdatedAt(ctx, s.db, project.ID, time.Now()); err != nil {
		logs.WarnContextf(ctx, "touch project updated_at after create task %s: %v", task.PublicID, err)
	}
	return convertToContractTask(task, project.PublicID, project.Name), nil
}

func (s *taskService) GetTask(ctx context.Context, publicID string) (*contract.Task, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	task, err := db.GetTaskByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, errors.New("task not found")
	}

	projectBrief, err := s.resolveProjectBrief(ctx, task.ProjectID)
	if err != nil {
		return nil, err
	}
	result := convertToContractTask(task, projectBrief.PublicID, projectBrief.Name)
	return s.attachTaskSession(ctx, task, result), nil
}

func (s *taskService) UpdateTask(ctx context.Context, publicID string, req *contract.UpdateTaskRequest) (*contract.Task, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	var task *types.Task
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err = db.GetTaskByPublicID(ctx, tx, caller.OrgID, publicID)
		if err != nil {
			return err
		}
		if task == nil {
			return errors.New("task not found")
		}

		if req.Title != nil {
			task.Title = strings.TrimSpace(*req.Title)
			if task.Title == "" {
				return errors.New("title cannot be empty")
			}
		}
		if req.Description != nil {
			task.Description = strings.TrimSpace(*req.Description)
		}
		if req.ExpectedOutput != nil {
			task.ExpectedOutput = strings.TrimSpace(*req.ExpectedOutput)
		}
		if req.AcceptanceCriteria != nil {
			task.AcceptanceCriteria = strings.TrimSpace(*req.AcceptanceCriteria)
		}
		if req.TaskType != nil {
			task.TaskType = types.TaskType(*req.TaskType)
		}
		if req.AssigneeID != nil {
			if *req.AssigneeID == 0 {
				task.AssigneeID = nil
			} else {
				project, dbErr := db.GetProjectByID(ctx, tx, task.ProjectID)
				if dbErr != nil {
					return dbErr
				}
				if project == nil {
					return errors.New("project not found")
				}
				if err := bindTaskAssignee(ctx, tx, caller, project, *req.AssigneeID); err != nil {
					return err
				}
				assigneeID := *req.AssigneeID
				task.AssigneeID = &assigneeID
			}
		}
		if req.Status != nil {
			nextStatus := types.TaskStatus(strings.TrimSpace(*req.Status))
			if err := ValidateTaskStatusTransition(types.TaskStatus(task.Status), nextStatus); err != nil {
				return err
			}
			task.Status = string(nextStatus)
		}
		if req.Deadline != nil {
			task.Deadline = req.Deadline
		}
		if req.ProjectID != nil {
			project, dbErr := db.GetProjectByPublicID(ctx, tx, caller.OrgID, *req.ProjectID)
			if dbErr != nil {
				return dbErr
			}
			if project == nil {
				return errors.New("project not found")
			}
			if err := s.permWithDB(tx).RequireProject(ctx, FromTypeCaller(caller), project, types.ActionProjectView); err != nil {
				return err
			}
			task.ProjectID = project.ID
			// 任务迁移到新项目后，assignee 必须仍然属于该项目，否则后续消息无法路由。
			if task.AssigneeID != nil && *task.AssigneeID > 0 {
				if err := bindTaskAssignee(ctx, tx, caller, project, *task.AssigneeID); err != nil {
					return err
				}
			}
		}
		if req.Metadata != nil {
			if *req.Metadata != nil {
				newMeta := types.ObjectMetadata{}
				if tags, ok := (*req.Metadata)["tags"].([]interface{}); ok {
					for _, t := range tags {
						if s, ok := t.(string); ok {
							newMeta.Tags = append(newMeta.Tags, s)
						}
					}
				}
				if t, ok := (*req.Metadata)["type"].(string); ok {
					newMeta.Type = t
				}
				if extra, ok := (*req.Metadata)["extra"].(map[string]interface{}); ok {
					newMeta.Extra = extra
				}
				task.Metadata = newMeta
			}
		}

		return db.UpdateTask(ctx, tx, task)
	}); err != nil {
		return nil, err
	}

	projectBrief, err := s.resolveProjectBrief(ctx, task.ProjectID)
	if err != nil {
		return nil, err
	}
	return convertToContractTask(task, projectBrief.PublicID, projectBrief.Name), nil
}

func (s *taskService) DeleteTask(ctx context.Context, publicID string) error {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(publicID) == "" {
		return errors.New("public_id is required")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := db.GetTaskByPublicID(ctx, tx, caller.OrgID, publicID)
		if err != nil {
			return err
		}
		if task == nil {
			return errors.New("task not found")
		}
		return db.DeleteTask(ctx, tx, task.ID)
	})
}

func (s *taskService) ListTasks(ctx context.Context, req *contract.ListTasksRequest) (*contract.TaskList, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	req.Fill()

	opt := types.NewPageQuery(*caller, req.Offset, req.Limit)
	opt.ListAll = req.ListAll
	if req.Keyword != nil && *req.Keyword != "" {
		opt.AddFilter("keyword", *req.Keyword)
	}
	if req.Status != nil && *req.Status != "" {
		opt.AddFilter("status", *req.Status)
	}
	if req.ProjectID != nil && *req.ProjectID != "" {
		project, dbErr := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, *req.ProjectID)
		if dbErr != nil {
			return nil, dbErr
		}
		if project == nil {
			return nil, errors.New("project not found")
		}
		if err := s.perm.RequireProject(ctx, FromTypeCaller(caller), project, types.ActionProjectView); err != nil {
			return nil, err
		}
		opt.AddExactFilter("project_id", fmt.Sprintf("%d", project.ID))
	} else {
		projectIDs, listErr := db.ListProjectIDsByUser(ctx, s.db, caller.OrgID, caller.Uin)
		if listErr != nil {
			return nil, listErr
		}
		if len(projectIDs) == 0 {
			return &contract.TaskList{
				Total:  0,
				Offset: req.Offset,
				Limit:  req.Limit,
				Items:  []contract.Task{},
			}, nil
		}
		opt.ProjectIDs = projectIDs
	}
	if req.TaskType != nil && *req.TaskType != "" {
		opt.AddFilter("task_type", *req.TaskType)
	}
	if req.AssigneeID != nil {
		opt.AddExactFilter("assignee_id", fmt.Sprintf("%d", *req.AssigneeID))
	}

	tasks, total, err := db.ListTasks(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}

	projectIDs := make([]uint, 0, len(tasks))
	projectIDSet := make(map[uint]struct{})
	for _, t := range tasks {
		if _, ok := projectIDSet[t.ProjectID]; !ok {
			projectIDSet[t.ProjectID] = struct{}{}
			projectIDs = append(projectIDs, t.ProjectID)
		}
	}
	projectBriefMap, err := s.resolveProjectBriefs(ctx, projectIDs)
	if err != nil {
		return nil, err
	}

	items := make([]contract.Task, 0, len(tasks))
	for _, task := range tasks {
		projectBrief := projectBriefMap[task.ProjectID]
		items = append(items, *convertToContractTask(task, projectBrief.PublicID, projectBrief.Name))
	}
	items = s.attachTasksSessions(ctx, tasks, items)
	return &contract.TaskList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func (s *taskService) attachTaskSession(
	ctx context.Context,
	task *types.Task,
	result *contract.Task,
) *contract.Task {
	if task == nil || result == nil || task.SessionID == nil {
		return result
	}
	sess, err := db.GetSessionByID(ctx, s.db, *task.SessionID)
	if err != nil {
		logs.WarnContextf(ctx, "attach task session failed task=%s session_id=%d: %v", task.PublicID, *task.SessionID, err)
		return result
	}
	if sess != nil {
		result.Session = convertToContractSession(ctx, sess, s.db)
	}
	return result
}

func (s *taskService) attachTasksSessions(
	ctx context.Context,
	tasks []*types.Task,
	items []contract.Task,
) []contract.Task {
	if len(tasks) == 0 || len(items) != len(tasks) {
		return items
	}

	taskSessionIDs := make([]uint, 0)
	for _, task := range tasks {
		if task.SessionID != nil {
			taskSessionIDs = append(taskSessionIDs, *task.SessionID)
		}
	}
	if len(taskSessionIDs) == 0 {
		return items
	}

	taskSessions, err := db.GetSessionsByIDs(ctx, s.db, taskSessionIDs)
	if err != nil {
		logs.WarnContextf(ctx, "attach tasks sessions failed: %v", err)
		return items
	}
	sessionMap := make(map[uint]*types.Session, len(taskSessions))
	for _, sess := range taskSessions {
		sessionMap[sess.ID] = sess
	}

	for i, task := range tasks {
		if task.SessionID == nil {
			continue
		}
		if sess, ok := sessionMap[*task.SessionID]; ok {
			items[i].Session = convertToContractSession(ctx, sess, s.db)
		}
	}
	return items
}

func convertToContractTask(task *types.Task, projectPublicID string, projectName string) *contract.Task {
	if task == nil {
		return nil
	}

	var metadata map[string]interface{}
	m := make(map[string]interface{})
	if len(task.Metadata.Tags) > 0 {
		m["tags"] = task.Metadata.Tags
	}
	if task.Metadata.Type != "" {
		m["type"] = task.Metadata.Type
	}
	if task.Metadata.Extra != nil && len(task.Metadata.Extra) > 0 {
		m["extra"] = task.Metadata.Extra
	}
	if len(m) > 0 {
		metadata = m
	}

	return &contract.Task{
		PublicID:           task.PublicID,
		OrgID:              task.OrgID,
		OwnerID:            task.OwnerID,
		ProjectID:          projectPublicID,
		ProjectName:        projectName,
		SessionID:          task.SessionID,
		TaskType:           string(task.TaskType),
		AssigneeID:         task.AssigneeID,
		Title:              task.Title,
		Description:        task.Description,
		ExpectedOutput:     task.ExpectedOutput,
		AcceptanceCriteria: task.AcceptanceCriteria,
		ResultSummary:      task.ResultSummary,
		Status:             task.Status,
		Deadline:           task.Deadline,
		AcceptedBy:         task.AcceptedBy,
		AcceptedAt:         task.AcceptedAt,
		AcceptanceNote:     task.AcceptanceNote,
		Metadata:           metadata,
		CreatedAt:          task.CreatedAt,
		UpdatedAt:          task.UpdatedAt,
	}
}

func generateTaskPublicID() string {
	return fmt.Sprintf("task_%s", snowflake.GenerateIDBase58())
}

func (s *taskService) resolveProjectBrief(ctx context.Context, projectID uint) (projectTaskBrief, error) {
	projectIDs := []uint{projectID}
	projectBriefs, err := s.resolveProjectBriefs(ctx, projectIDs)
	if err != nil {
		return projectTaskBrief{}, err
	}
	return projectBriefs[projectID], nil
}

func (s *taskService) resolveProjectBriefs(ctx context.Context, projectIDs []uint) (map[uint]projectTaskBrief, error) {
	result := make(map[uint]projectTaskBrief)
	if len(projectIDs) == 0 {
		return result, nil
	}
	projects, err := db.GetProjectsByIDs(ctx, s.db, projectIDs)
	if err != nil {
		return nil, err
	}
	// 中文注释：任务搜索结果需要同时展示项目 public_id 和项目名称，这里统一补齐，避免前端再做额外聚合查询。
	for _, p := range projects {
		result[p.ID] = projectTaskBrief{
			PublicID: p.PublicID,
			Name:     p.Name,
		}
	}
	return result, nil
}

func (s *taskService) GetTaskDeliverables(ctx context.Context, taskID string) (*contract.TaskDeliverables, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	task, project, err := s.loadTaskWithProject(ctx, caller, taskID)
	if err != nil {
		return nil, err
	}

	result := &contract.TaskDeliverables{
		TaskID:        task.PublicID,
		Artifacts:     []types.MessageArtifact{},
		Files:         []contract.TaskDeliverableFile{},
		ResultSummary: task.ResultSummary,
	}
	if task.SessionID != nil {
		artifacts, err := s.collectSessionArtifacts(ctx, *task.SessionID)
		if err != nil {
			return nil, err
		}
		result.Artifacts = artifacts
	}

	files, err := db.ListProjectFilesByTask(ctx, s.db, caller.OrgID, project.ID, task.ID, string(types.ProjectFileResourceTypeArtifact))
	if err != nil {
		return nil, fmt.Errorf("list task files: %w", err)
	}
	seenPaths := make(map[string]struct{}, len(files))
	for _, file := range files {
		key := file.RelativePath
		if key == "" {
			key = file.FilePublicID
		}
		if _, exists := seenPaths[key]; exists {
			continue
		}
		seenPaths[key] = struct{}{}
		result.Files = append(result.Files, contract.TaskDeliverableFile{
			FilePublicID: file.FilePublicID,
			RelativePath: file.RelativePath,
			ResourceType: string(file.ResourceType),
			VersionNo:    file.VersionNo,
			UpdatedAt:    file.UpdatedAt,
		})
	}
	return result, nil
}

func (s *taskService) collectSessionArtifacts(ctx context.Context, sessionID uint) ([]types.MessageArtifact, error) {
	var messages []types.SessionMessage
	if err := s.db.WithContext(ctx).
		Where("session_id = ? AND role = ?", sessionID, string(types.MessageRoleAssistant)).
		Order("sequence ASC").
		Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("list task session artifacts: %w", err)
	}
	seen := make(map[string]struct{})
	result := make([]types.MessageArtifact, 0)
	for _, message := range messages {
		for _, artifact := range message.Artifacts {
			key := artifact.ArtifactID
			if key == "" {
				key = artifact.StorageURI
			}
			if key == "" {
				key = artifact.Filename + "|" + artifact.Title
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, artifact)
		}
	}
	return result, nil
}

func (s *taskService) AcceptTask(ctx context.Context, taskID, note string) (*contract.Task, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	var task *types.Task
	var project *types.Project
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, project, err = s.loadTaskWithProjectTx(ctx, tx, caller, taskID)
		if err != nil {
			return err
		}
		if task.Status != string(types.TaskStatusPendingAcceptance) {
			return ErrTaskNotPendingAcceptance
		}
		now := time.Now().UTC()
		task.Status = string(types.TaskStatusCompleted)
		task.AcceptedBy = &caller.Uin
		task.AcceptedAt = &now
		task.AcceptanceNote = strings.TrimSpace(note)
		return db.UpdateTask(ctx, tx, task)
	}); err != nil {
		return nil, err
	}
	return convertToContractTask(task, project.PublicID, project.Name), nil
}

func (s *taskService) RejectTask(ctx context.Context, taskID, note string) (*contract.Task, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return nil, errors.New("acceptance note is required")
	}
	var task *types.Task
	var project *types.Project
	var previousNote string
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, project, err = s.loadTaskWithProjectTx(ctx, tx, caller, taskID)
		if err != nil {
			return err
		}
		if task.Status != string(types.TaskStatusPendingAcceptance) {
			return ErrTaskNotPendingAcceptance
		}
		previousNote = task.AcceptanceNote
		task.Status = string(types.TaskStatusInProgress)
		task.AcceptedBy = nil
		task.AcceptedAt = nil
		task.AcceptanceNote = note
		return db.UpdateTask(ctx, tx, task)
	}); err != nil {
		return nil, err
	}

	if err := s.notifyTaskRework(ctx, task, note); err != nil {
		// 返工消息没有成功投递时回到待验收，避免任务卡在 in_progress 且验收入口消失。
		revertErr := s.db.WithContext(ctx).Model(&types.Task{}).
			Where("id = ? AND status = ?", task.ID, string(types.TaskStatusInProgress)).
			Updates(map[string]interface{}{
				"status":          string(types.TaskStatusPendingAcceptance),
				"acceptance_note": previousNote,
			}).Error
		if revertErr != nil {
			logs.WarnContextf(ctx, "revert rejected task %s after rework message failure: %v", task.PublicID, revertErr)
		}
		return nil, err
	}
	return convertToContractTask(task, project.PublicID, project.Name), nil
}

func (s *taskService) notifyTaskRework(ctx context.Context, task *types.Task, note string) error {
	if s.sessions == nil || task == nil || task.SessionID == nil {
		return nil
	}
	session, err := db.GetSessionByID(ctx, s.db, *task.SessionID)
	if err != nil {
		return fmt.Errorf("load task session for rework: %w", err)
	}
	if session == nil {
		return nil
	}
	content := fmt.Sprintf("【验收未通过】%s\n请根据以上意见修改并重新交付。", note)
	if _, err := s.sessions.AddMessage(ctx, session.PublicID, &contract.AddMessageRequest{
		Role:    string(types.MessageRoleUser),
		Content: content,
	}); err != nil {
		return fmt.Errorf("task rejected but rework message failed: %w", err)
	}
	return nil
}

func (s *taskService) loadTaskWithProject(ctx context.Context, caller *types.Caller, taskID string) (*types.Task, *types.Project, error) {
	return s.loadTaskWithProjectTx(ctx, s.db, caller, taskID)
}

func (s *taskService) loadTaskWithProjectTx(ctx context.Context, database *gorm.DB, caller *types.Caller, taskID string) (*types.Task, *types.Project, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, nil, errors.New("task_id is required")
	}
	task, err := db.GetTaskByPublicID(ctx, database, caller.OrgID, taskID)
	if err != nil {
		return nil, nil, err
	}
	if task == nil {
		return nil, nil, errors.New("task not found")
	}
	project, err := db.GetProjectByID(ctx, database, task.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	if project == nil {
		return nil, nil, errors.New("project not found")
	}
	if err := s.permWithDB(database).RequireProject(ctx, FromTypeCaller(caller), project, types.ActionProjectView); err != nil {
		return nil, nil, err
	}
	return task, project, nil
}

var _ contract.TaskService = (*taskService)(nil)
