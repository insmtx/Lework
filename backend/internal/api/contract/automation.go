package contract

import "context"

// AutomationService 定义自动化服务接口
type AutomationService interface {
	CreateAutomation(ctx context.Context, req *CreateAutomationRequest) (*Automation, error)

	GetAutomation(ctx context.Context, publicID string) (*Automation, error)

	UpdateAutomation(ctx context.Context, publicID string, req *UpdateAutomationRequest) (*Automation, error)

	DeleteAutomation(ctx context.Context, publicID string) error

	ListAutomations(ctx context.Context, req *ListAutomationsRequest) (*AutomationList, error)

	RunAutomationNow(ctx context.Context, publicID string) (*AutomationExecution, error)

	ListAutomationExecutions(ctx context.Context, req *ListAutomationExecutionsRequest) (*AutomationExecutionList, error)

	GetAutomationExecution(ctx context.Context, publicID string) (*AutomationExecution, error)
}
