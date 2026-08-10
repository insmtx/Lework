//go:build !enterprise

package adapter

import (
	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/adapter/account/oss"
)

type ossEdition struct {
	auth        account.AuthProvider
	user        account.UserRepository
	org         account.OrgRepository
	department  account.DepartmentRepository
	tokenParser account.TokenParser
}

func NewEdition(cfg Config) Edition {
	deps := cfg.ToDeps()
	return &ossEdition{
		auth:        oss.NewAuth(deps.DB, deps.JWTSecret, deps.SmsSender, deps.WorkerProvisioning),
		user:        oss.NewUser(deps.DB),
		org:         oss.NewOrg(deps.DB, deps.WorkerProvisioning),
		department:  oss.NewDepartment(deps.DB),
		tokenParser: oss.NewTokenParser(deps.DB, deps.JWTSecret, deps.WorkerAuth),
	}
}

func (e *ossEdition) Auth() account.AuthProvider               { return e.auth }
func (e *ossEdition) User() account.UserRepository             { return e.user }
func (e *ossEdition) Org() account.OrgRepository               { return e.org }
func (e *ossEdition) Department() account.DepartmentRepository { return e.department }
func (e *ossEdition) TokenParser() account.TokenParser         { return e.tokenParser }
func (e *ossEdition) APIKeyIssuer() account.APIKeyIssuer       { return nil }
func (e *ossEdition) Edition() string                          { return account.EditionOSS }
func (e *ossEdition) DeployMode() string                       { return "saas" }
func (e *ossEdition) MaxOrgsPerUser() int                      { return 1 }
