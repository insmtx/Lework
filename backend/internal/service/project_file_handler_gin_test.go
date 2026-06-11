package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/handler"
	"github.com/insmtx/Leros/backend/types"
)

type mockProjectServiceForAddFile struct {
	addFileFn             func(ctx context.Context, publicID string, filePublicID string) error
	createProjectFn       func(ctx context.Context, req *contract.CreateProjectRequest) (*contract.Project, error)
	getProjectFn          func(ctx context.Context, publicID string) (*contract.Project, error)
	updateProjectFn       func(ctx context.Context, publicID string, req *contract.UpdateProjectRequest) (*contract.Project, error)
	deleteProjectFn       func(ctx context.Context, publicID string) error
	listProjectsFn        func(ctx context.Context, req *contract.ListProjectsRequest) (*contract.ProjectList, error)
	detailProjectFn       func(ctx context.Context, publicID string) (*contract.ProjectDetail, error)
	getProjectMemoryFn    func(ctx context.Context, publicID string) (*contract.ProjectMemory, error)
	getProjectFileTreeFn  func(ctx context.Context, publicID string, parentPath string, depth int) ([]*contract.FileTreeNode, error)
	downloadProjectFileFn func(ctx context.Context, publicID string, filePath string) (io.ReadCloser, string, int64, error)
	uploadProjectFileFn   func(ctx context.Context, publicID string, reader io.Reader, filename string) (*contract.FileUploadResult, error)
}

func (m *mockProjectServiceForAddFile) AddFile(ctx context.Context, publicID string, filePublicID string) error {
	return m.addFileFn(ctx, publicID, filePublicID)
}

func (m *mockProjectServiceForAddFile) CreateProject(ctx context.Context, req *contract.CreateProjectRequest) (*contract.Project, error) {
	if m.createProjectFn != nil {
		return m.createProjectFn(ctx, req)
	}
	return nil, nil
}
func (m *mockProjectServiceForAddFile) GetProject(ctx context.Context, publicID string) (*contract.Project, error) {
	if m.getProjectFn != nil {
		return m.getProjectFn(ctx, publicID)
	}
	return nil, nil
}
func (m *mockProjectServiceForAddFile) UpdateProject(ctx context.Context, publicID string, req *contract.UpdateProjectRequest) (*contract.Project, error) {
	if m.updateProjectFn != nil {
		return m.updateProjectFn(ctx, publicID, req)
	}
	return nil, nil
}
func (m *mockProjectServiceForAddFile) DeleteProject(ctx context.Context, publicID string) error {
	if m.deleteProjectFn != nil {
		return m.deleteProjectFn(ctx, publicID)
	}
	return nil
}
func (m *mockProjectServiceForAddFile) ListProjects(ctx context.Context, req *contract.ListProjectsRequest) (*contract.ProjectList, error) {
	if m.listProjectsFn != nil {
		return m.listProjectsFn(ctx, req)
	}
	return nil, nil
}
func (m *mockProjectServiceForAddFile) DetailProject(ctx context.Context, publicID string) (*contract.ProjectDetail, error) {
	if m.detailProjectFn != nil {
		return m.detailProjectFn(ctx, publicID)
	}
	return nil, nil
}
func (m *mockProjectServiceForAddFile) GetProjectMemory(ctx context.Context, publicID string) (*contract.ProjectMemory, error) {
	if m.getProjectMemoryFn != nil {
		return m.getProjectMemoryFn(ctx, publicID)
	}
	return nil, nil
}
func (m *mockProjectServiceForAddFile) GetProjectFileTree(ctx context.Context, publicID string, parentPath string, depth int) ([]*contract.FileTreeNode, error) {
	if m.getProjectFileTreeFn != nil {
		return m.getProjectFileTreeFn(ctx, publicID, parentPath, depth)
	}
	return nil, nil
}
func (m *mockProjectServiceForAddFile) DownloadProjectFile(ctx context.Context, publicID string, filePath string) (io.ReadCloser, string, int64, error) {
	if m.downloadProjectFileFn != nil {
		return m.downloadProjectFileFn(ctx, publicID, filePath)
	}
	return nil, "", 0, nil
}
func (m *mockProjectServiceForAddFile) UploadProjectFile(ctx context.Context, publicID string, reader io.Reader, filename string) (*contract.FileUploadResult, error) {
	if m.uploadProjectFileFn != nil {
		return m.uploadProjectFileFn(ctx, publicID, reader, filename)
	}
	return nil, nil
}

func setupProjectFileRouter(t *testing.T, svc contract.ProjectService, caller *types.Caller) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.Use(func(ctx *gin.Context) {
		trace := &types.Trace{
			RequestID: "test-request-id",
			TraceID:   "test-trace-id",
		}
		auth.WithGinContext(ctx, caller, trace)
		ctx.Next()
	})

	h := handler.NewProjectFileHandler(svc)
	h.RegisterRoutes(router.Group("/v1"))
	return router
}

