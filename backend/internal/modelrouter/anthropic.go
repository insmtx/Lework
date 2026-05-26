package modelrouter

import "fmt"

type anthropicDecoder struct{}

func (d *anthropicDecoder) DecodeRequest(body map[string]interface{}) (*IRRequest, error) {
	ir := &IRRequest{
		Model:     getString(body, "model"),
		Stream:    getBool(body, "stream"),
		MaxTokens: getIntDefault(body, "max_tokens"),
		Preserved: make(map[string]interface{}),
	}

	if system, ok := body["system"]; ok {
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

	if msgs, ok := getList(body, "messages"); ok {
		ir.Messages = decodeAnthropicMessages(msgs)
	}

	if t, ok := getFloat(body, "temperature"); ok {
		ir.Temperature = &t
	}
	if p, ok := getFloat(body, "top_p"); ok {
		ir.TopP = &p
	}
	if ss, ok := getStringList(body, "stop_sequences"); ok {
		ir.Stop = ss
	}

	if tools, ok := getList(body, "tools"); ok {
		ir.Tools = decodeAnthropicTools(tools)
	}

	if tc, ok := body["tool_choice"]; ok {
		if tcm, ok := tc.(map[string]interface{}); ok {
			t := getString(tcm, "type")
			n := getString(tcm, "name")
			switch t {
			case "any":
				ir.ToolChoice = &IRToolChoice{Type: "required"}
			case "tool":
				ir.ToolChoice = &IRToolChoice{Type: "specific", Name: n}
			default:
				ir.ToolChoice = &IRToolChoice{Type: t}
			}
		}
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
			msg.Content = decodeAnthropicContent(content)
		}

		msgs = append(msgs, msg)
	}
	return msgs
}

func decodeAnthropicContent(content interface{}) []IRContentBlock {
	switch v := content.(type) {
	case string:
		return []IRContentBlock{{Type: IRBlockText, Text: v}}
	case []interface{}:
		var blocks []IRContentBlock
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				switch getString(m, "type") {
				case "text":
					blocks = append(blocks, IRContentBlock{Type: IRBlockText, Text: getString(m, "text")})
				case "tool_use":
					input, _ := m["input"].(map[string]interface{})
					blocks = append(blocks, IRContentBlock{
						Type:         IRBlockToolUse,
						ToolUseID:    getString(m, "id"),
						ToolUseName:  getString(m, "name"),
						ToolUseInput: input,
					})
				case "tool_result":
					resultContent := ""
					if c := m["content"]; c != nil {
						resultContent = contentToString(c)
					}
					blocks = append(blocks, IRContentBlock{
						Type:                IRBlockToolResult,
						ToolResultToolUseID: getString(m, "tool_use_id"),
						ToolResultContent:   resultContent,
					})
				}
			}
		}
		return blocks
	}
	return nil
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
			Name:        getString(m, "name"),
			Description: getString(m, "description"),
			Parameters:  params,
		})
	}
	return tools
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

