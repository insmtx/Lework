package eino

import (
	"context"
	"fmt"
	"strings"

	einoclaude "github.com/cloudwego/eino-ext/components/model/claude"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
)

const (
	// ProviderOpenAI identifies the OpenAI provider.
	ProviderOpenAI = "openai"
	// ProviderCustom identifies an OpenAI-compatible custom provider.
	ProviderCustom = "custom"
	// ProviderDeepSeek identifies the DeepSeek provider.
	ProviderDeepSeek = "deepseek"
	// ProviderQwen identifies the Qwen provider.
	ProviderQwen = "qwen"
	// ProviderGemini identifies the Gemini provider.
	ProviderGemini = "gemini"
	// ProviderArk identifies the Volcano Ark provider.
	ProviderArk = "ark"
	// ProviderOpenRouter identifies the OpenRouter provider.
	ProviderOpenRouter = "openrouter"
	// ProviderAnthropic identifies the Anthropic provider.
	ProviderAnthropic = "anthropic"

	// defaultMaxTokens 是模型未配置最大输出 token 数时的兜底值。
	defaultMaxTokens = 4096
)

// ChatModelConfig contains provider-neutral chat model settings.
type ChatModelConfig struct {
	Provider         string
	APIKey           string
	Model            string
	BaseURL          string
	MaxTokens        int
	TopP             *float32
	FrequencyPenalty *float32
	PresencePenalty  *float32
	Temperature      *float32
	ResponseFormat   *einoopenai.ChatCompletionResponseFormat
	ReasoningEffort  einoopenai.ReasoningEffortLevel
}

// NewChatModel creates an Eino tool-calling chat model for a supported provider.
func NewChatModel(ctx context.Context, cfg *ChatModelConfig) (einomodel.ToolCallingChatModel, error) {
	if cfg == nil {
		return nil, fmt.Errorf("llm config is required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("llm api key is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("llm model is required")
	}

	provider := strings.TrimSpace(cfg.Provider)
	if provider == "" {
		provider = ProviderOpenAI
	}

	switch provider {
	case ProviderAnthropic:
		return newClaudeChatModel(ctx, cfg)
	case ProviderOpenAI, ProviderCustom, ProviderDeepSeek, ProviderQwen, ProviderGemini, ProviderArk, ProviderOpenRouter:
		return newOpenAICompatibleChatModel(ctx, cfg)
	default:
		return nil, fmt.Errorf("llm provider %q is not supported", provider)
	}
}

func newOpenAICompatibleChatModel(ctx context.Context, cfg *ChatModelConfig) (einomodel.ToolCallingChatModel, error) {
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	chatModel, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		APIKey:           cfg.APIKey,
		BaseURL:          cfg.BaseURL,
		Model:            cfg.Model,
		ResponseFormat:   cfg.ResponseFormat,
		ReasoningEffort:  cfg.ReasoningEffort,
		Temperature:      cfg.Temperature,
		TopP:             cfg.TopP,
		FrequencyPenalty: cfg.FrequencyPenalty,
		PresencePenalty:  cfg.PresencePenalty,
		MaxTokens:        &maxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("create eino openai chat model: %w", err)
	}
	return chatModel, nil
}

func newClaudeChatModel(ctx context.Context, cfg *ChatModelConfig) (einomodel.ToolCallingChatModel, error) {
	var baseURL *string
	if strings.TrimSpace(cfg.BaseURL) != "" {
		baseURL = &cfg.BaseURL
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	chatModel, err := einoclaude.NewChatModel(ctx, &einoclaude.Config{
		APIKey:      cfg.APIKey,
		BaseURL:     baseURL,
		Model:       cfg.Model,
		MaxTokens:   maxTokens,
		Temperature: cfg.Temperature,
		TopP:        cfg.TopP,
	})
	if err != nil {
		return nil, fmt.Errorf("create eino claude chat model: %w", err)
	}
	return chatModel, nil
}
