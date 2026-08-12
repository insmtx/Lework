package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
)

var (
	// ErrAutomationNotFound 表示自动化不存在或无权访问
	ErrAutomationNotFound = errors.New("automation not found")
	// ErrAutomationForbidden 表示无权访问该自动化
	ErrAutomationForbidden = errors.New("automation forbidden")
	// ErrAutomationLinkNotFound 表示关联项目不存在或不属于当前组织
	ErrAutomationLinkNotFound = errors.New("automation link project not found")
	// ErrAutomationLinkForbidden 表示当前用户在关联项目上无 task:create 权限
	ErrAutomationLinkForbidden = errors.New("automation link project forbidden")
	// ErrAutomationLinkUnavailable 表示固定 AI 队友无法在关联项目中执行
	ErrAutomationLinkUnavailable = errors.New("automation link project unavailable")
	// ErrAutomationProjectChangeConflict 表示存在活动执行时更换关联项目
	ErrAutomationProjectChangeConflict = errors.New("automation_project_change_conflict")
)

type automationService struct {
	db   *gorm.DB
	perm *PermissionService
}

// NewAutomationService 构造自动化服务
func NewAutomationService(db *gorm.DB, perm *PermissionService) contract.AutomationService {
	return &automationService{db: db, perm: perm}
}

// generateAutomationPublicID 生成自动化对外 ID
func generateAutomationPublicID() string {
	return fmt.Sprintf("auto_%s", snowflake.GenerateIDBase58())
}

// newAutoExecPublicID 生成执行记录对外 ID
func newAutoExecPublicID() string {
	return fmt.Sprintf("autoexec_%s", snowflake.GenerateIDBase58())
}

