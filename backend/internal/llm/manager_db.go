package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/internal/infra/db"
	pkgeino "github.com/insmtx/Leros/backend/pkg/eino"
	"github.com/insmtx/Leros/backend/types"
)

// modelConfigFromEntity 将持久化实体 types.LLMModel 转换为领域类型 ModelConfig。
// APIKey 字段存储的是存储层中的原始值（APIKeyEncrypted），当前未做额外解密。
func modelConfigFromEntity(m *types.LLMModel) *ModelConfig {
	if m == nil {
		return nil
	}
	return &ModelConfig{
		ID:           m.ID,
		OrgID:        m.OrgID,
		Code:         m.Code,
		Name:         m.Name,
		Description:  m.Description,
		Provider:     m.Provider,
		ModelName:    m.ModelName,
		BaseURL:      m.BaseURL,
		BaseURLHasV1: m.BaseURLHasV1,
		APIKey:       m.APIKeyEncrypted,
		MaxTokens:    m.MaxTokens,
		Temperature:  m.Temperature,
		TimeoutSec:   m.TimeoutSec,
		Status:       m.Status,
		IsDefault:    m.IsDefault,
		IsSystem:     m.IsSystem,
		Config:       map[string]any(m.Config),
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

// --- helper 函数（从 service/llm_model_service.go 迁移，保持行为不变） ---

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

// normalizeLLMBaseURL 清理 base_url 上的已知端点后缀和尾部斜杠，
// 仅保留根地址部分。
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

// detectURLHasV1 检查原始输入URL中是否显式包含 /v1 路径段
func detectURLHasV1(rawURL string) bool {
	normalized := strings.TrimRight(strings.TrimSpace(rawURL), "/")

	// Check if /v1 appears before a known endpoint suffix (excluding /v1 itself)
	for _, suffix := range llmEndpointSuffixes {
		if suffix == "/v1" {
			continue
		}
		if strings.HasSuffix(normalized, suffix) {
			withoutSuffix := strings.TrimSuffix(normalized, suffix)
			return strings.HasSuffix(strings.TrimRight(withoutSuffix, "/"), "/v1")
		}
	}

	// Check for trailing /v1 directly (includes bare /v1 or /v1 at the end)
	normalized = strings.TrimSuffix(normalized, "/v1")
	return normalized != strings.TrimRight(strings.TrimSpace(rawURL), "/")
}

// BuildLLMEndpointURL 根据存储的根URL和BaseURLHasV1标志构建完整的API端点URL
func BuildLLMEndpointURL(baseURL string, hasV1 bool) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if hasV1 {
		return baseURL + "/v1"
	}
	return baseURL
}

// generateLLMModelCode 生成组织内唯一的模型配置编码。
func generateLLMModelCode() string {
	return fmt.Sprintf("llm_%s", snowflake.GenerateIDBase58())
}

// maskAPIKey 对 API Key 进行脱敏处理，仅保留首尾少量字符。
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

// probeResult 记录连通性探测结果
type probeResult struct {
	v1Success   bool
	noV1Success bool
}

// probeConnectivity 对指定URL进行连通性探测，分别尝试带 /v1 和不带 /v1 的端点。
// 优先探测 preferV1 指示的候选地址，成功后立即返回。
func probeConnectivity(ctx context.Context, provider, modelName, apiKey, baseURL string, preferV1 bool) *probeResult {
	result := &probeResult{}
	baseURL = normalizeLLMBaseURL(baseURL)

	// Build candidate URLs
	withV1URL := BuildLLMEndpointURL(baseURL, true)
	noV1URL := BuildLLMEndpointURL(baseURL, false)

	// Determine probing order: prefer the user-indicated candidate first
	candidates := []struct {
		url    string
		result *bool
	}{
		{withV1URL, &result.v1Success},
		{noV1URL, &result.noV1Success},
	}
	if !preferV1 {
		candidates[0], candidates[1] = candidates[1], candidates[0]
	}

	for _, candidate := range candidates {
		chatModel, err := pkgeino.NewChatModel(ctx, &pkgeino.ChatModelConfig{
			Provider: provider,
			APIKey:   apiKey,
			Model:    modelName,
			BaseURL:  candidate.url,
		})
		if err != nil {
			continue
		}
		flow, err := pkgeino.NewFlow(ctx, &pkgeino.FlowConfig{
			Model:        chatModel,
			SystemPrompt: "connectivity test",
		})
		if err != nil {
			continue
		}
		_, err = flow.Generate(ctx, "ok")
		if err == nil {
			*candidate.result = true
			return result
		}
	}

	return result
}

// --- ManagerDb 实现 ---

// ManagerDb 是基于 gorm 的 Manager 接口实现，
// 封装 LLM 模型配置的 CRUD 和连通性测试能力。
type ManagerDb struct {
	db        *gorm.DB
	probeFunc func(ctx context.Context, provider, modelName, apiKey, baseURL string, preferV1 bool) *probeResult
}

// NewManager 创建一个基于 gorm 的 Manager 实现。
func NewManager(db *gorm.DB) *ManagerDb {
	return &ManagerDb{db: db, probeFunc: probeConnectivity}
}

var _ Manager = (*ManagerDb)(nil)

// Create 创建一条新的 LLM 模型配置。
// orgID 由调用方从认证上下文中解析后传入。
func (m *ManagerDb) Create(ctx context.Context, orgID uint, req *CreateRequest) (*ModelConfig, error) {
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
	name := req.Name
	if name == "" {
		name = req.Model
	}
	provider := req.Provider
	if provider == "" {
		provider = string(types.LLMProviderOpenAI)
	}
	baseURL := normalizeLLMBaseURL(req.BaseURL)
	hasV1 := detectURLHasV1(req.BaseURL)

	var probeResult *probeResult
	if provider == string(types.LLMProviderOpenAI) || provider == string(types.LLMProviderCustom) {
		probeResult = m.probeFunc(ctx, provider, req.Model, req.APIKey, baseURL, hasV1)
		if probeResult == nil || (!probeResult.v1Success && !probeResult.noV1Success) {
			return nil, errors.New("connectivity test failed: could not connect with or without /v1 prefix, check base_url, api_key and network")
		}
		hasV1 = probeResult.v1Success
	}

	status := req.Status
	if status == "" {
		status = string(types.LLMModelStatusActive)
	}

	model := &types.LLMModel{
		OrgID:           orgID,
		Code:            code,
		Name:            name,
		Description:     req.Description,
		Provider:        provider,
		ModelName:       req.Model,
		BaseURL:         baseURL,
		BaseURLHasV1:    hasV1,
		APIKeyEncrypted: req.APIKey,
		APIKeyMasked:    maskAPIKey(req.APIKey),
		MaxTokens:       4096,
		Temperature:     0.7,
		TimeoutSec:      120,
		Status:          status,
		IsDefault:       req.IsDefault,
		Config:          types.LLMModelConfig(req.Config),
	}

	if err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !model.IsDefault {
			hasModels, err := db.OrgHasLLMModels(ctx, tx, orgID)
			if err != nil {
				return err
			}
			model.IsDefault = !hasModels
		}
		if model.IsDefault {
			if err := db.ClearOrgDefaultLLMModels(ctx, tx, orgID, 0); err != nil {
				return err
			}
		}
		return db.CreateLLMModel(ctx, tx, model)
	}); err != nil {
		return nil, err
	}
	return modelConfigFromEntity(model), nil
}

