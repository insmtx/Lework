package contract

import (
	"time"

	"github.com/insmtx/Leros/backend/types"
)

// LLMModel LLM模型配置响应结构
type LLMModel struct {
	ID           uint                   `json:"id"`
	OrgID        uint                   `json:"org_id"`
	Code         string                 `json:"code"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Provider     string                 `json:"provider"`
	Model        string                 `json:"model"`
	BaseURL      string                 `json:"base_url"`
	BaseURLHasV1 bool                   `json:"base_url_has_v1"`
	APIKey       string                 `json:"api_key"`
	MaxTokens    int                    `json:"max_tokens"`
	Temperature  float64                `json:"temperature"`
	TimeoutSec   int                    `json:"timeout_sec"`
	Status       string                 `json:"status"`
	Purpose      string                 `json:"purpose"`
	IsDefault    bool                   `json:"is_default"`
	IsSystem     bool                   `json:"is_system"`
	Config       map[string]interface{} `json:"config,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// CreateLLMModelRequest 创建LLM模型配置请求
type CreateLLMModelRequest struct {
	Name        string                 `json:"name" binding:"required"`
	Description string                 `json:"description,omitempty"`
	Provider    string                 `json:"provider,omitempty"`
	Model       string                 `json:"model" binding:"required"`
	BaseURL     string                 `json:"base_url" binding:"required"`
	APIKey      string                 `json:"api_key" binding:"required"`
	Status      string                 `json:"status,omitempty"`
	Purpose     string                 `json:"purpose" binding:"required"`
	IsDefault   bool                   `json:"is_default,omitempty"`
	Config      map[string]interface{} `json:"config,omitempty"`
	MaxTokens   *int                   `json:"max_tokens,omitempty"`
	Temperature *float64               `json:"temperature,omitempty"`
}

// UpdateLLMModelRequest 更新LLM模型配置请求
// 不含 Status 字段：启用/禁用走独立的 SetStatus 逻辑/请求。
type UpdateLLMModelRequest struct {
	Name        string                  `json:"name" binding:"required"`
	Description *string                 `json:"description,omitempty"`
	Provider    string                  `json:"provider,omitempty"`
	Model       string                  `json:"model,omitempty"`
	BaseURL     *string                 `json:"base_url,omitempty"`
	APIKey      *string                 `json:"api_key,omitempty"`
	Purpose     *string                 `json:"purpose" binding:"required"`
	IsDefault   *bool                   `json:"is_default,omitempty"`
	Config      *map[string]interface{} `json:"config,omitempty"`
	MaxTokens   *int                    `json:"max_tokens,omitempty"`
	Temperature *float64                `json:"temperature,omitempty"`
}

// SetLLMModelStatusRequest 启用或禁用LLM模型配置请求
type SetLLMModelStatusRequest struct {
	ID     uint   `json:"id" binding:"required"`
	Status string `json:"status" binding:"required"`
}

// ListLLMModelsRequest 查询LLM模型配置列表请求
type ListLLMModelsRequest struct {
	Provider *string `json:"provider,omitempty"`
	Status   *string `json:"status,omitempty"`
	Purpose  *string `json:"purpose,omitempty"`
	Keyword  *string `json:"keyword,omitempty"`
	types.Pagination
}

// LLMModelList LLM模型配置列表响应
type LLMModelList struct {
	Total  int64      `json:"total"`
	Offset int        `json:"offset"`
	Limit  int        `json:"limit"`
	Items  []LLMModel `json:"items"`
}

// TestLLMModelRequest 测试LLM模型配置请求
type TestLLMModelRequest struct {
	ID       *uint  `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
}

// TestLLMModelResponse 测试LLM模型配置响应
type TestLLMModelResponse struct {
	Success      bool   `json:"success"`
	StatusCode   int    `json:"status_code"`
	Message      string `json:"message"`
	Endpoint     string `json:"endpoint"`
	LatencyMS    int64  `json:"latency_ms"`
	BaseURLHasV1 bool   `json:"base_url_has_v1"`
}