// CreateAutomation 创建自动化计划
func (s *automationService) CreateAutomation(ctx context.Context, req *contract.CreateAutomationRequest) (*contract.Automation, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 50 {
		return nil, errors.New("invalid automation name")
	}
	instruction := strings.TrimSpace(req.Instruction)
	if instruction == "" {
		return nil, errors.New("invalid automation instruction")
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	// 编译并校验调度规则（含时区、锚点校验），mode 以 schedule.mode 为权威来源
	spec, err := compileScheduleSpec(req.Schedule, req.Timezone, req.ScheduleMode)
	if err != nil {
		return nil, err
	}

	// 启用时必须在保存前成功计算出下一次执行时间，避免保存启用态却 next_run_at 为空
	nextRunAt := computeNextRunAt(spec, enabled, time.Now().UTC())
	if enabled && nextRunAt == nil {
		return nil, errInvalidAutomationSchedule
	}
	timezone := spec.Spec.Timezone
	mode := spec.Spec.Mode

	// 解析组织默认 AI 队友
	assistantID, err := db.GetDefaultAssistantIDByOrg(ctx, s.db, caller.OrgID)
	if err != nil {
		return nil, err
	}
	if assistantID == 0 {
		return nil, ErrNoDefaultAssistantInOrg
	}

	// 可选：显式关联既有项目（先校验权限与队友绑定，再设置关联）
	var linkProjectID *uint
	if strings.TrimSpace(req.ProjectPublicID) != "" {
		proj, linkErr := s.resolveLinkProject(ctx, caller, req.ProjectPublicID, assistantID)
		if linkErr != nil {
			return nil, linkErr
		}
		id := proj.ID
		linkProjectID = &id
	}

	automation := &types.Automation{
		OrgID:        caller.OrgID,
		OwnerID:      caller.Uin,
		PublicID:     generateAutomationPublicID(),
		Name:         name,
		Instruction:  instruction,
		ScheduleMode: mode,
		ScheduleSpec: *spec,
		Timezone:     timezone,
		AssistantID:  assistantID,
		NextRunAt:    nextRunAt,
		ProjectID:    linkProjectID, // 关联既有项目；否则 nil（首次执行懒创建）
	}
	automation.SetEnabled(enabled)
	if err := db.CreateAutomation(ctx, s.db, automation); err != nil {
		return nil, err
	}

	out := toContractAutomation(automation)
	s.enrichAutomation(ctx, automation, out)
	return out, nil
}

// GetAutomation 查询自动化详情
func (s *automationService) GetAutomation(ctx context.Context, publicID string) (*contract.Automation, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	automation, err := db.GetAutomationByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if automation == nil {
		return nil, ErrAutomationNotFound
	}
	if err := s.checkOwner(caller, automation); err != nil {
		return nil, err
	}
	out := toContractAutomation(automation)
	s.enrichAutomation(ctx, automation, out)
	return out, nil
}

// UpdateAutomation 更新自动化（部分更新）
func (s *automationService) UpdateAutomation(ctx context.Context, publicID string, req *contract.UpdateAutomationRequest) (*contract.Automation, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	// 关联目标在写事务前预校验；任何失败均发生在配置字段写入之前。
	// 写事务内仍会锁定 automation 并检查活动执行，保证最终提交原子。
	var requestedProjectID *uint
	if req.ProjectPublicID != nil && strings.TrimSpace(*req.ProjectPublicID) != "" {
		current, loadErr := db.GetAutomationByPublicID(ctx, s.db, caller.OrgID, publicID)
		if loadErr != nil {
			return nil, loadErr
		}
		if current == nil {
			return nil, ErrAutomationNotFound
		}
		if ownerErr := s.checkOwner(caller, current); ownerErr != nil {
			return nil, ownerErr
		}
		proj, resolveErr := s.resolveLinkProject(ctx, caller, *req.ProjectPublicID, current.AssistantID)
		if resolveErr != nil {
			return nil, resolveErr
		}
		id := proj.ID
		requestedProjectID = &id
	}

	// 配置字段与关联项目必须在同一事务内更新：关联校验或活动执行冲突时，不能留下半次更新。
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var automation types.Automation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("org_id = ? AND public_id = ?", caller.OrgID, publicID).First(&automation).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAutomationNotFound
			}
			return err
		}
		if err := s.checkOwner(caller, &automation); err != nil {
			return err
		}
		if err := applyAutomationUpdate(&automation, req); err != nil {
			return err
		}

		targetProjectID := requestedProjectID
		projectChanged := false
		projectGeneration := automation.ProjectGeneration
		if req.ProjectPublicID != nil {
			projectChanged = (targetProjectID == nil && automation.ProjectID != nil) ||
				(targetProjectID != nil && (automation.ProjectID == nil || *targetProjectID != *automation.ProjectID))
			if projectChanged {
				active, activeErr := db.HasActiveExecution(ctx, tx, automation.ID)
				if activeErr != nil {
					return activeErr
				}
				if active {
					return ErrAutomationProjectChangeConflict
				}
				if targetProjectID == nil {
					generation, generationErr := db.MaxAutomationProjectGeneration(ctx, tx, automation.OrgID, automation.ID)
					if generationErr != nil {
						return generationErr
					}
					projectGeneration = generation
				} else {
					projectGeneration = 0
				}
			}
		}

		if err := db.UpdateAutomation(ctx, tx, &automation); err != nil {
			return err
		}
		if projectChanged {
			return db.UpdateAutomationProjectLink(ctx, tx, automation.ID, targetProjectID, projectGeneration)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	automation, err := db.GetAutomationByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if automation == nil {
		return nil, ErrAutomationNotFound
	}
	out := toContractAutomation(automation)
	s.enrichAutomation(ctx, automation, out)
	return out, nil
}

// applyAutomationUpdate 将请求中的非关联配置应用到 automation；不写数据库，供事务统一提交。
func applyAutomationUpdate(automation *types.Automation, req *contract.UpdateAutomationRequest) error {
	if strings.TrimSpace(req.Name) != "" {
		if len([]rune(req.Name)) > 50 {
			return errors.New("invalid automation name")
		}
		automation.Name = strings.TrimSpace(req.Name)
	}
	if req.Instruction != nil {
		if strings.TrimSpace(*req.Instruction) == "" {
			return errors.New("invalid automation instruction")
		}
		automation.Instruction = strings.TrimSpace(*req.Instruction)
	}

	scheduleChanged := false
	if req.Schedule != nil {
		topMode := ""
		if req.ScheduleMode != nil {
			topMode = *req.ScheduleMode
		}
		newSpec, err := compileScheduleSpec(req.Schedule, automation.Timezone, topMode)
		if err != nil {
			return err
		}
		automation.ScheduleSpec = *newSpec
		automation.ScheduleMode = newSpec.Spec.Mode
		automation.Timezone = newSpec.Spec.Timezone
		scheduleChanged = true
	} else if req.Timezone != nil && *req.Timezone != "" {
		if _, err := validateTimezone(*req.Timezone); err != nil {
			return err
		}
		automation.Timezone = *req.Timezone
		automation.ScheduleSpec.Spec.Timezone = *req.Timezone
		if automation.ScheduleSpec.FormConfig != nil {
			automation.ScheduleSpec.FormConfig.Timezone = *req.Timezone
		}
		scheduleChanged = true
	}
	if req.Enabled != nil && *req.Enabled != automation.IsEnabled() {
		automation.SetEnabled(*req.Enabled)
		scheduleChanged = true
	}
	if !scheduleChanged {
		return nil
	}
	nextRunAt := computeNextRunAt(&automation.ScheduleSpec, automation.IsEnabled(), time.Now().UTC())
	if automation.IsEnabled() && nextRunAt == nil {
		return errInvalidAutomationSchedule
	}
	automation.NextRunAt = nextRunAt
	return nil
}

