package db

import (
	"context"
	"regexp"
	"testing"
	"time"

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
	mock.ExpectQuery(`INSERT INTO "leros_skill_marketplace_item" .*ON CONFLICT \("source","skill_id","version"\) DO UPDATE SET "name"="excluded"\."name","translated_name"="excluded"\."translated_name","description"="excluded"\."description","translated_description"="excluded"\."translated_description","author"="excluded"\."author","category"="excluded"\."category","tags"="excluded"\."tags","package_storage_path"="excluded"\."package_storage_path","updated_at"="excluded"\."updated_at" RETURNING "id"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	if err := BatchUpsertSkillMarketplaceItems(ctx, database, []types.SkillMarketplaceItem{item}); err != nil {
		t.Fatalf("upsert item: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListCachedSkillMarketplaceItemsFiltersAndOrders(t *testing.T) {
	ctx := context.Background()
	database, mock, cleanup := setupMockSkillMarketplaceItemDB(t)
	defer cleanup()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "created_at", "updated_at", "deleted_at",
		"skill_id", "name", "translated_name", "source", "description", "translated_description",
		"author", "installs", "version", "category", "tags", "package_storage_path",
	}).AddRow(
		1, now, now, nil,
		"demo-skill", "Demo Skill", "演示技能", "ClawHub", "Original description", "中文描述",
		"ClawHub", int64(7), "1.0.0", "productivity", []byte(`["demo"]`), "",
	)

	mock.ExpectQuery(`SELECT \* FROM "leros_skill_marketplace_item" WHERE source = \$1 AND \(skill_id LIKE \$2 OR name LIKE \$3 OR description LIKE \$4 OR translated_name LIKE \$5 OR translated_description LIKE \$6\) AND category = \$7 AND "leros_skill_marketplace_item"\."deleted_at" IS NULL ORDER BY updated_at DESC LIMIT \$8`).
		WithArgs("ClawHub", "%demo%", "%demo%", "%demo%", "%demo%", "%demo%", "productivity", 2).
		WillReturnRows(rows)

	items, err := ListCachedSkillMarketplaceItems(ctx, database, "ClawHub", " demo ", " productivity ", 2)
	if err != nil {
		t.Fatalf("list cached marketplace items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].SkillID != "demo-skill" || items[0].TranslatedName != "演示技能" || items[0].TranslatedDescription != "中文描述" {
		t.Fatalf("unexpected item: %#v", items[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
