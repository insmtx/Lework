package contract

import (
	"context"
	"io"
)

// SkillPackageDownload 技能包下载结果。
type SkillPackageDownload struct {
	Reader   io.ReadCloser
	FileName string
}

// InstallSkillRequest Skill 安装请求。
type InstallSkillRequest struct {
	Source  string `json:"source" binding:"required"`
	SkillID string `json:"skill_id" binding:"required"`
	Version string `json:"version,omitempty"`
}

// InstallSkillResponse Skill 安装响应（异步，仅表示已接受）。
type InstallSkillResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ImportSkillRequest 从已上传文件导入 Skill 的请求。
type ImportSkillRequest struct {
	FileUploadID string `json:"file_upload_id" binding:"required"`
}

// ImportSkillFromGitHubRequest 从 GitHub 链接导入 Skill 的请求。
type ImportSkillFromGitHubRequest struct {
	GitHubURL string `json:"github_url" binding:"required"`
}

// ImportSkillResponse Skill 导入响应（异步，仅表示已接受）。
type ImportSkillResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// SkillMarketplaceService 定义 Skill 市场搜索服务接口。
type SkillMarketplaceService interface {
	SearchSkillMarketplace(ctx context.Context, req *SearchSkillMarketplaceRequest) (*SearchSkillMarketplaceResponse, error)
	DownloadBuiltinSkill(ctx context.Context, skillID string) (*SkillPackageDownload, error)
	DownloadSkillPackage(ctx context.Context, req *DownloadSkillRequest) (*SkillPackageDownload, error)
	InstallSkill(ctx context.Context, req *InstallSkillRequest) (*InstallSkillResponse, error)
	InstalledSkills(ctx context.Context, req *InstalledSkillsRequest) (*InstalledSkillsResponse, error)
	UninstallSkill(ctx context.Context, req *UninstallSkillRequest) (*UninstallSkillResponse, error)
	GetSkillDetail(ctx context.Context, req *SkillDetailRequest) (*SkillDetailResponse, error)
	ImportSkill(ctx context.Context, req *ImportSkillRequest) (*ImportSkillResponse, error)
	ImportSkillFromGitHub(ctx context.Context, req *ImportSkillFromGitHubRequest) (*ImportSkillResponse, error)
}

// DownloadSkillRequest 从缓存下载 Skill 包的请求。
type DownloadSkillRequest struct {
	Source  string `form:"source" json:"source"`
	SkillID string `form:"skill_id" json:"skill_id"`
	Version string `form:"version" json:"version"`
}

// SearchSkillMarketplaceRequest Skill 市场搜索请求。
type SearchSkillMarketplaceRequest struct {
	Keyword     string   `form:"keyword" json:"keyword,omitempty"`
	Category    string   `form:"category" json:"category,omitempty"`
	SourceTypes []string `form:"source_types" json:"source_types,omitempty"`
	Limit       int      `form:"limit" json:"limit,omitempty"`
}

// SkillMarketplaceItemView Skill 市场条目视图。
type SkillMarketplaceItemView struct {
	SourceType  string   `json:"source_type"`
	SkillID     string   `json:"skill_id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name,omitempty"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Icon        string   `json:"icon,omitempty"`
	Installs    int64    `json:"installs"`
}

// SkillSourceWarning 源查询警告信息。
type SkillSourceWarning struct {
	SourceType string `json:"source_type"`
	Message    string `json:"message"`
}

// SearchSkillMarketplaceResponse Skill 市场搜索响应。
type SearchSkillMarketplaceResponse struct {
	Items    []SkillMarketplaceItemView `json:"items"`
	Warnings []SkillSourceWarning       `json:"warnings,omitempty"`
}

// InstalledSkillsRequest 查询 worker 上已安装 Skill 的请求。
type InstalledSkillsRequest struct{}

// SkillInstalledItem 表示 worker 上已安装的 Skill。
type SkillInstalledItem struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Source      string `json:"source"`
	Trust       string `json:"trust"`
}

// InstalledSkillsResponse 已安装 Skill 列表响应。
type InstalledSkillsResponse struct {
	Skills []SkillInstalledItem `json:"skills"`
}

// UninstallSkillRequest 卸载 Skill 请求。
type UninstallSkillRequest struct {
	Name string `json:"name" binding:"required"`
}

// UninstallSkillResponse 卸载 Skill 响应（异步，仅表示已接受）。
type UninstallSkillResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// SkillDetailRequest 获取 Skill 详情的请求。
type SkillDetailRequest struct {
	Source  string `json:"source" binding:"required"`   // "Leros" for marketplace, "installed" for installed skills
	SkillID string `json:"skill_id" binding:"required"` // skill identifier
	Version string `json:"version,omitempty"`           // optional version, default "latest" for external sources
}

// SkillDetailResponse Skill 详情响应，包含完整元数据和 SKILL.md 正文。
type SkillDetailResponse struct {
	SkillID     string   `json:"skill_id"`
	Source      string   `json:"source"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name,omitempty"`
	Description string   `json:"description"`
	SkillMD     string   `json:"skill_md"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Icon        string   `json:"icon,omitempty"`
	Installs    int64    `json:"installs"`
	Verified    bool     `json:"verified"`
	SourceType  string   `json:"source_type"`
	Files       []string `json:"files"`
}
