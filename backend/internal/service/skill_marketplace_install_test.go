package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/nats-io/nats.go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

type skillInstallPublisher struct {
	response messaging.WorkerCommandResult
	err      error
	requests []any
}

func (p *skillInstallPublisher) Publish(context.Context, string, any) error {
	return nil
}

func (p *skillInstallPublisher) Request(_ context.Context, _ string, event any) (*nats.Msg, error) {
	p.requests = append(p.requests, event)
	if p.err != nil {
		return nil, p.err
	}
	data, err := json.Marshal(p.response)
	if err != nil {
		return nil, err
	}
	return &nats.Msg{Data: data}, nil
}

func installedSkillsResponse(t *testing.T, skills []contract.SkillInstalledItem) messaging.WorkerCommandResult {
	t.Helper()

	data, err := json.Marshal(skills)
	if err != nil {
		t.Fatalf("marshal installed skills: %v", err)
	}
	return messaging.WorkerCommandResult{
		Success: true,
		Action:  "list",
		Data:    json.RawMessage(data),
	}
}

func setupSkillMarketplaceInstallServiceDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, context.Context, func()) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	database, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open gorm db: %v", err)
	}

	caller := &types.Caller{
		Uin:   1,
		OrgID: 100,
		Kind:  types.CallerKindUser,
		State: types.AuthStateSucc,
	}
	ctx := auth.WithContext(context.Background(), caller, &types.Trace{RequestID: "test-request"})
	cleanup := func() {
		sqlDB.Close()
	}
	return database, mock, ctx, cleanup
}

func expectDefaultWorkerDeployment(mock sqlmock.Sqlmock) {
	columns := []string{
		"id", "created_at", "updated_at", "deleted_at", "org_id",
		"digital_assistant_id", "worker_id", "deployment_name", "namespace",
		"status", "bootstrap_token_hash", "workspace_path", "last_error",
		"last_started_at", "last_reconciled_at",
	}
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "leros_worker_deployment" WHERE (org_id = $1 AND worker_id = $2) AND "leros_worker_deployment"."deleted_at" IS NULL ORDER BY "leros_worker_deployment"."id" LIMIT $3`)).
		WithArgs(uint(100), uint(1), 1).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			1, now, now, nil, 100,
			200, 300, "test-worker", "",
			string(types.WorkerDeploymentStatusReady), "", "", "",
			nil, nil,
		))
}

func expectNoBuiltinSkillItems(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT .* FROM "leros_builtin_skill_marketplace_item"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "skill_id", "status"}))
}

func initSkillImportTestStorage(t *testing.T) {
	t.Helper()
	if err := filestore.Init(&config.StorageConfig{
		Driver:     "local",
		LocalDir:   t.TempDir(),
		Bucket:     "test-bucket",
		BaseURL:    "http://127.0.0.1:8080/storage",
		SignSecret: "test-sign-secret",
	}); err != nil {
		t.Fatalf("init storage: %v", err)
	}
}

func putSkillImportTestFile(t *testing.T, content []byte) string {
	t.Helper()
	result, err := filestore.GetStorage().PutObject(
		context.Background(),
		filestore.DefaultBucket(),
		"imports/demo-skill/SKILL.md",
		bytes.NewReader(content),
	)
	if err != nil {
		t.Fatalf("put test file: %v", err)
	}
	return result.Path.URI()
}

func skillImportZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func expectFileUploadLookup(mock sqlmock.Sqlmock, publicID, originalName, storagePath string) {
	columns := []string{
		"id", "created_at", "updated_at", "deleted_at", "public_id",
		"org_id", "owner_id", "filename", "original_name", "mime_type",
		"file_size", "storage_uri", "sha256", "purpose", "status", "metadata",
	}
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "leros_file_upload" WHERE (public_id = $1 AND org_id = $2) AND "leros_file_upload"."deleted_at" IS NULL ORDER BY "leros_file_upload"."id" LIMIT $3`)).
		WithArgs(publicID, uint(100), 1).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			1, now, now, nil, publicID,
			100, 1, originalName, originalName, "text/markdown",
			128, storagePath, "", "project", "active", []byte(`{}`),
		))
}

