package modelrouter

import (
	"fmt"
	"strconv"
)

type openAIResponsesDecoder struct{}

func (d *openAIResponsesDecoder) DecodeRequest(body map[string]interface{}) (*IRRequest, error) {
	ir := &IRRequest{
		Model:     getString(body, "model"),
		Stream:    getBool(body, "stream"),
		Preserved: make(map[string]interface{}),
	}

	ir.Instructions = getString(body, "instructions")
	ir.System = ir.Instructions

	if input, ok := body["input"]; ok {
		ir.Messages = decodeResponsesInput(input)
	}

	if t, ok := getFloat(body, "temperature"); ok {
		ir.Temperature = &t
	}
	if p, ok := getFloat(body, "top_p"); ok {
		ir.TopP = &p
	}
	if mt, ok := getInt(body, "max_output_tokens"); ok {
		ir.MaxTokens = mt
	}
	if s, ok := getStringList(body, "stop"); ok {
		ir.Stop = s
	}
	if s, ok := getInt(body, "seed"); ok {
		ir.Seed = &s
	}

	ir.ReasoningEffort = getString(body, "reasoning_effort")

	if tools, ok := getList(body, "tools"); ok {
		ir.Tools = decodeResponsesTools(tools)
	}

	if tc, ok := body["tool_choice"]; ok {
		ir.ToolChoice = decodeResponsesToolChoice(tc)
	}

	ir.User = getString(body, "user")

	// 提取 Preserved：OpenAI Responses API 特有字段
	for k, v := range body {
		switch k {
		case "model", "input", "instructions", "stream", "temperature", "top_p",
			"max_output_tokens", "stop", "seed", "tools", "tool_choice",
			"user", "reasoning_effort", "metadata":
			// handled
		default:
			ir.Preserved[k] = v
		}
	}

	return ir, nil
}

func decodeResponsesInput(input interface{}) []IRMessage {
	switch v := input.(type) {
	case string:
		if v != "" {
			return []IRMessage{{
				Role:    IRRoleUser,
				Content: []IRContentBlock{{Type: IRBlockText, Text: v}},
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
				msg := IRMessage{Role: mapOpenAIRole(role)}
				if content := m["content"]; content != nil {
					msg.Content = decodeResponsesContent(content)
				}
				msgs = append(msgs, msg)
			case "function_call":
				input := make(map[string]interface{})
				if args := getString(m, "arguments"); args != "" {
					parseJSONString(args, &input)
				}
				msgs = append(msgs, IRMessage{
					Role: IRRoleAssistant,
					Content: []IRContentBlock{{
						Type:         IRBlockToolUse,
						ToolUseID:    getString(m, "call_id"),
						ToolUseName:  getString(m, "name"),
						ToolUseInput: input,
					}},
				})
			case "function_call_output":
				msgs = append(msgs, IRMessage{
					Role: IRRoleTool,
					Content: []IRContentBlock{{
						Type:                IRBlockToolResult,
						ToolResultToolUseID: getString(m, "call_id"),
						ToolResultContent:   getString(m, "output"),
					}},
				})
			}
		}
		return msgs
	}
	return nil
}

func decodeResponsesContent(content interface{}) []IRContentBlock {
	switch v := content.(type) {
	case string:
		return []IRContentBlock{{Type: IRBlockText, Text: v}}
	case []interface{}:
		var blocks []IRContentBlock
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				t := getString(m, "type")
				if t == "input_text" || t == "output_text" || t == "text" {
					blocks = append(blocks, IRContentBlock{Type: IRBlockText, Text: getString(m, "text")})
				}
			}
		}
		return blocks
	}
	return nil
}

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
			Name:        getString(m, "name"),
			Description: getString(m, "description"),
			Parameters:  params,
		})
	}
	return tools
}

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

