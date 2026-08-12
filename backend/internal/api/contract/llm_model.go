package contract

import "context"

// LLMModelService 定义LLM模型配置服务接口
type LLMModelService interface {
	// 创建LLM模型配置
	CreateLLMModel(ctx context.Context, req *CreateLLMModelRequest) (*LLMModel, error)

	// 根据ID或Code获取LLM模型配置
	GetLLMModel(ctx context.Context, id uint, code string) (*LLMModel, error)

	// 获取组织默认LLM模型配置
	GetDefaultLLMModel(ctx context.Context) (*LLMModel, error)

	// 更新LLM模型配置
	UpdateLLMModel(ctx context.Context, id uint, req *UpdateLLMModelRequest) (*LLMModel, error)

	// 启用或禁用LLM模型配置
	SetLLMModelStatus(ctx context.Context, id uint, status string) (*LLMModel, error)

	// 删除LLM模型配置
	DeleteLLMModel(ctx context.Context, id uint) error

	// 查询LLM模型配置列表
	ListLLMModels(ctx context.Context, req *ListLLMModelsRequest) (*LLMModelList, error)

	// 测试LLM模型配置连通性
	TestLLMModel(ctx context.Context, req *TestLLMModelRequest) (*TestLLMModelResponse, error)
}
