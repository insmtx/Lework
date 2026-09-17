package seed

import (
	"context"
	"errors"
	"strings"
	"time"

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

// llmProbeTimeout 限制启动期连通性探测的总耗时。
// 上游不可达时 TCP 建连可能长时间悬挂，若不设上限会把初始化拖住，表现为"服务起不来"。
const llmProbeTimeout = 10 * time.Second

// resolveLLMHasV1 实测 base_url 是否需要上游 /v1 前缀。
//
// 模型可用性属于运行时状态，不得决定服务能否启动：探测全部失败（上游不可用、鉴权错误、
// 网络不通或超时）时回退到 preferV1 指示的候选并仅记录告警，模型配置照常落库，
// 由运行时调用自行返回错误。注意此时 /v1 前缀只是推断值而非实测值，且 seed 仅在该模型
// 尚未落库时写入，重启不会重探 —— 上游恢复后若前缀推断有误，需通过模型配置的连通性测试
// 与编辑保存（会重新探测并写回 BaseURLHasV1）修正，无需重启进程。
func resolveLLMHasV1(ctx context.Context, provider, modelName, apiKey, baseURL string, preferV1 bool) bool {
	probeCtx, cancel := context.WithTimeout(ctx, llmProbeTimeout)
	defer cancel()

	result := probeLLMHasV1Fn(probeCtx, provider, modelName, apiKey, baseURL, preferV1)
	switch {
	case result == nil:
		logs.Warnf("seed: llm connectivity probe returned no result (provider=%s model=%s base_url=%q), "+
			"fallback base_url_has_v1=%v; llm features may fail at runtime until upstream is reachable",
			provider, modelName, baseURL, preferV1)
		return preferV1
	case result.V1Success:
		return true
	case result.NoV1Success:
		return false
	default:
		logs.Warnf("seed: llm connectivity probe failed (provider=%s model=%s base_url=%q), model is unavailable now "+
			"but server startup continues; fallback base_url_has_v1=%v; fix llm config or rerun the connectivity test "+
			"after upstream recovers, otherwise llm-dependent features fail at runtime",
			provider, modelName, baseURL, preferV1)
		return preferV1
	}
}

// preferV1ForBaseURL 判断配置的 base_url 是否已显式声明 /v1 路径段。
// 用于确定优先探测的候选，并在探测失败（上游不可用）时作为 /v1 前缀的推断依据。
// 必须传入原始配置值：NormalizeLLMBaseURL 会先剥掉 /v1，规范化结果上判断恒为 false。
func preferV1ForBaseURL(rawBaseURL string) bool {
	return llm.DetectURLHasV1(strings.TrimSpace(rawBaseURL))
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
		// preferV1 必须基于配置中的原始 base_url 判断：NormalizeLLMBaseURL 会剥掉 /v1 后缀，
		// 若传入规范化结果则该提示恒为 false。
		rawBaseURL := strings.TrimSpace(llmCfg.BaseURL)
		baseURL := llm.NormalizeLLMBaseURL(rawBaseURL)
		hasV1 := resolveLLMHasV1(ctx, llmCfg.Provider, modelName, llmCfg.APIKey, baseURL, preferV1ForBaseURL(rawBaseURL))
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
	spec, ok := buildSystemTranslationLLMModelSpec(ctx, llmCfg)
	if !ok {
		logs.Warn("seed: system translation LLM model skipped: no api_key configured")
		return nil
	}

	var existing types.LLMModel
	err := db.WithContext(ctx).Where("org_id = ? AND code = ?", spec.OrgID, spec.Code).First(&existing).Error
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
// 构造前会对 base_url 做一次连通性探测以确定 /v1 前缀；探测失败不阻断启动，仅回退到候选默认值。
func buildSystemTranslationLLMModelSpec(ctx context.Context, llmCfg *config.LLMConfig) (spec *types.LLMModel, ok bool) {
	if llmCfg == nil {
		return nil, false
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
		return nil, false
	}

	// 同 seedLLM：preferV1 需基于原始 base_url，规范化会剥掉 /v1 后缀。
	rawBaseURL := baseURL
	baseURL = llm.NormalizeLLMBaseURL(rawBaseURL)
	baseURLHasV1 := resolveLLMHasV1(ctx, provider, modelName, apiKey, baseURL, preferV1ForBaseURL(rawBaseURL))

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
	}, true
}

// maskAPIKey 对 API Key 进行脱敏，仅保留首尾少量字符（seed 包私有副本）。
func maskAPIKey(key string) string {
	if len(key) <= 7 {
		return "***"
	}
	return key[:3] + "***" + key[len(key)-4:]
}
