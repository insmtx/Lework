package llmprotocol

import (
	"fmt"
)

// Capability represents a single model capability.
type Capability string

const (
	CapText              Capability = "text"
	CapToolCall          Capability = "tool_call"
	CapReasoning         Capability = "reasoning"
	CapImage             Capability = "image"
	CapAudio             Capability = "audio"
	CapFile              Capability = "file"
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
		CapParallelToolCalls: true,
	}

	// AnthropicMessagesCapabilities defines capabilities for the Anthropic Messages API.
	AnthropicMessagesCapabilities = CapabilitySet{
		CapText:              true,
		CapToolCall:          true,
		CapReasoning:         true,
		CapImage:             true,
		CapFile:              true,
		CapParallelToolCalls: true,
	}

	// GeminiCapabilities defines capabilities for the Google Gemini API.
	GeminiCapabilities = CapabilitySet{
		CapText:              true,
		CapToolCall:          true,
		CapReasoning:         true,
		CapImage:             true,
		CapAudio:             true,
		CapFile:              true,
		CapParallelToolCalls: true,
	}
)

// CapabilitiesForProtocol returns the capability set for a supported protocol.
func CapabilitiesForProtocol(proto Protocol) CapabilitySet {
	switch proto {
	case ProtocolOpenAIChat:
		return OpenAIChatCapabilities
	case ProtocolOpenAIResponses:
		return OpenAIResponsesCapabilities
	case ProtocolAnthropicMessages:
		return AnthropicMessagesCapabilities
	case ProtocolGemini:
		return GeminiCapabilities
	default:
		return OpenAIChatCapabilities
	}
}

// NormalizeRequest adapts an IR request to the constraints of a target protocol.
func NormalizeRequest(ir *IRRequest, targetCaps CapabilitySet) (*IRRequest, []Warning, error) {
	// --- Hard blocks ---
	// Tools defined but target lacks tool_call capability.
	if len(ir.Tools) > 0 && !targetCaps.Has(CapToolCall) {
		return nil, nil, fmt.Errorf(
			"target protocol does not support tool calls (%d tools defined)",
			len(ir.Tools),
		)
	}

	// --- Degradable checks ---
	// Walk messages and remove unsupported content parts.

	// Normalize tool messages first — merge orphan text parts into ToolResult.Content.
	// This is a defensive layer; well-formed IR should not need it.
	normalizedMsgs, toolWarnings, toolChanged := normalizeToolMessages(ir)
	if toolChanged {
		ir.Messages = normalizedMsgs
	}
	warnings := append([]Warning{}, toolWarnings...)
	var newMessages []IRMessage
	needsCopy := toolChanged

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

// normalizeToolMessages cleans up IRRoleTool messages whose parts contain
// orphan text content outside IRPartToolResult.Content.  This can happen
// when an upstream protocol adapter constructs tool messages incorrectly.
func normalizeToolMessages(ir *IRRequest) ([]IRMessage, []Warning, bool) {
	var warnings []Warning
	changed := false
	messages := ir.Messages

	for i, msg := range messages {
		if msg.Role != IRRoleTool {
			continue
		}
		if len(msg.Parts) <= 1 {
			continue
		}

		var toolResultIdx int = -1
		var orphanTexts []string
		for j, part := range msg.Parts {
			if part.Type == IRPartToolResult && toolResultIdx < 0 {
				toolResultIdx = j
			} else if part.Type == IRPartText {
				orphanTexts = append(orphanTexts, part.Text)
			}
		}

		if toolResultIdx < 0 || len(orphanTexts) == 0 {
			if len(msg.Parts) > 1 {
				warnings = append(warnings, Warning{
					Field:   fmt.Sprintf("messages[%d]", i),
					Message: fmt.Sprintf("tool message has %d parts but no ToolResult — cannot normalize", len(msg.Parts)),
				})
			}
			continue
		}

		warnings = append(warnings, Warning{
			Field:   fmt.Sprintf("messages[%d]", i),
			Message: fmt.Sprintf("merged %d orphan text parts into ToolResult.Content", len(orphanTexts)),
		})

		toolResult := msg.Parts[toolResultIdx].ToolResult
		if toolResult == nil {
			toolResult = &IRToolResultPart{Status: "completed"}
		}
		for _, t := range orphanTexts {
			toolResult.Content = append(toolResult.Content, IRContentPart{Type: IRPartText, Text: t})
		}

		if !changed {
			messages = make([]IRMessage, len(ir.Messages))
			copy(messages, ir.Messages)
			changed = true
		}
		messages[i].Parts = []IRContentPart{{Type: IRPartToolResult, ToolResult: toolResult}}
	}

	return messages, warnings, changed
}
