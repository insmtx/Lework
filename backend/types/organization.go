package types

import "gorm.io/gorm"

// Organization 表示系统中的组织/企业信息
//
// 组织是系统中的顶级实体，可以代表公司、团队或项目。
// 多个用户可以关联到同一个组织。
type Organization struct {
	gorm.Model
	PublicID     string `gorm:"column:public_id;type:varchar(64);uniqueIndex;not null;default:''"` // 组织公开ID
	Type         string `gorm:"column:type;type:varchar(50);default:'company'"`                    // 组织类型: company/team/project
	Code         string `gorm:"column:code;type:varchar(255);unique_index;not null"`               // 组织代码（唯一）
	Name         string `gorm:"column:name;type:varchar(255);not null"`                            // 组织名称
	Status       string `gorm:"column:status;type:varchar(20);default:'active'"`                   // 状态: active/inactive
	Description  string `gorm:"column:description;type:varchar(256)"`                              // 组织描述
	Logo         string `gorm:"column:logo;type:varchar(256)"`                                     // 组织 Logo URL
	Address      string `gorm:"column:address;type:varchar(256)"`                                  // 联系地址
	Website      string `gorm:"column:website;type:varchar(256)"`                                  // 官网地址
	CreatedByUin uint   `gorm:"column:created_by_uin;type:bigint;default:0"`                       // 创建者 Uin
}

// TableName 重写表名
func (Organization) TableName() string {
	return TableNameOrganization
}
