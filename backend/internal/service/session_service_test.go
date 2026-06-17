package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/nats-io/nats.go"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/types"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	if err := db.AutoMigrate(
		&types.Project{},
		&types.ProjectMember{},
		&types.Task{},
		&types.Session{},
		&types.SessionMessage{},
		&types.Artifact{},
		&types.LLMModel{},
		&types.FileUpload{},
	); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	if err := db.Create(&types.LLMModel{
		OrgID:           1,
		Code:            "default",
		Name:            "Default",
		Provider:        "openai",
		ModelName:       "gpt-test",
		BaseURL:         "https://api.openai.com",
		BaseURLHasV1:    true,
		APIKeyEncrypted: "sk-test",
		Status:          string(types.LLMModelStatusActive),
		IsDefault:       true,
	}).Error; err != nil {
		t.Fatalf("failed to seed default llm model: %v", err)
	}

	return db
}

// mockEventBus is a simple test event bus.
type mockEventBus struct{}

func (m *mockEventBus) Publish(ctx context.Context, topic string, event any) error {
	return nil
}

func (m *mockEventBus) Subscribe(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error {
	return nil
}

func (m *mockEventBus) SubscribeFrom(ctx context.Context, topic string, startSeq int64, handler func(msg *nats.Msg)) error {
	return nil
}

// mockInferrer returns a fixed assistant ID.
type mockInferrer struct {
	assistantID uint
}

func (m *mockInferrer) InferAssignedAssistantID(ctx context.Context, sessionOrgID uint, sessionType string) uint {
	return m.assistantID
}

func setupTestService(t *testing.T) contract.SessionService {
	t.Helper()
	db := setupTestDB(t)
	inferrer := &mockInferrer{assistantID: 1}
	return NewSessionService(db, &mockEventBus{}, inferrer)
}

func setupTestServiceWithSubscriber(t *testing.T, subscriber mq.Subscriber) contract.SessionService {
	t.Helper()
	db := setupTestDB(t)
	inferrer := &mockInferrer{assistantID: 1}
	eb := &struct {
		mq.Publisher
		mq.Subscriber
	}{
		Publisher:  &mockEventBus{},
		Subscriber: subscriber,
	}
	return NewSessionService(db, eb, inferrer)
}

func setupTestContextWithoutCaller(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

func setupTestContextWithCaller(t *testing.T) context.Context {
	t.Helper()
	caller := &types.Caller{
		Uin:   1,
		OrgID: 1,
		State: types.AuthStateSucc,
	}
	trace := &types.Trace{
		RequestID: "test-request-id",
		TraceID:   "test-trace-id",
	}
	return auth.WithContext(context.Background(), caller, trace)
}

func addMessage(t *testing.T, service contract.SessionService, ctx context.Context, sessionID string, content string) {
	t.Helper()
	_, err := service.AddMessage(ctx, sessionID, &contract.AddMessageRequest{
		Role:    string(types.MessageRoleUser),
		Content: content,
	})
	if err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}
}

func TestCreateSession_ValidInput(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	req := &contract.CreateSessionRequest{
		Type:  string(types.SessionTypeUserChat),
		Title: "Test Session",
	}

	session, err := service.CreateSession(ctx, req)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if session.SessionID == "" {
		t.Error("expected session_id to be generated")
	}

	if session.Status != string(types.SessionStatusActive) {
		t.Errorf("expected status to be active, got %s", session.Status)
	}
}

func TestCreateSession_MissingType(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	req := &contract.CreateSessionRequest{
		Title: "Test Session",
	}

	_, err := service.CreateSession(ctx, req)
	if err == nil {
		t.Error("expected error for missing type")
	}

	if err.Error() != "type is required" {
		t.Errorf("expected 'type is required' error, got %s", err.Error())
	}
}

func TestCreateSession_CustomSessionID(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	req := &contract.CreateSessionRequest{
		SessionID: "custom_session_id",
		Type:      string(types.SessionTypeUserChat),
	}

	session, err := service.CreateSession(ctx, req)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if session.SessionID != "custom_session_id" {
		t.Errorf("expected session_id to be custom_session_id, got %s", session.SessionID)
	}
}

func TestCreateSession_DuplicateSessionID(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	req1 := &contract.CreateSessionRequest{
		SessionID: "duplicate_id",
		Type:      string(types.SessionTypeUserChat),
	}

	_, err := service.CreateSession(ctx, req1)
	if err != nil {
		t.Fatalf("first CreateSession failed: %v", err)
	}

	req2 := &contract.CreateSessionRequest{
		SessionID: "duplicate_id",
		Type:      string(types.SessionTypeUserChat),
	}

	_, err = service.CreateSession(ctx, req2)
	if err == nil {
		t.Error("expected error for duplicate session_id")
	}

	if err.Error() != "session with this public_id already exists" {
		t.Errorf("expected 'session already exists' error, got %s", err.Error())
	}
}

func TestGetSession_NotFound(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	_, err := service.GetSession(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent session")
	}

	if err.Error() != "session not found" {
		t.Errorf("expected 'session not found' error, got %s", err.Error())
	}
}

func TestGetSession_ByID(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type:  string(types.SessionTypeUserChat),
		Title: "Get By ID Test",
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	retrieved, err := service.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if retrieved.SessionID != session.SessionID {
		t.Errorf("expected SessionID %s, got %s", session.SessionID, retrieved.SessionID)
	}
}

func TestUpdateSession(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type:  string(types.SessionTypeUserChat),
		Title: "Original Title",
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	updateReq := &contract.UpdateSessionRequest{
		Title: "Updated Title",
	}

	updated, err := service.UpdateSession(ctx, session.SessionID, updateReq)
	if err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}

	if updated.Title != "Updated Title" {
		t.Errorf("expected title %q, got %q", "Updated Title", updated.Title)
	}
}

