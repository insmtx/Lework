package contract

import (
	"time"

	"github.com/insmtx/Leros/backend/types"
)

// Project 项目响应结构
type Project struct {
	PublicID    string                 `json:"public_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Objective   string                 `json:"objective,omitempty"`
	OwnerID     uint                   `json:"owner_id"`
	Status      string                 `json:"status"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// CreateProjectRequest 创建项目请求
type CreateProjectRequest struct {
	Name        string                 `json:"name" binding:"required"`
	Description string                 `json:"description,omitempty"`
	Objective   string                 `json:"objective,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateProjectRequest 更新项目请求
type UpdateProjectRequest struct {
	Name        *string                 `json:"name,omitempty"`
	Description *string                 `json:"description,omitempty"`
	Objective   *string                 `json:"objective,omitempty"`
	OwnerID     *uint                   `json:"owner_id,omitempty"`
	Status      *string                 `json:"status,omitempty"`
	Metadata    *map[string]interface{} `json:"metadata,omitempty"`
}

// ListProjectsRequest 查询项目列表请求
type ListProjectsRequest struct {
	Keyword *string `json:"keyword,omitempty"`
	Status  *string `json:"status,omitempty"`
	types.Pagination
}

// ProjectList 项目列表响应
type ProjectList struct {
	Total  int64     `json:"total"`
	Offset int       `json:"offset"`
	Limit  int       `json:"limit"`
	Items  []Project `json:"items"`
}