func (d *openAIResponsesDecoder) DecodeResponse(body map[string]interface{}) (*IRResponse, error) {
	ir := &IRResponse{
		ID:      getString(body, "id"),
		Model:   getString(body, "model"),
		Created: getInt64(body, "created_at"),
	}

	ir.StopReason = mapResponsesStatus(getString(body, "status"))

	if output, ok := getList(body, "output"); ok {
		hasFunctionCall := false
		for _, item := range output {
			m, _ := item.(map[string]interface{})
			switch getString(m, "type") {
			case "message":
				if content, ok := m["content"]; ok {
					ir.Content = append(ir.Content, decodeResponsesContent(content)...)
				}
			case "function_call":
				hasFunctionCall = true
				input := make(map[string]interface{})
				if args := getString(m, "arguments"); args != "" {
					parseJSONString(args, &input)
				}
				ir.Content = append(ir.Content, IRContentBlock{
					Type:         IRBlockToolUse,
					ToolUseID:    getString(m, "call_id"),
					ToolUseName:  getString(m, "name"),
					ToolUseInput: input,
				})
			}
		}
		if hasFunctionCall {
			ir.StopReason = IRStopToolUse
		}
	}

	if u, ok := body["usage"].(map[string]interface{}); ok {
		ir.Usage = &IRUsage{
			InputTokens:  getIntDefault(u, "input_tokens"),
			OutputTokens: getIntDefault(u, "output_tokens"),
		}
		if promptDetails, ok := u["prompt_tokens_details"].(map[string]interface{}); ok {
			ir.Usage.CachedInputTokens = getIntDefault(promptDetails, "cached_tokens")
		}
	}

	return ir, nil
}

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

type openAIResponsesEncoder struct{}

func (e *openAIResponsesEncoder) EncodeRequest(ir *IRRequest) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"model": ir.Model,
	}

	if len(ir.Messages) == 1 && ir.Messages[0].Role == IRRoleUser && !hasToolContent(ir.Messages[0]) {
		body["input"] = ir.Messages[0].getTextContent()
	} else {
		skipSystemMessages := ir.Instructions != "" || ir.System != ""
		body["input"] = e.encodeInput(ir.Messages, skipSystemMessages)
	}

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

	if len(ir.Tools) > 0 {
		var tools []map[string]interface{}
		for _, t := range ir.Tools {
			tools = append(tools, map[string]interface{}{
				"type":        "function",
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			})
		}
		body["tools"] = tools
	}

	if ir.ToolChoice != nil {
		body["tool_choice"] = encodeResponsesToolChoice(ir.ToolChoice)
	}

	if ir.ReasoningEffort != "" {
		body["reasoning_effort"] = ir.ReasoningEffort
	}

	// 回传 Preserved 字段
	for k, v := range ir.Preserved {
		if _, exists := body[k]; !exists {
			body[k] = v
		}
	}

	return body, nil
}

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

func (e *openAIResponsesEncoder) encodeInput(msgs []IRMessage, skipSystemMessages bool) []map[string]interface{} {
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

			// Build content blocks (text only for first pass)
			var content []map[string]interface{}
			var functionCalls []map[string]interface{}
			isInputMessage := m.Role == IRRoleUser || m.Role == IRRoleSystem
			textType := "input_text"
			if !isInputMessage {
				textType = "output_text"
			}

			for _, block := range m.Content {
				switch block.Type {
				case IRBlockText:
					content = append(content, map[string]interface{}{"type": textType, "text": block.Text})
				case IRBlockToolUse:
					args := "{}"
					if block.ToolUseInput != nil {
						if b, err := marshalJSON(block.ToolUseInput); err == nil {
							args = string(b)
						}
					}
					functionCalls = append(functionCalls, map[string]interface{}{
						"type":      "function_call",
						"call_id":   block.ToolUseID,
						"id":        block.ToolUseID,
						"name":      block.ToolUseName,
						"arguments": args,
					})
				}
			}

			// message item (only if there is text content)
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
			for _, block := range m.Content {
				if block.Type == IRBlockToolResult {
					items = append(items, map[string]interface{}{
						"type":    "function_call_output",
						"call_id": block.ToolResultToolUseID,
						"output":  block.ToolResultContent,
					})
				}
			}
		}
	}

	return items
}