// DeleteAutomation 软删除自动化
func (s *automationService) DeleteAutomation(ctx context.Context, publicID string) error {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return err
	}
	automation, err := db.GetAutomationByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return err
	}
	if automation == nil {
		return ErrAutomationNotFound
	}
	if err := s.checkOwner(caller, automation); err != nil {
		return err
	}
	return db.DeleteAutomationByPublicID(ctx, s.db, caller.OrgID, publicID)
}

// ListAutomations 分页查询当前用户自动化列表
func (s *automationService) ListAutomations(ctx context.Context, req *contract.ListAutomationsRequest) (*contract.AutomationList, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	req.Fill()

	opts := db.ListAutomationOptions{
		Offset: req.Offset,
		Limit:  req.Limit,
	}
	if req.Keyword != nil {
		opts.Keyword = *req.Keyword
	}
	if req.Enabled != nil {
		opts.Enabled = req.Enabled
	}
	if req.ScheduleMode != nil {
		opts.ScheduleMode = *req.ScheduleMode
	}

	entities, total, err := db.ListAutomations(ctx, s.db, caller.OrgID, caller.Uin, opts)
	if err != nil {
		return nil, err
	}

	// 批量预载关联项目摘要，避免逐条回填 project_* 造成 N+1
	projMap := s.loadProjectSummaries(ctx, entities)

	items := make([]contract.Automation, 0, len(entities))
	for _, e := range entities {
		item := toContractAutomation(e)
		s.enrichAutomationWithProjects(ctx, e, item, projMap)
		items = append(items, *item)
	}
	return &contract.AutomationList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

// loadProjectSummaries 一次性按 id 批量加载 entities 涉及的项目，返回 id -> project 映射（供列表回填）。
func (s *automationService) loadProjectSummaries(ctx context.Context, entities []*types.Automation) map[uint]*types.Project {
	m := make(map[uint]*types.Project)
	var ids []uint
	seen := make(map[uint]bool)
	for _, e := range entities {
		if e == nil || e.ProjectID == nil {
			continue
		}
		if !seen[*e.ProjectID] {
			seen[*e.ProjectID] = true
			ids = append(ids, *e.ProjectID)
		}
	}
	if len(ids) == 0 {
		return m
	}
	projects, err := db.GetProjectsByIDs(ctx, s.db, ids)
	if err != nil {
		return m
	}
	for _, p := range projects {
		if p != nil {
			m[p.ID] = p
		}
	}
	return m
}

// checkOwner 校验自动化归属
func (s *automationService) checkOwner(caller *types.Caller, automation *types.Automation) error {
	if caller.Uin != automation.OwnerID {
		return ErrAutomationForbidden
	}
	return nil
}

// resolveLinkProject 校验并解析关联的既有项目。
//
// 依次校验：
//   - 项目存在且属于调用者组织（GetProjectByPublicID 已按 org 过滤）；否则 ErrAutomationLinkNotFound。
//   - 调用者在项目下拥有 task:create 权限；否则 ErrAutomationLinkForbidden。
//   - 固定 AI 队友（assistantID）已绑定到项目；否则 ErrAutomationLinkUnavailable。
//
// 返回项目，供调用方取 ID 及回填响应。
func (s *automationService) resolveLinkProject(ctx context.Context, caller *types.Caller, projectPublicID string, assistantID uint) (*types.Project, error) {
	proj, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, projectPublicID)
	if err != nil {
		return nil, err
	}
	if proj == nil {
		return nil, ErrAutomationLinkNotFound
	}
	if s.perm != nil {
		if perr := s.perm.RequireProjectTaskAction(ctx, types.PermissionCaller{OrgID: caller.OrgID, Uin: caller.Uin}, proj, types.ActionTaskCreate); perr != nil {
			return nil, ErrAutomationLinkForbidden
		}
	}
	bound, berr := db.IsProjectAssistantBound(ctx, s.db, caller.OrgID, proj.ID, assistantID)
	if berr != nil {
		return nil, berr
	}
	if !bound {
		return nil, ErrAutomationLinkUnavailable
	}
	return proj, nil
}

