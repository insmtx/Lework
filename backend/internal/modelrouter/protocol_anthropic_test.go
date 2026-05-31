package modelrouter

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// TestAnthropicDecodeRequest
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAnthropicDecodeRequest(t *testing.T) {
	adapter := &anthropicMessagesAdapter{}

	t.Run("text_message", func(t *testing.T) {
		raw := map[string]interface{}{
			"model":      "claude-sonnet-4-20250514",
			"max_tokens": 1024,
			"system":     "You are a helpful assistant.",
			"messages": []interface{}{
				map[string]interface{}{
					"role": "user",
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": "Hello"},
					},
				},
			},
		}

		ir, err := adapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		if ir.Model != "claude-sonnet-4-20250514" {
			t.Errorf("Model = %q", ir.Model)
		}
		if ir.System != "You are a helpful assistant." {
			t.Errorf("System = %q", ir.System)
		}
		if len(ir.Messages) != 1 {
			t.Fatalf("len(Messages) = %d", len(ir.Messages))
		}
		if ir.Messages[0].Role != IRRoleUser {
			t.Errorf("Messages[0].Role = %q", ir.Messages[0].Role)
		}
		if len(ir.Messages[0].Parts) != 1 || ir.Messages[0].Parts[0].Type != IRPartText {
			t.Errorf("Messages[0].Parts = %+v", ir.Messages[0].Parts)
		}
	})

	t.Run("tool_use", func(t *testing.T) {
		raw := map[string]interface{}{
			"model":      "claude-sonnet-4-20250514",
			"max_tokens": 1024,
			"messages": []interface{}{
				map[string]interface{}{
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{
							"type":  "tool_use",
							"id":    "toolu_001",
							"name":  "get_weather",
							"input": map[string]interface{}{"city": "Tokyo"},
						},
					},
				},
			},
		}

		ir, err := adapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		parts := ir.Messages[0].Parts
		if len(parts) != 1 || parts[0].Type != IRPartToolCall {
			t.Fatalf("Parts[0].Type = %q", parts[0].Type)
		}
		if parts[0].ToolCall.ID != "toolu_001" {
			t.Errorf("ToolCall.ID = %q", parts[0].ToolCall.ID)
		}
		if parts[0].ToolCall.Name != "get_weather" {
			t.Errorf("ToolCall.Name = %q", parts[0].ToolCall.Name)
		}
		if parts[0].ToolCall.ArgumentsJSON == nil {
			t.Error("ToolCall.ArgumentsJSON is nil")
		} else if parts[0].ToolCall.ArgumentsJSON["city"] != "Tokyo" {
			t.Errorf("ToolCall.ArgumentsJSON[city] = %v", parts[0].ToolCall.ArgumentsJSON["city"])
		}
		if parts[0].ToolCall.Status != "completed" {
			t.Errorf("ToolCall.Status = %q", parts[0].ToolCall.Status)
		}
	})

	t.Run("tool_result", func(t *testing.T) {
		raw := map[string]interface{}{
			"model":      "claude-sonnet-4-20250514",
			"max_tokens": 1024,
			"messages": []interface{}{
				map[string]interface{}{
					"role": "user",
					"content": []interface{}{
						map[string]interface{}{
							"type":        "tool_result",
							"tool_use_id": "toolu_001",
							"content":     "25C sunny",
						},
					},
				},
			},
		}

		ir, err := adapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		parts := ir.Messages[0].Parts
		if len(parts) != 1 || parts[0].Type != IRPartToolResult {
			t.Fatalf("Parts[0].Type = %q", parts[0].Type)
		}
		if parts[0].ToolResult.ToolCallID != "toolu_001" {
			t.Errorf("ToolResult.ToolCallID = %q", parts[0].ToolResult.ToolCallID)
		}
		if parts[0].ToolResult.Status != "success" {
			t.Errorf("ToolResult.Status = %q", parts[0].ToolResult.Status)
		}
		if len(parts[0].ToolResult.Content) != 1 || parts[0].ToolResult.Content[0].Text != "25C sunny" {
			t.Errorf("ToolResult.Content = %+v", parts[0].ToolResult.Content)
		}
	})

	t.Run("thinking", func(t *testing.T) {
		raw := map[string]interface{}{
			"model":      "claude-sonnet-4-20250514",
			"max_tokens": 1024,
			"messages": []interface{}{
				map[string]interface{}{
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{
							"type":      "thinking",
							"thinking":  "Let me think...",
							"signature": "sig_abc",
						},
					},
				},
			},
		}

		ir, err := adapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		parts := ir.Messages[0].Parts
		if len(parts) != 1 || parts[0].Type != IRPartReasoning {
			t.Fatalf("Parts[0].Type = %q", parts[0].Type)
		}
		if parts[0].Reasoning.Content != "Let me think..." {
			t.Errorf("Reasoning.Content = %q", parts[0].Reasoning.Content)
		}
		if parts[0].Reasoning.Signature != "sig_abc" {
			t.Errorf("Reasoning.Signature = %q", parts[0].Reasoning.Signature)
		}
	})

	t.Run("redacted_thinking", func(t *testing.T) {
		raw := map[string]interface{}{
			"model":      "claude-sonnet-4-20250514",
			"max_tokens": 1024,
			"messages": []interface{}{
				map[string]interface{}{
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{
							"type":      "redacted_thinking",
							"signature": "sig_xyz",
						},
					},
				},
			},
		}

		ir, err := adapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		parts := ir.Messages[0].Parts
		if parts[0].Reasoning.Content != "[REDACTED]" {
			t.Errorf("Reasoning.Content = %q", parts[0].Reasoning.Content)
		}
		if parts[0].Reasoning.Signature != "sig_xyz" {
			t.Errorf("Reasoning.Signature = %q", parts[0].Reasoning.Signature)
		}
	})

	t.Run("system_as_array", func(t *testing.T) {
		raw := map[string]interface{}{
			"model":      "claude-sonnet-4-20250514",
			"max_tokens": 1024,
			"system": []interface{}{
				map[string]interface{}{"type": "text", "text": "You are helpful."},
				map[string]interface{}{"type": "text", "text": " Be concise."},
			},
			"messages": []interface{}{
				map[string]interface{}{
					"role": "user",
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": "Hi"},
					},
				},
			},
		}

		ir, err := adapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		if ir.System != "You are helpful. Be concise." {
			t.Errorf("System = %q", ir.System)
		}
	})

	t.Run("tool_choice_parsing", func(t *testing.T) {
		tests := []struct {
			name     string
			raw      interface{}
			wantType string
			wantName string
		}{
			{"any->required", map[string]interface{}{"type": "any"}, "required", ""},
			{"tool->specific", map[string]interface{}{"type": "tool", "name": "get_weather"}, "specific", "get_weather"},
			{"auto", map[string]interface{}{"type": "auto"}, "auto", ""},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				raw := map[string]interface{}{
					"model":       "claude-sonnet-4-20250514",
					"max_tokens":  1024,
					"tool_choice": tt.raw,
					"messages": []interface{}{
						map[string]interface{}{"role": "user", "content": "Hi"},
					},
				}
				ir, err := adapter.DecodeRequest(raw)
				if err != nil {
					t.Fatalf("DecodeRequest() error = %v", err)
				}
				if ir.ToolChoice == nil {
					t.Fatal("ToolChoice is nil")
				}
				if ir.ToolChoice.Type != tt.wantType {
					t.Errorf("ToolChoice.Type = %q, want %q", ir.ToolChoice.Type, tt.wantType)
				}
				if ir.ToolChoice.Name != tt.wantName {
					t.Errorf("ToolChoice.Name = %q", ir.ToolChoice.Name)
				}
			})
		}
	})
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// TestAnthropicEncodeRequest
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAnthropicEncodeRequest(t *testing.T) {
	adapter := &anthropicMessagesAdapter{}

	t.Run("basic_roundtrip", func(t *testing.T) {
		ir := &IRRequest{
			Model:     "claude-sonnet-4-20250514",
			System:    "You are a helpful assistant.",
			MaxTokens: 2048,
			Stream:    true,
			Messages: []IRMessage{
				{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "Hello"}}},
			},
		}

		raw, err := adapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest() error = %v", err)
		}
		if getString(raw, "model") != "claude-sonnet-4-20250514" {
			t.Errorf("model = %q", getString(raw, "model"))
		}
		if getIntDefault(raw, "max_tokens") != 2048 {
			t.Errorf("max_tokens = %d", getIntDefault(raw, "max_tokens"))
		}
		if getString(raw, "system") != "You are a helpful assistant." {
			t.Errorf("system = %q", getString(raw, "system"))
		}
		if !getBool(raw, "stream") {
			t.Error("stream = false")
		}
	})

	t.Run("default_max_tokens_4096", func(t *testing.T) {
		ir := &IRRequest{
			Model: "claude-sonnet-4-20250514",
			Messages: []IRMessage{
				{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "Hi"}}},
			},
		}
		raw, err := adapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest() error = %v", err)
		}
		if getIntDefault(raw, "max_tokens") != 4096 {
			t.Errorf("max_tokens = %d, want 4096", getIntDefault(raw, "max_tokens"))
		}
	})

	t.Run("system_mapping", func(t *testing.T) {
		ir := &IRRequest{
			Model:     "claude-sonnet-4-20250514",
			System:    "Be concise.",
			MaxTokens: 4096,
			Messages: []IRMessage{
				{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "Hi"}}},
			},
		}
		raw, err := adapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest() error = %v", err)
		}
		if getString(raw, "system") != "Be concise." {
			t.Errorf("system = %q", getString(raw, "system"))
		}
	})

	t.Run("tools_and_tool_choice", func(t *testing.T) {
		temp := 0.5
		ir := &IRRequest{
			Model:       "claude-sonnet-4-20250514",
			MaxTokens:   4096,
			Temperature: &temp,
			Tools: []IRToolDecl{
				{
					Type:        "function",
					Name:        "get_weather",
					Description: "Get weather",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"city": map[string]interface{}{"type": "string"},
						},
						"required": []interface{}{"city"},
					},
				},
			},
			ToolChoice: &IRToolChoice{Type: "auto"},
			Messages: []IRMessage{
				{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "weather?"}}},
			},
		}
		raw, err := adapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest() error = %v", err)
		}
		tools, ok := raw["tools"].([]map[string]interface{})
		if !ok || len(tools) != 1 {
			t.Fatalf("len(tools) = %v (%T), want 1", raw["tools"], raw["tools"])
		}
		if tools[0]["name"] != "get_weather" {
			t.Errorf("tool name = %q", tools[0]["name"])
		}
	})

	t.Run("thinking_in_request", func(t *testing.T) {
		ir := &IRRequest{
			Model:     "claude-sonnet-4-20250514",
			MaxTokens: 4096,
			Messages: []IRMessage{
				{
					Role: IRRoleAssistant,
					Parts: []IRContentPart{
						{
							Type:      IRPartReasoning,
							Reasoning: &IRReasoningPart{Content: "Let me think...", Signature: "sig_abc"},
						},
						{Type: IRPartText, Text: "The answer is 42."},
					},
				},
			},
		}
		raw, err := adapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest() error = %v", err)
		}
		msgs, ok := raw["messages"].([]map[string]interface{})
		if !ok || len(msgs) != 1 {
			t.Fatalf("len(messages) = %v (%T), want 1", raw["messages"], raw["messages"])
		}
		msg := msgs[0]
		content, ok := msg["content"].([]map[string]interface{})
		if !ok || len(content) != 2 {
			t.Fatalf("len(content) = %v (%T), want 2", msg["content"], msg["content"])
		}
		if content[0]["type"] != "thinking" {
			t.Errorf("first content type = %q", content[0]["type"])
		}
		if content[0]["signature"] != "sig_abc" {
			t.Errorf("signature = %q", content[0]["signature"])
		}
	})

	t.Run("roles_mapping", func(t *testing.T) {
		ir := &IRRequest{
			Model:     "claude-sonnet-4-20250514",
			MaxTokens: 4096,
			Messages: []IRMessage{
				{
					Role: IRRoleAssistant,
					Parts: []IRContentPart{
						{Type: IRPartText, Text: "I found the weather."},
						{
							Type: IRPartToolCall,
							ToolCall: &IRToolCallPart{
								ID:            "toolu_001",
								Name:          "get_weather",
								ArgumentsJSON: map[string]interface{}{"city": "Tokyo"},
							},
						},
					},
				},
				{
					Role: IRRoleUser,
					Parts: []IRContentPart{
						{
							Type: IRPartToolResult,
							ToolResult: &IRToolResultPart{
								ToolCallID: "toolu_001",
								Content:    []IRContentPart{{Type: IRPartText, Text: "25C"}},
							},
						},
					},
				},
			},
		}

		raw, err := adapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest() error = %v", err)
		}
		msgs, ok := raw["messages"].([]map[string]interface{})
		if !ok || len(msgs) != 2 {
			t.Fatalf("len(messages) = %v (%T), want 2", raw["messages"], raw["messages"])
		}
	})
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// TestAnthropicDecodeResponse
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAnthropicDecodeResponse(t *testing.T) {
	adapter := &anthropicMessagesAdapter{}

	t.Run("text_response", func(t *testing.T) {
		raw := map[string]interface{}{
			"id":    "msg_001",
			"model": "claude-sonnet-4-20250514",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "Hello, world!"},
			},
			"stop_reason": "end_turn",
			"usage": map[string]interface{}{
				"input_tokens":  10,
				"output_tokens": 15,
			},
		}

		ir, err := adapter.DecodeResponse(raw)
		if err != nil {
			t.Fatalf("DecodeResponse() error = %v", err)
		}
		if ir.ID != "msg_001" {
			t.Errorf("ID = %q", ir.ID)
		}
		if ir.StopReason != IRStopEndTurn {
			t.Errorf("StopReason = %q", ir.StopReason)
		}
		if ir.Usage == nil || ir.Usage.InputTokens != 10 || ir.Usage.OutputTokens != 15 {
			t.Errorf("Usage = %+v", ir.Usage)
		}
	})

	t.Run("cache_tokens", func(t *testing.T) {
		raw := map[string]interface{}{
			"id":    "msg_003",
			"model": "claude-sonnet-4-20250514",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "OK"},
			},
			"stop_reason": "end_turn",
			"usage": map[string]interface{}{
				"input_tokens":                10,
				"output_tokens":               5,
				"cache_creation_input_tokens": 20,
				"cache_read_input_tokens":     30,
			},
		}

		ir, err := adapter.DecodeResponse(raw)
		if err != nil {
			t.Fatalf("DecodeResponse() error = %v", err)
		}
		if ir.Usage.CacheReadInputTokens != 30 {
			t.Errorf("CacheReadInputTokens = %d, want 30", ir.Usage.CacheReadInputTokens)
		}
		if ir.Usage.CacheCreationInputTokens != 20 {
			t.Errorf("CacheCreationInputTokens = %d, want 20", ir.Usage.CacheCreationInputTokens)
		}
	})

	t.Run("stop_reasons", func(t *testing.T) {
		tests := []struct {
			rawReason string
			want      IRStopReason
		}{
			{"end_turn", IRStopEndTurn},
			{"max_tokens", IRStopMaxTokens},
			{"stop_sequence", IRStopStopSequence},
			{"tool_use", IRStopToolUse},
		}

		for _, tt := range tests {
			t.Run(tt.rawReason, func(t *testing.T) {
				raw := map[string]interface{}{
					"id":          "msg_004",
					"model":       "claude-sonnet-4-20250514",
					"content":     []interface{}{map[string]interface{}{"type": "text", "text": "x"}},
					"stop_reason": tt.rawReason,
				}
				ir, err := adapter.DecodeResponse(raw)
				if err != nil {
					t.Fatalf("DecodeResponse() error = %v", err)
				}
				if ir.StopReason != tt.want {
					t.Errorf("StopReason = %q, want %q", ir.StopReason, tt.want)
				}
			})
		}
	})
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// TestAnthropicEncodeResponse
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAnthropicEncodeResponse(t *testing.T) {
	adapter := &anthropicMessagesAdapter{}

	t.Run("text_response_roundtrip", func(t *testing.T) {
		ir := &IRResponse{
			ID:      "msg_001",
			Model:   "claude-sonnet-4-20250514",
			Content: []IRContentPart{{Type: IRPartText, Text: "Hello, world!"}},
			Usage: &IRUsage{
				InputTokens:       10,
				OutputTokens:      15,
				CacheReadInputTokens: 25,
			},
			StopReason: IRStopEndTurn,
		}

		raw, err := adapter.EncodeResponse(ir)
		if err != nil {
			t.Fatalf("EncodeResponse() error = %v", err)
		}
		if raw["id"] != "msg_001" {
			t.Errorf("id = %v", raw["id"])
		}
		if raw["stop_reason"] != "end_turn" {
			t.Errorf("stop_reason = %v", raw["stop_reason"])
		}
		if raw["type"] != "message" {
			t.Errorf("type = %v", raw["type"])
		}
		content, ok := raw["content"].([]map[string]interface{})
		if !ok || len(content) != 1 {
			t.Fatalf("len(content) = %v (%T), want 1", raw["content"], raw["content"])
		}
		if content[0]["text"] != "Hello, world!" {
			t.Errorf("content[0].text = %v", content[0]["text"])
		}
		usage, _ := raw["usage"].(map[string]interface{})
		if usage["input_tokens"] != 10 {
			t.Errorf("usage.input_tokens = %v", usage["input_tokens"])
		}
		if usage["cache_read_input_tokens"] != 25 {
			t.Errorf("usage.cache_read_input_tokens = %v", usage["cache_read_input_tokens"])
		}
	})

	t.Run("thinking_blocks", func(t *testing.T) {
		ir := &IRResponse{
			ID:    "msg_002",
			Model: "claude-sonnet-4-20250514",
			Content: []IRContentPart{
				{
					Type:      IRPartReasoning,
					Reasoning: &IRReasoningPart{Content: "thinking...", Signature: "sig_001"},
				},
				{Type: IRPartText, Text: "Answer"},
			},
			Usage:      &IRUsage{InputTokens: 10, OutputTokens: 5},
			StopReason: IRStopEndTurn,
		}

		raw, err := adapter.EncodeResponse(ir)
		if err != nil {
			t.Fatalf("EncodeResponse() error = %v", err)
		}
		content, ok := raw["content"].([]map[string]interface{})
		if !ok || len(content) != 2 {
			t.Fatalf("len(content) = %v (%T), want 2", raw["content"], raw["content"])
		}
		if content[0]["type"] != "thinking" {
			t.Errorf("content[0].type = %v", content[0]["type"])
		}
		if content[0]["signature"] != "sig_001" {
			t.Errorf("content[0].signature = %v", content[0]["signature"])
		}
	})

	t.Run("tool_use_response", func(t *testing.T) {
		ir := &IRResponse{
			ID:    "msg_003",
			Model: "claude-sonnet-4-20250514",
			Content: []IRContentPart{
				{
					Type: IRPartToolCall,
					ToolCall: &IRToolCallPart{
						ID:            "toolu_001",
						Name:          "get_weather",
						ArgumentsJSON: map[string]interface{}{"city": "Tokyo"},
					},
				},
			},
			StopReason: IRStopToolUse,
		}

		raw, err := adapter.EncodeResponse(ir)
		if err != nil {
			t.Fatalf("EncodeResponse() error = %v", err)
		}
		if raw["stop_reason"] != "tool_use" {
			t.Errorf("stop_reason = %v", raw["stop_reason"])
		}
		content, ok := raw["content"].([]map[string]interface{})
		if !ok || len(content) != 1 {
			t.Fatalf("len(content) = %v (%T)", raw["content"], raw["content"])
		}
		if content[0]["type"] != "tool_use" {
			t.Errorf("content[0].type = %v", content[0]["type"])
		}
		if content[0]["id"] != "toolu_001" {
			t.Errorf("content[0].id = %v", content[0]["id"])
		}
	})
}

