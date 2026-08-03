package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func setupProjectTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	if err := database.AutoMigrate(&types.Project{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	return database
}

func TestListProjects_DefaultOrderUsesUpdatedAtDesc(t *testing.T) {
	database := setupProjectTestDB(t)
	ctx := context.Background()

	projectOld := &types.Project{
		PublicID: "prj_list_old",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Old Project",
		Status:   string(types.ProjectStatusActive),
	}
	projectNew := &types.Project{
		PublicID: "prj_list_new",
		OrgID:    1,
		OwnerID:  1,
		Name:     "New Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := CreateProject(ctx, database, projectOld); err != nil {
		t.Fatalf("CreateProject old failed: %v", err)
	}
	if err := CreateProject(ctx, database, projectNew); err != nil {
		t.Fatalf("CreateProject new failed: %v", err)
	}

	if err := TouchProjectUpdatedAt(ctx, database, projectOld.ID, time.Now().UTC()); err != nil {
		t.Fatalf("TouchProjectUpdatedAt old failed: %v", err)
	}
	if err := TouchProjectUpdatedAt(ctx, database, projectNew.ID, time.Now().Add(-time.Hour).UTC()); err != nil {
		t.Fatalf("TouchProjectUpdatedAt new failed: %v", err)
	}

	items, total, err := ListProjects(ctx, database, &types.PageQuery{
		Caller:     types.Caller{OrgID: 1, Uin: 1},
		ProjectIDs: []uint{projectOld.ID, projectNew.ID},
		Pagination: types.Pagination{
			Offset: 0,
			Limit:  20,
		},
	})
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].PublicID != projectOld.PublicID {
		t.Fatalf("expected first project %q, got %q", projectOld.PublicID, items[0].PublicID)
	}
}
