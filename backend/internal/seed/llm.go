package seed

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/llm"
	"github.com/insmtx/Leros/backend/types"
)

// defaultLLMModelCode 是初始化时写入的默认 LLM 模型 code。
const defaultLLMModelCode = "llm_default"

// probeLLMHasV1Fn 实测 base_url 是否需上游 /v1 前缀。包级变量以便测试替换为 mock。
var probeLLMHasV1Fn = llm.ProbeConnectivity

// resolveLLMHasV1 实测 base_url 是否需要上游 /v1 前缀；探测全部失败返回 error，阻断初始化。
func resolveLLMHasV1(ctx context.Context, provider, modelName, apiKey, baseURL string, preferV1 bool) (bool, error) {
	result := probeLLMHasV1Fn(ctx, provider, modelName, apiKey, baseURL, preferV1)
	if result == nil || (!result.V1Success && !result.NoV1Success) {
		return false, fmt.Errorf("llm connectivity probe failed for base_url=%q", baseURL)
	}
	return result.V1Success, nil
}

// preferV1ForBaseURL 根据 base_url 是否显式带 /v1 后缀，确定优先探测候选；缺省优先不带 /v1。
func preferV1ForBaseURL(baseURL string) bool {
	return strings.HasSuffix(strings.TrimRight(strings.TrimSpace(baseURL), "/"), "/v1")
}

// seedLLM 初始化系统级默认 LLM 模型与内置翻译模型（org_id=1）。幂等。
func seedLLM(ctx context.Context, db *gorm.DB, llmCfg *config.LLMConfig) error {
	var modelCount int64
	if err := db.WithContext(ctx).Model(&types.LLMModel{}).Count(&modelCount).Error; err != nil {
		return err
	}
	if modelCount == 0 && llmCfg != nil && llmCfg.APIKey != "" {
		modelName := llmCfg.Model
		if modelName == "" {
			modelName = "default"
		}
		baseURL := llm.NormalizeLLMBaseURL(llmCfg.BaseURL)
		hasV1, err := resolveLLMHasV1(ctx, llmCfg.Provider, modelName, llmCfg.APIKey, baseURL, preferV1ForBaseURL(baseURL))
		if err != nil {
			return err
		}
		defaultLLMModel := &types.LLMModel{
			OrgID:           1,
			Code:            defaultLLMModelCode,
			Name:            "内置对话模型",
			Description:     "Default LLM model from config",
			Provider:        llmCfg.Provider,
			ModelName:       modelName,
			BaseURL:         baseURL,
			BaseURLHasV1:    hasV1,
			APIKeyEncrypted: llmCfg.APIKey,
			APIKeyMasked:    maskAPIKey(llmCfg.APIKey),
			MaxTokens:       4096,
			Temperature:     0.7,
			TimeoutSec:      120,
			Status:          string(types.LLMModelStatusActive),
			Purpose:         types.LLMModelPurposeConversation,
			IsDefault:       true,
			IsSystem:        true,
		}
		cfg := types.LLMModelConfig{}
		if llmCfg.Vision {
			cfg["vision"] = true
		}
		if llmCfg.TopP != nil {
			cfg["top_p"] = *llmCfg.TopP
		}
		if llmCfg.FrequencyPenalty != nil {
			cfg["frequency_penalty"] = *llmCfg.FrequencyPenalty
		}
		if llmCfg.PresencePenalty != nil {
			cfg["presence_penalty"] = *llmCfg.PresencePenalty
		}
		if llmCfg.Limit != nil && (llmCfg.Limit.Context > 0 || llmCfg.Limit.Output > 0) {
			limit := map[string]interface{}{}
			if llmCfg.Limit.Context > 0 {
				limit["context"] = llmCfg.Limit.Context
			}
			if llmCfg.Limit.Output > 0 {
				limit["output"] = llmCfg.Limit.Output
			}
			cfg["limit"] = limit
			if llmCfg.Limit.Output > 0 {
				defaultLLMModel.MaxTokens = llmCfg.Limit.Output
			}
		}
		if len(cfg) > 0 {
			defaultLLMModel.Config = cfg
		}
		if err := infradb.CreateLLMModel(ctx, db, defaultLLMModel); err != nil {
			return err
		}
		logs.Infof("seed: default LLM model created (provider=%s, model=%s)", llmCfg.Provider, modelName)
	}

	if err := seedSystemTranslationLLMModel(ctx, db, llmCfg); err != nil {
		return err
	}
	return nil
}