func TestUpdateSession_MarksTitleManuallySet(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type:  string(types.SessionTypeUserChat),
		Title: "Original Title",
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	updateReq := &contract.UpdateSessionRequest{
		Title: "Updated Title",
	}

	_, err = service.UpdateSession(ctx, session.SessionID, updateReq)
	if err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}

	retrieved, err := service.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if !retrieved.TitleManuallySet {
		t.Error("expected TitleManuallySet to be true after manual update")
	}
}

func TestHandleSessionTitleRequest_AfterManualRename(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	session, err := service.CreateSession(ctx, &contract.CreateSessionRequest{Type: string(types.SessionTypeUserChat)})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	_, err = service.UpdateSession(ctx, session.SessionID, &contract.UpdateSessionRequest{Title: "Manual title"})
	if err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}

	addMessage(t, service, ctx, session.SessionID, "hello")
	err = service.HandleSessionTitleRequest(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("HandleSessionTitleRequest failed: %v", err)
	}

	retrieved, err := service.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrieved.Title != "Manual title" {
		t.Errorf("expected title %q, got %q", "Manual title", retrieved.Title)
	}
	if !retrieved.TitleManuallySet {
		t.Error("expected TitleManuallySet to be true")
	}
}

func TestDeleteSession(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = service.DeleteSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	_, err = service.GetSession(ctx, session.SessionID)
	if err == nil {
		t.Error("expected error for deleted session")
	}
}

func TestActivateSession_InvalidState(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	service.EndSession(ctx, session.SessionID)

	err = service.ActivateSession(ctx, session.SessionID)
	if err == nil {
		t.Error("expected error for activating from ended state")
	}

	if err.Error() != "cannot activate from ended state" {
		t.Errorf("expected 'cannot activate from ended state' error, got %s", err.Error())
	}
}

func TestPauseSession(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = service.PauseSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("PauseSession failed: %v", err)
	}

	retrieved, err := service.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if retrieved.Status != string(types.SessionStatusPaused) {
		t.Errorf("expected status to be paused, got %s", retrieved.Status)
	}
}

func TestEndSession_AlreadyEnded(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	service.EndSession(ctx, session.SessionID)

	err = service.EndSession(ctx, session.SessionID)
	if err == nil {
		t.Error("expected error for ending already ended session")
	}

	if err.Error() != "session already ended" {
		t.Errorf("expected 'session already ended' error, got %s", err.Error())
	}
}

func TestResumeSession_NotPaused(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = service.ResumeSession(ctx, session.SessionID)
	if err == nil {
		t.Error("expected error for resuming non-paused session")
	}

	if err.Error() != "can only resume from paused state" {
		t.Errorf("expected 'can only resume from paused state' error, got %s", err.Error())
	}
}

