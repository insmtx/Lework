package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// SkillStringList 是 []string 的 JSONB 存储类型。
type SkillStringList []string

// Scan 实现 sql.Scanner 接口。
func (s *SkillStringList) Scan(value interface{}) error {
	if value == nil {
		*s = SkillStringList{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("cannot scan %T into SkillStringList", value)
	}
	var result []string
	if err := json.Unmarshal(bytes, &result); err != nil {
		return err
	}
	*s = SkillStringList(result)
	return nil
}

// Value 实现 driver.Valuer 接口。
func (s SkillStringList) Value() (driver.Value, error) {
	if len(s) == 0 {
		return nil, nil
	}
	return json.Marshal([]string(s))
}

// BuiltinSkillMarketplaceItem 内置 Skill 市场条目。
type BuiltinSkillMarketplaceItem struct {
	gorm.Model

	// SkillID 是内置 Skill 的唯一业务编码。
	SkillID string `gorm:"column:skill_id;type:varchar(100);not null;uniqueIndex:idx_builtin_skill_id_version" json:"skill_id"`

	// Name 是 Skill 在市场页面展示的名称。
	Name string `gorm:"column:name;type:varchar(255);not null" json:"name"`

	// Description 是 Skill 的简短说明。
	Description string `gorm:"column:description;type:text" json:"description"`

	// Version 是当前发布版本。
	Version string `gorm:"column:version;type:varchar(50);not null;uniqueIndex:idx_builtin_skill_id_version" json:"version"`

	// Author 是 Skill 作者或发布方。
	Author string `gorm:"column:author;type:varchar(255);not null;default:Leros" json:"author"`

	// Category 是 Skill 分类。
	Category string `gorm:"column:category;type:varchar(100);not null;index" json:"category"`

	// Tags 是 Skill 标签列表。
	Tags SkillStringList `gorm:"column:tags;type:jsonb" json:"tags"`

	// Icon 是 Skill 图标地址或内置图标标识。
	Icon string `gorm:"column:icon;type:varchar(1000)" json:"icon"`

	// LocalPath 是内置 Skill 在服务端的本地路径或打包路径。
	LocalPath string `gorm:"column:local_path;type:varchar(1000);not null" json:"local_path"`

	// PackageSHA256 是内置 Skill 包的 SHA256 校验值。
	PackageSHA256 string `gorm:"column:package_sha256;type:char(64)" json:"package_sha256"`

	// Verified 表示该 Skill 是否经过官方验证。
	Verified bool `gorm:"column:verified;type:boolean;not null;default:true" json:"verified"`

	// Status 表示市场条目状态：active、deprecated、hidden。
	Status string `gorm:"column:status;type:varchar(50);not null;default:active;index" json:"status"`

	// PublishedAt 是该 Skill 发布到市场的时间。
	PublishedAt *time.Time `gorm:"column:published_at" json:"published_at"`

	// Installs 是下载量/安装量计数。
	Installs int64 `gorm:"column:installs;type:bigint;not null;default:0" json:"installs"`
}

// TableName 返回数据库表名。
func (BuiltinSkillMarketplaceItem) TableName() string {
	return TableNameBuiltinSkillMarketplaceItem
}
