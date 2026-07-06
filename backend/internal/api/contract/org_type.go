package contract

import (
	"time"

	"github.com/insmtx/Leros/backend/types"
)

type Org struct {
	PublicID    string    `json:"public_id"`
	Type        string    `json:"type"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Description string    `json:"description,omitempty"`
	Logo        string    `json:"logo,omitempty"`
	Address     string    `json:"address,omitempty"`
	Website     string    `json:"website,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateOrgRequest struct {
	Name        string `json:"name" binding:"required"`
	Code        string `json:"code" binding:"required"`
	Type        string `json:"type,omitempty"`
	Status      string `json:"status,omitempty"`
	Description string `json:"description,omitempty"`
	Logo        string `json:"logo,omitempty"`
	Address     string `json:"address,omitempty"`
	Website     string `json:"website,omitempty"`
}

type UpdateOrgRequest struct {
	Name        *string `json:"name,omitempty"`
	Type        *string `json:"type,omitempty"`
	Status      *string `json:"status,omitempty"`
	Description *string `json:"description,omitempty"`
	Logo        *string `json:"logo,omitempty"`
	Address     *string `json:"address,omitempty"`
	Website     *string `json:"website,omitempty"`
}

type ListOrgsRequest struct {
	Keyword *string `json:"keyword,omitempty"`
	Status  *string `json:"status,omitempty"`
	types.Pagination
}

type OrgList struct {
	Total  int64 `json:"total"`
	Offset int   `json:"offset"`
	Limit  int   `json:"limit"`
	Items  []Org `json:"items"`
}

type OrgMember struct {
	ID        uint      `json:"id"`
	Uin       uint      `json:"uin"`
	UserID    string    `json:"user_id"`
	OrgID     string    `json:"org_id"`
	IsDefault bool      `json:"is_default"`
	UserName  string    `json:"user_name,omitempty"`
	UserLogin string    `json:"user_login,omitempty"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	OrgName   string    `json:"org_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateOrgMemberRequest struct {
	UserID    string `json:"user_id" binding:"required"`
	OrgID     string `json:"org_id" binding:"required"`
	IsDefault bool   `json:"is_default,omitempty"`
}

type UpdateOrgMemberRequest struct {
	OrgID     *string `json:"org_id,omitempty"`
	IsDefault *bool   `json:"is_default,omitempty"`
}

type ListOrgMembersRequest struct {
	OrgID  *string `json:"org_id,omitempty"`
	UserID *string `json:"user_id,omitempty"`
	types.Pagination
}

type OrgMemberList struct {
	Total  int64       `json:"total"`
	Offset int         `json:"offset"`
	Limit  int         `json:"limit"`
	Items  []OrgMember `json:"items"`
}