func TestAddMessage_UpdatesSession(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	addReq := &contract.AddMessageRequest{
		Role:    string(types.MessageRoleUser),
		Content: "Test message",
	}

	_, err = service.AddMessage(ctx, session.SessionID, addReq)
	if err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}

	retrieved, err := service.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if retrieved.MessageCount != 1 {
		t.Errorf("expected message_count to be 1, got %d", retrieved.MessageCount)
	}

	if retrieved.LastMessageAt == nil {
		t.Error("expected last_message_at to be set")
	}
}

func TestAddMessage_AutoSequence(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	for i := 1; i <= 3; i++ {
		addReq := &contract.AddMessageRequest{
			Role:    string(types.MessageRoleUser),
			Content: "Message " + string(rune(i)),
		}

		msg, err := service.AddMessage(ctx, session.SessionID, addReq)
		if err != nil {
			t.Fatalf("AddMessage failed: %v", err)
		}

		if msg.Sequence != int64(i) {
			t.Errorf("expected sequence %d, got %d", i, msg.Sequence)
		}
	}
}

func TestAddMessage_MissingContent(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	addReq := &contract.AddMessageRequest{
		Role: string(types.MessageRoleUser),
	}

	_, err = service.AddMessage(ctx, session.SessionID, addReq)
	if err == nil {
		t.Error("expected error for missing content")
	}

	if err.Error() != "content is required" {
		t.Errorf("expected 'content is required' error, got %s", err.Error())
	}
}

func TestHandleSessionTitleRequest_EmptyTitle(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	session, err := service.CreateSession(ctx, &contract.CreateSessionRequest{Type: string(types.SessionTypeUserChat)})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	addMessage(t, service, ctx, session.SessionID, "hello")
	err = service.HandleSessionTitleRequest(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("HandleSessionTitleRequest failed: %v", err)
	}

	retrieved, err := service.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrieved.Title != "hello" {
		t.Errorf("expected title %q, got %q", "hello", retrieved.Title)
	}
}

func TestHandleSessionTitleRequest_XinSessionTitle(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	session, err := service.CreateSession(ctx, &contract.CreateSessionRequest{
		Type:  string(types.SessionTypeUserChat),
		Title: "New Session",
	})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	addMessage(t, service, ctx, session.SessionID, "hello")
	err = service.HandleSessionTitleRequest(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("HandleSessionTitleRequest failed: %v", err)
	}

	retrieved, err := service.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrieved.Title != "Manual title" {
		t.Errorf("expected title %q, got %q", "hello", retrieved.Title)
	}
}

func TestHandleSessionTitleRequest_Truncated(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	session, err := service.CreateSession(ctx, &contract.CreateSessionRequest{Type: string(types.SessionTypeUserChat)})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	longContent := ""
	for i := 0; i < 150; i++ {
		longContent += "a"
	}

	addMessage(t, service, ctx, session.SessionID, longContent)
	err = service.HandleSessionTitleRequest(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("HandleSessionTitleRequest failed: %v", err)
	}

	retrieved, err := service.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if len([]rune(retrieved.Title)) != 100 {
		t.Errorf("expected title %q, got %q", "hello", retrieved.Title)
	}
}

func TestHandleSessionTitleRequest_CustomTitleUnchanged(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	session, err := service.CreateSession(ctx, &contract.CreateSessionRequest{
		Type:  string(types.SessionTypeUserChat),
		Title: "New Session",
	})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	addMessage(t, service, ctx, session.SessionID, "hello")
	err = service.HandleSessionTitleRequest(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("HandleSessionTitleRequest failed: %v", err)
	}

	retrieved, err := service.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrieved.Title != "Manual title" {
		t.Errorf("expected title %q, got %q", "hello", retrieved.Title)
	}
}

func TestHandleSessionTitleRequest_ManuallySetFlag(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	session, err := service.CreateSession(ctx, &contract.CreateSessionRequest{Type: string(types.SessionTypeUserChat)})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	_, err = service.UpdateSession(ctx, session.SessionID, &contract.UpdateSessionRequest{Title: "鎵嬪姩鏍囬"})
	if err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}

	addMessage(t, service, ctx, session.SessionID, "hello")
	err = service.HandleSessionTitleRequest(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("HandleSessionTitleRequest failed: %v", err)
	}

	retrieved, err := service.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrieved.Title != "鎵嬪姩鏍囬" {
		t.Errorf("expected title %q, got %q", "hello", retrieved.Title)
	}
	if !retrieved.TitleManuallySet {
		t.Error("expected TitleManuallySet to be true")
	}
}