// Get 按 ID 或 Code 获取单个模型配置，orgID 用于校验归属。
func (m *ManagerDb) Get(ctx context.Context, orgID uint, id uint, code string) (*ModelConfig, error) {
	var model *types.LLMModel
	var err error
	if id > 0 {
		model, err = db.GetLLMModelByID(ctx, m.db, id)
	} else if code != "" {
		model, err = db.GetLLMModelByCode(ctx, m.db, orgID, code)
	} else {
		return nil, errors.New("id or code is required")
	}
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, errors.New("llm model not found")
	}
	if model.OrgID != orgID {
		return nil, errors.New("permission denied")
	}
	return modelConfigFromEntity(model), nil
}

// GetDefault 获取组织默认模型配置。
func (m *ManagerDb) GetDefault(ctx context.Context, orgID uint) (*ModelConfig, error) {
	model, err := db.GetDefaultLLMModel(ctx, m.db, orgID)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, errors.New("llm model not found")
	}
	return modelConfigFromEntity(model), nil
}

// GetByModelName 按模型名称获取配置，orgID 用于校验归属。
func (m *ManagerDb) GetByModelName(ctx context.Context, orgID uint, modelName string) (*ModelConfig, error) {
	model, err := db.GetLLMModelByModelName(ctx, m.db, orgID, modelName)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, errors.New("llm model not found")
	}
	return modelConfigFromEntity(model), nil
}

// GetByModelCode 按 code 获取模型配置。
func (m *ManagerDb) GetByModelCode(ctx context.Context, orgID uint, code string) (*ModelConfig, error) {
	model, err := db.GetLLMModelByCode(ctx, m.db, orgID, code)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, errors.New("llm model not found")
	}
	return modelConfigFromEntity(model), nil
}