// RunAutomationNow 手动触发一次执行（立即运行）。
//
// 停用状态也可调用；不修改 next_run_at；有活动 execution 时返回冲突错误。
func (s *automationService) RunAutomationNow(ctx context.Context, publicID string) (*contract.AutomationExecution, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	automation, err := db.GetAutomationByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if automation == nil {
		return nil, ErrAutomationNotFound
	}
	if err := s.checkOwner(caller, automation); err != nil {
		return nil, err
	}

	var created *types.AutomationExecution
	now := time.Now().UTC()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		active, actErr := db.HasActiveExecution(ctx, tx, automation.ID)
		if actErr != nil {
			return actErr
		}
		if active {
			return ErrAutomationRunInProgress
		}
		notAfter := now.Add(defaultDispatchTimeout)
		execution := &types.AutomationExecution{
			OrgID:               automation.OrgID,
			AutomationID:        automation.ID,
			OwnerID:             automation.OwnerID,
			PublicID:            "autoexec_" + newAutoExecPublicID(),
			TriggerType:         types.AutomationTriggerManual,
			OccurrenceKey:       "manual_" + newAutoExecPublicID(),
			Status:              types.AutomationExecutionQueued,
			ScheduledAt:         now,
			NotAfter:            &notAfter,
			NameSnapshot:        automation.Name,
			InstructionSnapshot: automation.Instruction,
			AssistantIDSnapshot: automation.AssistantID,
		}
		if createErr := db.CreateAutomationExecution(ctx, tx, execution); createErr != nil {
			return createErr
		}
		created = execution
		return nil
	})
	if err != nil {
		return nil, err
	}
	return toContractExecution(created), nil
}

