package modelrouter

func (d *openAIChatDecoder) DecodeRequest(body map[string]interface{}) (*IRRequest, error) {
	ir := &IRRequest{
		Model:     getString(body, "model"),
		Stream:    getBool(body, "stream"),
		Preserved: make(map[string]interface{}),
	}

	if msgs, ok := getList(body, "messages"); ok {
		ir.Messages = decodeOpenAIChatMessages(msgs)
	}

	for _, msg := range ir.Messages {
		if msg.Role == IRRoleSystem {
			for _, block := range msg.Content {
				if block.Type == IRBlockText {
					ir.System += block.Text
				}
			}
		}
	}

	if t, ok := getFloat(body, "temperature"); ok {
		ir.Temperature = &t
	}
	if p, ok := getFloat(body, "top_p"); ok {
		ir.TopP = &p
	}
	if mt, ok := getInt(body, "max_tokens"); ok {
		ir.MaxTokens = mt
	}
	if mt, ok := getInt(body, "max_completion_tokens"); ok && mt > ir.MaxTokens {
		ir.MaxTokens = mt
	}
	if stopList, ok := getStringList(body, "stop"); ok {
		ir.Stop = stopList
	} else if s := getString(body, "stop"); s != "" {
		ir.Stop = []string{s}
	}

	if tools, ok := getList(body, "tools"); ok {
		ir.Tools = decodeOpenAIChatTools(tools)
	}

	if tc, ok := body["tool_choice"]; ok {
		ir.ToolChoice = decodeOpenAIChatToolChoice(tc)
	}

	if s, ok := getInt(body, "seed"); ok {
		ir.Seed = &s
	}
	ir.User = getString(body, "user")

	return ir, nil
}

func decodeOpenAIChatMessages(raw []interface{}) []IRMessage {
	var msgs []IRMessage
	for _, r := range raw {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}

		role := getString(m, "role")
		msg := IRMessage{Role: mapOpenAIRole(role)}

		if tcs, ok := getList(m, "tool_calls"); ok && role == "assistant" {
			for _, tc := range tcs {
				tcm, _ := tc.(map[string]interface{})
				fn, _ := tcm["function"].(map[string]interface{})
				input := make(map[string]interface{})
				if args := getString(fn, "arguments"); args != "" {
					parseJSONString(args, &input)
				}
				msg.Content = append(msg.Content, IRContentBlock{
					Type:         IRBlockToolUse,
					ToolUseID:    getString(tcm, "id"),
					ToolUseName:  getString(fn, "name"),
					ToolUseInput: input,
				})
			}
		}

		if role == "tool" {
			msg.Content = append(msg.Content, IRContentBlock{
				Type:                IRBlockToolResult,
				ToolResultToolUseID: getString(m, "tool_call_id"),
				ToolResultContent:   contentToString(m["content"]),
			})
		}

		if content := m["content"]; content != nil {
			blocks := decodeOpenAIChatContent(content)
			msg.Content = append(msg.Content, blocks...)
		}

		msgs = append(msgs, msg)
	}
	return msgs
}

func decodeOpenAIChatContent(content interface{}) []IRContentBlock {
	switch v := content.(type) {
	case string:
		if v != "" {
			return []IRContentBlock{{Type: IRBlockText, Text: v}}
		}
	case []interface{}:
		var blocks []IRContentBlock
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if t := getString(m, "type"); t == "text" {
					blocks = append(blocks, IRContentBlock{Type: IRBlockText, Text: getString(m, "text")})
				}
			}
		}
		return blocks
	}
	return nil
}

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
			Name:        getString(fn, "name"),
			Description: getString(fn, "description"),
			Parameters:  params,
		})
	}
	return tools
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

func (d *openAIChatDecoder) DecodeResponse(body map[string]interface{}) (*IRResponse, error) {
	ir := &IRResponse{
		ID:      getString(body, "id"),
		Model:   getString(body, "model"),
		Created: getInt64(body, "created"),
	}

	if choices, ok := getList(body, "choices"); ok && len(choices) > 0 {
		choice, _ := choices[0].(map[string]interface{})
		msg, _ := choice["message"].(map[string]interface{})

		if content := msg["content"]; content != nil {
			if s, ok := content.(string); ok && s != "" {
				ir.Content = append(ir.Content, IRContentBlock{Type: IRBlockText, Text: s})
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
				ir.Content = append(ir.Content, IRContentBlock{
					Type:         IRBlockToolUse,
					ToolUseID:    getString(tcm, "id"),
					ToolUseName:  getString(fn, "name"),
					ToolUseInput: input,
				})
			}
		}

		ir.StopReason = mapOpenAIFinishReason(getString(choice, "finish_reason"))
	}

	if u, ok := body["usage"].(map[string]interface{}); ok {
		ir.Usage = &IRUsage{
			InputTokens:  getIntDefault(u, "prompt_tokens"),
			OutputTokens: getIntDefault(u, "completion_tokens"),
		}
		ir.Usage.TotalTokens = ir.Usage.InputTokens + ir.Usage.OutputTokens
	}

	return ir, nil
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
		return IRStopError
	}
	return IRStopEndTurn
}

