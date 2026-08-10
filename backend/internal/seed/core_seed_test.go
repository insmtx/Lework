package seed

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newCoreSeedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&types.Organization{},
		&types.User{},
		&types.UserOrg{},
		&types.DigitalAssistant{},
		&types.WorkerDeployment{},
		&types.LLMModel{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestSeedCoreDataGatesAccountByEdition 验证运行时 edition 门控：OSS 种默认组织，企业版不种。
func TestSeedCoreDataGatesAccountByEdition(t *testing.T) {
	t.Run("oss seeds default org", func(t *testing.T) {
		db := newCoreSeedTestDB(t)
		if err := SeedCoreData(context.Background(), db, &stubEdition{editionName: "oss"}, &config.LLMConfig{}); err != nil {
			t.Fatalf("SeedCoreData: %v", err)
		}
		var count int64
		db.Model(&types.Organization{}).Count(&count)
		if count != 1 {
			t.Fatalf("expected 1 org for oss, got %d", count)
		}
	})

	t.Run("enterprise does not seed default org", func(t *testing.T) {
		db := newCoreSeedTestDB(t)
		if err := SeedCoreData(context.Background(), db, &stubEdition{editionName: "enterprise"}, &config.LLMConfig{}); err != nil {
			t.Fatalf("SeedCoreData: %v", err)
		}
		var count int64
		db.Model(&types.Organization{}).Count(&count)
		if count != 0 {
			t.Fatalf("expected 0 org for enterprise, got %d", count)
		}
	})
}
