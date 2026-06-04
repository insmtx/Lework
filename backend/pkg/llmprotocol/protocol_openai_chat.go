package llmprotocol

// openAIChatAdapter implements ProtocolAdapter for the OpenAI Chat Completions protocol.
type openAIChatAdapter struct{}

func init() {
	registerAdapterOnInit(&openAIChatAdapter{})
}

func (a *openAIChatAdapter) Protocol() Protocol { return ProtocolOpenAIChat }

// =============================================================================
// DecodeRequest — OpenAI Chat Completions request → IRRequest
// =============================================================================

func (a *openAIChatAdapter) DecodeRequest(raw map[string]interface{}) (*IRRequest, error) {
	ir := &IRRequest{
		Model:      getString(raw, "model"),
		Stream:     getBool(raw, "stream"),
		Extensions: make(map[string]map[string]interface{}),
	}

	if msgs, ok := getList(raw, "messages"); ok {
		ir.Messages = decodeOpenAIChatMessages(msgs)
	}

	// Extract system message from messages array into IRRequest.System
	for _, msg := range ir.Messages {
		if msg.Role == IRRoleSystem {
			for _, part := range msg.Parts {
				if part.Type == IRPartText {
					ir.System += part.Text
				}
			}
		}
	}

	if t, ok := getFloat(raw, "temperature"); ok {
		ir.Temperature = &t
	}
	if p, ok := getFloat(raw, "top_p"); ok {
		ir.TopP = &p
	}
	if mt, ok := getInt(raw, "max_tokens"); ok {
		ir.MaxTokens = mt
	}
	if mt, ok := getInt(raw, "max_completion_tokens"); ok && mt > ir.MaxTokens {
		ir.MaxTokens = mt
	}
	if stopList, ok := getStringList(raw, "stop"); ok {
		ir.Stop = stopList
	} else if s := getString(raw, "stop"); s != "" {
		ir.Stop = []string{s}
	}
	if s, ok := getInt(raw, "seed"); ok {
		ir.Seed = &s
	}
	ir.User = getString(raw, "user")
	ir.ReasoningEffort = getString(raw, "reasoning_effort")

	if tools, ok := getList(raw, "tools"); ok {
		ir.Tools = decodeOpenAIChatTools(tools)
	}
	if tc, ok := raw["tool_choice"]; ok {
		ir.ToolChoice = decodeOpenAIChatToolChoice(tc)
	}
	if rf, ok := raw["response_format"]; ok {
		if rfMap, ok := rf.(map[string]interface{}); ok {
			irf := &IRResponseFormat{Type: getString(rfMap, "type")}
			if irf.Type == "json_schema" {
				if schema, ok := rfMap["json_schema"].(map[string]interface{}); ok {
					irf.JSONSchema = schema
				}
			}
			ir.ResponseFormat = irf
		}
	}

	// Preserve OpenAI-specific fields in Extensions
	preserved := make(map[string]interface{})
	for k, v := range raw {
		switch k {
		case "model", "messages", "stream", "temperature", "top_p",
			"max_tokens", "max_completion_tokens", "stop", "tools",
			"tool_choice", "seed", "user", "reasoning_effort", "response_format":
			// handled above
		default:
			preserved[k] = v
		}
	}
	if len(preserved) > 0 {
		ir.Extensions["openai_chat"] = preserved
	}

	return ir, nil
}

// =============================================================================
// EncodeRequest — IRRequest → OpenAI Chat Completions request
// =============================================================================

func (a *openAIChatAdapter) EncodeRequest(ir *IRRequest) (map[string]interface{}, error) {
	body := a.encodeRequestBody(ir)

	// Restore Extensions
	if ext, ok := ir.Extensions["openai_chat"]; ok {
		for k, v := range ext {
			if _, exists := body[k]; !exists {
				body[k] = v
			}
		}
	}

	return body, nil
}

func (a *openAIChatAdapter) encodeRequestBody(ir *IRRequest) map[string]interface{} {
	body := map[string]interface{}{
		"model":    ir.Model,
		"messages": encodeOpenAIChatMessages(ir),
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
		body["max_completion_tokens"] = ir.MaxTokens
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
		body["tools"] = encodeOpenAIChatTools(ir.Tools)
	}
	if ir.ToolChoice != nil {
		body["tool_choice"] = encodeOpenAIChatToolChoice(ir.ToolChoice)
	}
	if ir.ResponseFormat != nil {
		rf := map[string]interface{}{"type": ir.ResponseFormat.Type}
		if ir.ResponseFormat.Type == "json_schema" && ir.ResponseFormat.JSONSchema != nil {
			rf["json_schema"] = ir.ResponseFormat.JSONSchema
		}
		body["response_format"] = rf
	}

	return body
}

