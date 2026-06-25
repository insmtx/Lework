package db

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func setupMockSkillMarketplaceItemDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, func()) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	database, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open gorm db: %v", err)
	}
	cleanup := func() {
		sqlDB.Close()
	}
	return database, mock, cleanup
}

func testSkillMarketplaceItem(installs int64) types.SkillMarketplaceItem {
	return types.SkillMarketplaceItem{
		SkillID:               "demo-skill",
		Name:                  "Demo Skill",
		Source:                "Leros",
		Description:           "Original description",
		TranslatedDescription: "原始描述",
		Author:                "Lework",
		Installs:              installs,
		Version:               "1.0.0",
		Category:              "productivity",
		Tags:                  types.SkillStringList{"demo"},
		PackageStoragePath:    "s3://bucket/original.zip",
	}
}

func TestIncrementSkillMarketplaceInstalls(t *testing.T) {
	ctx := context.Background()
	database, mock, cleanup := setupMockSkillMarketplaceItemDB(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "leros_skill_marketplace_item" SET "installs"=installs + $1 WHERE (source = $2 AND skill_id = $3 AND version = $4) AND "leros_skill_marketplace_item"."deleted_at" IS NULL`)).
		WithArgs(1, "Leros", "demo-skill", "1.0.0").
		WillReturnResult(sqlmock.NewResult(0, 1))

	updated, err := IncrementSkillMarketplaceInstalls(ctx, database, "Leros", "demo-skill", "1.0.0")
	if err != nil {
		t.Fatalf("increment installs: %v", err)
	}
	if !updated {
		t.Fatal("expected existing row to be updated")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestIncrementSkillMarketplaceInstallsMissingRow(t *testing.T) {
	ctx := context.Background()
	database, mock, cleanup := setupMockSkillMarketplaceItemDB(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "leros_skill_marketplace_item" SET "installs"=installs + $1 WHERE (source = $2 AND skill_id = $3 AND version = $4) AND "leros_skill_marketplace_item"."deleted_at" IS NULL`)).
		WithArgs(1, "Leros", "missing", "1.0.0").
		WillReturnResult(sqlmock.NewResult(0, 0))

	updated, err := IncrementSkillMarketplaceInstalls(ctx, database, "Leros", "missing", "1.0.0")
	if err != nil {
		t.Fatalf("increment missing installs: %v", err)
	}
	if updated {
		t.Fatal("expected missing row to report updated=false")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestBatchUpsertSkillMarketplaceItemsPreservesInstalls(t *testing.T) {
	ctx := context.Background()
	database, mock, cleanup := setupMockSkillMarketplaceItemDB(t)
	defer cleanup()

	item := testSkillMarketplaceItem(0)
	mock.ExpectQuery(`INSERT INTO "leros_skill_marketplace_item" .*ON CONFLICT \("source","skill_id","version"\) DO UPDATE SET "name"="excluded"\."name","description"="excluded"\."description","translated_description"="excluded"\."translated_description","author"="excluded"\."author","category"="excluded"\."category","tags"="excluded"\."tags","package_storage_path"="excluded"\."package_storage_path","updated_at"="excluded"\."updated_at" RETURNING "id"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	if err := BatchUpsertSkillMarketplaceItems(ctx, database, []types.SkillMarketplaceItem{item}); err != nil {
		t.Fatalf("upsert item: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
