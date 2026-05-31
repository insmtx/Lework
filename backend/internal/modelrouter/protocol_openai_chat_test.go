package modelrouter

import (
	"encoding/json"
	"strings"
	"testing"
)

var testAdapter = &openAIChatAdapter{}

func parseJSON(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("parseJSON: %v\nraw: %s", err, raw)
	}
	return m
}

func floatPtr(f float64) *float64 { return &f }
func intPtr(i int) *int           { return &i }

// =============================================================================
// TestOpenAIChatDecodeRequest
// =============================================================================

func TestOpenAIChatDecodeRequest(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		raw := parseJSON(t, `{"model":"gpt-4o","messages":[{"role":"user","content":"Hello, world!"}],"temperature":0.7}`)
		ir, err := testAdapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		if ir.Model != "gpt-4o" {
			t.Errorf("Model = %q, want gpt-4o", ir.Model)
		}
		if len(ir.Messages) != 1 {
			t.Fatalf("len(Messages) = %d, want 1", len(ir.Messages))
		}
		if got := ir.Messages[0].GetTextContent(); got != "Hello, world!" {
			t.Errorf("text = %q", got)
		}
		if ir.Temperature == nil || *ir.Temperature != 0.7 {
			t.Errorf("Temperature = %v", ir.Temperature)
		}
	})

	t.Run("with_system_message", func(t *testing.T) {
		raw := parseJSON(t, `{"model":"gpt-4o","messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"Hi"}]}`)
		ir, err := testAdapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		if ir.System != "You are helpful." {
			t.Errorf("System = %q", ir.System)
		}
		if got := ir.Messages[1].GetTextContent(); got != "Hi" {
			t.Errorf("user text = %q", got)
		}
	})

	t.Run("with_developer_role_as_system", func(t *testing.T) {
		raw := parseJSON(t, `{"model":"gpt-4o","messages":[{"role":"developer","content":"You are a dev."},{"role":"user","content":"Write code"}]}`)
		ir, err := testAdapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		if ir.Messages[0].Role != IRRoleSystem {
			t.Errorf("developer role = %q, want system", ir.Messages[0].Role)
		}
		if ir.System != "You are a dev." {
			t.Errorf("System = %q", ir.System)
		}
	})

	t.Run("with_tools", func(t *testing.T) {
		raw := parseJSON(t, `{"model":"gpt-4o","messages":[{"role":"user","content":"weather?"}],"tools":[{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}],"tool_choice":"auto"}`)
		ir, err := testAdapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		if len(ir.Tools) != 1 || ir.Tools[0].Name != "get_weather" {
			t.Fatalf("Tools = %+v", ir.Tools)
		}
		if ir.ToolChoice == nil || ir.ToolChoice.Type != "auto" {
			t.Errorf("ToolChoice = %v", ir.ToolChoice)
		}
	})

	t.Run("with_tool_choice_specific", func(t *testing.T) {
		raw := parseJSON(t, `{"model":"gpt-4o","messages":[{"role":"user","content":"call fn"}],"tools":[{"type":"function","function":{"name":"my_func","parameters":{}}}],"tool_choice":{"type":"function","function":{"name":"my_func"}}}`)
		ir, err := testAdapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		if ir.ToolChoice.Type != "specific" || ir.ToolChoice.Name != "my_func" {
			t.Errorf("ToolChoice = %+v", ir.ToolChoice)
		}
	})

	t.Run("with_all_optional_params", func(t *testing.T) {
		raw := parseJSON(t, `{"model":"gpt-4o","messages":[{"role":"user","content":"test"}],"seed":42,"user":"test-user","stop":["END"],"reasoning_effort":"high","response_format":{"type":"json_schema","json_schema":{"name":"test"}},"top_p":0.9,"max_tokens":2048}`)
		ir, err := testAdapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		if ir.Seed == nil || *ir.Seed != 42 {
			t.Errorf("Seed = %v", ir.Seed)
		}
		if ir.User != "test-user" {
			t.Errorf("User = %q", ir.User)
		}
		if len(ir.Stop) != 1 || ir.Stop[0] != "END" {
			t.Errorf("Stop = %v", ir.Stop)
		}
		if ir.ReasoningEffort != "high" {
			t.Errorf("ReasoningEffort = %q", ir.ReasoningEffort)
		}
		if ir.MaxTokens != 2048 {
			t.Errorf("MaxTokens = %d", ir.MaxTokens)
		}
	})

	t.Run("with_assistant_tool_calls", func(t *testing.T) {
		raw := parseJSON(t, `{"model":"gpt-4o","messages":[{"role":"user","content":"call fn"},{"role":"assistant","content":null,"tool_calls":[{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Tokyo\"}"}}]},{"role":"tool","tool_call_id":"call_abc","content":"Sunny, 25C"}]}`)
		ir, err := testAdapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		if len(ir.Messages) != 3 {
			t.Fatalf("len(Messages) = %d", len(ir.Messages))
		}
		asst := ir.Messages[1]
		if asst.Role != IRRoleAssistant {
			t.Errorf("msg[1].Role = %q", asst.Role)
		}
		foundTC := false
		for _, p := range asst.Parts {
			if p.Type == IRPartToolCall && p.ToolCall != nil && p.ToolCall.ID == "call_abc" {
				foundTC = true
			}
		}
		if !foundTC {
			t.Error("assistant missing tool_call part")
		}
		tool := ir.Messages[2]
		if tool.Role != IRRoleTool {
			t.Errorf("msg[2].Role = %q", tool.Role)
		}
		foundTR := false
		for _, p := range tool.Parts {
			if p.Type == IRPartToolResult && p.ToolResult != nil && p.ToolResult.ToolCallID == "call_abc" {
				foundTR = true
			}
		}
		if !foundTR {
			t.Error("tool missing tool_result part")
		}
	})

	t.Run("with_content_array", func(t *testing.T) {
		raw := parseJSON(t, `{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"text","text":"What is this?"},{"type":"image_url","image_url":{"url":"https://example.com/img.png"}}]}]}`)
		ir, err := testAdapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		msg := ir.Messages[0]
		var hasText, hasImage bool
		for _, p := range msg.Parts {
			if p.Type == IRPartText {
				hasText = true
			}
			if p.Type == IRPartImage {
				hasImage = true
			}
		}
		if !hasText || !hasImage {
			t.Errorf("parts missing: text=%v image=%v", hasText, hasImage)
		}
	})

	t.Run("preserved_to_extensions", func(t *testing.T) {
		raw := parseJSON(t, `{"model":"gpt-4o","messages":[{"role":"user","content":"test"}],"frequency_penalty":0.5,"n":1}`)
		ir, err := testAdapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		ext, ok := ir.Extensions["openai_chat"]
		if !ok {
			t.Fatal("Extensions[openai_chat] missing")
		}
		if v, ok := ext["frequency_penalty"]; !ok || v != 0.5 {
			t.Errorf("frequency_penalty = %v", v)
		}
	})

	t.Run("max_completion_tokens_takes_precedence", func(t *testing.T) {
		raw := parseJSON(t, `{"model":"gpt-4o","messages":[{"role":"user","content":"test"}],"max_tokens":1024,"max_completion_tokens":2048}`)
		ir, err := testAdapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest() error = %v", err)
		}
		if ir.MaxTokens != 2048 {
			t.Errorf("MaxTokens = %d", ir.MaxTokens)
		}
	})
}

