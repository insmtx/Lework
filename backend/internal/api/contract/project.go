package contract

import (
	"context"
	"io"
)

// ProjectService 定义项目服务接口
type ProjectService interface {
	// 创建项目
	CreateProject(ctx context.Context, req *CreateProjectRequest) (*Project, error)

	// 根据PublicID获取项目详情
	GetProject(ctx context.Context, publicID string) (*Project, error)

	// 更新项目
	UpdateProject(ctx context.Context, publicID string, req *UpdateProjectRequest) (*Project, error)

	// 删除项目
	DeleteProject(ctx context.Context, publicID string) error

	// 查询项目列表
	ListProjects(ctx context.Context, req *ListProjectsRequest) (*ProjectList, error)

	// 获取项目详情（含任务、会话、产物、成员）
	DetailProject(ctx context.Context, publicID string) (*ProjectDetail, error)

	// 获取项目记忆
	GetProjectMemory(ctx context.Context, publicID string) (*ProjectMemory, error)

	// 获取项目文件树
	GetProjectFileTree(ctx context.Context, publicID string, parentPath string, depth int) ([]*FileTreeNode, error)

	// 下载/预览项目文件（代理 Gitea raw 内容，返回流、Content-Type、Content-Length）
	DownloadProjectFile(ctx context.Context, publicID string, filePath string) (io.ReadCloser, string, int64, error)

	// 上传项目文件
	UploadProjectFile(ctx context.Context, publicID string, reader io.Reader, filename string) (*FileUploadResult, error)

	AddFile(ctx context.Context, publicID string, filePublicID string) error
}
