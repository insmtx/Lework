package llmprotocol

import (
	"fmt"
	"strconv"
)

// responsesStreamState is private stream state for the OpenAI Responses adapter.
// It accumulates text, tool calls, and tracking info during streaming.
type responsesStreamState struct {
	responseID    string
	model         string
	toolCallIDs   map[int]string
	toolCallNames map[int]string
	toolCallArgs  map[int]string
	textByIndex   map[int]string
	itemIDs       map[int]string
	itemTypes     map[int]IRPartType // index → text / tool_call / reasoning (protocol encoding state)
	stopReason    IRStopReason
	usage         *IRUsage
}

// openAIResponsesAdapter implements ProtocolAdapter for the OpenAI Responses API.
type openAIResponsesAdapter struct{}

func init() {
	registerAdapterOnInit(&openAIResponsesAdapter{})
}

// Protocol returns ProtocolOpenAIResponses.
func (a *openAIResponsesAdapter) Protocol() Protocol {
	return ProtocolOpenAIResponses
}

// =============================================================================
// DecodeRequest — Responses API → IR
// =============================================================================

// DecodeRequest converts a Responses API request body into canonical IR form.
func (a *openAIResponsesAdapter) DecodeRequest(raw map[string]interface{}) (*IRRequest, error) {
	ir := &IRRequest{
		Model:  getString(raw, "model"),
		Stream: getBool(raw, "stream"),
	}

	ir.Instructions = getString(raw, "instructions")
	ir.System = ir.Instructions

	if input, ok := raw["input"]; ok {
		ir.Messages = decodeResponsesInput(input)
		// 将 developer/system 消息的内容合并到 ir.System，
		// 避免 Chat 编码时被跳过（encodeOpenAIChatMessages 只保留 ir.System）。
		var kept []IRMessage
		for _, msg := range ir.Messages {
			if msg.Role == IRRoleSystem {
				for _, part := range msg.Parts {
					if part.Type == IRPartText && part.Text != "" {
						if ir.System != "" {
							ir.System += "\n\n"
						}
						ir.System += part.Text
					}
				}
			} else {
				kept = append(kept, msg)
			}
		}
		ir.Messages = kept
	}

	if t, ok := getFloat(raw, "temperature"); ok {
		ir.Temperature = &t
	}
	if p, ok := getFloat(raw, "top_p"); ok {
		ir.TopP = &p
	}
	if mt, ok := getInt(raw, "max_output_tokens"); ok {
		ir.MaxTokens = mt
	}
	if s, ok := getStringList(raw, "stop"); ok {
		ir.Stop = s
	}
	if s, ok := getInt(raw, "seed"); ok {
		ir.Seed = &s
	}
	ir.User = getString(raw, "user")

	ir.ReasoningEffort = getString(raw, "reasoning_effort")
	if ir.ReasoningEffort == "" {
		// Check for reasoning.effort nested object: {"reasoning": {"effort": "high"}}
		if reasoning, ok := raw["reasoning"].(map[string]interface{}); ok {
			ir.ReasoningEffort = getString(reasoning, "effort")
		}
	}

	if tools, ok := getList(raw, "tools"); ok {
		ir.Tools = decodeResponsesTools(tools)
	}

	if tc, ok := raw["tool_choice"]; ok {
		ir.ToolChoice = decodeResponsesToolChoice(tc)
	}

	return ir, nil
}

