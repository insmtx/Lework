package modelrouter

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Resolver struct{}

// NewResolver creates a model resolver backed by the worker-local singleton store.
func NewResolver() *Resolver {
	return &Resolver{}
}

// Resolve returns the cached upstream model configuration for one model name.
func (r *Resolver) Resolve(ctx context.Context, orgID uint, modelName string) (*UpstreamConfig, error) {
	_ = ctx
	_ = orgID

	if r == nil || DefaultStore() == nil {
		return nil, errors.New("llm model cache is not initialized")
	}

	cfg, ok := DefaultStore().resolve(modelName)
	if !ok {
		if modelName != "" {
			return nil, fmt.Errorf("llm model %q not found in worker cache", modelName)
		}
		return nil, errors.New("no llm model configured in worker cache")
	}
	if cfg.Provider == "" || cfg.ModelName == "" || cfg.APIKey == "" {
		return nil, errors.New("cached llm model config is incomplete")
	}
	return cfg, nil
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
