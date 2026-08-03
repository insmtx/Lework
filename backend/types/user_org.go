package types

import "gorm.io/gorm"

// UserOrg 表示用户与组织的关联关系
//
// 该表是多对多关系的中间表，用于关联用户和组织。
// 每个用户可以关联多个组织，其中一个可以标记为默认组织。
type UserOrg struct {
	gorm.Model
	ExternalUin uint `gorm:"column:external_uin;index"`                                                 // identity-platform UIN.ID
	UserID      uint `gorm:"column:user_id;type:bigint;index;not null;uniqueIndex:uni_user_org_member"` // 用户ID
	OrgID       uint `gorm:"column:org_id;type:bigint;index;not null;uniqueIndex:uni_user_org_member"`  // 组织ID
	IsDefault   bool `gorm:"column:is_default;type:boolean;default:false"`                              // 是否为默认组织
}

// TableName 重写表名
func (UserOrg) TableName() string {
	return TableNameUserOrg
}
