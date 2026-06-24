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

// ProjectDetail 项目详情响应，包含关联的会话、任务、产物和成员
type ProjectDetail struct {
	Project
	Session   *Session            `json:"session,omitempty"`
	Tasks     []ProjectTaskItem   `json:"tasks"`
	Artifacts []Artifact          `json:"artifacts,omitempty"`
	Members   []ProjectMemberItem `json:"members"`
}

// ProjectTaskItem 项目详情中的任务项，包含关联的会话信息
type ProjectTaskItem struct {
	Task
	Session *Session `json:"session,omitempty"`
}

// ProjectMemberItem 项目详情中的成员项，包含用户基本信息
type ProjectMemberItem struct {
	MemberID   uint      `json:"member_id"`
	MemberType string    `json:"member_type"`
	MemberRole string    `json:"member_role"`
	JoinedAt   time.Time `json:"joined_at"`
	Name       string    `json:"name,omitempty"`
	AvatarURL  string    `json:"avatar_url,omitempty"`
}

// ProjectMemory 项目记忆响应
type ProjectMemory struct {
	Entries []string `json:"entries"`
	Total   int      `json:"total"`
}

// FileTreeNode 文件树节点，递归结构
type FileTreeNode struct {
	Name      string          `json:"name"`                 // 文件/目录名
	Path      string          `json:"path"`                 // 相对路径，兼做节点标识
	Type      string          `json:"type"`                 // "file" | "directory"
	Children  []*FileTreeNode `json:"children,omitempty"`   // 仅目录有
	Size      int64           `json:"size,omitempty"`       // 仅文件有
	MimeType  string          `json:"mime_type,omitempty"`  // 仅文件有
	ModTime   int64           `json:"mod_time,omitempty"`   // 最后修改时间，Unix 时间戳（秒）
	CreatedAt int64           `json:"created_at,omitempty"` // 文件首次 commit 时间，Unix 秒；未找到则为 0
	PublicID  string          `json:"public_id,omitempty"`  // 上传文件关联的 public_id，仓库文件为空
}

// FileUploadResult 文件上传结果
type FileUploadResult struct {
	Path     string `json:"path"`          // 相对 repo 根目录的路径
	Filename string `json:"filename"`      // 文件名
	Size     int64  `json:"size"`          // 文件大小（字节）
	URL      string `json:"url,omitempty"` // 文件访问 URL
}

// AddFileRequest 将已上传文件关联到项目的请求
type AddFileRequest struct {
	PublicID string `json:"public_id" binding:"required"`
}