// decodeResponsesInput converts a Responses API input (string or typed items array)
// into IR messages with ContentParts.
func decodeResponsesInput(input interface{}) []IRMessage {
	switch v := input.(type) {
	case string:
		if v != "" {
			return []IRMessage{{
				Role: IRRoleUser,
				Parts: []IRContentPart{
					{Type: IRPartText, Text: v},
				},
			}}
		}
	case []interface{}:
		var msgs []IRMessage
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			itemType := getString(m, "type")
			if itemType == "" {
				itemType = "message"
			}

			switch itemType {
			case "message":
				role := getString(m, "role")
				msg := IRMessage{Role: decodeResponsesRole(role)}
				if content := m["content"]; content != nil {
					msg.Parts = decodeResponsesContent(content)
				}
				msgs = append(msgs, msg)
			case "function_call":
				args := make(map[string]interface{})
				if argStr := getString(m, "arguments"); argStr != "" {
					parseJSONString(argStr, &args)
				}
				msgs = append(msgs, IRMessage{
					Role: IRRoleAssistant,
					Parts: []IRContentPart{{
						Type: IRPartToolCall,
						ID:   getString(m, "call_id"),
						ToolCall: &IRToolCallPart{
							ID:            getString(m, "call_id"),
							Name:          getString(m, "name"),
							ArgumentsRaw:  getString(m, "arguments"),
							ArgumentsJSON: args,
							Status:        getString(m, "status"),
						},
					}},
				})
			case "function_call_output":
				msgs = append(msgs, IRMessage{
					Role: IRRoleTool,
					Parts: []IRContentPart{{
						Type: IRPartToolResult,
						ToolResult: &IRToolResultPart{
							ToolCallID: getString(m, "call_id"),
							Status:     getString(m, "status"),
							Content: []IRContentPart{
								{Type: IRPartText, Text: getString(m, "output")},
							},
						},
					}},
				})
			case "reasoning":
				msgs = append(msgs, IRMessage{
					Role: IRRoleAssistant,
					Parts: []IRContentPart{{
						Type: IRPartReasoning,
						Reasoning: &IRReasoningPart{
							Content: contentToString(m["summary"]),
						},
					}},
				})
			}
		}
		return msgs
	}
	return nil
}

// decodeResponsesRole maps Responses API roles to IR roles.
func decodeResponsesRole(role string) IRRole {
	switch role {
	case "user":
		return IRRoleUser
	case "assistant":
		return IRRoleAssistant
	case "system", "developer":
		return IRRoleSystem
	default:
		return IRRoleUser
	}
}

// decodeResponsesContent converts Responses API content (string or typed array)
// into IRContentParts.
func decodeResponsesContent(content interface{}) []IRContentPart {
	switch v := content.(type) {
	case string:
		return []IRContentPart{{Type: IRPartText, Text: v}}
	case []interface{}:
		return decodeContentItems(v)
	case []map[string]interface{}:
		items := make([]interface{}, len(v))
		for i, m := range v {
			items[i] = m
		}
		parts := decodeContentItems(items)
		return parts
	}
	return nil
}

func decodeContentItems(items []interface{}) []IRContentPart {
	var parts []IRContentPart
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		t := getString(m, "type")
		switch t {
		case "input_text", "output_text", "text":
			parts = append(parts, IRContentPart{
				Type: IRPartText,
				Text: getString(m, "text"),
			})
		case "refusal":
			parts = append(parts, IRContentPart{
				Type: IRPartRefusal,
				Refusal: &IRRefusalPart{
					Text: getString(m, "refusal"),
				},
			})
		}
	}
	return parts
}

// decodeResponsesTools converts Responses API flat tools into IRToolDecls.
func decodeResponsesTools(raw []interface{}) []IRToolDecl {
	var tools []IRToolDecl
	for _, r := range raw {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if getString(m, "type") != "function" {
			continue
		}
		params, _ := m["parameters"].(map[string]interface{})
		tools = append(tools, IRToolDecl{
			Type:        "function",
			Name:        getString(m, "name"),
			Description: getString(m, "description"),
			Parameters:  params,
		})
	}
	return tools
}

// decodeResponsesToolChoice converts Responses API tool_choice to IRToolChoice.
func decodeResponsesToolChoice(tc interface{}) *IRToolChoice {
	switch v := tc.(type) {
	case string:
		return &IRToolChoice{Type: v}
	case map[string]interface{}:
		t := getString(v, "type")
		if t == "function" {
			return &IRToolChoice{Type: "specific", Name: getString(v, "name")}
		}
		return &IRToolChoice{Type: t, Name: getString(v, "name")}
	}
	return nil
}

// =============================================================================
// EncodeRequest — IR → Responses API
// =============================================================================

