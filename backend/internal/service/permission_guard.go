// Package service 权限守卫：Handler PermGuard 作声明式快速失败；Service Require* 为权威 Gate。
// RequireCallerOrg 注入 request-scoped eval cache，HTTP 双检共享命中。
// 两者均委托 PermissionService.Can / BatchCan / guardActions。
package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

// GuardCheck 描述单次资源权限检查所需的三元组。
// 供 GuardAll 批量检查多资源、多动作的场景使用。
type GuardCheck struct {
	ResourceType types.ResourceType
	PublicID     string
	Action       Action
}

// FromTypeCaller 将 types.Caller 转换为 PermissionService 所需的 Caller（types.PermissionCaller）。
// handler 层从 gin.Context 取出 *types.Caller 后调用此函数，避免重复手写字段映射。
func FromTypeCaller(c *types.Caller) Caller {
	if c == nil {
		return Caller{}
	}
	caller := Caller{
		OrgID: c.OrgID,
		Uin:   c.Uin,
	}
	if c.Kind == types.CallerKindWorker && c.WorkerID != 0 {
		caller.AssistantID = c.WorkerID
	}
	return caller
}

// GuardProject 校验 caller 对指定项目（通过 publicID 定位）是否拥有所有给定 action 的权限。
// 项目仅查询一次数据库，对多个 action 使用 BatchCan 一次完成校验，避免 N 次独立查询。
// 任何一个 action 被拒绝即返回 "permission denied: <reason>" 错误。
func (s *PermissionService) GuardProject(ctx context.Context, caller Caller, publicID string, actions ...Action) error {
	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	if project == nil {
		return fmt.Errorf("project not found")
	}
	return s.guardActions(ctx, caller, ResourceRef{
		Type:  types.ResourceTypeProject,
		BizID: project.ID,
	}, actions...)
}

// GuardTask 校验 caller 对指定任务（通过 publicID 定位）是否拥有所有给定 action 的权限。
// 任务仅查询一次数据库，对多个 action 使用 BatchCan 一次完成校验。
func (s *PermissionService) GuardTask(ctx context.Context, caller Caller, publicID string, actions ...Action) error {
	task, err := db.GetTaskByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return fmt.Errorf("load task: %w", err)
	}
	if task == nil {
		return fmt.Errorf("task not found")
	}
	return s.guardActions(ctx, caller, ResourceRef{
		Type:  types.ResourceTypeTask,
		BizID: task.ID,
	}, actions...)
}

// GuardByPublicID 通过资源类型分发到对应的 Guard 方法，支持 PermGuard 中间件在不知道资源类型的
// 情况下统一调用。task:create 的资源定位点是项目，但策略应按项目下的任务动作解释。
// 当前支持 project 和 task；未知类型返回错误。
func (s *PermissionService) GuardByPublicID(ctx context.Context, caller Caller, resourceType types.ResourceType, publicID string, actions ...Action) error {
	switch resourceType {
	case types.ResourceTypeProject:
		projectActions := make([]Action, 0, len(actions))
		projectTaskActions := make([]Action, 0, len(actions))
		for _, action := range actions {
			if action == ActionTaskCreate {
				projectTaskActions = append(projectTaskActions, action)
				continue
			}
			projectActions = append(projectActions, action)
		}
		if len(projectActions) > 0 {
			if err := s.GuardProject(ctx, caller, publicID, projectActions...); err != nil {
				return err
			}
		}
		for _, action := range projectTaskActions {
			if err := s.GuardProjectTaskAction(ctx, caller, publicID, action); err != nil {
				return err
			}
		}
		return nil
	case types.ResourceTypeTask:
		return s.GuardTask(ctx, caller, publicID, actions...)
	default:
		return fmt.Errorf("GuardByPublicID: unsupported resource type %q", resourceType)
	}
}

// GuardAll 依次校验多个资源/动作组合，全部通过才返回 nil。
// 遇到第一个不通过的检查即短路返回，不继续后续检查。
// 适用于单个 handler 需要跨资源类型同时校验多个权限的场景（如 DetailProject 同时检查
// project:view 和 project:member.list）。
func (s *PermissionService) GuardAll(ctx context.Context, caller Caller, checks []GuardCheck) error {
	for _, check := range checks {
		if err := s.GuardByPublicID(ctx, caller, check.ResourceType, check.PublicID, check.Action); err != nil {
			return err
		}
	}
	return nil
}

// RequireProject 校验 caller 对已加载项目实体是否拥有所有给定 action 的权限。
func (s *PermissionService) RequireProject(ctx context.Context, caller Caller, project *types.Project, actions ...Action) error {
	if project == nil {
		return fmt.Errorf("project not found")
	}
	return s.guardActions(ctx, caller, ResourceRef{
		Type:  types.ResourceTypeProject,
		BizID: project.ID,
	}, actions...)
}

