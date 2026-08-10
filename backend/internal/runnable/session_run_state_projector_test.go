package runnable

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

func TestRecordSkillInvocationsScopesPluginByOrganization(t *testing.T) {
	dsn := fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano())
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.Session{}, &types.Plugin{}, &types.MessageResource{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	session := &types.Session{PublicID: "session-skill-scope", OrgID: 1, Uin: 7, Status: string(types.SessionStatusActive)}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, plugin := range []types.Plugin{
		{PublicID: "plugin_other", OrgID: 2, Code: "review", Kind: "skill", Name: "Other", Status: types.PluginStatusActive, Origin: "org", CreatedBy: 1, UpdatedBy: 1},
		{PublicID: "plugin_current", OrgID: 1, Code: "review", Kind: "skill", Name: "Current", Status: types.PluginStatusActive, Origin: "org", CreatedBy: 1, UpdatedBy: 1},
	} {
		if err := database.Create(&plugin).Error; err != nil {
			t.Fatalf("create plugin: %v", err)
		}
	}
	payload, err := json.Marshal(messaging.ToolCallPayload{
		ToolCallID: "call-1",
		Name:       "use_skill",
		Arguments:  json.RawMessage(`{"skill":"review"}`),
	})
	if err != nil {
		t.Fatalf("marshal tool call: %v", err)
	}
	recordSkillInvocationsFromMessaging(context.Background(), database, 1, session.PublicID, []messaging.RunEventRecord{
		{Type: string(messaging.RunEventToolCallStarted), Payload: payload},
	})
	var resource types.MessageResource
	if err := database.First(&resource).Error; err != nil {
		t.Fatalf("load message resource: %v", err)
	}
	if resource.OrgID != 1 || resource.ResourceID != "plugin_current" {
		t.Fatalf("message resource = %#v", resource)
	}
}

