package contract

import (
	"context"
	"io"
)

// ProjectService 定义项目服务接口
type ProjectService interface {
	CreateProject(ctx context.Context, req *CreateProjectRequest) (*Project, error)

	GetProject(ctx context.Context, publicID string) (*Project, error)

	UpdateProject(ctx context.Context, publicID string, req *UpdateProjectRequest) (*Project, error)

	DeleteProject(ctx context.Context, publicID string) error

	ListProjects(ctx context.Context, req *ListProjectsRequest) (*ProjectList, error)

	DetailProject(ctx context.Context, publicID string) (*ProjectDetail, error)

	GetProjectMemory(ctx context.Context, publicID string) (*ProjectMemory, error)

	GetProjectFileTree(ctx context.Context, publicID string, resourceType string, taskPublicID string) ([]*FileTreeNode, error)

	DownloadProjectFile(ctx context.Context, publicID string, filePath string) (io.ReadCloser, string, int64, error)
}
