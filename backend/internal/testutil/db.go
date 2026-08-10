//go:build integration

package testutil

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter/account"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/seed"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// testEdition 集成测试用的最小 OSS Edition，驱动 seed.SeedCoreData 的账号种子。
type testEdition struct{}

func (testEdition) Auth() account.AuthProvider               { return nil }
func (testEdition) User() account.UserRepository             { return nil }
func (testEdition) Org() account.OrgRepository               { return nil }
func (testEdition) Department() account.DepartmentRepository { return nil }
func (testEdition) TokenParser() account.TokenParser         { return nil }
func (testEdition) APIKeyIssuer() account.APIKeyIssuer       { return nil }
func (testEdition) Edition() string                          { return "oss" }
func (testEdition) DeployMode() string                       { return "saas" }
func (testEdition) MaxOrgsPerUser() int                      { return 1 }

// Setup 初始化集成测试数据库：连接 + schema 迁移 + 核心种子（OSS 账号 + 系统 LLM）。
// 种子作用于事务之外（与生产 InitDB 一致），调用方在返回的事务内操作。
func Setup(t *testing.T) *gorm.DB {
	t.Helper()

	cfg := LoadTestConfig(t)
	db, err := infradb.InitDB(*cfg.Database)
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	if err := seed.SeedCoreData(context.Background(), db, testEdition{}, cfg.LLM); err != nil {
		t.Fatalf("seed test db: %v", err)
	}

	tx := db.Begin()
	t.Cleanup(func() { tx.Rollback() })

	return tx
}

func SetupTestDB(t *testing.T, cfg *config.DatabaseConfig) *gorm.DB {
	t.Helper()

	database, err := gorm.Open(postgres.Open(cfg.URL), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}

	tx := database.Begin()
	t.Cleanup(func() { tx.Rollback() })

	return tx
}

func SetupTestDBWithMigrations(t *testing.T, cfg *config.DatabaseConfig, models ...interface{}) *gorm.DB {
	t.Helper()

	db := SetupTestDB(t, cfg)
	if len(models) > 0 {
		if err := db.AutoMigrate(models...); err != nil {
			t.Fatalf("migrate test db: %v", err)
		}
	}

	return db
}
