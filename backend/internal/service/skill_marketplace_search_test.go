package service

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

func setupSkillMarketplaceSearchDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, func()) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	mock.MatchExpectationsInOrder(false)

	database, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open gorm db: %v", err)
	}

	return database, mock, func() {
		sqlDB.Close()
	}
}

func expectBuiltinSkillSearch(mock sqlmock.Sqlmock, limit int, items ...types.BuiltinSkillMarketplaceItem) {
	rows := sqlmock.NewRows([]string{
		"id", "created_at", "updated_at", "deleted_at",
		"skill_id", "name", "description", "version", "author", "category", "tags",
		"icon", "local_path", "package_sha256", "verified", "published_at", "installs",
	})
	now := time.Now()
	for i, item := range items {
		rows.AddRow(
			i+1, now, now, nil,
			item.SkillID, item.Name, item.Description, item.Version, item.Author, item.Category, []byte(`["builtin"]`),
			item.Icon, item.LocalPath, item.PackageSHA256, item.Verified, now, item.Installs,
		)
	}

	mock.ExpectQuery(`SELECT \* FROM "leros_builtin_skill_marketplace_item" WHERE "leros_builtin_skill_marketplace_item"\."deleted_at" IS NULL ORDER BY published_at DESC LIMIT \$1`).
		WithArgs(limit).
		WillReturnRows(rows)
}

func expectCachedClawHubFallback(mock sqlmock.Sqlmock, limit int, items ...types.SkillMarketplaceItem) {
	rows := sqlmock.NewRows([]string{
		"id", "created_at", "updated_at", "deleted_at",
		"skill_id", "name", "translated_name", "source", "description", "translated_description",
		"author", "installs", "version", "category", "tags", "package_storage_path",
	})
	now := time.Now()
	for i, item := range items {
		rows.AddRow(
			i+1, now, now.Add(time.Duration(i)*time.Second), nil,
			item.SkillID, item.Name, item.TranslatedName, item.Source, item.Description, item.TranslatedDescription,
			item.Author, item.Installs, item.Version, item.Category, []byte(`["cached"]`), item.PackageStoragePath,
		)
	}

	mock.ExpectQuery(`SELECT \* FROM "leros_skill_marketplace_item" WHERE source = \$1 AND "leros_skill_marketplace_item"\."deleted_at" IS NULL ORDER BY updated_at DESC LIMIT \$2`).
		WithArgs("ClawHub", limit).
		WillReturnRows(rows)
}

func expectMarketplaceCacheLookup(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "leros_skill_marketplace_item" WHERE (source = $1 AND (`)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "deleted_at",
			"skill_id", "name", "translated_name", "source", "description", "translated_description",
			"author", "installs", "version", "category", "tags", "package_storage_path",
		}))
}

func builtinSkillForSearch(skillID string) types.BuiltinSkillMarketplaceItem {
	return types.BuiltinSkillMarketplaceItem{
		SkillID:     skillID,
		Name:        skillID,
		Description: "builtin description",
		Version:     "1.0.0",
		Author:      "Leros",
		Category:    "",
		LocalPath:   skillID,
		Verified:    true,
	}
}

func cachedClawHubSkillForSearch(skillID string) types.SkillMarketplaceItem {
	return types.SkillMarketplaceItem{
		SkillID:               skillID,
		Name:                  skillID,
		TranslatedName:        "中文" + skillID,
		Source:                "ClawHub",
		Description:           "cached description",
		TranslatedDescription: "缓存中文描述",
		Author:                "ClawHub",
		Version:               "1.0.0",
		Category:              "",
	}
}

func TestSearchCachedClawHubFallbackFillsRemainingItems(t *testing.T) {
	database, mock, cleanup := setupSkillMarketplaceSearchDB(t)
	defer cleanup()

	expectCachedClawHubFallback(mock, 4,
		cachedClawHubSkillForSearch("cached-one"),
		cachedClawHubSkillForSearch("cached-two"),
		cachedClawHubSkillForSearch("cached-three"),
	)

	svc := &skillMarketplaceService{db: database}
	items, err := svc.searchCachedClawHubFallback(context.Background(), "", "", 4, []contract.SkillMarketplaceItemView{
		{SourceType: "Leros", SkillID: "builtin-one", Version: "1.0.0"},
		{SourceType: "Leros", SkillID: "builtin-two", Version: "1.0.0"},
	})
	if err != nil {
		t.Fatalf("cached fallback: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected fallback to fill 2 remaining items, got %d: %#v", len(items), items)
	}
	if items[0].SourceType != "ClawHub" || items[0].DisplayName == "" || items[0].Description != "缓存中文描述" {
		t.Fatalf("expected translated cached fallback item, got %#v", items[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSearchCachedClawHubFallbackSkipsExistingItems(t *testing.T) {
	database, mock, cleanup := setupSkillMarketplaceSearchDB(t)
	defer cleanup()

	expectCachedClawHubFallback(mock, 3,
		cachedClawHubSkillForSearch("cached-one"),
		cachedClawHubSkillForSearch("cached-two"),
		cachedClawHubSkillForSearch("cached-three"),
	)

	svc := &skillMarketplaceService{db: database}
	items, err := svc.searchCachedClawHubFallback(context.Background(), "", "", 3, []contract.SkillMarketplaceItemView{
		{SourceType: "ClawHub", SkillID: "cached-one", Version: "1.0.0"},
	})
	if err != nil {
		t.Fatalf("cached fallback: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected cached fallback to fill 2 items, got %d: %#v", len(items), items)
	}
	if items[0].SkillID != "cached-two" || items[1].SkillID != "cached-three" {
		t.Fatalf("expected duplicate cached-one to be skipped, got %#v", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSearchSkillMarketplaceLerosOnlySkipsClawHubFallback(t *testing.T) {
	database, mock, cleanup := setupSkillMarketplaceSearchDB(t)
	defer cleanup()

	expectBuiltinSkillSearch(mock, 4, builtinSkillForSearch("builtin-one"), builtinSkillForSearch("builtin-two"))
	expectMarketplaceCacheLookup(mock)

	svc := &skillMarketplaceService{db: database}
	resp, err := svc.SearchSkillMarketplace(context.Background(), &contract.SearchSkillMarketplaceRequest{
		SourceTypes: []string{"Leros"},
		Limit:       4,
	})
	if err != nil {
		t.Fatalf("search marketplace: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected only builtin items, got %d: %#v", len(resp.Items), resp.Items)
	}
	if len(resp.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", resp.Warnings)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
