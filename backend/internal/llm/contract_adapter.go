package llm

import (
	"context"
	"errors"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

// errNotAuthenticated 表示认证上下文中缺少有效的组织身份。
var errNotAuthenticated = errors.New("user not authenticated or org not set")

// orgCreatorChecker 判断 uin 是否为 orgID 组织的创建者。
// 由 account 层（oss/enterprise 各自实现）注入，保证组织权限判定走 edition 抽象而非本模块直查。
type orgCreatorChecker interface {
	IsOrgCreator(ctx context.Context, orgID, uin uint) (bool, error)
}

// ContractAdapter 将 llm.Manager 适配为 contract.LLMModelService。
type ContractAdapter struct {
	manager         Manager
	orgCreatorCheck orgCreatorChecker
}

// NewContractAdapter 创建一个实现 contract.LLMModelService 的适配器。
// orgCreatorCheck 提供组织创建者的权限判定，nil 时禁用校验（仅测试用）。
func NewContractAdapter(manager Manager, orgCreatorCheck orgCreatorChecker) contract.LLMModelService {
	return &ContractAdapter{manager: manager, orgCreatorCheck: orgCreatorCheck}
}

// CreateLLMModel 创建 LLM 模型配置。
func (a *ContractAdapter) CreateLLMModel(ctx context.Context, req *contract.CreateLLMModelRequest) (*contract.LLMModel, error) {
	orgID, err := orgIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.requireOrgCreator(ctx, orgID); err != nil {
		return nil, err
	}
	cfg, err := a.manager.Create(ctx, orgID, &CreateRequest{
		Name:        req.Name,
		Description: req.Description,
		Provider:    req.Provider,
		Model:       req.Model,
		BaseURL:     req.BaseURL,
		APIKey:      req.APIKey,
		Status:      req.Status,
		Purpose:     types.LLMModelPurpose(req.Purpose),
		IsDefault:   req.IsDefault,
		Config:      req.Config,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	})
	if err != nil {
		return nil, err
	}
	return modelConfigToContract(cfg), nil
}

// GetLLMModel 根据 ID 或 Code 获取 LLM 模型配置。
func (a *ContractAdapter) GetLLMModel(ctx context.Context, id uint, code string) (*contract.LLMModel, error) {
	orgID, err := orgIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.requireOrgCreator(ctx, orgID); err != nil {
		return nil, err
	}
	cfg, err := a.manager.Get(ctx, orgID, id, code)
	if err != nil {
		return nil, err
	}
	return modelConfigToContract(cfg), nil
}

// GetDefaultLLMModel 获取组织默认 LLM 模型配置。
func (a *ContractAdapter) GetDefaultLLMModel(ctx context.Context) (*contract.LLMModel, error) {
	orgID, err := orgIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.requireOrgCreator(ctx, orgID); err != nil {
		return nil, err
	}
	cfg, err := a.manager.GetDefault(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return modelConfigToContract(cfg), nil
}

// UpdateLLMModel 更新 LLM 模型配置。
func (a *ContractAdapter) UpdateLLMModel(ctx context.Context, id uint, req *contract.UpdateLLMModelRequest) (*contract.LLMModel, error) {
	orgID, err := orgIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.requireOrgCreator(ctx, orgID); err != nil {
		return nil, err
	}
	cfg, err := a.manager.Update(ctx, orgID, id, &UpdateRequest{
		Name:        req.Name,
		Description: req.Description,
		Provider:    req.Provider,
		Model:       req.Model,
		BaseURL:     req.BaseURL,
		APIKey:      req.APIKey,
		Purpose:     purposePtr(req.Purpose),
		IsDefault:   req.IsDefault,
		Config:      req.Config,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	})
	if err != nil {
		return nil, err
	}
	return modelConfigToContract(cfg), nil
}

// SetLLMModelStatus 启用或禁用 LLM 模型配置。
func (a *ContractAdapter) SetLLMModelStatus(ctx context.Context, id uint, status string) (*contract.LLMModel, error) {
	orgID, err := orgIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.requireOrgCreator(ctx, orgID); err != nil {
		return nil, err
	}
	cfg, err := a.manager.SetStatus(ctx, orgID, id, status)
	if err != nil {
		return nil, err
	}
	return modelConfigToContract(cfg), nil
}

// DeleteLLMModel 删除 LLM 模型配置。
func (a *ContractAdapter) DeleteLLMModel(ctx context.Context, id uint) error {
	orgID, err := orgIDFromContext(ctx)
	if err != nil {
		return err
	}
	if err := a.requireOrgCreator(ctx, orgID); err != nil {
		return err
	}
	return a.manager.Delete(ctx, orgID, id)
}

// ListLLMModels 查询 LLM 模型配置列表。
func (a *ContractAdapter) ListLLMModels(ctx context.Context, req *contract.ListLLMModelsRequest) (*contract.LLMModelList, error) {
	orgID, err := orgIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.requireOrgCreator(ctx, orgID); err != nil {
		return nil, err
	}
	result, err := a.manager.List(ctx, orgID, &ListRequest{
		Provider: req.Provider,
		Status:   req.Status,
		Purpose:  req.Purpose,
		Keyword:  req.Keyword,
		Offset:   req.Offset,
		Limit:    req.Limit,
	})
	if err != nil {
		return nil, err
	}
	items := make([]contract.LLMModel, 0, len(result.Items))
	for _, cfg := range result.Items {
		items = append(items, *modelConfigToContract(cfg))
	}
	return &contract.LLMModelList{
		Total:  result.Total,
		Offset: result.Offset,
		Limit:  result.Limit,
		Items:  items,
	}, nil
}

// TestLLMModel 测试 LLM 模型配置连通性。
func (a *ContractAdapter) TestLLMModel(ctx context.Context, req *contract.TestLLMModelRequest) (*contract.TestLLMModelResponse, error) {
	orgID, err := orgIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.requireOrgCreator(ctx, orgID); err != nil {
		return nil, err
	}
	result, err := a.manager.TestConnectivity(ctx, orgID, &TestRequest{
		ID:       req.ID,
		Code:     req.Code,
		Provider: req.Provider,
		Model:    req.Model,
		BaseURL:  req.BaseURL,
		APIKey:   req.APIKey,
	})
	if err != nil {
		return nil, err
	}
	return &contract.TestLLMModelResponse{
		Success:      result.Success,
		Message:      result.Message,
		Endpoint:     result.Endpoint,
		LatencyMS:    result.LatencyMS,
		BaseURLHasV1: result.BaseURLHasV1,
	}, nil
}

// orgIDFromContext 从认证上下文解析 orgID。
func orgIDFromContext(ctx context.Context) (uint, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 || caller.State != types.AuthStateSucc {
		return 0, errNotAuthenticated
	}
	return caller.OrgID, nil
}

// requireOrgCreator 校验当前 caller 是否为 orgID 组织的创建者。
// 非创建者返回 permission denied，由 handler 映射为 403。
func (a *ContractAdapter) requireOrgCreator(ctx context.Context, orgID uint) error {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 || caller.State != types.AuthStateSucc {
		return errNotAuthenticated
	}
	if a.orgCreatorCheck == nil {
		return errors.New("permission denied")
	}
	if orgID == 0 {
		orgID = caller.OrgID
	}
	ok, err := a.orgCreatorCheck.IsOrgCreator(ctx, orgID, caller.Uin)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("permission denied")
	}
	return nil
}

// purposePtr 将 *string 用途转换为 *types.LLMModelPurpose。
func purposePtr(s *string) *types.LLMModelPurpose {
	if s == nil {
		return nil
	}
	p := types.LLMModelPurpose(*s)
	return &p
}

// modelConfigToContract 将领域类型 ModelConfig 转换为 contract.LLMModel。
// APIKey 字段做脱敏处理，不暴露明文。
func modelConfigToContract(cfg *ModelConfig) *contract.LLMModel {
	if cfg == nil {
		return nil
	}
	return &contract.LLMModel{
		ID:           cfg.ID,
		OrgID:        cfg.OrgID,
		Code:         cfg.Code,
		Name:         cfg.Name,
		Description:  cfg.Description,
		Provider:     cfg.Provider,
		Model:        cfg.ModelName,
		BaseURL:      cfg.BaseURL,
		BaseURLHasV1: cfg.BaseURLHasV1,
		APIKey:       maskAPIKey(cfg.APIKey),
		MaxTokens:    cfg.MaxTokens,
		Temperature:  cfg.Temperature,
		TimeoutSec:   cfg.TimeoutSec,
		Status:       cfg.Status,
		Purpose:      string(cfg.Purpose),
		IsDefault:    cfg.IsDefault,
		IsSystem:     cfg.IsSystem,
		Config:       cfg.Config,
		CreatedAt:    cfg.CreatedAt,
		UpdatedAt:    cfg.UpdatedAt,
	}
}

// 确保 ContractAdapter 实现 contract.LLMModelService 接口。
var _ contract.LLMModelService = (*ContractAdapter)(nil)
