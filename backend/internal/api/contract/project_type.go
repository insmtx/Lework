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
	TaskCount   int64                  `json:"task_count"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// MemberInput 创建/编辑项目时传入的成员项
type MemberInput struct {
	Type string `json:"type" binding:"required"` // "user" | "assistant"
	ID   string `json:"id" binding:"required"`   // user 传 public_id, assistant 传 public_id
	Role string `json:"role,omitempty"`          // 仅 type=user 生效，可选 owner|admin|member，空值默认 member
}

// CreateProjectRequest 创建项目请求
type CreateProjectRequest struct {
	Name        string                 `json:"name" binding:"required"`
	Description string                 `json:"description,omitempty"`
	Objective   string                 `json:"objective,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Members     []MemberInput          `json:"members,omitempty"`
}

// UpdateProjectRequest 更新项目请求
type UpdateProjectRequest struct {
	Name        *string                 `json:"name,omitempty"`
	Description *string                 `json:"description,omitempty"`
	Objective   *string                 `json:"objective,omitempty"`
	OwnerID     *uint                   `json:"owner_id,omitempty"`
	Status      *string                 `json:"status,omitempty"`
	Metadata    *map[string]interface{} `json:"metadata,omitempty"`
	Members     []MemberInput           `json:"members,omitempty"`
}

// ListProjectsRequest 查询项目列表请求
type ListProjectsRequest struct {
	Keyword *string `json:"keyword,omitempty"`
	Status  *string `json:"status,omitempty"`
	types.Pagination
}

// ListProjectActivitiesRequest 查询项目操作动态请求。
type ListProjectActivitiesRequest struct {
	ProjectID   string   `json:"project_id,omitempty"`
	OperatorID  string   `json:"operator_id,omitempty"`
	OperatorIDs []string `json:"operator_ids,omitempty"`
	Cursor      string   `json:"cursor,omitempty"`
	Limit       int      `json:"limit,omitempty"`
}

// ProjectList 项目列表响应
type ProjectList struct {
	Total  int64     `json:"total"`
	Offset int       `json:"offset"`
	Limit  int       `json:"limit"`
	Items  []Project `json:"items"`
}

// ProjectActivityList 项目动态列表响应。
type ProjectActivityList struct {
	Items      []ProjectActivityItem `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

// ProjectActivityItem 项目动态响应项。
type ProjectActivityItem struct {
	ID         uint                       `json:"id"`
	ProjectID  string                     `json:"project_id"`
	OperatorID string                     `json:"operator_id"`
	Operator   *ProjectActivityActor      `json:"operator,omitempty"`
	ActionType string                     `json:"action_type"`
	Payload    ProjectActivityPayloadView `json:"payload"`
	CreatedAt  time.Time                  `json:"created_at"`
}

// ProjectActivityActor 是动态中的用户或 AI 队友展示信息。
type ProjectActivityActor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// ProjectActivitySkill 是动态中的技能展示信息。
type ProjectActivitySkill struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

// ProjectActivityPayloadView 是补全展示信息后的动态 payload。
type ProjectActivityPayloadView struct {
	AddedSkills        []ProjectActivitySkill `json:"added_skills"`
	RemovedSkills      []ProjectActivitySkill `json:"removed_skills"`
	AddedMCPs          []ProjectActivitySkill `json:"added_mcps"`
	RemovedMCPs        []ProjectActivitySkill `json:"removed_mcps"`
	AddedMembers       []ProjectActivityActor `json:"added_members"`
	RemovedMembers     []ProjectActivityActor `json:"removed_members"`
	AddedAITeammates   []ProjectActivityActor `json:"added_ai_teammates"`
	RemovedAITeammates []ProjectActivityActor `json:"removed_ai_teammates"`
}

// WorkbenchRecentContext 首页工作台最近明确使用的项目/任务上下文。
type WorkbenchRecentContext struct {
	ProjectID   string    `json:"project_id"`
	ProjectName string    `json:"project_name"`
	TaskID      *string   `json:"task_id,omitempty"`
	TaskTitle   *string   `json:"task_title,omitempty"`
	UsedAt      time.Time `json:"used_at"`
}

// SaveWorkbenchRecentContextRequest 保存首页工作台最近使用上下文的请求。
type SaveWorkbenchRecentContextRequest struct {
	ProjectID string  `json:"project_id" binding:"required"`
	TaskID    *string `json:"task_id,omitempty"`
}

// ProjectDetail 项目详情响应，包含关联的会话、任务、产物和成员
type ProjectDetail struct {
	Project
	Session *Session            `json:"session,omitempty"`
	Tasks   []ProjectTaskItem   `json:"tasks"`
	Members []ProjectMemberItem `json:"members"`
}

// ProjectPlugin is a project-authorized organization plugin.
type ProjectPlugin struct {
	PublicID        string `json:"public_id"`
	Code            string `json:"code"`
	Kind            string `json:"kind"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	Status          string `json:"status"`
	CurrentRevision int    `json:"current_revision"`
}

