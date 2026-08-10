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
}

// CreateAutomationRequest 创建自动化请求
type CreateAutomationRequest struct {
	Name         string                              `json:"name" binding:"required"`
	Instruction  string                              `json:"instruction,omitempty"`
	Enabled      *bool                               `json:"enabled,omitempty"`
	ScheduleMode string                              `json:"schedule_mode" binding:"required"`
	Schedule     *types.AutomationScheduleFormConfig `json:"schedule" binding:"required"`
	Timezone     string                              `json:"timezone,omitempty"`
}

// UpdateAutomationRequest 更新自动化请求（部分更新）
type UpdateAutomationRequest struct {
	Name        string  `json:"name,omitempty"`
	Instruction *string `json:"instruction,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
	// 修改周期时必须提交完整 schedule
	ScheduleMode *string                             `json:"schedule_mode,omitempty"`
	Schedule     *types.AutomationScheduleFormConfig `json:"schedule,omitempty"`
	Timezone     *string                             `json:"timezone,omitempty"`
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
