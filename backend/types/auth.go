package types

import (
	"time"

	"gorm.io/gorm"
)

// AuthRefreshToken stores a hashed refresh token for local account sessions.
type AuthRefreshToken struct {
	gorm.Model
	TokenHash string     `gorm:"column:token_hash;type:varchar(64);uniqueIndex;not null"`
	UserID    uint       `gorm:"column:user_id;type:bigint;index;not null"`
	ExpiresAt time.Time  `gorm:"column:expires_at;index;not null"`
	RevokedAt *time.Time `gorm:"column:revoked_at;index"`
}

// TableName 指定AuthRefreshToken结构体对应的数据库表名。
func (AuthRefreshToken) TableName() string {
	return TableNameAuthRefreshToken
}

// AuthLoginAttempt stores failed login counters in a rolling window.
type AuthLoginAttempt struct {
	gorm.Model
	Identifier      string    `gorm:"column:identifier;type:varchar(255);uniqueIndex;not null"`
	FailureCount    int       `gorm:"column:failure_count;type:integer;default:0;not null"`
	WindowExpiresAt time.Time `gorm:"column:window_expires_at;index;not null"`
}

// TableName 指定AuthLoginAttempt结构体对应的数据库表名。
func (AuthLoginAttempt) TableName() string {
	return TableNameAuthLoginAttempt
}

// AuthPhoneVerificationCode stores one-time phone login codes.
type AuthPhoneVerificationCode struct {
	gorm.Model
	Phone     string     `gorm:"column:phone;type:varchar(32);index;not null"`
	CodeHash  string     `gorm:"column:code_hash;type:varchar(64);not null"`
	ExpiresAt time.Time  `gorm:"column:expires_at;index;not null"`
	UsedAt    *time.Time `gorm:"column:used_at;index"`
}

// TableName 指定AuthPhoneVerificationCode结构体对应的数据库表名。
func (AuthPhoneVerificationCode) TableName() string {
	return TableNameAuthPhoneVerificationCode
}