// =============================================================================
// TestOpenAIChatEncodeRequest
// =============================================================================

func TestOpenAIChatEncodeRequest(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		ir := &IRRequest{
			Model:    "gpt-4o",
			Messages: []IRMessage{{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "Hello"}}}},
		}
		body, err := testAdapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest() error = %v", err)
		}
		if getString(body, "model") != "gpt-4o" {
			t.Errorf("model = %q", getString(body, "model"))
		}
		msgs, _ := getList(body, "messages")
		if len(msgs) != 1 {
			t.Fatalf("len(messages) = %d", len(msgs))
		}
	})

	t.Run("with_system", func(t *testing.T) {
		ir := &IRRequest{
			Model:    "gpt-4o",
			System:   "You are helpful.",
			Messages: []IRMessage{{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "Hi"}}}},
		}
		body, err := testAdapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest() error = %v", err)
		}
		msgs, _ := getList(body, "messages")
		if len(msgs) != 2 {
			t.Fatalf("len(messages) = %d, want 2", len(msgs))
		}
		first, _ := msgs[0].(map[string]interface{})
		if getString(first, "role") != "system" {
			t.Errorf("first role = %q", getString(first, "role"))
		}
	})

	t.Run("with_tools", func(t *testing.T) {
		ir := &IRRequest{
			Model:      "gpt-4o",
			Messages:   []IRMessage{{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "weather"}}}},
			Tools:      []IRToolDecl{{Type: "function", Name: "get_weather"}},
			ToolChoice: &IRToolChoice{Type: "auto"},
		}
		body, err := testAdapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest() error = %v", err)
		}
		tools, _ := getList(body, "tools")
		if len(tools) != 1 {
			t.Fatalf("tools = %v", tools)
		}
		if tc := body["tool_choice"]; tc != "auto" {
			t.Errorf("tool_choice = %v", tc)
		}
	})

	t.Run("with_all_optional_params", func(t *testing.T) {
		ir := &IRRequest{
			Model:           "gpt-4o",
			Stream:          true,
			Temperature:     floatPtr(0.7),
			TopP:            floatPtr(0.9),
			MaxTokens:       2048,
			Stop:            []string{"END"},
			Seed:            intPtr(42),
			User:            "test-user",
			ReasoningEffort: "high",
			ResponseFormat:  &IRResponseFormat{Type: "json_schema", JSONSchema: map[string]interface{}{"name": "test"}},
			Messages:        []IRMessage{{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "test"}}}},
		}
		body, err := testAdapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest() error = %v", err)
		}
		if !getBool(body, "stream") {
			t.Error("stream should be true")
		}
		if v, ok := getFloat(body, "temperature"); !ok || v != 0.7 {
			t.Errorf("temperature = %v", v)
		}
		if v, ok := getInt(body, "max_completion_tokens"); !ok || v != 2048 {
			t.Errorf("max_completion_tokens = %v", v)
		}
		if getString(body, "reasoning_effort") != "high" {
			t.Errorf("reasoning_effort = %q", getString(body, "reasoning_effort"))
		}
	})

	t.Run("encode_assistant_with_tool_calls_and_tool_result", func(t *testing.T) {
		ir := &IRRequest{
			Model: "gpt-4o",
			Messages: []IRMessage{
				{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "call fn"}}},
				{
					Role: IRRoleAssistant,
					Parts: []IRContentPart{
						{Type: IRPartText, Text: "Let me call."},
						{Type: IRPartToolCall, ToolCall: &IRToolCallPart{ID: "call_123", Name: "get_weather", ArgumentsJSON: map[string]interface{}{"city": "Tokyo"}}},
					},
				},
				{
					Role: IRRoleTool,
					Parts: []IRContentPart{
						{Type: IRPartToolResult, ToolResult: &IRToolResultPart{ToolCallID: "call_123", Content: []IRContentPart{{Type: IRPartText, Text: "Sunny, 25C"}}}},
					},
				},
			},
		}
		body, err := testAdapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest() error = %v", err)
		}
		msgs, _ := getList(body, "messages")
		if len(msgs) != 3 {
			t.Fatalf("len(messages) = %d, want 3", len(msgs))
		}
		asst, _ := msgs[1].(map[string]interface{})
		if tcs, ok := getList(asst, "tool_calls"); !ok || len(tcs) != 1 {
			t.Fatalf("tool_calls missing: %v", asst)
		}
		tool, _ := msgs[2].(map[string]interface{})
		if getString(tool, "tool_call_id") != "call_123" {
			t.Errorf("tool_call_id = %q", getString(tool, "tool_call_id"))
		}
	})

	t.Run("tool_choice_specific", func(t *testing.T) {
		ir := &IRRequest{
			Model:      "gpt-4o",
			Messages:   []IRMessage{{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "test"}}}},
			ToolChoice: &IRToolChoice{Type: "specific", Name: "my_func"},
		}
		body, err := testAdapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest() error = %v", err)
		}
		tc, ok := body["tool_choice"].(map[string]interface{})
		if !ok || getString(tc, "type") != "function" {
			t.Errorf("tool_choice = %v", body["tool_choice"])
		}
	})

	t.Run("roundtrip_basic", func(t *testing.T) {
		raw := parseJSON(t, `{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}],"temperature":0.7,"max_tokens":1024}`)
		ir, err := testAdapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest: %v", err)
		}
		encoded, err := testAdapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest: %v", err)
		}
		if getString(encoded, "model") != "gpt-4o" {
			t.Errorf("model mismatch: %q", getString(encoded, "model"))
		}
		if v, ok := getFloat(encoded, "temperature"); !ok || v != 0.7 {
			t.Errorf("temperature = %v", v)
		}
	})

	t.Run("roundtrip_with_system_and_tools", func(t *testing.T) {
		raw := parseJSON(t, `{"model":"gpt-4o","messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"weather?"}],"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}],"tool_choice":"auto"}`)
		ir, err := testAdapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest: %v", err)
		}
		encoded, err := testAdapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest: %v", err)
		}
		msgs, _ := getList(encoded, "messages")
		if len(msgs) != 2 {
			t.Fatalf("len(messages) = %d", len(msgs))
		}
		first, _ := msgs[0].(map[string]interface{})
		if getString(first, "role") != "system" {
			t.Errorf("first role = %q", getString(first, "role"))
		}
		tools, _ := getList(encoded, "tools")
		if len(tools) != 1 {
			t.Errorf("tools missing or wrong count: %v", tools)
		}
	})
}

