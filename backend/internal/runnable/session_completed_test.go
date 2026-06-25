package runnable

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/insmtx/Leros/backend/types"
	"github.com/nats-io/nats.go"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestHandleSessionCompletedMessageUsesRunCompletedPayload(t *testing.T) {
	service := &recordingSessionService{}
	createdAt := time.Now().UTC()
	streamMsg := protocol.MessageStreamMessage{
		CreatedAt: createdAt,
		Route: protocol.RouteContext{
			SessionID: "sess_test",
		},
		Body: protocol.StreamBody{
			Seq:   9,
			Event: protocol.StreamEventRunCompleted,
			RunCompleted: &events.RunCompletedPayload{
				Result: events.RunResultPayload{Message: "done"},
				Usage: &events.UsagePayload{
					InputTokens:  11,
					OutputTokens: 22,
					TotalTokens:  33,
				},
				Artifacts: []events.ArtifactPayload{
					{ArtifactID: "art_test", Title: "Report", Filename: "report.md", MimeType: "text/markdown", ArtifactType: "file"},
				},
				Events: []events.RunEventRecord{
					{
						Seq:       1,
						Type:      events.EventMessageDelta,
						Timestamp: 1779243000000,
					},
				},
			},
		},
	}
	body, err := json.Marshal(streamMsg)
	if err != nil {
		t.Fatalf("marshal stream message: %v", err)
	}

	handleSessionCompletedMessage(context.Background(), service, &nats.Msg{Data: body})

	if service.completeReq == nil {
		t.Fatal("expected CompleteSessionMessage to be called")
	}
	if service.completeReq.SessionID != "sess_test" || service.completeReq.Content != "done" || service.completeReq.Seq != 9 {
		t.Fatalf("unexpected complete request: %#v", service.completeReq)
	}
	if len(service.completeReq.Chunks) != 1 {
		t.Fatalf("expected one chunk, got %#v", service.completeReq.Chunks)
	}
	if service.completeReq.Chunks[0].Seq != 1 || service.completeReq.Chunks[0].Type != string(events.EventMessageDelta) ||
		service.completeReq.Chunks[0].Timestamp != 1779243000000 {
		t.Fatalf("unexpected chunk: %#v", service.completeReq.Chunks[0])
	}
	if service.completeReq.Usage == nil || service.completeReq.Usage.TotalTokens != 33 {
		t.Fatalf("expected usage to be forwarded, got %#v", service.completeReq.Usage)
	}
	if len(service.completeReq.Artifacts) != 1 ||
		service.completeReq.Artifacts[0].ArtifactID != "art_test" ||
		service.completeReq.Artifacts[0].Filename != "report.md" ||
		service.completeReq.Artifacts[0].MimeType != "text/markdown" {
		t.Fatalf("expected artifacts to be forwarded, got %#v", service.completeReq.Artifacts)
	}
}

func TestHandleSessionCompletedMessageRequiresRunCompletedPayload(t *testing.T) {
	service := &recordingSessionService{}
	streamMsg := protocol.MessageStreamMessage{
		CreatedAt: time.Now().UTC(),
		Route:     protocol.RouteContext{SessionID: "sess_test"},
		Body: protocol.StreamBody{
			Seq:   9,
			Event: protocol.StreamEventRunCompleted,
		},
	}
	body, err := json.Marshal(streamMsg)
	if err != nil {
		t.Fatalf("marshal stream message: %v", err)
	}

	handleSessionCompletedMessage(context.Background(), service, &nats.Msg{Data: body})

	if service.completeReq != nil {
		t.Fatalf("expected no complete request, got %#v", service.completeReq)
	}
}

func TestHandleSessionCompletedMessageProjectsRunArtifacts(t *testing.T) {
	service := &recordingSessionService{}
	streamMsg := protocol.MessageStreamMessage{
		CreatedAt: time.Now().UTC(),
		Route:     protocol.RouteContext{SessionID: "sess_test"},
		Body: protocol.StreamBody{
			Seq:   9,
			Event: protocol.StreamEventRunCompleted,
			RunCompleted: &events.RunCompletedPayload{
				Result: events.RunResultPayload{Message: "done"},
				Artifacts: []events.ArtifactPayload{
					{
						ArtifactID:   "art_worker",
						Title:        "Report",
						Filename:     "report.md",
						Description:  "final report",
						MimeType:     "text/markdown",
						ArtifactType: "file",
						StorageKey:   "projects/1/prj/repo/report.md",
					},
				},
				Events: []events.RunEventRecord{
					{
						Seq:  2,
						Type: events.EventArtifactDeclared,
					},
				},
			},
		},
	}
	body, err := json.Marshal(streamMsg)
	if err != nil {
		t.Fatalf("marshal stream message: %v", err)
	}

	handleSessionCompletedMessage(context.Background(), service, &nats.Msg{Data: body})

	if service.completeReq == nil {
		t.Fatal("expected CompleteSessionMessage to be called")
	}
	if len(service.completeReq.Artifacts) != 1 || service.completeReq.Artifacts[0].ArtifactID != "art_worker" {
		t.Fatalf("expected projected artifact ref, got %#v", service.completeReq.Artifacts)
	}
	if len(service.completeReq.Chunks) != 1 {
		t.Fatalf("expected artifact chunk, got %#v", service.completeReq.Chunks)
	}
	var chunkPayload events.ArtifactPayload
	if err := json.Unmarshal(service.completeReq.Chunks[0].Payload, &chunkPayload); err != nil {
		t.Fatalf("decode artifact chunk: %v", err)
	}
	if chunkPayload.ArtifactID != "art_worker" || chunkPayload.StorageKey != "" || chunkPayload.Description != "" {
		t.Fatalf("expected public artifact payload in chunk, got %#v", chunkPayload)
	}
}

