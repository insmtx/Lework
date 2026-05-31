// Package modelrouter provides Worker built-in LLM model routing capability.
package modelrouter

import (
	"fmt"
	"strings"
)

// Protocol represents an LLM API protocol type.
type Protocol string

const (
	// ProtocolOpenAIChat is the OpenAI Chat Completions protocol.
	ProtocolOpenAIChat Protocol = "openai_chat"
	// ProtocolOpenAIResponses is the OpenAI Responses API protocol.
	ProtocolOpenAIResponses Protocol = "openai_responses"
	// ProtocolAnthropicMessages is the Anthropic Messages API protocol.
	ProtocolAnthropicMessages Protocol = "anthropic_messages"
	// ProtocolGemini is the Google Gemini API protocol.
	ProtocolGemini Protocol = "gemini"
)

// ProtocolFromPath determines the entry protocol from the request path.
func ProtocolFromPath(path string) (Protocol, error) {
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		return ProtocolOpenAIChat, nil
	case strings.HasSuffix(path, "/messages"):
		return ProtocolAnthropicMessages, nil
	case strings.HasSuffix(path, "/responses"):
		return ProtocolOpenAIResponses, nil
	case strings.HasSuffix(path, "/models/") || strings.Contains(path, ":generateContent"):
		return ProtocolGemini, nil
	default:
		return "", fmt.Errorf("unsupported path: %s", path)
	}
}

// DefaultProtocolForProvider returns the default upstream protocol for a provider.
func DefaultProtocolForProvider(provider string) Protocol {
	switch strings.ToLower(provider) {
	case "anthropic":
		return ProtocolAnthropicMessages
	case "gemini":
		return ProtocolGemini
	default:
		return ProtocolOpenAIChat
	}
}

// UpstreamAPIPath returns the API path suffix for the given protocol.
func UpstreamAPIPath(proto Protocol, hasV1 bool) string {
	switch proto {
	case ProtocolOpenAIChat:
		if hasV1 {
			return "/v1/chat/completions"
		}
		return "/chat/completions"
	case ProtocolOpenAIResponses:
		if hasV1 {
			return "/v1/responses"
		}
		return "/responses"
	case ProtocolAnthropicMessages:
		if hasV1 {
			return "/v1/messages"
		}
		return "/messages"
	case ProtocolGemini:
		return "/v1beta/models/gemini-2.0-flash:generateContent"
	default:
		// Default to chat completions.
		if hasV1 {
			return "/v1/chat/completions"
		}
		return "/chat/completions"
	}
}

// UpstreamConfig describes the complete upstream forwarding configuration.
type UpstreamConfig struct {
	ModelName    string
	Provider     string
	BaseURL      string
	BaseURLHasV1 bool
	APIKey       string
	Protocol     Protocol
	MaxTokens    int
	Temperature  float64
	TimeoutSec   int
}

// ProtocolAdapter defines the contract for protocol-specific encoding/decoding.
// Each adapter fully owns its stream state — the handler/converter never touches it.
type ProtocolAdapter interface {
	// Protocol returns which protocol this adapter handles.
	Protocol() Protocol

	// DecodeRequest converts a raw protocol-specific request to canonical IR.
	DecodeRequest(raw map[string]interface{}) (*IRRequest, error)

	// EncodeRequest converts canonical IR to protocol-specific request format.
	EncodeRequest(ir *IRRequest) (map[string]interface{}, error)

	// DecodeResponse converts a raw protocol-specific response to canonical IR.
	DecodeResponse(raw map[string]interface{}) (*IRResponse, error)

	// EncodeResponse converts canonical IR to protocol-specific response format.
	EncodeResponse(ir *IRResponse) (map[string]interface{}, error)

	// NewStreamState creates a new opaque stream state for this adapter.
	// The returned value is owned entirely by this adapter.
	NewStreamState() interface{}

	// DecodeStreamEvent converts a raw SSE data payload to IR stream events.
	// May return multiple events from a single raw event (e.g., Gemini multi-part).
	DecodeStreamEvent(raw map[string]interface{}, state interface{}) ([]*IRStreamEvent, error)

	// EncodeStreamEvent converts an IR stream event to protocol-specific SSE payloads.
	// May return multiple payloads from a single IR event (e.g., Responses text.done+part.done+item.done).
	EncodeStreamEvent(ir *IRStreamEvent, state interface{}) ([]map[string]interface{}, error)
}

// StreamContext holds shared context during stream conversion.
// State is opaque and owned by the adapter.
type StreamContext struct {
	ResponseID string
	Model      string
	Usage      *IRUsage
	State      interface{} // adapter-owned opaque state
}

// adapters maps protocol to registered ProtocolAdapter implementations.
var adapters = map[Protocol]ProtocolAdapter{}

// RegisterAdapter registers a ProtocolAdapter for its protocol.
// Panics if a different adapter is already registered for the same protocol.
func RegisterAdapter(a ProtocolAdapter) {
	proto := a.Protocol()
	if existing, ok := adapters[proto]; ok && existing != a {
		// This is a programming error — different adapter for same protocol.
		// Using log.Fatal would pull in log dependency; panicking in registration
		// is acceptable as it's a startup-time programmer error.
		panic(fmt.Sprintf("modelrouter: ProtocolAdapter already registered for %q", proto))
	}
	adapters[proto] = a
}

// GetAdapter returns the registered ProtocolAdapter for the given protocol.
func GetAdapter(proto Protocol) (ProtocolAdapter, error) {
	a, ok := adapters[proto]
	if !ok {
		return nil, fmt.Errorf("modelrouter: no ProtocolAdapter registered for %q", proto)
	}
	return a, nil
}
