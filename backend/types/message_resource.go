package types

import "gorm.io/gorm"

// MessageResource records which resource (skill/MCP/tool/etc.) a message used.
type MessageResource struct {
	gorm.Model
	ResourceID   string `gorm:"column:resource_id;type:varchar(255);not null;index"`  // DB primary key of the resource (empty if not found)
	MessageID    uint   `gorm:"column:message_id;type:bigint;not null;index"`         // FK to leros_session_message.id
	SessionID    uint   `gorm:"column:session_id;type:bigint;not null;index"`         // FK to leros_session.id
	OrgID        uint   `gorm:"column:org_id;type:integer;not null;default:0;index"`  // org isolation
	Uin          uint   `gorm:"column:uin;type:bigint;default:0;index"`               // user isolation
	ResourceType string `gorm:"column:resource_type;type:varchar(50);not null;index"` // skill / MCP / tool / ...
	ResourceKey  string `gorm:"column:resource_key;type:varchar(255);not null;index"` // {source}:{skill_id} composite key
	ResourceName string `gorm:"column:resource_name;type:varchar(255);not null"`      // resource display name
	InvokeType   string `gorm:"column:invoke_type;type:varchar(50);not null"`         // slash_command / mention / auto / ...
	Seq          int    `gorm:"column:seq;type:integer;not null;default:0"`           // order within the same message
}

// TableName specifies the database table name for MessageResource.
func (MessageResource) TableName() string {
	return TableNameMessageResource
}