// EncodeRequest converts canonical IR into a Responses API request body.
func (a *openAIResponsesAdapter) EncodeRequest(ir *IRRequest) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"model": ir.Model,
	}

	// Determine if system messages should be skipped (instructions will be used instead)
	skipSystemMessages := ir.Instructions != "" || ir.System != ""

	// Encode input
	if len(ir.Messages) == 1 && ir.Messages[0].Role == IRRoleUser && !hasToolContent(ir.Messages[0]) {
		// Single user text-only message → string input
		body["input"] = ir.Messages[0].GetTextContent()
	} else {
		body["input"] = encodeResponsesInput(ir.Messages, skipSystemMessages)
	}

	// Instructions
	if ir.Instructions != "" {
		body["instructions"] = ir.Instructions
	} else if ir.System != "" {
		body["instructions"] = ir.System
	}

	if ir.Stream {
		body["stream"] = true
	}
	if ir.Temperature != nil {
		body["temperature"] = *ir.Temperature
	}
	if ir.TopP != nil {
		body["top_p"] = *ir.TopP
	}
	if ir.MaxTokens > 0 {
		body["max_output_tokens"] = ir.MaxTokens
	}
	if len(ir.Stop) > 0 {
		body["stop"] = ir.Stop
	}
	if ir.Seed != nil {
		body["seed"] = *ir.Seed
	}
	if ir.User != "" {
		body["user"] = ir.User
	}
	if ir.ReasoningEffort != "" {
		body["reasoning_effort"] = ir.ReasoningEffort
	}

	if len(ir.Tools) > 0 {
		var tools []interface{}
		for _, t := range ir.Tools {
			tool := map[string]interface{}{
				"type":        "function",
				"name":        t.Name,
				"description": t.Description,
			}
			if t.Parameters != nil {
				tool["parameters"] = t.Parameters
			}
			tools = append(tools, tool)
		}
		body["tools"] = tools
	}

	if ir.ToolChoice != nil {
		body["tool_choice"] = encodeResponsesToolChoice(ir.ToolChoice)
	}

	return body, nil
}

// encodeResponsesToolChoice converts IRToolChoice to Responses API format.
func encodeResponsesToolChoice(tc *IRToolChoice) interface{} {
	switch tc.Type {
	case "auto":
		return "auto"
	case "none":
		return "none"
	case "required":
		return "required"
	case "specific":
		return map[string]interface{}{
			"type": "function",
			"name": tc.Name,
		}
	}
	return "auto"
}

// encodeResponsesInput converts IR messages to Responses API input items.
func encodeResponsesInput(msgs []IRMessage, skipSystemMessages bool) []map[string]interface{} {
	var items []map[string]interface{}

	for _, m := range msgs {
		if skipSystemMessages && m.Role == IRRoleSystem {
			continue
		}

		switch m.Role {
		case IRRoleUser, IRRoleAssistant, IRRoleSystem:
			role := "user"
			if m.Role == IRRoleAssistant {
				role = "assistant"
			} else if m.Role == IRRoleSystem {
				role = "system"
			}

			var content []map[string]interface{}
			var functionCalls []map[string]interface{}
			isInputMessage := m.Role == IRRoleUser || m.Role == IRRoleSystem
			textType := "input_text"
			if !isInputMessage {
				textType = "output_text"
			}

			for _, part := range m.Parts {
				switch part.Type {
				case IRPartText:
					content = append(content, map[string]interface{}{
						"type": textType,
						"text": part.Text,
					})
				case IRPartToolCall:
					if part.ToolCall != nil {
						args := part.ToolCall.ArgumentsRaw
						if args == "" {
							args = "{}"
						}
						functionCalls = append(functionCalls, map[string]interface{}{
							"type":      "function_call",
							"call_id":   part.ToolCall.ID,
							"id":        part.ToolCall.ID,
							"name":      part.ToolCall.Name,
							"arguments": args,
							"status":    part.ToolCall.Status,
						})
					}
				}
			}

			// message item (only if there is text content or no function calls)
			if len(content) > 0 || len(functionCalls) == 0 {
				if content == nil {
					content = []map[string]interface{}{}
				}
				items = append(items, map[string]interface{}{
					"type":    "message",
					"role":    role,
					"content": content,
				})
			}

			// function_call items (separate siblings)
			items = append(items, functionCalls...)

		case IRRoleTool:
			for _, part := range m.Parts {
				if part.Type == IRPartToolResult && part.ToolResult != nil {
					output := ""
					for _, c := range part.ToolResult.Content {
						if c.Type == IRPartText {
							output += c.Text
						}
					}
					items = append(items, map[string]interface{}{
						"type":    "function_call_output",
						"call_id": part.ToolResult.ToolCallID,
						"output":  output,
					})
				}
			}
		}
	}

	return items
}

