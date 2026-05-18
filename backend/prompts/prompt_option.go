package prompts

import "github.com/insmtx/Leros/backend/config"

type RunOption func(*config.LLMConfig)

func WithModel(model string) RunOption {
	return func(cfg *config.LLMConfig) { cfg.Model = model }
}

func WithProvider(provider string) RunOption {
	return func(cfg *config.LLMConfig) { cfg.Provider = provider }
}

func WithBaseURL(url string) RunOption {
	return func(cfg *config.LLMConfig) { cfg.BaseURL = url }
}