// DecodeResponse tests

func TestOpenAIChatDecodeResponse(t *testing.T) {
	t.Run("text_response", func(t *testing.T) {
		raw := parseJSON(t, `{"id":"chatcmpl-abc","model":"gpt-4o","created":1700000000,"choices":[{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
		ir, err := testAdapter.DecodeResponse(raw)
		if err != nil {
			t.Fatalf("DecodeResponse() error = %v", err)
		}
		if ir.ID != "chatcmpl-abc" {
			t.Errorf("ID = %q", ir.ID)
		}
		if len(ir.Content) == 0 || ir.Content[0].Text != "Hello!" {
			t.Errorf("Content = %v", ir.Content)
		}
		if ir.StopReason != IRStopEndTurn {
			t.Errorf("StopReason = %q", ir.StopReason)
		}
		if ir.Usage.InputTokens != 10 || ir.Usage.OutputTokens != 5 {
			t.Errorf("Usage = %+v", ir.Usage)
		}
	})

	t.Run("tool_calls_response", func(t *testing.T) {
		raw := parseJSON(t, `{"id":"chatcmpl-tool","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Tokyo\"}"}}]},"finish_reason":"tool_calls"}]}`)
		ir, err := testAdapter.DecodeResponse(raw)
		if err != nil {
			t.Fatalf("DecodeResponse() error = %v", err)
		}
		if ir.StopReason != IRStopToolUse {
			t.Errorf("StopReason = %q", ir.StopReason)
		}
		found := false
		for _, p := range ir.Content {
			if p.Type == IRPartToolCall && p.ToolCall != nil && p.ToolCall.Name == "get_weather" {
				found = true
			}
		}
		if !found {
			t.Error("no tool_call in content")
		}
	})

	t.Run("finish_reason_length", func(t *testing.T) {
		raw := parseJSON(t, `{"id":"chatcmpl-len","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"trunc"},"finish_reason":"length"}]}`)
		ir, err := testAdapter.DecodeResponse(raw)
		if err != nil {
			t.Fatalf("DecodeResponse() error = %v", err)
		}
		if ir.StopReason != IRStopMaxTokens {
			t.Errorf("StopReason = %q, want max_tokens", ir.StopReason)
		}
	})

	t.Run("finish_reason_content_filter", func(t *testing.T) {
		raw := parseJSON(t, `{"id":"chatcmpl-filter","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"content_filter"}]}`)
		ir, err := testAdapter.DecodeResponse(raw)
		if err != nil {
			t.Fatalf("DecodeResponse() error = %v", err)
		}
		if ir.StopReason != IRStopEndTurn {
			t.Errorf("StopReason = %q, want end_turn", ir.StopReason)
		}
	})

	t.Run("usage_with_details", func(t *testing.T) {
		raw := parseJSON(t, `{"id":"chatcmpl-detail","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150,"prompt_tokens_details":{"cached_tokens":80},"completion_tokens_details":{"reasoning_tokens":20}}}`)
		ir, err := testAdapter.DecodeResponse(raw)
		if err != nil {
			t.Fatalf("DecodeResponse() error = %v", err)
		}
		if ir.Usage.CacheReadInputTokens != 80 {
			t.Errorf("CacheReadInputTokens = %d", ir.Usage.CacheReadInputTokens)
		}
		if ir.Usage.ReasoningTokens != 20 {
			t.Errorf("ReasoningTokens = %d", ir.Usage.ReasoningTokens)
		}
	})
}

