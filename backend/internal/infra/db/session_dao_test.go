package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	if err := db.AutoMigrate(&types.Session{}, &types.SessionMessage{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	return db
}

func TestCreateSession(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "test_session_1",
		Type:      string(types.SessionTypeUserChat),
		Uin:       1,
		Title:     "Test Session",
	}

	err := CreateSession(ctx, db, session)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if session.ID == 0 {
		t.Error("expected session ID to be set")
	}

	if session.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestCreateSession_DuplicatePublicID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session1 := &types.Session{
		PublicID: "duplicate_id",
		Type:      string(types.SessionTypeUserChat),
	}

	err := CreateSession(ctx, db, session1)
	if err != nil {
		t.Fatalf("first CreateSession failed: %v", err)
	}

	session2 := &types.Session{
		PublicID: "duplicate_id",
		Type:      string(types.SessionTypeUserChat),
	}

	err = CreateSession(ctx, db, session2)
	if err == nil {
		t.Error("expected error for duplicate session_id")
	}
}

func TestGetSessionByID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "get_by_id_test",
		Type:      string(types.SessionTypeUserChat),
		Uin:       1,
		Title:     "Get By ID Test",
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	retrieved, err := GetSessionByID(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetSessionByID failed: %v", err)
	}

	if retrieved == nil {
		t.Fatal("expected session to be found")
	}

	if retrieved.PublicID != session.PublicID {
		t.Errorf("expected PublicID %s, got %s", session.PublicID, retrieved.PublicID)
	}
}

func TestGetSessionByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	retrieved, err := GetSessionByID(ctx, db, 999)
	if err != nil {
		t.Fatalf("GetSessionByID failed: %v", err)
	}

	if retrieved != nil {
		t.Error("expected nil for non-existent session")
	}
}

func TestGetSessionByPublicID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "get_by_session_id_test",
		Type:      string(types.SessionTypeUserChat),
		Title:     "Get By PublicID Test",
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	retrieved, err := GetSessionByPublicID(ctx, db, session.PublicID)
	if err != nil {
		t.Fatalf("GetSessionByPublicID failed: %v", err)
	}

	if retrieved == nil {
		t.Fatal("expected session to be found")
	}

	if retrieved.ID != session.ID {
		t.Errorf("expected ID %d, got %d", session.ID, retrieved.ID)
	}
}

func TestUpdateSession(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "update_test",
		Type:      string(types.SessionTypeUserChat),
		Title:     "Original Title",
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	session.Title = "Updated Title"
	err := UpdateSession(ctx, db, session)
	if err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}

	retrieved, err := GetSessionByID(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetSessionByID failed: %v", err)
	}

	if retrieved.Title != "Updated Title" {
		t.Errorf("expected title to be updated, got %s", retrieved.Title)
	}
}

