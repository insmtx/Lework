package contract

import (
	"time"

	"github.com/insmtx/Lework/backend/types"
)

// Task 任务响应结构
type Task struct {
	PublicID           string                 `json:"public_id"`
	OrgID              uint                   `json:"org_id"`
	OwnerID            uint                   `json:"owner_id"`
	ProjectID          string                 `json:"project_id"`
	ProjectName        string                 `json:"project_name"`
	SessionID          *uint                  `json:"session_id,omitempty"`
	TaskType           string                 `json:"task_type"`
	AssigneeID         *uint                  `json:"assignee_id,omitempty"`
	Title              string                 `json:"title"`
	Description        string                 `json:"description"`
	ExpectedOutput     string                 `json:"expected_output"`
	AcceptanceCriteria string                 `json:"acceptance_criteria"`
	ResultSummary      string                 `json:"result_summary"`
	Status             string                 `json:"status"`
	Deadline           *time.Time             `json:"deadline,omitempty"`
	AcceptedBy         *uint                  `json:"accepted_by,omitempty"`
	AcceptedAt         *time.Time             `json:"accepted_at,omitempty"`
	AcceptanceNote     string                 `json:"acceptance_note"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
	Session            *Session               `json:"session,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
}

// CreateTaskRequest 创建任务请求
type CreateTaskRequest struct {
	ProjectID          string                 `json:"project_id" binding:"required"`
	Title              string                 `json:"title" binding:"required"`
	Description        string                 `json:"description,omitempty"`
	ExpectedOutput     string                 `json:"expected_output,omitempty"`
	AcceptanceCriteria string                 `json:"acceptance_criteria,omitempty"`
	TaskType           *string                `json:"task_type,omitempty"`
	AssigneeID         *uint                  `json:"assignee_id,omitempty"`
	Deadline           *time.Time             `json:"deadline,omitempty"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateTaskRequest 更新任务请求
type UpdateTaskRequest struct {
	ProjectID          *string                 `json:"project_id,omitempty"`
	Title              *string                 `json:"title,omitempty"`
	Description        *string                 `json:"description,omitempty"`
	ExpectedOutput     *string                 `json:"expected_output,omitempty"`
	AcceptanceCriteria *string                 `json:"acceptance_criteria,omitempty"`
	TaskType           *string                 `json:"task_type,omitempty"`
	AssigneeID         *uint                   `json:"assignee_id,omitempty"`
	Status             *string                 `json:"status,omitempty"`
	Deadline           *time.Time              `json:"deadline,omitempty"`
	Metadata           *map[string]interface{} `json:"metadata,omitempty"`
}

// ListTasksRequest 查询任务列表请求
type ListTasksRequest struct {
	Keyword    *string `json:"keyword,omitempty"`
	Status     *string `json:"status,omitempty"`
	ProjectID  *string `json:"project_id,omitempty"`
	TaskType   *string `json:"task_type,omitempty"`
	AssigneeID *uint   `json:"assignee_id,omitempty"`
	types.Pagination
}

// TaskList 任务列表响应
type TaskList struct {
	Total  int64  `json:"total"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
	Items  []Task `json:"items"`
}

// TaskDeliverableFile 是一次任务交付中变更或产生的项目文件。
type TaskDeliverableFile struct {
	FilePublicID string    `json:"file_public_id"`
	RelativePath string    `json:"relative_path"`
	ResourceType string    `json:"resource_type"`
	VersionNo    int       `json:"version_no"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TaskDeliverables 汇总任务会话产物与项目文件，供验收页展示。
type TaskDeliverables struct {
	TaskID        string                  `json:"task_id"`
	Artifacts     []types.MessageArtifact `json:"artifacts"`
	Files         []TaskDeliverableFile   `json:"files"`
	GitCommit     string                  `json:"git_commit"`
	ResultSummary string                  `json:"result_summary"`
}

// GetTaskDeliverablesRequest 查询任务交付物请求。
type GetTaskDeliverablesRequest struct {
	TaskID string `json:"task_id" binding:"required"`
}

// AcceptTaskRequest 验收通过请求。
type AcceptTaskRequest struct {
	TaskID string `json:"task_id" binding:"required"`
	Note   string `json:"note,omitempty"`
}

// RejectTaskRequest 验收驳回请求。
type RejectTaskRequest struct {
	TaskID string `json:"task_id" binding:"required"`
	Note   string `json:"note,omitempty"`
}
