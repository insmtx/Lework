//go:build enterprise

package adapter

import (
	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/adapter/account/enterprise"
)

type enterpriseEdition struct {
	auth        account.AuthProvider
	user        account.UserRepository
	org         account.OrgRepository
	department  account.DepartmentRepository
	tokenParser account.TokenParser
}

func NewEdition(cfg Config) Edition {
	deps := cfg.ToDeps()
	client := enterprise.NewIAMClient(deps.IAM, deps.Env)
	return &enterpriseEdition{
		auth:        enterprise.NewAuth(deps.DB, deps.IAM, deps.Env, deps.WorkerProvisioning),
		user:        enterprise.NewUser(client, deps.DB),
		org:         enterprise.NewOrg(deps.DB, client, deps.WorkerProvisioning),
		department:  enterprise.NewDepartment(client),
		tokenParser: enterprise.NewTokenParser(deps.DB, deps.IAM, deps.Env, deps.JWTSecret, deps.WorkerAuth),
	}
}

func (e *enterpriseEdition) Auth() account.AuthProvider               { return e.auth }
func (e *enterpriseEdition) User() account.UserRepository             { return e.user }
func (e *enterpriseEdition) Org() account.OrgRepository               { return e.org }
func (e *enterpriseEdition) Department() account.DepartmentRepository { return e.department }
func (e *enterpriseEdition) TokenParser() account.TokenParser         { return e.tokenParser }
func (e *enterpriseEdition) Edition() string                          { return "enterprise" }