type openAIChatEncoder struct{}

func (e *openAIChatEncoder) EncodeRequest(ir *IRRequest) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"model":    ir.Model,
		"messages": e.encodeMessages(ir),
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

	if len(ir.Tools) > 0 {
		var tools []map[string]interface{}
		for _, t := range ir.Tools {
			tools = append(tools, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Parameters,
				},
			})
		}
		body["tools"] = tools
	}

	if ir.ToolChoice != nil {
		body["tool_choice"] = encodeOpenAIChatToolChoice(ir.ToolChoice)
	}

	return body, nil
}

func (e *openAIChatEncoder) encodeMessages(ir *IRRequest) []map[string]interface{} {
	var msgs []map[string]interface{}

	if ir.System != "" {
		msgs = append(msgs, map[string]interface{}{
			"role":    "system",
			"content": ir.System,
		})
	}

	for _, m := range ir.Messages {
		if m.Role == IRRoleSystem {
			continue
		}

		em := map[string]interface{}{}
		switch m.Role {
		case IRRoleUser:
			em["role"] = "user"
		case IRRoleAssistant:
			em["role"] = "assistant"
		case IRRoleTool:
			em["role"] = "tool"
		}

		var toolCalls []map[string]interface{}
		for _, block := range m.Content {
			switch block.Type {
			case IRBlockText:
				if existing, ok := em["content"].(string); ok {
					em["content"] = existing + block.Text
				} else {
					em["content"] = block.Text
				}
			case IRBlockToolUse:
				args := "{}"
				if block.ToolUseInput != nil {
					if b, err := marshalJSON(block.ToolUseInput); err == nil {
						args = string(b)
					}
				}
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   block.ToolUseID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      block.ToolUseName,
						"arguments": args,
					},
				})
			case IRBlockToolResult:
				em["role"] = "tool"
				em["tool_call_id"] = block.ToolResultToolUseID
				em["content"] = block.ToolResultContent
			}
		}

		if len(toolCalls) > 0 {
			em["tool_calls"] = toolCalls
			if _, ok := em["content"]; !ok {
				em["content"] = nil
			}
		}

		if _, ok := em["content"]; !ok && len(toolCalls) == 0 && m.Role != IRRoleTool {
			em["content"] = ""
		}

		msgs = append(msgs, em)
	}

	return msgs
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

func (e *openAIChatEncoder) EncodeResponse(ir *IRResponse) (map[string]interface{}, error) {
	msg := map[string]interface{}{"role": "assistant"}
	var text string
	var toolCalls []map[string]interface{}

	for _, block := range ir.Content {
		switch block.Type {
		case IRBlockText:
			text += block.Text
		case IRBlockToolUse:
			args := "{}"
			if block.ToolUseInput != nil {
				if b, err := marshalJSON(block.ToolUseInput); err == nil {
					args = string(b)
				}
			}
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   block.ToolUseID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      block.ToolUseName,
					"arguments": args,
				},
			})
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

	finishReason := "stop"
	switch ir.StopReason {
	case IRStopToolUse:
		finishReason = "tool_calls"
	case IRStopMaxTokens:
		finishReason = "length"
	case IRStopEndTurn:
		finishReason = "stop"
	case IRStopError:
		finishReason = "content_filter"
	}

	resp := map[string]interface{}{
		"id":      ensurePrefix(ir.ID, "chatcmpl"),
		"object":  "chat.completion",
		"created": maybeNow(ir.Created),
		"model":   ir.Model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       msg,
				"finish_reason": finishReason,
			},
		},
	}

	if ir.Usage != nil {
		resp["usage"] = map[string]interface{}{
			"prompt_tokens":     ir.Usage.InputTokens,
			"completion_tokens": ir.Usage.OutputTokens,
			"total_tokens":      ir.Usage.InputTokens + ir.Usage.OutputTokens,
		}
	}

	return resp, nil
}

func ensurePrefix(id, prefix string) string {
	if len(id) >= len(prefix) && id[:len(prefix)] == prefix {
		return id
	}
	return prefix + "-" + id
}

