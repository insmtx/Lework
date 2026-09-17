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
	sampling := types.SamplingParamsFromConfig(m.Config)
	return &ModelConfig{
		ID:               m.ID,
		OrgID:            m.OrgID,
		Code:             m.Code,
		Name:             m.Name,
		Description:      m.Description,
		Provider:         m.Provider,
		ModelName:        m.ModelName,
		BaseURL:          m.BaseURL,
		BaseURLHasV1:     m.BaseURLHasV1,
		APIKey:           m.APIKeyEncrypted,
		MaxTokens:        m.MaxTokens,
		Temperature:      m.Temperature,
		TimeoutSec:       m.TimeoutSec,
		Status:           m.Status,
		Purpose:          m.Purpose,
		IsDefault:        m.IsDefault,
		IsSystem:         m.IsSystem,
		Config:           map[string]any(m.Config),
		Vision:           VisionFromConfig(m.Config),
		TopP:             sampling.TopP,
		FrequencyPenalty: sampling.FrequencyPenalty,
		PresencePenalty:  sampling.PresencePenalty,
		ContextLimit:     contextLimit(sampling.Limit),
		OutputLimit:      outputLimit(sampling.Limit),
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
}

func contextLimit(l *types.LLMLimitFields) int {
	if l == nil {
		return 0
	}
	return l.Context
}

func outputLimit(l *types.LLMLimitFields) int {
	if l == nil {
		return 0
	}
	return l.Output
}

