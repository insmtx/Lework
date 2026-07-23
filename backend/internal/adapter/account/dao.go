package account

import (
	"context"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// PageRequest carries pagination parameters.
type PageRequest struct {
	Page     int32
	PageSize int32
}

// PageResult holds a paginated result set.
type PageResult[T any] struct {
	Items    []T
	Total    int64
	Page     int32
	PageSize int32
}

// BatchResult describes the outcome of a batch operation.
type BatchResult struct {
	SuccessCount int
	FailedItems  []BatchFailedItem
}

// BatchFailedItem describes one failed item in a batch operation.
type BatchFailedItem struct {
	Index int
	Err   error
}

// UserFilter carries optional filtering conditions for user queries.
type UserFilter struct {
	Keyword string
}

// OrgFilter carries optional filtering conditions for organization queries.
type OrgFilter struct {
	Keyword string
}

// DepartmentFilter carries optional filtering conditions for department queries.
type DepartmentFilter struct {
	Keyword  string
	ParentID *uint
}

// ─── UserDAO ───────────────────────────────────────────────────────────────────

// UserDAO abstracts the user data access layer. Both oss (database-backed) and
// enterprise (HTTP-backed) implementations realize this interface.
type UserDAO interface {
	Get(ctx context.Context, ref UserRef) (*types.User, error)
	GetByIDs(ctx context.Context, ids []uint) ([]*types.User, error)
	Create(ctx context.Context, u *types.User) (*types.User, error)
	Update(ctx context.Context, u *types.User) (*types.User, error)
	Delete(ctx context.Context, id uint) error
	List(ctx context.Context, filter UserFilter, page PageRequest) (*PageResult[*types.User], error)
	BatchCreate(ctx context.Context, users []*types.User) (*BatchResult, error)
}

// ─── OrgDAO ────────────────────────────────────────────────────────────────────

// OrgDAO abstracts the organization data access layer.
type OrgDAO interface {
	Get(ctx context.Context, ref OrgRef) (*types.Organization, error)
	GetByIDs(ctx context.Context, ids []uint) ([]*types.Organization, error)
	Create(ctx context.Context, o *types.Organization) (*types.Organization, error)
	Update(ctx context.Context, o *types.Organization) (*types.Organization, error)
	Delete(ctx context.Context, id uint) error
	List(ctx context.Context, filter OrgFilter, page PageRequest) (*PageResult[*types.Organization], error)
	IsUniqueConstraintError(err error) bool
}

// ─── DepartmentDAO ─────────────────────────────────────────────────────────────

// DepartmentDAO abstracts the department data access layer.
type DepartmentDAO interface {
	Get(ctx context.Context, ref DepartmentRef) (*types.Department, error)
	GetByIDs(ctx context.Context, ids []uint) ([]*types.Department, error)
	Create(ctx context.Context, d *types.Department) (*types.Department, error)
	Update(ctx context.Context, d *types.Department) (*types.Department, error)
	Delete(ctx context.Context, id uint) error
	List(ctx context.Context, filter DepartmentFilter, page PageRequest) (*PageResult[*types.Department], error)
	ListChildren(ctx context.Context, parentID uint) ([]*types.Department, error)
	ListSiblings(ctx context.Context, id uint) ([]*types.Department, error)
	ListDescendantIDs(ctx context.Context, id uint, orgID uint) ([]uint, error)
	BatchCreate(ctx context.Context, departments []*types.Department) (*BatchResult, error)
	GetDefaultRootByOrgID(ctx context.Context, orgID uint) (*types.Department, error)
	UpdateSort(ctx context.Context, id uint, sort uint) error
}

// ─── UserOrgDAO ────────────────────────────────────────────────────────────────

// UserOrgDAO abstracts the user-org mapping data access layer.
type UserOrgDAO interface {
	Get(ctx context.Context, ref UserOrgRef) (*types.UserOrg, error)
	GetByUserID(ctx context.Context, userID uint) (*types.UserOrg, error)
	ListByUserID(ctx context.Context, userID uint) ([]*types.UserOrg, error)
	Create(ctx context.Context, uo *types.UserOrg) (*types.UserOrg, error)
	Update(ctx context.Context, uo *types.UserOrg) (*types.UserOrg, error)
	Delete(ctx context.Context, id uint) error
	DeleteMemberDepartments(ctx context.Context, uin uint, orgID uint) error
	CountByUserID(ctx context.Context, userID uint) (int64, error)
	ListByUinAndOrgID(ctx context.Context, uin uint, orgID uint) ([]*types.MemberDepartment, error)
	ListByUin(ctx context.Context, uin uint) ([]*types.MemberDepartment, error)
	CreateMemberDepartments(ctx context.Context, deps []*types.MemberDepartment) error
}

// ─── AuthDAO ───────────────────────────────────────────────────────────────────

// AuthDAO abstracts the local auth data access layer (phone verification codes,
// refresh tokens, login attempt tracking).
type AuthDAO interface {
	CreatePhoneCode(ctx context.Context, code *types.AuthPhoneVerificationCode) error
	GetActivePhoneCode(ctx context.Context, phone string) (*types.AuthPhoneVerificationCode, error)
	DeleteExpiredPhoneCodes(ctx context.Context) error
	CreateRefreshToken(ctx context.Context, token *types.AuthRefreshToken) error
	GetActiveRefreshToken(ctx context.Context, uin uint) (*types.AuthRefreshToken, error)
	DeleteExpiredRefreshTokens(ctx context.Context) error
	GetLoginAttempt(ctx context.Context, key string) (*types.AuthLoginAttempt, error)
	DeleteLoginAttempt(ctx context.Context, key string) error
	DeleteExpiredLoginAttempts(ctx context.Context) error
	CreateLoginAttempt(ctx context.Context, attempts *types.AuthLoginAttempt) error
	IncrementLoginAttempt(ctx context.Context, key string) error
	IsUniqueConstraintError(err error) bool
}

// DBGetter abstracts gorm.DB read access.
type DBGetter interface {
	DB() *gorm.DB
}