func TestDeleteMessage_UpdatesSession(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	addReq := &contract.AddMessageRequest{
		Role:    string(types.MessageRoleUser),
		Content: "Test message",
	}

	// 娣诲姞娑堟伅鑾峰彇 ID
	msg, err := service.AddMessage(ctx, session.SessionID, addReq)
	if err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}

	// 灏?string ID 杞崲鍥?uint
	var messageID uint
	fmt.Sscanf(msg.ID, "%d", &messageID)

	err = service.DeleteMessage(ctx, messageID)
	if err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	retrieved, err := service.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if retrieved.MessageCount != 1 {
		t.Errorf("expected message_count to be 1 after delete, got %d", retrieved.MessageCount)
	}
}

func TestListSessions_FilterByType(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	req1 := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}
	req2 := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeTask),
	}

	_, err := service.CreateSession(ctx, req1)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	_, err = service.CreateSession(ctx, req2)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	typeFilter := string(types.SessionTypeUserChat)
	listReq := &contract.ListSessionsRequest{
		Type: &typeFilter,
		Pagination: types.Pagination{
			Offset: 0,
			Limit:  20,
		},
	}

	result, err := service.ListSessions(ctx, listReq)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if result.Total != 1 {
		t.Errorf("expected 1 session, got %d", result.Total)
	}

	if result.Items[0].Type != string(types.SessionTypeUserChat) {
		t.Errorf("expected user_chat type, got %s", result.Items[0].Type)
	}
}

func TestListSessions_FilterByStatus(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	req1 := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}
	req2 := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}

	_, err := service.CreateSession(ctx, req1)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	session2, _ := service.CreateSession(ctx, req2)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	service.PauseSession(ctx, session2.SessionID)

	statusFilter := string(types.SessionStatusActive)
	listReq := &contract.ListSessionsRequest{
		Status: &statusFilter,
		Pagination: types.Pagination{
			Offset: 0,
			Limit:  20,
		},
	}

	result, err := service.ListSessions(ctx, listReq)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if result.Total != 1 {
		t.Errorf("expected 1 active session, got %d", result.Total)
	}
}

func TestGetSessionMessages(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	for i := 1; i <= 3; i++ {
		addReq := &contract.AddMessageRequest{
			Role:    string(types.MessageRoleUser),
			Content: "Message " + string(rune(i)),
		}
		_, err := service.AddMessage(ctx, session.SessionID, addReq)
		if err != nil {
			t.Fatalf("AddMessage failed: %v", err)
		}
	}

	result, err := service.GetSessionMessages(ctx, session.SessionID, 1, 20)
	if err != nil {
		t.Fatalf("GetSessionMessages failed: %v", err)
	}

	if result.Total != 3 {
		t.Errorf("expected 3 messages, got %d", result.Total)
	}

	if len(result.Items) != 3 {
		t.Errorf("expected 3 messages, got %d", len(result.Items))
	}
}