// ListAutomationExecutions 分页查询某自动化的执行历史。
func (s *automationService) ListAutomationExecutions(ctx context.Context, req *contract.ListAutomationExecutionsRequest) (*contract.AutomationExecutionList, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	automation, err := db.GetAutomationByPublicID(ctx, s.db, caller.OrgID, req.PublicID)
	if err != nil {
		return nil, err
	}
	if automation == nil {
		return nil, ErrAutomationNotFound
	}
	if err := s.checkOwner(caller, automation); err != nil {
		return nil, err
	}

	req.Fill()
	var status *string
	if req.Status != nil {
		status = req.Status
	}
	entities, total, err := db.ListAutomationExecutions(ctx, s.db, automation.ID, status, req.Offset, req.Limit)
	if err != nil {
		return nil, err
	}
	items := make([]contract.AutomationExecution, 0, len(entities))
	for _, ex := range entities {
		item := toContractExecution(ex)
		s.enrichExecutionLinks(ctx, ex, item)
		items = append(items, *item)
	}
	return &contract.AutomationExecutionList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

// GetAutomationExecution 查询单条执行详情
func (s *automationService) GetAutomationExecution(ctx context.Context, publicID string) (*contract.AutomationExecution, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	ex, err := db.GetAutomationExecutionByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if ex == nil {
		return nil, ErrExecutionNotFound
	}
	out := toContractExecution(ex)
	s.enrichExecutionLinks(ctx, ex, out)
	return out, nil
}

// enrichExecutionLinks 按主键查询关联实体的 public_id，填入 execution 响应的链接字段。
// 仅展示用，不落库。
func (s *automationService) enrichExecutionLinks(ctx context.Context, ex *types.AutomationExecution, out *contract.AutomationExecution) {
	if ex == nil || out == nil {
		return
	}
	if ex.ProjectID != nil {
		if p, err := db.GetProjectByID(ctx, s.db, *ex.ProjectID); err == nil && p != nil {
			out.ProjectPublicID = p.PublicID
		}
	}
	if ex.TaskID != nil {
		if t, err := db.GetTaskByID(ctx, s.db, ex.OrgID, *ex.TaskID); err == nil && t != nil {
			out.TaskPublicID = t.PublicID
			// SessionID 优先用 execution 记录的；历史数据可能缺失，回退从 Task.SessionID 反查。
			effSessionID := ex.SessionID
			if effSessionID == nil && t.SessionID != nil {
				effSessionID = t.SessionID
			}
			if effSessionID != nil {
				if s2, err := db.GetSessionByID(ctx, s.db, *effSessionID); err == nil && s2 != nil {
					out.SessionPublicID = s2.PublicID
					out.SessionID = effSessionID
				}
			}
		}
	}
}

// toContractAutomation 将数据库模型转换为响应结构
func toContractAutomation(a *types.Automation) *contract.Automation {
	out := &contract.Automation{
		PublicID:     a.PublicID,
		OrgID:        a.OrgID,
		OwnerID:      a.OwnerID,
		Name:         a.Name,
		Instruction:  a.Instruction,
		Enabled:      a.IsEnabled(),
		ScheduleMode: a.ScheduleMode,
		Timezone:     a.Timezone,
		AssistantID:  a.AssistantID,
		NextRunAt:    a.NextRunAt,
		ProjectID:    a.ProjectID,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
	}
	// 返回调度配置副本，避免共享底层指针
	spec := a.ScheduleSpec
	out.ScheduleSpec = &spec
	out.Summary = buildAutomationSummary(&spec)
	return out
}

// enrichAutomationExecutions 填充自动化的执行状态聚合（最近执行、活跃标识、任务入口）。
func (s *automationService) enrichAutomation(ctx context.Context, a *types.Automation, out *contract.Automation) {
	if a == nil || out == nil {
		return
	}
	s.enrichAutomationWithProjects(ctx, a, out, nil)
}

// enrichAutomationWithProjects 在 enrichAutomation 基础上，回填关联项目摘要。
//
// projMap 可为 nil（单条详情场景，逐条查一次项目），也可由 loadProjectSummaries 提供（列表批量，避免 N+1）。
func (s *automationService) enrichAutomationWithProjects(ctx context.Context, a *types.Automation, out *contract.Automation, projMap map[uint]*types.Project) {
	if a == nil || out == nil {
		return
	}
	// 回填关联项目的 public_id / name（展示用，不落库）
	if a.ProjectID != nil {
		var p *types.Project
		if projMap != nil {
			p = projMap[*a.ProjectID]
		} else {
			p, _ = db.GetProjectByID(ctx, s.db, *a.ProjectID)
		}
		if p != nil {
			out.ProjectPublicID = p.PublicID
			out.ProjectName = p.Name
		}
	}
	active, err := db.HasActiveExecution(ctx, s.db, a.ID)
	if err != nil {
		active = false
	}
	out.HasActiveExecution = active

	latest, err := db.GetLatestExecutionByAutomation(ctx, s.db, a.ID)
	if err != nil || latest == nil {
		return
	}
	out.LastExecutionStatus = string(latest.Status)
	out.LastExecutionTime = &latest.ScheduledAt
	out.LastExecutionPublicID = latest.PublicID
	out.LastTaskID = latest.TaskID
}

// toContractExecution 将执行记录模型转换为响应结构。
func toContractExecution(ex *types.AutomationExecution) *contract.AutomationExecution {
	return &contract.AutomationExecution{
		PublicID:            ex.PublicID,
		AutomationID:        ex.AutomationID,
		OrgID:               ex.OrgID,
		OwnerID:             ex.OwnerID,
		TriggerType:         string(ex.TriggerType),
		Status:              string(ex.Status),
		ScheduledAt:         ex.ScheduledAt,
		NotAfter:            ex.NotAfter,
		StartedAt:           ex.StartedAt,
		FinishedAt:          ex.FinishedAt,
		NameSnapshot:        ex.NameSnapshot,
		InstructionSnapshot: ex.InstructionSnapshot,
		AssistantIDSnapshot: ex.AssistantIDSnapshot,
		MissedCount:         ex.MissedCount,
		ProjectID:           ex.ProjectID,
		TaskID:              ex.TaskID,
		SessionID:           ex.SessionID,
		MessageID:           ex.MessageID,
		RunID:               ex.RunID,
		AttemptCount:        ex.AttemptCount,
		ErrorCode:           ex.ErrorCode,
		ErrorMsg:            ex.ErrorMsg,
		CreatedAt:           ex.CreatedAt,
	}
}

// 编译期断言：automationService 实现 contract.AutomationService
var _ contract.AutomationService = (*automationService)(nil)
