package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/types"
)

func setupProjectActivityTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	if err := database.AutoMigrate(&types.ProjectActivity{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	return database
}

func TestListProjectActivities_FiltersByOperatorIDs(t *testing.T) {
	database := setupProjectActivityTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	activities := []*types.ProjectActivity{
		{
			ProjectID:  "prj_test",
			OperatorID: "user_a",
			ActionType: types.ProjectActivityActionProjectCreated,
			CreatedAt:  now.Add(-2 * time.Hour),
		},
		{
			ProjectID:  "prj_test",
			OperatorID: "user_b",
			ActionType: types.ProjectActivityActionParticipantsChanged,
			CreatedAt:  now.Add(-1 * time.Hour),
		},
		{
			ProjectID:  "prj_test",
			OperatorID: "user_c",
			ActionType: types.ProjectActivityActionSkillsChanged,
			CreatedAt:  now,
		},
	}
	for _, activity := range activities {
		if err := CreateProjectActivity(ctx, database, activity); err != nil {
			t.Fatalf("CreateProjectActivity failed: %v", err)
		}
	}

	filtered, err := ListProjectActivities(ctx, database, ProjectActivityListOptions{
		ProjectID:   "prj_test",
		OperatorIDs: []string{"user_a", "user_c"},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("ListProjectActivities failed: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("filtered count = %d, want 2", len(filtered))
	}
	if filtered[0].OperatorID != "user_c" || filtered[1].OperatorID != "user_a" {
		t.Fatalf("unexpected operator order: %#v", []string{filtered[0].OperatorID, filtered[1].OperatorID})
	}
}