// ListProjectPluginsRequest filters plugins bound to a project.
type ListProjectPluginsRequest struct {
	PublicID string `json:"public_id" binding:"required"`
	Kind     string `json:"kind,omitempty"`
}

// UpdateProjectPluginRequest binds or unbinds one organization plugin.
type UpdateProjectPluginRequest struct {
	PublicID string `json:"public_id" binding:"required"`
	PluginID string `json:"plugin_id" binding:"required"`
}

// ProjectTaskItem 项目详情中的任务项，包含关联的会话信息
type ProjectTaskItem struct {
	Task
	Session *Session `json:"session,omitempty"`
}

// ProjectMemberItem 项目详情中的成员项，包含用户基本信息
type ProjectMemberItem struct {
	MemberID   uint      `json:"member_id"`
	PublicID   string    `json:"public_id,omitempty"`
	MemberType string    `json:"member_type"`
	MemberRole string    `json:"member_role"`
	IsDefault  bool      `json:"is_default"`
	JoinedAt   time.Time `json:"joined_at"`
	Name       string    `json:"name,omitempty"`
	AvatarURL  string    `json:"avatar_url"`
}

// ProjectMemory 项目记忆响应
type ProjectMemory struct {
	Entries []string `json:"entries"`
	Total   int      `json:"total"`
}

// FileTreeNode 文件树节点。列表接口以平铺结构返回，前端按 parent_id 组装树。
type FileTreeNode struct {
	Name                string `json:"name"`
	Path                string `json:"path"`
	Type                string `json:"type"`
	Size                int64  `json:"size,omitempty"`
	MimeType            string `json:"mime_type,omitempty"`
	ModTime             int64  `json:"mod_time,omitempty"`
	CreatedAt           int64  `json:"created_at,omitempty"`
	PublicID            string `json:"public_id,omitempty"`
	StorageURI          string `json:"storage_uri,omitempty"`
	Sha256              string `json:"sha256,omitempty"`
	InitialFilePublicID string `json:"initial_file_public_id,omitempty"`
	VersionNo           int    `json:"version_no,omitempty"`
	VersionLabel        string `json:"version_label,omitempty"`
	VersionCount        int    `json:"version_count,omitempty"`
	ResourceType        string `json:"resource_type,omitempty"`
}

// ProjectFileTreeQuery scopes project file tree list requests.
type ProjectFileTreeQuery struct {
	ResourceType string
	TaskPublicID string
	FileExt      string
}

// ProjectFileVersion describes one concrete version in a logical project file chain.
type ProjectFileVersion struct {
	PublicID            string `json:"public_id"`
	InitialFilePublicID string `json:"initial_file_public_id"`
	RelativePath        string `json:"relative_path"`
	Name                string `json:"name"`
	VersionNo           int    `json:"version_no"`
	VersionLabel        string `json:"version_label"`
	Size                int64  `json:"size,omitempty"`
	MimeType            string `json:"mime_type,omitempty"`
	CreatedAt           int64  `json:"created_at,omitempty"`
	StorageURI          string `json:"storage_uri,omitempty"`
	Sha256              string `json:"sha256,omitempty"`
}

// ProjectFileVersionList contains all versions of one logical project file.
type ProjectFileVersionList struct {
	InitialFilePublicID string               `json:"initial_file_public_id"`
	CurrentFilePublicID string               `json:"current_file_public_id"`
	Items               []ProjectFileVersion `json:"items"`
}

// FileUploadResult 文件上传结果
type FileUploadResult struct {
	PublicID string `json:"public_id"`     // 文件记录 public_id
	Path     string `json:"path"`          // 相对 repo 根目录的路径
	Filename string `json:"filename"`      // 文件名
	Size     int64  `json:"size"`          // 文件大小（字节）
	URL      string `json:"url,omitempty"` // 文件访问 URL
}

// AddFileRequest 将已上传文件关联到项目的请求
type AddFileRequest struct {
	PublicID string `json:"public_id" binding:"required"`
}
