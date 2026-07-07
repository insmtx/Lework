package types

import "gorm.io/gorm"

// ResourceRole 描述主体与资源之间的关系角色。
type ResourceRole string

const (
	// ResourceRoleOwner 授予 owner 级别的策略权限。
	ResourceRoleOwner ResourceRole = "owner"
	// ResourceRoleAdmin 授予 admin 级别的策略权限。
	ResourceRoleAdmin ResourceRole = "admin"
	// ResourceRoleMember 授予 member 级别的策略权限。
	ResourceRoleMember ResourceRole = "member"
)

// ResourceRoleStrength 定义各资源角色的强度值，用于跨资源层继承时取最高角色。
// owner > admin > member；未知角色强度视为 0。
var ResourceRoleStrength = map[ResourceRole]int{
	ResourceRoleOwner:  3,
	ResourceRoleAdmin:  2,
	ResourceRoleMember: 1,
}

// ResourceBinding 表示用户或助手在资源上的身份绑定。
// Uin 和 AssistantID 互斥：同一条绑定只能属于用户或助手之一。
type ResourceBinding struct {
	gorm.Model

	// OrgID 冗余组织字段，用于快速过滤和防止跨组织误查。
	OrgID uint `gorm:"column:org_id;type:bigint;not null;index:idx_leros_resource_binding_org_uin,priority:1;index:idx_leros_resource_binding_org_assistant,priority:1"`
	// Uin 是被绑定用户的 Uin，用户主体时非 0。
	Uin uint `gorm:"column:uin;type:bigint;not null;default:0;index:idx_leros_resource_binding_uin;uniqueIndex:ux_leros_resource_binding_uin,priority:2,where:deleted_at IS NULL AND uin > 0;index:idx_leros_resource_binding_org_uin,priority:2"`
	// ResourceID 是绑定的资源 ID。
	ResourceID uint `gorm:"column:resource_id;type:bigint;not null;index:idx_leros_resource_binding_resource;uniqueIndex:ux_leros_resource_binding_uin,priority:1,where:deleted_at IS NULL AND uin > 0;uniqueIndex:ux_leros_resource_binding_assistant,priority:1,where:deleted_at IS NULL AND assistant_id IS NOT NULL"`
	// AssistantID 是被绑定的助手 ID，与 Uin 互斥，助手主体时非空。
	AssistantID *uint `gorm:"column:assistant_id;type:bigint;uniqueIndex:ux_leros_resource_binding_assistant,priority:2,where:deleted_at IS NULL AND assistant_id IS NOT NULL;index:idx_leros_resource_binding_org_assistant,priority:2"`
	// Role 是主体在该资源上的角色。
	Role ResourceRole `gorm:"column:resource_role;type:varchar(50);not null"`
}

// TableName 返回 ResourceBinding 对应的数据库表名。
func (ResourceBinding) TableName() string {
	return TableNameResourceBinding
}
