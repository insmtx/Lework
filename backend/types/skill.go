// types 包提供 Leros 的核心数据类型定义
//
// 该包定义了数字助手、事件、用户、技能等核心领域模型，
// 以及相关的常量和数据库表名定义。
package types

import "gorm.io/gorm"

// Skill 定义一个在数据库中持久化的技能实体
type Skill struct {
	gorm.Model
	// 技能唯一标识符，与技能实现相关联的ID
	Code string `gorm:"column:code;type:varchar(255);not null;uniqueIndex"`
	// 技能所属组织ID
	OrgID uint `gorm:"column:org_id;type:integer;not null;index"`
	// 技能拥有者ID
	OwnerID uint `gorm:"column:owner_id;type:integer;not null;index"`
	// 技能名称
	Name string `gorm:"column:name;type:varchar(255);not null"`
	// 技能描述
	Description string `gorm:"column:description;type:text"`
	// 技能图标URL
	Icon string `gorm:"column:icon;type:varchar(500)"`
	// 技能版本号
	Version string `gorm:"column:version;type:varchar(50);not null"`
	// 技能类别（例如："integration","tool","workflow","ai"等）
	Category string `gorm:"column:category;type:varchar(100);not null;index"` // 建议使用 types.SkillCategory 定义的常量值
	// 技能类型（本地技能或远程技能）
	SkillType string `gorm:"column:skill_type;type:varchar(50);not null"` // 建议使用 types.SkillType 定义的常量值
	// 技能作者
	Author string `gorm:"column:author;type:varchar(255)"`
	// 输入参数Schema定义
	InputSchema map[string]interface{} `gorm:"column:input_schema;type:jsonb"`
	// 输出参数Schema定义
	OutputSchema map[string]interface{} `gorm:"column:output_schema;type:jsonb"`
	// 技能所需权限
	Permissions []interface{} `gorm:"column:permissions;type:jsonb"`
	// 技能配置
	Config map[string]interface{} `gorm:"column:config;type:jsonb"`
	// 是否为系统内置技能
	IsSystem bool `gorm:"column:is_system;type:boolean;default:false"`
}

// TableName 指定Skill对应的数据库表名
func (Skill) TableName() string {
	return TableNameSkill
}
