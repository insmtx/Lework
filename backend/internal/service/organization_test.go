package service

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/types"
)

func setupAccountServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := database.AutoMigrate(
		&types.Department{},
		&types.MemberDepartment{},
	); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	return database
}

func accountServiceTestContext() context.Context {
	return auth.WithContext(context.Background(), &types.Caller{
		Uin:   1,
		OrgID: 1,
		Kind:  types.CallerKindUser,
		State: types.AuthStateSucc,
	}, nil)
}
