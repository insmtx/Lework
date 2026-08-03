package account

import (
	"context"

	"github.com/insmtx/Leros/backend/types"
)

type AuthProvider interface {
	RegisterByEmail(ctx context.Context, req *RegisterByEmailInput) (*AuthTokens, error)
	LoginByPassword(ctx context.Context, req *LoginByPasswordInput) (*LoginByPasswordOutput, error)
	SendPhoneLoginCode(ctx context.Context, req *SendPhoneLoginCodeInput) (*SendPhoneLoginCodeOutput, error)
	LoginByPhoneCode(ctx context.Context, req *LoginByPhoneCodeInput) (*AuthTokens, error)
	RefreshToken(ctx context.Context, req *RefreshTokenInput) (*AuthTokens, error)
	ChooseUin(ctx context.Context, req *ChooseUinInput) (*AuthTokens, error)
	SwitchOrganization(ctx context.Context, req *SwitchOrganizationInput) (*AuthTokens, error)
	CreateOrganization(ctx context.Context, req *CreateOrganizationInput) (*AuthTokens, error)
	AuthSession(ctx context.Context) (*AuthSessionOutput, error)
}

type UserRepository interface {
	CreateUser(ctx context.Context, req *CreateUserInput) (*CreateUserResponse, error)
	GetUser(ctx context.Context, publicID string, phone string) (*UserInfo, error)
	UpdateUser(ctx context.Context, publicID string, req *UpdateUserInput) (*UserInfo, error)
	UpdateCurrentUser(ctx context.Context, req *UpdateCurrentUserInput) (*UserInfo, error)
	DeleteUser(ctx context.Context, publicID string) error
	ListUser(ctx context.Context, req *ListUserInput) (*UserList, error)

	GetUserByID(ctx context.Context, id uint) (*UserInfo, error)
	GetUserByUin(ctx context.Context, uin uint) (*UserInfo, error)
	GetUserByGithubID(ctx context.Context, githubID int64) (*UserInfo, error)
	GetUsersByIDs(ctx context.Context, ids []uint) ([]*UserInfo, error)
	GetUsersByUins(ctx context.Context, uins []uint) (map[uint]*UserInfo, error)
	GetUsersByPublicIDs(ctx context.Context, publicIDs []string) ([]*UserInfo, error)
	GetUinMapByPublicIDs(ctx context.Context, orgID uint, publicIDs []string) (map[string]uint, error)

	ListUin(ctx context.Context) (*ListUinOutput, error)
}

type OrgRepository interface {
	CreateOrg(ctx context.Context, req *CreateOrgInput) (*Org, error)
	GetOrg(ctx context.Context, publicID string, code string) (*Org, error)
	UpdateOrg(ctx context.Context, publicID string, req *UpdateOrgInput) (*Org, error)
	DeleteOrg(ctx context.Context, publicID string) error
	ListOrgs(ctx context.Context, req *ListOrgsInput) (*OrgList, error)

	CreateOrgMember(ctx context.Context, req *CreateOrgMemberInput) (*OrgMember, error)
	GetOrgMember(ctx context.Context, id uint, uin uint) (*OrgMember, error)
	UpdateOrgMember(ctx context.Context, id uint, req *UpdateOrgMemberInput) (*OrgMember, error)
	ListOrgMembers(ctx context.Context, req *ListOrgMembersInput) (*OrgMemberList, error)
}

type DepartmentRepository interface {
	CreateDepartment(ctx context.Context, req *CreateDepartmentInput) (*Department, error)
	GetDepartment(ctx context.Context, id uint) (*Department, error)
	UpdateDepartment(ctx context.Context, id uint, req *UpdateDepartmentInput) (*Department, error)
	DeleteDepartment(ctx context.Context, id uint) error
	ListDepartment(ctx context.Context, req *ListDepartmentInput) (*DepartmentList, error)
}

type TokenParser interface {
	ParseUser(ctx context.Context, tokenStr string) (*types.Caller, error)
	ParseWorker(ctx context.Context, tokenStr string) (*types.Caller, error)
	IssueWorker(ctx context.Context, orgID, workerID uint, bootstrapToken string) (token string, expiredAt int64, err error)
}

// CreateAPIKeyInput describes a user-owned API key requested from the identity provider.
type CreateAPIKeyInput struct {
	Name         string
	Purpose      string
	ResourceType string
	ResourceID   uint
	ExpireHours  int
}

// CreatedAPIKey contains the opaque API key returned once by the identity provider.
type CreatedAPIKey struct {
	ID     uint
	APIKey string
}

// APIKeyIssuer creates API keys for the user carried by the request context.
type APIKeyIssuer interface {
	CreateAPIKey(ctx context.Context, input CreateAPIKeyInput) (*CreatedAPIKey, error)
}
