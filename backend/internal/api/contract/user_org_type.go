package contract

import (
	"time"

	"github.com/insmtx/Leros/backend/types"
)

type UserOrg struct {
	ID        uint      `json:"id"`
	Uin       uint      `json:"uin"`
	UserID    uint      `json:"user_id"`
	OrgID     uint      `json:"org_id"`
	IsDefault bool      `json:"is_default"`
	UserName  string    `json:"user_name,omitempty"`
	UserLogin string    `json:"user_login,omitempty"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	OrgName   string    `json:"org_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateUserOrgRequest struct {
	UserID    uint `json:"user_id" binding:"required"`
	OrgID     uint `json:"org_id" binding:"required"`
	IsDefault bool `json:"is_default,omitempty"`
}

type UpdateUserOrgRequest struct {
	OrgID     *uint `json:"org_id,omitempty"`
	IsDefault *bool `json:"is_default,omitempty"`
}

type ListUserOrgsRequest struct {
	OrgID  *uint `json:"org_id,omitempty"`
	UserID *uint `json:"user_id,omitempty"`
	types.Pagination
}

type UserOrgList struct {
	Total  int64     `json:"total"`
	Offset int       `json:"offset"`
	Limit  int       `json:"limit"`
	Items  []UserOrg `json:"items"`
}
