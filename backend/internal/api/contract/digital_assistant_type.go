package contract

import (
	"time"

	"github.com/insmtx/Lework/backend/types"
)

// DigitalAssistantStatus 数字助手状态常量
type DigitalAssistantStatus string

const (
	DigitalAssistantStatusDraft    DigitalAssistantStatus = "draft"
	DigitalAssistantStatusActive   DigitalAssistantStatus = "active"
	DigitalAssistantStatusInactive DigitalAssistantStatus = "inactive"
	DigitalAssistantStatusArchived DigitalAssistantStatus = "archived"
)

// DigitalAssistant 数字助手信息
type DigitalAssistant struct {
	ID           uint                             `json:"id"`
	PublicID     string                           `json:"public_id"`
	OrgID        uint                             `json:"org_id"`
	OwnerID      uint                             `json:"owner_id"`
	Name         string                           `json:"name"`
	RoleName     string                           `json:"role_name"`
	Description  string                           `json:"description"`
	Avatar       string                           `json:"avatar"`
	Status       string                           `json:"status"`
	Version      int                              `json:"version"`
	SystemPrompt string                           `json:"system_prompt,omitempty"`
	Expertise    []string                         `json:"expertise"`
	Visibility   types.DigitalAssistantVisibility `json:"visibility"`
	Permission   *DigitalAssistantPermission      `json:"permission,omitempty"`
	TemplateID   *uint                            `json:"template_id,omitempty"`
	Source       string                           `json:"source"`
	Deployment   *WorkerDeploymentStatus          `json:"deployment,omitempty"`
	CreatedAt    time.Time                        `json:"created_at"`
	UpdatedAt    time.Time                        `json:"updated_at"`
}

// DigitalAssistantPermission 描述当前调用者在 AI 队友上的直接或隐式角色。
type DigitalAssistantPermission struct {
	Role types.ResourceRole `json:"role,omitempty"`
}

// DigitalAssistantPermissionUserView 是共享设置中展示的组织成员信息。
type DigitalAssistantPermissionUserView struct {
	PublicID string `json:"public_id"`
	Name     string `json:"name"`
	Email    string `json:"email,omitempty"`
	Avatar   string `json:"avatar_url,omitempty"`
}

// DigitalAssistantPermissionMemberView 是共享设置中的成员角色。
type DigitalAssistantPermissionMemberView struct {
	User DigitalAssistantPermissionUserView `json:"user"`
	Role types.ResourceRole                 `json:"role"`
}

// DigitalAssistantPermissionSettingsView 是 AI 队友共享配置的读写视图。
type DigitalAssistantPermissionSettingsView struct {
	Visibility types.DigitalAssistantVisibility       `json:"visibility"`
	Members    []DigitalAssistantPermissionMemberView `json:"members"`
}

// DigitalAssistantPermissionMemberInput 是共享配置写入的成员身份。
type DigitalAssistantPermissionMemberInput struct {
	User struct {
		PublicID string `json:"public_id"`
	} `json:"user"`
	Role types.ResourceRole `json:"role"`
}

// GetDigitalAssistantPermissionsRequest 是读取共享配置的请求。
type GetDigitalAssistantPermissionsRequest struct {
	PublicID string `json:"public_id" binding:"required"`
}

// UpdateDigitalAssistantPermissionsRequest 是全量替换共享配置的请求。
type UpdateDigitalAssistantPermissionsRequest struct {
	PublicID   string                                  `json:"public_id" binding:"required"`
	Visibility types.DigitalAssistantVisibility        `json:"visibility"`
	Members    []DigitalAssistantPermissionMemberInput `json:"members"`
}

// WorkerDeploymentStatus describes the runtime deployment state of an AI teammate.
type WorkerDeploymentStatus struct {
	PublicID  string `json:"public_id"`
	Status    string `json:"status"`
	LastError string `json:"last_error,omitempty"`
}

// CreateDigitalAssistantRequest 创建数字助手请求
type CreateDigitalAssistantRequest struct {
	Name         string   `json:"name" binding:"required"`
	RoleName     string   `json:"role_name"`
	Description  string   `json:"description"`
	Avatar       string   `json:"avatar"`
	SystemPrompt string   `json:"system_prompt"`
	Expertise    []string `json:"expertise"`
	TemplateID   *uint    `json:"template_id,omitempty"`
	Source       string   `json:"source"`
}

// UpdateDigitalAssistantRequest 更新数字助手请求
type UpdateDigitalAssistantRequest struct {
	Name         string    `json:"name"`
	RoleName     string    `json:"role_name"`
	Description  string    `json:"description"`
	Avatar       string    `json:"avatar"`
	SystemPrompt *string   `json:"system_prompt,omitempty"`
	Expertise    *[]string `json:"expertise,omitempty"`
}

// CheckDigitalAssistantNameRequest checks whether a teammate name can be used in the current organization.
type CheckDigitalAssistantNameRequest struct {
	Name      string `json:"name" binding:"required"`
	ExcludeID uint   `json:"exclude_id,omitempty"`
}

// CheckDigitalAssistantNameResponse returns the availability of a normalized teammate name.
type CheckDigitalAssistantNameResponse struct {
	Available bool `json:"available"`
}

// UpdateDigitalAssistantStatusRequest 更新数字助手状态请求
type UpdateDigitalAssistantStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// ListDigitalAssistantRequest 查询数字助手列表请求
type ListDigitalAssistantRequest struct {
	Status  *string `json:"status,omitempty"`
	Keyword *string `json:"keyword,omitempty"`
	Source  *string `json:"source,omitempty"`
	types.Pagination
}

// DigitalAssistantList 数字助手列表响应
type DigitalAssistantList struct {
	Total  int64              `json:"total"`
	Offset int                `json:"offset"`
	Limit  int                `json:"limit"`
	Items  []DigitalAssistant `json:"items"`
}

// DigitalAssistantDetail 数字助手详情响应
type DigitalAssistantDetail struct {
	DigitalAssistant
}

// CreateDigitalAssistantFromTemplateRequest 基于模板创建数字助手请求
type CreateDigitalAssistantFromTemplateRequest struct {
	TemplateID   uint     `json:"template_id" binding:"required"`
	Name         string   `json:"name"`
	RoleName     string   `json:"role_name"`
	Description  string   `json:"description"`
	Avatar       string   `json:"avatar"`
	SystemPrompt string   `json:"system_prompt"`
	Expertise    []string `json:"expertise"`
}
