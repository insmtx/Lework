package contract

import "context"

type UserOrgService interface {
	CreateUserOrg(ctx context.Context, req *CreateUserOrgRequest) (*UserOrg, error)
	GetUserOrg(ctx context.Context, id uint, uin uint) (*UserOrg, error)
	UpdateUserOrg(ctx context.Context, id uint, req *UpdateUserOrgRequest) (*UserOrg, error)
	DeleteUserOrg(ctx context.Context, id uint) error
	ListUserOrgs(ctx context.Context, req *ListUserOrgsRequest) (*UserOrgList, error)
}
