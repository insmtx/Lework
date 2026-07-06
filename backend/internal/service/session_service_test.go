package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	db "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/prompts"
	"github.com/insmtx/Leros/backend/types"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf(
		"file:%s-%d?mode=memory&cache=shared",
		strings.ReplaceAll(t.Name(), "/", "-"),
		time.Now().UnixNano(),
	)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get test database handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&types.Project{},
		&types.ProjectMember{},
		&types.Task{},
		&types.Session{},
		&types.SessionMessage{},
		&types.DigitalAssistant{},
		&types.DigitalAssistantPromptBlock{},
		&types.DigitalAssistantMemory{},
		&types.AssistantPromptTrace{},
		&types.LLMModel{},
		&types.FileUpload{},
		&types.ProjectFile{},
		&types.WorkerDeployment{},
		&types.OrgSkillInstallation{},
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

type recordingEventBus struct {
	topic string
	event any
}

func (m *recordingEventBus) Publish(ctx context.Context, topic string, event any) error {
	m.topic = topic
	m.event = event
	return nil
}

func (m *recordingEventBus) Subscribe(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error {
	return nil
}

func (m *recordingEventBus) SubscribeFrom(ctx context.Context, topic string, startSeq int64, handler func(msg *nats.Msg)) error {
	return nil
}

func (m *recordingEventBus) SubscribeManualDurable(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error {
	return nil
}

func (m *recordingEventBus) Request(_ context.Context, _ string, _ any) (*nats.Msg, error) {
	return nil, fmt.Errorf("recordingEventBus: Request not supported")
}

// publishedEvent 记录一次 Publish 调用的 topic 和 event。
type publishedEvent struct {
	topic string
	event any
}

// publishRecorder 嵌入 mockEventBus 并覆盖 Publish，记录所有发布调用。
type publishRecorder struct {
	mockEventBus
	mu     sync.Mutex
	events []publishedEvent
}

func (p *publishRecorder) Publish(ctx context.Context, topic string, event any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, publishedEvent{topic: topic, event: event})
	return nil
}

func (p *publishRecorder) lastEvent() (publishedEvent, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.events) == 0 {
		return publishedEvent{}, false
	}
	return p.events[len(p.events)-1], true
}

