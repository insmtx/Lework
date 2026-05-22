package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	agenteino "github.com/insmtx/Leros/backend/internal/runtime/eino"
	"github.com/insmtx/Leros/backend/pkg/utils"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
)

var _ contract.LLMModelService = (*llmModelService)(nil)

type llmModelService struct {
	db *gorm.DB
}

func NewLLMModelService(db *gorm.DB) contract.LLMModelService {
	return &llmModelService{
		db: db,
	}
}

func (s *llmModelService) CreateLLMModel(ctx context.Context, req *contract.CreateLLMModelRequest) (*contract.LLMModel, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if req.Model == "" {
		return nil, errors.New("model is required")
	}
	if strings.TrimSpace(req.BaseURL) == "" {
		return nil, errors.New("base_url is required")
	}
	if strings.TrimSpace(req.APIKey) == "" {
		return nil, errors.New("api_key is required")
	}

	code := generateLLMModelCode()
	name := utils.DefaultString(req.Name, req.Model)
	provider := utils.DefaultString(req.Provider, string(types.LLMProviderOpenAI))
	baseURL := normalizeLLMBaseURL(req.BaseURL)

	model := &types.LLMModel{
		OrgID:           caller.OrgID,
		Code:            code,
		Name:            name,
		Description:     req.Description,
		Provider:        provider,
		ModelName:       req.Model,
		BaseURL:         baseURL,
		APIKeyEncrypted: req.APIKey,
		APIKeyMasked:    maskAPIKey(req.APIKey),
		MaxTokens:       4096,
		Temperature:     0.7,
		TimeoutSec:      120,
		Status:          utils.DefaultString(req.Status, string(types.LLMModelStatusActive)),
		IsDefault:       req.IsDefault,
		Config:          types.LLMModelConfig(req.Config),
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !model.IsDefault {
			hasModels, err := orgHasLLMModels(ctx, tx, caller.OrgID)
			if err != nil {
				return err
			}
			model.IsDefault = !hasModels
		}
		if model.IsDefault {
			if err := clearOrgDefaultLLMModels(ctx, tx, caller.OrgID, 0); err != nil {
				return err
			}
		}
		return db.CreateLLMModel(ctx, tx, model)
	}); err != nil {
		return nil, err
	}
	return convertToContractLLMModel(model), nil
}

func (s *llmModelService) GetLLMModel(ctx context.Context, id uint, code string) (*contract.LLMModel, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	var model *types.LLMModel
	if id > 0 {
		model, err = db.GetLLMModelByID(ctx, s.db, id)
	} else if code != "" {
		model, err = db.GetLLMModelByCode(ctx, s.db, caller.OrgID, code)
	} else {
		return nil, errors.New("id or code is required")
	}
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, errors.New("llm model not found")
	}
	if model.OrgID != caller.OrgID {
		return nil, errors.New("permission denied")
	}
	return convertToContractLLMModel(model), nil
}

func (s *llmModelService) GetDefaultLLMModel(ctx context.Context) (*contract.LLMModel, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	model, err := db.GetDefaultLLMModel(ctx, s.db, caller.OrgID)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, errors.New("llm model not found")
	}
	return convertToContractLLMModel(model), nil
}

func (s *llmModelService) UpdateLLMModel(ctx context.Context, id uint, req *contract.UpdateLLMModelRequest) (*contract.LLMModel, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	var model *types.LLMModel
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		model, err = db.GetLLMModelByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if model == nil {
			return errors.New("llm model not found")
		}
		if model.OrgID != caller.OrgID {
			return errors.New("permission denied")
		}

		if req.Name != "" {
			model.Name = req.Name
		}
		if req.Description != nil {
			model.Description = *req.Description
		}
		if req.Provider != "" {
			model.Provider = req.Provider
		}
		if req.Model != "" {
			model.ModelName = req.Model
		}
		if req.BaseURL != nil {
			model.BaseURL = normalizeLLMBaseURL(*req.BaseURL)
		}
		if req.APIKey != nil {
			model.APIKeyEncrypted = *req.APIKey
			model.APIKeyMasked = maskAPIKey(*req.APIKey)
		}
		if req.Status != "" {
			model.Status = req.Status
		}
		if req.Config != nil {
			model.Config = types.LLMModelConfig(*req.Config)
		}
		if req.IsDefault != nil {
			model.IsDefault = *req.IsDefault
			if model.IsDefault {
				if err := clearOrgDefaultLLMModels(ctx, tx, caller.OrgID, model.ID); err != nil {
					return err
				}
			}
		}

		return db.UpdateLLMModel(ctx, tx, model)
	}); err != nil {
		return nil, err
	}
	return convertToContractLLMModel(model), nil
}