func TestAddProjectFile_Success(t *testing.T) {
	var capturedPID, capturedFID string
	svc := &mockProjectServiceForAddFile{
		addFileFn: func(ctx context.Context, publicID string, filePublicID string) error {
			capturedPID = publicID
			capturedFID = filePublicID
			return nil
		},
	}
	router := setupProjectFileRouter(t, svc, authenticatedCaller())

	body, _ := json.Marshal(contract.AddFileRequest{PublicID: "test://file-id"})
	req := httptest.NewRequest("POST", "/v1/projects/prj_abc/AddFile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	if capturedPID != "prj_abc" {
		t.Errorf("expected project public_id 'prj_abc', got '%s'", capturedPID)
	}
	if capturedFID != "test://file-id" {
		t.Errorf("expected file public_id 'test://file-id', got '%s'", capturedFID)
	}
}

func TestAddProjectFile_EmptyProjectID(t *testing.T) {
	svc := &mockProjectServiceForAddFile{
		addFileFn: func(ctx context.Context, publicID string, filePublicID string) error {
			t.Error("AddFile should not be called with empty project_id")
			return nil
		},
	}
	router := setupProjectFileRouter(t, svc, authenticatedCaller())

	body, _ := json.Marshal(contract.AddFileRequest{PublicID: "test://file-id"})
	req := httptest.NewRequest("POST", "/v1/projects/%20/AddFile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestAddProjectFile_InvalidJSON(t *testing.T) {
	svc := &mockProjectServiceForAddFile{
		addFileFn: func(ctx context.Context, publicID string, filePublicID string) error {
			t.Error("AddFile should not be called with invalid body")
			return nil
		},
	}
	router := setupProjectFileRouter(t, svc, authenticatedCaller())

	req := httptest.NewRequest("POST", "/v1/projects/prj_abc/AddFile", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestAddProjectFile_EmptyPublicID(t *testing.T) {
	var capturedFID string
	svc := &mockProjectServiceForAddFile{
		addFileFn: func(ctx context.Context, publicID string, filePublicID string) error {
			capturedFID = filePublicID
			return errors.New("public_id is required")
		},
	}
	router := setupProjectFileRouter(t, svc, authenticatedCaller())

	body, _ := json.Marshal(contract.AddFileRequest{PublicID: ""})
	req := httptest.NewRequest("POST", "/v1/projects/prj_abc/AddFile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	if capturedFID != "" {
		t.Errorf("expected empty public_id in request, got '%s'", capturedFID)
	}
}

func TestAddProjectFile_ProjectNotFound(t *testing.T) {
	svc := &mockProjectServiceForAddFile{
		addFileFn: func(ctx context.Context, publicID string, filePublicID string) error {
			return errors.New("project not found")
		},
	}
	router := setupProjectFileRouter(t, svc, authenticatedCaller())

	body, _ := json.Marshal(contract.AddFileRequest{PublicID: "test://file-id"})
	req := httptest.NewRequest("POST", "/v1/projects/nonexistent/AddFile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestAddProjectFile_FileNotFound(t *testing.T) {
	svc := &mockProjectServiceForAddFile{
		addFileFn: func(ctx context.Context, publicID string, filePublicID string) error {
			return errors.New("file not found")
		},
	}
	router := setupProjectFileRouter(t, svc, authenticatedCaller())

	body, _ := json.Marshal(contract.AddFileRequest{PublicID: "nonexistent-file"})
	req := httptest.NewRequest("POST", "/v1/projects/prj_abc/AddFile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestAddProjectFile_RequireFilePublicID(t *testing.T) {
	svc := &mockProjectServiceForAddFile{
		addFileFn: func(ctx context.Context, publicID string, filePublicID string) error {
			return errors.New("file_public_id is required")
		},
	}
	router := setupProjectFileRouter(t, svc, authenticatedCaller())

	body, _ := json.Marshal(contract.AddFileRequest{PublicID: ""})
	req := httptest.NewRequest("POST", "/v1/projects/prj_abc/AddFile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestAddProjectFile_InternalError(t *testing.T) {
	svc := &mockProjectServiceForAddFile{
		addFileFn: func(ctx context.Context, publicID string, filePublicID string) error {
			return errors.New("unexpected database error")
		},
	}
	router := setupProjectFileRouter(t, svc, authenticatedCaller())

	body, _ := json.Marshal(contract.AddFileRequest{PublicID: "test://file-id"})
	req := httptest.NewRequest("POST", "/v1/projects/prj_abc/AddFile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestAddProjectFile_Unauthenticated(t *testing.T) {
	svc := &mockProjectServiceForAddFile{
		addFileFn: func(ctx context.Context, publicID string, filePublicID string) error {
			return errors.New("user not authenticated or org not set")
		},
	}
	router := setupProjectFileRouter(t, svc, unauthenticatedCaller())

	body, _ := json.Marshal(contract.AddFileRequest{PublicID: "test://file-id"})
	req := httptest.NewRequest("POST", "/v1/projects/prj_abc/AddFile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}