func encodeOpenAIChatStreamEvent(event *IRStreamEvent) []map[string]interface{} {
	chunk := map[string]interface{}{
		"id":      "chatcmpl-stream",
		"object":  "chat.completion.chunk",
		"created": now(),
		"model":   "",
	}

	switch event.Type {
	case IRStreamMessageStart:
		chunk["choices"] = []map[string]interface{}{
			{"index": 0, "delta": map[string]interface{}{"role": "assistant", "content": ""}, "finish_reason": nil},
		}
	case IRStreamContentDelta:
		if event.DeltaType == "text" {
			chunk["choices"] = []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{"content": event.DeltaText}, "finish_reason": nil},
			}
		} else if event.DeltaType == "input_json" {
			chunk["choices"] = []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{
					"tool_calls": []map[string]interface{}{
						{"index": event.Index, "function": map[string]interface{}{"arguments": event.DeltaJSON}},
					},
				}, "finish_reason": nil},
			}
		}
	case IRStreamContentStart:
		if event.ContentBlock != nil && event.ContentBlock.Type == IRBlockToolUse {
			chunk["choices"] = []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{
					"tool_calls": []map[string]interface{}{
						{
							"index": event.Index, "id": event.ContentBlock.ToolUseID, "type": "function",
							"function": map[string]interface{}{"name": event.ContentBlock.ToolUseName, "arguments": ""},
						},
					},
				}, "finish_reason": nil},
			}
		}
	case IRStreamMessageDelta:
		finishReason := "stop"
		switch event.StopReason {
		case IRStopToolUse:
			finishReason = "tool_calls"
		case IRStopMaxTokens:
			finishReason = "length"
		}
		chunk["choices"] = []map[string]interface{}{
			{"index": 0, "delta": map[string]interface{}{}, "finish_reason": finishReason},
		}
		if event.Usage != nil {
			chunk["choices"] = []map[string]interface{}{}
			chunk["usage"] = map[string]interface{}{
				"prompt_tokens":     event.Usage.InputTokens,
				"completion_tokens": event.Usage.OutputTokens,
				"total_tokens":      event.Usage.InputTokens + event.Usage.OutputTokens,
			}
		}
	}

	if chunk["choices"] == nil {
		return nil
	}
	return []map[string]interface{}{chunk}
}

func decodeOpenAIChatStreamEvent(data map[string]interface{}) []*IRStreamEvent {
	choices, ok := getList(data, "choices")
	if !ok || len(choices) == 0 {
		if usage, ok := data["usage"].(map[string]interface{}); ok {
			return []*IRStreamEvent{{
				Type: IRStreamMessageDelta,
				Usage: &IRUsage{
					InputTokens:  getIntDefault(usage, "prompt_tokens"),
					OutputTokens: getIntDefault(usage, "completion_tokens"),
				},
			}}
		}
		return nil
	}

	choice, _ := choices[0].(map[string]interface{})
	delta, _ := choice["delta"].(map[string]interface{})
	finishReason := getString(choice, "finish_reason")

	var events []*IRStreamEvent

	if role := getString(delta, "role"); role == "assistant" {
		events = append(events, &IRStreamEvent{
			Type:          IRStreamMessageStart,
			ResponseID:    getString(data, "id"),
			ResponseModel: getString(data, "model"),
		})
	}

	if content := getString(delta, "content"); content != "" {
		events = append(events, &IRStreamEvent{
			Type:      IRStreamContentDelta,
			DeltaType: "text",
			DeltaText: content,
		})
	}

	if tcs, ok := getList(delta, "tool_calls"); ok {
		for _, tc := range tcs {
			tcm, _ := tc.(map[string]interface{})
			fn, _ := tcm["function"].(map[string]interface{})
			idx := getIntDefault(tcm, "index")

			if id := getString(tcm, "id"); id != "" {
				events = append(events, &IRStreamEvent{
					Type:  IRStreamContentStart,
					Index: idx,
					ContentBlock: &IRContentBlock{
						Type:        IRBlockToolUse,
						ToolUseID:   id,
						ToolUseName: getString(fn, "name"),
					},
				})
			}

			if args := getString(fn, "arguments"); args != "" {
				events = append(events, &IRStreamEvent{
					Type:      IRStreamContentDelta,
					Index:     idx,
					DeltaType: "input_json",
					DeltaJSON: args,
				})
			}
		}
	}

	if finishReason != "" {
		events = append(events, &IRStreamEvent{
			Type:       IRStreamMessageDelta,
			StopReason: mapOpenAIFinishReason(finishReason),
		})
	}

	return events
}

type openAIChatDecoder struct{}
