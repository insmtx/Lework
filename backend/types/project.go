package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Project 表示系统中的项目
//
// 项目是长期工作的核心容器，承载会话、任务、AI队友、知识库和项目资产。
// 项目提供了长期上下文和协作空间。
type Project struct {
	gorm.Model
	// project - 项目唯一标识，格式如：prj_xxx，VARCHAR(255)，NOT NULL，UNIQUE
	PublicID string `gorm:"column:public_id;type:varchar(255);not null;uniqueIndex"`

	// project - 所属组织ID，INTEGER，NOT NULL，INDEX
	OrgID uint `gorm:"column:org_id;type:integer;not null;index"`

	// project - 创建者用户ID，INTEGER，NOT NULL，INDEX
	OwnerID uint `gorm:"column:owner_id;type:integer;not null;index"`

	// project - 项目名称，VARCHAR(255)，NOT NULL
	Name string `gorm:"column:name;type:varchar(255);not null"`

	// project - 项目描述，TEXT，允许为空
	Description string `gorm:"column:description;type:text"`

	// project - 项目状态，VARCHAR(50)，NOT NULL，DEFAULT 'active'
	Status string `gorm:"column:status;type:varchar(50);not null;default:'active';index"`

	// project - 元数据（JSON格式存储标签等扩展信息），JSONB，允许为空
	Metadata ProjectMetadata `gorm:"column:metadata;type:jsonb"`
}

// TableName 指定Project结构体对应的数据库表名
func (Project) TableName() string {
	return TableNameProject
}

// ProjectMetadata 项目元数据结构
type ProjectMetadata struct {
	// 元数据 - 项目标签
	Tags []string `json:"tags,omitempty"`
	// 元数据 - 项目类型/模板标识
	Type string `json:"type,omitempty"`
	// 元数据 - 其他扩展字段
	Extra map[string]interface{} `json:"extra,omitempty"`
}

// Scan 实现 sql.Scanner 接口
func (pm *ProjectMetadata) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("cannot scan %T into ProjectMetadata", value)
	}

	return json.Unmarshal(bytes, pm)
}

// Value 实现 driver.Valuer 接口
func (pm ProjectMetadata) Value() (driver.Value, error) {
	return json.Marshal(pm)
}

// ProjectMember 项目成员关联表
//
// 关联项目与用户或数字助手，定义项目中的成员类型和角色。
type ProjectMember struct {
	gorm.Model
	// project_member - 关联项目ID，BIGINT，NOT NULL，INDEX
	ProjectID uint `gorm:"column:project_id;type:bigint;not null;index"`

	// project_member - 成员ID（用户ID或数字助手ID），BIGINT，NOT NULL，INDEX
	MemberID uint `gorm:"column:member_id;type:bigint;not null;index"`

	// project_member - 成员类型（user/assistant），VARCHAR(50)，NOT NULL
	MemberType MemberType `gorm:"column:member_type;type:varchar(50);not null;default:'user'"`

	// project_member - 成员角色（owner/admin/member/viewer），VARCHAR(50)，NOT NULL
	MemberRole MemberRole `gorm:"column:member_role;type:varchar(50);not null;default:'member'"`

	// project_member - 加入时间，TIMESTAMP，NOT NULL
	JoinedAt time.Time `gorm:"column:joined_at;not null;default:now()"`
}

// TableName 指定ProjectMember结构体对应的数据库表名
func (ProjectMember) TableName() string {
	return TableNameProjectMember
}
