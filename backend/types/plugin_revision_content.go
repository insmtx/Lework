package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
)

const (
	// PluginRevisionContentSchemaSkillV1 identifies the first Skill content snapshot shape.
	PluginRevisionContentSchemaSkillV1 = "skill-content/v1"
)

// PluginRevisionFile describes one immutable regular file in a plugin package.
type PluginRevisionFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

// PluginRevisionFileList is the stable, path-sorted file index of a plugin revision.
type PluginRevisionFileList []PluginRevisionFile

// Scan implements sql.Scanner.
func (files *PluginRevisionFileList) Scan(value interface{}) error {
	if value == nil {
		*files = PluginRevisionFileList{}
		return nil
	}
	var raw []byte
	switch typed := value.(type) {
	case []byte:
		raw = typed
	case string:
		raw = []byte(typed)
	default:
		return fmt.Errorf("cannot scan %T into PluginRevisionFileList", value)
	}
	var result []PluginRevisionFile
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	*files = PluginRevisionFileList(result)
	return nil
}

// Value implements driver.Valuer and always stores a JSON array.
func (files PluginRevisionFileList) Value() (driver.Value, error) {
	if len(files) == 0 {
		return "[]", nil
	}
	return json.Marshal([]PluginRevisionFile(files))
}

// PluginRevisionContent is the one-to-one immutable content projection of a plugin revision.
type PluginRevisionContent struct {
	gorm.Model

	PluginRevisionID  uint                   `gorm:"column:plugin_revision_id;type:bigint;not null;uniqueIndex:ux_plugin_revision_content_revision"`
	Schema            string                 `gorm:"column:schema;type:varchar(32);not null"`
	ArtifactSHA256    string                 `gorm:"column:artifact_sha256;type:varchar(64);not null"`
	EntrypointPath    string                 `gorm:"column:entrypoint_path;type:varchar(1000);not null"`
	EntrypointContent string                 `gorm:"column:entrypoint_content;type:text;not null"`
	FileIndex         PluginRevisionFileList `gorm:"column:file_index;type:jsonb;not null;default:'[]'"`
}

// TableName returns the plugin revision content table name.
func (PluginRevisionContent) TableName() string {
	return TableNamePluginRevisionContent
}
