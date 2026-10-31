package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/types"
)

func setupHistoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&types.LLMHistory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func TestCreateAndListLLMHistory(t *testing.T) {
	ctx := context.Background()
	database := setupHistoryDB(t)

	r1 := &types.LLMHistory{
		OrgID: 1, Provider: "openai", ModelName: "gpt-4o",
		CallerType: "feedback_summarizer", Success: true,
		InputTokens: 100, OutputTokens: 50, TotalTokens: 150,
		StartedAt: time.Now().Add(-1 * time.Hour),
	}
	r2 := &types.LLMHistory{
		OrgID: 1, Provider: "anthropic", ModelName: "claude-3",
		CallerType: "work_title_updater", Success: false,
		InputTokens: 200, OutputTokens: 0, TotalTokens: 200,
		Message:   "timeout",
		StartedAt: time.Now(),
	}
	if err := CreateLLMHistory(ctx, database, r1); err != nil {
		t.Fatalf("create r1: %v", err)
	}
	if err := CreateLLMHistory(ctx, database, r2); err != nil {
		t.Fatalf("create r2: %v", err)
	}
	if r1.ID == 0 || r2.ID == 0 {
		t.Fatal("expected ID to be set after insert")
	}

	// List all for org 1
	records, total, err := ListLLMHistory(ctx, database, 1, 0, 50, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 || len(records) != 2 {
		t.Fatalf("expected 2 records, got total=%d len=%d", total, len(records))
	}
	// Verify DESC order by started_at — r2 (later) should be first
	if records[0].ModelName != "claude-3" {
		t.Fatalf("expected claude-3 first (DESC), got %s", records[0].ModelName)
	}

	// Filter by provider
	provider := "openai"
	records, total, err = ListLLMHistory(ctx, database, 1, 0, 50, nil, &provider, nil, nil)
	if err != nil {
		t.Fatalf("list by provider: %v", err)
	}
	if total != 1 || records[0].Provider != "openai" {
		t.Fatalf("expected 1 openai record, got total=%d", total)
	}

	// Filter by callerType
	ct := "work_title_updater"
	records, total, err = ListLLMHistory(ctx, database, 1, 0, 50, nil, nil, &ct, nil)
	if err != nil {
		t.Fatalf("list by callerType: %v", err)
	}
	if total != 1 || records[0].CallerType != "work_title_updater" {
		t.Fatalf("expected 1 work_title_updater record, got total=%d", total)
	}

	// Filter by success
	failed := false
	records, total, err = ListLLMHistory(ctx, database, 1, 0, 50, nil, nil, nil, &failed)
	if err != nil {
		t.Fatalf("list by success: %v", err)
	}
	if total != 1 || records[0].Success != false {
		t.Fatalf("expected 1 failed record, got total=%d", total)
	}

	// Different org — should return 0
	records, total, err = ListLLMHistory(ctx, database, 99, 0, 50, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("list other org: %v", err)
	}
	if total != 0 || len(records) != 0 {
		t.Fatalf("expected 0 records for org 99, got total=%d", total)
	}
}