// GetByModelID 按主键 ID 获取模型配置。
func (m *ManagerDb) GetByModelID(ctx context.Context, orgID uint, modelID uint) (*ModelConfig, error) {
	model, err := db.GetLLMModelByID(ctx, m.db, modelID)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, errors.New("llm model not found")
	}
	return modelConfigFromEntity(model), nil
}

// Update 更新指定 ID 的模型配置，orgID 用于校验归属。
// 当 provider/model/baseURL/apiKey 变更时重新执行连通性探测。
func (m *ManagerDb) Update(ctx context.Context, orgID uint, id uint, req *UpdateRequest) (*ModelConfig, error) {
	var model *types.LLMModel
	if err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		model, err = db.GetLLMModelByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if model == nil {
			return errors.New("llm model not found")
		}
		if model.OrgID != orgID {
			return errors.New("permission denied")
		}

		needsReDetect := false

		if req.Name != "" {
			model.Name = req.Name
		}
		if req.Description != nil {
			model.Description = *req.Description
		}
		if req.Provider != "" {
			model.Provider = req.Provider
			needsReDetect = true
		}
		if req.Model != "" {
			model.ModelName = req.Model
			needsReDetect = true
		}
		if req.BaseURL != nil {
			model.BaseURL = normalizeLLMBaseURL(*req.BaseURL)
			needsReDetect = true
		}
		if req.APIKey != nil {
			model.APIKeyEncrypted = *req.APIKey
			model.APIKeyMasked = maskAPIKey(*req.APIKey)
			needsReDetect = true
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
				if err := db.ClearOrgDefaultLLMModels(ctx, tx, orgID, model.ID); err != nil {
					return err
				}
			}
		}

		if needsReDetect {
			provider := model.Provider
			if provider == string(types.LLMProviderOpenAI) || provider == string(types.LLMProviderCustom) {
				probeResult := m.probeFunc(ctx, provider, model.ModelName, model.APIKeyEncrypted, model.BaseURL, model.BaseURLHasV1)
				if probeResult == nil || (!probeResult.v1Success && !probeResult.noV1Success) {
					return errors.New("connectivity test failed after update: could not connect with or without /v1 prefix, check the updated fields")
				}
				model.BaseURLHasV1 = probeResult.v1Success
			} else {
				// For non-OpenAI providers, keep existing flag or detect from URL path pattern
				hasV1 := detectURLHasV1(model.BaseURL + "/v1/chat/completions")
				model.BaseURLHasV1 = hasV1
			}
		}

		return db.UpdateLLMModel(ctx, tx, model)
	}); err != nil {
		return nil, err
	}
	return modelConfigFromEntity(model), nil
}

// Delete 删除指定 ID 的模型配置，orgID 用于校验归属。
func (m *ManagerDb) Delete(ctx context.Context, orgID uint, id uint) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		model, err := db.GetLLMModelByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if model == nil {
			return errors.New("llm model not found")
		}
		if model.OrgID != orgID {
			return errors.New("permission denied")
		}
		return db.DeleteLLMModel(ctx, tx, id)
	})
}

