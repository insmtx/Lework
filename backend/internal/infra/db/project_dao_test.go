package db

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
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
		Caller: types.Caller{OrgID: 1, Uin: 1},
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

func setupProjectDAOPostgresMock(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, func()) {
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

func TestListProjectsReferencingSkill_ReturnsMatchingProjects(t *testing.T) {
	database, mock, cleanup := setupProjectDAOPostgresMock(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	columns := []string{
		"id", "created_at", "updated_at", "deleted_at", "public_id",
		"org_id", "owner_id", "name", "description", "objective", "status",
		"gitea_repo_full_name", "gitea_repo_id", "gitea_default_branch", "metadata",
	}
	metadata := []byte(`{"extra":{"skills":[{"code":"demo-skill","name":"Demo Skill"}]}}`)

	mock.ExpectQuery(`SELECT .* FROM "leros_project" WHERE \(org_id = \$1 AND deleted_at IS NULL\) AND \(EXISTS`).
		WithArgs(uint(100), "demo-skill", "demo-skill").
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			1, now, now, nil, "prj_demo",
			100, 1, "Demo Project", "", "", "active",
			"", 0, "main", metadata,
		))

	projects, err := ListProjectsReferencingSkill(ctx, database, 100, "demo-skill")
	if err != nil {
		t.Fatalf("ListProjectsReferencingSkill failed: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].PublicID != "prj_demo" {
		t.Fatalf("public_id = %q, want prj_demo", projects[0].PublicID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListProjectsReferencingSkill_EmptySkillName(t *testing.T) {
	database, mock, cleanup := setupProjectDAOPostgresMock(t)
	defer cleanup()

	projects, err := ListProjectsReferencingSkill(context.Background(), database, 100, "  ")
	if err != nil {
		t.Fatalf("ListProjectsReferencingSkill failed: %v", err)
	}
	if projects != nil {
		t.Fatalf("expected nil projects, got %#v", projects)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListProjectsReferencingSkill_QueryUsesJSONBMatch(t *testing.T) {
	database, mock, cleanup := setupProjectDAOPostgresMock(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT .* FROM "leros_project" WHERE \(org_id = \$1 AND deleted_at IS NULL\) AND \(EXISTS`).
		WithArgs(uint(100), "my-skill", "my-skill").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, err := ListProjectsReferencingSkill(context.Background(), database, 100, "my-skill")
	if err != nil {
		t.Fatalf("ListProjectsReferencingSkill failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