func TestHandleSessionArtifactDeclaredMessagePersistsArtifactFromWorkerStorage(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.Session{}, &types.Artifact{}, &types.FileUpload{}, &types.Project{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	projectID := uint(101)
	taskID := uint(202)
	project := &types.Project{
		Model:    gorm.Model{ID: projectID},
		PublicID: "prj_test",
		OrgID:    7,
		OwnerID:  9,
		Name:     "test-project",
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	session := &types.Session{
		PublicID:  "sess_test",
		OrgID:     7,
		Uin:       9,
		ProjectID: &projectID,
		TaskID:    &taskID,
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	storageKey := "artifacts/report.md"
	streamMsg := protocol.MessageStreamMessage{
		CreatedAt: time.Now().UTC(),
		Route: protocol.RouteContext{
			OrgID:     7,
			SessionID: "sess_test",
			WorkerID:  3,
		},
		Body: protocol.StreamBody{
			Seq:   2,
			Event: protocol.StreamEventArtifactDeclared,
			Payload: protocol.StreamPayload{
				Artifact: &events.ArtifactPayload{
					ArtifactID:   "art_worker",
					Title:        "Report",
					Filename:     "report.md",
					Description:  "final report",
					MimeType:     "text/markdown",
					ArtifactType: "file",
					StorageKey:   storageKey,
					FileSize:     100,
					Sha256:       "abc123",
				},
			},
		},
	}
	body, err := json.Marshal(streamMsg)
	if err != nil {
		t.Fatalf("marshal stream message: %v", err)
	}

	handleSessionArtifactDeclaredMessage(context.Background(), &declaredArtifactPersister{db: database}, &nats.Msg{Data: body})

	var artifact types.Artifact
	if err := database.Where("public_id = ?", "art_worker").First(&artifact).Error; err != nil {
		t.Fatalf("load artifact: %v", err)
	}
	if artifact.OrgID != 7 || artifact.OwnerID != 9 || artifact.ProjectID != projectID || artifact.TaskID != taskID {
		t.Fatalf("artifact ownership not from session: %#v", artifact)
	}
	if artifact.SessionID == nil || *artifact.SessionID != session.ID {
		t.Fatalf("expected artifact session id %d, got %#v", session.ID, artifact.SessionID)
	}
	if artifact.StorageKey != storageKey {
		t.Fatalf("expected storage_key %q, got %q", storageKey, artifact.StorageKey)
	}
	if artifact.FileSize != 100 || artifact.Sha256 != "abc123" {
		t.Fatalf("unexpected artifact storage fields: %#v", artifact)
	}
	if artifact.FileURL == "" {
		t.Fatalf("expected FileURL to be set")
	}
	if artifact.Metadata.Extra["worker_id"] == nil {
		t.Fatalf("expected worker_id metadata, got %#v", artifact.Metadata)
	}
	if artifact.Metadata.Extra["project_public_id"] == nil {
		t.Fatalf("expected project_public_id metadata, got %#v", artifact.Metadata)
	}
}

func TestHandleSessionCompletedMessageUsesFailedRunCompletedPayload(t *testing.T) {
	service := &recordingSessionService{}
	createdAt := time.Now().UTC()
	streamMsg := protocol.MessageStreamMessage{
		CreatedAt: createdAt,
		Route: protocol.RouteContext{
			SessionID: "sess_test",
		},
		Body: protocol.StreamBody{
			Seq:   10,
			Event: protocol.StreamEventRunFailed,
			RunCompleted: &events.RunCompletedPayload{
				Status: "failed",
				Result: events.RunResultPayload{
					Message: "runtime unavailable",
				},
			},
			Error: &protocol.StreamError{
				Code:    "runtime_error",
				Message: "runtime unavailable",
			},
		},
	}
	body, err := json.Marshal(streamMsg)
	if err != nil {
		t.Fatalf("marshal stream message: %v", err)
	}

	handleSessionCompletedMessage(context.Background(), service, &nats.Msg{Data: body})

	if service.failedReq == nil {
		t.Fatal("expected FailedSessionMessage to be called")
	}
	if service.failedReq.SessionID != "sess_test" || service.failedReq.ErrorMsg != "runtime unavailable" || service.failedReq.Seq != 10 {
		t.Fatalf("unexpected failed request: %#v", service.failedReq)
	}
	if service.failedReq.ErrorCode != "runtime_error" {
		t.Fatalf("expected error code to be forwarded, got %q", service.failedReq.ErrorCode)
	}
}

func TestMessageMetadataFromRunCompletedEnrichesDisplayFields(t *testing.T) {
	startedAt := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(1500 * time.Millisecond)

	metadata := messageMetadataFromRunCompleted(&events.RunCompletedPayload{
		Metadata: map[string]any{
			"model_name":        "gpt-4o",
			"run_started_at_ms": startedAt.UnixMilli(),
			"runtime":           "external_cli",
		},
		Usage: &events.UsagePayload{
			InputTokens:  100,
			OutputTokens: 20,
			TotalTokens:  120,
		},
		StartedAt:   startedAt,
		CompletedAt: completedAt,
	})
	if metadata == nil || metadata.Extra == nil {
		t.Fatal("expected metadata extra to be populated")
	}
	if metadata.Extra["model"] != "gpt-4o" {
		t.Fatalf("expected model gpt-4o, got %#v", metadata.Extra["model"])
	}
	if metadata.Extra["tokens"] != 120 {
		t.Fatalf("expected tokens 120, got %#v", metadata.Extra["tokens"])
	}
	if metadata.Extra["latency"] != int64(1500) {
		t.Fatalf("expected latency 1500, got %#v", metadata.Extra["latency"])
	}
}

func TestMessageMetadataFromRunCompletedWithoutRuntimeMetadata(t *testing.T) {
	startedAt := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(800 * time.Millisecond)

	metadata := messageMetadataFromRunCompleted(&events.RunCompletedPayload{
		Usage: &events.UsagePayload{
			TotalTokens: 42,
		},
		StartedAt:   startedAt,
		CompletedAt: completedAt,
	})
	if metadata == nil || metadata.Extra == nil {
		t.Fatal("expected metadata extra to be populated from usage and timing")
	}
	if metadata.Extra["tokens"] != 42 {
		t.Fatalf("expected tokens 42, got %#v", metadata.Extra["tokens"])
	}
	if metadata.Extra["latency"] != int64(800) {
		t.Fatalf("expected latency 800, got %#v", metadata.Extra["latency"])
	}
}

type recordingSessionService struct {
	completeReq *contract.CompleteSessionMessageRequest
	failedReq   *contract.FailedSessionMessageRequest
}

func (s *recordingSessionService) CreateSession(context.Context, *contract.CreateSessionRequest) (*contract.Session, error) {
	return nil, nil
}
func (s *recordingSessionService) GetSession(context.Context, string) (*contract.Session, error) {
	return nil, nil
}
func (s *recordingSessionService) UpdateSession(context.Context, string, *contract.UpdateSessionRequest) (*contract.Session, error) {
	return nil, nil
}
func (s *recordingSessionService) DeleteSession(context.Context, string) error { return nil }
func (s *recordingSessionService) ListSessions(context.Context, *contract.ListSessionsRequest) (*contract.SessionList, error) {
	return nil, nil
}
func (s *recordingSessionService) ActivateSession(context.Context, string) error { return nil }
func (s *recordingSessionService) PauseSession(context.Context, string) error    { return nil }
func (s *recordingSessionService) EndSession(context.Context, string) error      { return nil }
func (s *recordingSessionService) ResumeSession(context.Context, string) error   { return nil }
func (s *recordingSessionService) AddMessage(context.Context, string, *contract.AddMessageRequest) (*contract.SessionMessage, error) {
	return nil, nil
}
func (s *recordingSessionService) GetSessionMessages(context.Context, string, int, int) (*contract.MessageList, error) {
	return nil, nil
}
func (s *recordingSessionService) DeleteMessage(context.Context, uint) error          { return nil }
func (s *recordingSessionService) ClearSessionMessages(context.Context, string) error { return nil }
func (s *recordingSessionService) StreamSessionEvents(context.Context, string, bool, events.Sink) error {
	return nil
}
func (s *recordingSessionService) HandleSessionRunStarted(context.Context, *contract.SessionRunStartedRequest) error {
	return nil
}
func (s *recordingSessionService) CompleteSessionMessage(_ context.Context, req *contract.CompleteSessionMessageRequest) error {
	s.completeReq = req
	return nil
}
func (s *recordingSessionService) FailedSessionMessage(_ context.Context, req *contract.FailedSessionMessageRequest) error {
	s.failedReq = req
	return nil
}
func (s *recordingSessionService) HandleSessionTitleRequest(context.Context, string) error {
	return nil
}
func (s *recordingSessionService) SubmitApproval(context.Context, *contract.SubmitApprovalRequest) error {
	return nil
}

func (s *recordingSessionService) SubmitQuestionAnswer(context.Context, *contract.SubmitQuestionAnswerRequest) error {
	return nil
}

var _ contract.SessionService = (*recordingSessionService)(nil)
