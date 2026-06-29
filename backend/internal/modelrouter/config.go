package modelrouter

import (
	"net/http"
	"strings"

	"github.com/insmtx/Leros/backend/pkg/llmprotocol"
)

// UpstreamConfig describes the complete upstream forwarding configuration.
type UpstreamConfig struct {
	ModelName    string
	Provider     string
	BaseURL      string
	BaseURLHasV1 bool
	APIKey       string
	Protocol     llmprotocol.Protocol
	MaxTokens    int
	Temperature  float64
	TimeoutSec   int
	httpClient   *http.Client
}

// protocolForProvider returns the default upstream protocol for a provider.
// This is modelrouter's own mapping — it does not delegate to llmprotocol.
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
