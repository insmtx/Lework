package contract

import (
	"context"
	"time"

	"github.com/insmtx/Leros/backend/types"
)

type MemberDepartmentService interface {
	CreateMemberDepartment(ctx context.Context, req *CreateMemberDepartmentRequest) (*MemberDepartment, error)
	GetMemberDepartment(ctx context.Context, id uint) (*MemberDepartment, error)
	UpdateMemberDepartment(ctx context.Context, id uint, req *UpdateMemberDepartmentRequest) (*MemberDepartment, error)
	DeleteMemberDepartment(ctx context.Context, id uint) error
	ListMemberDepartments(ctx context.Context, req *ListMemberDepartmentsRequest) (*MemberDepartmentList, error)
}

type MemberDepartment struct {
	ID           uint      `json:"id"`
	Uin          uint      `json:"uin"`
	OrgID        uint      `json:"org_id"`
	DepartmentID uint      `json:"department_id"`
	IsPrimary    bool      `json:"is_primary"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CreateMemberDepartmentRequest struct {
	Uin          uint `json:"uin" binding:"required"`
	DepartmentID uint `json:"department_id" binding:"required"`
	IsPrimary    bool `json:"is_primary,omitempty"`
}

type UpdateMemberDepartmentRequest struct {
	Uin          *uint `json:"uin,omitempty"`
	DepartmentID *uint `json:"department_id,omitempty"`
	IsPrimary    *bool `json:"is_primary,omitempty"`
}

type ListMemberDepartmentsRequest struct {
	Uin          *uint `json:"uin,omitempty"`
	DepartmentID *uint `json:"department_id,omitempty"`
	OrgID        *uint `json:"org_id,omitempty"`
	IsPrimary    *bool `json:"is_primary,omitempty"`
	types.Pagination
}

type MemberDepartmentList struct {
	Total  int64              `json:"total"`
	Offset int                `json:"offset"`
	Limit  int                `json:"limit"`
	Items  []MemberDepartment `json:"items"`
}