func (s *llmModelService) DeleteLLMModel(ctx context.Context, id uint) error {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return err
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		model, err := db.GetLLMModelByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if model == nil {
			return errors.New("llm model not found")
		}
		if model.OrgID != caller.OrgID {
			return errors.New("permission denied")
		}
		return db.DeleteLLMModel(ctx, tx, id)
	})
}

func (s *llmModelService) ListLLMModels(ctx context.Context, req *contract.ListLLMModelsRequest) (*contract.LLMModelList, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	opt := &types.PageQuery{
		OrgID:  caller.OrgID,
		Offset: req.Offset,
		Limit:  req.Limit,
	}
	if req.Provider != nil && *req.Provider != "" {
		opt.Filters = append(opt.Filters, types.Filter{Field: "provider", Value: []string{*req.Provider}})
	}
	if req.Status != nil && *req.Status != "" {
		opt.Filters = append(opt.Filters, types.Filter{Field: "status", Value: []string{*req.Status}})
	}
	if req.Keyword != nil && *req.Keyword != "" {
		opt.Filters = append(opt.Filters, types.Filter{Field: "keyword", Value: []string{*req.Keyword}})
	}

	models, total, err := db.ListLLMModels(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}

	items := make([]contract.LLMModel, 0, len(models))
	for _, model := range models {
		items = append(items, *convertToContractLLMModel(model))
	}
	return &contract.LLMModelList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func (s *llmModelService) TestLLMModel(ctx context.Context, req *contract.TestLLMModelRequest) (*contract.TestLLMModelResponse, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	baseURL := strings.TrimSpace(req.BaseURL)
	apiKey := strings.TrimSpace(req.APIKey)
	provider := strings.TrimSpace(req.Provider)
	modelName := strings.TrimSpace(req.Model)
	if req.ID != nil || req.Code != "" {
		var model *types.LLMModel
		if req.ID != nil {
			model, err = db.GetLLMModelByID(ctx, s.db, *req.ID)
		} else {
			model, err = db.GetLLMModelByCode(ctx, s.db, caller.OrgID, req.Code)
		}
		if err != nil {
			return nil, err
		}
		if model == nil {
			return nil, errors.New("llm model not found")
		}
		if model.OrgID != caller.OrgID {
			return nil, errors.New("permission denied")
		}
		baseURL = model.BaseURL
		apiKey = model.APIKeyEncrypted
		if provider == "" {
			provider = model.Provider
		}
		if modelName == "" {
			modelName = model.ModelName
		}
	}
	baseURL = normalizeLLMBaseURL(baseURL)
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("base_url is required")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("api_key is required")
	}
	if provider == "" {
		provider = string(types.LLMProviderOpenAI)
	}
	if strings.TrimSpace(modelName) == "" {
		return nil, errors.New("model is required")
	}

	start := time.Now()
	chatModel, err := agenteino.NewChatModel(ctx, &config.LLMConfig{
		Provider: provider,
		APIKey:   apiKey,
		Model:    modelName,
		BaseURL:  baseURL,
	})
	latencyMS := time.Since(start).Milliseconds()
	if err != nil {
		return &contract.TestLLMModelResponse{
			Success:   false,
			Message:   err.Error(),
			Endpoint:  baseURL,
			LatencyMS: latencyMS,
		}, nil
	}

	flow, err := agenteino.NewFlow(ctx, &agenteino.FlowConfig{
		Model:        chatModel,
		SystemPrompt: "You are testing Leros LLM connectivity. Reply with only: ok",
		MaxStep:      1,
	})
	if err != nil {
		return &contract.TestLLMModelResponse{
			Success:   false,
			Message:   err.Error(),
			Endpoint:  baseURL,
			LatencyMS: time.Since(start).Milliseconds(),
		}, nil
	}

	message, err := flow.Generate(ctx, "Reply with only: ok")
	latencyMS = time.Since(start).Milliseconds()
	if err != nil {
		return &contract.TestLLMModelResponse{
			Success:   false,
			Message:   err.Error(),
			Endpoint:  baseURL,
			LatencyMS: latencyMS,
		}, nil
	}
	responseMessage := "model call succeeded"
	if message != nil && strings.TrimSpace(message.Content) != "" {
		responseMessage = strings.TrimSpace(message.Content)
	}
	return &contract.TestLLMModelResponse{
		Success:   true,
		Message:   responseMessage,
		Endpoint:  baseURL,
		LatencyMS: latencyMS,
	}, nil
}