// hasToolContent checks if an IRMessage has tool-related parts.
func hasToolContent(m IRMessage) bool {
	for _, p := range m.Parts {
		if p.Type == IRPartToolCall || p.Type == IRPartToolResult {
			return true
		}
	}
	return false
}

// =============================================================================
// DecodeResponse — Responses API → IR
// =============================================================================

// DecodeResponse converts a Responses API response body into canonical IR form.
func (a *openAIResponsesAdapter) DecodeResponse(raw map[string]interface{}) (*IRResponse, error) {
	ir := &IRResponse{
		ID:      getString(raw, "id"),
		Model:   getString(raw, "model"),
		Created: getInt64(raw, "created_at"),
	}

	ir.StopReason = mapResponsesStatus(getString(raw, "status"))

	if output, ok := getList(raw, "output"); ok {
		hasFunctionCall := false
		for _, item := range output {
			m, _ := item.(map[string]interface{})
			itemType := getString(m, "type")
			switch itemType {
			case "message":
				if content, ok := m["content"]; ok {
					parts := decodeResponsesContent(content)
					ir.Content = append(ir.Content, parts...)
				}
			case "function_call":
				hasFunctionCall = true
				args := make(map[string]interface{})
				if argStr := getString(m, "arguments"); argStr != "" {
					parseJSONString(argStr, &args)
				}
				ir.Content = append(ir.Content, IRContentPart{
					Type: IRPartToolCall,
					ID:   getString(m, "call_id"),
					ToolCall: &IRToolCallPart{
						ID:            getString(m, "call_id"),
						Name:          getString(m, "name"),
						ArgumentsRaw:  getString(m, "arguments"),
						ArgumentsJSON: args,
						Status:        getString(m, "status"),
					},
				})
			case "reasoning":
				ir.Content = append(ir.Content, IRContentPart{
					Type: IRPartReasoning,
					Reasoning: &IRReasoningPart{
						Content: contentToString(m["summary"]),
					},
				})
			}
		}
		if hasFunctionCall {
			ir.StopReason = IRStopToolUse
		}
	}

	if u, ok := raw["usage"].(map[string]interface{}); ok {
		ir.Usage = decodeResponsesUsage(u)
	}

	return ir, nil
}

// mapResponsesStatus maps Responses API status to IR stop reason.
func mapResponsesStatus(status string) IRStopReason {
	switch status {
	case "completed":
		return IRStopEndTurn
	case "incomplete":
		return IRStopMaxTokens
	case "failed":
		return IRStopError
	}
	return IRStopEndTurn
}