// VisionFromConfig 从扩展配置中解包视觉能力标志。
// 缺省（无 config 或未声明 vision）即 false。仅在此处触碰 map。
func VisionFromConfig(config types.LLMModelConfig) bool {
	if len(config) == 0 {
		return false
	}
	v, ok := config["vision"].(bool)
	return ok && v
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

// NormalizeLLMBaseURL 导出规范化包装，供 seed 等跨包复用。
func NormalizeLLMBaseURL(baseURL string) string { return normalizeLLMBaseURL(baseURL) }

// DetectURLHasV1 导出 /v1 路径段探测包装，供 seed 等跨包在无法实测连通性时推断前缀。
// 必须在规范化之前调用：normalizeLLMBaseURL 会剥掉 /v1。
func DetectURLHasV1(rawURL string) bool { return detectURLHasV1(rawURL) }

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

// ProbeResult 记录连通性探测结果，供其他包（如 seed）在构建模型配置时判定上游是否需要 /v1 前缀。
type ProbeResult struct {
	V1Success   bool
	NoV1Success bool
}

// probeResult 记录连通性探测结果
type probeResult = ProbeResult

// ProbeConnectivity 对指定URL进行连通性探测，分别尝试带 /v1 和不带 /v1 的端点。
// 优先探测 preferV1 指示的候选地址，成功后立即返回。
func ProbeConnectivity(ctx context.Context, provider, modelName, apiKey, baseURL string, preferV1 bool) *ProbeResult {
	return probeConnectivity(ctx, provider, modelName, apiKey, baseURL, preferV1)
}

// probeConnectivity 对指定URL进行连通性探测，分别尝试带 /v1 和不带 /v1 的端点。
// 优先探测 preferV1 指示的候选地址，成功后立即返回。
func probeConnectivity(ctx context.Context, provider, modelName, apiKey, baseURL string, preferV1 bool) *ProbeResult {
	result := &ProbeResult{}
	baseURL = normalizeLLMBaseURL(baseURL)

	// Build candidate URLs
	withV1URL := BuildLLMEndpointURL(baseURL, true)
	noV1URL := BuildLLMEndpointURL(baseURL, false)

	// Determine probing order: prefer the user-indicated candidate first
	candidates := []struct {
		url    string
		result *bool
	}{
		{withV1URL, &result.V1Success},
		{noV1URL, &result.NoV1Success},
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
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("name is required")
	}
	if req.Purpose == "" {
		return nil, errors.New("purpose is required")
	}

	code := generateLLMModelCode()
	name := strings.TrimSpace(req.Name)
	provider := req.Provider
	if provider == "" {
		provider = string(types.LLMProviderOpenAI)
	}
	baseURL := normalizeLLMBaseURL(req.BaseURL)
	hasV1 := detectURLHasV1(req.BaseURL)

	var probeResult *probeResult
	if provider == string(types.LLMProviderOpenAI) || provider == string(types.LLMProviderCustom) {
		probeResult = m.probeFunc(ctx, provider, req.Model, req.APIKey, baseURL, hasV1)
		if probeResult == nil || (!probeResult.V1Success && !probeResult.NoV1Success) {
			return nil, errors.New("connectivity test failed: could not connect with or without /v1 prefix, check base_url, api_key and network")
		}
		hasV1 = probeResult.V1Success
	}

	status := req.Status
	if status == "" {
		status = string(types.LLMModelStatusActive)
	}

	purpose := req.Purpose

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
		Purpose:         purpose,
		IsDefault:       req.IsDefault,
		Config:          types.LLMModelConfig(req.Config),
	}
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		model.MaxTokens = *req.MaxTokens
	}
	if req.Temperature != nil {
		model.Temperature = *req.Temperature
	}

	if err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !model.IsDefault {
			hasModels, err := db.OrgHasLLMModels(ctx, tx, orgID, purpose)
			if err != nil {
				return err
			}
			model.IsDefault = !hasModels
		}
		if model.IsDefault {
			if err := db.ClearOrgDefaultLLMModels(ctx, tx, orgID, 0, purpose); err != nil {
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

		// 启用中的模型不可编辑业务配置（启用/禁用走独立的 SetStatus 接口）。
		isEditOperation := req.Name != "" || req.Description != nil || req.Provider != "" ||
			req.Model != "" || req.BaseURL != nil || req.APIKey != nil || req.Config != nil ||
			req.MaxTokens != nil || req.Temperature != nil || req.Purpose != nil
		if isEditOperation {
			// 编辑业务配置时名称与用途必填，不允许保留旧值或清空。
			if req.Name == "" {
				return errors.New("name is required")
			}
			if req.Purpose == nil || *req.Purpose == "" {
				return errors.New("purpose is required")
			}
		}
		if isEditOperation && model.Status == string(types.LLMModelStatusActive) {
			return errors.New("启用中的模型不可编辑，请先禁用")
		}
		// 只能将启用中的模型设为默认。
		if req.IsDefault != nil && *req.IsDefault && model.Status != string(types.LLMModelStatusActive) {
			return errors.New("只能将启用中的模型设为默认")
		}

		needsReDetect := false

		if req.Name != "" {
			model.Name = strings.TrimSpace(req.Name)
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
		if req.Config != nil {
			model.Config = types.LLMModelConfig(*req.Config)
		}
		if req.MaxTokens != nil {
			model.MaxTokens = *req.MaxTokens
		}
		if req.Temperature != nil {
			model.Temperature = *req.Temperature
		}
		prevPurpose := model.Purpose
		if req.Purpose != nil {
			model.Purpose = *req.Purpose
		}
		if req.IsDefault != nil {
			prevDefault := model.IsDefault
			model.IsDefault = *req.IsDefault
			if prevPurpose != model.Purpose && prevDefault {
				// 默认模型变更用途：需保证原用途仍有一个默认，否则回填或拒绝。
				if err := m.backfillOrRejectDefault(ctx, tx, orgID, prevPurpose, model.ID); err != nil {
					return err
				}
			}
			if model.IsDefault {
				if err := db.ClearOrgDefaultLLMModels(ctx, tx, orgID, model.ID, model.Purpose); err != nil {
					return err
				}
			} else if prevDefault {
				// 取消用途内唯一默认：需保证该用途仍有一个默认，否则回填或拒绝。
				if err := m.backfillOrRejectDefault(ctx, tx, orgID, model.Purpose, model.ID); err != nil {
					return err
				}
			}
		}

		if needsReDetect {
			provider := model.Provider
			if provider == string(types.LLMProviderOpenAI) || provider == string(types.LLMProviderCustom) {
				probeResult := m.probeFunc(ctx, provider, model.ModelName, model.APIKeyEncrypted, model.BaseURL, model.BaseURLHasV1)
				if probeResult == nil || (!probeResult.V1Success && !probeResult.NoV1Success) {
					return errors.New("connectivity test failed after update: could not connect with or without /v1 prefix, check the updated fields")
				}
				model.BaseURLHasV1 = probeResult.V1Success
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

// setDefault 在事务内将指定 ID 的模型设为该用途下的默认模型，orgID 用于校验归属。
// 仅允许将启用中的模型设为默认；同一用途下其他模型默认标记会被清除，保证唯一性。
func (m *ManagerDb) setDefault(ctx context.Context, tx *gorm.DB, orgID uint, id uint) (*types.LLMModel, error) {
	model, err := db.GetLLMModelByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, errors.New("llm model not found")
	}
	if model.OrgID != orgID {
		return nil, errors.New("permission denied")
	}
	// 只能将启用中的模型设为默认。
	if model.Status != string(types.LLMModelStatusActive) {
		return nil, errors.New("只能将启用中的模型设为默认")
	}
	// 该用途下其他默认模型降为非默认，保证每用途有且仅有一个默认。
	if err := db.ClearOrgDefaultLLMModels(ctx, tx, orgID, model.ID, model.Purpose); err != nil {
		return nil, err
	}
	model.IsDefault = true
	if err := db.UpdateLLMModel(ctx, tx, model); err != nil {
		return nil, err
	}
	return model, nil
}

// SetDefault 设指定 ID 的模型为该用途下的默认模型，orgID 用于校验归属。
func (m *ManagerDb) SetDefault(ctx context.Context, orgID uint, id uint) (*ModelConfig, error) {
	var model *types.LLMModel
	if err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		model, err = m.setDefault(ctx, tx, orgID, id)
		return err
	}); err != nil {
		return nil, err
	}
	return modelConfigFromEntity(model), nil
}

// SetStatus 启用或禁用指定 ID 的模型配置，orgID 用于校验归属。
// 仅允许合法状态：LLMModelStatusActive 与 LLMModelStatusInactive。
// 禁用默认模型前需保证该用途仍有一个启用中的默认，否则回填或拒绝。
func (m *ManagerDb) SetStatus(ctx context.Context, orgID uint, id uint, status string) (*ModelConfig, error) {
	if status != string(types.LLMModelStatusActive) && status != string(types.LLMModelStatusInactive) {
		return nil, errors.New("invalid status")
	}
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

		if status == string(types.LLMModelStatusInactive) && model.IsDefault {
			// 禁用默认模型前需保证该用途仍有一个启用中的默认，否则拒绝禁用。
			if err := m.backfillOrRejectDefault(ctx, tx, orgID, model.Purpose, model.ID); err != nil {
				return err
			}
			model.IsDefault = false
		}
		model.Status = status
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
		if model.IsSystem {
			return errors.New("系统内置模型不可删除")
		}
		if model.Status == string(types.LLMModelStatusActive) {
			return errors.New("启用中的模型不可删除，请先禁用")
		}
		if model.IsDefault {
			// 删除用途内唯一默认前，需保证该用途删除后仍有一个默认。
			if err := m.backfillOrRejectDefault(ctx, tx, orgID, model.Purpose, model.ID); err != nil {
				return err
			}
		}
		return db.DeleteLLMModel(ctx, tx, id)
	})
}

