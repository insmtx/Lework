package contract

import "context"

// DigitalAssistantService 定义数字助手服务接口
type DigitalAssistantService interface {
	// 创建数字助手
	CreateDigitalAssistant(ctx context.Context, req *CreateDigitalAssistantRequest) (*DigitalAssistant, error)

	// 根据 ID 获取数字助手详情（从上下文获取权限信息）
	GetDigitalAssistantByID(ctx context.Context, id uint) (*DigitalAssistantDetail, error)

	// 根据 Code 获取数字助手详情（从上下文获取权限信息）
	GetDigitalAssistantByCode(ctx context.Context, code string) (*DigitalAssistantDetail, error)

	// 更新数字助手信息（从上下文获取权限信息）
	UpdateDigitalAssistant(ctx context.Context, id uint, req *UpdateDigitalAssistantRequest) (*DigitalAssistant, error)

	// 删除数字助手（从上下文获取权限信息）
	DeleteDigitalAssistant(ctx context.Context, id uint) error

	// 查询数字助手列表（从上下文获取权限信息）
	ListDigitalAssistant(ctx context.Context, req *ListDigitalAssistantRequest) (*DigitalAssistantList, error)

	// 更新数字助手状态（从上下文获取权限信息）
	UpdateDigitalAssistantStatus(ctx context.Context, id uint, req *UpdateDigitalAssistantStatusRequest) error

	// 基于模板创建数字助手（从上下文获取权限信息）
	CreateDigitalAssistantFromTemplate(ctx context.Context, req *CreateDigitalAssistantFromTemplateRequest) (*DigitalAssistant, error)
}
