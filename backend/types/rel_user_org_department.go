package types

import "gorm.io/gorm"

// MemberDepartment 表示组织成员部门关联表。
type MemberDepartment struct {
	gorm.Model

	Uin          uint `gorm:"column:uin;type:bigint;not null;uniqueIndex:uni_member_dept,composite:department_id;comment:组织成员Uin"`
	OrgID        uint `gorm:"column:org_id;type:bigint;not null;comment:组织ID"`
	DepartmentID uint `gorm:"column:department_id;type:bigint;not null;index;uniqueIndex:uni_member_dept,composite:uin;comment:部门ID"`
	IsPrimary    bool `gorm:"column:is_primary;type:boolean;not null;default:false;comment:是否为主部门"`
}

// TableName 返回组织成员部门关联表名。
func (MemberDepartment) TableName() string {
	return TableNameMemberDepartment
}

// MemberDepartmentList 表示组织成员部门关联列表。
type MemberDepartmentList []MemberDepartment

// ToMap 按主键索引组织成员部门关联列表。
func (l MemberDepartmentList) ToMap() map[uint]MemberDepartment {
	m := make(map[uint]MemberDepartment)
	for _, v := range l {
		m[v.ID] = v
	}
	return m
}