func requireCallerOrg(ctx context.Context) (*types.Caller, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		return nil, errors.New("user not authenticated or org not set")
	}
	return caller, nil
}

func clearOrgDefaultLLMModels(ctx context.Context, database *gorm.DB, orgID uint, excludeID uint) error {
	query := database.WithContext(ctx).Model(&types.LLMModel{}).Where("org_id = ? AND is_default = ?", orgID, true)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	return query.Update("is_default", false).Error
}

func orgHasLLMModels(ctx context.Context, database *gorm.DB, orgID uint) (bool, error) {
	var count int64
	if err := database.WithContext(ctx).Model(&types.LLMModel{}).Where("org_id = ?", orgID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func convertToContractLLMModel(model *types.LLMModel) *contract.LLMModel {
	if model == nil {
		return nil
	}
	return &contract.LLMModel{
		ID:          model.ID,
		OrgID:       model.OrgID,
		Code:        model.Code,
		Name:        model.Name,
		Description: model.Description,
		Provider:    model.Provider,
		Model:       model.ModelName,
		BaseURL:     model.BaseURL,
		APIKey:      model.APIKeyMasked,
		MaxTokens:   model.MaxTokens,
		Temperature: model.Temperature,
		TimeoutSec:  model.TimeoutSec,
		Status:      model.Status,
		IsDefault:   model.IsDefault,
		IsSystem:    model.IsSystem,
		Config:      map[string]interface{}(model.Config),
		CreatedAt:   model.CreatedAt,
		UpdatedAt:   model.UpdatedAt,
	}
}

func generateLLMModelCode() string {
	return fmt.Sprintf("llm_%s", snowflake.GenerateIDBase58())
}

func maskAPIKey(apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ""
	}
	if utf8.RuneCountInString(apiKey) <= 8 {
		return "***"
	}
	prefix := firstRunes(apiKey, 3)
	suffix := lastRunes(apiKey, 4)
	return prefix + "***" + suffix
}

func normalizeLLMBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	trimmed := strings.TrimRight(baseURL, "/")
	for _, suffix := range llmEndpointSuffixes {
		if trimmed, ok := strings.CutSuffix(trimmed, suffix); ok {
			if trimmed, ok := strings.CutSuffix(trimmed, "/v1"); ok {
				return strings.TrimRight(trimmed, "/")
			}
			return strings.TrimRight(trimmed, "/")
		}
	}
	return strings.TrimRight(trimmed, "/")
}

var llmEndpointSuffixes = []string{
	"/v1",
	"/chat/completions",
	"/api/generate",
	"/completions",
	"/responses",
	"/messages",
	"/generate",
	"/api/chat",
	":generateContent",
	":streamGenerateContent",
}

func firstRunes(value string, count int) string {
	runes := []rune(value)
	if len(runes) <= count {
		return value
	}
	return string(runes[:count])
}

func lastRunes(value string, count int) string {
	runes := []rune(value)
	if len(runes) <= count {
		return value
	}
	return string(runes[len(runes)-count:])
}