// RequireProjectByID 按项目内部 ID 校验 caller 是否拥有所有给定 action 的权限。
func (s *PermissionService) RequireProjectByID(ctx context.Context, caller Caller, projectID uint, actions ...Action) error {
	if projectID == 0 {
		return fmt.Errorf("project not found")
	}
	return s.guardActions(ctx, caller, ResourceRef{
		Type:  types.ResourceTypeProject,
		BizID: projectID,
	}, actions...)
}

// RequireTask 校验 caller 对已加载任务实体是否拥有所有给定 action 的权限。
func (s *PermissionService) RequireTask(ctx context.Context, caller Caller, task *types.Task, actions ...Action) error {
	if task == nil {
		return fmt.Errorf("task not found")
	}
	return s.guardActions(ctx, caller, ResourceRef{
		Type:  types.ResourceTypeTask,
		BizID: task.ID,
	}, actions...)
}

// RequireProjectFile 校验 caller 对项目文件/产物是否拥有指定 action 权限。
func (s *PermissionService) RequireProjectFile(ctx context.Context, caller Caller, pf *types.ProjectFile, action Action) error {
	if pf == nil {
		return fmt.Errorf("project file not found")
	}
	if !isProjectFileActionCompatible(pf, action) {
		return fmt.Errorf("permission denied: %s", reasonPolicyDenied)
	}
	ref, err := projectFileResourceRef(pf)
	if err != nil {
		return err
	}
	return s.guardActions(ctx, caller, ref, action)
}

// RequireProjectMember 校验 caller 对项目成员管理类 action 是否被允许。
// input 必须由后端根据数据库派生，不允许前端传入。
func (s *PermissionService) RequireProjectMember(ctx context.Context, caller Caller, projectID uint, action Action, input *MemberInput) error {
	if caller.OrgID == 0 {
		return fmt.Errorf("user not authenticated or org not set")
	}
	decision, err := s.Can(ctx, caller, action, ResourceRef{
		Type:  types.ResourceTypeProject,
		BizID: projectID,
	}, input)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("permission denied: %s", decision.Reason)
	}
	return nil
}

// AllowsTaskActionViaProject 判断 caller 在项目上的 effective role 是否允许对子任务执行 action。
func (s *PermissionService) AllowsTaskActionViaProject(ctx context.Context, caller Caller, project *types.Project, action Action) (bool, error) {
	if project == nil {
		return false, fmt.Errorf("project not found")
	}
	resource, err := db.GetResourceByBizID(ctx, s.db, caller.OrgID, types.ResourceTypeProject, project.ID)
	if err != nil {
		return false, fmt.Errorf("load project resource: %w", err)
	}
	if resource == nil {
		return false, nil
	}
	effectiveRole, _, _, err := s.ResolveEffectiveRole(ctx, caller, resource)
	if err != nil {
		return false, fmt.Errorf("resolve effective role: %w", err)
	}
	if effectiveRole == "" {
		return false, nil
	}
	return SystemPolicy.Allows(types.ResourceTypeTask, effectiveRole, action), nil
}

// AllowsTaskViewViaProject 判断 caller 在项目上的 effective role 是否允许查看其下所有任务。
func (s *PermissionService) AllowsTaskViewViaProject(ctx context.Context, caller Caller, project *types.Project) (bool, error) {
	return s.AllowsTaskActionViaProject(ctx, caller, project, ActionTaskView)
}

// RequireProjectTaskAction 校验 caller 是否能在项目下执行 task 类 action（任务资源尚不存在）。
func (s *PermissionService) RequireProjectTaskAction(ctx context.Context, caller Caller, project *types.Project, action Action) error {
	allowed, err := s.AllowsTaskActionViaProject(ctx, caller, project, action)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("permission denied: %s", reasonPolicyDenied)
	}
	return nil
}

// GuardSessionAccess 校验 caller 是否可访问指定 session（HTTP 入口门控）。
func (s *PermissionService) GuardSessionAccess(ctx context.Context, caller Caller, sessionPublicID string) error {
	session, err := db.GetSessionByPublicID(ctx, s.db, sessionPublicID)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session not found")
	}
	if session.OrgID != caller.OrgID {
		return fmt.Errorf("permission denied")
	}
	if caller.AssistantID != 0 {
		if session.ProjectID == nil || *session.ProjectID == 0 {
			return fmt.Errorf("permission denied")
		}
		ok, err := db.IsProjectAssistantBound(ctx, s.db, caller.OrgID, *session.ProjectID, caller.AssistantID)
		if err != nil {
			return fmt.Errorf("check project binding: %w", err)
		}
		if !ok {
			return fmt.Errorf("permission denied")
		}
		return nil
	}
	if (session.Type == types.SessionTypeTask || session.Type == types.SessionTypeProject) &&
		session.ProjectID != nil && *session.ProjectID > 0 {
		return s.RequireProjectByID(ctx, caller, *session.ProjectID, types.ActionProjectView)
	}
	if caller.Uin != session.Uin {
		return fmt.Errorf("permission denied")
	}
	return nil
}

