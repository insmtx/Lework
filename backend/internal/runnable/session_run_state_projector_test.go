package runnable

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

func TestPersistPublishedPlanCreatesFileUploadAndProjectFileIdempotently(t *testing.T) {
	dsn := fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano())
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.Session{}, &types.FileUpload{}, &types.ProjectFile{}); err != nil {
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
}
