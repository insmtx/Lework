package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

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
)

type automationService struct {
	db *gorm.DB
}

// NewAutomationService 构造自动化服务
func NewAutomationService(db *gorm.DB) contract.AutomationService {
	return &automationService{db: db}
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

	automation := &types.Automation{
		OrgID:        caller.OrgID,
		OwnerID:      caller.Uin,
		PublicID:     generateAutomationPublicID(),
		Name:         name,
		Instruction:  instruction,
		Enabled:      enabled,
		ScheduleMode: mode,
		ScheduleSpec: *spec,
		Timezone:     timezone,
		AssistantID:  assistantID,
		NextRunAt:    nextRunAt,
	}
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

	// 名称
	if strings.TrimSpace(req.Name) != "" {
		if len([]rune(req.Name)) > 50 {
			return nil, errors.New("invalid automation name")
		}
		automation.Name = strings.TrimSpace(req.Name)
	}
	// 指令：传了就必须非空
	if req.Instruction != nil {
		if strings.TrimSpace(*req.Instruction) == "" {
			return nil, errors.New("invalid automation instruction")
		}
		automation.Instruction = strings.TrimSpace(*req.Instruction)
	}

	// 修改周期/时区：必须提交完整 schedule，重新编译
	scheduleChanged := false
	if req.Schedule != nil {
		// mode 以 schedule.mode 为权威来源，顶层 schedule_mode（如有）必须与之一致
		topMode := ""
		if req.ScheduleMode != nil {
			topMode = *req.ScheduleMode
		}
		newSpec, compileErr := compileScheduleSpec(req.Schedule, automation.Timezone, topMode)
		if compileErr != nil {
			return nil, compileErr
		}
		automation.ScheduleSpec = *newSpec
		automation.ScheduleMode = newSpec.Spec.Mode
		automation.Timezone = newSpec.Spec.Timezone
		scheduleChanged = true
	} else if req.Timezone != nil && *req.Timezone != "" {
		// 仅改时区：重新校验并沿用现有规则
		if _, tzErr := validateTimezone(*req.Timezone); tzErr != nil {
			return nil, tzErr
		}
		automation.Timezone = *req.Timezone
		// 重新生成时区后的 next_run_at
		automation.ScheduleSpec.Spec.Timezone = *req.Timezone
		if automation.ScheduleSpec.FormConfig != nil {
			automation.ScheduleSpec.FormConfig.Timezone = *req.Timezone
		}
		scheduleChanged = true
	}

	// 启停状态
	if req.Enabled != nil && *req.Enabled != automation.Enabled {
		automation.Enabled = *req.Enabled
		scheduleChanged = true
	}

	// 重新计算或清空 next_run_at；启用态若无法算出下一次执行时间，则拒绝保存
	if scheduleChanged {
		nextRunAt := computeNextRunAt(&automation.ScheduleSpec, automation.Enabled, time.Now().UTC())
		if automation.Enabled && nextRunAt == nil {
			return nil, errInvalidAutomationSchedule
		}
		automation.NextRunAt = nextRunAt
	}

	if err := db.UpdateAutomation(ctx, s.db, automation); err != nil {
		return nil, err
	}

	out := toContractAutomation(automation)
	s.enrichAutomation(ctx, automation, out)
	return out, nil
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

	items := make([]contract.Automation, 0, len(entities))
	for _, e := range entities {
		item := toContractAutomation(e)
		s.enrichAutomation(ctx, e, item)
		items = append(items, *item)
	}
	return &contract.AutomationList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

// checkOwner 校验自动化归属
func (s *automationService) checkOwner(caller *types.Caller, automation *types.Automation) error {
	if caller.Uin != automation.OwnerID {
		return ErrAutomationForbidden
	}
	return nil
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
		Enabled:      a.Enabled,
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