func TestAnthropicDecodeStreamEvent(t *testing.T) {
	adapter := &anthropicMessagesAdapter{}
	newState := func() interface{} { return adapter.NewStreamState() }

	t.Run("message_start", func(t *testing.T) {
		raw := map[string]interface{}{
			"type": "message_start",
			"message": map[string]interface{}{
				"id":      "msg_ant_001",
				"type":    "message",
				"role":    "assistant",
				"model":   "claude-sonnet-4-20250514",
				"content": []interface{}{},
				"usage": map[string]interface{}{
					"input_tokens":  15,
					"output_tokens": 0,
				},
			},
		}

		events, err := adapter.DecodeStreamEvent(raw, newState())
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		e := events[0]
		if e.Type != IRStreamMessageStart {
			t.Errorf("Type = %q", e.Type)
		}
		if e.ResponseID != "msg_ant_001" {
			t.Errorf("ResponseID = %q", e.ResponseID)
		}
		if e.Usage == nil || e.Usage.InputTokens != 15 {
			t.Errorf("Usage = %+v", e.Usage)
		}
	})

	t.Run("content_block_start_text", func(t *testing.T) {
		raw := map[string]interface{}{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]interface{}{
				"type": "text",
				"text": "",
			},
		}

		events, err := adapter.DecodeStreamEvent(raw, newState())
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		e := events[0]
		if e.Type != IRStreamContentStart || e.Index != 0 {
			t.Errorf("Type=%q Index=%d", e.Type, e.Index)
		}
		if e.Part == nil || e.Part.Type != IRPartText {
			t.Errorf("Part.Type = %v", e.Part)
		}
	})

	t.Run("content_block_start_tool_use", func(t *testing.T) {
		raw := map[string]interface{}{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]interface{}{
				"type": "tool_use",
				"id":   "toolu_ant_002_get_weather",
				"name": "get_weather",
			},
		}

		events, err := adapter.DecodeStreamEvent(raw, newState())
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		e := events[0]
		if e.Part.ToolCall == nil || e.Part.ToolCall.ID != "toolu_ant_002_get_weather" {
			t.Errorf("ToolCall = %+v", e.Part.ToolCall)
		}
	})

	t.Run("content_block_start_thinking", func(t *testing.T) {
		raw := map[string]interface{}{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]interface{}{
				"type":      "thinking",
				"thinking":  "Let me think...",
				"signature": "sig_001",
			},
		}

		events, err := adapter.DecodeStreamEvent(raw, newState())
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		e := events[0]
		if e.Part.Reasoning == nil || e.Part.Reasoning.Content != "Let me think..." {
			t.Errorf("Reasoning = %+v", e.Part.Reasoning)
		}
	})

	t.Run("text_delta", func(t *testing.T) {
		st := newState()
		// Simulate block start
		start := map[string]interface{}{
			"type":          "content_block_start",
			"index":         0,
			"content_block": map[string]interface{}{"type": "text", "text": ""},
		}
		adapter.DecodeStreamEvent(start, st)

		raw := map[string]interface{}{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]interface{}{
				"type": "text_delta",
				"text": "Paris.",
			},
		}
		events, err := adapter.DecodeStreamEvent(raw, st)
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		if events[0].DeltaText != "Paris." {
			t.Errorf("DeltaText = %q", events[0].DeltaText)
		}
	})

	t.Run("input_json_delta", func(t *testing.T) {
		st := newState()
		// Simulate tool_use start
		start := map[string]interface{}{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]interface{}{
				"type": "tool_use", "id": "t1", "name": "search",
			},
		}
		adapter.DecodeStreamEvent(start, st)

		raw := map[string]interface{}{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]interface{}{
				"type":         "input_json_delta",
				"partial_json": `{"q`,
			},
		}
		events, err := adapter.DecodeStreamEvent(raw, st)
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		if events[0].DeltaJSON != `{"q` {
			t.Errorf("DeltaJSON = %q", events[0].DeltaJSON)
		}
	})

	t.Run("no_delta_after_content_block_stop", func(t *testing.T) {
		st := newState()
		// Start text block
		adapter.DecodeStreamEvent(map[string]interface{}{
			"type": "content_block_start", "index": 0,
			"content_block": map[string]interface{}{"type": "text", "text": ""},
		}, st)
		// Stop it
		adapter.DecodeStreamEvent(map[string]interface{}{
			"type": "content_block_stop", "index": 0,
		}, st)
		// Try delta
		events, err := adapter.DecodeStreamEvent(map[string]interface{}{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]interface{}{"type": "text_delta", "text": "AFTER_STOP"},
		}, st)
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		if len(events) != 0 {
			t.Errorf("Expected no events after stop, got %d", len(events))
		}
	})

	t.Run("message_delta_with_stop_reason", func(t *testing.T) {
		raw := map[string]interface{}{
			"type": "message_delta",
			"delta": map[string]interface{}{
				"stop_reason":   "end_turn",
				"stop_sequence": nil,
			},
			"usage": map[string]interface{}{"output_tokens": 15},
		}
		events, err := adapter.DecodeStreamEvent(raw, newState())
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		e := events[0]
		if e.Type != IRStreamMessageDelta {
			t.Errorf("Type = %q", e.Type)
		}
		if e.StopReason != IRStopEndTurn {
			t.Errorf("StopReason = %q", e.StopReason)
		}
	})

	t.Run("message_stop", func(t *testing.T) {
		raw := map[string]interface{}{"type": "message_stop"}
		events, err := adapter.DecodeStreamEvent(raw, newState())
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		if events[0].Type != IRStreamDone {
			t.Errorf("Type = %q", events[0].Type)
		}
	})

	t.Run("error_event", func(t *testing.T) {
		raw := map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "overloaded_error",
				"message": "Overloaded",
			},
		}
		events, err := adapter.DecodeStreamEvent(raw, newState())
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		e := events[0]
		if e.Type != IRStreamError {
			t.Errorf("Type = %q", e.Type)
		}
		if e.ErrorType != "overloaded_error" {
			t.Errorf("ErrorType = %q", e.ErrorType)
		}
	})
}

