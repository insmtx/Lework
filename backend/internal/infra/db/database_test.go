package db

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func TestRunMigrationsCreatesOrganizationTables(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	if err := runMigrations(database); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	for _, tableName := range []string{
		types.TableNameDepartment,
		types.TableNameMemberDepartment,
	} {
		if !database.Migrator().HasTable(tableName) {
			t.Fatalf("expected table %s to be migrated", tableName)
		}
	}
}
