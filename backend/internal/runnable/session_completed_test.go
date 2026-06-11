package runnable

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/insmtx/Leros/backend/pkg/leros"
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
	ctx := context.Background()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.Session{}, &types.Artifact{}, &types.FileUpload{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	projectID := uint(101)
	taskID := uint(202)
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
	root := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, root)

	if err := filestore.Init(&config.StorageConfig{
		Driver:   "local",
		LocalDir: root,
		Bucket:   "bucket",
	}); err != nil {
		t.Fatalf("init local storage: %v", err)
	}

	storageKey := "projects/7/prj/repo/report.md"
	driverFilePath := filepath.Join(root, "data", "bucket", filepath.FromSlash(storageKey))
	if err := os.MkdirAll(filepath.Dir(driverFilePath), 0o755); err != nil {
		t.Fatalf("create artifact dir: %v", err)
	}
	if err := os.WriteFile(driverFilePath, []byte("hello artifact"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
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
	if artifact.StorageKey == "" || !strings.Contains(artifact.StorageKey, "://") || artifact.FileURL == "" || artifact.FileSize != int64(len("hello artifact")) || artifact.Sha256 == "" {
		t.Fatalf("unexpected artifact storage fields: %#v", artifact)
	}
	if artifact.Metadata.Extra["storage_key_raw"] == nil {
		t.Fatalf("expected storage_key_raw metadata, got %#v", artifact.Metadata)
	}
	if artifact.Metadata.Extra["storage_key_raw"] != storageKey {
		t.Fatalf("expected storage_key_raw %q, got %#v", storageKey, artifact.Metadata)
	}
	if artifact.Metadata.Extra["storage_path"] == nil {
		t.Fatalf("expected storage_path metadata, got %#v", artifact.Metadata)
	}

	fileUpload, err := infradb.GetFileUploadByPublicID(ctx, database, 7, artifact.StorageKey)
	if err != nil {
		t.Fatalf("get file upload by public id: %v", err)
	}
	if fileUpload == nil {
		t.Fatal("expected file upload record created")
	}
	if fileUpload.StoragePath == "" {
		t.Fatal("expected file upload storage path")
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
func (s *recordingSessionService) StreamSessionEvents(context.Context, string, int64, events.Sink) error {
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

var _ contract.SessionService = (*recordingSessionService)(nil)
