package service

import (
	"context"
	"io"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/handler"
	"github.com/insmtx/Leros/backend/types"
)

type mockProjectServiceForAddFile struct {
	createProjectFn       func(ctx context.Context, req *contract.CreateProjectRequest) (*contract.Project, error)
	getProjectFn          func(ctx context.Context, publicID string) (*contract.Project, error)
	updateProjectFn       func(ctx context.Context, publicID string, req *contract.UpdateProjectRequest) (*contract.Project, error)
	deleteProjectFn       func(ctx context.Context, publicID string) error
	listProjectsFn        func(ctx context.Context, req *contract.ListProjectsRequest) (*contract.ProjectList, error)
	detailProjectFn       func(ctx context.Context, publicID string) (*contract.ProjectDetail, error)
	getProjectMemoryFn    func(ctx context.Context, publicID string) (*contract.ProjectMemory, error)
	getProjectFileTreeFn  func(ctx context.Context, publicID string, parentPath string, depth int) ([]*contract.FileTreeNode, error)
	downloadProjectFileFn func(ctx context.Context, publicID string, filePath string) (io.ReadCloser, string, int64, error)
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
