package contract

import "context"

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
}
