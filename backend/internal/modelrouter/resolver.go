package modelrouter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

type Resolver struct {
	db *gorm.DB
}

func NewResolver(db *gorm.DB) *Resolver {
	return &Resolver{db: db}
}

func (r *Resolver) Resolve(ctx context.Context, orgID uint, modelName string) (*UpstreamConfig, error) {
	if r.db == nil {
		return nil, errors.New("database is not initialized")
	}

	var (
		model *types.LLMModel
		err   error
	)

	if modelName != "" {
		model, err = infradb.GetActiveLLMModelByName(ctx, r.db, orgID, modelName)
	} else {
		model, err = infradb.GetDefaultLLMModel(ctx, r.db, orgID)
	}
	if err != nil {
		return nil, fmt.Errorf("query llm model: %w", err)
	}
	if model == nil {
		if modelName != "" {
			return nil, fmt.Errorf("llm model %q not found or inactive", modelName)
		}
		return nil, errors.New("no default llm model configured for this organization")
	}

	if model.OrgID != orgID {
		return nil, fmt.Errorf("llm model %q does not belong to current organization", modelName)
	}

	upstreamProtocol := resolveUpstreamProtocol(model)

	baseURL := model.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL(model.Provider)
	}

	cfg := &UpstreamConfig{
		ModelName:    model.ModelName,
		Provider:     model.Provider,
		BaseURL:      baseURL,
		BaseURLHasV1: model.BaseURLHasV1,
		APIKey:       model.APIKeyEncrypted,
		Protocol:     upstreamProtocol,
		MaxTokens:    model.MaxTokens,
		Temperature:  model.Temperature,
		TimeoutSec:   model.TimeoutSec,
	}
	return cfg, nil
}

func resolveUpstreamProtocol(model *types.LLMModel) Protocol {
	if model.Config != nil {
		if raw, ok := model.Config["protocol"]; ok {
			if s, ok := raw.(string); ok && s != "" {
				return Protocol(s)
			}
		}
	}
	return DefaultProtocolForProvider(model.Provider)
}

func defaultBaseURL(provider string) string {
	switch strings.ToLower(provider) {
	case "openai":
		return "https://api.openai.com"
	case "anthropic":
		return "https://api.anthropic.com"
	case "deepseek":
		return "https://api.deepseek.com"
	case "qwen":
		return "https://dashscope.aliyuncs.com"
	case "gemini":
		return "https://generativelanguage.googleapis.com"
	case "ark":
		return "https://ark.cn-beijing.volces.com"
	case "openrouter":
		return "https://openrouter.ai"
	default:
		return ""
	}
}
