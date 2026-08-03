package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// PluginStringList is a non-null JSON array used by plugin marketplace metadata.
type PluginStringList []string

// Scan implements sql.Scanner.
func (s *PluginStringList) Scan(value interface{}) error {
	if value == nil {
		*s = PluginStringList{}
		return nil
	}
	var bytes []byte
	switch typed := value.(type) {
	case []byte:
		bytes = typed
	case string:
		bytes = []byte(typed)
	default:
		return fmt.Errorf("cannot scan %T into PluginStringList", value)
	}
	var result []string
	if err := json.Unmarshal(bytes, &result); err != nil {
		return err
	}
	*s = PluginStringList(result)
	return nil
}

// Value implements driver.Valuer and always stores a JSON array.
func (s PluginStringList) Value() (driver.Value, error) {
	if len(s) == 0 {
		return "[]", nil
	}
	return json.Marshal([]string(s))
}

const (
	// PluginStatusActive means the plugin can be used by future executions.
	PluginStatusActive = "active"
	// PluginStatusArchived means the plugin is retained for history but cannot be newly used.
	PluginStatusArchived = "archived"
)

// Plugin is a scope-owned plugin identity shared by organizations and the system catalogue.
type Plugin struct {
	gorm.Model

	PublicID        string     `gorm:"column:public_id;type:varchar(255);not null"`
	OwnerScope      OwnerScope `gorm:"column:owner_scope;type:varchar(32);not null;default:'organization';index;check:chk_plugin_owner_scope,(owner_scope = 'organization' AND org_id > 0) OR (owner_scope = 'system' AND org_id = 0)"`
	OrgID           uint       `gorm:"column:org_id;type:bigint;not null"`
	Code            string     `gorm:"column:code;type:varchar(128);not null"`
	Kind            string     `gorm:"column:kind;type:varchar(32);not null"`
	Name            string     `gorm:"column:name;type:varchar(255);not null"`
	Description     string     `gorm:"column:description;type:text"`
	Status          string     `gorm:"column:status;type:varchar(32);not null;default:'active'"`
	Origin          string     `gorm:"column:origin;type:varchar(32);not null;default:'org'"`
	CurrentRevision int        `gorm:"column:current_revision;type:integer;not null;default:0"`
	CreatedBy       uint       `gorm:"column:created_by;type:bigint;not null"`
	UpdatedBy       uint       `gorm:"column:updated_by;type:bigint;not null"`
}

// TableName returns the plugin table name.
func (Plugin) TableName() string {
	return TableNamePlugin
}

// PluginRevision is an immutable published plugin package record.
type PluginRevision struct {
	gorm.Model

	PluginID                uint            `gorm:"column:plugin_id;type:bigint;not null"`
	SourceMarketplaceItemID *uint           `gorm:"column:source_marketplace_item_id;type:bigint"`
	SourcePluginRevisionID  *uint           `gorm:"column:source_plugin_revision_id;type:bigint"`
	Revision                int             `gorm:"column:revision;type:integer;not null"`
	Status                  string          `gorm:"column:status;type:varchar(32);not null;default:'published'"`
	Definition              json.RawMessage `gorm:"column:definition;type:jsonb;not null;default:'{}'"`
	PublishedByType         string          `gorm:"column:published_by_type;type:varchar(32);not null"`
	PublishedByID           uint            `gorm:"column:published_by_id;type:bigint;not null"`
	PublishedAt             time.Time       `gorm:"column:published_at;not null"`
}

// TableName returns the plugin revision table name.
func (PluginRevision) TableName() string {
	return TableNamePluginRevision
}

// ProjectPluginBinding authorizes a plugin for one project.
type ProjectPluginBinding struct {
	gorm.Model

	ProjectID uint   `gorm:"column:project_id;type:bigint;not null"`
	PluginID  uint   `gorm:"column:plugin_id;type:bigint;not null"`
	Enabled   bool   `gorm:"column:enabled;type:boolean;not null;default:true"`
	Config    []byte `gorm:"column:config;type:jsonb;not null;default:'{}'"`
	CreatedBy uint   `gorm:"column:created_by;type:bigint;not null"`
	UpdatedBy uint   `gorm:"column:updated_by;type:bigint;not null"`
}

// TableName returns the project plugin binding table name.
func (ProjectPluginBinding) TableName() string {
	return TableNameProjectPluginBinding
}

// PluginMarketplaceItem is a system-maintained public plugin directory item.
type PluginMarketplaceItem struct {
	gorm.Model

	PublicID    string           `gorm:"column:public_id;type:varchar(255);not null"`
	PluginID    uint             `gorm:"column:plugin_id;type:bigint;not null;check:chk_marketplace_plugin_id,plugin_id > 0"`
	Kind        string           `gorm:"column:kind;type:varchar(32);not null"`
	Code        string           `gorm:"column:code;type:varchar(128);not null"`
	Name        string           `gorm:"column:name;type:varchar(255);not null"`
	Description string           `gorm:"column:description;type:text"`
	Author      string           `gorm:"column:author;type:varchar(255);not null;default:''"`
	SourceType  string           `gorm:"column:source_type;type:varchar(32);not null"`
	SourceRef   string           `gorm:"column:source_ref;type:varchar(1000);not null"`
	Status      string           `gorm:"column:status;type:varchar(32);not null;default:'published'"`
	Category    string           `gorm:"column:category;type:varchar(100);not null;default:''"`
	Tags        PluginStringList `gorm:"column:tags;type:jsonb;not null;default:'[]'"`
	Icon        string           `gorm:"column:icon;type:varchar(1000)"`
	Verified    bool             `gorm:"column:verified;type:boolean;not null;default:false"`
	PublishedAt time.Time        `gorm:"column:published_at;not null"`
}

// TableName returns the plugin marketplace item table name.
func (PluginMarketplaceItem) TableName() string {
	return TableNamePluginMarketplaceItem
}
