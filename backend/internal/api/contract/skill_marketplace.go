package contract

import "context"

// SkillMarketplaceService 定义 Skill 市场搜索服务接口。
type SkillMarketplaceService interface {
	SearchSkillMarketplace(ctx context.Context, req *SearchSkillMarketplaceRequest) (*SearchSkillMarketplaceResponse, error)
}

// SearchSkillMarketplaceRequest Skill 市场搜索请求。
type SearchSkillMarketplaceRequest struct {
	Keyword     string   `form:"keyword" json:"keyword,omitempty"`
	Category    string   `form:"category" json:"category,omitempty"`
	SourceCodes []string `form:"source_codes" json:"source_codes,omitempty"`
	Offset      int      `form:"offset" json:"offset,omitempty"`
	Limit       int      `form:"limit" json:"limit,omitempty"`
}

// SkillMarketplaceItemView Skill 市场条目视图。
type SkillMarketplaceItemView struct {
	SourceCode string   `json:"source_code"`
	SourceName string   `json:"source_name"`
	SourceType string   `json:"source_type"`
	SkillID    string   `json:"skill_id"`
	Name       string   `json:"name"`
	Description string  `json:"description"`
	Version    string   `json:"version"`
	Author     string   `json:"author"`
	Category   string   `json:"category"`
	Tags       []string `json:"tags"`
	Icon       string   `json:"icon,omitempty"`
	Installs   int64    `json:"installs"`
}

// SkillSourceWarning 源查询警告信息。
type SkillSourceWarning struct {
	SourceCode string `json:"source_code"`
	Message    string `json:"message"`
}

// SearchSkillMarketplaceResponse Skill 市场搜索响应。
type SearchSkillMarketplaceResponse struct {
	Items    []SkillMarketplaceItemView `json:"items"`
	Total    int64                      `json:"total"`
	Warnings []SkillSourceWarning       `json:"warnings,omitempty"`
}
