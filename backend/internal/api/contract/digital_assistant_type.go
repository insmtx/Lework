package contract

import (
	"time"

	"github.com/insmtx/Leros/backend/types"
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
	ID           uint                    `json:"id"`
	Code         string                  `json:"code"`
	OrgID        uint                    `json:"org_id"`
	OwnerID      uint                    `json:"owner_id"`
	Name         string                  `json:"name"`
	Description  string                  `json:"description"`
	Avatar       string                  `json:"avatar"`
	Status       string                  `json:"status"`
	Version      int                     `json:"version"`
	SystemPrompt string                  `json:"system_prompt"`
	Expertise    []string                `json:"expertise"`
	TemplateID   *uint                   `json:"template_id,omitempty"`
	Source       string                  `json:"source"`
	Deployment   *WorkerDeploymentStatus `json:"deployment,omitempty"`
	CreatedAt    time.Time               `json:"created_at"`
	UpdatedAt    time.Time               `json:"updated_at"`
}

// WorkerDeploymentStatus describes the runtime deployment state of an AI teammate.
type WorkerDeploymentStatus struct {
	PublicID  string `json:"public_id"`
	Status    string `json:"status"`
	LastError string `json:"last_error,omitempty"`
}

// CreateDigitalAssistantRequest 创建数字助手请求
type CreateDigitalAssistantRequest struct {
	Code         string   `json:"code"`
	Name         string   `json:"name" binding:"required"`
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
	Description  string    `json:"description"`
	Avatar       string    `json:"avatar"`
	SystemPrompt *string   `json:"system_prompt,omitempty"`
	Expertise    *[]string `json:"expertise,omitempty"`
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
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Avatar       string   `json:"avatar"`
	SystemPrompt string   `json:"system_prompt"`
	Expertise    []string `json:"expertise"`
}
