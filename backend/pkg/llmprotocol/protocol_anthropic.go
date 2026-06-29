package llmprotocol

import (
	"fmt"
	"strings"

	"github.com/bytedance/sonic"
)

const anthropicBillingHeaderPrefix = "x-anthropic-billing-header:"

func getCacheControl(m map[string]interface{}) map[string]interface{} {
	if cc, ok := m["cache_control"].(map[string]interface{}); ok {
		return cloneMap(cc)
	}
	return nil
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func applyCacheControl(m map[string]interface{}, cc map[string]interface{}) {
	if len(cc) > 0 {
		m["cache_control"] = cc
	}
}

func stripLeadingAnthropicBillingHeader(text string) string {
	if !strings.HasPrefix(text, anthropicBillingHeaderPrefix) {
		return text
	}
	lineEnd := strings.IndexAny(text, "\r\n")
	if lineEnd < 0 {
		return ""
	}
	restStart := lineEnd + 1
	if text[lineEnd] == '\r' && restStart < len(text) && text[restStart] == '\n' {
		restStart++
	}
	rest := text[restStart:]
	rest = strings.TrimPrefix(rest, "\r\n")
	rest = strings.TrimPrefix(rest, "\n")
	rest = strings.TrimPrefix(rest, "\r")
	return rest
}

// ensureContentArray returns a non-nil content array for Anthropic protocol compliance.
func ensureContentArray(content []map[string]interface{}) []map[string]interface{} {
	if content == nil {
		return []map[string]interface{}{}
	}
	return content
}

// anthropicStreamState tracks the stream lifecycle for Anthropic protocol.
// Owned entirely by the adapter — the handler/converter never touches it.
type anthropicStreamState struct {
	textStarted      map[int]bool
	textStopped      map[int]bool
	reasoningStarted map[int]bool
	reasoningStopped map[int]bool
	toolStarted      map[int]bool
	toolStopped      map[int]bool
	toolBlockIDs     map[int]string
	toolBlockNames   map[int]string

	accumulatedInputTokens         int
	accumulatedCacheReadTokens     int
	accumulatedCacheCreationTokens int
}

// anthropicMessagesAdapter implements ProtocolAdapter for the Anthropic Messages API.
type anthropicMessagesAdapter struct{}

func init() {
	registerAdapterOnInit(&anthropicMessagesAdapter{})
}

func (a *anthropicMessagesAdapter) Protocol() Protocol {
	return ProtocolAnthropicMessages
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// DecodeRequest
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (a *anthropicMessagesAdapter) DecodeRequest(raw map[string]interface{}) (*IRRequest, error) {
	ir := &IRRequest{
		Model:     getString(raw, "model"),
		Stream:    getBool(raw, "stream"),
		MaxTokens: getIntDefault(raw, "max_tokens"),
	}
	ir.CacheControl = getCacheControl(raw)
	if metadata, ok := raw["metadata"].(map[string]interface{}); ok {
		ir.Metadata = metadata
	}

	if outputConfig, ok := raw["output_config"].(map[string]interface{}); ok {
		ir.ReasoningEffort = getString(outputConfig, "effort")
		if len(outputConfig) > 0 {
			if ir.Extensions == nil {
				ir.Extensions = map[string]map[string]interface{}{}
			}
			ir.Extensions["anthropic"] = map[string]interface{}{"output_config": cloneMap(outputConfig)}
		}
	}

	// Thinking config.
	if thinking, ok := raw["thinking"].(map[string]interface{}); ok {
		ir.Thinking = &IRThinkingConfig{
			Type:         getString(thinking, "type"),
			BudgetTokens: getIntDefault(thinking, "budget_tokens"),
		}
		if ir.ReasoningEffort != "" {
			ir.Thinking.Effort = ir.ReasoningEffort
		}
	}

	// System: string or array of text blocks.
	if system, ok := raw["system"]; ok {
		switch v := system.(type) {
		case string:
			ir.System = stripLeadingAnthropicBillingHeader(v)
		case []interface{}:
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok && getString(m, "type") == "text" {
					text := stripLeadingAnthropicBillingHeader(getString(m, "text"))
					ir.System += text
					ir.SystemParts = append(ir.SystemParts, IRSystemPart{
						Type:         "text",
						Text:         text,
						CacheControl: getCacheControl(m),
					})
				}
			}
		}
	}

	// Messages.
	if msgs, ok := getList(raw, "messages"); ok {
		ir.Messages = decodeAnthropicMessages(msgs)
	}

	// Temperature.
	if t, ok := getFloat(raw, "temperature"); ok {
		ir.Temperature = &t
	}
	// TopP.
	if p, ok := getFloat(raw, "top_p"); ok {
		ir.TopP = &p
	}
	if k, ok := getInt(raw, "top_k"); ok {
		ir.TopK = &k
	}
	// Stop sequences.
	if ss, ok := getStringList(raw, "stop_sequences"); ok {
		ir.Stop = ss
	}

	// Tools.
	if tools, ok := getList(raw, "tools"); ok {
		ir.Tools = decodeAnthropicTools(tools)
	}

	// Tool choice.
	if tc, ok := raw["tool_choice"]; ok {
		ir.ToolChoice = decodeAnthropicToolChoice(tc)
	}

	return ir, nil
}

func decodeAnthropicMessages(raw []interface{}) []IRMessage {
	var msgs []IRMessage
	for _, r := range raw {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}

		role := getString(m, "role")
		msg := IRMessage{Role: mapAnthropicRole(role)}

		if content := m["content"]; content != nil {
			msg.Parts = decodeAnthropicContent(content)
		}

		msgs = append(msgs, msg)
	}
	return msgs
}

