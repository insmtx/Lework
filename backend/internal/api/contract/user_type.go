package contract

import "github.com/insmtx/Leros/backend/internal/adapter/account"

type UserInfo struct {
	account.UserInfo
}

type CreateUserRequest struct {
	account.CreateUserInput
}

type CreateUserResponse struct {
	account.CreateUserResponse
}

type UpdateUserRequest struct {
	account.UpdateUserInput
}

type UpdateCurrentUserRequest struct {
	account.UpdateCurrentUserInput
}

type ListUserRequest struct {
	account.ListUserInput
}

type UserList struct {
	account.UserList
}

type ListUinRequest struct{}

type ListUinResponse struct {
	account.ListUinOutput
}
