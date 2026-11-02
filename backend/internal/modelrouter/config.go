package modelrouter

import (
	"strings"

	"github.com/insmtx/Lework/backend/internal/llm"
	"github.com/insmtx/Lework/backend/pkg/llmprotocol"
)

type UpstreamConfig struct {
	ModelID      uint
	ModelName    string
	Provider     string
	BaseURL      string
	BaseURLHasV1 bool
	APIKey       string
	Protocol     llmprotocol.Protocol
	MaxTokens    int
	Temperature  float64
	TimeoutSec   int
}

func (c UpstreamConfig) ToModelConfig() *llm.ModelConfig {
	return &llm.ModelConfig{
		ID:           c.ModelID,
		Provider:     c.Provider,
		ModelName:    c.ModelName,
		BaseURL:      c.BaseURL,
		BaseURLHasV1: c.BaseURLHasV1,
		APIKey:       c.APIKey,
		MaxTokens:    c.MaxTokens,
		Temperature:  c.Temperature,
		TimeoutSec:   c.TimeoutSec,
		Status:       "active",
	}
}

func protocolForProvider(provider string) llmprotocol.Protocol {
	switch strings.ToLower(provider) {
	case "anthropic":
		return llmprotocol.ProtocolAnthropicMessages
	case "gemini":
		return llmprotocol.ProtocolGemini
	default:
		return llmprotocol.ProtocolOpenAIChat
	}
}