func decodeAnthropicContent(content interface{}) []IRContentPart {
	switch v := content.(type) {
	case string:
		return []IRContentPart{{Type: IRPartText, Text: v}}
	case []interface{}:
		var parts []IRContentPart
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			switch getString(m, "type") {
			case "text":
				parts = append(parts, IRContentPart{Type: IRPartText, Text: getString(m, "text"), CacheControl: getCacheControl(m)})
			case "thinking":
				parts = append(parts, IRContentPart{
					Type:         IRPartReasoning,
					CacheControl: getCacheControl(m),
					Reasoning: &IRReasoningPart{
						Content:   getString(m, "thinking"),
						Signature: getString(m, "signature"),
						Subtype:   "thinking",
					},
				})
			case "redacted_thinking":
				parts = append(parts, IRContentPart{
					Type:         IRPartReasoning,
					CacheControl: getCacheControl(m),
					Reasoning: &IRReasoningPart{
						Subtype:          "redacted_thinking",
						EncryptedContent: getString(m, "data"),
						Signature:        getString(m, "signature"),
					},
				})
			case "tool_use":
				input, _ := m["input"].(map[string]interface{})
				parts = append(parts, IRContentPart{
					ID:           getString(m, "id"),
					Type:         IRPartToolCall,
					CacheControl: getCacheControl(m),
					ToolCall: &IRToolCallPart{
						ID:            getString(m, "id"),
						Name:          getString(m, "name"),
						ArgumentsRaw:  CanonicalToolArguments("", input),
						ArgumentsJSON: input,
						Status:        "completed",
					},
				})
			case "tool_result":
				resultContent := decodeToolResultContent(m["content"])
				parts = append(parts, IRContentPart{
					Type:         IRPartToolResult,
					CacheControl: getCacheControl(m),
					ToolResult: &IRToolResultPart{
						ToolCallID: getString(m, "tool_use_id"),
						Content:    resultContent,
						Status:     "success",
					},
				})
			}
		}
		return parts
	}
	return nil
}

func decodeToolResultContent(content interface{}) []IRContentPart {
	switch v := content.(type) {
	case string:
		return []IRContentPart{{Type: IRPartText, Text: v}}
	case []interface{}:
		var parts []IRContentPart
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			switch getString(m, "type") {
			case "text":
				parts = append(parts, IRContentPart{Type: IRPartText, Text: getString(m, "text")})
			}
		}
		return parts
	default:
		return []IRContentPart{{Type: IRPartText, Text: contentToString(v)}}
	}
}

