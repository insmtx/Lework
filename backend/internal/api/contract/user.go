package contract

import "context"

type UserService interface {
	CreateUser(ctx context.Context, req *CreateUserRequest) (*UserInfo, error)
	GetUser(ctx context.Context, id uint, githubLogin string) (*UserInfo, error)
	UpdateUser(ctx context.Context, id uint, req *UpdateUserRequest) (*UserInfo, error)
	DeleteUser(ctx context.Context, id uint) error
	ListUsers(ctx context.Context, req *ListUsersRequest) (*UserList, error)
}
