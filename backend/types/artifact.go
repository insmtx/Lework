package types

import (
	"gorm.io/gorm"
)

// Artifact 表示任务产出物（交付件）
//
// Artifact 是任务执行后的产出物记录。每个 Artifact 必须关联一个 Task，
// project_id 为冗余字段便于项目资产聚合查询。Artifact 不存储 inline 内容正文，
// 统一以 file_url 指向文件路径或存储链接。
// 通过 version + parent_id 构建版本链，支持产出物变更追溯。
type Artifact struct {
	gorm.Model
	PublicID string `gorm:"column:public_id;type:varchar(255);not null;uniqueIndex"`
	OrgID    uint   `gorm:"column:org_id;type:integer;not null;index"`
	OwnerID  uint   `gorm:"column:owner_id;type:bigint;not null;index"`
	TaskID   uint   `gorm:"column:task_id;type:bigint;not null;index"`

	// ProjectID 关联项目（冗余字段，便于项目资产聚合查询）
	ProjectID uint  `gorm:"column:project_id;type:bigint;not null;index"`
	SessionID *uint `gorm:"column:session_id;type:bigint;index"`

	Title       string `gorm:"column:title;type:varchar(500);not null"`
	Description string `gorm:"column:description;type:text"`

	// ArtifactType 产出物类型，使用 ArtifactType 常量值
	ArtifactType string `gorm:"column:artifact_type;type:varchar(50);not null;index"`

	// FileURL 文件链接或路径
	FileURL  string `gorm:"column:file_url;type:varchar(2000);not null"`
	MimeType string `gorm:"column:mime_type;type:varchar(100)"`
	FileSize int64  `gorm:"column:file_size;type:bigint"`

	// ExportFormat 导出格式：markdown / pdf / word / html
	ExportFormat string `gorm:"column:export_format;type:varchar(50)"`

	// Version 版本号，从 1 起递增
	Version  int   `gorm:"column:version;type:integer;not null;default:1"`
	ParentID *uint `gorm:"column:parent_id;type:bigint"`

	// Status 状态，使用 ArtifactStatus 常量值
	Status string `gorm:"column:status;type:varchar(50);not null;default:'completed';index"`

	Metadata ObjectMetadata `gorm:"column:metadata;type:jsonb"`
}

// TableName 指定 Artifact 结构体对应的数据库表名
func (Artifact) TableName() string {
	return TableNameArtifact
}