// =============================================================================
// DecodeResponse — OpenAI Chat response → IRResponse
// =============================================================================

func (a *openAIChatAdapter) DecodeResponse(raw map[string]interface{}) (*IRResponse, error) {
	ir := &IRResponse{
		ID:      getString(raw, "id"),
		Model:   getString(raw, "model"),
		Created: getInt64(raw, "created"),
	}

	if choices, ok := getList(raw, "choices"); ok && len(choices) > 0 {
		choice, _ := choices[0].(map[string]interface{})
		msg, _ := choice["message"].(map[string]interface{})

		if content := msg["content"]; content != nil {
			if s, ok := content.(string); ok && s != "" {
				ir.Content = append(ir.Content, IRContentPart{Type: IRPartText, Text: s})
			}
		}

		if tcs, ok := getList(msg, "tool_calls"); ok {
			for _, tc := range tcs {
				tcm, _ := tc.(map[string]interface{})
				fn, _ := tcm["function"].(map[string]interface{})
				input := make(map[string]interface{})
				if args := getString(fn, "arguments"); args != "" {
					parseJSONString(args, &input)
				}
				ir.Content = append(ir.Content, IRContentPart{
					Type: IRPartToolCall,
					ToolCall: &IRToolCallPart{
						ID:            getString(tcm, "id"),
						Name:          getString(fn, "name"),
						ArgumentsJSON: input,
						Status:        "completed",
					},
				})
			}
		}

		// Save reasoning_content from DeepSeek/OpenAI thinking mode (mandatory round-trip)
		if reasoningContent := getString(msg, "reasoning_content"); reasoningContent != "" {
			ir.Content = append(ir.Content, IRContentPart{
				Type: IRPartReasoning,
				Reasoning: &IRReasoningPart{
					Content: reasoningContent,
				},
			})
		}

		ir.StopReason = mapOpenAIFinishReason(getString(choice, "finish_reason"))
	}

	if u, ok := raw["usage"].(map[string]interface{}); ok {
		ir.Usage = &IRUsage{
			InputTokens:  getIntDefault(u, "prompt_tokens"),
			OutputTokens: getIntDefault(u, "completion_tokens"),
			TotalTokens:  getIntDefault(u, "total_tokens"),
		}
		if promptDetails, ok := u["prompt_tokens_details"].(map[string]interface{}); ok {
			ir.Usage.CacheReadInputTokens = getIntDefault(promptDetails, "cached_tokens")
		}
		if completionDetails, ok := u["completion_tokens_details"].(map[string]interface{}); ok {
			ir.Usage.ReasoningTokens = getIntDefault(completionDetails, "reasoning_tokens")
		}
	}

	return ir, nil
}

// =============================================================================
// EncodeResponse — IRResponse → OpenAI Chat response
// =============================================================================

func (a *openAIChatAdapter) EncodeResponse(ir *IRResponse) (map[string]interface{}, error) {
	msg := map[string]interface{}{"role": "assistant"}
	var text string
	var toolCalls []interface{}

	for _, part := range ir.Content {
		switch part.Type {
		case IRPartText:
			text += part.Text
		case IRPartRefusal:
			text += part.Refusal.Text
		case IRPartReasoning:
			// reasoning content typically not sent in chat completions response body
		case IRPartToolCall:
			if part.ToolCall != nil {
				args := "{}"
				if part.ToolCall.ArgumentsJSON != nil {
					if b, err := marshalJSON(part.ToolCall.ArgumentsJSON); err == nil {
						args = string(b)
					}
				} else if part.ToolCall.ArgumentsRaw != "" {
					args = part.ToolCall.ArgumentsRaw
				}
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   part.ToolCall.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      part.ToolCall.Name,
						"arguments": args,
					},
				})
			}
		}
	}

	if text != "" {
		msg["content"] = text
	} else {
		msg["content"] = nil
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}

	finishReason := mapIRStopReasonToOpenAIFinish(ir.StopReason)

	resp := map[string]interface{}{
		"id":      ensureChatPrefix(ir.ID),
		"object":  "chat.completion",
		"created": maybeNow(ir.Created),
		"model":   ir.Model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"message":       msg,
				"finish_reason": finishReason,
			},
		},
	}

	if ir.Usage != nil {
		resp["usage"] = encodeOpenAIUsage(ir.Usage)
	}

	return resp, nil
}