func decodeAnthropicTools(raw []interface{}) []IRToolDecl {
	var tools []IRToolDecl
	for _, r := range raw {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		params, _ := m["input_schema"].(map[string]interface{})
		tools = append(tools, IRToolDecl{
			Type:         "function",
			Name:         getString(m, "name"),
			Description:  getString(m, "description"),
			Parameters:   params,
			CacheControl: getCacheControl(m),
		})
	}
	return tools
}

func decodeAnthropicToolChoice(tc interface{}) *IRToolChoice {
	if tcm, ok := tc.(map[string]interface{}); ok {
		t := getString(tcm, "type")
		n := getString(tcm, "name")
		switch t {
		case "any":
			return &IRToolChoice{Type: "required"}
		case "tool":
			return &IRToolChoice{Type: "specific", Name: n}
		case "auto":
			return &IRToolChoice{Type: "auto"}
		default:
			return &IRToolChoice{Type: t}
		}
	}
	return nil
}

func mapAnthropicRole(role string) IRRole {
	switch role {
	case "user":
		return IRRoleUser
	case "assistant":
		return IRRoleAssistant
	}
	return IRRoleUser
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// EncodeRequest
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (a *anthropicMessagesAdapter) EncodeRequest(ir *IRRequest) (map[string]interface{}, error) {
	maxTokens := ir.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	body := map[string]interface{}{
		"model":      ir.Model,
		"max_tokens": maxTokens,
		"messages":   encodeAnthropicMessages(ir.Messages),
	}

	if len(ir.SystemParts) > 0 {
		body["system"] = encodeAnthropicSystemParts(ir.SystemParts)
	} else if ir.System != "" {
		body["system"] = stripLeadingAnthropicBillingHeader(ir.System)
	}
	if len(ir.CacheControl) > 0 {
		body["cache_control"] = ir.CacheControl
	}
	if len(ir.Metadata) > 0 {
		body["metadata"] = ir.Metadata
	}
	if tc := ir.Thinking; tc != nil {
		body["thinking"] = encodeAnthropicThinkingConfig(tc, maxTokens)
	}
	if outputConfig := encodeAnthropicOutputConfig(ir); len(outputConfig) > 0 {
		body["output_config"] = outputConfig
	}
	if ir.Temperature != nil {
		body["temperature"] = *ir.Temperature
	}
	if ir.TopP != nil {
		body["top_p"] = *ir.TopP
	}
	if ir.TopK != nil {
		body["top_k"] = *ir.TopK
	}
	if len(ir.Stop) > 0 {
		body["stop_sequences"] = ir.Stop
	}
	if ir.Stream {
		body["stream"] = true
	}

	if len(ir.Tools) > 0 {
		var tools []map[string]interface{}
		for _, t := range ir.Tools {
			tool := map[string]interface{}{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.Parameters,
			}
			if len(t.CacheControl) > 0 {
				tool["cache_control"] = t.CacheControl
			}
			tools = append(tools, tool)
		}
		body["tools"] = tools
	}

	if ir.ToolChoice != nil {
		body["tool_choice"] = encodeAnthropicToolChoice(ir.ToolChoice)
	}

	return body, nil
}

func encodeAnthropicThinkingConfig(tc *IRThinkingConfig, maxTokens int) map[string]interface{} {
	out := map[string]interface{}{}
	if tc == nil {
		return out
	}
	if tc.Type != "" {
		out["type"] = tc.Type
	}
	if strings.EqualFold(tc.Type, "enabled") && tc.BudgetTokens > 0 {
		budget := tc.BudgetTokens
		if maxTokens > 1 && budget >= maxTokens {
			budget = maxTokens - 1
		}
		out["budget_tokens"] = budget
	}
	return out
}

func encodeAnthropicOutputConfig(ir *IRRequest) map[string]interface{} {
	if ir == nil {
		return nil
	}
	effort := normalizeAnthropicEffort(ir.ReasoningEffort)
	if effort == "" && ir.Thinking != nil {
		effort = normalizeAnthropicEffort(ir.Thinking.Effort)
	}
	if effort == "" {
		return nil
	}
	return map[string]interface{}{"effort": effort}
}

func normalizeAnthropicEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "minimal":
		return "low"
	case "low", "medium", "high", "xhigh", "max":
		return strings.ToLower(strings.TrimSpace(effort))
	default:
		return ""
	}
}