func TestPersistPublishedPlanCreatesFileUploadAndProjectFileIdempotently(t *testing.T) {
	dsn := fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano())
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.Session{}, &types.FileUpload{}, &types.ProjectFile{}, &types.Resource{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	if err := filestore.Init(&config.StorageConfig{
		Driver:   "local",
		LocalDir: t.TempDir(),
		Bucket:   "dev-bucket",
	}); err != nil {
		t.Fatalf("init filestore: %v", err)
	}

	projectID := uint(10)
	taskID := uint(20)
	projResource := &types.Resource{
		OrgID: 1,
		Uin:   30,
		Type:  types.ResourceTypeProject,
		BizID: projectID,
	}
	if err := db.CreateResource(context.Background(), database, projResource); err != nil {
		t.Fatalf("create project resource: %v", err)
	}
	session := &types.Session{
		PublicID:  "session-1",
		Type:      types.SessionTypeTask,
		Uin:       30,
		OrgID:     1,
		ProjectID: &projectID,
		TaskID:    &taskID,
		Status:    string(types.SessionStatusActive),
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	payload := &messaging.PlanPublishedPayload{
		FileID:       "file_plan_1",
		Directive:    ":::plan{\"file_id\":\"file_plan_1\",\"summary_lines\":1,\"total_lines\":1}\nInspect\n:::",
		SummaryLines: 1,
		TotalLines:   1,
		StorageKey:   "projects/1/sess/session-1/plans/file_plan_1.md",
		StorageURI:   "file:///dev-bucket/projects/1/sess/session-1/plans/file_plan_1.md",
		Filename:     "plan.md",
		OriginalName: ".opencode/plans/plan.md",
		MimeType:     "text/markdown",
		FileSize:     7,
		Sha256:       strings.Repeat("c", 64),
	}
	persister := &declaredArtifactPersister{db: database}
	route := messaging.RouteContext{OrgID: 1, WorkerID: 1, SessionID: session.PublicID}

	if err := persister.PersistPublishedPlan(context.Background(), route, payload); err != nil {
		t.Fatalf("persist plan: %v", err)
	}
	if err := persister.PersistPublishedPlan(context.Background(), route, payload); err != nil {
		t.Fatalf("persist duplicate plan: %v", err)
	}

	var uploads []types.FileUpload
	if err := database.Find(&uploads).Error; err != nil {
		t.Fatalf("list uploads: %v", err)
	}
	if len(uploads) != 1 {
		t.Fatalf("upload count = %d, want 1", len(uploads))
	}
	if uploads[0].PublicID != payload.FileID ||
		uploads[0].Purpose != filestore.PurposePlan ||
		uploads[0].Sha256 != payload.Sha256 {
		t.Fatalf("upload = %#v", uploads[0])
	}

	var projectFiles []types.ProjectFile
	if err := database.Find(&projectFiles).Error; err != nil {
		t.Fatalf("list project files: %v", err)
	}
	if len(projectFiles) != 1 {
		t.Fatalf("project file count = %d, want 1", len(projectFiles))
	}
	if projectFiles[0].FilePublicID != payload.FileID ||
		projectFiles[0].ProjectID != projectID ||
		projectFiles[0].TaskID != taskID ||
		projectFiles[0].ResourceID != uploads[0].ID ||
		projectFiles[0].ResourceType != types.ProjectFileResourceTypePlan {
		t.Fatalf("project file = %#v", projectFiles[0])
	}

	var resources []types.Resource
	if err := database.Where("type = ?", types.ResourceTypeFile).Find(&resources).Error; err != nil {
		t.Fatalf("list file resources: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("file resource count = %d, want 1", len(resources))
	}
	if resources[0].BizID != projectFiles[0].ID ||
		resources[0].ParentResourceID == nil ||
		*resources[0].ParentResourceID != projResource.ID {
		t.Fatalf("file resource = %#v", resources[0])
	}
}

func TestPersistDeclaredArtifactCreatesPathVersionChain(t *testing.T) {
	dsn := fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano())
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.Session{}, &types.FileUpload{}, &types.ProjectFile{}, &types.Resource{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	if err := filestore.Init(&config.StorageConfig{
		Driver:   "local",
		LocalDir: t.TempDir(),
		Bucket:   "dev-bucket",
	}); err != nil {
		t.Fatalf("init filestore: %v", err)
	}

	projectID := uint(10)
	taskID := uint(20)
	projResource := &types.Resource{
		OrgID: 1,
		Uin:   30,
		Type:  types.ResourceTypeProject,
		BizID: projectID,
	}
	if err := db.CreateResource(context.Background(), database, projResource); err != nil {
		t.Fatalf("create project resource: %v", err)
	}
	session := &types.Session{
		PublicID:  "session-artifact-versions",
		Type:      types.SessionTypeTask,
		Uin:       30,
		OrgID:     1,
		ProjectID: &projectID,
		TaskID:    &taskID,
		Status:    string(types.SessionStatusActive),
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	persister := &declaredArtifactPersister{db: database}
	route := messaging.RouteContext{OrgID: 1, WorkerID: 1, SessionID: session.PublicID}
	items := []messaging.ArtifactPayload{
		{
			ArtifactID:   "file_artifact_v1",
			Filename:     "report.md",
			OriginalName: "report.md",
			RelativePath: "artifacts/report.md",
			StorageURI:   "file:///dev-bucket/projects/1/prj/artifacts/v1.md",
			MimeType:     "text/markdown",
			FileSize:     2,
			Sha256:       strings.Repeat("a", 64),
		},
		{
			ArtifactID:   "file_artifact_v2",
			Filename:     "report.md",
			OriginalName: "report.md",
			RelativePath: "artifacts/report.md",
			StorageURI:   "file:///dev-bucket/projects/1/prj/artifacts/v2.md",
			MimeType:     "text/markdown",
			FileSize:     3,
			Sha256:       strings.Repeat("b", 64),
		},
		{
			ArtifactID:           "file_artifact_v3",
			Filename:             "renamed-report.md",
			OriginalName:         "renamed-report.md",
			RelativePath:         "artifacts/renamed-report.md",
			PreviousRelativePath: "artifacts/report.md",
			StorageURI:           "file:///dev-bucket/projects/1/prj/artifacts/v3.md",
			MimeType:             "text/markdown",
			FileSize:             4,
			Sha256:               strings.Repeat("c", 64),
		},
	}
	for i := range items {
		persisted, err := persister.PersistDeclaredArtifact(context.Background(), route, items[i])
		if err != nil {
			t.Fatalf("persist artifact %d: %v", i, err)
		}
		if persisted == nil || persisted.VersionNo != i+1 {
			t.Fatalf("persisted artifact %d version = %#v, want %d", i, persisted, i+1)
		}
	}

	var projectFiles []types.ProjectFile
	if err := database.Order("version_no ASC").Find(&projectFiles).Error; err != nil {
		t.Fatalf("list project files: %v", err)
	}
	if len(projectFiles) != 3 {
		t.Fatalf("project file count = %d, want 3", len(projectFiles))
	}
	if projectFiles[0].RelativePath != "artifacts/report.md" ||
		projectFiles[0].InitialFilePublicID != "file_artifact_v1" ||
		projectFiles[0].VersionNo != 1 {
		t.Fatalf("first project file = %#v", projectFiles[0])
	}
	if projectFiles[1].InitialFilePublicID != "file_artifact_v1" || projectFiles[1].VersionNo != 2 {
		t.Fatalf("second project file = %#v", projectFiles[1])
	}
	if projectFiles[2].RelativePath != "artifacts/renamed-report.md" ||
		projectFiles[2].InitialFilePublicID != "file_artifact_v1" ||
		projectFiles[2].VersionNo != 3 {
		t.Fatalf("renamed project file = %#v", projectFiles[2])
	}

	latest, err := db.ListProjectFiles(context.Background(), database, 1, projectID, string(types.ProjectFileResourceTypeArtifact))
	if err != nil {
		t.Fatalf("list latest project files: %v", err)
	}
	if len(latest) != 1 || latest[0].FilePublicID != "file_artifact_v3" {
		t.Fatalf("latest project files = %#v", latest)
	}

	persisted, err := persister.PersistDeclaredArtifact(context.Background(), route, items[2])
	if err != nil {
		t.Fatalf("replay artifact: %v", err)
	}
	if persisted == nil || persisted.VersionNo != 3 {
		t.Fatalf("replayed artifact version = %#v, want 3", persisted)
	}
	var count int64
	if err := database.Model(&types.ProjectFile{}).Count(&count).Error; err != nil {
		t.Fatalf("count project files: %v", err)
	}
	if count != 3 {
		t.Fatalf("project file count after replay = %d, want 3", count)
	}
}

type fakeCaptureCompleteService struct {
	completeReq *contract.CompleteSessionMessageRequest
	failedReq   *contract.FailedSessionMessageRequest
}

func (f *fakeCaptureCompleteService) CreateSession(ctx context.Context, req *contract.CreateSessionRequest) (*contract.Session, error) {
	return nil, nil
}

func (f *fakeCaptureCompleteService) GetSession(ctx context.Context, sessionID string) (*contract.Session, error) {
	return nil, nil
}

func (f *fakeCaptureCompleteService) UpdateSession(ctx context.Context, sessionID string, req *contract.UpdateSessionRequest) (*contract.Session, error) {
	return nil, nil
}

func (f *fakeCaptureCompleteService) DeleteSession(ctx context.Context, sessionID string) error {
	return nil
}

func (f *fakeCaptureCompleteService) ListSessions(ctx context.Context, req *contract.ListSessionsRequest) (*contract.SessionList, error) {
	return nil, nil
}

func (f *fakeCaptureCompleteService) AddMessage(ctx context.Context, sessionID string, req *contract.AddMessageRequest) (*contract.SessionMessage, error) {
	return nil, nil
}

func (f *fakeCaptureCompleteService) GetSessionMessages(ctx context.Context, sessionID string, page, perPage int) (*contract.MessageList, error) {
	return nil, nil
}

func (f *fakeCaptureCompleteService) DeleteMessage(ctx context.Context, messageID uint) error {
	return nil
}

func (f *fakeCaptureCompleteService) ClearSessionMessages(ctx context.Context, sessionID string) error {
	return nil
}

func (f *fakeCaptureCompleteService) StreamSessionEvents(ctx context.Context, sessionID string, replay bool, assistantID string, sink contract.SessionEventSink) error {
	return nil
}

func (f *fakeCaptureCompleteService) StreamGlobalEvents(ctx context.Context, orgID, userID uint, replaySinceSeq uint64, ch chan<- *messaging.GlobalEventPayload) error {
	return nil
}

func (f *fakeCaptureCompleteService) HandleSessionRunStarted(ctx context.Context, req *contract.SessionRunStartedRequest) error {
	return nil
}

func (f *fakeCaptureCompleteService) CompleteSessionMessage(ctx context.Context, req *contract.CompleteSessionMessageRequest) error {
	f.completeReq = req
	return nil
}

func (f *fakeCaptureCompleteService) FailedSessionMessage(ctx context.Context, req *contract.FailedSessionMessageRequest) error {
	f.failedReq = req
	return nil
}

func (f *fakeCaptureCompleteService) SubmitApproval(ctx context.Context, req *contract.SubmitApprovalRequest) error {
	return nil
}

func (f *fakeCaptureCompleteService) SubmitQuestionAnswer(ctx context.Context, req *contract.SubmitQuestionAnswerRequest) error {
	return nil
}

func (f *fakeCaptureCompleteService) CancelSessionRun(ctx context.Context, sessionID string, req *contract.CancelSessionRunRequest) (*contract.CancelSessionRunResponse, error) {
	return nil, nil
}

func (f *fakeCaptureCompleteService) SetSessionStreamStartSeq(ctx context.Context, sessionID string, streamSeq uint64) error {
	return nil
}

func (f *fakeCaptureCompleteService) CreateInitialMessage(ctx context.Context, req *contract.NewMessageRequest) (*contract.NewMessageResponse, error) {
	return nil, nil
}

func TestCompleteSessionMessageUsesAssistantIDFromRoute(t *testing.T) {
	const assistantID string = "da_public_42"
	const workerID uint = 7

	dsn := fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano())
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	svc := &fakeCaptureCompleteService{}

	runEvent := messaging.RunEvent{
		ID:   "event-1",
		Type: messaging.MessageTypeRunEvent,
		Route: messaging.RouteContext{
			OrgID:     1,
			SessionID: "session-1",
			WorkerID:  workerID,
		},
		Trace: messaging.TraceContext{
			RunID: "run-1",
		},
		Body: messaging.RunEventBody{
			Event:       messaging.RunEventRunCompleted,
			Seq:         1,
			AssistantID: assistantID,
			RunCompleted: &messaging.RunCompletedPayload{
				Result: messaging.RunResultPayload{
					Message: "done",
				},
			},
		},
	}

	handleRunCompletedEvent(context.Background(), svc, database, runEvent)

	if svc.completeReq == nil {
		t.Fatal("CompleteSessionMessage was not called")
	}
	if svc.completeReq.AssistantID != assistantID {
		t.Fatalf("AssistantID = %s, want %s", svc.completeReq.AssistantID, assistantID)
	}
}

func TestFailedSessionMessageUsesAssistantIDFromRoute(t *testing.T) {
	const assistantID string = "da_public_99"
	const workerID uint = 3

	svc := &fakeCaptureCompleteService{}

	runEvent := messaging.RunEvent{
		ID:   "event-2",
		Type: messaging.MessageTypeRunEvent,
		Route: messaging.RouteContext{
			OrgID:     1,
			SessionID: "session-2",
			WorkerID:  workerID,
		},
		Trace: messaging.TraceContext{
			RunID: "run-2",
		},
		Body: messaging.RunEventBody{
			Event:       messaging.RunEventRunFailed,
			Seq:         2,
			AssistantID: assistantID,
			Payload: messaging.RunEventPayload{
				Content: "boom",
			},
		},
	}

	handleRunFailedEvent(context.Background(), svc, nil, runEvent)

	if svc.failedReq == nil {
		t.Fatal("FailedSessionMessage was not called")
	}
	if svc.failedReq.AssistantID != assistantID {
		t.Fatalf("AssistantID = %s, want %s", svc.failedReq.AssistantID, assistantID)
	}
}
