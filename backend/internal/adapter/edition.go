package adapter

import (
	"github.com/insmtx/Leros/backend/internal/adapter/account"
)

// Edition aggregates all adapter implementations for the current build
// variant. The concrete edition (oss or enterprise) is selected at build
// time via //go:build tags.
type Edition interface {
	Auth() account.AuthProvider
	User() account.UserRepository
	Org() account.OrgRepository
	Department() account.DepartmentRepository
	TokenParser() account.TokenParser
	APIKeyIssuer() account.APIKeyIssuer
	Edition() string
	DeployMode() string
	MaxOrgsPerUser() int
}