// List 按分页和过滤条件查询组织内模型配置列表。
func (m *ManagerDb) List(ctx context.Context, orgID uint, req *ListRequest) (*ListModelResult, error) {
	opt := types.NewPageQuery(types.Caller{OrgID: orgID}, req.Offset, req.Limit)
	if req.Provider != nil && *req.Provider != "" {
		opt.AddFilter("provider", *req.Provider)
	}
	if req.Status != nil && *req.Status != "" {
		opt.AddFilter("status", *req.Status)
	}
	if req.Keyword != nil && *req.Keyword != "" {
		opt.AddFilter("keyword", *req.Keyword)
	}

	models, total, err := db.ListLLMModels(ctx, m.db, opt)
	if err != nil {
		return nil, err
	}

	items := make([]*ModelConfig, 0, len(models))
	for _, model := range models {
		items = append(items, modelConfigFromEntity(model))
	}
	return &ListModelResult{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

// TestConnectivity 测试指定模型配置或临时配置的连通性。
// 当 req.ID 非 nil 时按已有配置测试，否则使用 req 其余字段指定的临时配置测试。
func (m *ManagerDb) TestConnectivity(ctx context.Context, orgID uint, req *TestRequest) (*TestResult, error) {
	baseURL := strings.TrimSpace(req.BaseURL)
	apiKey := strings.TrimSpace(req.APIKey)
	provider := strings.TrimSpace(req.Provider)
	modelName := strings.TrimSpace(req.Model)
	var baseURLHasV1 bool
	if req.ID != nil || req.Code != "" {
		var model *types.LLMModel
		var err error
		if req.ID != nil {
			model, err = db.GetLLMModelByID(ctx, m.db, *req.ID)
		} else {
			model, err = db.GetLLMModelByCode(ctx, m.db, orgID, req.Code)
		}
		if err != nil {
			return nil, err
		}
		if model == nil {
			return nil, errors.New("llm model not found")
		}
		if model.OrgID != orgID {
			return nil, errors.New("permission denied")
		}
		baseURL = model.BaseURL
		baseURLHasV1 = model.BaseURLHasV1
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

	// Build endpoint URL using the stored flag
	endpointURL := BuildLLMEndpointURL(baseURL, baseURLHasV1)

	start := time.Now()
	chatModel, err := pkgeino.NewChatModel(ctx, &pkgeino.ChatModelConfig{
		Provider: provider,
		APIKey:   apiKey,
		Model:    modelName,
		BaseURL:  endpointURL,
	})
	latencyMS := time.Since(start).Milliseconds()
	if err != nil {
		return &TestResult{
			Success:      false,
			Message:      err.Error(),
			Endpoint:     endpointURL,
			LatencyMS:    latencyMS,
			BaseURLHasV1: baseURLHasV1,
		}, nil
	}

	flow, err := pkgeino.NewFlow(ctx, &pkgeino.FlowConfig{
		Model:        chatModel,
		SystemPrompt: "You are testing Leros LLM connectivity. Reply with only: ok",
	})
	if err != nil {
		return &TestResult{
			Success:      false,
			Message:      err.Error(),
			Endpoint:     endpointURL,
			LatencyMS:    time.Since(start).Milliseconds(),
			BaseURLHasV1: baseURLHasV1,
		}, nil
	}

	message, err := flow.Generate(ctx, "Reply with only: ok")
	latencyMS = time.Since(start).Milliseconds()
	if err != nil {
		return &TestResult{
			Success:      false,
			Message:      err.Error(),
			Endpoint:     endpointURL,
			LatencyMS:    latencyMS,
			BaseURLHasV1: baseURLHasV1,
		}, nil
	}
	responseMessage := "model call succeeded"
	if message != nil && strings.TrimSpace(message.Content) != "" {
		responseMessage = strings.TrimSpace(message.Content)
	}
	return &TestResult{
		Success:      true,
		Message:      responseMessage,
		Endpoint:     endpointURL,
		LatencyMS:    latencyMS,
		BaseURLHasV1: baseURLHasV1,
	}, nil
}

// ResolveDefaultLLMModel 解析组织的默认 LLM 模型。
// 若组织缺少系统模型，先从 org_id=1 克隆再查询。
func ResolveDefaultLLMModel(ctx context.Context, database *gorm.DB, orgID uint) (*types.LLMModel, error) {
	model, err := db.GetDefaultLLMModel(ctx, database, orgID)
	if err != nil {
		return nil, err
	}
	if model != nil {
		return model, nil
	}

	cloned, err := db.EnsureOrgSystemLLMModels(ctx, database, orgID)
	if err != nil {
		logs.WarnContextf(ctx, "[llm] ensure system models for org %d: %v", orgID, err)
		return nil, nil
	}
	if !cloned {
		return nil, nil
	}
	return db.GetDefaultLLMModel(ctx, database, orgID)
}

// ResolveSystemTranslationLLMModel 解析组织的系统翻译 LLM 模型。
// 若组织缺少系统模型，先从 org_id=1 克隆再查询。
func ResolveSystemTranslationLLMModel(ctx context.Context, database *gorm.DB, orgID uint) (*types.LLMModel, error) {
	model, err := db.GetSystemTranslationLLMModel(ctx, database, orgID)
	if err != nil {
		return nil, err
	}
	if model != nil {
		return model, nil
	}

	if orgID == 1 {
		return nil, nil
	}

	cloned, err := db.EnsureOrgSystemLLMModels(ctx, database, orgID)
	if err != nil {
		logs.WarnContextf(ctx, "[llm] ensure system models for org %d: %v", orgID, err)
		return db.GetSystemTranslationLLMModel(ctx, database, 1)
	}
	if !cloned {
		return db.GetSystemTranslationLLMModel(ctx, database, 1)
	}
	return db.GetSystemTranslationLLMModel(ctx, database, orgID)
}
