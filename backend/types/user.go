// types 包提供 Leros 的核心数据类型定义
//
// 该包定义了数字助手、事件、用户、技能等核心领域模型，
// 以及相关的常量和数据库表名定义。
package types

import (
	"gorm.io/gorm"
)

// User 表示系统中的用户信息
type User struct {
	gorm.Model
	ExternalID uint   `gorm:"column:external_id;index"`                                          // identity-platform User.ID
	PublicID   string `gorm:"column:public_id;type:varchar(64);uniqueIndex;not null;default:''"` // 用户公开ID
	Password   string `gorm:"column:password;type:varchar(255)"`                                 // 密码（本地认证用）
	Name       string `gorm:"column:name;type:varchar(255)"`                                     // 用户姓名
	Email      string `gorm:"column:email;type:varchar(255)"`                                    // 用户邮箱
	Phone      string `gorm:"column:phone;type:varchar(32);uniqueIndex"`                         // 手机号
	AvatarURL  string `gorm:"column:avatar_url;type:varchar(500)"`                               // 头像 URL
}

// TableName 重写表名
func (User) TableName() string {
	return TableNameUser
}