// decodeResponsesUsage converts Responses API usage to IRUsage.
func decodeResponsesUsage(u map[string]interface{}) *IRUsage {
	usage := &IRUsage{
		InputTokens:  getIntDefault(u, "input_tokens"),
		OutputTokens: getIntDefault(u, "output_tokens"),
	}
	if total, ok := getInt(u, "total_tokens"); ok {
		usage.TotalTokens = total
	} else {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if promptDetails, ok := u["prompt_tokens_details"].(map[string]interface{}); ok {
		usage.CacheReadInputTokens = getIntDefault(promptDetails, "cached_tokens")
	}
	if completionDetails, ok := u["completion_tokens_details"].(map[string]interface{}); ok {
		usage.ReasoningTokens = getIntDefault(completionDetails, "reasoning_tokens")
	}
	return usage
}

// =============================================================================
// EncodeResponse — IR → Responses API
// =============================================================================

// EncodeResponse converts canonical IR into a Responses API response body.
func (a *openAIResponsesAdapter) EncodeResponse(ir *IRResponse) (map[string]interface{}, error) {
	var output []interface{}
	var textParts []map[string]interface{}

	for _, part := range ir.Content {
		switch part.Type {
		case IRPartText:
			textParts = append(textParts, map[string]interface{}{
				"type": "output_text",
				"text": part.Text,
			})
		case IRPartToolCall:
			if len(textParts) > 0 {
				output = append(output, map[string]interface{}{
					"id":      ensureMsgID("msg_resp"),
					"type":    "message",
					"role":    "assistant",
					"content": textParts,
				})
				textParts = nil
			}
			args := "{}"
			if part.ToolCall != nil {
				if part.ToolCall.ArgumentsRaw != "" {
					args = part.ToolCall.ArgumentsRaw
				} else if b, err := marshalJSON(part.ToolCall.ArgumentsJSON); err == nil && len(b) > 2 {
					args = string(b)
				}
			}
			status := part.ToolCall.Status
			if status == "" {
				status = "completed"
			}
			output = append(output, map[string]interface{}{
				"type":      "function_call",
				"id":        part.ToolCall.ID,
				"call_id":   part.ToolCall.ID,
				"name":      part.ToolCall.Name,
				"arguments": args,
				"status":    status,
			})
		}
	}

	if len(textParts) > 0 {
		output = append(output, map[string]interface{}{
			"id":      ensureMsgID("msg_resp"),
			"type":    "message",
			"role":    "assistant",
			"content": textParts,
		})
	}

	status := "completed"
	switch ir.StopReason {
	case IRStopMaxTokens:
		status = "incomplete"
	case IRStopError:
		status = "failed"
	}

	resp := map[string]interface{}{
		"id":         ensurePrefix(ir.ID, "resp"),
		"object":     "response",
		"created_at": maybeNow(ir.Created),
		"model":      ir.Model,
		"output":     output,
		"status":     status,
	}

	if ir.Usage != nil {
		usage := map[string]interface{}{
			"input_tokens":  ir.Usage.InputTokens,
			"output_tokens": ir.Usage.OutputTokens,
			"total_tokens":  ir.Usage.TotalTokens,
		}
		if ir.Usage.CacheReadInputTokens > 0 {
			usage["prompt_tokens_details"] = map[string]interface{}{
				"cached_tokens": ir.Usage.CacheReadInputTokens,
			}
		}
		if ir.Usage.ReasoningTokens > 0 {
			usage["completion_tokens_details"] = map[string]interface{}{
				"reasoning_tokens": ir.Usage.ReasoningTokens,
			}
		}
		resp["usage"] = usage
	}

	return resp, nil
}

// =============================================================================
// NewStreamState — opaque state for streaming
// =============================================================================

// NewStreamState creates a new responsesStreamState.
func (a *openAIResponsesAdapter) NewStreamState() interface{} {
	return &responsesStreamState{
		toolCallIDs:   make(map[int]string),
		toolCallNames: make(map[int]string),
		toolCallArgs:  make(map[int]string),
		textByIndex:   make(map[int]string),
		itemIDs:       make(map[int]string),
		itemTypes:     make(map[int]IRPartType),
	}
}

// =============================================================================
// DecodeStreamEvent — Responses API SSE → IR events
// =============================================================================

// DecodeStreamEvent converts a Responses API SSE data payload to IR stream events.
func (a *openAIResponsesAdapter) DecodeStreamEvent(raw map[string]interface{}, state interface{}) ([]*IRStreamEvent, error) {
	st, _ := state.(*responsesStreamState)
	if st == nil {
		st = &responsesStreamState{}
	}
	eventType := getString(raw, "type")

	switch eventType {
	case "response.created":
		resp, _ := raw["response"].(map[string]interface{})
		respID := getString(resp, "id")
		respModel := getString(resp, "model")
		st.setResponseID(respID)
		st.setModel(respModel)
		return []*IRStreamEvent{{
			Type:          IRStreamMessageStart,
			ResponseID:    respID,
			ResponseModel: respModel,
		}}, nil

	case "response.output_item.added":
		item, _ := raw["item"].(map[string]interface{})
		idx := getIntDefault(raw, "output_index")
		itemType := getString(item, "type")

		if itemType == "function_call" {
			callID := getString(item, "call_id")
			name := getString(item, "name")
			st.toolCallIDs[idx] = callID
			st.toolCallNames[idx] = name
			st.toolCallArgs[idx] = ""
			return []*IRStreamEvent{{
				Type:  IRStreamContentStart,
				Index: idx,
				Part: &IRContentPart{
					Type: IRPartToolCall,
					ID:   callID,
					ToolCall: &IRToolCallPart{
						ID:   callID,
						Name: name,
					},
				},
			}}, nil
		}

		// text item (message)
		itemID := getString(item, "id")
		if itemID != "" {
			st.itemIDs[idx] = itemID
		}
		return []*IRStreamEvent{{
			Type:  IRStreamContentStart,
			Index: idx,
			Part: &IRContentPart{
				Type: IRPartText,
			},
		}}, nil

	case "response.content_part.added":
		// Skip — internal bookkeeping, no IR event
		return nil, nil

	case "response.output_text.delta", "response.text.delta":
		idx := getIntDefault(raw, "output_index")
		delta := getString(raw, "delta")
		st.appendText(idx, delta)
		return []*IRStreamEvent{{
			Type:      IRStreamContentDelta,
			Index:     idx,
			DeltaText: delta,
		}}, nil

	case "response.function_call_arguments.delta":
		idx := getIntDefault(raw, "output_index")
		delta := getString(raw, "delta")
		st.appendToolArg(idx, delta)
		return []*IRStreamEvent{{
			Type:      IRStreamContentDelta,
			Index:     idx,
			DeltaJSON: delta,
		}}, nil

	case "response.output_text.done":
		// Skip — the actual content stop is output_item.done
		return nil, nil

	case "response.content_part.done":
		// Skip — internal bookkeeping
		return nil, nil

	case "response.output_item.done":
		return []*IRStreamEvent{{
			Type:  IRStreamContentStop,
			Index: getIntDefault(raw, "output_index"),
		}}, nil

	case "response.done", "response.completed":
		resp, _ := raw["response"].(map[string]interface{})
		status := getString(resp, "status")
		var usage *IRUsage
		if u, ok := resp["usage"].(map[string]interface{}); ok {
			usage = decodeResponsesUsage(u)
		}
		st.updateStopReason(mapResponsesStatus(status))
		st.updateUsage(usage)
		return []*IRStreamEvent{{
			Type:       IRStreamMessageDelta,
			StopReason: st.stopReason,
			Usage:      st.usage,
		}, {
			Type:  IRStreamDone,
			Usage: st.usage,
		}}, nil

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

// =============================================================================
// EncodeStreamEvent — IR events → Responses API SSE payloads
// =============================================================================

// EncodeStreamEvent converts an IR stream event into Responses API SSE payloads.
// May return multiple payloads (e.g., text.done + content_part.done + output_item.done).
func (a *openAIResponsesAdapter) EncodeStreamEvent(ir *IRStreamEvent, state interface{}) ([]map[string]interface{}, error) {
	st, _ := state.(*responsesStreamState)
	if st == nil {
		st = &responsesStreamState{
			toolCallIDs:   make(map[int]string),
			toolCallNames: make(map[int]string),
			toolCallArgs:  make(map[int]string),
			textByIndex:   make(map[int]string),
			itemIDs:       make(map[int]string),
			itemTypes:     make(map[int]IRPartType),
		}
	}

	switch ir.Type {
	case IRStreamMessageStart:
		st.setResponse(ir)
		responseID := ensurePrefix(ir.ResponseID, "resp")
		if responseID == "resp-" {
			responseID = "resp_stream"
		}
		st.setResponseID(responseID)
		return []map[string]interface{}{{
			"type": "response.created",
			"response": map[string]interface{}{
				"id":         responseID,
				"object":     "response",
				"created_at": now(),
				"model":      ir.ResponseModel,
				"status":     "in_progress",
				"output":     []interface{}{},
			},
		}}, nil

	case IRStreamContentStart:
		if ir.Part != nil && ir.Part.Type == IRPartToolCall {
			tc := ir.Part.ToolCall
			callID := ""
			name := ""
			if tc != nil {
				callID = tc.ID
				name = tc.Name
			}
			st.toolCallIDs[ir.Index] = callID
			st.toolCallNames[ir.Index] = name
			st.toolCallArgs[ir.Index] = ""
			if callID == "" {
				callID = fmt.Sprintf("call_%d", ir.Index)
				st.toolCallIDs[ir.Index] = callID
			}
			st.itemTypes[ir.Index] = IRPartToolCall
			return []map[string]interface{}{{
				"type":         "response.output_item.added",
				"output_index": ir.Index,
				"item": map[string]interface{}{
					"type":      "function_call",
					"id":        callID,
					"call_id":   callID,
					"name":      name,
					"arguments": "",
					"status":    "in_progress",
				},
			}}, nil
		}

		// Text or reasoning content start
		itemID := st.itemID(ir.Index)
		if ir.Part != nil {
			st.itemTypes[ir.Index] = ir.Part.Type
		}
		return []map[string]interface{}{{
			"type":         "response.output_item.added",
			"output_index": ir.Index,
			"item": map[string]interface{}{
				"id":      itemID,
				"type":    "message",
				"status":  "in_progress",
				"role":    "assistant",
				"content": []interface{}{},
			},
		}, {
			"type":          "response.content_part.added",
			"item_id":       itemID,
			"output_index":  ir.Index,
			"content_index": 0,
			"part": map[string]interface{}{
				"type":        "output_text",
				"text":        "",
				"annotations": []interface{}{},
			},
		}}, nil

	case IRStreamContentDelta:
		if ir.DeltaText != "" {
			itemID := st.itemID(ir.Index)
			// StreamAggregator guarantees ContentStart precedes every ContentDelta.
			// No auto-start logic needed here.
			st.appendText(ir.Index, ir.DeltaText)
			return []map[string]interface{}{{
				"type":          "response.output_text.delta",
				"item_id":       itemID,
				"output_index":  ir.Index,
				"content_index": 0,
				"delta":         ir.DeltaText,
			}}, nil
		}
		if ir.DeltaJSON != "" {
			st.appendToolArg(ir.Index, ir.DeltaJSON)
			return []map[string]interface{}{{
				"type":         "response.function_call_arguments.delta",
				"output_index": ir.Index,
				"delta":        ir.DeltaJSON,
			}}, nil
		}
		return nil, nil

	case IRStreamContentStop:
		itemType, ok := st.itemTypes[ir.Index]
		if !ok {
			// No type recorded — upstream adapter or aggregator may have skipped ContentStart.
			// Safe fallback: skip encoding (won't panic, won't emit garbage).
			return nil, nil
		}

		if itemType == IRPartText || itemType == IRPartReasoning {
			itemID := st.itemID(ir.Index)
			text := st.text(ir.Index)
			part := map[string]interface{}{
				"type":        "output_text",
				"text":        text,
				"annotations": []interface{}{},
			}
			return []map[string]interface{}{{
				"type":          "response.output_text.done",
				"item_id":       itemID,
				"output_index":  ir.Index,
				"content_index": 0,
				"text":          text,
			}, {
				"type":          "response.content_part.done",
				"item_id":       itemID,
				"output_index":  ir.Index,
				"content_index": 0,
				"part":          part,
			}, {
				"type":         "response.output_item.done",
				"output_index": ir.Index,
				"item": map[string]interface{}{
					"id":      itemID,
					"type":    "message",
					"status":  "completed",
					"role":    "assistant",
					"content": []map[string]interface{}{part},
				},
			}}, nil
		}

		// Tool call stop
		callID := st.toolCallIDs[ir.Index]
		if callID == "" {
			callID = fmt.Sprintf("call_%d", ir.Index)
		}
		return []map[string]interface{}{{
			"type":         "response.function_call_arguments.done",
			"output_index": ir.Index,
		}, {
			"type":         "response.output_item.done",
			"output_index": ir.Index,
			"item": map[string]interface{}{
				"type":      "function_call",
				"id":        callID,
				"call_id":   callID,
				"name":      st.toolCallNames[ir.Index],
				"arguments": st.toolCallArgs[ir.Index],
				"status":    "completed",
			},
		}}, nil

	case IRStreamMessageDelta:
		st.setMessageDelta(ir)
		return nil, nil

	case IRStreamDone:
		// Build usage
		usage := map[string]interface{}{
			"input_tokens":  float64(0),
			"output_tokens": float64(0),
			"total_tokens":  float64(0),
		}
		eventUsage := ir.Usage
		if eventUsage == nil {
			eventUsage = st.usage
		}
		if eventUsage != nil {
			totalTokens := eventUsage.TotalTokens
			if totalTokens == 0 {
				totalTokens = eventUsage.InputTokens + eventUsage.OutputTokens
			}
			usage = map[string]interface{}{
				"input_tokens":  float64(eventUsage.InputTokens),
				"output_tokens": float64(eventUsage.OutputTokens),
				"total_tokens":  float64(totalTokens),
			}
			if eventUsage.CacheReadInputTokens > 0 {
				usage["prompt_tokens_details"] = map[string]interface{}{
					"cached_tokens": float64(eventUsage.CacheReadInputTokens),
				}
			}
			if eventUsage.ReasoningTokens > 0 {
				usage["completion_tokens_details"] = map[string]interface{}{
					"reasoning_tokens": float64(eventUsage.ReasoningTokens),
				}
			}
		}

		status := "completed"
		if st.stopReason == IRStopMaxTokens {
			status = "incomplete"
		} else if st.stopReason == IRStopError {
			status = "failed"
		}

		responseID := ensurePrefix(ir.ResponseID, "resp")
		if responseID == "resp-" {
			responseID = st.responseIDValue()
		}
		if responseID == "resp-" {
			responseID = "resp_stream"
		}
		model := ir.ResponseModel
		if model == "" {
			model = st.model
		}

		return []map[string]interface{}{{
			"type": "response.completed",
			"response": map[string]interface{}{
				"id":         responseID,
				"object":     "response",
				"created_at": now(),
				"model":      model,
				"status":     status,
				"output":     []interface{}{},
				"usage":      usage,
			},
		}}, nil

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

// =============================================================================
// responsesStreamState helper methods
// =============================================================================

func (s *responsesStreamState) setResponse(event *IRStreamEvent) {
	if event == nil {
		return
	}
	if event.ResponseID != "" {
		s.responseID = ensurePrefix(event.ResponseID, "resp")
	}
	if event.ResponseModel != "" {
		s.model = event.ResponseModel
	}
}

func (s *responsesStreamState) setResponseID(id string) {
	s.responseID = id
}

func (s *responsesStreamState) setModel(m string) {
	s.model = m
}

func (s *responsesStreamState) responseIDValue() string {
	if s.responseID == "" {
		return "resp_stream"
	}
	return s.responseID
}

func (s *responsesStreamState) itemID(index int) string {
	if s.itemIDs == nil {
		s.itemIDs = make(map[int]string)
	}
	if id := s.itemIDs[index]; id != "" {
		return id
	}
	id := "msg_stream_" + strconv.Itoa(index)
	s.itemIDs[index] = id
	return id
}

func (s *responsesStreamState) appendText(index int, delta string) {
	if s.textByIndex == nil {
		s.textByIndex = make(map[int]string)
	}
	s.textByIndex[index] += delta
}

func (s *responsesStreamState) appendToolArg(index int, delta string) {
	if s.toolCallArgs == nil {
		s.toolCallArgs = make(map[int]string)
	}
	s.toolCallArgs[index] += delta
}

func (s *responsesStreamState) text(index int) string {
	if s.textByIndex == nil {
		return ""
	}
	return s.textByIndex[index]
}

func (s *responsesStreamState) setMessageDelta(event *IRStreamEvent) {
	if event == nil {
		return
	}
	if event.StopReason != "" {
		s.stopReason = event.StopReason
	}
	if event.Usage != nil {
		s.usage = event.Usage
	}
}

func (s *responsesStreamState) updateStopReason(r IRStopReason) {
	s.stopReason = r
}

func (s *responsesStreamState) updateUsage(u *IRUsage) {
	if u != nil {
		s.usage = u
	}
}

// ensurePrefix ensures a string starts with a given prefix.
// If the string already starts with the prefix, it is returned unchanged.
// If empty, returns "prefix-". Otherwise returns "prefix-" + s.
func ensurePrefix(s, prefix string) string {
	if s == "" {
		return prefix + "-"
	}
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s
	}
	return prefix + "-" + s
}

// ensureMsgID generates a unique message ID with a counter.
var msgIDCounter int

func ensureMsgID(prefix string) string {
	msgIDCounter++
	return prefix + "_" + strconv.Itoa(msgIDCounter)
}
