package contract

import (
	"time"

	"github.com/insmtx/Leros/backend/types"
)

// Automation 自动化响应结构
type Automation struct {
	PublicID     string                        `json:"public_id"`
	OrgID        uint                          `json:"org_id"`
	OwnerID      uint                          `json:"owner_id"`
	Name         string                        `json:"name"`
	Instruction  string                        `json:"instruction,omitempty"`
	Enabled      bool                          `json:"enabled"`
	ScheduleMode string                        `json:"schedule_mode"`
	ScheduleSpec *types.AutomationScheduleSpec `json:"schedule_spec"`
	Timezone     string                        `json:"timezone"`
	AssistantID  uint                          `json:"assistant_id"`
	NextRunAt    *time.Time                    `json:"next_run_at,omitempty"`
	// Summary 周期摘要文案（前端直接展示）
	Summary   string    `json:"summary,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// 执行状态聚合（列表/详情展示）
	// HasActiveExecution 是否存在 queued/running 执行
	HasActiveExecution bool `json:"has_active_execution"`
	// LastExecutionStatus 最近一次执行状态（无则空）
	LastExecutionStatus string `json:"last_execution_status,omitempty"`
	// LastExecutionTime 最近一次执行的理论触发时间
	LastExecutionTime *time.Time `json:"last_execution_time,omitempty"`
	// LastExecutionPublicID 最近一次执行对外 ID
	LastExecutionPublicID string `json:"last_execution_public_id,omitempty"`
	// LastTaskID 最近一次执行对应的任务主键（用于跳转）
	LastTaskID *uint `json:"last_task_id,omitempty"`
	// ProjectID 当前自动化项目主键
	ProjectID *uint `json:"project_id,omitempty"`
	// ProjectPublicID 当前自动化项目的对外 ID（用于前端跳转/回显）
	ProjectPublicID string `json:"project_public_id,omitempty"`
	// ProjectName 当前自动化项目的名称（用于前端展示）
	ProjectName string `json:"project_name,omitempty"`
}

// AutomationScheduleInput 是创建/更新请求的调度输入模型。
// 它不暴露存量 v1 的 anchor_at；旧字段只存在于数据库回填模型中。
type AutomationScheduleInput struct {
	Mode     string                          `json:"mode"`
	Calendar *types.AutomationCalendarConfig `json:"calendar,omitempty"`
	Interval *AutomationIntervalInput        `json:"interval,omitempty"`
	Timezone string                          `json:"timezone,omitempty"`
}

// AutomationIntervalInput 是不含 legacy anchor_at 的固定间隔请求模型。
type AutomationIntervalInput struct {
	IntervalSeconds int64  `json:"interval_seconds,omitempty"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	IntervalUnit    string `json:"interval_unit,omitempty"`
}

// FormConfig 转为内部存储/编译使用的表单结构。
func (s *AutomationScheduleInput) FormConfig() *types.AutomationScheduleFormConfig {
	if s == nil {
		return nil
	}
	var interval *types.AutomationIntervalConfig
	if s.Interval != nil {
		interval = &types.AutomationIntervalConfig{
			IntervalSeconds: s.Interval.IntervalSeconds,
			IntervalMinutes: s.Interval.IntervalMinutes,
			IntervalUnit:    s.Interval.IntervalUnit,
		}
	}
	return &types.AutomationScheduleFormConfig{
		Mode:     s.Mode,
		Calendar: s.Calendar,
		Interval: interval,
		Timezone: s.Timezone,
	}
}

// CreateAutomationRequest 创建自动化请求
type CreateAutomationRequest struct {
	Name         string                   `json:"name" binding:"required"`
	Instruction  string                   `json:"instruction,omitempty"`
	Enabled      *bool                    `json:"enabled,omitempty"`
	ScheduleMode string                   `json:"schedule_mode" binding:"required"`
	Schedule     *AutomationScheduleInput `json:"schedule" binding:"required"`
	Timezone     string                   `json:"timezone,omitempty"`
	// ProjectPublicID 关联的既有项目对外 ID（可选）。空/省略：使用默认新项目（首次执行懒创建）。
	ProjectPublicID string `json:"project_public_id,omitempty"`
}

