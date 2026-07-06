package types

import "gorm.io/gorm"

// Department 表示组织部门表。
type Department struct {
	gorm.Model

	Name      string `gorm:"column:name;type:varchar(100);not null;uniqueIndex:uni_dept_org_name,composite:org_id;comment:部门名称，同组织内唯一"`
	ParentID  uint   `gorm:"column:parent_id;type:bigint;not null;default:0;index:idx_dept_org_parent,priority:2;comment:父部门ID，0表示顶层部门"`
	ParentIDs []uint `gorm:"column:parent_ids;type:jsonb;serializer:json;comment:祖先部门ID链，从根到直接父部门"`
	Sort      uint   `gorm:"column:sort;type:bigint;not null;default:0;comment:同级排序"`
	OrgID     uint   `gorm:"column:org_id;type:bigint;not null;default:0;index:idx_dept_org_parent,priority:1;uniqueIndex:uni_dept_org_name,composite:name;comment:组织ID"`
}

// TableName 返回组织部门表名。
func (Department) TableName() string {
	return TableNameDepartment
}

// DepartmentList 表示组织部门列表。
type DepartmentList []Department

// BuildDepartmentParentIDs 根据父部门构建祖先 ID 链（从根到直接父部门）。
func BuildDepartmentParentIDs(parent *Department) []uint {
	if parent == nil || parent.ID == 0 {
		return nil
	}
	parentIDs := make([]uint, len(parent.ParentIDs), len(parent.ParentIDs)+1)
	copy(parentIDs, parent.ParentIDs)
	return append(parentIDs, parent.ID)
}

// ToMap 按主键索引组织部门列表。
func (l DepartmentList) ToMap() map[uint]Department {
	m := make(map[uint]Department)
	for _, v := range l {
		m[v.ID] = v
	}
	return m
}
