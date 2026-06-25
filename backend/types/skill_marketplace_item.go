package types

import (
	"gorm.io/gorm"
)

// SkillMarketplaceItem 市场搜索结果缓存表，用于缓存 Skill 元数据和中文翻译描述。
type SkillMarketplaceItem struct {
	gorm.Model

	// SkillID 是 Skill 的唯一业务编码。
	SkillID string `gorm:"column:skill_id;type:varchar(100);not null;uniqueIndex:idx_marketplace_item_source_skill_version" json:"skill_id"`

	// Name 是 Skill 在市场页面展示的名称。
	Name string `gorm:"column:name;type:varchar(255);not null" json:"name"`

	// TranslatedName 是模型根据名称和描述生成的中文展示名。
	TranslatedName string `gorm:"column:translated_name;type:varchar(255)" json:"translated_name"`

	// Source 是数据来源，例如 Leros、ClawHub。
	Source string `gorm:"column:source;type:varchar(50);not null;uniqueIndex:idx_marketplace_item_source_skill_version" json:"source"`

	// Description 是原始描述（通常是英文）。
	Description string `gorm:"column:description;type:text" json:"description"`

	// TranslatedDescription 是模型翻译后的中文描述。
	TranslatedDescription string `gorm:"column:translated_description;type:text" json:"translated_description"`

	// Author 是 Skill 作者或发布方。
	Author string `gorm:"column:author;type:varchar(255);not null" json:"author"`

	// Installs 是下载量/安装量计数。
	Installs int64 `gorm:"column:installs;type:bigint;not null;default:0" json:"installs"`

	// Version 是当前版本。
	Version string `gorm:"column:version;type:varchar(50);not null;uniqueIndex:idx_marketplace_item_source_skill_version" json:"version"`

	// Category 是 Skill 分类。
	Category string `gorm:"column:category;type:varchar(100);not null;index" json:"category"`

	// Tags 是 Skill 标签列表。
	Tags SkillStringList `gorm:"column:tags;type:jsonb" json:"tags"`

	// PackageStoragePath 是缓存到 storage-go 的标准化 zip 包 URI，由 server 在获取详情后异步写入。
	// 格式为 storage-go URI，例如 "s3://bucket/skills/marketplace/{source}/{skill_id}/{version}/skill/package.zip"。
	// 空表示尚未缓存。Worker 安装时优先从此路径读取。
	PackageStoragePath string `gorm:"column:package_storage_path;type:varchar(500)" json:"package_storage_path"`
}

// TableName 返回数据库表名。
func (SkillMarketplaceItem) TableName() string {
	return TableNameSkillMarketplaceItem
}