func encodeAnthropicMessages(msgs []IRMessage) []map[string]interface{} {
	var result []map[string]interface{}

	for _, m := range msgs {
		switch m.Role {
		case IRRoleSystem, IRRoleTool:
			// System and tool roles map to "user" in Anthropic.
			em := map[string]interface{}{"role": "user"}
			content := encodeAnthropicParts(m.Parts)
			if len(content) > 0 {
				em["content"] = content
			}
			result = append(result, em)
		default:
			role := "assistant"
			if m.Role == IRRoleUser {
				role = "user"
			}
			em := map[string]interface{}{"role": role}
			content := encodeAnthropicParts(m.Parts)
			if len(content) > 0 {
				em["content"] = content
			}
			result = append(result, em)
		}
	}

	return result
}

func encodeAnthropicSystemParts(parts []IRSystemPart) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(parts))
	for _, part := range parts {
		if part.Type != "" && part.Type != "text" {
			continue
		}
		block := map[string]interface{}{
			"type": "text",
			"text": stripLeadingAnthropicBillingHeader(part.Text),
		}
		applyCacheControl(block, part.CacheControl)
		result = append(result, block)
	}
	return result
}

func encodeAnthropicParts(parts []IRContentPart) []map[string]interface{} {
	var content []map[string]interface{}
	for _, part := range parts {
		switch part.Type {
		case IRPartText:
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": part.Text,
			})
			applyCacheControl(content[len(content)-1], part.CacheControl)
		case IRPartReasoning:
			if part.Reasoning == nil {
				continue
			}
			subtype := part.Reasoning.Subtype
			if subtype == "" {
				subtype = "thinking"
			}
			tb := map[string]interface{}{"type": subtype}
			if subtype == "redacted_thinking" {
				tb["data"] = part.Reasoning.EncryptedContent
			} else {
				tb["thinking"] = part.Reasoning.Content
			}
			if part.Reasoning.Signature != "" {
				tb["signature"] = part.Reasoning.Signature
			}
			applyCacheControl(tb, part.CacheControl)
			content = append(content, tb)
		case IRPartToolCall:
			var input interface{}
			if part.ToolCall.ArgumentsRaw != "" && validJSON([]byte(part.ToolCall.ArgumentsRaw)) {
				_ = sonic.Unmarshal([]byte(part.ToolCall.ArgumentsRaw), &input)
			} else if part.ToolCall.ArgumentsJSON != nil {
				input = part.ToolCall.ArgumentsJSON
			} else if part.ToolCall.ArgumentsRaw != "" {
				_ = sonic.Unmarshal([]byte(part.ToolCall.ArgumentsRaw), &input)
			}
			tb := map[string]interface{}{
				"type":  "tool_use",
				"id":    part.ToolCall.ID,
				"name":  part.ToolCall.Name,
				"input": input,
			}
			applyCacheControl(tb, part.CacheControl)
			content = append(content, tb)
		case IRPartToolResult:
			tb := map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": part.ToolResult.ToolCallID,
			}
			if len(part.ToolResult.Content) > 0 {
				resultContent := encodeAnthropicParts(part.ToolResult.Content)
				if len(resultContent) > 0 {
					tb["content"] = resultContent
				}
			}
			if part.ToolResult.Error != "" {
				tb["is_error"] = true
				tb["content"] = part.ToolResult.Error
			}
			applyCacheControl(tb, part.CacheControl)
			content = append(content, tb)
		}
	}
	return content
}