// seedSystemTranslationLLMModel 初始化内置翻译模型（org_id=1），作为系统级 fallback 源。
func seedSystemTranslationLLMModel(ctx context.Context, db *gorm.DB, llmCfg *config.LLMConfig) error {
	spec, ok, err := buildSystemTranslationLLMModelSpec(ctx, llmCfg)
	if err != nil {
		return err
	}
	if !ok {
		logs.Warn("seed: system translation LLM model skipped: no api_key configured")
		return nil
	}

	var existing types.LLMModel
	err = db.WithContext(ctx).Where("org_id = ? AND code = ?", spec.OrgID, spec.Code).First(&existing).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := infradb.CreateLLMModel(ctx, db, spec); err != nil {
			return err
		}
		logs.Infof("seed: system translation LLM model created (provider=%s, model=%s)", spec.Provider, spec.ModelName)
		return nil
	}

	if !existing.IsSystem {
		logs.Warnf("seed: system translation LLM model skipped: code %q occupied by non-system model", spec.Code)
		return nil
	}
	logs.Infof("seed: system translation LLM model already exists, skip (provider=%s, model=%s)", existing.Provider, existing.ModelName)
	return nil
}

// buildSystemTranslationLLMModelSpec 构造内置翻译模型描述。返回 ok=false 表示无需构造。
// 构造前会对 base_url 做一次连通性探测以确定 /v1 前缀，探测失败返回 error 阻断初始化。
func buildSystemTranslationLLMModelSpec(ctx context.Context, llmCfg *config.LLMConfig) (spec *types.LLMModel, ok bool, err error) {
	if llmCfg == nil {
		return nil, false, nil
	}
	provider := strings.TrimSpace(string(types.LLMProviderDeepSeek))
	modelName := "deepseek-v4-flash"
	baseURL := strings.TrimSpace(llmCfg.BaseURL)
	apiKey := strings.TrimSpace(llmCfg.APIKey)
	isDefault := true
	var isDefaultOverride *bool

	if llmCfg.Translation != nil {
		if v := strings.TrimSpace(llmCfg.Translation.Provider); v != "" {
			provider = v
		}
		if v := strings.TrimSpace(llmCfg.Translation.Model); v != "" {
			modelName = v
		}
		if v := strings.TrimSpace(llmCfg.Translation.BaseURL); v != "" {
			baseURL = v
		}
		if v := strings.TrimSpace(llmCfg.Translation.APIKey); v != "" {
			apiKey = v
		}
		isDefaultOverride = llmCfg.Translation.IsDefault
	}
	if isDefaultOverride != nil {
		isDefault = *isDefaultOverride
	}

	if apiKey == "" {
		return nil, false, nil
	}

	baseURL = llm.NormalizeLLMBaseURL(baseURL)
	baseURLHasV1, err := resolveLLMHasV1(ctx, provider, modelName, apiKey, baseURL, preferV1ForBaseURL(baseURL))
	if err != nil {
		return nil, false, err
	}

	return &types.LLMModel{
		OrgID:           1,
		Code:            infradb.SystemTranslationLLMModelCode,
		Name:            "内置翻译模型",
		Description:     "用于 Skill 描述和文档翻译的快速系统模型",
		Provider:        provider,
		ModelName:       modelName,
		BaseURL:         baseURL,
		BaseURLHasV1:    baseURLHasV1,
		APIKeyEncrypted: apiKey,
		APIKeyMasked:    maskAPIKey(apiKey),
		MaxTokens:       4096,
		Temperature:     0.1,
		TimeoutSec:      60,
		Status:          string(types.LLMModelStatusActive),
		Purpose:         types.LLMModelPurposeTranslation,
		IsDefault:       isDefault,
		IsSystem:        true,
		Config: types.LLMModelConfig{
			"purpose": "translation",
		},
	}, true, nil
}

// maskAPIKey 对 API Key 进行脱敏，仅保留首尾少量字符（seed 包私有副本）。
func maskAPIKey(key string) string {
	if len(key) <= 7 {
		return "***"
	}
	return key[:3] + "***" + key[len(key)-4:]
}
