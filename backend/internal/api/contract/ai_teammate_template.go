package contract

import (
	"context"
	"time"

	"github.com/insmtx/Leros/backend/types"
)

// AITeammateTemplateStatus AI 队友模板状态。
type AITeammateTemplateStatus string

const (
	// AITeammateTemplateStatusActive 表示模板可展示和使用。
	AITeammateTemplateStatusActive AITeammateTemplateStatus = "active"
	// AITeammateTemplateStatusInactive 表示模板暂不展示和使用。
	AITeammateTemplateStatusInactive AITeammateTemplateStatus = "inactive"
)

// AITeammateTemplateService defines preset AI teammate template operations.
type AITeammateTemplateService interface {
	ListAITeammateTemplates(ctx context.Context, req *ListAITeammateTemplateRequest) (*AITeammateTemplateList, error)
	GetAITeammateTemplate(ctx context.Context, req *GetAITeammateTemplateRequest) (*AITeammateTemplate, error)
	IncrementAITeammateTemplateUseCount(ctx context.Context, req *IncrementAITeammateTemplateCountRequest) (*AITeammateTemplate, error)
	IncrementAITeammateTemplateRecommendCount(ctx context.Context, req *IncrementAITeammateTemplateCountRequest) (*AITeammateTemplate, error)
}

// AITeammateTemplate is the API view for a preset AI teammate template.
type AITeammateTemplate struct {
	ID             uint      `json:"id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Avatar         string    `json:"avatar"`
	Provider       string    `json:"provider"`
	SystemPrompt   string    `json:"system_prompt"`
	Expertise      []string  `json:"expertise"`
	Category       string    `json:"category"`
	Tags           []string  `json:"tags"`
	SortOrder      int       `json:"sort_order"`
	UseCount       int64     `json:"use_count"`
	RecommendCount int64     `json:"recommend_count"`
	Status         string    `json:"status"`
	IsSystem       bool      `json:"is_system"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ListAITeammateTemplateRequest queries AI teammate templates.
type ListAITeammateTemplateRequest struct {
	Keyword  *string `json:"keyword,omitempty"`
	Category *string `json:"category,omitempty"`
	Status   *string `json:"status,omitempty"`
	types.Pagination
}

// AITeammateTemplateList is a paginated template list response.
type AITeammateTemplateList struct {
	Total  int64                `json:"total"`
	Offset int                  `json:"offset"`
	Limit  int                  `json:"limit"`
	Items  []AITeammateTemplate `json:"items"`
}

// GetAITeammateTemplateRequest gets a template by ID or code.
type GetAITeammateTemplateRequest struct {
	ID   *uint   `json:"id,omitempty"`
	Code *string `json:"code,omitempty"`
}

// IncrementAITeammateTemplateCountRequest increments a template counter.
type IncrementAITeammateTemplateCountRequest struct {
	ID   *uint   `json:"id,omitempty"`
	Code *string `json:"code,omitempty"`
}