func expectInstallIncrement(mock sqlmock.Sqlmock, rowsAffected int64) {
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "leros_skill_marketplace_item" SET "installs"=installs + $1 WHERE (source = $2 AND skill_id = $3 AND version = $4) AND "leros_skill_marketplace_item"."deleted_at" IS NULL`)).
		WithArgs(1, "Leros", "demo-skill", "1.2.3").
		WillReturnResult(sqlmock.NewResult(0, rowsAffected))
}

func TestInstallSkillIncrementsMarketplaceInstallsAfterWorkerSuccess(t *testing.T) {
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectDefaultWorkerDeployment(mock)
	expectInstallIncrement(mock, 1)
	publisher := &skillInstallPublisher{
		response: messaging.WorkerCommandResult{
			Success: true,
			Action:  "install",
			Message: "skill installed",
		},
	}
	service := NewSkillMarketplaceService(database, publisher, nil, "")

	resp, err := service.InstallSkill(ctx, &contract.InstallSkillRequest{
		Source:  "Leros",
		SkillID: "demo-skill",
		Version: "1.2.3",
	})
	if err != nil {
		t.Fatalf("install skill: %v", err)
	}
	if resp.Status != "accepted" {
		t.Fatalf("status = %q, want accepted", resp.Status)
	}
	if resp.Message != "skill installed" {
		t.Fatalf("message = %q, want worker message", resp.Message)
	}
	if len(publisher.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(publisher.requests))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestInstallSkillWorkerFailureDoesNotIncrementInstalls(t *testing.T) {
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectDefaultWorkerDeployment(mock)
	publisher := &skillInstallPublisher{
		response: messaging.WorkerCommandResult{
			Success: false,
			Action:  "install",
			Error:   "install failed",
		},
	}
	service := NewSkillMarketplaceService(database, publisher, nil, "")

	_, err := service.InstallSkill(ctx, &contract.InstallSkillRequest{
		Source:  "Leros",
		SkillID: "demo-skill",
		Version: "1.2.3",
	})
	if err == nil {
		t.Fatal("expected install error")
	}
	if !strings.Contains(err.Error(), "install failed") {
		t.Fatalf("error = %q, want install failed", err.Error())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestInstallSkillMissingMarketplaceRowDoesNotBlockInstall(t *testing.T) {
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectDefaultWorkerDeployment(mock)
	expectInstallIncrement(mock, 0)
	publisher := &skillInstallPublisher{
		response: messaging.WorkerCommandResult{
			Success: true,
			Action:  "install",
			Message: "skill installed",
		},
	}
	service := NewSkillMarketplaceService(database, publisher, nil, "")

	resp, err := service.InstallSkill(ctx, &contract.InstallSkillRequest{
		Source:  "Leros",
		SkillID: "demo-skill",
		Version: "1.2.3",
	})
	if err != nil {
		t.Fatalf("install skill: %v", err)
	}
	if resp.Status != "accepted" {
		t.Fatalf("status = %q, want accepted", resp.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestInstallSkillRequestErrorDoesNotIncrementInstalls(t *testing.T) {
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectDefaultWorkerDeployment(mock)
	publisher := &skillInstallPublisher{err: errors.New("nats unavailable")}
	service := NewSkillMarketplaceService(database, publisher, nil, "")

	_, err := service.InstallSkill(ctx, &contract.InstallSkillRequest{
		Source:  "Leros",
		SkillID: "demo-skill",
		Version: "1.2.3",
	})
	if err == nil {
		t.Fatal("expected request error")
	}
	if !strings.Contains(err.Error(), "nats unavailable") {
		t.Fatalf("error = %q, want nats unavailable", err.Error())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestImportSkillReturnsImportedAfterWorkerSuccess(t *testing.T) {
	initSkillImportTestStorage(t)
	content := []byte("---\nname: demo-skill\ndescription: Demo skill\n---\nUse this skill.\n")
	storagePath := putSkillImportTestFile(t, content)
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectFileUploadLookup(mock, "file_demo", "SKILL.md", storagePath)
	expectFileUploadLookup(mock, "file_demo", "SKILL.md", storagePath)
	expectDefaultWorkerDeployment(mock)
	publisher := &skillInstallPublisher{
		response: messaging.WorkerCommandResult{
			Success: true,
			Action:  "import",
			Message: "skill imported",
		},
	}
	service := NewSkillMarketplaceService(database, publisher, nil, "")

	resp, err := service.ImportSkill(ctx, &contract.ImportSkillRequest{FileUploadID: "file_demo"})
	if err != nil {
		t.Fatalf("import skill: %v", err)
	}
	if resp.Status != "imported" {
		t.Fatalf("status = %q, want imported", resp.Status)
	}
	if resp.Message != "skill imported" {
		t.Fatalf("message = %q, want worker message", resp.Message)
	}
	if len(publisher.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(publisher.requests))
	}
	msg, ok := publisher.requests[0].(messaging.WorkerCommand)
	if !ok {
		t.Fatalf("request type = %T, want WorkerCommand", publisher.requests[0])
	}
	payload, err := messaging.DecodeCommandPayload[messaging.SkillCommandPayload](&msg.Body)
	if err != nil {
		t.Fatalf("decode skill command payload: %v", err)
	}
	if payload.Action != "import" || payload.Source != "url" || payload.DownloadURL == "" {
		t.Fatalf("unexpected import body: %+v", payload)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestImportSkillWorkerFailureReturnsError(t *testing.T) {
	initSkillImportTestStorage(t)
	content := []byte("---\nname: demo-skill\ndescription: Demo skill\n---\nUse this skill.\n")
	storagePath := putSkillImportTestFile(t, content)
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectFileUploadLookup(mock, "file_demo", "SKILL.md", storagePath)
	expectFileUploadLookup(mock, "file_demo", "SKILL.md", storagePath)
	expectDefaultWorkerDeployment(mock)
	publisher := &skillInstallPublisher{
		response: messaging.WorkerCommandResult{
			Success: false,
			Action:  "import",
			Error:   "parse SKILL.md: frontmatter must include name",
		},
	}
	service := NewSkillMarketplaceService(database, publisher, nil, "")

	_, err := service.ImportSkill(ctx, &contract.ImportSkillRequest{FileUploadID: "file_demo"})
	if err == nil {
		t.Fatal("expected import error")
	}
	if want := "SKILL.md 格式错误：frontmatter 必须包含 name"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestImportSkillValidationErrorsAreLocalized(t *testing.T) {
	tests := []struct {
		name         string
		originalName string
		content      []byte
		want         string
	}{
		{
			name:         "corrupted zip",
			originalName: "demo.zip",
			content:      []byte("not a zip"),
			want:         "技能包文件损坏，请重新导出或重新下载后再试",
		},
		{
			name:         "zip missing skill md",
			originalName: "demo.zip",
			content: skillImportZip(t, map[string]string{
				"demo/README.md": "hello",
			}),
			want: "技能包中未找到 SKILL.md，请确认上传的是技能目录或技能压缩包",
		},
		{
			name:         "invalid skill md",
			originalName: "SKILL.md",
			content:      []byte("---\ndescription: Demo skill\n---\nUse this skill.\n"),
			want:         "SKILL.md 格式错误：frontmatter 必须包含 name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initSkillImportTestStorage(t)
			storagePath := putSkillImportTestFile(t, tt.content)
			database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
			defer cleanup()
			expectFileUploadLookup(mock, "file_demo", tt.originalName, storagePath)
			expectFileUploadLookup(mock, "file_demo", tt.originalName, storagePath)
			service := NewSkillMarketplaceService(database, &skillInstallPublisher{}, nil, "")

			_, err := service.ImportSkill(ctx, &contract.ImportSkillRequest{FileUploadID: "file_demo"})
			if err == nil {
				t.Fatal("expected import validation error")
			}
			if err.Error() != tt.want {
				t.Fatalf("error = %q, want %q", err.Error(), tt.want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("sql expectations: %v", err)
			}
		})
	}
}

func TestImportSkillRequestTimeoutIsLocalized(t *testing.T) {
	initSkillImportTestStorage(t)
	content := []byte("---\nname: demo-skill\ndescription: Demo skill\n---\nUse this skill.\n")
	storagePath := putSkillImportTestFile(t, content)
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectFileUploadLookup(mock, "file_demo", "SKILL.md", storagePath)
	expectFileUploadLookup(mock, "file_demo", "SKILL.md", storagePath)
	expectDefaultWorkerDeployment(mock)
	publisher := &skillInstallPublisher{err: context.DeadlineExceeded}
	service := NewSkillMarketplaceService(database, publisher, nil, "")

	_, err := service.ImportSkill(ctx, &contract.ImportSkillRequest{FileUploadID: "file_demo"})
	if err == nil {
		t.Fatal("expected import timeout error")
	}
	want := "技能导入处理中超时，请稍后查看是否已安装，或重试导入"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestImportSkillFromGitHubReturnsImportedAfterWorkerSuccess(t *testing.T) {
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectDefaultWorkerDeployment(mock)
	publisher := &skillInstallPublisher{
		response: messaging.WorkerCommandResult{
			Success: true,
			Action:  "import",
			Message: "github skill imported",
		},
	}
	service := NewSkillMarketplaceService(database, publisher, nil, "")

	resp, err := service.ImportSkillFromGitHub(ctx, &contract.ImportSkillFromGitHubRequest{
		GitHubURL: "https://github.com/browser-use/video-use",
	})
	if err != nil {
		t.Fatalf("import GitHub skill: %v", err)
	}
	if resp.Status != "imported" {
		t.Fatalf("status = %q, want imported", resp.Status)
	}
	if resp.Message != "github skill imported" {
		t.Fatalf("message = %q, want worker message", resp.Message)
	}
	if len(publisher.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(publisher.requests))
	}
	msg, ok := publisher.requests[0].(messaging.WorkerCommand)
	if !ok {
		t.Fatalf("request type = %T, want WorkerCommand", publisher.requests[0])
	}
	payload, err := messaging.DecodeCommandPayload[messaging.SkillCommandPayload](&msg.Body)
	if err != nil {
		t.Fatalf("decode skill command payload: %v", err)
	}
	if payload.Action != "import" || payload.Source != "github" {
		t.Fatalf("unexpected GitHub import body: %+v", msg.Body)
	}
	if payload.SkillID != "browser-use/video-use/." || payload.Version != "" {
		t.Fatalf("github target = %q@%q, want browser-use/video-use/.@", payload.SkillID, payload.Version)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestImportSkillFromGitHubWorkerFailureReturnsError(t *testing.T) {
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectDefaultWorkerDeployment(mock)
	publisher := &skillInstallPublisher{
		response: messaging.WorkerCommandResult{
			Success: false,
			Action:  "import",
			Error:   "fetch GitHub skill: download GitHub skill: GitHub returned status 404",
		},
	}
	service := NewSkillMarketplaceService(database, publisher, nil, "")

	_, err := service.ImportSkillFromGitHub(ctx, &contract.ImportSkillFromGitHubRequest{
		GitHubURL: "https://github.com/openai/skills/tree/main/agents/example",
	})
	if err == nil {
		t.Fatal("expected GitHub import error")
	}
	if want := "GitHub 技能下载失败，请检查链接或网络后重试"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestImportSkillFromGitHubMultipleSkillMDFailureReturnsLocalizedError(t *testing.T) {
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectDefaultWorkerDeployment(mock)
	publisher := &skillInstallPublisher{
		response: messaging.WorkerCommandResult{
			Success: false,
			Action:  "import",
			Error:   "fetch GitHub skill: multiple SKILL.md files found in owner/repo; use a tree link to a skill directory or a blob link to SKILL.md",
		},
	}
	service := NewSkillMarketplaceService(database, publisher, nil, "")

	_, err := service.ImportSkillFromGitHub(ctx, &contract.ImportSkillFromGitHubRequest{
		GitHubURL: "https://github.com/owner/repo",
	})
	if err == nil {
		t.Fatal("expected GitHub import error")
	}
	if want := "仓库中包含多个 SKILL.md，请使用具体技能目录 tree 链接或 SKILL.md blob/raw 链接"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestImportSkillFromGitHubValidationErrorsAreLocalized(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "non skill blob",
			url:  "https://github.com/browser-use/video-use/blob/main/skills/manim-video/README.md",
			want: "GitHub 链接必须指向 SKILL.md 文件",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database, _, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
			defer cleanup()
			service := NewSkillMarketplaceService(database, &skillInstallPublisher{}, nil, "")

			_, err := service.ImportSkillFromGitHub(ctx, &contract.ImportSkillFromGitHubRequest{GitHubURL: tt.url})
			if err == nil {
				t.Fatal("expected GitHub validation error")
			}
			if err.Error() != tt.want {
				t.Fatalf("error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestAnnotateMarketplaceInstalledMatchesNameOrSkillID(t *testing.T) {
	tests := []struct {
		name   string
		detail *contract.SkillDetailResponse
		skills []contract.SkillInstalledItem
	}{
		{
			name: "matches display name",
			detail: &contract.SkillDetailResponse{
				SkillID: "demo-skill",
				Source:  "Leros",
				Name:    "Demo Skill",
			},
			skills: []contract.SkillInstalledItem{{Name: "demo skill"}},
		},
		{
			name: "matches skill id",
			detail: &contract.SkillDetailResponse{
				SkillID: "demo-skill",
				Source:  "Leros",
				Name:    "Localized Demo",
			},
			skills: []contract.SkillInstalledItem{{Name: "DEMO-SKILL"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
			defer cleanup()
			expectDefaultWorkerDeployment(mock)
			expectNoBuiltinSkillItems(mock)
			publisher := &skillInstallPublisher{
				response: installedSkillsResponse(t, tt.skills),
			}
			service := NewSkillMarketplaceService(database, publisher, nil, "").(*skillMarketplaceService)

			service.annotateMarketplaceInstalled(ctx, tt.detail)

			if !tt.detail.Installed {
				t.Fatal("expected detail to be marked installed")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("sql expectations: %v", err)
			}
		})
	}
}

func TestAnnotateMarketplaceInstalledNoMatch(t *testing.T) {
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectDefaultWorkerDeployment(mock)
	expectNoBuiltinSkillItems(mock)
	publisher := &skillInstallPublisher{
		response: installedSkillsResponse(t, []contract.SkillInstalledItem{{Name: "other-skill"}}),
	}
	service := NewSkillMarketplaceService(database, publisher, nil, "").(*skillMarketplaceService)
	detail := &contract.SkillDetailResponse{
		SkillID: "demo-skill",
		Source:  "Leros",
		Name:    "Demo Skill",
	}

	service.annotateMarketplaceInstalled(ctx, detail)

	if detail.Installed {
		t.Fatal("expected detail to remain not installed")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAnnotateMarketplaceInstalledListFailureDoesNotBlockDetail(t *testing.T) {
	database, mock, ctx, cleanup := setupSkillMarketplaceInstallServiceDB(t)
	defer cleanup()
	expectDefaultWorkerDeployment(mock)
	publisher := &skillInstallPublisher{err: errors.New("list unavailable")}
	service := NewSkillMarketplaceService(database, publisher, nil, "").(*skillMarketplaceService)
	detail := &contract.SkillDetailResponse{
		SkillID: "demo-skill",
		Source:  "Leros",
		Name:    "Demo Skill",
	}

	service.annotateMarketplaceInstalled(ctx, detail)

	if detail.Installed {
		t.Fatal("expected detail to remain not installed when list fails")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAnnotateMarketplaceInstalledSourceIsAlwaysInstalled(t *testing.T) {
	service := &skillMarketplaceService{}
	detail := &contract.SkillDetailResponse{
		SkillID: "demo-skill",
		Source:  "installed",
		Name:    "Demo Skill",
	}

	service.annotateMarketplaceInstalled(context.Background(), detail)

	if !detail.Installed {
		t.Fatal("expected installed source detail to be installed")
	}
}
