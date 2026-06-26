package types

import (
	"gorm.io/gorm"
)

// ProjectFileResourceType 项目文件关联的资源类型
type ProjectFileResourceType string

const (
	ProjectFileResourceTypeUserUpload ProjectFileResourceType = "user_upload" // 用户上传
	ProjectFileResourceTypeArtifact   ProjectFileResourceType = "artifact"    // 工作产物
)

// ProjectFile 项目文件关联表，记录项目/任务/资源之间的映射关系
type ProjectFile struct {
	gorm.Model
	FilePublicID string                  `gorm:"column:file_public_id;type:varchar(255);not null;uniqueIndex"`
	OrgID        uint                    `gorm:"column:org_id;type:integer;not null;index"`
	ProjectID    uint                    `gorm:"column:project_id;type:bigint;not null;index"`
	TaskID       uint                    `gorm:"column:task_id;type:bigint;index"`
	ResourceID   uint                    `gorm:"column:resource_id;type:bigint;not null;index"`
	ResourceType ProjectFileResourceType `gorm:"column:resource_type;type:varchar(50);not null;index"`
	Uin          uint                    `gorm:"column:uin;type:bigint;index"`
}

func (ProjectFile) TableName() string {
	return TableNameProjectFile
}
