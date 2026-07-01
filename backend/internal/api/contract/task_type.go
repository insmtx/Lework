package contract

import (
	"time"

	"github.com/insmtx/Leros/backend/types"
)

// Task 任务响应结构
type Task struct {
	PublicID    string                 `json:"public_id"`
	OrgID       uint                   `json:"org_id"`
	OwnerID     uint                   `json:"owner_id"`
	ProjectID   string                 `json:"project_id"`
	ProjectName string                 `json:"project_name"`
	SessionID   *uint                  `json:"session_id,omitempty"`
	TaskType    string                 `json:"task_type"`
	AssigneeID  *uint                  `json:"assignee_id,omitempty"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Status      string                 `json:"status"`
	Deadline    *time.Time             `json:"deadline,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// CreateTaskRequest 创建任务请求
type CreateTaskRequest struct {
	ProjectID   string                 `json:"project_id" binding:"required"`
	Title       string                 `json:"title" binding:"required"`
	Description string                 `json:"description,omitempty"`
	TaskType    *string                `json:"task_type,omitempty"`
	AssigneeID  *uint                  `json:"assignee_id,omitempty"`
	Deadline    *time.Time             `json:"deadline,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateTaskRequest 更新任务请求
type UpdateTaskRequest struct {
	ProjectID   *string                 `json:"project_id,omitempty"`
	Title       *string                 `json:"title,omitempty"`
	Description *string                 `json:"description,omitempty"`
	TaskType    *string                 `json:"task_type,omitempty"`
	AssigneeID  *uint                   `json:"assignee_id,omitempty"`
	Status      *string                 `json:"status,omitempty"`
	Deadline    *time.Time              `json:"deadline,omitempty"`
	Metadata    *map[string]interface{} `json:"metadata,omitempty"`
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