func TestAnthropicEncodeStreamEvent(t *testing.T) {
	adapter := &anthropicMessagesAdapter{}
	st := adapter.NewStreamState()

	t.Run("message_start", func(t *testing.T) {
		ir := &IRStreamEvent{
			Type:          IRStreamMessageStart,
			ResponseID:    "msg_ant_001",
			ResponseModel: "claude-sonnet-4-20250514",
		}

		payloads, err := adapter.EncodeStreamEvent(ir, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		if len(payloads) != 1 {
			t.Fatalf("len(payloads) = %d", len(payloads))
		}
		p := payloads[0]
		if getString(p, "type") != "message_start" {
			t.Errorf("type = %q", getString(p, "type"))
		}
		msg, _ := p["message"].(map[string]interface{})
		if getString(msg, "id") != "msg_ant_001" {
			t.Errorf("message.id = %q", getString(msg, "id"))
		}
	})

	t.Run("content_start_text", func(t *testing.T) {
		ir := &IRStreamEvent{
			Type:  IRStreamContentStart,
			Index: 0,
			Part:  &IRContentPart{Type: IRPartText},
		}

		payloads, err := adapter.EncodeStreamEvent(ir, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		p := payloads[0]
		if getString(p, "type") != "content_block_start" {
			t.Errorf("type = %q", getString(p, "type"))
		}
		cb, _ := p["content_block"].(map[string]interface{})
		if getString(cb, "type") != "text" {
			t.Errorf("content_block.type = %q", getString(cb, "type"))
		}
	})

	t.Run("content_start_tool_use", func(t *testing.T) {
		ir := &IRStreamEvent{
			Type:  IRStreamContentStart,
			Index: 0,
			Part: &IRContentPart{
				Type:     IRPartToolCall,
				ToolCall: &IRToolCallPart{ID: "toolu_001", Name: "get_weather"},
			},
		}

		payloads, err := adapter.EncodeStreamEvent(ir, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		cb, _ := payloads[0]["content_block"].(map[string]interface{})
		if getString(cb, "id") != "toolu_001" {
			t.Errorf("content_block.id = %q", getString(cb, "id"))
		}
	})

	t.Run("content_start_reasoning", func(t *testing.T) {
		ir := &IRStreamEvent{
			Type:  IRStreamContentStart,
			Index: 0,
			Part: &IRContentPart{
				Type:      IRPartReasoning,
				Reasoning: &IRReasoningPart{Content: "thinking...", Signature: "sig_001"},
			},
		}

		payloads, err := adapter.EncodeStreamEvent(ir, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		cb, _ := payloads[0]["content_block"].(map[string]interface{})
		if getString(cb, "type") != "thinking" {
			t.Errorf("content_block.type = %q", getString(cb, "type"))
		}
		if getString(cb, "signature") != "sig_001" {
			t.Errorf("content_block.signature = %q", getString(cb, "signature"))
		}
	})

	t.Run("delta_text", func(t *testing.T) {
		ir := &IRStreamEvent{
			Type:      IRStreamContentDelta,
			Index:     0,
			DeltaText: "hello",
		}

		payloads, err := adapter.EncodeStreamEvent(ir, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		delta, _ := payloads[0]["delta"].(map[string]interface{})
		if getString(delta, "type") != "text_delta" {
			t.Errorf("delta.type = %q", getString(delta, "type"))
		}
		if getString(delta, "text") != "hello" {
			t.Errorf("delta.text = %q", getString(delta, "text"))
		}
	})

	t.Run("delta_input_json", func(t *testing.T) {
		ir := &IRStreamEvent{
			Type:      IRStreamContentDelta,
			Index:     0,
			DeltaJSON: `{"city"`,
		}

		payloads, err := adapter.EncodeStreamEvent(ir, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		delta, _ := payloads[0]["delta"].(map[string]interface{})
		if getString(delta, "type") != "input_json_delta" {
			t.Errorf("delta.type = %q", getString(delta, "type"))
		}
		if getString(delta, "partial_json") != `{"city"` {
			t.Errorf("delta.partial_json = %q", getString(delta, "partial_json"))
		}
	})

	t.Run("delta_thinking", func(t *testing.T) {
		ir := &IRStreamEvent{
			Type:      IRStreamContentDelta,
			Index:     0,
			DeltaText: "hmm...",
			Part:      &IRContentPart{Type: IRPartReasoning},
		}

		payloads, err := adapter.EncodeStreamEvent(ir, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		delta, _ := payloads[0]["delta"].(map[string]interface{})
		if getString(delta, "type") != "thinking_delta" {
			t.Errorf("delta.type = %q", getString(delta, "type"))
		}
		if getString(delta, "thinking") != "hmm..." {
			t.Errorf("delta.thinking = %q", getString(delta, "thinking"))
		}
	})

	t.Run("content_stop", func(t *testing.T) {
		ir := &IRStreamEvent{
			Type:  IRStreamContentStop,
			Index: 0,
		}

		payloads, err := adapter.EncodeStreamEvent(ir, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		if getString(payloads[0], "type") != "content_block_stop" {
			t.Errorf("type = %q", getString(payloads[0], "type"))
		}
	})

	t.Run("message_delta", func(t *testing.T) {
		ir := &IRStreamEvent{
			Type:       IRStreamMessageDelta,
			StopReason: IRStopToolUse,
			Usage:      &IRUsage{OutputTokens: 25},
		}

		payloads, err := adapter.EncodeStreamEvent(ir, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		p := payloads[0]
		if getString(p, "type") != "message_delta" {
			t.Errorf("type = %q", getString(p, "type"))
		}
		delta, _ := p["delta"].(map[string]interface{})
		if getString(delta, "stop_reason") != "tool_use" {
			t.Errorf("delta.stop_reason = %q", getString(delta, "stop_reason"))
		}
		usage, _ := p["usage"].(map[string]interface{})
		if getIntDefault(usage, "output_tokens") != 25 {
			t.Errorf("usage.output_tokens = %d", getIntDefault(usage, "output_tokens"))
		}
	})

	t.Run("done", func(t *testing.T) {
		ir := &IRStreamEvent{Type: IRStreamDone}
		payloads, err := adapter.EncodeStreamEvent(ir, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		if getString(payloads[0], "type") != "message_stop" {
			t.Errorf("type = %q", getString(payloads[0], "type"))
		}
	})

	t.Run("error", func(t *testing.T) {
		ir := &IRStreamEvent{
			Type:         IRStreamError,
			ErrorMessage: "rate limit exceeded",
			ErrorType:    "rate_limit",
		}
		payloads, err := adapter.EncodeStreamEvent(ir, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		errObj, _ := payloads[0]["error"].(map[string]interface{})
		if getString(errObj, "type") != "rate_limit" {
			t.Errorf("error.type = %q", getString(errObj, "type"))
		}
		if getString(errObj, "message") != "rate limit exceeded" {
			t.Errorf("error.message = %q", getString(errObj, "message"))
		}
	})
}

func TestAnthropicParallelToolCalls(t *testing.T) {
	adapter := &anthropicMessagesAdapter{}
	st := adapter.NewStreamState()

	// Simulate 2 tool_use blocks with interleaved deltas.
	// Block 0: tool_use start -> input_json deltas -> stop
	// Block 1: tool_use start -> input_json deltas -> stop

	// Start block 0
	events, err := adapter.DecodeStreamEvent(map[string]interface{}{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]interface{}{
			"type": "tool_use", "id": "call_1", "name": "Write",
		},
	}, st)
	if err != nil {
		t.Fatalf("DecodeStreamEvent(block0 start) error = %v", err)
	}
	if events[0].Type != IRStreamContentStart {
		t.Errorf("block 0 start Type = %q", events[0].Type)
	}
	if events[0].Part.ToolCall.Name != "Write" {
		t.Errorf("block 0 ToolCall.Name = %q", events[0].Part.ToolCall.Name)
	}

	// Start block 1
	events, err = adapter.DecodeStreamEvent(map[string]interface{}{
		"type":  "content_block_start",
		"index": 1,
		"content_block": map[string]interface{}{
			"type": "tool_use", "id": "call_2", "name": "Read",
		},
	}, st)
	if err != nil {
		t.Fatalf("DecodeStreamEvent(block1 start) error = %v", err)
	}
	if events[0].Part.ToolCall.Name != "Read" {
		t.Errorf("block 1 ToolCall.Name = %q", events[0].Part.ToolCall.Name)
	}

	// Delta for block 0
	events, err = adapter.DecodeStreamEvent(map[string]interface{}{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]interface{}{"type": "input_json_delta", "partial_json": `{"file`},
	}, st)
	if err != nil {
		t.Fatalf("DecodeStreamEvent(block0 delta) error = %v", err)
	}
	if events[0].DeltaJSON != `{"file` {
		t.Errorf("block 0 DeltaJSON = %q", events[0].DeltaJSON)
	}

	// Delta for block 1
	events, err = adapter.DecodeStreamEvent(map[string]interface{}{
		"type":  "content_block_delta",
		"index": 1,
		"delta": map[string]interface{}{"type": "input_json_delta", "partial_json": `{"path`},
	}, st)
	if err != nil {
		t.Fatalf("DecodeStreamEvent(block1 delta) error = %v", err)
	}
	if events[0].DeltaJSON != `{"path` {
		t.Errorf("block 1 DeltaJSON = %q", events[0].DeltaJSON)
	}

	// Stop block 0
	events, err = adapter.DecodeStreamEvent(map[string]interface{}{
		"type": "content_block_stop", "index": 0,
	}, st)
	if err != nil {
		t.Fatalf("DecodeStreamEvent(block0 stop) error = %v", err)
	}
	if events[0].Type != IRStreamContentStop {
		t.Errorf("block 0 stop Type = %q", events[0].Type)
	}

	// Stop block 1
	events, err = adapter.DecodeStreamEvent(map[string]interface{}{
		"type": "content_block_stop", "index": 1,
	}, st)
	if err != nil {
		t.Fatalf("DecodeStreamEvent(block1 stop) error = %v", err)
	}
	if events[0].Type != IRStreamContentStop {
		t.Errorf("block 1 stop Type = %q", events[0].Type)
	}
}

func TestAnthropicStreamLifecycle(t *testing.T) {
	adapter := &anthropicMessagesAdapter{}
	st := adapter.NewStreamState()

	// Read golden file: anthropic_stream_text.jsonl
	f, err := os.Open("testdata/anthropic_stream_text.jsonl")
	if err != nil {
		t.Fatalf("open golden file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var events []*IRStreamEvent
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatalf("json.Unmarshal: %v, line=%q", err, line)
		}
		evts, err := adapter.DecodeStreamEvent(raw, st)
		if err != nil {
			t.Fatalf("DecodeStreamEvent: %v, line=%q", err, line)
		}
		events = append(events, evts...)
	}

	// Verify event sequence.
	if len(events) < 7 {
		t.Fatalf("len(events) = %d, want >= 7", len(events))
	}

	// First event: message_start
	if events[0].Type != IRStreamMessageStart {
		t.Errorf("events[0].Type = %q, want message_start", events[0].Type)
	}
	if events[0].ResponseID != "msg_ant_001" {
		t.Errorf("events[0].ResponseID = %q", events[0].ResponseID)
	}

	// Second event: content_start (text)
	if events[1].Type != IRStreamContentStart {
		t.Errorf("events[1].Type = %q, want content_part_start", events[1].Type)
	}

	// Text deltas
	gotText := ""
	for i := 2; i < 8; i++ {
		if events[i].Type != IRStreamContentDelta {
			t.Errorf("events[%d].Type = %q, want content_part_delta", i, events[i].Type)
		}
		gotText += events[i].DeltaText
	}
	if gotText != "The capital of France is Paris." {
		t.Errorf("accumulated text = %q", gotText)
	}

	// content_part_stop
	if events[8].Type != IRStreamContentStop {
		t.Errorf("events[8].Type = %q, want content_part_stop", events[8].Type)
	}

	// message_delta
	if events[9].Type != IRStreamMessageDelta {
		t.Errorf("events[9].Type = %q, want message_delta", events[9].Type)
	}
	if events[9].StopReason != IRStopEndTurn {
		t.Errorf("events[9].StopReason = %q", events[9].StopReason)
	}

	// done
	if events[10].Type != IRStreamDone {
		t.Errorf("events[10].Type = %q, want done", events[10].Type)
	}
}
