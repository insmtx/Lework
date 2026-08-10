package seed

import (
	"testing"

	"github.com/insmtx/Leros/backend/internal/adapter"
	"github.com/insmtx/Leros/backend/internal/adapter/account"
)

type stubEdition struct{ editionName string }

func (s *stubEdition) Auth() account.AuthProvider               { return nil }
func (s *stubEdition) User() account.UserRepository             { return nil }
func (s *stubEdition) Org() account.OrgRepository               { return nil }
func (s *stubEdition) Department() account.DepartmentRepository { return nil }
func (s *stubEdition) TokenParser() account.TokenParser         { return nil }
func (s *stubEdition) APIKeyIssuer() account.APIKeyIssuer       { return nil }
func (s *stubEdition) Edition() string                          { return s.editionName }
func (s *stubEdition) DeployMode() string                       { return "saas" }
func (s *stubEdition) MaxOrgsPerUser() int                      { return 1 }

func TestIsOSSDistinguishesEditions(t *testing.T) {
	if !isOSS(&stubEdition{editionName: "oss"}) {
		t.Fatal("expected oss edition to build HTTP OSS start")
	}
	if isOSS(&stubEdition{editionName: "enterprise"}) {
		t.Fatal("expected enterprise edition to NOT be oss")
	}
}

var _ = adapter.Edition((*stubEdition)(nil))
