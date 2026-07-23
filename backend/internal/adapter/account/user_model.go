package account

import (
	"time"

	"github.com/insmtx/Leros/backend/types"
)

type UserInfo struct {
	ID        uint      `json:"id"`
	PublicID  string    `json:"public_id"`
	Uin       uint      `json:"uin"`
	Name      string    `json:"name"`
	Email     string    `json:"email,omitempty"`
	Phone     string    `json:"phone,omitempty"`
	AvatarURL   string                `json:"avatar_url,omitempty"`
	Departments []OrgMemberDepartment `json:"departments,omitempty"`
	CreatedAt   time.Time             `json:"created_at"`
	UpdatedAt   time.Time             `json:"updated_at"`
}

type CreateUserInput struct {
	Phone         string `json:"phone"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	DepartmentIDs []uint `json:"department_ids"`
}

type UpdateUserInput struct {
	Name      string  `json:"name"`
	AvatarURL string  `json:"avatar_url"`
	Email     *string `json:"email"`
}

type ListUserInput struct {
	Keyword      *string `json:"keyword,omitempty"`
	DepartmentID *uint   `json:"department_id,omitempty"`
	types.Pagination
}

type UserList struct {
	Total  int64      `json:"total"`
	Offset int        `json:"offset"`
	Limit  int        `json:"limit"`
	Items  []UserInfo `json:"items"`
}

type CreateUserResponse struct {
	UserID uint   `json:"user_id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Phone  string `json:"phone"`
	IsNew  bool   `json:"is_new"`
}

type UinInfo struct {
	Uin           uint       `json:"uin"`
	UserID        uint       `json:"user_id"`
	SubjectType   string     `json:"subject_type"`
	SubjectID     uint       `json:"subject_id"`
	Name          string     `json:"name"`
	UinStatus     string     `json:"uin_status"`
	Issuer        string     `json:"issuer,omitempty"`
	CompanyName   string     `json:"company_name,omitempty"`
	CompanyLogo   string     `json:"company_logo,omitempty"`
	CompanyStatus string     `json:"company_status,omitempty"`
	Role          string     `json:"role,omitempty"`
	LastLoginAt   *time.Time `json:"last_login_at,omitempty"`
}

type ListUinOutput struct {
	Uin []UinInfo `json:"uin"`
}