// EncodeResponse tests

func TestOpenAIChatEncodeResponse(t *testing.T) {
	t.Run("text_response", func(t *testing.T) {
		ir := &IRResponse{
			ID:         "chatcmpl-abc",
			Model:      "gpt-4o",
			Content:    []IRContentPart{{Type: IRPartText, Text: "Hello!"}},
			StopReason: IRStopEndTurn,
			Usage:      &IRUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		}
		body, err := testAdapter.EncodeResponse(ir)
		if err != nil {
			t.Fatalf("EncodeResponse() error = %v", err)
		}
		choices, _ := getList(body, "choices")
		if len(choices) != 1 {
			t.Fatalf("choices = %v", choices)
		}
		choice, _ := choices[0].(map[string]interface{})
		msg, _ := choice["message"].(map[string]interface{})
		if getString(msg, "content") != "Hello!" {
			t.Errorf("content = %q", getString(msg, "content"))
		}
		if getString(choice, "finish_reason") != "stop" {
			t.Errorf("finish_reason = %q", getString(choice, "finish_reason"))
		}
	})

	t.Run("tool_calls_response", func(t *testing.T) {
		ir := &IRResponse{
			ID:    "chatcmpl-tool",
			Model: "gpt-4o",
			Content: []IRContentPart{
				{Type: IRPartToolCall, ToolCall: &IRToolCallPart{ID: "call_abc", Name: "get_weather", ArgumentsJSON: map[string]interface{}{"city": "Tokyo"}}},
			},
			StopReason: IRStopToolUse,
		}
		body, err := testAdapter.EncodeResponse(ir)
		if err != nil {
			t.Fatalf("EncodeResponse() error = %v", err)
		}
		choices, _ := getList(body, "choices")
		choice, _ := choices[0].(map[string]interface{})
		if getString(choice, "finish_reason") != "tool_calls" {
			t.Errorf("finish_reason = %q", getString(choice, "finish_reason"))
		}
		msg, _ := choice["message"].(map[string]interface{})
		if tcs, ok := getList(msg, "tool_calls"); !ok || len(tcs) != 1 {
			t.Fatalf("tool_calls = %v", tcs)
		}
	})

	t.Run("finish_reason_mapping", func(t *testing.T) {
		tests := []struct {
			r    IRStopReason
			want string
		}{
			{IRStopEndTurn, "stop"},
			{IRStopToolUse, "tool_calls"},
			{IRStopMaxTokens, "length"},
			{IRStopContentFilter, "content_filter"},
			{IRStopError, "error"},
			{IRStopStopSequence, "stop"},
			{IRStopLength, "length"},
		}
		for _, tt := range tests {
			ir := &IRResponse{ID: "id", Model: "m", Content: []IRContentPart{{Type: IRPartText, Text: "x"}}, StopReason: tt.r}
			body, err := testAdapter.EncodeResponse(ir)
			if err != nil {
				t.Fatalf("EncodeResponse(%q) error = %v", tt.r, err)
			}
			choices, _ := getList(body, "choices")
			choice, _ := choices[0].(map[string]interface{})
			if got := getString(choice, "finish_reason"); got != tt.want {
				t.Errorf("finish_reason for %q = %q, want %q", tt.r, got, tt.want)
			}
		}
	})

	t.Run("id_prefix_ensured", func(t *testing.T) {
		ir := &IRResponse{ID: "abc123", Model: "gpt-4o", Content: []IRContentPart{{Type: IRPartText, Text: "ok"}}, StopReason: IRStopEndTurn}
		body, err := testAdapter.EncodeResponse(ir)
		if err != nil {
			t.Fatalf("EncodeResponse() error = %v", err)
		}
		id := getString(body, "id")
		if !strings.HasPrefix(id, "chatcmpl-") {
			t.Errorf("id = %q, want chatcmpl- prefix", id)
		}
	})

	t.Run("roundtrip", func(t *testing.T) {
		raw := parseJSON(t, `{"id":"chatcmpl-ab","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
		ir, err := testAdapter.DecodeResponse(raw)
		if err != nil {
			t.Fatalf("DecodeResponse: %v", err)
		}
		encoded, err := testAdapter.EncodeResponse(ir)
		if err != nil {
			t.Fatalf("EncodeResponse: %v", err)
		}
		choices, _ := getList(encoded, "choices")
		choice, _ := choices[0].(map[string]interface{})
		msg, _ := choice["message"].(map[string]interface{})
		if getString(msg, "content") != "Hello!" {
			t.Errorf("content = %q", getString(msg, "content"))
		}
	})
}

// DecodeStreamEvent tests

func TestOpenAIChatDecodeStreamEvent(t *testing.T) {
	t.Run("message_start", func(t *testing.T) {
		raw := parseJSON(t, `{"id":"chatcmpl-s1","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`)
		st := testAdapter.NewStreamState()
		events, err := testAdapter.DecodeStreamEvent(raw, st)
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		if len(events) == 0 || events[0].Type != IRStreamMessageStart {
			t.Fatalf("events = %v", events)
		}
		if events[0].ResponseID != "chatcmpl-s1" {
			t.Errorf("ResponseID = %q", events[0].ResponseID)
		}
	})

	t.Run("text_delta", func(t *testing.T) {
		raw := parseJSON(t, `{"id":"chatcmpl-s1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`)
		st := testAdapter.NewStreamState()
		events, err := testAdapter.DecodeStreamEvent(raw, st)
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		// Adapter emits plain text delta — StreamAggregator handles ContentStart autocomplete.
		if len(events) != 1 {
			t.Fatalf("expected 1 event (text_delta), got %d: %v", len(events), events)
		}
		if events[0].Type != IRStreamContentDelta || events[0].DeltaText != "Hello" {
			t.Errorf("events[0] = %v, want content_part_delta with 'Hello'", events[0])
		}
	})

	t.Run("tool_call_start", func(t *testing.T) {
		raw := parseJSON(t, `{"id":"chatcmpl-s2","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_001","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`)
		st := testAdapter.NewStreamState()
		events, err := testAdapter.DecodeStreamEvent(raw, st)
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		found := false
		for _, e := range events {
			if e.Type == IRStreamContentStart && e.Part != nil && e.Part.ToolCall != nil && e.Part.ToolCall.ID == "call_001" {
				found = true
			}
		}
		if !found {
			t.Errorf("events = %v, want content_part_start with call_001", events)
		}
	})

	t.Run("tool_call_delta", func(t *testing.T) {
		// Tool delta with args only (no id) — depends on ContentStart arriving first.
		// Simulate: ContentStart sets up toolStartEmitted + toolIndexToGlobalIndex.
		rawStart := parseJSON(t, `{"id":"chatcmpl-s2","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_001","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`)
		rawDelta := parseJSON(t, `{"id":"chatcmpl-s2","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\"Tokyo\"}"}}]},"finish_reason":null}]}`)

		st := testAdapter.NewStreamState()
		// First chunk: ContentStart
		startEvents, _ := testAdapter.DecodeStreamEvent(rawStart, st)
		_ = startEvents
		// Second chunk: arguments delta — should produce delta at the mapped global index
		events, err := testAdapter.DecodeStreamEvent(rawDelta, st)
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		found := false
		for _, e := range events {
			if e.Type == IRStreamContentDelta && e.DeltaJSON != "" {
				found = true
			}
		}
		if !found {
			t.Errorf("events = %v, want content_part_delta with JSON", events)
		}
	})

	t.Run("finish_reason_stop", func(t *testing.T) {
		raw := parseJSON(t, `{"id":"chatcmpl-s1","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
		st := testAdapter.NewStreamState()
		events, err := testAdapter.DecodeStreamEvent(raw, st)
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		if len(events) == 0 || events[0].Type != IRStreamMessageDelta || events[0].StopReason != IRStopEndTurn {
			t.Errorf("events = %v", events)
		}
	})

	t.Run("finish_reason_tool_calls", func(t *testing.T) {
		raw := parseJSON(t, `{"id":"chatcmpl-s2","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)
		st := testAdapter.NewStreamState()
		events, err := testAdapter.DecodeStreamEvent(raw, st)
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		if len(events) == 0 || events[0].StopReason != IRStopToolUse {
			t.Errorf("events = %v, want stop_reason=tool_use", events)
		}
	})

	t.Run("usage_only_chunk", func(t *testing.T) {
		raw := parseJSON(t, `{"id":"chatcmpl-s1","object":"chat.completion.chunk","model":"gpt-4o","usage":{"prompt_tokens":15,"completion_tokens":7,"total_tokens":22}}`)
		st := testAdapter.NewStreamState()
		events, err := testAdapter.DecodeStreamEvent(raw, st)
		if err != nil {
			t.Fatalf("DecodeStreamEvent() error = %v", err)
		}
		if len(events) == 0 || events[0].Type != IRStreamMessageDelta {
			t.Fatalf("events = %v", events)
		}
		if events[0].Usage == nil || events[0].Usage.InputTokens != 15 {
			t.Errorf("Usage = %+v", events[0].Usage)
		}
	})
}

// EncodeStreamEvent tests

func TestOpenAIChatEncodeStreamEvent(t *testing.T) {
	t.Run("message_start", func(t *testing.T) {
		ev := &IRStreamEvent{Type: IRStreamMessageStart, ResponseID: "chatcmpl-s1", ResponseModel: "gpt-4o"}
		st := testAdapter.NewStreamState()
		chunks, err := testAdapter.EncodeStreamEvent(ev, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		if len(chunks) != 1 {
			t.Fatalf("len(chunks) = %d", len(chunks))
		}
		choices, _ := getList(chunks[0], "choices")
		if len(choices) == 0 {
			t.Fatal("no choices")
		}
		choice, _ := choices[0].(map[string]interface{})
		delta, _ := choice["delta"].(map[string]interface{})
		if getString(delta, "role") != "assistant" {
			t.Errorf("role = %q", getString(delta, "role"))
		}
	})

	t.Run("text_delta", func(t *testing.T) {
		ev := &IRStreamEvent{Type: IRStreamContentDelta, DeltaText: "Hello", Index: 0}
		st := testAdapter.NewStreamState()
		chunks, err := testAdapter.EncodeStreamEvent(ev, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		if len(chunks) != 1 {
			t.Fatalf("len(chunks) = %d", len(chunks))
		}
		choices, _ := getList(chunks[0], "choices")
		choice, _ := choices[0].(map[string]interface{})
		delta, _ := choice["delta"].(map[string]interface{})
		if getString(delta, "content") != "Hello" {
			t.Errorf("content = %q", getString(delta, "content"))
		}
	})

	t.Run("tool_call_start", func(t *testing.T) {
		ev := &IRStreamEvent{
			Type:  IRStreamContentStart,
			Index: 0,
			Part:  &IRContentPart{Type: IRPartToolCall, ToolCall: &IRToolCallPart{ID: "call_001", Name: "get_weather"}},
		}
		st := testAdapter.NewStreamState()
		chunks, err := testAdapter.EncodeStreamEvent(ev, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		if len(chunks) != 1 {
			t.Fatalf("len(chunks) = %d", len(chunks))
		}
		choices, _ := getList(chunks[0], "choices")
		choice, _ := choices[0].(map[string]interface{})
		delta, _ := choice["delta"].(map[string]interface{})
		tcs, _ := getList(delta, "tool_calls")
		if len(tcs) != 1 {
			t.Fatalf("tool_calls = %v", tcs)
		}
		tc, _ := tcs[0].(map[string]interface{})
		if getString(tc, "id") != "call_001" {
			t.Errorf("id = %q", getString(tc, "id"))
		}
	})

	t.Run("tool_call_delta", func(t *testing.T) {
		ev := &IRStreamEvent{Type: IRStreamContentDelta, DeltaJSON: `{"city":"Tokyo"}`, Index: 0}
		st := testAdapter.NewStreamState()
		chunks, err := testAdapter.EncodeStreamEvent(ev, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		if len(chunks) != 1 {
			t.Fatalf("len(chunks) = %d", len(chunks))
		}
		choices, _ := getList(chunks[0], "choices")
		choice, _ := choices[0].(map[string]interface{})
		delta, _ := choice["delta"].(map[string]interface{})
		tcs, _ := getList(delta, "tool_calls")
		if len(tcs) != 1 {
			t.Fatalf("tool_calls = %v", tcs)
		}
		tc, _ := tcs[0].(map[string]interface{})
		fn, _ := tc["function"].(map[string]interface{})
		if getString(fn, "arguments") != `{"city":"Tokyo"}` {
			t.Errorf("arguments = %q", getString(fn, "arguments"))
		}
	})

	t.Run("message_delta_stop", func(t *testing.T) {
		ev := &IRStreamEvent{Type: IRStreamMessageDelta, StopReason: IRStopEndTurn}
		st := testAdapter.NewStreamState()
		chunks, err := testAdapter.EncodeStreamEvent(ev, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		if len(chunks) != 1 {
			t.Fatalf("len(chunks) = %d", len(chunks))
		}
		choices, _ := getList(chunks[0], "choices")
		choice, _ := choices[0].(map[string]interface{})
		if getString(choice, "finish_reason") != "stop" {
			t.Errorf("finish_reason = %q", getString(choice, "finish_reason"))
		}
	})

	t.Run("message_delta_with_usage", func(t *testing.T) {
		ev := &IRStreamEvent{Type: IRStreamMessageDelta, Usage: &IRUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}}
		st := testAdapter.NewStreamState()
		chunks, err := testAdapter.EncodeStreamEvent(ev, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		if len(chunks) != 1 {
			t.Fatalf("len(chunks) = %d", len(chunks))
		}
		usage, _ := chunks[0]["usage"].(map[string]interface{})
		if getIntDefault(usage, "prompt_tokens") != 10 {
			t.Errorf("usage.prompt_tokens = %d", getIntDefault(usage, "prompt_tokens"))
		}
	})

	t.Run("error", func(t *testing.T) {
		ev := &IRStreamEvent{Type: IRStreamError, ErrorMessage: "something went wrong", ErrorType: "server_error"}
		st := testAdapter.NewStreamState()
		chunks, err := testAdapter.EncodeStreamEvent(ev, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		if len(chunks) != 1 {
			t.Fatalf("len(chunks) = %d", len(chunks))
		}
		choices, _ := getList(chunks[0], "choices")
		choice, _ := choices[0].(map[string]interface{})
		if getString(choice, "finish_reason") != "error" {
			t.Errorf("finish_reason = %q", getString(choice, "finish_reason"))
		}
	})

	t.Run("done_returns_nil", func(t *testing.T) {
		ev := &IRStreamEvent{Type: IRStreamDone}
		st := testAdapter.NewStreamState()
		chunks, err := testAdapter.EncodeStreamEvent(ev, st)
		if err != nil {
			t.Fatalf("EncodeStreamEvent() error = %v", err)
		}
		if chunks != nil {
			t.Errorf("chunks = %v, want nil", chunks)
		}
	})
}

// TestOpenAIChatStreamLifecycle reads the golden stream files and simulates
// a full streaming lifecycle, decoding each chunk and verifying event types.

func TestOpenAIChatStreamLifecycle(t *testing.T) {
	t.Run("text_stream", func(t *testing.T) {
		lines := []string{
			`{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"content":"The"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"content":" capital"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"content":" of"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"content":" France"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"content":" is"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"content":" Paris."},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":15,"completion_tokens":7,"total_tokens":22}}`,
		}

		st := testAdapter.NewStreamState()
		var gotMessageStart, gotFinish bool
		var allText string

		for _, line := range lines {
			raw := parseJSON(t, line)
			events, err := testAdapter.DecodeStreamEvent(raw, st)
			if err != nil {
				t.Fatalf("DecodeStreamEvent error: %v", err)
			}
			for _, ev := range events {
				switch ev.Type {
				case IRStreamMessageStart:
					gotMessageStart = true
				case IRStreamContentDelta:
					allText += ev.DeltaText
				case IRStreamMessageDelta:
					gotFinish = true
				}
			}
		}

		if !gotMessageStart {
			t.Error("missing message_start event")
		}
		if !gotFinish {
			t.Error("missing finish event")
		}
		if allText != "The capital of France is Paris." {
			t.Errorf("text = %q", allText)
		}
	})

	t.Run("tool_stream", func(t *testing.T) {
		lines := []string{
			`{"id":"chatcmpl-stream-002","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-002","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_getWeather_001","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-002","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"c"}}]},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-002","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ity\":"}}]},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-002","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Tokyo\"}"}}]},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-002","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":40,"completion_tokens":20,"total_tokens":60}}`,
		}

		st := testAdapter.NewStreamState()
		var gotMessageStart, gotToolStart, gotFinish bool
		var toolID, toolName string
		var allArgs string

		for _, line := range lines {
			raw := parseJSON(t, line)
			events, err := testAdapter.DecodeStreamEvent(raw, st)
			if err != nil {
				t.Fatalf("DecodeStreamEvent error: %v", err)
			}
			for _, ev := range events {
				switch ev.Type {
				case IRStreamMessageStart:
					gotMessageStart = true
				case IRStreamContentStart:
					if ev.Part != nil && ev.Part.ToolCall != nil {
						gotToolStart = true
						toolID = ev.Part.ToolCall.ID
						toolName = ev.Part.ToolCall.Name
					}
				case IRStreamContentDelta:
					allArgs += ev.DeltaJSON
				case IRStreamMessageDelta:
					gotFinish = true
				}
			}
		}

		if !gotMessageStart {
			t.Error("missing message_start")
		}
		if !gotToolStart {
			t.Error("missing tool start")
		}
		if toolID != "call_getWeather_001" {
			t.Errorf("toolID = %q", toolID)
		}
		if toolName != "get_weather" {
			t.Errorf("toolName = %q", toolName)
		}
		if allArgs != `{"city":"Tokyo"}` {
			t.Errorf("args = %q", allArgs)
		}
		if !gotFinish {
			t.Error("missing finish")
		}
	})
}

// TestOpenAIChatRequestRoundTrip verifies a full decode-then-encode cycle
// using the golden chat_to_anthropic_request.json structure (Anthropic format
// is used by the golden file; we simulate a chat request with similar structure).

func TestOpenAIChatRequestRoundTrip(t *testing.T) {
	raw := parseJSON(t, `{
		"model": "gpt-4o",
		"max_tokens": 4096,
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "What is the weather in Tokyo?"}
		],
		"tools": [
			{
				"type": "function",
				"function": {
					"name": "get_weather",
					"description": "Get current weather for a city",
					"parameters": {
						"type": "object",
						"properties": {"city": {"type": "string", "description": "City name"}},
						"required": ["city"]
					}
				}
			}
		],
		"temperature": 0.5
	}`)

	ir, err := testAdapter.DecodeRequest(raw)
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}

	if ir.Model != "gpt-4o" {
		t.Errorf("Model = %q", ir.Model)
	}
	if ir.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d", ir.MaxTokens)
	}
	if ir.System != "You are a helpful assistant." {
		t.Errorf("System = %q", ir.System)
	}
	if len(ir.Tools) != 1 || ir.Tools[0].Name != "get_weather" {
		t.Errorf("Tools = %+v", ir.Tools)
	}
	if ir.Temperature == nil || *ir.Temperature != 0.5 {
		t.Errorf("Temperature = %v", ir.Temperature)
	}
	if len(ir.Messages) != 2 {
		t.Fatalf("len(Messages) = %d", len(ir.Messages))
	}

	// Encode back
	encoded, err := testAdapter.EncodeRequest(ir)
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}

	if getString(encoded, "model") != "gpt-4o" {
		t.Errorf("encoded model = %q", getString(encoded, "model"))
	}

	msgs, _ := getList(encoded, "messages")
	if len(msgs) != 2 {
		t.Fatalf("encoded len(messages) = %d, want 2", len(msgs))
	}

	first, _ := msgs[0].(map[string]interface{})
	if getString(first, "role") != "system" {
		t.Errorf("first role = %q", getString(first, "role"))
	}

	tools, _ := getList(encoded, "tools")
	if len(tools) != 1 {
		t.Fatalf("encoded tools = %v", tools)
	}

	if v, ok := getFloat(encoded, "temperature"); !ok || v != 0.5 {
		t.Errorf("encoded temperature = %v", v)
	}

	if v, ok := getInt(encoded, "max_completion_tokens"); !ok || v != 4096 {
		t.Errorf("encoded max_completion_tokens = %v", v)
	}
}