func (d *anthropicDecoder) DecodeResponse(body map[string]interface{}) (*IRResponse, error) {
	ir := &IRResponse{
		ID:    getString(body, "id"),
		Model: getString(body, "model"),
	}

	if content, ok := getList(body, "content"); ok {
		ir.Content = decodeAnthropicContent(content)
	}

	ir.StopReason = mapAnthropicStopReason(getString(body, "stop_reason"))

	if u, ok := body["usage"].(map[string]interface{}); ok {
		ir.Usage = &IRUsage{
			InputTokens:  getIntDefault(u, "input_tokens"),
			OutputTokens: getIntDefault(u, "output_tokens"),
		}
		ir.Usage.TotalTokens = ir.Usage.InputTokens + ir.Usage.OutputTokens
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

type anthropicEncoder struct{}

func (e *anthropicEncoder) EncodeRequest(ir *IRRequest) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"model":      ir.Model,
		"max_tokens": 4096,
		"messages":   e.encodeMessages(ir.Messages),
	}

	if ir.System != "" {
		body["system"] = ir.System
	}

	if ir.MaxTokens > 0 {
		body["max_tokens"] = ir.MaxTokens
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

func (e *anthropicEncoder) encodeMessages(msgs []IRMessage) []map[string]interface{} {
	var result []map[string]interface{}

	for _, m := range msgs {
		if m.Role == IRRoleSystem || m.Role == IRRoleTool {
			em := map[string]interface{}{"role": "user"}
			var content []map[string]interface{}
			for _, block := range m.Content {
				if block.Type == IRBlockToolResult {
					content = append(content, map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": block.ToolResultToolUseID,
						"content":     block.ToolResultContent,
					})
				} else if block.Type == IRBlockText {
					content = append(content, map[string]interface{}{
						"type": "text",
						"text": block.Text,
					})
				}
			}
			if len(content) > 0 {
				em["content"] = content
			}
			result = append(result, em)
			continue
		}

		role := "user"
		if m.Role == IRRoleAssistant {
			role = "assistant"
		}

		em := map[string]interface{}{"role": role}

		var content []map[string]interface{}
		for _, block := range m.Content {
			switch block.Type {
			case IRBlockText:
				content = append(content, map[string]interface{}{"type": "text", "text": block.Text})
			case IRBlockToolUse:
				content = append(content, map[string]interface{}{
					"type":  "tool_use",
					"id":    block.ToolUseID,
					"name":  block.ToolUseName,
					"input": block.ToolUseInput,
				})
			}
		}

		if len(content) == 1 && content[0]["type"] == "text" {
			em["content"] = content[0]["text"]
		} else if len(content) > 0 {
			em["content"] = content
		} else {
			em["content"] = ""
		}

		result = append(result, em)
	}

	return result
}

func encodeAnthropicToolChoice(tc *IRToolChoice) map[string]interface{} {
	switch tc.Type {
	case "auto":
		return map[string]interface{}{"type": "auto"}
	case "none":
		return map[string]interface{}{"type": "none"}
	case "required":
		return map[string]interface{}{"type": "any"}
	case "specific":
		return map[string]interface{}{"type": "tool", "name": tc.Name}
	}
	return map[string]interface{}{"type": "auto"}
}

func (e *anthropicEncoder) EncodeResponse(ir *IRResponse) (map[string]interface{}, error) {
	var content []map[string]interface{}

	for _, block := range ir.Content {
		switch block.Type {
		case IRBlockText:
			content = append(content, map[string]interface{}{"type": "text", "text": block.Text})
		case IRBlockToolUse:
			content = append(content, map[string]interface{}{
				"type":  "tool_use",
				"id":    block.ToolUseID,
				"name":  block.ToolUseName,
				"input": block.ToolUseInput,
			})
		}
	}

	stopReason := "end_turn"
	switch ir.StopReason {
	case IRStopMaxTokens:
		stopReason = "max_tokens"
	case IRStopToolUse:
		stopReason = "tool_use"
	case IRStopStopSequence:
		stopReason = "stop_sequence"
	}

	resp := map[string]interface{}{
		"id":          ensurePrefix(ir.ID, "msg"),
		"type":        "message",
		"role":        "assistant",
		"model":       ir.Model,
		"content":     content,
		"stop_reason": stopReason,
	}

	if ir.Usage != nil {
		resp["usage"] = map[string]interface{}{
			"input_tokens":  ir.Usage.InputTokens,
			"output_tokens": ir.Usage.OutputTokens,
		}
	}

	return resp, nil
}

func decodeAnthropicStreamEvent(eventType string, data map[string]interface{}) []*IRStreamEvent {
	switch eventType {
	case "message_start":
		msg, _ := data["message"].(map[string]interface{})
		return []*IRStreamEvent{{
			Type:          IRStreamMessageStart,
			ResponseID:    getString(msg, "id"),
			ResponseModel: getString(msg, "model"),
		}}

	case "content_block_start":
		block, _ := data["content_block"].(map[string]interface{})
		idx := getIntDefault(data, "index")
		var cb *IRContentBlock
		if getString(block, "type") == "tool_use" {
			cb = &IRContentBlock{
				Type:        IRBlockToolUse,
				ToolUseID:   getString(block, "id"),
				ToolUseName: getString(block, "name"),
			}
		} else {
			cb = &IRContentBlock{Type: IRBlockText}
		}
		return []*IRStreamEvent{{
			Type:         IRStreamContentStart,
			Index:        idx,
			ContentBlock: cb,
		}}

	case "content_block_delta":
		delta, _ := data["delta"].(map[string]interface{})
		idx := getIntDefault(data, "index")
		deltaType := getString(delta, "type")
		if deltaType == "text_delta" {
			return []*IRStreamEvent{{
				Type:      IRStreamContentDelta,
				Index:     idx,
				DeltaType: "text",
				DeltaText: getString(delta, "text"),
			}}
		} else if deltaType == "input_json_delta" {
			return []*IRStreamEvent{{
				Type:      IRStreamContentDelta,
				Index:     idx,
				DeltaType: "input_json",
				DeltaJSON: getString(delta, "partial_json"),
			}}
		}

	case "content_block_stop":
		return []*IRStreamEvent{{Type: IRStreamContentStop, Index: getIntDefault(data, "index")}}

	case "message_delta":
		delta, _ := data["delta"].(map[string]interface{})
		var usage *IRUsage
		if u, ok := data["usage"].(map[string]interface{}); ok {
			usage = &IRUsage{OutputTokens: getIntDefault(u, "output_tokens")}
		}
		return []*IRStreamEvent{{
			Type:       IRStreamMessageDelta,
			StopReason: mapAnthropicStopReason(getString(delta, "stop_reason")),
			Usage:      usage,
		}}

	case "message_stop":
		return []*IRStreamEvent{{Type: IRStreamDone}}

	case "error":
		err, _ := data["error"].(map[string]interface{})
		return []*IRStreamEvent{{
			Type:         IRStreamError,
			ErrorMessage: fmt.Sprintf("%s: %s", getString(err, "type"), getString(err, "message")),
		}}
	}

	return nil
}

func encodeAnthropicStreamEvent(event *IRStreamEvent) []map[string]interface{} {
	switch event.Type {
	case IRStreamMessageStart:
		return []map[string]interface{}{{
			"type": "message_start",
			"message": map[string]interface{}{
				"id":      event.ResponseID,
				"type":    "message",
				"role":    "assistant",
				"model":   event.ResponseModel,
				"content": []interface{}{},
			},
		}}

	case IRStreamContentStart:
		if event.ContentBlock != nil && event.ContentBlock.Type == IRBlockToolUse {
			return []map[string]interface{}{{
				"type":  "content_block_start",
				"index": event.Index,
				"content_block": map[string]interface{}{
					"type": "tool_use",
					"id":   event.ContentBlock.ToolUseID,
					"name": event.ContentBlock.ToolUseName,
				},
			}}
		}
		return []map[string]interface{}{{
			"type":  "content_block_start",
			"index": event.Index,
			"content_block": map[string]interface{}{
				"type": "text",
				"text": "",
			},
		}}

	case IRStreamContentDelta:
		if event.DeltaType == "text" {
			return []map[string]interface{}{{
				"type":  "content_block_delta",
				"index": event.Index,
				"delta": map[string]interface{}{
					"type": "text_delta",
					"text": event.DeltaText,
				},
			}}
		} else if event.DeltaType == "input_json" {
			return []map[string]interface{}{{
				"type":  "content_block_delta",
				"index": event.Index,
				"delta": map[string]interface{}{
					"type":         "input_json_delta",
					"partial_json": event.DeltaJSON,
				},
			}}
		}

	case IRStreamContentStop:
		return []map[string]interface{}{{
			"type":  "content_block_stop",
			"index": event.Index,
		}}

	case IRStreamMessageDelta:
		evt := map[string]interface{}{
			"type": "message_delta",
			"delta": map[string]interface{}{
				"stop_reason":   mapAnthropicStopReasonReverse(event.StopReason),
				"stop_sequence": nil,
			},
		}
		if event.Usage != nil {
			evt["usage"] = map[string]interface{}{
				"output_tokens": event.Usage.OutputTokens,
			}
		}
		return []map[string]interface{}{evt}

	case IRStreamDone:
		return []map[string]interface{}{{"type": "message_stop"}}

	case IRStreamError:
		return []map[string]interface{}{{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "error",
				"message": event.ErrorMessage,
			},
		}}
	}

	return nil
}

func mapAnthropicStopReasonReverse(reason IRStopReason) string {
	switch reason {
	case IRStopEndTurn:
		return "end_turn"
	case IRStopMaxTokens:
		return "max_tokens"
	case IRStopToolUse:
		return "tool_use"
	}
	return "end_turn"
}
