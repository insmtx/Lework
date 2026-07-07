package types

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// ResourceType 标识受权限管理的业务对象类型。
type ResourceType string

const (
	// ResourceTypeProject 表示项目根资源。
	ResourceTypeProject ResourceType = "project"
	// ResourceTypeFile 表示项目下的文件资源。
	ResourceTypeFile ResourceType = "file"
	// ResourceTypeArtifact 表示项目下的产物资源。
	ResourceTypeArtifact ResourceType = "artifact"
)

// ResourcePathIDs 按资源树顺序存储祖先资源 ID。
type ResourcePathIDs []uint

// Scan 实现 sql.Scanner，用于读取 PostgreSQL bigint[] 值。
func (ids *ResourcePathIDs) Scan(value interface{}) error {
	if value == nil {
		*ids = ResourcePathIDs{}
		return nil
	}

	var raw string
	switch v := value.(type) {
	case []byte:
		raw = string(v)
	case string:
		raw = v
	default:
		return fmt.Errorf("cannot scan %T into ResourcePathIDs", value)
	}

	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		*ids = ResourcePathIDs{}
		return nil
	}
	if !strings.HasPrefix(raw, "{") || !strings.HasSuffix(raw, "}") {
		return fmt.Errorf("invalid ResourcePathIDs value %q", raw)
	}

	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(raw, "{"), "}"), ",")
	result := make(ResourcePathIDs, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return fmt.Errorf("parse resource path id %q: %w", part, err)
		}
		result = append(result, uint(id))
	}

	*ids = result
	return nil
}

// Value 实现 driver.Valuer，用于写入 PostgreSQL bigint[] 值。
func (ids ResourcePathIDs) Value() (driver.Value, error) {
	if len(ids) == 0 {
		return "{}", nil
	}

	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatUint(uint64(id), 10))
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

// Resource 是统一的权限资源记录。
type Resource struct {
	gorm.Model

	// OrgID 将资源限定在特定组织范围内。
	OrgID uint `gorm:"column:org_id;type:bigint;not null;index:idx_leros_resource_org_type;uniqueIndex:ux_leros_resource_active_biz,where:deleted_at IS NULL"`
	// Uin 记录资源创建或归属用户，不作为最终权限来源。
	Uin uint `gorm:"column:uin;type:bigint;not null;default:0;index:idx_leros_resource_uin"`
	// Type 是资源类型，PermissionService 按资源类型和动作判断权限。
	Type ResourceType `gorm:"column:type;type:varchar(50);not null;index:idx_leros_resource_org_type;uniqueIndex:ux_leros_resource_active_biz,where:deleted_at IS NULL"`
	// BizID 指向业务对象的内部 ID，例如 projects.id、文件 ID、artifact ID。
	BizID uint `gorm:"column:biz_id;type:bigint;not null;uniqueIndex:ux_leros_resource_active_biz,where:deleted_at IS NULL"`
	// ParentResourceID 指向父资源，用于继承权限。
	ParentResourceID *uint `gorm:"column:parent_resource_id;type:bigint;index:idx_leros_resource_parent"`
	// ParentResourcePathIDs 从根到父节点存储祖先资源 ID。
	ParentResourcePathIDs ResourcePathIDs `gorm:"column:parent_resource_path_ids;type:bigint[];not null;default:'{}'"`
}

// TableName 返回 Resource 对应的数据库表名。
func (Resource) TableName() string {
	return TableNameResource
}
