package service

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

func TestRemoveSkillFromProjectMetadata_MatchesCode(t *testing.T) {
	meta := types.ObjectMetadata{
		Tags: []string{"tag-a"},
		Type: "demo",
		Extra: map[string]interface{}{
			"skills": []interface{}{
				map[string]interface{}{"code": "demo-skill", "name": "Demo Skill"},
				map[string]interface{}{"code": "other-skill", "name": "Other"},
			},
			"note": "keep-me",
		},
	}

	newMeta, changed := removeSkillFromProjectMetadata(meta, "demo-skill")
	if !changed {
		t.Fatal("expected metadata change")
	}
	if len(newMeta.Tags) != 1 || newMeta.Tags[0] != "tag-a" {
		t.Fatalf("tags = %#v, want preserved", newMeta.Tags)
	}
	if newMeta.Type != "demo" {
		t.Fatalf("type = %q, want demo", newMeta.Type)
	}
	if newMeta.Extra["note"] != "keep-me" {
		t.Fatalf("extra.note = %#v, want keep-me", newMeta.Extra["note"])
	}

	skills, ok := newMeta.Extra["skills"].([]interface{})
	if !ok {
		t.Fatalf("skills type = %T, want []interface{}", newMeta.Extra["skills"])
	}
	if len(skills) != 1 {
		t.Fatalf("skills len = %d, want 1", len(skills))
	}
	entry, ok := skills[0].(map[string]interface{})
	if !ok || entry["code"] != "other-skill" {
		t.Fatalf("remaining skill = %#v, want other-skill", skills[0])
	}
}

func TestRemoveSkillFromProjectMetadata_MatchesNameCaseInsensitive(t *testing.T) {
	meta := types.ObjectMetadata{
		Extra: map[string]interface{}{
			"skills": []interface{}{
				map[string]interface{}{"code": "alpha", "name": "Demo Skill"},
			},
		},
	}

	_, changed := removeSkillFromProjectMetadata(meta, "demo skill")
	if !changed {
		t.Fatal("expected metadata change when matching display name")
	}
}

func TestRemoveSkillFromProjectMetadata_NoSkills(t *testing.T) {
	meta := types.ObjectMetadata{
		Extra: map[string]interface{}{"note": "only"},
	}

	_, changed := removeSkillFromProjectMetadata(meta, "demo-skill")
	if changed {
		t.Fatal("expected no change without skills array")
	}
}

func TestRemoveSkillFromProjectMetadata_NoMatch(t *testing.T) {
	meta := types.ObjectMetadata{
		Extra: map[string]interface{}{
			"skills": []interface{}{
				map[string]interface{}{"code": "other-skill", "name": "Other"},
			},
		},
	}

	_, changed := removeSkillFromProjectMetadata(meta, "demo-skill")
	if changed {
		t.Fatal("expected no change when skill not referenced")
	}
}

func setupProjectSkillReferenceDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, func()) {
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

func TestCleanupOrgProjectSkillReferences_UpdatesMatchingProjects(t *testing.T) {
	database, mock, cleanup := setupProjectSkillReferenceDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	columns := []string{
		"id", "created_at", "updated_at", "deleted_at", "public_id",
		"org_id", "owner_id", "name", "description", "objective", "status",
		"gitea_repo_full_name", "gitea_repo_id", "gitea_default_branch", "metadata",
	}
	metadata := []byte(`{"extra":{"skills":[{"code":"demo-skill","name":"Demo Skill"},{"code":"keep","name":"Keep"}]}}`)

	mock.ExpectQuery(`SELECT .* FROM "leros_project" WHERE \(org_id = \$1 AND deleted_at IS NULL\) AND \(EXISTS`).
		WithArgs(uint(100), "demo-skill", "demo-skill").
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			1, now, now, nil, "prj_demo",
			100, 1, "Demo Project", "", "", "active",
			"", 0, "main", metadata,
		))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "leros_project" SET`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	updated, err := cleanupOrgProjectSkillReferences(ctx, database, 100, "demo-skill")
	if err != nil {
		t.Fatalf("cleanupOrgProjectSkillReferences failed: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