func TestDeleteSession(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "delete_test",
		Type:      string(types.SessionTypeUserChat),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	err := DeleteSession(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	retrieved, err := GetSessionByID(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetSessionByID failed: %v", err)
	}

	if retrieved != nil {
		t.Error("expected session to be deleted")
	}
}

func TestActivateSession(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "activate_test",
		Type:      string(types.SessionTypeUserChat),
		Status:    string(types.SessionStatusEnded),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	err := ActivateSession(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("ActivateSession failed: %v", err)
	}

	retrieved, err := GetSessionByID(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetSessionByID failed: %v", err)
	}

	if retrieved.Status != string(types.SessionStatusActive) {
		t.Errorf("expected status to be active, got %s", retrieved.Status)
	}
}

func TestPauseSession(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "pause_test",
		Type:      string(types.SessionTypeUserChat),
		Status:    string(types.SessionStatusActive),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	err := PauseSession(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("PauseSession failed: %v", err)
	}

	retrieved, err := GetSessionByID(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetSessionByID failed: %v", err)
	}

	if retrieved.Status != string(types.SessionStatusPaused) {
		t.Errorf("expected status to be paused, got %s", retrieved.Status)
	}
}

func TestEndSession(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "end_test",
		Type:      string(types.SessionTypeUserChat),
		Status:    string(types.SessionStatusActive),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	err := EndSession(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("EndSession failed: %v", err)
	}

	retrieved, err := GetSessionByID(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetSessionByID failed: %v", err)
	}

	if retrieved.Status != string(types.SessionStatusEnded) {
		t.Errorf("expected status to be ended, got %s", retrieved.Status)
	}
}

func TestResumeSession(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "resume_test",
		Type:      string(types.SessionTypeUserChat),
		Status:    string(types.SessionStatusPaused),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	err := ResumeSession(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("ResumeSession failed: %v", err)
	}

	retrieved, err := GetSessionByID(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetSessionByID failed: %v", err)
	}

	if retrieved.Status != string(types.SessionStatusActive) {
		t.Errorf("expected status to be active, got %s", retrieved.Status)
	}
}

func TestExpireSessions(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	expiredAt := time.Now().Add(-1 * time.Hour)
	session := &types.Session{
		PublicID: "expire_test",
		Type:      string(types.SessionTypeUserChat),
		Status:    string(types.SessionStatusActive),
		ExpiredAt: &expiredAt,
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	err := ExpireSessions(ctx, db)
	if err != nil {
		t.Fatalf("ExpireSessions failed: %v", err)
	}

	retrieved, err := GetSessionByID(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetSessionByID failed: %v", err)
	}

	if retrieved.Status != string(types.SessionStatusExpired) {
		t.Errorf("expected status to be expired, got %s", retrieved.Status)
	}
}

func TestListSessions_ByType(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session1 := &types.Session{
		PublicID: "type_test_1",
		Type:      string(types.SessionTypeUserChat),
	}
	session2 := &types.Session{
		PublicID: "type_test_2",
		Type:      string(types.SessionTypeAssistantInstance),
	}

	if err := CreateSession(ctx, db, session1); err != nil {
		t.Fatalf("failed to create session1: %v", err)
	}
	if err := CreateSession(ctx, db, session2); err != nil {
		t.Fatalf("failed to create session2: %v", err)
	}

	typeFilter := string(types.SessionTypeUserChat)
	sessions, total, err := ListSessions(ctx, db, &typeFilter, nil, nil, nil, nil, nil, nil, 0, 20)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if total != 1 {
		t.Errorf("expected 1 session, got %d", total)
	}

	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}

	if sessions[0].Type != string(types.SessionTypeUserChat) {
		t.Errorf("expected user_chat type, got %s", sessions[0].Type)
	}
}

func TestListSessions_ByStatus(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session1 := &types.Session{
		PublicID: "status_test_1",
		Type:      string(types.SessionTypeUserChat),
		Status:    string(types.SessionStatusActive),
	}
	session2 := &types.Session{
		PublicID: "status_test_2",
		Type:      string(types.SessionTypeUserChat),
		Status:    string(types.SessionStatusPaused),
	}

	if err := CreateSession(ctx, db, session1); err != nil {
		t.Fatalf("failed to create session1: %v", err)
	}
	if err := CreateSession(ctx, db, session2); err != nil {
		t.Fatalf("failed to create session2: %v", err)
	}

	statusFilter := string(types.SessionStatusActive)
	sessions, total, err := ListSessions(ctx, db, nil, &statusFilter, nil, nil, nil, nil, nil, 0, 20)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if total != 1 {
		t.Errorf("expected 1 session, got %d", total)
	}

	if sessions[0].Status != string(types.SessionStatusActive) {
		t.Errorf("expected active status, got %s", sessions[0].Status)
	}
}

func TestListSessions_ByKeyword(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session1 := &types.Session{
		PublicID: "keyword_test_1",
		Type:      string(types.SessionTypeUserChat),
		Title:     "Project Alpha",
	}
	session2 := &types.Session{
		PublicID: "keyword_test_2",
		Type:      string(types.SessionTypeUserChat),
		Title:     "Project Beta",
	}

	if err := CreateSession(ctx, db, session1); err != nil {
		t.Fatalf("failed to create session1: %v", err)
	}
	if err := CreateSession(ctx, db, session2); err != nil {
		t.Fatalf("failed to create session2: %v", err)
	}

	keyword := "Alpha"
	sessions, total, err := ListSessions(ctx, db, nil, nil, nil, nil, nil, nil, &keyword, 0, 20)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if total != 1 {
		t.Errorf("expected 1 session, got %d", total)
	}

	if sessions[0].Title != "Project Alpha" {
		t.Errorf("expected Project Alpha, got %s", sessions[0].Title)
	}
}

func TestListSessions_Pagination(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		session := &types.Session{
			PublicID: "pagination_test_" + string(rune(i)),
			Type:      string(types.SessionTypeUserChat),
		}
		if err := CreateSession(ctx, db, session); err != nil {
			t.Fatalf("failed to create session %d: %v", i, err)
		}
	}

	sessions, total, err := ListSessions(ctx, db, nil, nil, nil, nil, nil, nil, nil, 0, 2)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}

	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions on page 1, got %d", len(sessions))
	}
}

func TestIncrementMessageCount(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID:    "increment_test",
		Type:         string(types.SessionTypeUserChat),
		MessageCount: 0,
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	err := IncrementMessageCount(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("IncrementMessageCount failed: %v", err)
	}

	retrieved, err := GetSessionByID(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetSessionByID failed: %v", err)
	}

	if retrieved.MessageCount != 1 {
		t.Errorf("expected message_count to be 1, got %d", retrieved.MessageCount)
	}
}

func TestUpdateLastMessageAt(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "last_message_test",
		Type:      string(types.SessionTypeUserChat),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	now := time.Now()
	err := UpdateLastMessageAt(ctx, db, session.ID, now)
	if err != nil {
		t.Fatalf("UpdateLastMessageAt failed: %v", err)
	}

	retrieved, err := GetSessionByID(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetSessionByID failed: %v", err)
	}

	if retrieved.LastMessageAt == nil {
		t.Error("expected LastMessageAt to be set")
	}
}