func (m *mockEventBus) Subscribe(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error {
	return nil
}

func (m *mockEventBus) SubscribeFrom(ctx context.Context, topic string, startSeq int64, handler func(msg *nats.Msg)) error {
	return nil
}

func (m *mockEventBus) SubscribeManualDurable(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error {
	return nil
}

func (m *mockEventBus) Request(_ context.Context, _ string, _ any) (*nats.Msg, error) {
	return nil, fmt.Errorf("mockEventBus: Request not supported")
}

type replayEventBus struct {
	messages []*nats.Msg
	startSeq int64
}

func (m *replayEventBus) Publish(ctx context.Context, topic string, event any) error {
	return nil
}

func (m *replayEventBus) Subscribe(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error {
	return nil
}

func (m *replayEventBus) Request(_ context.Context, _ string, _ any) (*nats.Msg, error) {
	return nil, fmt.Errorf("replayEventBus: Request not supported")
}

func (m *replayEventBus) SubscribeFrom(ctx context.Context, topic string, startSeq int64, handler func(msg *nats.Msg)) error {
	if !strings.Contains(topic, ".run.stream") {
		<-ctx.Done()
		return ctx.Err()
	}
	m.startSeq = startSeq
	for _, msg := range m.messages {
		handler(msg)
	}
	return nil
}

func (m *replayEventBus) SubscribeManualDurable(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error {
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
	return NewSessionService(db, &mockEventBus{}, inferrer, nil, nil, "test")
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
	return NewSessionService(db, eb, inferrer, nil, nil, "test")
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

func createTestSession(t *testing.T, database *gorm.DB, svc contract.SessionService, ctx context.Context) *types.Session {
	t.Helper()
	session, err := svc.CreateSession(ctx, &contract.CreateSessionRequest{
		Type:  string(types.SessionTypeUserChat),
		Title: "test",
	})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	entity, err := db.GetSessionByPublicID(ctx, database, session.SessionID)
	if err != nil {
		t.Fatalf("GetSessionByPublicID failed: %v", err)
	}
	return entity
}

func createUserMessage(t *testing.T, database *gorm.DB, sessionID uint, status string, sequence int64) *types.SessionMessage {
	t.Helper()
	message := &types.SessionMessage{
		SessionID:   sessionID,
		Role:        string(types.MessageRoleUser),
		Content:     fmt.Sprintf("user %d", sequence),
		MessageType: string(types.MessageTypeText),
		Status:      status,
		Sequence:    sequence,
		Timestamp:   time.Now().UnixMilli(),
	}
	if err := db.CreateMessage(context.Background(), database, message); err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}
	return message
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

func TestGetSessionRuntimeStatusRespondingForRecentProcessingMessage(t *testing.T) {
	database := setupTestDB(t)
	service := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test")
	ctx := setupTestContextWithCaller(t)
	session := createTestSession(t, database, service, ctx)
	createUserMessage(t, database, session.ID, string(types.MessageStatusProcessing), 1)

	got, err := service.GetSession(ctx, session.PublicID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.RuntimeStatus != sessionRuntimeStatusResponding {
		t.Fatalf("runtime_status = %q, want %q", got.RuntimeStatus, sessionRuntimeStatusResponding)
	}
}

func TestGetSessionRuntimeStatusIgnoresOldProcessingMessage(t *testing.T) {
	database := setupTestDB(t)
	service := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test")
	ctx := setupTestContextWithCaller(t)
	session := createTestSession(t, database, service, ctx)
	message := createUserMessage(t, database, session.ID, string(types.MessageStatusProcessing), 1)
	old := time.Now().Add(-31 * time.Minute)
	if err := database.Model(message).Updates(map[string]any{
		"updated_at": old,
	}).Error; err != nil {
		t.Fatalf("update message failed: %v", err)
	}

	got, err := service.GetSession(ctx, session.PublicID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.RuntimeStatus != sessionRuntimeStatusIdle {
		t.Fatalf("runtime_status = %q, want %q", got.RuntimeStatus, sessionRuntimeStatusIdle)
	}
}

func TestHandleSessionRunStartedMarksReplyMessagesProcessing(t *testing.T) {
	database := setupTestDB(t)
	service := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test")
	ctx := setupTestContextWithCaller(t)
	session := createTestSession(t, database, service, ctx)
	first := createUserMessage(t, database, session.ID, string(types.MessageStatusPending), 1)
	second := createUserMessage(t, database, session.ID, string(types.MessageStatusPending), 2)

	err := service.HandleSessionRunStarted(ctx, &contract.SessionRunStartedRequest{
		SessionID:         session.PublicID,
		ReplyToMessageIDs: []string{fmt.Sprintf("%d", first.ID), fmt.Sprintf("%d", second.ID)},
		StreamStartSeq:    123,
		StateStartSeq:     321,
	})
	if err != nil {
		t.Fatalf("HandleSessionRunStarted failed: %v", err)
	}

	for _, id := range []uint{first.ID, second.ID} {
		message, err := db.GetMessageByID(ctx, database, id)
		if err != nil {
			t.Fatalf("GetMessageByID failed: %v", err)
		}
		if message.Status != string(types.MessageStatusProcessing) {
			t.Fatalf("message %d status = %q, want processing", id, message.Status)
		}
		seq, ok := responseStreamStartSeq(message.Metadata)
		if !ok || seq != 123 {
			t.Fatalf("message %d response_stream_start_seq = %d/%v, want 123/true", id, seq, ok)
		}
	}
}

func TestCompleteSessionMessageStoresReplyIDsAndCompletesUsers(t *testing.T) {
	database := setupTestDB(t)
	service := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test")
	ctx := setupTestContextWithCaller(t)
	session := createTestSession(t, database, service, ctx)
	first := createUserMessage(t, database, session.ID, string(types.MessageStatusProcessing), 1)
	second := createUserMessage(t, database, session.ID, string(types.MessageStatusProcessing), 2)
	replyIDs := []string{fmt.Sprintf("%d", first.ID), fmt.Sprintf("%d", second.ID)}

	err := service.CompleteSessionMessage(ctx, &contract.CompleteSessionMessageRequest{
		SessionID:         session.PublicID,
		Content:           "done",
		ReplyToMessageIDs: replyIDs,
		CreatedAt:         time.Now(),
	})
	if err != nil {
		t.Fatalf("CompleteSessionMessage failed: %v", err)
	}

	for _, id := range []uint{first.ID, second.ID} {
		message, err := db.GetMessageByID(ctx, database, id)
		if err != nil {
			t.Fatalf("GetMessageByID failed: %v", err)
		}
		if message.Status != string(types.MessageStatusCompleted) {
			t.Fatalf("message %d status = %q, want completed", id, message.Status)
		}
	}
	latest, err := db.GetLatestMessage(ctx, database, session.ID)
	if err != nil {
		t.Fatalf("GetLatestMessage failed: %v", err)
	}
	rawIDs, ok := latest.Metadata.Extra[replyToMessageIDsKey].([]interface{})
	if !ok {
		t.Fatalf("assistant reply_to_message_ids = %#v, want JSON array", latest.Metadata.Extra[replyToMessageIDsKey])
	}
	got := make([]string, 0, len(rawIDs))
	for _, raw := range rawIDs {
		got = append(got, fmt.Sprint(raw))
	}
	if strings.Join(got, ",") != strings.Join(replyIDs, ",") {
		t.Fatalf("reply_to_message_ids = %v, want %v", got, replyIDs)
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

func TestAddMessage_TouchesProjectUpdatedAtForUserMessage(t *testing.T) {
	database := setupTestDB(t)
	service := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test")
	ctx := setupTestContextWithCaller(t)

	project := &types.Project{
		PublicID: "prj_test_add_message_touch",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Project Chat",
		Status:   string(types.ProjectStatusActive),
	}
	if err := db.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	_ = db.CreateProjectMember(ctx, database, &types.ProjectMember{ProjectID: project.ID, MemberID: 1, MemberType: types.MemberTypeUser, MemberRole: types.MemberRoleOwner})

	session := &types.Session{
		PublicID:    "sess_test_add_message_touch",
		Type:        types.SessionTypeProject,
		Uin:         1,
		OrgID:       1,
		AssistantID: 1,
		ProjectID:   &project.ID,
		Status:      string(types.SessionStatusActive),
		Title:       "项目协作",
	}
	if err := db.CreateSession(ctx, database, session); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	oldUpdatedAt := time.Now().Add(-time.Hour).UTC()
	if err := database.Model(&types.Project{}).
		Where("id = ?", project.ID).
		Update("updated_at", oldUpdatedAt).Error; err != nil {
		t.Fatalf("set old project updated_at: %v", err)
	}

	_, err := service.AddMessage(ctx, session.PublicID, &contract.AddMessageRequest{
		Role:    string(types.MessageRoleUser),
		Content: "项目里补充一条用户消息",
	})
	if err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}

	refreshedProject, err := db.GetProjectByID(ctx, database, project.ID)
	if err != nil {
		t.Fatalf("GetProjectByID failed: %v", err)
	}
	if refreshedProject == nil {
		t.Fatal("expected project to exist after AddMessage")
	}
	if !refreshedProject.UpdatedAt.After(oldUpdatedAt) {
		t.Fatalf("expected project updated_at after %v, got %v", oldUpdatedAt, refreshedProject.UpdatedAt)
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
	if retrieved.Title != "hello" {
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
		t.Errorf("expected title length 100, got %d", len([]rune(retrieved.Title)))
	}
}

func TestHandleSessionTitleRequest_CustomTitleUnchanged(t *testing.T) {
	service := setupTestService(t)
	ctx := setupTestContextWithCaller(t)

	session, err := service.CreateSession(ctx, &contract.CreateSessionRequest{
		Type:  string(types.SessionTypeUserChat),
		Title: "Manual title",
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
		t.Errorf("expected title %q, got %q", "Manual title", retrieved.Title)
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

func createTaskSessionWithFirstMessage(
	t *testing.T,
	database *gorm.DB,
	content string,
	projectName string,
	taskTitle string,
) *types.Session {
	t.Helper()
	ctx := context.Background()
	project := &types.Project{
		PublicID: "prj_test_work_title",
		OrgID:    1,
		OwnerID:  1,
		Name:     projectName,
		Status:   string(types.ProjectStatusActive),
	}
	if err := db.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	task := &types.Task{
		PublicID:  "task_test_work_title",
		OrgID:     1,
		OwnerID:   1,
		ProjectID: project.ID,
		TaskType:  types.TaskTypeGeneral,
		Title:     taskTitle,
		Status:    string(types.TaskStatusCreated),
	}
	if err := db.CreateTask(ctx, database, task); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	session := &types.Session{
		PublicID:     "sess_test_work_title",
		Type:         types.SessionTypeTask,
		Uin:          1,
		OrgID:        1,
		AssistantID:  1,
		ProjectID:    &project.ID,
		TaskID:       &task.ID,
		Status:       string(types.SessionStatusActive),
		Title:        taskTitle,
		MessageCount: 1,
	}
	if err := db.CreateSession(ctx, database, session); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	message := &types.SessionMessage{
		SessionID: session.ID,
		Role:      string(types.MessageRoleUser),
		Content:   content,
		Sequence:  1,
		Timestamp: time.Now().UnixMilli(),
		Status:    string(types.MessageStatusPending),
	}
	if err := db.CreateMessage(ctx, database, message); err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}
	return session
}

func TestHandleSessionTitleRequest_UpdatesWorkTitle(t *testing.T) {
	database := setupTestDB(t)
	service := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test")
	ctx := setupTestContextWithCaller(t)
	prompts.SetDefaultExecutor(workTitlePromptExecutor("生成的任务标题"))
	t.Cleanup(func() {
		prompts.SetDefaultExecutor(prompts.NewEinoExecutor())
	})

	content := "请帮我做一份季度经营分析报告"
	session := createTaskSessionWithFirstMessage(
		t,
		database,
		content,
		fallbackWorkTitle(content),
		fallbackWorkTitle(content),
	)

	if err := service.HandleSessionTitleRequest(ctx, session.PublicID); err != nil {
		t.Fatalf("HandleSessionTitleRequest failed: %v", err)
	}

	project, err := db.GetProjectByID(ctx, database, *session.ProjectID)
	if err != nil {
		t.Fatalf("GetProjectByID failed: %v", err)
	}
	if project.Name != "生成的任务标题" {
		t.Fatalf("expected project name %q, got %q", "生成的任务标题", project.Name)
	}

	var task types.Task
	if err := database.WithContext(ctx).First(&task, *session.TaskID).Error; err != nil {
		t.Fatalf("load task failed: %v", err)
	}
	if task.Title != "生成的任务标题" {
		t.Fatalf("expected task title %q, got %q", "生成的任务标题", task.Title)
	}
}

func TestHandleSessionTitleRequest_PublishesWorkTitleUpdatedStreamEvent(t *testing.T) {
	database := setupTestDB(t)
	bus := &recordingEventBus{}
	service := NewSessionService(database, bus, &mockInferrer{assistantID: 1}, nil, nil, "test")
	ctx := setupTestContextWithCaller(t)
	prompts.SetDefaultExecutor(workTitlePromptExecutor("生成的任务标题"))
	t.Cleanup(func() {
		prompts.SetDefaultExecutor(prompts.NewEinoExecutor())
	})

	content := "请帮我做一份季度经营分析报告"
	session := createTaskSessionWithFirstMessage(
		t,
		database,
		content,
		fallbackWorkTitle(content),
		fallbackWorkTitle(content),
	)

	if err := service.HandleSessionTitleRequest(ctx, session.PublicID); err != nil {
		t.Fatalf("HandleSessionTitleRequest failed: %v", err)
	}

	if bus.event == nil {
		t.Fatal("expected work title stream event to be published")
	}
	runEvent, ok := bus.event.(messaging.RunEvent)
	if !ok {
		t.Fatalf("expected RunEvent, got %T", bus.event)
	}
	if runEvent.Body.Event != messaging.RunEventWorkTitleUpdated {
		t.Fatalf("got stream event %q, want %q", runEvent.Body.Event, messaging.RunEventWorkTitleUpdated)
	}
	if runEvent.Body.Payload.WorkTitle == nil || runEvent.Body.Payload.WorkTitle.ProjectName != "生成的任务标题" {
		t.Fatalf("unexpected work title payload: %#v", runEvent.Body.Payload.WorkTitle)
	}
	expectedTopic, err := messaging.RunEventSubject(session.OrgID, session.PublicID, messaging.RunEventLaneState)
	if err != nil {
		t.Fatalf("RunEventSubject failed: %v", err)
	}
	if bus.topic != expectedTopic {
		t.Fatalf("got topic %q, want %q", bus.topic, expectedTopic)
	}
}

func TestHandleSessionTitleRequest_DoesNotOverwriteCustomWorkTitle(t *testing.T) {
	database := setupTestDB(t)
	service := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test")
	ctx := setupTestContextWithCaller(t)
	prompts.SetDefaultExecutor(workTitlePromptExecutor("生成的任务标题"))
	t.Cleanup(func() {
		prompts.SetDefaultExecutor(prompts.NewEinoExecutor())
	})

	content := "请帮我做一份季度经营分析报告"
	session := createTaskSessionWithFirstMessage(
		t,
		database,
		content,
		"自定义项目名",
		fallbackWorkTitle(content),
	)

	if err := service.HandleSessionTitleRequest(ctx, session.PublicID); err != nil {
		t.Fatalf("HandleSessionTitleRequest failed: %v", err)
	}

	project, err := db.GetProjectByID(ctx, database, *session.ProjectID)
	if err != nil {
		t.Fatalf("GetProjectByID failed: %v", err)
	}
	if project.Name != "自定义项目名" {
		t.Fatalf("expected project name unchanged, got %q", project.Name)
	}
}

type workTitlePromptExecutor string

func (e workTitlePromptExecutor) Execute(_ context.Context, _ string, _ config.LLMConfig) (string, error) {
	return string(e), nil
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

	payload, err := json.Marshal(recordedMessagePayload{
		MessageID: "msg_1",
		Role:      messaging.MessageRoleAssistant,
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
			TotalTokens:       999,
			InputTokens:       10,
			OutputTokens:      20,
			CacheInputTokens:  3,
			CacheOutputTokens: 2,
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
	if msg.Usage == nil || msg.Usage.TotalTokens != 30 || msg.Usage.InputTokens != 10 || msg.Usage.OutputTokens != 20 || msg.Usage.CacheInputTokens != 3 || msg.Usage.CacheOutputTokens != 2 {
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

func TestFailedSessionMessageStoresContentAndErrorMsgSeparately(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := database.AutoMigrate(
		&types.Session{},
		&types.SessionMessage{},
		&types.LLMModel{},
		&types.DigitalAssistant{},
		&types.WorkerDeployment{},
	); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	if err := database.Create(&types.LLMModel{
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
	if err := database.Create(&types.DigitalAssistant{
		Code:    "da-1",
		OrgID:   1,
		OwnerID: 1,
		Name:    "CodeReviewer",
		Status:  "active",
	}).Error; err != nil {
		t.Fatalf("failed to seed digital assistant: %v", err)
	}
	if err := database.Create(&types.WorkerDeployment{
		OrgID:              1,
		DigitalAssistantID: 1,
		WorkerID:           1,
		DeploymentName:     "default",
		Status:             "active",
	}).Error; err != nil {
		t.Fatalf("failed to seed worker deployment: %v", err)
	}
	service := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test")
	ctx := setupTestContextWithCaller(t)

	session, err := service.CreateSession(ctx, &contract.CreateSessionRequest{
		Type: string(types.SessionTypeUserChat),
	})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = service.FailedSessionMessage(ctx, &contract.FailedSessionMessageRequest{
		SessionID: session.SessionID,
		Content:   "已取消",
		ErrorMsg:  "scan repo for reconciliation: context canceled",
		Status:    string(types.MessageStatusCancelled),
		CreatedAt: time.Now().UTC(),
		RunID:     "run-abc-123",
	})
	if err != nil {
		t.Fatalf("FailedSessionMessage failed: %v", err)
	}

	result, err := service.GetSessionMessages(ctx, session.SessionID, 1, 20)
	if err != nil {
		t.Fatalf("GetSessionMessages failed: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected one message, got %d", len(result.Items))
	}
	if result.Items[0].Content != "已取消" {
		t.Fatalf("response content = %q, want 已取消", result.Items[0].Content)
	}
	if result.Items[0].ErrorMsg != "scan repo for reconciliation: context canceled" {
		t.Fatalf("response error_msg = %q", result.Items[0].ErrorMsg)
	}
	if result.Items[0].SenderName != "CodeReviewer" {
		t.Fatalf("response sender_name = %q, want CodeReviewer", result.Items[0].SenderName)
	}
	if result.Items[0].RunID != "run-abc-123" {
		t.Fatalf("response run_id = %q, want run-abc-123", result.Items[0].RunID)
	}
}

func TestCompleteSessionMessageBindsExistingDeclaredArtifact(t *testing.T) {
	database := setupTestDB(t)
	service := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test")
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
	if err := database.Create(&types.ProjectMember{ProjectID: projectID, MemberID: 1, MemberType: types.MemberTypeUser, MemberRole: types.MemberRoleOwner}).Error; err != nil {
		t.Fatalf("create project member: %v", err)
	}
	// Create a FileUpload + ProjectFile to simulate an existing artifact.
	fileUpload := &types.FileUpload{
		PublicID:     "file_existing",
		OrgID:        1,
		OwnerID:      1,
		Filename:     "report.md",
		OriginalName: "report.md",
		MimeType:     "text/markdown",
		FileSize:     100,
		StorageURI:   "s3://bucket/projects/1/project_10/repo/docs/report.md",
		Purpose:      "artifact",
		Status:       "active",
	}
	if err := database.Create(fileUpload).Error; err != nil {
		t.Fatalf("create file upload: %v", err)
	}
	projectFile := &types.ProjectFile{
		FilePublicID: fileUpload.PublicID,
		OrgID:        1,
		ProjectID:    projectID,
		TaskID:       taskID,
		ResourceID:   fileUpload.ID,
		ResourceType: types.ProjectFileResourceTypeArtifact,
		Uin:          1,
	}
	if err := database.Create(projectFile).Error; err != nil {
		t.Fatalf("create project file: %v", err)
	}

	chunkPayload, err := json.Marshal(messaging.ArtifactPayload{
		ArtifactID:   fileUpload.PublicID,
		Title:        fileUpload.Filename,
		Filename:     fileUpload.Filename,
		MimeType:     fileUpload.MimeType,
		ArtifactType: string(types.ArtifactTypeFile),
	})
	if err != nil {
		t.Fatalf("marshal artifact chunk: %v", err)
	}
	messageArtifacts := []types.MessageArtifact{
		{ArtifactID: fileUpload.PublicID, Title: fileUpload.Filename, Filename: fileUpload.Filename, MimeType: fileUpload.MimeType, ArtifactType: string(types.ArtifactTypeFile)},
	}
	err = service.CompleteSessionMessage(ctx, &contract.CompleteSessionMessageRequest{
		SessionID: session.PublicID,
		Content:   "done",
		Artifacts: messageArtifacts,
		Chunks: []types.MessageChunk{
			{Seq: 1, Type: string(messaging.RunEventArtifactDeclared), Timestamp: 1779243000000, Payload: chunkPayload},
		},
	})
	if err != nil {
		t.Fatalf("CompleteSessionMessage failed: %v", err)
	}

	var projectFiles []types.ProjectFile
	if err := database.Where("resource_type = ?", types.ProjectFileResourceTypeArtifact).Find(&projectFiles).Error; err != nil {
		t.Fatalf("list project files: %v", err)
	}
	if len(projectFiles) != 1 {
		t.Fatalf("expected existing artifact to have 1 project file, got %d rows", len(projectFiles))
	}
	// 不再验证 artifact.message_id 绑定，artifact 通过 session_id 关联查询
	result, err := service.GetSessionMessages(ctx, session.PublicID, 1, 20)
	if err != nil {
		t.Fatalf("GetSessionMessages failed: %v", err)
	}
	if len(result.Items) != 1 ||
		len(result.Items[0].Artifacts) != 1 ||
		result.Items[0].Artifacts[0].ArtifactID != fileUpload.PublicID ||
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

	deltaPayload, err := json.Marshal(recordedMessagePayload{
		MessageID: "msg_1",
		Role:      messaging.MessageRoleAssistant,
		Content:   "done",
	})
	if err != nil {
		t.Fatalf("marshal delta payload: %v", err)
	}
	todoPayload, err := json.Marshal([]messaging.RuntimeTodoItem{
		{ID: "todo_1", Title: "Inspect code", Status: "completed"},
	})
	if err != nil {
		t.Fatalf("marshal todo payload: %v", err)
	}

	err = service.CompleteSessionMessage(ctx, &contract.CompleteSessionMessageRequest{
		SessionID: session.SessionID,
		Content:   "done",
		Chunks: []types.MessageChunk{
			{Seq: 1, Type: string(messaging.RunEventMessageDelta), Timestamp: 1779243000000, Payload: deltaPayload},
			{Seq: 2, Type: string(messaging.RunEventTodoSnapshot), Timestamp: 1779243000001, Payload: todoPayload},
			{Seq: 3, Type: string(messaging.RunEventTodoUpdated), Timestamp: 1779243000002, Payload: todoPayload},
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
	if chunks[0].Type != string(messaging.RunEventMessageDelta) || chunks[0].Sequence != 1 {
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

	err := service.StreamSessionEvents(ctx, "test_session", false, 0, nil)
	if err == nil {
		t.Error("expected error when caller is not authenticated")
	}

	if err.Error() != "user not authenticated or org not set" {
		t.Errorf("expected 'user not authenticated or org not set' error, got %s", err.Error())
	}
}

func TestStreamSessionEventsReplayUsesProcessingMessageStartSeqAndFiltersReplies(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	sessionService := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test")
	session := createTestSession(t, database, sessionService, ctx)
	reply := createUserMessage(t, database, session.ID, string(types.MessageStatusProcessing), 1)
	other := createUserMessage(t, database, session.ID, string(types.MessageStatusProcessing), 2)
	setResponseStreamStartSeq(&reply.Metadata, 50)
	setResponseStreamStartSeq(&other.Metadata, 70)
	if err := database.Save(reply).Error; err != nil {
		t.Fatalf("save reply failed: %v", err)
	}
	if err := database.Save(other).Error; err != nil {
		t.Fatalf("save other failed: %v", err)
	}

	matching := messaging.RunEvent{
		ID:        "evt-match",
		Type:      messaging.MessageTypeRunEvent,
		CreatedAt: time.Now().UTC(),
		Route:     messaging.RouteContext{SessionID: session.PublicID},
		Body: messaging.RunEventBody{
			Seq:               1,
			Event:             messaging.RunEventMessageDelta,
			ReplyToMessageIDs: []string{fmt.Sprintf("%d", reply.ID)},
			Payload: messaging.RunEventPayload{
				Content: "match",
			},
		},
	}
	nonMatching := messaging.RunEvent{
		ID:        "evt-skip",
		Type:      messaging.MessageTypeRunEvent,
		CreatedAt: time.Now().UTC(),
		Route:     messaging.RouteContext{SessionID: session.PublicID},
		Body: messaging.RunEventBody{
			Seq:               2,
			Event:             messaging.RunEventMessageDelta,
			ReplyToMessageIDs: []string{"999999"},
			Payload: messaging.RunEventPayload{
				Content: "skip",
			},
		},
	}
	bus := &replayEventBus{messages: []*nats.Msg{
		mustStreamNATSMessage(t, nonMatching),
		mustStreamNATSMessage(t, matching),
	}}
	service := NewSessionService(database, bus, &mockInferrer{assistantID: 1}, nil, nil, "test")
	var emitted []string
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	err := service.StreamSessionEvents(streamCtx, session.PublicID, true, 0, contract.SessionEventSinkFunc(func(
		ctx context.Context,
		event *contract.SessionEvent,
	) error {
		data, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return marshalErr
		}
		emitted = append(emitted, string(data))
		if strings.Contains(string(data), "match") {
			cancel()
		}
		return nil
	}))
	if err != nil {
		t.Fatalf("StreamSessionEvents failed: %v", err)
	}
	if bus.startSeq != 50 {
		t.Fatalf("SubscribeFrom startSeq = %d, want 50", bus.startSeq)
	}
	if len(emitted) != 1 || !strings.Contains(emitted[0], "match") {
		t.Fatalf("emitted = %v, want only matching event", emitted)
	}
}

func mustStreamNATSMessage(t *testing.T, msg messaging.RunEvent) *nats.Msg {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal stream message: %v", err)
	}
	return &nats.Msg{Data: data}
}

// TestStreamSessionEventsFiltersByAssistantID verifies that when assistantID > 0,
// the service resolves DigitalAssistant.ID → WorkerDeployment.WorkerID and only
// delivers RunEvents whose Route.WorkerID matches; assistantID == 0 disables
// filtering (back-compat). The test seeds DISTINCT DigitalAssistant.ID and
// WorkerID values to catch the bug where they were compared directly.
func TestStreamSessionEventsFiltersByAssistantID(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	sessionService := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test")
	session := createTestSession(t, database, sessionService, ctx)

	// Seed WorkerDeployments with DISTINCT DigitalAssistant.ID and WorkerID
	// to verify the filter resolves DigitalAssistant.ID → WorkerDeployment.WorkerID
	// instead of comparing them directly.
	//   Assistant 100 → WorkerID 1000
	//   Assistant 200 → WorkerID 2000
	if err := db.CreateWorkerDeployment(ctx, database, &types.WorkerDeployment{
		OrgID: 1, DigitalAssistantID: 100, WorkerID: 1000,
		DeploymentName: "dep-100", Status: string(types.WorkerDeploymentStatusReady),
	}); err != nil {
		t.Fatalf("seed worker deployment 100: %v", err)
	}
	if err := db.CreateWorkerDeployment(ctx, database, &types.WorkerDeployment{
		OrgID: 1, DigitalAssistantID: 200, WorkerID: 2000,
		DeploymentName: "dep-200", Status: string(types.WorkerDeploymentStatusReady),
	}); err != nil {
		t.Fatalf("seed worker deployment 200: %v", err)
	}

	// RunEvent produced by the AI teammate bound to WorkerID=1000 (DigitalAssistant.ID=100).
	workerEvent := messaging.RunEvent{
		ID:        "evt-worker-1000",
		Type:      messaging.MessageTypeRunEvent,
		CreatedAt: time.Now().UTC(),
		Route:     messaging.RouteContext{SessionID: session.PublicID, WorkerID: 1000},
		Body: messaging.RunEventBody{
			Seq:   1,
			Event: messaging.RunEventMessageDelta,
			Payload: messaging.RunEventPayload{
				Content: "from-assistant-100",
			},
		},
	}

	// assistantID=100 (DigitalAssistant.ID) resolves to WorkerID=1000 → matches → event delivered.
	t.Run("matching assistant receives event", func(t *testing.T) {
		bus := &replayEventBus{messages: []*nats.Msg{mustStreamNATSMessage(t, workerEvent)}}
		service := NewSessionService(database, bus, &mockInferrer{assistantID: 1}, nil, nil, "test")

		var emitted []string
		streamCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		err := service.StreamSessionEvents(streamCtx, session.PublicID, false, 100, contract.SessionEventSinkFunc(func(ctx context.Context, event *contract.SessionEvent) error {
			data, _ := json.Marshal(event)
			emitted = append(emitted, string(data))
			cancel()
			return nil
		}))
		if err != nil {
			t.Fatalf("StreamSessionEvents (matching) failed: %v", err)
		}
		if len(emitted) != 1 || !strings.Contains(emitted[0], "from-assistant-100") {
			t.Fatalf("matching assistantID: emitted = %v, want exactly one event from assistant 100", emitted)
		}
	})

	// assistantID=200 (DigitalAssistant.ID) resolves to WorkerID=2000 ≠ 1000 → event filtered out.
	t.Run("non-matching assistant receives no event", func(t *testing.T) {
		bus := &replayEventBus{messages: []*nats.Msg{mustStreamNATSMessage(t, workerEvent)}}
		service := NewSessionService(database, bus, &mockInferrer{assistantID: 1}, nil, nil, "test")

		var emitted []string
		streamCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()
		_ = service.StreamSessionEvents(streamCtx, session.PublicID, false, 200, contract.SessionEventSinkFunc(func(ctx context.Context, event *contract.SessionEvent) error {
			data, _ := json.Marshal(event)
			emitted = append(emitted, string(data))
			return nil
		}))
		if len(emitted) != 0 {
			t.Fatalf("non-matching assistantID: emitted = %v, want no events", emitted)
		}
	})

	// assistantID=0 (disabled) → event delivered regardless of WorkerID.
	t.Run("zero assistant receives event unfiltered", func(t *testing.T) {
		bus := &replayEventBus{messages: []*nats.Msg{mustStreamNATSMessage(t, workerEvent)}}
		service := NewSessionService(database, bus, &mockInferrer{assistantID: 1}, nil, nil, "test")

		var emitted []string
		streamCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		err := service.StreamSessionEvents(streamCtx, session.PublicID, false, 0, contract.SessionEventSinkFunc(func(ctx context.Context, event *contract.SessionEvent) error {
			data, _ := json.Marshal(event)
			emitted = append(emitted, string(data))
			cancel()
			return nil
		}))
		if err != nil {
			t.Fatalf("StreamSessionEvents (unfiltered) failed: %v", err)
		}
		if len(emitted) != 1 {
			t.Fatalf("zero assistantID: emitted = %v, want exactly one event", emitted)
		}
	})
}

func TestGetSessionForCallerAllowsProjectMemberForTaskSession(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()
	proj := &types.Project{PublicID: "prj_g1", OrgID: 1, OwnerID: 1, Name: "P", Status: string(types.ProjectStatusActive)}
	if err := db.CreateProject(ctx, database, proj); err != nil {
		t.Fatalf("create project: %v", err)
	}
	_ = db.CreateProjectMember(ctx, database, &types.ProjectMember{ProjectID: proj.ID, MemberID: 2, MemberType: types.MemberTypeUser, MemberRole: types.MemberRoleMember})
	pid := proj.ID
	sess := &types.Session{PublicID: "sess_task1", Type: types.SessionTypeTask, Uin: 1, OrgID: 1, ProjectID: &pid, Status: string(types.SessionStatusActive)}
	if err := db.CreateSession(ctx, database, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	ss := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test").(*sessionService)
	memberCtx := auth.WithContext(ctx, &types.Caller{Uin: 2, OrgID: 1, Kind: types.CallerKindUser}, nil)
	if _, _, err := ss.getSessionForCaller(memberCtx, "sess_task1"); err != nil {
		t.Fatalf("project member should access task session: %v", err)
	}
}

func TestGetSessionForCallerDeniesNonMemberForTaskSession(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()
	proj := &types.Project{PublicID: "prj_g2", OrgID: 1, OwnerID: 1, Name: "P", Status: string(types.ProjectStatusActive)}
	_ = db.CreateProject(ctx, database, proj)
	pid := proj.ID
	sess := &types.Session{PublicID: "sess_task2", Type: types.SessionTypeTask, Uin: 1, OrgID: 1, ProjectID: &pid, Status: string(types.SessionStatusActive)}
	_ = db.CreateSession(ctx, database, sess)
	ss := NewSessionService(database, &mockEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test").(*sessionService)
	strangerCtx := auth.WithContext(ctx, &types.Caller{Uin: 99, OrgID: 1, Kind: types.CallerKindUser}, nil)
	if _, _, err := ss.getSessionForCaller(strangerCtx, "sess_task2"); err == nil {
		t.Fatal("non-member should be denied")
	}
}

func TestPublishWorkerTaskHistoryContext(t *testing.T) {
	database := setupTestDB(t)
	bus := &recordingEventBus{}
	poster := NewMessagePoster(database, bus, &mockInferrer{assistantID: 1}, nil, nil, "test", nil)
	ctx := setupTestContextWithCaller(t)

	proj := &types.Project{
		PublicID: "prj_hist",
		OrgID:    1,
		OwnerID:  1,
		Name:     "HistoryProject",
		Status:   string(types.ProjectStatusActive),
	}
	if err := db.CreateProject(ctx, database, proj); err != nil {
		t.Fatalf("create project: %v", err)
	}

	session := &types.Session{
		PublicID:             "sess_hist",
		Type:                 types.SessionTypeTask,
		Uin:                  1,
		OrgID:                1,
		AssistantID:          1,
		AllocatedAssistantID: 1,
		ProjectID:            &proj.ID,
		Status:               string(types.SessionStatusActive),
		Title:                "history test",
	}
	if err := db.CreateSession(ctx, database, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	histUser := &types.SessionMessage{
		SessionID:   session.ID,
		Role:        string(types.MessageRoleUser),
		Content:     "历史用户提问",
		MessageType: string(types.MessageTypeText),
		Status:      string(types.MessageStatusCompleted),
		Sequence:    1,
		SenderName:  "张三",
		Timestamp:   time.Now().UnixMilli(),
	}
	if err := db.CreateMessage(ctx, database, histUser); err != nil {
		t.Fatalf("create history user message: %v", err)
	}
	histAssistant := &types.SessionMessage{
		SessionID:   session.ID,
		Role:        string(types.MessageRoleAssistant),
		Content:     "历史AI回复",
		MessageType: string(types.MessageTypeText),
		Status:      string(types.MessageStatusCompleted),
		Sequence:    2,
		SenderName:  "AI助手",
		Timestamp:   time.Now().UnixMilli(),
	}
	if err := db.CreateMessage(ctx, database, histAssistant); err != nil {
		t.Fatalf("create history assistant message: %v", err)
	}

	message := &types.SessionMessage{
		SessionID:   session.ID,
		Role:        string(types.MessageRoleUser),
		Content:     "这是当前消息",
		MessageType: string(types.MessageTypeText),
		Status:      string(types.MessageStatusPending),
		Sequence:    3,
		SenderName:  "李四",
		Timestamp:   time.Now().UnixMilli(),
	}
	if err := db.CreateMessage(ctx, database, message); err != nil {
		t.Fatalf("create current message: %v", err)
	}

	if err := poster.publishWorkerTask(ctx, session, message, types.ExecutionModeDefault); err != nil {
		t.Fatalf("publishWorkerTask failed: %v", err)
	}

	cmd, ok := bus.event.(messaging.WorkerCommand)
	if !ok {
		t.Fatalf("expected WorkerCommand, got %T", bus.event)
	}
	payload, err := messaging.DecodeCommandPayload[messaging.RunCommandPayload](&cmd.Body)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	messages := payload.Input.Messages
	if len(messages) != 3 {
		t.Fatalf("expected 3 input messages (2 history + 1 current), got %d: %+v", len(messages), messages)
	}

	if messages[0].SenderName != "张三" {
		t.Errorf("history[0] sender_name = %q, want %q", messages[0].SenderName, "张三")
	}
	if messages[0].Role != messaging.MessageRole(types.MessageRoleUser) {
		t.Errorf("history[0] role = %q, want %q", messages[0].Role, types.MessageRoleUser)
	}
	if messages[1].SenderName != "AI助手" {
		t.Errorf("history[1] sender_name = %q, want %q", messages[1].SenderName, "AI助手")
	}
	if messages[1].Role != messaging.MessageRole(types.MessageRoleAssistant) {
		t.Errorf("history[1] role = %q, want %q", messages[1].Role, types.MessageRoleAssistant)
	}

	if messages[2].SenderName != "李四" {
		t.Errorf("current sender_name = %q, want %q", messages[2].SenderName, "李四")
	}
	if messages[2].Content != "这是当前消息" {
		t.Errorf("current content = %q, want %q", messages[2].Content, "这是当前消息")
	}
}

func TestConvertToContractSessionMessageAlwaysIncludesNormalizedUsage(t *testing.T) {
	msg := &types.SessionMessage{
		Role:    string(types.MessageRoleAssistant),
		Content: "done",
		Usage: types.MessageUsage{
			TotalTokens:       999,
			InputTokens:       7,
			OutputTokens:      5,
			CacheInputTokens:  3,
			CacheOutputTokens: 2,
		},
	}

	converted := convertToContractSessionMessage(msg, "sess_1")
	if converted.Usage == nil {
		t.Fatalf("expected usage object")
	}
	if converted.Usage.TotalTokens != 12 || converted.Usage.InputTokens != 7 || converted.Usage.OutputTokens != 5 ||
		converted.Usage.CacheInputTokens != 3 || converted.Usage.CacheOutputTokens != 2 {
		t.Fatalf("unexpected normalized usage: %#v", converted.Usage)
	}

	converted = convertToContractSessionMessage(&types.SessionMessage{}, "sess_1")
	if converted.Usage == nil {
		t.Fatalf("expected zero usage object")
	}
	if converted.Usage.TotalTokens != 0 || converted.Usage.InputTokens != 0 || converted.Usage.OutputTokens != 0 ||
		converted.Usage.CacheInputTokens != 0 || converted.Usage.CacheOutputTokens != 0 {
		t.Fatalf("unexpected zero usage: %#v", converted.Usage)
	}
}

func TestCompleteSessionMessageDoesNotPublishAssistantEvent(t *testing.T) {
	database := setupTestDB(t)
	recorder := &publishRecorder{}
	service := NewSessionService(database, recorder, &mockInferrer{assistantID: 1}, nil, nil, "test")
	ctx := setupTestContextWithCaller(t)

	assistant := &types.DigitalAssistant{OrgID: 1, Name: "Beta"}
	if err := database.Create(assistant).Error; err != nil {
		t.Fatalf("seed assistant: %v", err)
	}

	project := &types.Project{
		PublicID: "prj_complete_no_publish",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Complete No Publish",
		Status:   string(types.ProjectStatusActive),
	}
	if err := db.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	session := &types.Session{
		PublicID:    "sess_complete_no_publish",
		Type:        types.SessionTypeProject,
		Uin:         1,
		OrgID:       1,
		AssistantID: assistant.ID,
		ProjectID:   &project.ID,
		Status:      string(types.SessionStatusActive),
	}
	if err := db.CreateSession(ctx, database, session); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err := service.CompleteSessionMessage(ctx, &contract.CompleteSessionMessageRequest{
		SessionID: session.PublicID,
		Content:   "done",
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CompleteSessionMessage failed: %v", err)
	}

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for _, e := range recorder.events {
		payload, ok := e.event.(messaging.GlobalEventPayload)
		if !ok {
			continue
		}
		var data messaging.MessageCreatedData
		if err := json.Unmarshal(payload.Data, &data); err != nil {
			continue
		}
		if data.SenderType == messaging.SenderTypeAssistant {
			t.Fatalf("CompleteSessionMessage should not publish assistant event, got topic=%s type=%q sender_type=%q",
				e.topic, payload.Type, data.SenderType)
		}
	}
}

func TestPublishMessageCreatedEvent_HumanFullContent(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)

	project := &types.Project{
		PublicID: "prj_publish_full_content",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Full Content Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := db.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	session := &types.Session{
		PublicID:  "sess_publish_full",
		Type:      types.SessionTypeProject,
		Uin:       1,
		OrgID:     1,
		ProjectID: &project.ID,
		Status:    string(types.SessionStatusActive),
	}
	if err := db.CreateSession(ctx, database, session); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	longContent := strings.Repeat("一二三四五", 30) // 150 runes, over old 100 limit
	message := &types.SessionMessage{
		SessionID: session.ID,
		Role:      string(types.MessageRoleUser),
		Content:   longContent,
		SenderUin: uintPtr(1),
		Sequence:  1,
	}

	recorder := &publishRecorder{}
	publishMessageCreatedEvent(ctx, database, recorder, session, message)

	last, ok := recorder.lastEvent()
	if !ok {
		t.Fatal("expected one publish event, got none")
	}
	payload, ok := last.event.(messaging.GlobalEventPayload)
	if !ok {
		t.Fatalf("event type = %T, want messaging.GlobalEventPayload", last.event)
	}
	var data messaging.MessageCreatedData
	if err := json.Unmarshal(payload.Data, &data); err != nil {
		t.Fatalf("unmarshal payload data: %v", err)
	}
	if data.SenderType != messaging.SenderTypeHuman {
		t.Fatalf("sender_type = %q, want %q", data.SenderType, messaging.SenderTypeHuman)
	}
	if data.Content != longContent {
		t.Fatalf("content truncated: got len=%d, want len=%d", len([]rune(data.Content)), len([]rune(longContent)))
	}
}

func TestPublishMessageCreatedEvent_HumanWithMetadata(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)

	project := &types.Project{
		PublicID: "prj_publish_meta",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Metadata Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := db.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	session := &types.Session{
		PublicID:  "sess_publish_meta",
		Type:      types.SessionTypeProject,
		Uin:       1,
		OrgID:     1,
		ProjectID: &project.ID,
		Status:    string(types.SessionStatusActive),
	}
	if err := db.CreateSession(ctx, database, session); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	t.Run("metadata with extra should be included", func(t *testing.T) {
		message := &types.SessionMessage{
			SessionID: session.ID,
			Role:      string(types.MessageRoleUser),
			Content:   "hello with metadata",
			SenderUin: uintPtr(1),
			Sequence:  1,
			Metadata: types.ObjectMetadata{
				Extra: map[string]interface{}{
					"composerTokens": []map[string]interface{}{
						{"kind": "skill", "label": "/tech-design-proposal", "start": 0, "end": 21},
					},
				},
			},
		}

		recorder := &publishRecorder{}
		publishMessageCreatedEvent(ctx, database, recorder, session, message)

		last, ok := recorder.lastEvent()
		if !ok {
			t.Fatal("expected one publish event, got none")
		}
		payload, ok := last.event.(messaging.GlobalEventPayload)
		if !ok {
			t.Fatalf("event type = %T, want messaging.GlobalEventPayload", last.event)
		}
		var data messaging.MessageCreatedData
		if err := json.Unmarshal(payload.Data, &data); err != nil {
			t.Fatalf("unmarshal payload data: %v", err)
		}
		if data.Metadata == nil {
			t.Fatal("data.Metadata is nil, expected non-nil when Extra is set")
		}
		if data.Metadata.Extra == nil {
			t.Fatal("data.Metadata.Extra is nil, expected composerTokens")
		}
		tokens, ok := data.Metadata.Extra["composerTokens"].([]interface{})
		if !ok || len(tokens) == 0 {
			t.Fatalf("data.Metadata.Extra missing composerTokens, got: %+v", data.Metadata.Extra)
		}
	})

	t.Run("metadata with tags should be included", func(t *testing.T) {
		message := &types.SessionMessage{
			SessionID: session.ID,
			Role:      string(types.MessageRoleUser),
			Content:   "hello with tags",
			SenderUin: uintPtr(1),
			Sequence:  2,
			Metadata: types.ObjectMetadata{
				Tags: []string{"important", "question"},
				Type: "support",
			},
		}

		recorder := &publishRecorder{}
		publishMessageCreatedEvent(ctx, database, recorder, session, message)

		last, ok := recorder.lastEvent()
		if !ok {
			t.Fatal("expected one publish event, got none")
		}
		payload, ok := last.event.(messaging.GlobalEventPayload)
		if !ok {
			t.Fatalf("event type = %T, want messaging.GlobalEventPayload", last.event)
		}
		var data messaging.MessageCreatedData
		if err := json.Unmarshal(payload.Data, &data); err != nil {
			t.Fatalf("unmarshal payload data: %v", err)
		}
		if data.Metadata == nil {
			t.Fatal("data.Metadata is nil, expected non-nil when Tags/Type are set")
		}
		if len(data.Metadata.Tags) != 2 || data.Metadata.Tags[0] != "important" {
			t.Fatalf("unexpected tags: %v", data.Metadata.Tags)
		}
		if data.Metadata.Type != "support" {
			t.Fatalf("unexpected type: %q", data.Metadata.Type)
		}
	})

	t.Run("zero metadata should be omitted", func(t *testing.T) {
		message := &types.SessionMessage{
			SessionID: session.ID,
			Role:      string(types.MessageRoleUser),
			Content:   "hello without metadata",
			SenderUin: uintPtr(1),
			Sequence:  3,
		}

		recorder := &publishRecorder{}
		publishMessageCreatedEvent(ctx, database, recorder, session, message)

		last, ok := recorder.lastEvent()
		if !ok {
			t.Fatal("expected one publish event, got none")
		}
		payload, ok := last.event.(messaging.GlobalEventPayload)
		if !ok {
			t.Fatalf("event type = %T, want messaging.GlobalEventPayload", last.event)
		}
		var data messaging.MessageCreatedData
		if err := json.Unmarshal(payload.Data, &data); err != nil {
			t.Fatalf("unmarshal payload data: %v", err)
		}
		if data.Metadata != nil {
			t.Fatal("data.Metadata should be nil (omitempty) for zero-valued ObjectMetadata")
		}
	})
}

func uintPtr(v uint) *uint {
	return &v
}

func TestHandleSessionRunStartedPublishesAssistantReplyStarted(t *testing.T) {
	database := setupTestDB(t)
	recorder := &publishRecorder{}
	service := NewSessionService(database, recorder, &mockInferrer{assistantID: 1}, nil, nil, "test")
	ctx := setupTestContextWithCaller(t)

	// seed DigitalAssistant so AssistantName can be resolved
	assistant := &types.DigitalAssistant{
		OrgID: 1,
		Name:  "Alpha",
	}
	if err := database.Create(assistant).Error; err != nil {
		t.Fatalf("seed assistant: %v", err)
	}

	project := &types.Project{
		PublicID: "prj_run_started_publish",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Run Started Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := db.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	session := &types.Session{
		PublicID:    "sess_run_started_publish",
		Type:        types.SessionTypeProject,
		Uin:         1,
		OrgID:       1,
		AssistantID: assistant.ID,
		ProjectID:   &project.ID,
		Status:      string(types.SessionStatusActive),
	}
	if err := db.CreateSession(ctx, database, session); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	userMsg := createUserMessage(t, database, session.ID, string(types.MessageStatusPending), 1)

	err := service.HandleSessionRunStarted(ctx, &contract.SessionRunStartedRequest{
		SessionID:         session.PublicID,
		ReplyToMessageIDs: []string{fmt.Sprintf("%d", userMsg.ID)},
		RequestID:         "test-req",
		StateStartSeq:     500,
		RunID:             "run-abc-123",
	})
	if err != nil {
		t.Fatalf("HandleSessionRunStarted failed: %v", err)
	}

	last, ok := recorder.lastEvent()
	if !ok {
		t.Fatal("expected assistant reply_started publish, got none")
	}
	if !strings.Contains(last.topic, ".notify") {
		t.Fatalf("topic = %q, want contains .notify", last.topic)
	}
	payload, ok := last.event.(messaging.GlobalEventPayload)
	if !ok {
		t.Fatalf("event type = %T, want messaging.GlobalEventPayload", last.event)
	}
	if payload.Type != messaging.GlobalEventMessageCreated {
		t.Fatalf("event type = %q, want %q", payload.Type, messaging.GlobalEventMessageCreated)
	}
	var data messaging.MessageCreatedData
	if err := json.Unmarshal(payload.Data, &data); err != nil {
		t.Fatalf("unmarshal payload data: %v", err)
	}
	if data.SenderType != messaging.SenderTypeAssistant {
		t.Fatalf("sender_type = %q, want %q", data.SenderType, messaging.SenderTypeAssistant)
	}
	if data.Content != "" {
		t.Fatalf("content = %q, want empty (T1 has no message content)", data.Content)
	}
	if data.RunID != "run-abc-123" {
		t.Fatalf("run_id = %q, want %q", data.RunID, "run-abc-123")
	}
	if data.AssistantName != "Alpha" {
		t.Fatalf("assistant_name = %q, want %q", data.AssistantName, "Alpha")
	}
	if data.AssistantID == nil || *data.AssistantID != assistant.ID {
		t.Fatalf("assistant_id = %v, want %d", data.AssistantID, assistant.ID)
	}
}
