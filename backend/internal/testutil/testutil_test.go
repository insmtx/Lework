//go:build integration

package testutil

import (
	"testing"

	"github.com/insmtx/Lework/backend/types"
)

func TestSetup(t *testing.T) {
	db := Setup(t)

	if !db.Migrator().HasTable(&types.Organization{}) {
		t.Fatal("expected organization table to exist after full init")
	}
	if !db.Migrator().HasTable(&types.User{}) {
		t.Fatal("expected user table to exist after full init")
	}
}

func TestSetupTestDB(t *testing.T) {
	cfg := LoadTestConfig(t)
	db := SetupTestDBWithMigrations(t, cfg.Database, &types.Organization{})

	if !db.Migrator().HasTable(&types.Organization{}) {
		t.Fatal("expected organization table to exist after migration")
	}
}

func TestSetupTestDB_RollbackIsolation(t *testing.T) {
	cfg := LoadTestConfig(t)
	db := SetupTestDBWithMigrations(t, cfg.Database, &types.Organization{})
	org := &types.Organization{Code: "test_rollback", Name: "Test Rollback"}

	if err := db.Create(org).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}
}

func TestSetupNATS(t *testing.T) {
	cfg := LoadTestConfig(t)
	nc := SetupNATS(t, cfg.NATS)

	if !nc.IsConnected() {
		t.Fatal("expected nats connection to be connected")
	}
}
