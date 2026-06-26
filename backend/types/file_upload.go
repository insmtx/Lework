package types

import (
	"gorm.io/gorm"
)

// FileUpload 表示用户上传的文件
type FileUpload struct {
	gorm.Model
	PublicID     string         `gorm:"column:public_id;type:varchar(255);not null;uniqueIndex"`
	OrgID        uint           `gorm:"column:org_id;type:integer;not null;index"`
	OwnerID      uint           `gorm:"column:owner_id;type:integer;not null;index"`
	Filename     string         `gorm:"column:filename;type:varchar(500)"`
	OriginalName string         `gorm:"column:original_name;type:varchar(500)"`
	MimeType     string         `gorm:"column:mime_type;type:varchar(100)"`
	FileSize     int64          `gorm:"column:file_size;type:bigint"`
	StorageURI  string         `gorm:"column:storage_uri;type:varchar(500);not null"`
	Sha256       string         `gorm:"column:sha256;type:varchar(64);index"`
	Purpose      string         `gorm:"column:purpose;type:varchar(50);index"`
	Status       string         `gorm:"column:status;type:varchar(50);not null;default:'active';index"`
	Metadata     ObjectMetadata `gorm:"column:metadata;type:jsonb"`
}

// TableName 指定 FileUpload 结构体对应的数据库表名
func (FileUpload) TableName() string {
	return TableNameFileUpload
}