func encodeAnthropicToolChoice(tc *IRToolChoice) interface{} {
	switch tc.Type {
	case "auto":
		return map[string]interface{}{"type": "auto"}
	case "any", "required":
		return map[string]interface{}{"type": "any"}
	case "none":
		return map[string]interface{}{"type": "none"}
	case "specific":
		return map[string]interface{}{"type": "tool", "name": tc.Name}
	}
	return map[string]interface{}{"type": "auto"}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// DecodeResponse
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (a *anthropicMessagesAdapter) DecodeResponse(raw map[string]interface{}) (*IRResponse, error) {
	ir := &IRResponse{
		ID:    getString(raw, "id"),
		Model: getString(raw, "model"),
	}

	if content, ok := getList(raw, "content"); ok {
		ir.Content = decodeAnthropicContent(content)
	}

	ir.StopReason = mapAnthropicStopReason(getString(raw, "stop_reason"))

	// Usage: input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens.
	if u, ok := raw["usage"].(map[string]interface{}); ok {
		ir.Usage = &IRUsage{
			InputTokens:  getIntDefault(u, "input_tokens"),
			OutputTokens: getIntDefault(u, "output_tokens"),
		}
		if cct, ok := getInt(u, "cache_creation_input_tokens"); ok {
			ir.Usage.CacheCreationInputTokens = cct
		}
		if crt, ok := getInt(u, "cache_read_input_tokens"); ok {
			ir.Usage.CacheReadInputTokens = crt
		}
	}

	return ir, nil
}

func mapAnthropicStopReason(reason string) IRStopReason {
	switch reason {
	case "end_turn":
		return IRStopEndTurn
	case "max_tokens":
		return IRStopMaxTokens
	case "stop_sequence":
		return IRStopStopSequence
	case "tool_use":
		return IRStopToolUse
	}
	return IRStopEndTurn
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// EncodeResponse
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (a *anthropicMessagesAdapter) EncodeResponse(ir *IRResponse) (map[string]interface{}, error) {
	content := encodeAnthropicParts(ir.Content)

	// Auto-detect stop_reason from content when IR stop_reason is end_turn.
	// If any block is a tool_use, the actual stop reason is tool_use.
	stopReason := mapAnthropicEncodedStopReason(ir.StopReason)
	if stopReason == "end_turn" {
		for _, block := range content {
			if getString(block, "type") == "tool_use" {
				stopReason = "tool_use"
				break
			}
		}
	}

	resp := map[string]interface{}{
		"id":            ir.ID,
		"type":          "message",
		"role":          "assistant",
		"model":         ir.Model,
		"content":       ensureContentArray(content),
		"stop_reason":   stopReason,
		"stop_sequence": nil,
	}

	if ir.Usage != nil {
		usageMap := map[string]interface{}{
			"input_tokens":  ir.Usage.InputTokens,
			"output_tokens": ir.Usage.OutputTokens,
		}
		if ir.Usage.CacheReadInputTokens > 0 {
			usageMap["cache_read_input_tokens"] = ir.Usage.CacheReadInputTokens
		}
		if ir.Usage.CacheCreationInputTokens > 0 {
			usageMap["cache_creation_input_tokens"] = ir.Usage.CacheCreationInputTokens
		}
		resp["usage"] = usageMap
	}

	return resp, nil
}

func mapAnthropicEncodedStopReason(reason IRStopReason) string {
	switch reason {
	case IRStopEndTurn:
		return "end_turn"
	case IRStopMaxTokens:
		return "max_tokens"
	case IRStopStopSequence:
		return "stop_sequence"
	case IRStopToolUse:
		return "tool_use"
	}
	return "end_turn"
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Stream State
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (a *anthropicMessagesAdapter) NewStreamState() interface{} {
	return &anthropicStreamState{
		textStarted:      make(map[int]bool),
		textStopped:      make(map[int]bool),
		reasoningStarted: make(map[int]bool),
		reasoningStopped: make(map[int]bool),
		toolStarted:      make(map[int]bool),
		toolStopped:      make(map[int]bool),
		toolBlockIDs:     make(map[int]string),
		toolBlockNames:   make(map[int]string),
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// DecodeStreamEvent
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (a *anthropicMessagesAdapter) DecodeStreamEvent(raw map[string]interface{}, state interface{}) ([]*IRStreamEvent, error) {
	st, ok := state.(*anthropicStreamState)
	if !ok {
		return nil, fmt.Errorf("anthropic: invalid stream state type")
	}

	eventType := getString(raw, "type")
	switch eventType {

	case "message_start":
		msg, _ := raw["message"].(map[string]interface{})
		event := &IRStreamEvent{
			Type:          IRStreamMessageStart,
			ResponseID:    getString(msg, "id"),
			ResponseModel: getString(msg, "model"),
		}
		if u, ok := msg["usage"].(map[string]interface{}); ok {
			event.Usage = &IRUsage{
				InputTokens:  getIntDefault(u, "input_tokens"),
				OutputTokens: getIntDefault(u, "output_tokens"),
			}
			st.accumulatedInputTokens = event.Usage.InputTokens
			st.accumulatedCacheReadTokens = event.Usage.CacheReadInputTokens
			st.accumulatedCacheCreationTokens = event.Usage.CacheCreationInputTokens
		}
		return []*IRStreamEvent{event}, nil

	case "content_block_start":
		block, _ := raw["content_block"].(map[string]interface{})
		idx := getIntDefault(raw, "index")
		blockType := getString(block, "type")

		switch blockType {
		case "tool_use":
			st.toolStarted[idx] = true
			st.toolBlockIDs[idx] = getString(block, "id")
			st.toolBlockNames[idx] = getString(block, "name")

			return []*IRStreamEvent{{
				Type:  IRStreamContentStart,
				Index: idx,
				Part: &IRContentPart{
					Type: IRPartToolCall,
					ToolCall: &IRToolCallPart{
						ID:   getString(block, "id"),
						Name: getString(block, "name"),
					},
				},
			}}, nil

		case "thinking", "redacted_thinking":
			st.reasoningStarted[idx] = true
			thinkingContent := getString(block, "thinking")
			subtype := "thinking"
			encryptedContent := ""
			if blockType == "redacted_thinking" {
				thinkingContent = ""
				subtype = "redacted_thinking"
				encryptedContent = getString(block, "data")
			}
			return []*IRStreamEvent{{
				Type:  IRStreamContentStart,
				Index: idx,
				Part: &IRContentPart{
					Type: IRPartReasoning,
					Reasoning: &IRReasoningPart{
						Content:          thinkingContent,
						Signature:        getString(block, "signature"),
						Subtype:          subtype,
						EncryptedContent: encryptedContent,
					},
				},
			}}, nil

		default: // text
			st.textStarted[idx] = true
			return []*IRStreamEvent{{
				Type:  IRStreamContentStart,
				Index: idx,
				Part: &IRContentPart{
					Type: IRPartText,
				},
			}}, nil
		}

	case "content_block_delta":
		delta, _ := raw["delta"].(map[string]interface{})
		idx := getIntDefault(raw, "index")
		deltaType := getString(delta, "type")

		switch deltaType {
		case "text_delta":
			// Do not send deltas after content_block_stop.
			if st.textStopped[idx] {
				return nil, nil
			}
			return []*IRStreamEvent{{
				Type:      IRStreamContentDelta,
				Index:     idx,
				DeltaText: getString(delta, "text"),
			}}, nil

		case "input_json_delta":
			if st.toolStopped[idx] {
				return nil, nil
			}
			return []*IRStreamEvent{{
				Type:      IRStreamContentDelta,
				Index:     idx,
				DeltaJSON: getString(delta, "partial_json"),
			}}, nil

		case "thinking_delta":
			return []*IRStreamEvent{{
				Type:      IRStreamContentDelta,
				Index:     idx,
				DeltaText: getString(delta, "thinking"),
			}}, nil
		}

	case "content_block_stop":
		idx := getIntDefault(raw, "index")
		// Mark the block as stopped.
		if st.textStarted[idx] {
			st.textStopped[idx] = true
		}
		if st.toolStarted[idx] {
			st.toolStopped[idx] = true
		}
		return []*IRStreamEvent{{
			Type:  IRStreamContentStop,
			Index: idx,
		}}, nil

	case "message_delta":
		delta, _ := raw["delta"].(map[string]interface{})
		var usage *IRUsage
		if u, ok := raw["usage"].(map[string]interface{}); ok {
			usage = &IRUsage{
				InputTokens:  st.accumulatedInputTokens,
				OutputTokens: getIntDefault(u, "output_tokens"),
				TotalTokens:  st.accumulatedInputTokens + getIntDefault(u, "output_tokens"),
			}
			if i, ok := getInt(u, "input_tokens"); ok && i > 0 {
				usage.InputTokens = i
			}
		}
		return []*IRStreamEvent{{
			Type:       IRStreamMessageDelta,
			StopReason: mapAnthropicStopReason(getString(delta, "stop_reason")),
			Usage:      usage,
		}}, nil

	case "message_stop":
		return []*IRStreamEvent{{Type: IRStreamDone}}, nil

	case "error":
		err, _ := raw["error"].(map[string]interface{})
		return []*IRStreamEvent{{
			Type:         IRStreamError,
			ErrorMessage: fmt.Sprintf("%s: %s", getString(err, "type"), getString(err, "message")),
			ErrorType:    getString(err, "type"),
		}}, nil
	}

	return nil, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// EncodeStreamEvent
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (a *anthropicMessagesAdapter) EncodeStreamEvent(ir *IRStreamEvent, state interface{}) ([]map[string]interface{}, error) {
	st, _ := state.(*anthropicStreamState)

	switch ir.Type {

	case IRStreamMessageStart:
		msgEvt := map[string]interface{}{
			"id":      ir.ResponseID,
			"type":    "message",
			"role":    "assistant",
			"model":   ir.ResponseModel,
			"content": []interface{}{},
		}
		if ir.Usage != nil {
			usageMap := map[string]interface{}{
				"input_tokens":  ir.Usage.InputTokens,
				"output_tokens": ir.Usage.OutputTokens,
			}
			if ir.Usage.CacheReadInputTokens > 0 {
				usageMap["cache_read_input_tokens"] = ir.Usage.CacheReadInputTokens
			}
			if ir.Usage.CacheCreationInputTokens > 0 {
				usageMap["cache_creation_input_tokens"] = ir.Usage.CacheCreationInputTokens
			}
			msgEvt["usage"] = usageMap
		}
		return []map[string]interface{}{{
			"type":    "message_start",
			"message": msgEvt,
		}}, nil

	case IRStreamContentStart:
		if ir.Part == nil {
			if st != nil {
				st.textStarted[ir.Index] = true
			}
			return []map[string]interface{}{{
				"type":  "content_block_start",
				"index": ir.Index,
				"content_block": map[string]interface{}{
					"type": "text",
					"text": "",
				},
			}}, nil
		}

		switch ir.Part.Type {
		case IRPartToolCall:
			if st != nil {
				st.toolStarted[ir.Index] = true
				st.toolBlockIDs[ir.Index] = ir.Part.ToolCall.ID
				st.toolBlockNames[ir.Index] = ir.Part.ToolCall.Name
			}
			return []map[string]interface{}{{
				"type":  "content_block_start",
				"index": ir.Index,
				"content_block": map[string]interface{}{
					"type":  "tool_use",
					"id":    ir.Part.ToolCall.ID,
					"name":  ir.Part.ToolCall.Name,
					"input": map[string]interface{}{},
				},
			}}, nil

		case IRPartReasoning:
			if st != nil {
				st.textStarted[ir.Index] = true
			}
			tb := map[string]interface{}{
				"type":     "thinking",
				"thinking": ir.Part.Reasoning.Content,
			}
			if ir.Part.Reasoning.Signature != "" {
				tb["signature"] = ir.Part.Reasoning.Signature
			}
			return []map[string]interface{}{{
				"type":          "content_block_start",
				"index":         ir.Index,
				"content_block": tb,
			}}, nil

		default: // text
			if st != nil {
				st.textStarted[ir.Index] = true
			}
			return []map[string]interface{}{{
				"type":  "content_block_start",
				"index": ir.Index,
				"content_block": map[string]interface{}{
					"type": "text",
					"text": "",
				},
			}}, nil
		}

	case IRStreamContentDelta:
		if ir.DeltaText != "" {
			// Check if this is a thinking delta or text delta.
			// If Part is set and is reasoning, use thinking_delta.
			if ir.Part != nil && ir.Part.Type == IRPartReasoning {
				// Auto-inject content_block_start for thinking if not started
				var result []map[string]interface{}
				if st != nil && !st.textStarted[ir.Index] {
					st.textStarted[ir.Index] = true
					tb := map[string]interface{}{
						"type":     "thinking",
						"thinking": "",
					}
					result = append(result, map[string]interface{}{
						"type":          "content_block_start",
						"index":         ir.Index,
						"content_block": tb,
					})
				}
				result = append(result, map[string]interface{}{
					"type":  "content_block_delta",
					"index": ir.Index,
					"delta": map[string]interface{}{
						"type":     "thinking_delta",
						"thinking": ir.DeltaText,
					},
				})
				return result, nil
			}

			// Auto-inject content_block_start for text if not started
			var result []map[string]interface{}
			if st != nil && !st.textStarted[ir.Index] {
				st.textStarted[ir.Index] = true
				result = append(result, map[string]interface{}{
					"type":  "content_block_start",
					"index": ir.Index,
					"content_block": map[string]interface{}{
						"type": "text",
						"text": "",
					},
				})
			}
			result = append(result, map[string]interface{}{
				"type":  "content_block_delta",
				"index": ir.Index,
				"delta": map[string]interface{}{
					"type": "text_delta",
					"text": ir.DeltaText,
				},
			})
			return result, nil
		}
		if ir.DeltaJSON != "" {
			// Auto-inject content_block_start for tool_use if not started
			var result []map[string]interface{}
			if st != nil && !st.toolStarted[ir.Index] {
				st.toolStarted[ir.Index] = true
				toolID := fmt.Sprintf("toolu_stream_%d", ir.Index)
				toolName := ""
				if st.toolBlockIDs != nil && st.toolBlockIDs[ir.Index] != "" {
					toolID = st.toolBlockIDs[ir.Index]
				}
				if st.toolBlockNames != nil && st.toolBlockNames[ir.Index] != "" {
					toolName = st.toolBlockNames[ir.Index]
				}
				result = append(result, map[string]interface{}{
					"type":  "content_block_start",
					"index": ir.Index,
					"content_block": map[string]interface{}{
						"type":  "tool_use",
						"id":    toolID,
						"name":  toolName,
						"input": map[string]interface{}{},
					},
				})
			}
			result = append(result, map[string]interface{}{
				"type":  "content_block_delta",
				"index": ir.Index,
				"delta": map[string]interface{}{
					"type":         "input_json_delta",
					"partial_json": ir.DeltaJSON,
				},
			})
			return result, nil
		}
		return nil, nil

	case IRStreamContentStop:
		if st != nil {
			if st.textStarted[ir.Index] {
				st.textStopped[ir.Index] = true
			}
			if st.toolStarted[ir.Index] {
				st.toolStopped[ir.Index] = true
			}
		}
		return []map[string]interface{}{{
			"type":  "content_block_stop",
			"index": ir.Index,
		}}, nil

	case IRStreamMessageDelta:
		evt := map[string]interface{}{
			"type": "message_delta",
			"delta": map[string]interface{}{
				"stop_reason":   mapAnthropicEncodedStopReason(ir.StopReason),
				"stop_sequence": nil,
			},
		}
		if ir.Usage != nil {
			usageMap := map[string]interface{}{
				"input_tokens":  ir.Usage.InputTokens,
				"output_tokens": ir.Usage.OutputTokens,
			}
			if ir.Usage.CacheReadInputTokens > 0 {
				usageMap["cache_read_input_tokens"] = ir.Usage.CacheReadInputTokens
			}
			if ir.Usage.CacheCreationInputTokens > 0 {
				usageMap["cache_creation_input_tokens"] = ir.Usage.CacheCreationInputTokens
			}
			evt["usage"] = usageMap
		}
		return []map[string]interface{}{evt}, nil

	case IRStreamDone:
		return []map[string]interface{}{{"type": "message_stop"}}, nil

	case IRStreamError:
		evt := map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "error",
				"message": ir.ErrorMessage,
			},
		}
		if ir.ErrorType != "" {
			evt["error"].(map[string]interface{})["type"] = ir.ErrorType
		}
		return []map[string]interface{}{evt}, nil
	}

	return nil, nil
}