// backfillOrRejectDefault 在取消/删除一个默认模型时，保证目标用途仍保留一个默认。
// 若该用途除 excludeID 外仍存在默认模型，直接返回成功；否则尝试把该用途内其余任一 active 模型设为默认；
// 若该用途已无其他活跃模型，则拒绝该操作并返回错误。
// 仅在事务内调用。
func (m *ManagerDb) backfillOrRejectDefault(ctx context.Context, tx *gorm.DB, orgID uint, purpose types.LLMModelPurpose, excludeID uint) error {
	var otherDefault int64
	q := db.QueryByPurpose(tx.Model(&types.LLMModel{}), purpose).
		Where("org_id = ? AND is_default = ? AND status = ?", orgID, true, string(types.LLMModelStatusActive)).
		Where("id != ?", excludeID)
	if err := q.Count(&otherDefault).Error; err != nil {
		return err
	}
	if otherDefault > 0 {
		return nil
	}

	var candidate types.LLMModel
	q2 := db.QueryByPurpose(tx.Model(&types.LLMModel{}), purpose).
		Where("org_id = ? AND status = ?", orgID, string(types.LLMModelStatusActive)).
		Where("id != ?", excludeID).
		Order("updated_at DESC").
		First(&candidate)
	if q2.Error != nil {
		if errors.Is(q2.Error, gorm.ErrRecordNotFound) {
			return errors.New("该用途下没有其他启用中的模型可设为默认，无法禁用当前默认模型")
		}
		return q2.Error
	}
	if err := db.ClearOrgDefaultLLMModels(ctx, tx, orgID, candidate.ID, purpose); err != nil {
		return err
	}
	candidate.IsDefault = true
	return db.UpdateLLMModel(ctx, tx, &candidate)
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
	if req.Purpose != nil && *req.Purpose != "" {
		opt.AddFilter("purpose", *req.Purpose)
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

	// 按已知（或猜测的）前缀构造端点。
	endpointURL := BuildLLMEndpointURL(baseURL, baseURLHasV1)

	start := time.Now()
	responseMessage, callErr := callConnectivityEndpoint(ctx, provider, apiKey, modelName, endpointURL)

	// 前缀猜错时换另一侧重试一次。临时配置（未落库）没有 /v1 首选项，
	// BaseURLHasV1 恒为零值 false；若首次调用因"请求路径不对"失败——上游把网关首页
	// 当成 API 响应返回（如 New API / one-api），或该路径 404——说明前缀猜错了。
	// 只有真正调通才采纳重试结果，否则仍返回首次调用的错误。
	if callErr != nil && supportsV1PrefixSwitch(provider) && isEndpointPathError(callErr) {
		altHasV1 := !baseURLHasV1
		altEndpointURL := BuildLLMEndpointURL(baseURL, altHasV1)
		if altMessage, altErr := callConnectivityEndpoint(ctx, provider, apiKey, modelName, altEndpointURL); altErr == nil {
			endpointURL, baseURLHasV1, responseMessage, callErr = altEndpointURL, altHasV1, altMessage, nil
		}
	}

	latencyMS := time.Since(start).Milliseconds()
	if callErr != nil {
		return &TestResult{
			Success:      false,
			Message:      connectivityErrorMessage(callErr),
			Endpoint:     endpointURL,
			LatencyMS:    latencyMS,
			BaseURLHasV1: baseURLHasV1,
		}, nil
	}
	return &TestResult{
		Success:      true,
		Message:      responseMessage,
		Endpoint:     endpointURL,
		LatencyMS:    latencyMS,
		BaseURLHasV1: baseURLHasV1,
	}, nil
}

const (
	// connectivityTestSystemPrompt 连通性测试的系统提示词。
	connectivityTestSystemPrompt = "You are testing Leros LLM connectivity. Reply with only: ok"
	// connectivityTestUserPrompt 连通性测试的用户输入。
	connectivityTestUserPrompt = "Reply with only: ok"
	// connectivityTestSuccessMessage 上游未返回文本内容时的兜底成功提示。
	connectivityTestSuccessMessage = "model call succeeded"
)

// callConnectivityEndpoint 用指定端点执行一次连通性调用，返回模型回复文本。
// 与 TestConnectivity 共用同一套提示词，保证不同前缀的两次调用可比。
func callConnectivityEndpoint(ctx context.Context, provider, apiKey, modelName, endpointURL string) (string, error) {
	chatModel, err := pkgeino.NewChatModel(ctx, &pkgeino.ChatModelConfig{
		Provider: provider,
		APIKey:   apiKey,
		Model:    modelName,
		BaseURL:  endpointURL,
	})
	if err != nil {
		return "", err
	}

	flow, err := pkgeino.NewFlow(ctx, &pkgeino.FlowConfig{
		Model:        chatModel,
		SystemPrompt: connectivityTestSystemPrompt,
	})
	if err != nil {
		return "", err
	}

	message, err := flow.Generate(ctx, connectivityTestUserPrompt)
	if err != nil {
		return "", err
	}
	if message != nil && strings.TrimSpace(message.Content) != "" {
		return strings.TrimSpace(message.Content), nil
	}
	return connectivityTestSuccessMessage, nil
}

// supportsV1PrefixSwitch 判断该 provider 是否走 OpenAI 兼容协议。
// 只有这类上游使用 ".../v1/chat/completions" 形式的路径，切换 /v1 前缀才有意义；
// anthropic 等协议由 SDK 自行拼接路径前缀，切换反而会拼出错误地址。
func supportsV1PrefixSwitch(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case pkgeino.ProviderOpenAI, pkgeino.ProviderCustom, pkgeino.ProviderDeepSeek,
		pkgeino.ProviderQwen, pkgeino.ProviderGemini, pkgeino.ProviderArk, pkgeino.ProviderOpenRouter:
		return true
	default:
		return false
	}
}