func TestCompleteSessionMessageStoresChunksAndUsage(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	session, err := service.CreateSession(ctx, &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	payload, err := json.Marshal(events.MessageDeltaPayload{
		MessageID: "msg_1",
		Role:      string(protocol.MessageRoleAssistant),
		Content:   "done",
	})
	if err != nil {
		t.Fatalf("marshal chunk payload: %v", err)
	}

	err = service.CompleteSessionMessage(ctx, &contract.CompleteSessionMessageRequest{
		SessionID: session.SessionID,
		Content:   "done",
		Chunks: []types.MessageChunk{
			{Seq: 1, Type: "message.delta", Timestamp: 1779243000000, Payload: payload},
		},
		Usage: &types.MessageUsage{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})
	if err != nil {
		t.Fatalf("CompleteSessionMessage failed: %v", err)
	}

	result, err := service.GetSessionMessages(ctx, session.SessionID, 1, 20)
	if err != nil {
		t.Fatalf("GetSessionMessages failed: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("expected one message, got total=%d len=%d", result.Total, len(result.Items))
	}
	msg := result.Items[0]
	if msg.Content != "done" {
		t.Fatalf("expected content %q, got %q", "done", msg.Content)
	}
	if len(msg.Chunks) != 1 {
		t.Fatalf("expected one chunk, got %#v", msg.Chunks)
	}
	if msg.Chunks[0].Sequence != 1 || msg.Chunks[0].Type != "message.delta" || msg.Chunks[0].Timestamp != 1779243000000 {
		t.Fatalf("unexpected chunk: %#v", msg.Chunks[0])
	}
	deltaPayload, ok := msg.Chunks[0].Payload.(dto.MessageDeltaPayload)
	if !ok || deltaPayload.Content != "done" || deltaPayload.MessageID != "msg_1" {
		t.Fatalf("unexpected projected payload: %#v", msg.Chunks[0].Payload)
	}
	if msg.Usage == nil || msg.Usage.InputTokens != 10 || msg.Usage.OutputTokens != 20 || msg.Usage.TotalTokens != 30 {
		t.Fatalf("unexpected usage: %#v", msg.Usage)
	}
	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if _, ok := raw["thinking"]; ok {
		t.Fatalf("history message should not include top-level thinking: %s", body)
	}
	if _, ok := raw["tool_calls"]; ok {
		t.Fatalf("history message should not include top-level tool_calls: %s", body)
	}
}

func TestCompleteSessionMessageBindsExistingDeclaredArtifact(t *testing.T) {
	database := setupTestDB(t)
	service := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1})
	ctx := setupTestContextWithCaller(t)
	projectID := uint(10)
	taskID := uint(20)
	session := &types.Session{
		PublicID:  "sess_artifact",
		Type:      types.SessionTypeTask,
		Uin:       1,
		OrgID:     1,
		ProjectID: &projectID,
		TaskID:    &taskID,
		Status:    string(types.SessionStatusActive),
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	artifact := &types.Artifact{
		PublicID:     "art_existing",
		OrgID:        1,
		OwnerID:      1,
		TaskID:       taskID,
		ProjectID:    projectID,
		SessionID:    &session.ID,
		Title:        "Report",
		Filename:     "report.md",
		ArtifactType: string(types.ArtifactTypeFile),
		MimeType:     "text/markdown",
		FileURL:      "/v1/artifacts/art_existing/download",
		RelativePath: "docs/report.md",
		StorageKey:   "projects/1/project_10/repo/docs/report.md",
		Source:       string(types.ArtifactSourceAgentDeclared),
		Status:       string(types.ArtifactStatusCompleted),
	}
	if err := database.Create(artifact).Error; err != nil {
		t.Fatalf("create artifact: %v", err)
	}

	chunkPayload, err := json.Marshal(events.ArtifactPayload{
		ArtifactID:   artifact.PublicID,
		Title:        artifact.Title,
		Filename:     artifact.Filename,
		MimeType:     artifact.MimeType,
		ArtifactType: artifact.ArtifactType,
	})
	if err != nil {
		t.Fatalf("marshal artifact chunk: %v", err)
	}
	messageArtifacts := []types.MessageArtifact{
		{ArtifactID: artifact.PublicID, Title: artifact.Title, Filename: artifact.Filename, MimeType: artifact.MimeType, ArtifactType: artifact.ArtifactType},
	}
	err = service.CompleteSessionMessage(ctx, &contract.CompleteSessionMessageRequest{
		SessionID: session.PublicID,
		Content:   "done",
		Artifacts: messageArtifacts,
		Chunks: []types.MessageChunk{
			{Seq: 1, Type: string(events.EventArtifactDeclared), Timestamp: 1779243000000, Payload: chunkPayload},
		},
	})
	if err != nil {
		t.Fatalf("CompleteSessionMessage failed: %v", err)
	}

	var artifacts []types.Artifact
	if err := database.Find(&artifacts).Error; err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected existing artifact to be reused, got %d rows", len(artifacts))
	}
	// 不再验证 artifact.message_id 绑定，artifact 通过 session_id 关联查询
	result, err := service.GetSessionMessages(ctx, session.PublicID, 1, 20)
	if err != nil {
		t.Fatalf("GetSessionMessages failed: %v", err)
	}
	if len(result.Items) != 1 ||
		len(result.Items[0].Artifacts) != 1 ||
		result.Items[0].Artifacts[0].ArtifactID != artifact.PublicID ||
		result.Items[0].Artifacts[0].Filename != "report.md" ||
		result.Items[0].Artifacts[0].MimeType != "text/markdown" {
		t.Fatalf("expected message artifacts to be persisted, got %#v", result.Items)
	}
}

func TestGetSessionMessagesFiltersTodoChunks(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	session, err := service.CreateSession(ctx, &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	deltaPayload, err := json.Marshal(events.MessageDeltaPayload{
		MessageID: "msg_1",
		Role:      string(protocol.MessageRoleAssistant),
		Content:   "done",
	})
	if err != nil {
		t.Fatalf("marshal delta payload: %v", err)
	}
	todoPayload, err := json.Marshal([]events.RuntimeTodoItem{
		{ID: "todo_1", Title: "Inspect code", Status: "completed"},
	})
	if err != nil {
		t.Fatalf("marshal todo payload: %v", err)
	}

	err = service.CompleteSessionMessage(ctx, &contract.CompleteSessionMessageRequest{
		SessionID: session.SessionID,
		Content:   "done",
		Chunks: []types.MessageChunk{
			{Seq: 1, Type: string(events.EventMessageDelta), Timestamp: 1779243000000, Payload: deltaPayload},
			{Seq: 2, Type: string(events.EventTodoSnapshot), Timestamp: 1779243000001, Payload: todoPayload},
			{Seq: 3, Type: string(events.EventTodoUpdated), Timestamp: 1779243000002, Payload: todoPayload},
		},
	})
	if err != nil {
		t.Fatalf("CompleteSessionMessage failed: %v", err)
	}

	result, err := service.GetSessionMessages(ctx, session.SessionID, 1, 20)
	if err != nil {
		t.Fatalf("GetSessionMessages failed: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("expected one message, got total=%d len=%d", result.Total, len(result.Items))
	}
	chunks := result.Items[0].Chunks
	if len(chunks) != 1 {
		t.Fatalf("expected only non-todo chunk, got %#v", chunks)
	}
	if chunks[0].Type != string(events.EventMessageDelta) || chunks[0].Sequence != 1 {
		t.Fatalf("unexpected remaining chunk: %#v", chunks[0])
	}
}

func TestClearSessionMessages(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	createReq := &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	}

	session, err := service.CreateSession(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	for i := 1; i <= 3; i++ {
		addReq := &contract.AddMessageRequest{
			Role:    string(types.MessageRoleUser),
			Content: "Message " + string(rune(i)),
		}
		_, err := service.AddMessage(ctx, session.SessionID, addReq)
		if err != nil {
			t.Fatalf("AddMessage failed: %v", err)
		}
	}

	err = service.ClearSessionMessages(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("ClearSessionMessages failed: %v", err)
	}

	retrieved, err := service.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if retrieved.MessageCount != 0 {
		t.Errorf("expected message_count to be 0 after clear, got %d", retrieved.MessageCount)
	}

	if retrieved.LastMessageAt != nil {
		t.Error("expected last_message_at to be nil after clear")
	}
}

func TestCreateSession_MissingCaller(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithoutCaller(t)

	req := &contract.CreateSessionRequest{
		Type:  string(types.SessionTypeUserChat),
		Title: "Test Session",
	}

	_, err := service.CreateSession(ctx, req)
	if err == nil {
		t.Error("expected error when caller is not authenticated")
	}

	if err.Error() != "user not authenticated or org not set" {
		t.Errorf("expected 'user not authenticated or org not set' error, got %s", err.Error())
	}
}

func TestStreamSessionEvents_MissingCaller(t *testing.T) {
	service := setupTestServiceWithSubscriber(t, nil)
	ctx := setupTestContextWithoutCaller(t)

	err := service.StreamSessionEvents(ctx, "test_session", 0, nil)
	if err == nil {
		t.Error("expected error when caller is not authenticated")
	}

	if err.Error() != "user not authenticated or org not set" {
		t.Errorf("expected 'user not authenticated or org not set' error, got %s", err.Error())
	}
}