// =============================================================================
// NewStreamState — create chat stream state
// =============================================================================

type chatStreamState struct {
	toolStartEmitted       map[int]bool // OpenAI tool_calls[index] 的 ContentStart 是否已从协议分片中 emit
	toolIndexToGlobalIndex map[int]int  // OpenAI tool_call.index → IR global index
	toolGlobalIndices      []int        // 所有 tool 的 IR global indices，finish_reason 遍历用
	nextGlobalIndex        int          // IR 全局 index 分配器
	responseID             string
	model                  string
}

func (a *openAIChatAdapter) NewStreamState() interface{} {
	return &chatStreamState{
		toolStartEmitted:       make(map[int]bool),
		toolIndexToGlobalIndex: make(map[int]int),
	}
}

// =============================================================================
// DecodeStreamEvent — OpenAI Chat SSE chunk → IRStreamEvent
// =============================================================================

func (a *openAIChatAdapter) DecodeStreamEvent(raw map[string]interface{}, state interface{}) ([]*IRStreamEvent, error) {
	st, _ := state.(*chatStreamState)

	choices, ok := getList(raw, "choices")
	if !ok || len(choices) == 0 {
		if usage, ok := raw["usage"].(map[string]interface{}); ok {
			irUsage := &IRUsage{
				InputTokens:  getIntDefault(usage, "prompt_tokens"),
				OutputTokens: getIntDefault(usage, "completion_tokens"),
				TotalTokens:  getIntDefault(usage, "total_tokens"),
			}
			if promptDetails, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok {
				irUsage.CacheReadInputTokens = getIntDefault(promptDetails, "cached_tokens")
			}
			if completionDetails, ok := usage["completion_tokens_details"].(map[string]interface{}); ok {
				irUsage.ReasoningTokens = getIntDefault(completionDetails, "reasoning_tokens")
			}
			return []*IRStreamEvent{{
				Type:  IRStreamMessageDelta,
				Usage: irUsage,
			}}, nil
		}
		return nil, nil
	}

	choice, _ := choices[0].(map[string]interface{})
	delta, _ := choice["delta"].(map[string]interface{})
	finishReason := getString(choice, "finish_reason")

	var events []*IRStreamEvent

	if id := getString(raw, "id"); id != "" && st.responseID == "" {
		st.responseID = id
	}
	if model := getString(raw, "model"); model != "" && st.model == "" {
		st.model = model
	}

	// message_start: delta.role == "assistant"
	if role := getString(delta, "role"); role == "assistant" {
		events = append(events, &IRStreamEvent{
			Type:          IRStreamMessageStart,
			ResponseID:    st.responseID,
			ResponseModel: st.model,
		})
	}

	// text delta: delta.content
	// StreamAggregator handles ContentStart autocomplete — adapter emits plain delta only.
	if content := getString(delta, "content"); content != "" {
		events = append(events, &IRStreamEvent{
			Type:      IRStreamContentDelta,
			DeltaText: content,
			Index:     0,
		})
	}

	// Reasoning content delta (DeepSeek thinking mode)
	if reasoningContent := getString(delta, "reasoning_content"); reasoningContent != "" {
		events = append(events, &IRStreamEvent{
			Type:      IRStreamContentDelta,
			Index:     0,
			DeltaText: reasoningContent,
			DeltaType: "reasoning",
		})
	}

	// tool_calls from delta
	if tcs, ok := getList(delta, "tool_calls"); ok {
		for _, tc := range tcs {
			tcm, _ := tc.(map[string]interface{})
			fn, _ := tcm["function"].(map[string]interface{})
			chatIdx := 0
			if f, ok := getFloat(tcm, "index"); ok {
				chatIdx = int(f)
			}
			id := getString(tcm, "id")
			args := getString(fn, "arguments")

			// content_part_start: tool_call with id present (first segment)
			if id != "" {
				if st.toolStartEmitted[chatIdx] {
					continue // already emitted — dedup
				}
				st.toolStartEmitted[chatIdx] = true

				globalIdx := st.nextGlobalIndex
				st.nextGlobalIndex++
				st.toolIndexToGlobalIndex[chatIdx] = globalIdx
				st.toolGlobalIndices = append(st.toolGlobalIndices, globalIdx)

				events = append(events, &IRStreamEvent{
					Type:  IRStreamContentStart,
					Index: globalIdx,
					Part: &IRContentPart{
						Type: IRPartToolCall,
						ToolCall: &IRToolCallPart{
							ID:   id,
							Name: getString(fn, "name"),
						},
					},
				})
			}

			// content_part_delta: function.arguments
			if args != "" {
				globalIdx, ok := st.toolIndexToGlobalIndex[chatIdx]
				if !ok {
					continue // no start yet — skip orphan delta
				}
				events = append(events, &IRStreamEvent{
					Type:      IRStreamContentDelta,
					Index:     globalIdx,
					DeltaJSON: args,
				})
			}
		}
	}

	// message_delta: finish_reason present
	// StreamAggregator handles ContentStop cleanup — adapter emits MessageDelta only.
	if finishReason != "" {
		events = append(events, &IRStreamEvent{
			Type:       IRStreamMessageDelta,
			StopReason: mapOpenAIFinishReason(finishReason),
		})
	}

	// Attach usage if present
	if len(events) > 0 {
		var chunkUsage *IRUsage
		if u, ok := raw["usage"].(map[string]interface{}); ok {
			chunkUsage = &IRUsage{
				InputTokens:  getIntDefault(u, "prompt_tokens"),
				OutputTokens: getIntDefault(u, "completion_tokens"),
				TotalTokens:  getIntDefault(u, "total_tokens"),
			}
			if promptDetails, ok := u["prompt_tokens_details"].(map[string]interface{}); ok {
				chunkUsage.CacheReadInputTokens = getIntDefault(promptDetails, "cached_tokens")
			}
			if completionDetails, ok := u["completion_tokens_details"].(map[string]interface{}); ok {
				chunkUsage.ReasoningTokens = getIntDefault(completionDetails, "reasoning_tokens")
			}
		}
		if chunkUsage != nil {
			// Attach usage to last event (typically the MessageDelta or latest delta)
			last := events[len(events)-1]
			if last.Usage == nil {
				last.Usage = chunkUsage
			}
		}
	}

	return events, nil
}

