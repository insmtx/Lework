package db

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/types"
)

func TestCreateMessage(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "create_msg_session",
		Type:      string(types.SessionTypeUserChat),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	message := &types.SessionMessage{
		SessionID: session.ID,
		Role:      string(types.MessageRoleUser),
		Content:   "Test message",
		Sequence:  1,
	}

	err := CreateMessage(ctx, db, message)
	if err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}

	if message.ID == 0 {
		t.Error("expected message ID to be set")
	}
}

func TestGetMessageByID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "get_msg_session",
		Type:      string(types.SessionTypeUserChat),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	message := &types.SessionMessage{
		SessionID: session.ID,
		Role:      string(types.MessageRoleUser),
		Content:   "Get message test",
		Sequence:  1,
	}

	if err := CreateMessage(ctx, db, message); err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	retrieved, err := GetMessageByID(ctx, db, message.ID)
	if err != nil {
		t.Fatalf("GetMessageByID failed: %v", err)
	}

	if retrieved == nil {
		t.Fatal("expected message to be found")
	}

	if retrieved.Content != "Get message test" {
		t.Errorf("expected content 'Get message test', got %s", retrieved.Content)
	}
}

func TestGetSessionMessages(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "get_messages_session",
		Type:      string(types.SessionTypeUserChat),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	for i := 1; i <= 3; i++ {
		message := &types.SessionMessage{
			SessionID: session.ID,
			Role:      string(types.MessageRoleUser),
			Content:   "Message " + string(rune(i)),
			Sequence:  int64(i),
		}
		if err := CreateMessage(ctx, db, message); err != nil {
			t.Fatalf("failed to create message %d: %v", i, err)
		}
	}

	messages, total, err := GetSessionMessages(ctx, db, session.ID, 1, 20)
	if err != nil {
		t.Fatalf("GetSessionMessages failed: %v", err)
	}

	if total != 3 {
		t.Errorf("expected 3 messages, got %d", total)
	}

	if len(messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(messages))
	}

	if messages[0].Sequence != 1 {
		t.Errorf("expected messages to be sorted by sequence, got %d", messages[0].Sequence)
	}
}

func TestGetSessionMessages_Pagination(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "pagination_msg_session",
		Type:      string(types.SessionTypeUserChat),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	for i := 1; i <= 5; i++ {
		message := &types.SessionMessage{
			SessionID: session.ID,
			Role:      string(types.MessageRoleUser),
			Content:   "Message " + string(rune(i)),
			Sequence:  int64(i),
		}
		if err := CreateMessage(ctx, db, message); err != nil {
			t.Fatalf("failed to create message %d: %v", i, err)
		}
	}

	messages, total, err := GetSessionMessages(ctx, db, session.ID, 1, 2)
	if err != nil {
		t.Fatalf("GetSessionMessages failed: %v", err)
	}

	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}

	if len(messages) != 2 {
		t.Errorf("expected 2 messages on page 1, got %d", len(messages))
	}
}

func TestDeleteMessage(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "delete_msg_session",
		Type:      string(types.SessionTypeUserChat),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	message := &types.SessionMessage{
		SessionID: session.ID,
		Role:      string(types.MessageRoleUser),
		Content:   "Delete message test",
		Sequence:  1,
	}

	if err := CreateMessage(ctx, db, message); err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	err := DeleteMessage(ctx, db, message.ID)
	if err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	retrieved, err := GetMessageByID(ctx, db, message.ID)
	if err != nil {
		t.Fatalf("GetMessageByID failed: %v", err)
	}

	if retrieved != nil {
		t.Error("expected message to be deleted")
	}
}

func TestClearSessionMessages(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "clear_msg_session",
		Type:      string(types.SessionTypeUserChat),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	for i := 1; i <= 3; i++ {
		message := &types.SessionMessage{
			SessionID: session.ID,
			Role:      string(types.MessageRoleUser),
			Content:   "Message " + string(rune(i)),
			Sequence:  int64(i),
		}
		if err := CreateMessage(ctx, db, message); err != nil {
			t.Fatalf("failed to create message %d: %v", i, err)
		}
	}

	err := ClearSessionMessages(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("ClearSessionMessages failed: %v", err)
	}

	count, err := GetMessageCount(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetMessageCount failed: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 messages after clear, got %d", count)
	}
}

func TestGetLatestMessage(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "latest_msg_session",
		Type:      string(types.SessionTypeUserChat),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	for i := 1; i <= 3; i++ {
		message := &types.SessionMessage{
			SessionID: session.ID,
			Role:      string(types.MessageRoleUser),
			Content:   "Message " + string(rune(i)),
			Sequence:  int64(i),
		}
		if err := CreateMessage(ctx, db, message); err != nil {
			t.Fatalf("failed to create message %d: %v", i, err)
		}
	}

	latest, err := GetLatestMessage(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetLatestMessage failed: %v", err)
	}

	if latest == nil {
		t.Fatal("expected latest message to be found")
	}

	if latest.Sequence != 3 {
		t.Errorf("expected latest message sequence to be 3, got %d", latest.Sequence)
	}
}

func TestGetMessageCount(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "count_msg_session",
		Type:      string(types.SessionTypeUserChat),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	for i := 1; i <= 5; i++ {
		message := &types.SessionMessage{
			SessionID: session.ID,
			Role:      string(types.MessageRoleUser),
			Content:   "Message " + string(rune(i)),
			Sequence:  int64(i),
		}
		if err := CreateMessage(ctx, db, message); err != nil {
			t.Fatalf("failed to create message %d: %v", i, err)
		}
	}

	count, err := GetMessageCount(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetMessageCount failed: %v", err)
	}

	if count != 5 {
		t.Errorf("expected count to be 5, got %d", count)
	}
}

func TestGetNextSequence(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "sequence_session",
		Type:      string(types.SessionTypeUserChat),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	for i := 1; i <= 3; i++ {
		message := &types.SessionMessage{
			SessionID: session.ID,
			Role:      string(types.MessageRoleUser),
			Content:   "Message " + string(rune(i)),
			Sequence:  int64(i),
		}
		if err := CreateMessage(ctx, db, message); err != nil {
			t.Fatalf("failed to create message %d: %v", i, err)
		}
	}

	nextSequence, err := GetNextSequence(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetNextSequence failed: %v", err)
	}

	if nextSequence != 4 {
		t.Errorf("expected next sequence to be 4, got %d", nextSequence)
	}
}

func TestGetNextSequence_EmptySession(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	session := &types.Session{
		PublicID: "empty_sequence_session",
		Type:      string(types.SessionTypeUserChat),
	}

	if err := CreateSession(ctx, db, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	nextSequence, err := GetNextSequence(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetNextSequence failed: %v", err)
	}

	if nextSequence != 1 {
		t.Errorf("expected next sequence to be 1 for empty session, got %d", nextSequence)
	}
}
