package modelrouter

import (
	"encoding/json"
	"fmt"
)

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
	textStarted    map[int]bool
	textStopped    map[int]bool
	toolStarted    map[int]bool
	toolStopped    map[int]bool
	toolBlockIDs   map[int]string
	toolBlockNames map[int]string

	// accumulatedInputTokens tracks input_tokens from message_start for merging into message_delta.
	// Anthropic sends input_tokens in message_start and output_tokens in message_delta;
	// we need to combine them to produce a complete IRUsage.
	accumulatedInputTokens int
}

// anthropicMessagesAdapter implements ProtocolAdapter for the Anthropic Messages API.
type anthropicMessagesAdapter struct{}

func init() {
	RegisterAdapter(&anthropicMessagesAdapter{})
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

	// System: string or array of text blocks.
	if system, ok := raw["system"]; ok {
		switch v := system.(type) {
		case string:
			ir.System = v
		case []interface{}:
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok && getString(m, "type") == "text" {
					ir.System += getString(m, "text")
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
				parts = append(parts, IRContentPart{Type: IRPartText, Text: getString(m, "text")})
			case "thinking":
				parts = append(parts, IRContentPart{
					Type: IRPartReasoning,
					Reasoning: &IRReasoningPart{
						Content:   getString(m, "thinking"),
						Signature: getString(m, "signature"),
					},
				})
			case "redacted_thinking":
				parts = append(parts, IRContentPart{
					Type: IRPartReasoning,
					Reasoning: &IRReasoningPart{
						Content:   "[REDACTED]",
						Signature: getString(m, "signature"),
					},
				})
			case "tool_use":
				input, _ := m["input"].(map[string]interface{})
				inputJSON, _ := json.Marshal(input)
				parts = append(parts, IRContentPart{
					ID:   getString(m, "id"),
					Type: IRPartToolCall,
					ToolCall: &IRToolCallPart{
						ID:            getString(m, "id"),
						Name:          getString(m, "name"),
						ArgumentsRaw:  string(inputJSON),
						ArgumentsJSON: input,
						Status:        "completed",
					},
				})
			case "tool_result":
				resultContent := decodeToolResultContent(m["content"])
				parts = append(parts, IRContentPart{
					Type: IRPartToolResult,
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
			Type:        "function",
			Name:        getString(m, "name"),
			Description: getString(m, "description"),
			Parameters:  params,
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

	if ir.System != "" {
		body["system"] = ir.System
	}
	if ir.Temperature != nil {
		body["temperature"] = *ir.Temperature
	}
	if ir.TopP != nil {
		body["top_p"] = *ir.TopP
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
			tools = append(tools, map[string]interface{}{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.Parameters,
			})
		}
		body["tools"] = tools
	}

	if ir.ToolChoice != nil {
		body["tool_choice"] = encodeAnthropicToolChoice(ir.ToolChoice)
	}

	return body, nil
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

func encodeAnthropicParts(parts []IRContentPart) []map[string]interface{} {
	var content []map[string]interface{}
	for _, part := range parts {
		switch part.Type {
		case IRPartText:
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": part.Text,
			})
		case IRPartReasoning:
			tb := map[string]interface{}{
				"type":     "thinking",
				"thinking": part.Reasoning.Content,
			}
			if part.Reasoning.Signature != "" {
				tb["signature"] = part.Reasoning.Signature
			}
			content = append(content, tb)
		case IRPartToolCall:
			var input interface{}
			if part.ToolCall.ArgumentsJSON != nil {
				input = part.ToolCall.ArgumentsJSON
			} else if part.ToolCall.ArgumentsRaw != "" {
				_ = json.Unmarshal([]byte(part.ToolCall.ArgumentsRaw), &input)
			}
			content = append(content, map[string]interface{}{
				"type":  "tool_use",
				"id":    part.ToolCall.ID,
				"name":  part.ToolCall.Name,
				"input": input,
			})
		case IRPartToolResult:
			tb := map[string]interface{}{
				"type":         "tool_result",
				"tool_use_id":  part.ToolResult.ToolCallID,
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
		textStarted:    make(map[int]bool),
		textStopped:    make(map[int]bool),
		toolStarted:    make(map[int]bool),
		toolStopped:    make(map[int]bool),
		toolBlockIDs:   make(map[int]string),
		toolBlockNames: make(map[int]string),
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
			st.accumulatedInputTokens = getIntDefault(u, "input_tokens")
			event.Usage = &IRUsage{
				InputTokens:  st.accumulatedInputTokens,
				OutputTokens: getIntDefault(u, "output_tokens"),
			}
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
				Type: IRStreamContentStart,
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
			thinkingContent := getString(block, "thinking")
			if blockType == "redacted_thinking" {
				thinkingContent = "[REDACTED]"
			}
			return []*IRStreamEvent{{
				Type: IRStreamContentStart,
				Index: idx,
				Part: &IRContentPart{
					Type: IRPartReasoning,
					Reasoning: &IRReasoningPart{
						Content:   thinkingContent,
						Signature: getString(block, "signature"),
					},
				},
			}}, nil

		default: // text
			st.textStarted[idx] = true
			return []*IRStreamEvent{{
				Type: IRStreamContentStart,
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
				"type":  "content_block_start",
				"index": ir.Index,
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