// UpdateAutomationRequest 更新自动化请求（部分更新）
type UpdateAutomationRequest struct {
	Name        string  `json:"name,omitempty"`
	Instruction *string `json:"instruction,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
	// 修改周期时必须提交完整 schedule
	ScheduleMode *string                  `json:"schedule_mode,omitempty"`
	Schedule     *AutomationScheduleInput `json:"schedule,omitempty"`
	Timezone     *string                  `json:"timezone,omitempty"`
	// ProjectPublicID 关联项目三态：
	//   nil：保持原关联；""：切回默认新项目；非空：关联指定项目。
	ProjectPublicID *string `json:"project_public_id,omitempty"`
}

// ListAutomationsRequest 查询自动化列表请求
type ListAutomationsRequest struct {
	Keyword      *string `json:"keyword,omitempty"`
	Enabled      *bool   `json:"enabled,omitempty"`
	ScheduleMode *string `json:"schedule_mode,omitempty"`
	types.Pagination
}

// GetAutomationRequest 查询自动化详情请求
type GetAutomationRequest struct {
	PublicID string `json:"public_id" binding:"required"`
}

// DeleteAutomationRequest 删除自动化请求
type DeleteAutomationRequest struct {
	PublicID string `json:"public_id" binding:"required"`
}

// AutomationList 自动化列表响应
type AutomationList struct {
	Total  int64        `json:"total"`
	Offset int          `json:"offset"`
	Limit  int          `json:"limit"`
	Items  []Automation `json:"items"`
}

// RunAutomationNowRequest 立即运行请求
type RunAutomationNowRequest struct {
	PublicID string `json:"public_id" binding:"required"`
}

// AutomationExecution 执行记录响应
type AutomationExecution struct {
	PublicID            string     `json:"public_id"`
	AutomationID        uint       `json:"automation_id"`
	OrgID               uint       `json:"org_id"`
	OwnerID             uint       `json:"owner_id"`
	TriggerType         string     `json:"trigger_type"`
	Status              string     `json:"status"`
	ScheduledAt         time.Time  `json:"scheduled_at"`
	NotAfter            *time.Time `json:"not_after,omitempty"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
	NameSnapshot        string     `json:"name_snapshot"`
	InstructionSnapshot string     `json:"instruction_snapshot,omitempty"`
	AssistantIDSnapshot uint       `json:"assistant_id_snapshot"`
	MissedCount         int        `json:"missed_count"`
	ProjectID           *uint      `json:"project_id,omitempty"`
	TaskID              *uint      `json:"task_id,omitempty"`
	SessionID           *uint      `json:"session_id,omitempty"`
	MessageID           *uint      `json:"message_id,omitempty"`
	// 关联实体的对外 public_id（用于前端跳转），仅展示，不落库
	ProjectPublicID string    `json:"project_public_id,omitempty"`
	TaskPublicID    string    `json:"task_public_id,omitempty"`
	SessionPublicID string    `json:"session_public_id,omitempty"`
	MessagePublicID string    `json:"message_public_id,omitempty"`
	RunID           string    `json:"run_id,omitempty"`
	AttemptCount    int       `json:"attempt_count"`
	ErrorCode       string    `json:"error_code,omitempty"`
	ErrorMsg        string    `json:"error_msg,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// GetAutomationExecutionRequest 查询执行详情请求
type GetAutomationExecutionRequest struct {
	PublicID string `json:"public_id" binding:"required"`
}

// ListAutomationExecutionsRequest 查询执行历史请求
type ListAutomationExecutionsRequest struct {
	PublicID string  `json:"public_id" binding:"required"`
	Status   *string `json:"status,omitempty"`
	types.Pagination
}

// AutomationExecutionList 执行历史列表响应
type AutomationExecutionList struct {
	Total  int64                 `json:"total"`
	Offset int                   `json:"offset"`
	Limit  int                   `json:"limit"`
	Items  []AutomationExecution `json:"items"`
}