// =============================================================================
// EncodeStreamEvent — IRStreamEvent → OpenAI Chat SSE chunks
// =============================================================================

func (a *openAIChatAdapter) EncodeStreamEvent(ir *IRStreamEvent, state interface{}) ([]map[string]interface{}, error) {
	st, _ := state.(*chatStreamState)

	// Track IDs
	if ir.ResponseID != "" && st.responseID == "" {
		st.responseID = ir.ResponseID
	}
	if ir.ResponseModel != "" && st.model == "" {
		st.model = ir.ResponseModel
	}

	chunk := map[string]interface{}{
		"id":      "chatcmpl-stream",
		"object":  "chat.completion.chunk",
		"created": now(),
		"model":   st.model,
	}

	switch ir.Type {
	case IRStreamMessageStart:
		chunk["choices"] = []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         map[string]interface{}{"role": "assistant", "content": ""},
				"finish_reason": nil,
			},
		}

	case IRStreamContentDelta:
		switch {
		case ir.DeltaText != "":
			chunk["choices"] = []interface{}{
				map[string]interface{}{
					"index":         ir.Index,
					"delta":         map[string]interface{}{"content": ir.DeltaText},
					"finish_reason": nil,
				},
			}
		case ir.DeltaJSON != "":
			chunk["choices"] = []interface{}{
				map[string]interface{}{
					"index": ir.Index,
					"delta": map[string]interface{}{
						"tool_calls": []interface{}{
							map[string]interface{}{
								"index":    ir.Index,
								"function": map[string]interface{}{"arguments": ir.DeltaJSON},
							},
						},
					},
					"finish_reason": nil,
				},
			}
		}

	case IRStreamContentStart:
		if ir.Part != nil && ir.Part.Type == IRPartToolCall && ir.Part.ToolCall != nil {
			chunk["choices"] = []interface{}{
				map[string]interface{}{
					"index": ir.Index,
					"delta": map[string]interface{}{
						"tool_calls": []interface{}{
							map[string]interface{}{
								"index": ir.Index,
								"id":    ir.Part.ToolCall.ID,
								"type":  "function",
								"function": map[string]interface{}{
									"name":      ir.Part.ToolCall.Name,
									"arguments": serializeChatToolCallArgs(ir.Part.ToolCall),
								},
							},
						},
					},
					"finish_reason": nil,
				},
			}
		}

	case IRStreamContentStop:
		// OpenAI Chat doesn't emit per-part stop events; handled at message level

	case IRStreamMessageDelta:
		finishReason := mapIRStopReasonToOpenAIFinish(ir.StopReason)
		if ir.Usage != nil {
			// Usage-only chunk
			chunk["choices"] = []interface{}{}
			chunk["usage"] = encodeOpenAIUsage(ir.Usage)
		} else {
			chunk["choices"] = []interface{}{
				map[string]interface{}{
					"index":         0,
					"delta":         map[string]interface{}{},
					"finish_reason": finishReason,
				},
			}
		}

	case IRStreamDone:
		// terminal [DONE] is handled by SSE framing, not as JSON chunk
		return nil, nil

	case IRStreamError:
		chunk["choices"] = []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": "error",
			},
		}
		if ir.ErrorMessage != "" {
			chunk["error"] = map[string]interface{}{
				"message": ir.ErrorMessage,
				"type":    ir.ErrorType,
			}
		}

	default:
		return nil, nil
	}

	if chunk["choices"] == nil {
		return nil, nil
	}
	return []map[string]interface{}{chunk}, nil
}