// isEndpointPathError 判断错误是否由"请求路径不对"引起，从而可以通过切换 /v1 前缀修复：
// 上游返回 HTML 页面而非 JSON（网关首页兜底路由），或该路径不存在（404）。
// 其余错误（401 鉴权失败、模型名不存在等）重试另一种前缀没有意义。
func isEndpointPathError(err error) bool {
	if err == nil {
		return false
	}
	if pkgeino.IsUpstreamNotJSON(err) {
		return true
	}
	msg := err.Error()
	// 兼容未经过响应守卫的链路：JSON 解析器读到 HTML 首页时报出的语法错误。
	return strings.Contains(msg, "invalid character '<'") || strings.Contains(msg, "404")
}

// connectivityErrorMessage 把响应守卫产生的错误压平成单条可读信息。
// TestResult.Message 会直接展示给用户，因此去掉 eino/SDK 的包裹前缀
// （"failed to create chat completion"、node path 等），只保留定位问题所需的内容。
func connectivityErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var notJSON *pkgeino.UpstreamNotJSONError
	if errors.As(err, &notJSON) {
		return notJSON.Error()
	}
	return err.Error()
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

// ResolveSystemTranslationLLMModel resolves an active translation model owned by orgID.
// A missing model is cloned from the system seed organization into orgID; cross-org
// fallback is not allowed because model execution is authorized per organization.
func ResolveSystemTranslationLLMModel(ctx context.Context, database *gorm.DB, orgID uint) (*types.LLMModel, error) {
	if orgID == 0 {
		return nil, errors.New("organization is required")
	}
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

	_, err = db.EnsureOrgSystemTranslationLLMModel(ctx, database, orgID)
	if err != nil {
		return nil, fmt.Errorf("ensure system translation model for org %d: %w", orgID, err)
	}
	return db.GetSystemTranslationLLMModel(ctx, database, orgID)
}
