package contract

import "context"

// TaskService 定义任务服务接口
type TaskService interface {
	CreateTask(ctx context.Context, req *CreateTaskRequest) (*Task, error)

	GetTask(ctx context.Context, publicID string) (*Task, error)

	UpdateTask(ctx context.Context, publicID string, req *UpdateTaskRequest) (*Task, error)

	DeleteTask(ctx context.Context, publicID string) error

	ListTasks(ctx context.Context, req *ListTasksRequest) (*TaskList, error)

	GetTaskDeliverables(ctx context.Context, taskID string) (*TaskDeliverables, error)

	AcceptTask(ctx context.Context, taskID, note string) (*Task, error)

	RejectTask(ctx context.Context, taskID, note string) (*Task, error)
}