// =============================================================================

// =============================================================================
// Helpers — Message Decoding
// =============================================================================

func decodeOpenAIChatMessages(raw []interface{}) []IRMessage {
	var msgs []IRMessage
	for _, r := range raw {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}

		role := getString(m, "role")
		msg := IRMessage{Role: mapOpenAIRole(role)}

		// Assistant tool_calls → IRPartToolCall
		if tcs, ok := getList(m, "tool_calls"); ok && role == "assistant" {
			for _, tc := range tcs {
				tcm, _ := tc.(map[string]interface{})
				fn, _ := tcm["function"].(map[string]interface{})
				input := make(map[string]interface{})
				if args := getString(fn, "arguments"); args != "" {
					parseJSONString(args, &input)
				}
				msg.Parts = append(msg.Parts, IRContentPart{
					Type: IRPartToolCall,
					ToolCall: &IRToolCallPart{
						ID:            getString(tcm, "id"),
						Name:          getString(fn, "name"),
						ArgumentsJSON: input,
						Status:        "completed",
					},
				})
			}
		}

		// Save reasoning_content from DeepSeek thinking mode (mandatory round-trip)
		if reasoningContent := getString(m, "reasoning_content"); reasoningContent != "" && role == "assistant" {
			msg.Parts = append(msg.Parts, IRContentPart{
				Type: IRPartReasoning,
				Reasoning: &IRReasoningPart{
					Content: reasoningContent,
				},
			})
		}

		// Tool result — content goes into IRPartToolResult.Content.
		// Use continue to skip the generic content decode below.
		if role == "tool" {
			toolResult := &IRToolResultPart{
				ToolCallID: getString(m, "tool_call_id"),
				Status:     "completed",
			}
			if content := m["content"]; content != nil {
				if s, ok := content.(string); ok && s != "" {
					toolResult.Content = []IRContentPart{{Type: IRPartText, Text: s}}
				} else if parts := decodeOpenAIChatContent(content); len(parts) > 0 {
					toolResult.Content = parts
				}
			}
			msg.Parts = append(msg.Parts, IRContentPart{
				Type:       IRPartToolResult,
				ToolResult: toolResult,
			})
			msgs = append(msgs, msg)
			continue
		}

		// Content (string or array) → IRPartText / IRPartRefusal
		if content := m["content"]; content != nil {
			parts := decodeOpenAIChatContent(content)
			msg.Parts = append(msg.Parts, parts...)
		}

		msgs = append(msgs, msg)
	}
	return msgs
}

