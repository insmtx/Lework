package modelrouter

import (
	"fmt"
)

// Capability represents a single model capability.
type Capability string

const (
	CapText             Capability = "text"
	CapToolCall         Capability = "tool_call"
	CapReasoning        Capability = "reasoning"
	CapImage            Capability = "image"
	CapAudio            Capability = "audio"
	CapFile             Capability = "file"
	CapParallelToolCalls Capability = "parallel_tool_calls"
)

// CapabilitySet is a set of capabilities supported by a target protocol.
type CapabilitySet map[Capability]bool

// Has returns true if the capability is set.
func (cs CapabilitySet) Has(cap Capability) bool {
	return cs[cap]
}

// Warning represents a non-fatal degradation notice.
type Warning struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Predefined capability sets for supported protocols.
var (
	// OpenAIChatCapabilities defines capabilities for the OpenAI Chat Completions API.
	OpenAIChatCapabilities = CapabilitySet{
		CapText:              true,
		CapToolCall:          true,
		CapReasoning:         true,
		CapImage:             true,
		CapParallelToolCalls: true,
	}

	// OpenAIResponsesCapabilities defines capabilities for the OpenAI Responses API.
	OpenAIResponsesCapabilities = CapabilitySet{
		CapText:              true,
		CapToolCall:          true,
		CapReasoning:         true,
		CapImage:             true,
		CapFile:              true,
		CapParallelToolCalls: true,
	}

	// AnthropicMessagesCapabilities defines capabilities for the Anthropic Messages API.
	AnthropicMessagesCapabilities = CapabilitySet{
		CapText:              true,
		CapToolCall:          true,
		CapReasoning:         true,
		CapImage:             true,
		CapParallelToolCalls: true,
	}

	// GeminiCapabilities defines capabilities for the Google Gemini API.
	GeminiCapabilities = CapabilitySet{
		CapText:              true,
		CapToolCall:          true,
		CapImage:             true,
		CapAudio:             true,
		CapFile:              true,
		CapParallelToolCalls: true,
	}
)

// NormalizeRequest validates an IRRequest against target capabilities,
// returning a modified request, warnings for degradations, or an error
// for fatal incompatibilities.
//
// Fatal errors: the request fundamentally cannot be satisfied
// (e.g., required tool use but target lacks tool_call support).
// Warnings: the request can still succeed with degraded content
// (e.g., removing image parts when target lacks image support).
func NormalizeRequest(ir *IRRequest, targetCaps CapabilitySet) (*IRRequest, []Warning, error) {
	// --- Fatal checks ---

	// Tool choice required/auto/any but target lacks tool_call capability.
	if ir.ToolChoice != nil && !targetCaps.Has(CapToolCall) {
		return nil, nil, fmt.Errorf(
			"target protocol does not support tool calls (tool_choice=%q)",
			ir.ToolChoice.Type,
		)
	}

	// Tools defined but target lacks tool_call capability.
	if len(ir.Tools) > 0 && !targetCaps.Has(CapToolCall) {
		return nil, nil, fmt.Errorf(
			"target protocol does not support tool calls (%d tools defined)",
			len(ir.Tools),
		)
	}

	// --- Degradable checks ---
	// Walk messages and remove unsupported content parts.

	var warnings []Warning
	var newMessages []IRMessage
	needsCopy := false

	for msgIdx, msg := range ir.Messages {
		var newParts []IRContentPart
		for _, part := range msg.Parts {
			switch part.Type {
			case IRPartImage:
				if !targetCaps.Has(CapImage) {
					warnings = append(warnings, Warning{
						Field:   fmt.Sprintf("messages[%d].parts[image]", msgIdx),
						Message: "image content removed: target protocol does not support image inputs",
					})
					needsCopy = true
					continue
				}
			case IRPartAudio:
				if !targetCaps.Has(CapAudio) {
					warnings = append(warnings, Warning{
						Field:   fmt.Sprintf("messages[%d].parts[audio]", msgIdx),
						Message: "audio content removed: target protocol does not support audio inputs",
					})
					needsCopy = true
					continue
				}
			case IRPartFile:
				if !targetCaps.Has(CapFile) {
					warnings = append(warnings, Warning{
						Field:   fmt.Sprintf("messages[%d].parts[file]", msgIdx),
						Message: "file content removed: target protocol does not support file inputs",
					})
					needsCopy = true
					continue
				}
			case IRPartReasoning:
				if !targetCaps.Has(CapReasoning) {
					warnings = append(warnings, Warning{
						Field:   fmt.Sprintf("messages[%d].parts[reasoning]", msgIdx),
						Message: "reasoning content removed: target protocol does not support reasoning inputs",
					})
					needsCopy = true
					continue
				}
			}
			newParts = append(newParts, part)
		}

		if needsCopy && newMessages == nil {
			// Lazy-copy messages up to this point.
			newMessages = make([]IRMessage, len(ir.Messages))
			copy(newMessages, ir.Messages)
		}

		if needsCopy {
			newMessages[msgIdx].Parts = newParts
		}
	}

	if !needsCopy {
		// No changes needed — return original.
		return ir, nil, nil
	}

	// Build a copy of the request with modified messages.
	result := *ir
	result.Messages = newMessages
	return &result, warnings, nil
}
