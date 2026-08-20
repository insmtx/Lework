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

	LeaveProject(ctx context.Context, publicID string) error

	ListProjects(ctx context.Context, req *ListProjectsRequest) (*ProjectList, error)

	ListProjectActivities(ctx context.Context, req *ListProjectActivitiesRequest) (*ProjectActivityList, error)

	GetWorkbenchRecentContext(ctx context.Context) (*WorkbenchRecentContext, error)

	SaveWorkbenchRecentContext(ctx context.Context, req *SaveWorkbenchRecentContextRequest) (*WorkbenchRecentContext, error)

	DetailProject(ctx context.Context, publicID string) (*ProjectDetail, error)

	ListProjectPlugins(ctx context.Context, req *ListProjectPluginsRequest) ([]ProjectPlugin, error)

	AddProjectPlugin(ctx context.Context, req *UpdateProjectPluginRequest) (*ProjectPluginMutationResult, error)

	RemoveProjectPlugin(ctx context.Context, req *UpdateProjectPluginRequest) (*ProjectPluginMutationResult, error)

	GetProjectMemory(ctx context.Context, publicID string) (*ProjectMemory, error)

	GetProjectFileTree(ctx context.Context, publicID string, query ProjectFileTreeQuery) ([]*FileTreeNode, error)

	DownloadProjectFile(ctx context.Context, publicID string, filePath string) (io.ReadCloser, string, int64, error)

	GetProjectFileVersions(ctx context.Context, publicID string, filePublicID string) (*ProjectFileVersionList, error)

	DownloadProjectFileByPublicID(ctx context.Context, publicID string, filePublicID string) (io.ReadCloser, string, int64, error)

	RestoreProjectFileVersion(ctx context.Context, publicID string, filePublicID string) (*FileTreeNode, error)
}