func decodeOpenAIChatContent(content interface{}) []IRContentPart {
	switch v := content.(type) {
	case string:
		if v != "" {
			return []IRContentPart{{Type: IRPartText, Text: v}}
		}
	case []interface{}:
		var parts []IRContentPart
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				t := getString(m, "type")
				switch t {
				case "text":
					parts = append(parts, IRContentPart{Type: IRPartText, Text: getString(m, "text")})
				case "refusal":
					parts = append(parts, IRContentPart{
						Type:    IRPartRefusal,
						Refusal: &IRRefusalPart{Text: getString(m, "refusal")},
					})
				case "image_url":
					parts = append(parts, IRContentPart{
						Type: IRPartImage,
						Metadata: map[string]string{
							"url": getStringFromMap(m, "image_url", "url"),
						},
					})
				}
			}
		}
		return parts
	}
	return nil
}

// =============================================================================
// Helpers — Message Encoding
// =============================================================================

func encodeOpenAIChatMessages(ir *IRRequest) []interface{} {
	var msgs []interface{}

	// System message from IRRequest.System
	if ir.System != "" {
		msgs = append(msgs, map[string]interface{}{
			"role":    "system",
			"content": ir.System,
		})
	}

	for _, m := range ir.Messages {
		if m.Role == IRRoleSystem {
			continue // already emitted as system message above
		}

		baseRole := "user"
		switch m.Role {
		case IRRoleAssistant:
			baseRole = "assistant"
		case IRRoleTool:
			baseRole = "tool"
		}

		em := map[string]interface{}{"role": baseRole}
		hasContent := false
		var toolCalls []interface{}
		var reasoningContent string

		flushMessage := func() {
			if len(toolCalls) > 0 {
				em["tool_calls"] = toolCalls
				if _, ok := em["content"]; !ok {
					em["content"] = nil
				}
				// DeepSeek requires reasoning_content on every assistant tool-call message.
				if reasoningContent == "" {
					reasoningContent = " "
				}
			}
			if reasoningContent != "" {
				em["reasoning_content"] = reasoningContent
			}
			if _, ok := em["content"]; !ok && len(toolCalls) == 0 && baseRole != "tool" {
				em["content"] = ""
			}
			if hasContent || len(toolCalls) > 0 || reasoningContent != "" || baseRole != "tool" {
				msgs = append(msgs, em)
			}
			em = map[string]interface{}{"role": baseRole}
			toolCalls = nil
			reasoningContent = ""
			hasContent = false
		}

		for _, part := range m.Parts {
			switch part.Type {
			case IRPartText:
				if existing, ok := em["content"].(string); ok {
					em["content"] = existing + part.Text
				} else {
					em["content"] = part.Text
				}
				hasContent = true
			case IRPartToolCall:
				if part.ToolCall != nil {
					args := "{}"
					if part.ToolCall.ArgumentsJSON != nil {
						if b, err := marshalJSON(part.ToolCall.ArgumentsJSON); err == nil {
							args = string(b)
						}
					}
					toolCalls = append(toolCalls, map[string]interface{}{
						"id":   part.ToolCall.ID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      part.ToolCall.Name,
							"arguments": args,
						},
					})
				}
			case IRPartReasoning:
				if part.Reasoning != nil {
					reasoningContent += part.Reasoning.Content
				}
			case IRPartToolResult:
				if hasContent || len(toolCalls) > 0 {
					flushMessage()
				}
				toolMsg := map[string]interface{}{
					"role":         "tool",
					"tool_call_id": part.ToolResult.ToolCallID,
				}
				if part.ToolResult.Content != nil {
					var content string
					for _, cp := range part.ToolResult.Content {
						if cp.Type == IRPartText {
							content += cp.Text
						}
					}
					toolMsg["content"] = content
				} else if part.ToolResult.Error != "" {
					toolMsg["content"] = part.ToolResult.Error
				}
				msgs = append(msgs, toolMsg)
			}
		}

		if hasContent || len(toolCalls) > 0 || reasoningContent != "" || (len(m.Parts) == 0 && baseRole != "tool") {
			flushMessage()
		}
	}

	return msgs
}

// =============================================================================
// Helpers — Tools
// =============================================================================