func (e *openAIResponsesEncoder) EncodeResponse(ir *IRResponse) (map[string]interface{}, error) {
	var output []map[string]interface{}
	var textParts []map[string]interface{}

	for _, block := range ir.Content {
		switch block.Type {
		case IRBlockText:
			textParts = append(textParts, map[string]interface{}{"type": "output_text", "text": block.Text})
		case IRBlockToolUse:
			if len(textParts) > 0 {
				output = append(output, map[string]interface{}{
					"type":    "message",
					"role":    "assistant",
					"content": textParts,
				})
				textParts = nil
			}
			args := "{}"
			if block.ToolUseInput != nil {
				if b, err := marshalJSON(block.ToolUseInput); err == nil {
					args = string(b)
				}
			}
			output = append(output, map[string]interface{}{
				"type":      "function_call",
				"id":        block.ToolUseID,
				"call_id":   block.ToolUseID,
				"name":      block.ToolUseName,
				"arguments": args,
				"status":    "completed",
			})
		}
	}

	if len(textParts) > 0 {
		output = append(output, map[string]interface{}{
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
			"total_tokens":  ir.Usage.InputTokens + ir.Usage.OutputTokens,
		}
		if ir.Usage.CachedInputTokens > 0 {
			usage["prompt_tokens_details"] = map[string]interface{}{
				"cached_tokens": ir.Usage.CachedInputTokens,
			}
		}
		resp["usage"] = usage
	}

	return resp, nil
}

func hasToolContent(m IRMessage) bool {
	for _, b := range m.Content {
		if b.Type == IRBlockToolUse || b.Type == IRBlockToolResult {
			return true
		}
	}
	return false
}

func decodeResponsesStreamEvent(data map[string]interface{}) []*IRStreamEvent {
	eventType := getString(data, "type")

	switch eventType {
	case "response.created":
		resp, _ := data["response"].(map[string]interface{})
		return []*IRStreamEvent{{
			Type:          IRStreamMessageStart,
			ResponseID:    getString(resp, "id"),
			ResponseModel: getString(resp, "model"),
		}}

	case "response.output_item.added":
		item, _ := data["item"].(map[string]interface{})
		idx := getIntDefault(data, "output_index")
		itemType := getString(item, "type")

		var block *IRContentBlock
		if itemType == "function_call" {
			block = &IRContentBlock{
				Type:        IRBlockToolUse,
				ToolUseID:   getString(item, "call_id"),
				ToolUseName: getString(item, "name"),
			}
		}
		return []*IRStreamEvent{{
			Type:         IRStreamContentStart,
			Index:        idx,
			ContentBlock: block,
		}}

	case "response.output_text.delta", "response.text.delta":
		idx := getIntDefault(data, "output_index")
		return []*IRStreamEvent{{
			Type:      IRStreamContentDelta,
			Index:     idx,
			DeltaType: "text",
			DeltaText: getString(data, "delta"),
		}}

	case "response.function_call_arguments.delta":
		idx := getIntDefault(data, "output_index")
		return []*IRStreamEvent{{
			Type:      IRStreamContentDelta,
			Index:     idx,
			DeltaType: "input_json",
			DeltaJSON: getString(data, "delta"),
		}}

	case "response.output_item.done":
		return []*IRStreamEvent{{Type: IRStreamContentStop, Index: getIntDefault(data, "output_index")}}

	case "response.done", "response.completed":
		resp, _ := data["response"].(map[string]interface{})
		var usage *IRUsage
		if u, ok := resp["usage"].(map[string]interface{}); ok {
			usage = &IRUsage{
				InputTokens:  getIntDefault(u, "input_tokens"),
				OutputTokens: getIntDefault(u, "output_tokens"),
			}
			if promptDetails, ok := u["prompt_tokens_details"].(map[string]interface{}); ok {
				usage.CachedInputTokens = getIntDefault(promptDetails, "cached_tokens")
			}
		}
		return []*IRStreamEvent{{
			Type:       IRStreamMessageDelta,
			StopReason: mapResponsesStatus(getString(resp, "status")),
			Usage:      usage,
		}, {Type: IRStreamDone}}

	case "error":
		err, _ := data["error"].(map[string]interface{})
		return []*IRStreamEvent{{
			Type:         IRStreamError,
			ErrorMessage: fmt.Sprintf("%s: %s", getString(err, "type"), getString(err, "message")),
			ErrorType:    getString(err, "type"),
			ErrorCode:    getString(err, "code"),
		}}
	}

	return nil
}

func encodeResponsesStreamEvent(event *IRStreamEvent) []map[string]interface{} {
	return encodeResponsesStreamEventWithState(event, nil)
}

type responsesStreamState struct {
	textStarted map[int]bool
	textStopped map[int]bool
	itemIDs     map[int]string
	textByIndex map[int]string
	responseID  string
	model       string
	stopReason  IRStopReason
	usage       *IRUsage
}

func (s *responsesStreamState) setResponse(event *IRStreamEvent) {
	if s == nil || event == nil {
		return
	}
	if event.ResponseID != "" {
		s.responseID = ensurePrefix(event.ResponseID, "resp")
	}
	if event.ResponseModel != "" {
		s.model = event.ResponseModel
	}
}

func (s *responsesStreamState) responseIDValue() string {
	if s == nil || s.responseID == "" {
		return "resp_stream"
	}
	return s.responseID
}

func (s *responsesStreamState) hasStartedText(index int) bool {
	return s != nil && s.textStarted != nil && s.textStarted[index]
}

func (s *responsesStreamState) markStartedText(index int) {
	if s == nil {
		return
	}
	if s.textStarted == nil {
		s.textStarted = make(map[int]bool)
	}
	s.textStarted[index] = true
}

func (s *responsesStreamState) hasStoppedText(index int) bool {
	return s != nil && s.textStopped != nil && s.textStopped[index]
}

func (s *responsesStreamState) markStoppedText(index int) {
	if s == nil {
		return
	}
	if s.textStopped == nil {
		s.textStopped = make(map[int]bool)
	}
	s.textStopped[index] = true
}

func (s *responsesStreamState) itemID(index int) string {
	if s == nil {
		return "msg_stream_" + strconv.Itoa(index)
	}
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
	if s == nil {
		return
	}
	if s.textByIndex == nil {
		s.textByIndex = make(map[int]string)
	}
	s.textByIndex[index] += delta
}

func (s *responsesStreamState) text(index int) string {
	if s == nil || s.textByIndex == nil {
		return ""
	}
	return s.textByIndex[index]
}

func (s *responsesStreamState) setMessageDelta(event *IRStreamEvent) {
	if s == nil || event == nil {
		return
	}
	if event.StopReason != "" {
		s.stopReason = event.StopReason
	}
	if event.Usage != nil {
		s.usage = event.Usage
	}
}

func encodeResponsesStreamEventWithState(event *IRStreamEvent, state *streamConversionState) []map[string]interface{} {
	var responseState *responsesStreamState
	if state != nil {
		responseState = &state.responses
	}
	switch event.Type {
	case IRStreamMessageStart:
		if responseState != nil {
			responseState.setResponse(event)
		}
		responseID := ensurePrefix(event.ResponseID, "resp")
		if responseID == "resp-" {
			responseID = "resp_stream"
		}
		return []map[string]interface{}{{
			"type": "response.created",
			"response": map[string]interface{}{
				"id":         responseID,
				"object":     "response",
				"created_at": now(),
				"model":      event.ResponseModel,
				"status":     "in_progress",
				"output":     []interface{}{},
			},
		}}

	case IRStreamContentStart:
		if event.ContentBlock != nil && event.ContentBlock.Type == IRBlockToolUse {
			return []map[string]interface{}{{
				"type":         "response.output_item.added",
				"output_index": event.Index,
				"item": map[string]interface{}{
					"type":      "function_call",
					"id":        event.ContentBlock.ToolUseID,
					"call_id":   event.ContentBlock.ToolUseID,
					"name":      event.ContentBlock.ToolUseName,
					"arguments": "",
					"status":    "in_progress",
				},
			}}
		}
		itemID := responseState.itemID(event.Index)
		responseState.markStartedText(event.Index)
		return []map[string]interface{}{{
			"type":         "response.output_item.added",
			"output_index": event.Index,
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
			"output_index":  event.Index,
			"content_index": 0,
			"part": map[string]interface{}{
				"type":        "output_text",
				"text":        "",
				"annotations": []interface{}{},
			},
		}}

	case IRStreamContentDelta:
		if event.DeltaType == "text" {
			itemID := responseState.itemID(event.Index)
			responseState.appendText(event.Index, event.DeltaText)
			return []map[string]interface{}{{
				"type":          "response.output_text.delta",
				"item_id":       itemID,
				"output_index":  event.Index,
				"content_index": 0,
				"delta":         event.DeltaText,
			}}
		} else if event.DeltaType == "input_json" {
			return []map[string]interface{}{{
				"type":         "response.function_call_arguments.delta",
				"output_index": event.Index,
				"delta":        event.DeltaJSON,
			}}
		}

	case IRStreamContentStop:
		if responseState.hasStartedText(event.Index) {
			itemID := responseState.itemID(event.Index)
			text := responseState.text(event.Index)
			responseState.markStoppedText(event.Index)
			part := map[string]interface{}{
				"type":        "output_text",
				"text":        text,
				"annotations": []interface{}{},
			}
			return []map[string]interface{}{{
				"type":          "response.output_text.done",
				"item_id":       itemID,
				"output_index":  event.Index,
				"content_index": 0,
				"text":          text,
			}, {
				"type":          "response.content_part.done",
				"item_id":       itemID,
				"output_index":  event.Index,
				"content_index": 0,
				"part":          part,
			}, {
				"type":         "response.output_item.done",
				"output_index": event.Index,
				"item": map[string]interface{}{
					"id":      itemID,
					"type":    "message",
					"status":  "completed",
					"role":    "assistant",
					"content": []map[string]interface{}{part},
				},
			}}
		}
		return []map[string]interface{}{{
			"type":         "response.output_item.done",
			"output_index": event.Index,
		}}

	case IRStreamMessageDelta:
		if responseState != nil {
			responseState.setMessageDelta(event)
		}
		return nil

	case IRStreamDone:
		usage := map[string]interface{}{
			"input_tokens":  0,
			"output_tokens": 0,
			"total_tokens":  0,
		}
		eventUsage := event.Usage
		if eventUsage == nil && responseState != nil {
			eventUsage = responseState.usage
		}
		if eventUsage != nil {
			totalTokens := eventUsage.TotalTokens
			if totalTokens == 0 {
				totalTokens = eventUsage.InputTokens + eventUsage.OutputTokens
			}
			usage = map[string]interface{}{
				"input_tokens":  eventUsage.InputTokens,
				"output_tokens": eventUsage.OutputTokens,
				"total_tokens":  totalTokens,
			}
			if eventUsage.CachedInputTokens > 0 {
				usage["prompt_tokens_details"] = map[string]interface{}{
					"cached_tokens": eventUsage.CachedInputTokens,
				}
			}
		}
		status := "completed"
		if responseState != nil && responseState.stopReason == IRStopMaxTokens {
			status = "incomplete"
		} else if responseState != nil && responseState.stopReason == IRStopError {
			status = "failed"
		}
		responseID := ensurePrefix(event.ResponseID, "resp")
		if responseID == "resp-" && responseState != nil {
			responseID = responseState.responseIDValue()
		}
		if responseID == "resp-" {
			responseID = "resp_stream"
		}
		model := event.ResponseModel
		if model == "" && responseState != nil {
			model = responseState.model
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
		}}

	case IRStreamError:
		evt := map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "error",
				"message": event.ErrorMessage,
			},
		}
		if event.ErrorType != "" {
			evt["error"].(map[string]interface{})["type"] = event.ErrorType
		}
		if event.ErrorCode != "" {
			evt["error"].(map[string]interface{})["code"] = event.ErrorCode
		}
		return []map[string]interface{}{evt}
	}

	return nil
}
