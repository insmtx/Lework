package contract

import (
	"time"

	"github.com/insmtx/Leros/backend/types"
)

type UserInfo struct {
	PublicID    string    `json:"public_id"`
	GithubID    int64     `json:"github_id,omitempty"`
	GithubLogin string    `json:"github_login"`
	Name        string    `json:"name"`
	Email       string    `json:"email,omitempty"`
	Phone       string    `json:"phone,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	Bio         string    `json:"bio,omitempty"`
	Company     string    `json:"company,omitempty"`
	Location    string    `json:"location,omitempty"`
	PublicRepos int       `json:"public_repos,omitempty"`
	Followers   int       `json:"followers,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateUserRequest struct {
	GithubLogin string `json:"github_login" binding:"required"`
	Password    string `json:"password,omitempty"`
	Name        string `json:"name" binding:"required"`
	Email       string `json:"email,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Bio         string `json:"bio,omitempty"`
	Company     string `json:"company,omitempty"`
	Location    string `json:"location,omitempty"`
}

type UpdateUserRequest struct {
	GithubLogin *string `json:"github_login,omitempty"`
	Password    *string `json:"password,omitempty"`
	Name        *string `json:"name,omitempty"`
	Email       *string `json:"email,omitempty"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
	Bio         *string `json:"bio,omitempty"`
	Company     *string `json:"company,omitempty"`
	Location    *string `json:"location,omitempty"`
}

type ListUsersRequest struct {
	Keyword     *string `json:"keyword,omitempty"`
	GithubLogin *string `json:"github_login,omitempty"`
	types.Pagination
}

type UserList struct {
	Total  int64      `json:"total"`
	Offset int        `json:"offset"`
	Limit  int        `json:"limit"`
	Items  []UserInfo `json:"items"`
}