func decodeOpenAIChatTools(raw []interface{}) []IRToolDecl {
	var tools []IRToolDecl
	for _, r := range raw {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if getString(m, "type") != "function" {
			continue
		}
		fn, ok := m["function"].(map[string]interface{})
		if !ok {
			continue
		}
		params, _ := fn["parameters"].(map[string]interface{})
		tools = append(tools, IRToolDecl{
			Type:        "function",
			Name:        getString(fn, "name"),
			Description: getString(fn, "description"),
			Parameters:  params,
		})
	}
	return tools
}

func encodeOpenAIChatTools(tools []IRToolDecl) []interface{} {
	var result []interface{}
	for _, t := range tools {
		result = append(result, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			},
		})
	}
	return result
}

func decodeOpenAIChatToolChoice(tc interface{}) *IRToolChoice {
	switch v := tc.(type) {
	case string:
		return &IRToolChoice{Type: v}
	case map[string]interface{}:
		if t := getString(v, "type"); t == "function" {
			if fn, ok := v["function"].(map[string]interface{}); ok {
				return &IRToolChoice{Type: "specific", Name: getString(fn, "name")}
			}
		}
		return &IRToolChoice{Type: getString(v, "type")}
	}
	return nil
}

func encodeOpenAIChatToolChoice(tc *IRToolChoice) interface{} {
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
			"function": map[string]interface{}{
				"name": tc.Name,
			},
		}
	}
	return "auto"
}

// =============================================================================
// Helpers — Role & Finish Reason Mapping
// =============================================================================

func mapOpenAIRole(role string) IRRole {
	switch role {
	case "system", "developer":
		return IRRoleSystem
	case "user":
		return IRRoleUser
	case "assistant":
		return IRRoleAssistant
	case "tool":
		return IRRoleTool
	}
	return IRRoleUser
}

func mapOpenAIFinishReason(reason string) IRStopReason {
	switch reason {
	case "stop":
		return IRStopEndTurn
	case "length":
		return IRStopMaxTokens
	case "tool_calls":
		return IRStopToolUse
	case "content_filter":
		// Treat content_filter as end_turn — the response completed but was filtered.
		// This is not an error; the caller can decide to retry or adjust.
		return IRStopEndTurn
	case "error":
		return IRStopError
	}
	return IRStopEndTurn
}

func mapIRStopReasonToOpenAIFinish(reason IRStopReason) string {
	switch reason {
	case IRStopToolUse:
		return "tool_calls"
	case IRStopMaxTokens:
		return "length"
	case IRStopEndTurn:
		return "stop"
	case IRStopStopSequence:
		return "stop"
	case IRStopContentFilter:
		return "content_filter"
	case IRStopError:
		return "error"
	case IRStopLength:
		return "length"
	}
	return "stop"
}

// =============================================================================
// Helpers — Usage Encoding
// =============================================================================

func encodeOpenAIUsage(u *IRUsage) map[string]interface{} {
	usage := map[string]interface{}{
		"prompt_tokens":     u.InputTokens,
		"completion_tokens": u.OutputTokens,
		"total_tokens":      u.InputTokens + u.OutputTokens,
	}
	if u.CacheReadInputTokens > 0 {
		usage["prompt_tokens_details"] = map[string]interface{}{
			"cached_tokens": u.CacheReadInputTokens,
		}
	}
	if u.ReasoningTokens > 0 {
		if details, ok := usage["completion_tokens_details"].(map[string]interface{}); ok {
			details["reasoning_tokens"] = u.ReasoningTokens
		} else {
			usage["completion_tokens_details"] = map[string]interface{}{
				"reasoning_tokens": u.ReasoningTokens,
			}
		}
	}
	return usage
}

// =============================================================================
// Helpers — Misc
// =============================================================================

func ensureChatPrefix(id string) string {
	const prefix = "chatcmpl"
	if len(id) >= len(prefix) && id[:len(prefix)] == prefix {
		return id
	}
	return prefix + "-" + id
}

// serializeChatToolCallArgs serializes tool call arguments for OpenAI Chat format.
// Uses ArgumentsJSON if populated, falls back to ArgumentsRaw, defaults to "{}".
func serializeChatToolCallArgs(tc *IRToolCallPart) string {
	if tc == nil {
		return "{}"
	}
	if tc.ArgumentsJSON != nil {
		if b, err := marshalJSON(tc.ArgumentsJSON); err == nil {
			return string(b)
		}
	}
	if tc.ArgumentsRaw != "" {
		return tc.ArgumentsRaw
	}
	return "{}"
}