// GuardSessionAccessByMessageID 通过 message_id 反查 session 并校验访问权限。
func (s *PermissionService) GuardSessionAccessByMessageID(ctx context.Context, caller Caller, messageID uint) error {
	if messageID == 0 {
		return fmt.Errorf("message_id is required")
	}
	message, err := db.GetMessageByID(ctx, s.db, messageID)
	if err != nil {
		return fmt.Errorf("load message: %w", err)
	}
	if message == nil {
		return fmt.Errorf("message not found")
	}
	session, err := db.GetSessionByID(ctx, s.db, message.SessionID)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session not found")
	}
	return s.GuardSessionAccess(ctx, caller, session.PublicID)
}

// GuardNewMessageRequest 校验 CreateInitialMessage 请求的资源权限。
func (s *PermissionService) GuardNewMessageRequest(ctx context.Context, caller Caller, projectID, taskID string) error {
	projectID = strings.TrimSpace(projectID)
	taskID = strings.TrimSpace(taskID)

	if taskID != "" {
		if err := s.GuardTask(ctx, caller, taskID, ActionTaskView); err != nil {
			return err
		}
	}
	if projectID != "" {
		if err := s.GuardProject(ctx, caller, projectID, ActionProjectView); err != nil {
			return err
		}
		if taskID == "" {
			if err := s.GuardProjectTaskAction(ctx, caller, projectID, ActionTaskCreate); err != nil {
				return err
			}
		}
	}
	return nil
}

// GuardProjectTaskAction 校验 caller 对指定项目是否能在其下执行 task 类 action。
func (s *PermissionService) GuardProjectTaskAction(ctx context.Context, caller Caller, publicID string, action Action) error {
	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	if project == nil {
		return fmt.Errorf("project not found")
	}
	return s.RequireProjectTaskAction(ctx, caller, project, action)
}

// FilterProjectFilesByAction 批量过滤 caller 有权访问的项目文件。
func (s *PermissionService) FilterProjectFilesByAction(ctx context.Context, caller Caller, files []types.ProjectFile, actionForFile func(*types.ProjectFile) Action) []types.ProjectFile {
	if len(files) == 0 {
		return nil
	}

	filesByType := make(map[types.ResourceType][]uint)
	for i := range files {
		ref, err := projectFileResourceRef(&files[i])
		if err != nil {
			continue
		}
		filesByType[ref.Type] = append(filesByType[ref.Type], ref.BizID)
	}

	resourceByKey := make(map[types.ResourceType]map[uint]*types.Resource)
	for resourceType, bizIDs := range filesByType {
		unique := uniqueUints(bizIDs)
		resources, err := db.ListResourcesByBizIDs(ctx, s.db, caller.OrgID, resourceType, unique)
		if err != nil {
			continue
		}
		m := make(map[uint]*types.Resource, len(resources))
		for _, resource := range resources {
			m[resource.BizID] = resource
		}
		resourceByKey[resourceType] = m
	}

	roleCache := make(map[uint]types.ResourceRole)
	authorized := make([]types.ProjectFile, 0, len(files))
	for i := range files {
		pf := &files[i]
		if strings.HasSuffix(strings.TrimSpace(pf.RelativePath), "/") {
			authorized = append(authorized, files[i])
			continue
		}
		action := actionForFile(pf)
		if !isProjectFileActionCompatible(pf, action) {
			continue
		}
		ref, err := projectFileResourceRef(pf)
		if err != nil {
			continue
		}
		resourceMap := resourceByKey[ref.Type]
		if resourceMap == nil {
			continue
		}
		resource := resourceMap[ref.BizID]
		if resource == nil {
			continue
		}
		effectiveRole, ok := roleCache[resource.ID]
		if !ok {
			role, _, _, resolveErr := s.ResolveEffectiveRole(ctx, caller, resource)
			if resolveErr != nil {
				continue
			}
			effectiveRole = role
			roleCache[resource.ID] = effectiveRole
		}
		if effectiveRole == "" {
			continue
		}
		if !SystemPolicy.Allows(resource.Type, effectiveRole, action) {
			continue
		}
		authorized = append(authorized, files[i])
	}
	return authorized
}

func uniqueUints(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// guardActions 对同一资源的多个 action 批量校验（单次加载 resource）。
func (s *PermissionService) guardActions(ctx context.Context, caller Caller, ref ResourceRef, actions ...Action) error {
	if len(actions) == 0 {
		return nil
	}

	refs := make([]ResourceRef, len(actions))
	for i := range actions {
		refs[i] = ref
	}

	var input *MemberInput
	for _, action := range actions {
		if input = memberInputForGuardAction(action, caller); input != nil {
			break
		}
	}

	decisions, err := s.BatchCan(ctx, caller, actions, refs, input)
	if err != nil {
		return fmt.Errorf("permission check: %w", err)
	}
	for i, decision := range decisions {
		if !decision.Allowed {
			return fmt.Errorf("permission denied: %s on %s", decision.Reason, actions[i])
		}
	}
	return nil
}

func memberInputForGuardAction(action Action, caller Caller) *MemberInput {
	if action == ActionProjectMemberLeave && caller.Uin != 0 {
		return &MemberInput{TargetUin: caller.Uin}
	}
	return nil
}
