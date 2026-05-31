package modelrouter

import (
	"encoding/json"
	"testing"
)

func newAdapter() *openAIResponsesAdapter {
	return &openAIResponsesAdapter{}
}

// =============================================================================
// Protocol
// =============================================================================

func TestOpenAIResponsesProtocol(t *testing.T) {
	a := newAdapter()
	if got := a.Protocol(); got != ProtocolOpenAIResponses {
		t.Errorf("Protocol() = %q, want %q", got, ProtocolOpenAIResponses)
	}
}

// =============================================================================
// DecodeRequest
// =============================================================================

func TestOpenAIResponsesDecodeRequest_StringInput(t *testing.T) {
	a := newAdapter()
	ir, err := a.DecodeRequest(map[string]interface{}{
		"model": "gpt-5",
		"input": "What is the capital of France?",
	})
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if ir.Model != "gpt-5" {
		t.Errorf("Model = %q, want %q", ir.Model, "gpt-5")
	}
	if len(ir.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(ir.Messages))
	}
	if ir.Messages[0].Role != IRRoleUser {
		t.Errorf("Messages[0].Role = %q, want %q", ir.Messages[0].Role, IRRoleUser)
	}
	if len(ir.Messages[0].Parts) != 1 || ir.Messages[0].Parts[0].Type != IRPartText {
		t.Fatalf("Messages[0].Parts = %+v, want one text part", ir.Messages[0].Parts)
	}
	if ir.Messages[0].Parts[0].Text != "What is the capital of France?" {
		t.Errorf("Text = %q", ir.Messages[0].Parts[0].Text)
	}
}

func TestOpenAIResponsesDecodeRequest_ArrayInput(t *testing.T) {
	a := newAdapter()
	ir, err := a.DecodeRequest(map[string]interface{}{
		"model": "gpt-5",
		"input": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_text", "text": "Hello world"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if len(ir.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(ir.Messages))
	}
	if len(ir.Messages[0].Parts) != 1 || ir.Messages[0].Parts[0].Text != "Hello world" {
		t.Errorf("Parts = %+v", ir.Messages[0].Parts)
	}
}

func TestOpenAIResponsesDecodeRequest_FunctionCallItems(t *testing.T) {
	a := newAdapter()
	ir, err := a.DecodeRequest(map[string]interface{}{
		"model": "gpt-5",
		"input": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_text", "text": "What is the weather?"},
				},
			},
			map[string]interface{}{
				"type":      "function_call",
				"call_id":   "call_abc",
				"name":      "get_weather",
				"arguments": "{\"city\":\"Tokyo\"}",
				"status":    "completed",
			},
			map[string]interface{}{
				"type":    "function_call_output",
				"call_id": "call_abc",
				"output":  "Sunny, 22C",
			},
		},
	})
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if len(ir.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(ir.Messages))
	}

	// First: user message
	if ir.Messages[0].Role != IRRoleUser {
		t.Errorf("Messages[0].Role = %q", ir.Messages[0].Role)
	}

	// Second: function_call -> assistant message
	if ir.Messages[1].Role != IRRoleAssistant {
		t.Fatalf("Messages[1].Role = %q, want assistant", ir.Messages[1].Role)
	}
	parts := ir.Messages[1].Parts
	if len(parts) != 1 || parts[0].Type != IRPartToolCall {
		t.Fatalf("Messages[1].Parts[0] = %+v, want tool_call", parts)
	}
	if parts[0].ToolCall == nil {
		t.Fatal("ToolCall is nil")
	}
	if parts[0].ToolCall.ID != "call_abc" {
		t.Errorf("ToolCall.ID = %q", parts[0].ToolCall.ID)
	}
	if parts[0].ToolCall.Name != "get_weather" {
		t.Errorf("ToolCall.Name = %q", parts[0].ToolCall.Name)
	}
	if parts[0].ToolCall.ArgumentsRaw != "{\"city\":\"Tokyo\"}" {
		t.Errorf("ToolCall.ArgumentsRaw = %q", parts[0].ToolCall.ArgumentsRaw)
	}
	if city, ok := parts[0].ToolCall.ArgumentsJSON["city"]; !ok || city != "Tokyo" {
		t.Errorf("ToolCall.ArgumentsJSON = %v", parts[0].ToolCall.ArgumentsJSON)
	}
	if parts[0].ToolCall.Status != "completed" {
		t.Errorf("ToolCall.Status = %q", parts[0].ToolCall.Status)
	}

	// Third: function_call_output -> tool message
	if ir.Messages[2].Role != IRRoleTool {
		t.Fatalf("Messages[2].Role = %q, want tool", ir.Messages[2].Role)
	}
	parts2 := ir.Messages[2].Parts
	if len(parts2) != 1 || parts2[0].Type != IRPartToolResult {
		t.Fatalf("Messages[2].Parts[0] = %+v, want tool_result", parts2)
	}
	if parts2[0].ToolResult.ToolCallID != "call_abc" {
		t.Errorf("ToolResult.ToolCallID = %q", parts2[0].ToolResult.ToolCallID)
	}
	if len(parts2[0].ToolResult.Content) != 1 || parts2[0].ToolResult.Content[0].Text != "Sunny, 22C" {
		t.Errorf("ToolResult.Content = %+v", parts2[0].ToolResult.Content)
	}
}

func TestOpenAIResponsesDecodeRequest_Reasoning(t *testing.T) {
	a := newAdapter()
	ir, err := a.DecodeRequest(map[string]interface{}{
		"model": "gpt-5",
		"input": []interface{}{
			map[string]interface{}{
				"type":    "reasoning",
				"summary": "Let me think about this...",
			},
		},
	})
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if len(ir.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(ir.Messages))
	}
	if ir.Messages[0].Role != IRRoleAssistant {
		t.Errorf("Messages[0].Role = %q, want assistant", ir.Messages[0].Role)
	}
	parts := ir.Messages[0].Parts
	if len(parts) != 1 || parts[0].Type != IRPartReasoning {
		t.Fatalf("Parts[0] = %+v, want reasoning", parts[0])
	}
	if parts[0].Reasoning.Content != "Let me think about this..." {
		t.Errorf("Reasoning.Content = %q", parts[0].Reasoning.Content)
	}
}

func TestOpenAIResponsesDecodeRequest_InstructionsAndSystem(t *testing.T) {
	a := newAdapter()
	ir, err := a.DecodeRequest(map[string]interface{}{
		"model":        "gpt-5",
		"instructions": "You are a helpful assistant.",
		"input":        "Hello",
	})
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if ir.Instructions != "You are a helpful assistant." {
		t.Errorf("Instructions = %q", ir.Instructions)
	}
	if ir.System != ir.Instructions {
		t.Errorf("System = %q, want same as Instructions", ir.System)
	}
}

func TestOpenAIResponsesDecodeRequest_Tools(t *testing.T) {
	a := newAdapter()
	ir, err := a.DecodeRequest(map[string]interface{}{
		"model": "gpt-5",
		"input": "Search projects",
		"tools": []interface{}{
			map[string]interface{}{
				"type":        "function",
				"name":        "search_project",
				"description": "Search project records",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{"type": "string"},
					},
					"required": []interface{}{"query"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if len(ir.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(ir.Tools))
	}
	if ir.Tools[0].Name != "search_project" {
		t.Errorf("Tools[0].Name = %q", ir.Tools[0].Name)
	}
	if ir.Tools[0].Type != "function" {
		t.Errorf("Tools[0].Type = %q", ir.Tools[0].Type)
	}
}

func TestOpenAIResponsesDecodeRequest_ToolChoice(t *testing.T) {
	a := newAdapter()

	// String choice
	ir, _ := a.DecodeRequest(map[string]interface{}{
		"model":       "gpt-5",
		"input":       "test",
		"tool_choice": "required",
	})
	if ir.ToolChoice == nil || ir.ToolChoice.Type != "required" {
		t.Errorf("ToolChoice = %+v, want {required}", ir.ToolChoice)
	}

	// Object choice
	ir2, _ := a.DecodeRequest(map[string]interface{}{
		"model": "gpt-5",
		"input": "test",
		"tool_choice": map[string]interface{}{
			"type": "function",
			"name": "search_project",
		},
	})
	if ir2.ToolChoice == nil || ir2.ToolChoice.Type != "specific" {
		t.Errorf("ToolChoice.Type = %q, want specific", ir2.ToolChoice.Type)
	}
	if ir2.ToolChoice.Name != "search_project" {
		t.Errorf("ToolChoice.Name = %q", ir2.ToolChoice.Name)
	}
}

func TestOpenAIResponsesDecodeRequest_ReasoningEffort(t *testing.T) {
	a := newAdapter()

	// Direct field
	ir, _ := a.DecodeRequest(map[string]interface{}{
		"model":            "gpt-5",
		"input":            "test",
		"reasoning_effort": "high",
	})
	if ir.ReasoningEffort != "high" {
		t.Errorf("ReasoningEffort = %q, want high", ir.ReasoningEffort)
	}

	// Nested reasoning.effort
	ir2, _ := a.DecodeRequest(map[string]interface{}{
		"model": "gpt-5",
		"input": "test",
		"reasoning": map[string]interface{}{
			"effort": "medium",
		},
	})
	if ir2.ReasoningEffort != "medium" {
		t.Errorf("ReasoningEffort = %q, want medium", ir2.ReasoningEffort)
	}
}

func TestOpenAIResponsesDecodeRequest_ScalarParams(t *testing.T) {
	a := newAdapter()
	ir, err := a.DecodeRequest(map[string]interface{}{
		"model":             "gpt-5",
		"input":             "test",
		"temperature":       0.7,
		"top_p":             0.9,
		"max_output_tokens": float64(1000),
		"stop":              []interface{}{"STOP"},
		"seed":              float64(42),
		"user":              "testuser",
	})
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if ir.Temperature == nil || *ir.Temperature != 0.7 {
		t.Errorf("Temperature = %v", ir.Temperature)
	}
	if ir.TopP == nil || *ir.TopP != 0.9 {
		t.Errorf("TopP = %v", ir.TopP)
	}
	if ir.MaxTokens != 1000 {
		t.Errorf("MaxTokens = %d", ir.MaxTokens)
	}
	if len(ir.Stop) != 1 || ir.Stop[0] != "STOP" {
		t.Errorf("Stop = %v", ir.Stop)
	}
	if ir.Seed == nil || *ir.Seed != 42 {
		t.Errorf("Seed = %v", ir.Seed)
	}
	if ir.User != "testuser" {
		t.Errorf("User = %q", ir.User)
	}
}

// =============================================================================
// EncodeRequest
// =============================================================================

func TestOpenAIResponsesEncodeRequest_Simple(t *testing.T) {
	a := newAdapter()
	body, err := a.EncodeRequest(&IRRequest{
		Model: "gpt-5",
		Messages: []IRMessage{
			{
				Role:  IRRoleUser,
				Parts: []IRContentPart{{Type: IRPartText, Text: "Hello"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("EncodeRequest() error = %v", err)
	}
	if body["model"] != "gpt-5" {
		t.Errorf("model = %v", body["model"])
	}
	// Single user text-only should be string input
	if s, ok := body["input"].(string); !ok || s != "Hello" {
		t.Errorf("input = %v (type=%T), want string 'Hello'", body["input"], body["input"])
	}
}

func TestOpenAIResponsesEncodeRequest_InstructionsVsSystem(t *testing.T) {
	a := newAdapter()

	// Instructions should take priority
	body, err := a.EncodeRequest(&IRRequest{
		Model:        "gpt-5",
		Instructions: "Be helpful",
		System:       "Be safe",
		Messages: []IRMessage{
			{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "Hi"}}},
		},
	})
	if err != nil {
		t.Fatalf("EncodeRequest() error = %v", err)
	}
	if body["instructions"] != "Be helpful" {
		t.Errorf("instructions = %v", body["instructions"])
	}

	// System should be used if Instructions is empty
	body2, _ := a.EncodeRequest(&IRRequest{
		Model:  "gpt-5",
		System: "Be safe",
		Messages: []IRMessage{
			{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "Hi"}}},
		},
	})
	if body2["instructions"] != "Be safe" {
		t.Errorf("instructions = %v", body2["instructions"])
	}
}

func TestOpenAIResponsesEncodeRequest_WithTools(t *testing.T) {
	a := newAdapter()
	body, err := a.EncodeRequest(&IRRequest{
		Model: "gpt-5",
		Messages: []IRMessage{
			{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "Search"}}},
		},
		Tools: []IRToolDecl{
			{
				Type:        "function",
				Name:        "search",
				Description: "Search things",
				Parameters:  map[string]interface{}{"type": "object"},
			},
		},
		ToolChoice: &IRToolChoice{Type: "required"},
	})
	if err != nil {
		t.Fatalf("EncodeRequest() error = %v", err)
	}
	tools, ok := body["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", body["tools"])
	}
	tool, ok := tools[0].(map[string]interface{})
	if !ok {
		t.Fatalf("tools[0] = %T, want map", tools[0])
	}
	if tool["name"] != "search" {
		t.Errorf("tools[0].name = %v", tool["name"])
	}
	if body["tool_choice"] != "required" {
		t.Errorf("tool_choice = %v", body["tool_choice"])
	}
}

func TestOpenAIResponsesEncodeRequest_WithMaxTokensAndStream(t *testing.T) {
	a := newAdapter()
	body, err := a.EncodeRequest(&IRRequest{
		Model:     "gpt-5",
		Stream:    true,
		MaxTokens: 2000,
		Messages: []IRMessage{
			{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "Hi"}}},
		},
	})
	if err != nil {
		t.Fatalf("EncodeRequest() error = %v", err)
	}
	if body["stream"] != true {
		t.Errorf("stream = %v", body["stream"])
	}
	if body["max_output_tokens"] != 2000 {
		t.Errorf("max_output_tokens = %v", body["max_output_tokens"])
	}
}

func TestOpenAIResponsesEncodeRequest_RoundTrip(t *testing.T) {
	a := newAdapter()
	temp := 0.7
	seed := 42

	orig := &IRRequest{
		Model:        "gpt-5",
		Instructions: "Be concise",
		Temperature:  &temp,
		MaxTokens:    1000,
		Seed:         &seed,
		User:         "test",
		Messages: []IRMessage{
			{
				Role: IRRoleUser,
				Parts: []IRContentPart{
					{Type: IRPartText, Text: "Hello"},
				},
			},
		},
		Tools: []IRToolDecl{
			{Type: "function", Name: "search", Description: "Search"},
		},
	}

	body, err := a.EncodeRequest(orig)
	if err != nil {
		t.Fatalf("EncodeRequest() error = %v", err)
	}

	decoded, err := a.DecodeRequest(body)
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}

	if decoded.Model != orig.Model {
		t.Errorf("Model = %q, want %q", decoded.Model, orig.Model)
	}
	if decoded.Instructions != orig.Instructions {
		t.Errorf("Instructions = %q, want %q", decoded.Instructions, orig.Instructions)
	}
	if decoded.MaxTokens != orig.MaxTokens {
		t.Errorf("MaxTokens = %d, want %d", decoded.MaxTokens, orig.MaxTokens)
	}
	if decoded.User != orig.User {
		t.Errorf("User = %q, want %q", decoded.User, orig.User)
	}
	if decoded.Seed == nil || *decoded.Seed != *orig.Seed {
		t.Errorf("Seed = %v", decoded.Seed)
	}
	if len(decoded.Tools) != len(orig.Tools) {
		t.Errorf("len(Tools) = %d, want %d", len(decoded.Tools), len(orig.Tools))
	}
	if len(decoded.Messages) != len(orig.Messages) {
		t.Fatalf("len(Messages) = %d, want %d", len(decoded.Messages), len(orig.Messages))
	}
}

// =============================================================================
// DecodeResponse
// =============================================================================

func TestOpenAIResponsesDecodeResponse_TextOutput(t *testing.T) {
	a := newAdapter()
	ir, err := a.DecodeResponse(map[string]interface{}{
		"id":         "resp_001",
		"object":     "response",
		"model":      "gpt-5",
		"created_at": float64(1700000000),
		"status":     "completed",
		"output": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{"type": "output_text", "text": "Hello, world!"},
				},
			},
		},
		"usage": map[string]interface{}{
			"input_tokens":  float64(10),
			"output_tokens": float64(20),
			"total_tokens":  float64(30),
		},
	})
	if err != nil {
		t.Fatalf("DecodeResponse() error = %v", err)
	}
	if ir.ID != "resp_001" {
		t.Errorf("ID = %q", ir.ID)
	}
	if ir.Model != "gpt-5" {
		t.Errorf("Model = %q", ir.Model)
	}
	if ir.Created != 1700000000 {
		t.Errorf("Created = %d", ir.Created)
	}
	if ir.StopReason != IRStopEndTurn {
		t.Errorf("StopReason = %q, want %q", ir.StopReason, IRStopEndTurn)
	}
	if len(ir.Content) != 1 || ir.Content[0].Type != IRPartText || ir.Content[0].Text != "Hello, world!" {
		t.Errorf("Content = %+v", ir.Content)
	}
	if ir.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if ir.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d", ir.Usage.InputTokens)
	}
	if ir.Usage.OutputTokens != 20 {
		t.Errorf("OutputTokens = %d", ir.Usage.OutputTokens)
	}
	if ir.Usage.TotalTokens != 30 {
		t.Errorf("TotalTokens = %d", ir.Usage.TotalTokens)
	}
}

func TestOpenAIResponsesDecodeResponse_FunctionCall(t *testing.T) {
	a := newAdapter()
	ir, err := a.DecodeResponse(map[string]interface{}{
		"id":     "resp_002",
		"model":  "gpt-5",
		"status": "completed",
		"output": []interface{}{
			map[string]interface{}{
				"type":      "function_call",
				"id":        "fc_001",
				"call_id":   "call_001",
				"name":      "get_weather",
				"arguments": "{\"city\":\"Paris\"}",
				"status":    "completed",
			},
		},
	})
	if err != nil {
		t.Fatalf("DecodeResponse() error = %v", err)
	}
	if ir.StopReason != IRStopToolUse {
		t.Errorf("StopReason = %q, want %q", ir.StopReason, IRStopToolUse)
	}
	if len(ir.Content) != 1 || ir.Content[0].Type != IRPartToolCall {
		t.Fatalf("Content = %+v", ir.Content)
	}
	tc := ir.Content[0].ToolCall
	if tc == nil {
		t.Fatal("ToolCall is nil")
	}
	if tc.ID != "call_001" {
		t.Errorf("ToolCall.ID = %q", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("ToolCall.Name = %q", tc.Name)
	}
	if tc.ArgumentsRaw != "{\"city\":\"Paris\"}" {
		t.Errorf("ToolCall.ArgumentsRaw = %q", tc.ArgumentsRaw)
	}
}

func TestOpenAIResponsesDecodeResponse_StatusMapping(t *testing.T) {
	a := newAdapter()

	tests := []struct {
		status string
		want   IRStopReason
	}{
		{"completed", IRStopEndTurn},
		{"incomplete", IRStopMaxTokens},
		{"failed", IRStopError},
		{"", IRStopEndTurn},
	}

	for _, tt := range tests {
		ir, _ := a.DecodeResponse(map[string]interface{}{
			"id":     "test",
			"model":  "gpt-5",
			"status": tt.status,
			"output": []interface{}{},
		})
		if ir.StopReason != tt.want {
			t.Errorf("status=%q -> StopReason=%q, want %q", tt.status, ir.StopReason, tt.want)
		}
	}
}

func TestOpenAIResponsesDecodeResponse_UsageWithDetails(t *testing.T) {
	a := newAdapter()
	ir, err := a.DecodeResponse(map[string]interface{}{
		"id":     "resp_003",
		"model":  "gpt-5",
		"status": "completed",
		"output": []interface{}{},
		"usage": map[string]interface{}{
			"input_tokens":  float64(50),
			"output_tokens": float64(100),
			"total_tokens":  float64(150),
			"prompt_tokens_details": map[string]interface{}{
				"cached_tokens": float64(10),
			},
			"completion_tokens_details": map[string]interface{}{
				"reasoning_tokens": float64(5),
			},
		},
	})
	if err != nil {
		t.Fatalf("DecodeResponse() error = %v", err)
	}
	if ir.Usage.CacheReadInputTokens != 10 {
		t.Errorf("CacheReadInputTokens = %d", ir.Usage.CacheReadInputTokens)
	}
	if ir.Usage.ReasoningTokens != 5 {
		t.Errorf("ReasoningTokens = %d", ir.Usage.ReasoningTokens)
	}
}

// =============================================================================
// EncodeResponse
// =============================================================================

func TestOpenAIResponsesEncodeResponse_TextOnly(t *testing.T) {
	a := newAdapter()
	body, err := a.EncodeResponse(&IRResponse{
		ID:      "resp_test",
		Model:   "gpt-5",
		Created: 1700000000,
		Content: []IRContentPart{
			{Type: IRPartText, Text: "Hello, world!"},
		},
		StopReason: IRStopEndTurn,
	})
	if err != nil {
		t.Fatalf("EncodeResponse() error = %v", err)
	}
	if body["status"] != "completed" {
		t.Errorf("status = %v", body["status"])
	}
	outItems, ok := body["output"].([]interface{})
	if !ok || len(outItems) != 1 {
		t.Fatalf("output = %#v", body["output"])
	}
	outItem, ok := outItems[0].(map[string]interface{})
	if !ok {
		t.Fatalf("output[0] = %T, want map", outItems[0])
	}
	if outItem["type"] != "message" || outItem["role"] != "assistant" {
		t.Errorf("output[0] = %v", outItem)
	}
	content, ok := outItem["content"].([]map[string]interface{})
	if !ok || len(content) != 1 || content[0]["text"] != "Hello, world!" {
		t.Errorf("content = %v", outItem["content"])
	}
}

func TestOpenAIResponsesEncodeResponse_FunctionCall(t *testing.T) {
	a := newAdapter()
	body, err := a.EncodeResponse(&IRResponse{
		ID:      "resp_test",
		Model:   "gpt-5",
		Created: 1700000000,
		Content: []IRContentPart{
			{
				Type: IRPartToolCall,
				ID:   "call_001",
				ToolCall: &IRToolCallPart{
					ID:            "call_001",
					Name:          "get_weather",
					ArgumentsRaw:  "{\"city\":\"Paris\"}",
					ArgumentsJSON: map[string]interface{}{"city": "Paris"},
					Status:        "completed",
				},
			},
		},
		StopReason: IRStopToolUse,
	})
	if err != nil {
		t.Fatalf("EncodeResponse() error = %v", err)
	}
	outItems2, ok := body["output"].([]interface{})
	if !ok || len(outItems2) != 1 {
		t.Fatalf("output = %#v", body["output"])
	}
	outItem2, ok := outItems2[0].(map[string]interface{})
	if !ok {
		t.Fatalf("output[0] = %T, want map", outItems2[0])
	}
	if outItem2["type"] != "function_call" {
		t.Errorf("output[0].type = %v", outItem2["type"])
	}
	if outItem2["name"] != "get_weather" {
		t.Errorf("output[0].name = %v", outItem2["name"])
	}
	if outItem2["arguments"] != "{\"city\":\"Paris\"}" {
		t.Errorf("output[0].arguments = %v", outItem2["arguments"])
	}
}

func TestOpenAIResponsesEncodeResponse_RoundTrip(t *testing.T) {
	a := newAdapter()
	orig := &IRResponse{
		ID:      "resp_001",
		Model:   "gpt-5",
		Created: 1700000000,
		Content: []IRContentPart{
			{Type: IRPartText, Text: "The answer is 42."},
		},
		Usage: &IRUsage{
			InputTokens:       10,
			OutputTokens:      5,
			TotalTokens:       15,
			ReasoningTokens:   2,
			CacheReadInputTokens: 3,
		},
		StopReason: IRStopEndTurn,
	}

	body, err := a.EncodeResponse(orig)
	if err != nil {
		t.Fatalf("EncodeResponse() error = %v", err)
	}

	decoded, err := a.DecodeResponse(body)
	if err != nil {
		t.Fatalf("DecodeResponse() error = %v", err)
	}

	if decoded.ID != orig.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, orig.ID)
	}
	if decoded.Model != orig.Model {
		t.Errorf("Model = %q, want %q", decoded.Model, orig.Model)
	}
	if decoded.StopReason != orig.StopReason {
		t.Errorf("StopReason = %q, want %q", decoded.StopReason, orig.StopReason)
	}
	if len(decoded.Content) != 1 || decoded.Content[0].Text != "The answer is 42." {
		t.Errorf("Content = %+v", decoded.Content)
	}
	if decoded.Usage.InputTokens != orig.Usage.InputTokens {
		t.Errorf("InputTokens = %d", decoded.Usage.InputTokens)
	}
	if decoded.Usage.OutputTokens != orig.Usage.OutputTokens {
		t.Errorf("OutputTokens = %d", decoded.Usage.OutputTokens)
	}
	if decoded.Usage.ReasoningTokens != orig.Usage.ReasoningTokens {
		t.Errorf("ReasoningTokens = %d", decoded.Usage.ReasoningTokens)
	}
}

func TestOpenAIResponsesEncodeResponse_StatusMapping(t *testing.T) {
	a := newAdapter()
	tests := []struct {
		reason IRStopReason
		want   string
	}{
		{IRStopEndTurn, "completed"},
		{IRStopMaxTokens, "incomplete"},
		{IRStopError, "failed"},
	}

	for _, tt := range tests {
		body, _ := a.EncodeResponse(&IRResponse{
			ID:         "test",
			Model:      "gpt-5",
			StopReason: tt.reason,
		})
		if body["status"] != tt.want {
			t.Errorf("StopReason=%q -> status=%v, want %q", tt.reason, body["status"], tt.want)
		}
	}
}

// =============================================================================
// NewStreamState
// =============================================================================

func TestOpenAIResponsesNewStreamState(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	if state == nil {
		t.Fatal("NewStreamState() returned nil")
	}
	st, ok := state.(*responsesStreamState)
	if !ok {
		t.Fatalf("NewStreamState() returned %T, want *responsesStreamState", state)
	}
	if st.textStarted == nil || st.textStopped == nil || st.itemIDs == nil {
		t.Error("Maps should be initialized")
	}
}

// =============================================================================
// DecodeStreamEvent
// =============================================================================

func TestOpenAIResponsesDecodeStreamEvent_ResponseCreated(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	events, err := a.DecodeStreamEvent(map[string]interface{}{
		"type": "response.created",
		"response": map[string]interface{}{
			"id":    "resp_001",
			"model": "gpt-5",
		},
	}, state)
	if err != nil {
		t.Fatalf("DecodeStreamEvent() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Type != IRStreamMessageStart {
		t.Errorf("Type = %q, want %q", events[0].Type, IRStreamMessageStart)
	}
	if events[0].ResponseID != "resp_001" {
		t.Errorf("ResponseID = %q", events[0].ResponseID)
	}
	if events[0].ResponseModel != "gpt-5" {
		t.Errorf("ResponseModel = %q", events[0].ResponseModel)
	}
}

func TestOpenAIResponsesDecodeStreamEvent_OutputItemAdded_Text(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	events, err := a.DecodeStreamEvent(map[string]interface{}{
		"type":         "response.output_item.added",
		"output_index": float64(0),
		"item": map[string]interface{}{
			"id":   "msg_001",
			"type": "message",
		},
	}, state)
	if err != nil {
		t.Fatalf("DecodeStreamEvent() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Type != IRStreamContentStart {
		t.Errorf("Type = %q", events[0].Type)
	}
	if events[0].Part == nil || events[0].Part.Type != IRPartText {
		t.Errorf("Part = %+v", events[0].Part)
	}
}

func TestOpenAIResponsesDecodeStreamEvent_OutputItemAdded_Tool(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	events, err := a.DecodeStreamEvent(map[string]interface{}{
		"type":         "response.output_item.added",
		"output_index": float64(0),
		"item": map[string]interface{}{
			"type":    "function_call",
			"call_id": "call_001",
			"name":    "get_weather",
		},
	}, state)
	if err != nil {
		t.Fatalf("DecodeStreamEvent() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Type != IRStreamContentStart {
		t.Errorf("Type = %q", events[0].Type)
	}
	if events[0].Part == nil || events[0].Part.Type != IRPartToolCall {
		t.Fatalf("Part = %+v, want tool_call", events[0].Part)
	}
	if events[0].Part.ToolCall == nil || events[0].Part.ToolCall.ID != "call_001" {
		t.Errorf("ToolCall = %+v", events[0].Part.ToolCall)
	}
	if events[0].Part.ToolCall.Name != "get_weather" {
		t.Errorf("ToolCall.Name = %q", events[0].Part.ToolCall.Name)
	}
}

func TestOpenAIResponsesDecodeStreamEvent_TextDelta(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	events, err := a.DecodeStreamEvent(map[string]interface{}{
		"type":         "response.output_text.delta",
		"output_index": float64(0),
		"delta":        "Hello",
	}, state)
	if err != nil {
		t.Fatalf("DecodeStreamEvent() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Type != IRStreamContentDelta {
		t.Errorf("Type = %q", events[0].Type)
	}
	if events[0].DeltaText != "Hello" {
		t.Errorf("DeltaText = %q", events[0].DeltaText)
	}
}

func TestOpenAIResponsesDecodeStreamEvent_FunctionCallDelta(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	events, err := a.DecodeStreamEvent(map[string]interface{}{
		"type":         "response.function_call_arguments.delta",
		"output_index": float64(0),
		"delta":        "{\"city\":\"Tokyo\"}",
	}, state)
	if err != nil {
		t.Fatalf("DecodeStreamEvent() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Type != IRStreamContentDelta {
		t.Errorf("Type = %q", events[0].Type)
	}
	if events[0].DeltaJSON != "{\"city\":\"Tokyo\"}" {
		t.Errorf("DeltaJSON = %q", events[0].DeltaJSON)
	}
}

func TestOpenAIResponsesDecodeStreamEvent_OutputItemDone(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	events, err := a.DecodeStreamEvent(map[string]interface{}{
		"type":         "response.output_item.done",
		"output_index": float64(0),
	}, state)
	if err != nil {
		t.Fatalf("DecodeStreamEvent() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Type != IRStreamContentStop {
		t.Errorf("Type = %q, want %q", events[0].Type, IRStreamContentStop)
	}
	if events[0].Index != 0 {
		t.Errorf("Index = %d", events[0].Index)
	}
}

func TestOpenAIResponsesDecodeStreamEvent_Completed(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	events, err := a.DecodeStreamEvent(map[string]interface{}{
		"type": "response.completed",
		"response": map[string]interface{}{
			"status": "completed",
			"usage": map[string]interface{}{
				"input_tokens":  float64(10),
				"output_tokens": float64(20),
				"total_tokens":  float64(30),
			},
		},
	}, state)
	if err != nil {
		t.Fatalf("DecodeStreamEvent() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2 (message_delta + done)", len(events))
	}
	if events[0].Type != IRStreamMessageDelta {
		t.Errorf("events[0].Type = %q, want %q", events[0].Type, IRStreamMessageDelta)
	}
	if events[1].Type != IRStreamDone {
		t.Errorf("events[1].Type = %q, want %q", events[1].Type, IRStreamDone)
	}
	if events[0].StopReason != IRStopEndTurn {
		t.Errorf("StopReason = %q", events[0].StopReason)
	}
	if events[0].Usage == nil || events[0].Usage.InputTokens != 10 {
		t.Errorf("Usage = %+v", events[0].Usage)
	}
}

func TestOpenAIResponsesDecodeStreamEvent_Error(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	events, err := a.DecodeStreamEvent(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "rate_limit",
			"message": "Rate limit exceeded",
		},
	}, state)
	if err != nil {
		t.Fatalf("DecodeStreamEvent() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Type != IRStreamError {
		t.Errorf("Type = %q", events[0].Type)
	}
	if events[0].ErrorType != "rate_limit" {
		t.Errorf("ErrorType = %q", events[0].ErrorType)
	}
}

func TestOpenAIResponsesDecodeStreamEvent_SkipBookkeepingEvents(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()

	// content_part.added should be skipped
	events, err := a.DecodeStreamEvent(map[string]interface{}{
		"type": "response.content_part.added",
	}, state)
	if err != nil {
		t.Fatalf("DecodeStreamEvent() error = %v", err)
	}
	if events != nil {
		t.Errorf("content_part.added should return nil, got %d events", len(events))
	}

	// output_text.done should be skipped
	events2, _ := a.DecodeStreamEvent(map[string]interface{}{
		"type": "response.output_text.done",
	}, state)
	if events2 != nil {
		t.Errorf("output_text.done should return nil, got %d events", len(events2))
	}

	// content_part.done should be skipped
	events3, _ := a.DecodeStreamEvent(map[string]interface{}{
		"type": "response.content_part.done",
	}, state)
	if events3 != nil {
		t.Errorf("content_part.done should return nil, got %d events", len(events3))
	}
}

// =============================================================================
// EncodeStreamEvent
// =============================================================================

func TestOpenAIResponsesEncodeStreamEvent_MessageStart(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	payloads, err := a.EncodeStreamEvent(&IRStreamEvent{
		Type:          IRStreamMessageStart,
		ResponseID:    "resp_001",
		ResponseModel: "gpt-5",
	}, state)
	if err != nil {
		t.Fatalf("EncodeStreamEvent() error = %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("len(payloads) = %d, want 1", len(payloads))
	}
	if payloads[0]["type"] != "response.created" {
		t.Errorf("type = %v", payloads[0]["type"])
	}
	resp, ok := payloads[0]["response"].(map[string]interface{})
	if !ok {
		t.Fatalf("response is not a map")
	}
	if resp["id"] != "resp_001" {
		t.Errorf("response.id = %v", resp["id"])
	}
	if resp["status"] != "in_progress" {
		t.Errorf("response.status = %v", resp["status"])
	}
	if resp["model"] != "gpt-5" {
		t.Errorf("response.model = %v", resp["model"])
	}
}

func TestOpenAIResponsesEncodeStreamEvent_ContentStart_Text(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	payloads, err := a.EncodeStreamEvent(&IRStreamEvent{
		Type:  IRStreamContentStart,
		Index: 0,
		Part:  &IRContentPart{Type: IRPartText},
	}, state)
	if err != nil {
		t.Fatalf("EncodeStreamEvent() error = %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("len(payloads) = %d, want 2 (output_item + content_part)", len(payloads))
	}
	if payloads[0]["type"] != "response.output_item.added" {
		t.Errorf("payloads[0].type = %v", payloads[0]["type"])
	}
	if payloads[1]["type"] != "response.content_part.added" {
		t.Errorf("payloads[1].type = %v", payloads[1]["type"])
	}
}

func TestOpenAIResponsesEncodeStreamEvent_ContentStart_Tool(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	payloads, err := a.EncodeStreamEvent(&IRStreamEvent{
		Type:  IRStreamContentStart,
		Index: 0,
		Part: &IRContentPart{
			Type: IRPartToolCall,
			ID:   "call_001",
			ToolCall: &IRToolCallPart{
				ID:   "call_001",
				Name: "get_weather",
			},
		},
	}, state)
	if err != nil {
		t.Fatalf("EncodeStreamEvent() error = %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("len(payloads) = %d, want 1", len(payloads))
	}
	if payloads[0]["type"] != "response.output_item.added" {
		t.Errorf("type = %v", payloads[0]["type"])
	}
	item, ok := payloads[0]["item"].(map[string]interface{})
	if !ok {
		t.Fatalf("item is not a map")
	}
	if item["type"] != "function_call" {
		t.Errorf("item.type = %v", item["type"])
	}
	if item["status"] != "in_progress" {
		t.Errorf("item.status = %v", item["status"])
	}
}

func TestOpenAIResponsesEncodeStreamEvent_ContentDelta_Text(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	// Start text first
	a.EncodeStreamEvent(&IRStreamEvent{Type: IRStreamContentStart, Index: 0, Part: &IRContentPart{Type: IRPartText}}, state)

	payloads, err := a.EncodeStreamEvent(&IRStreamEvent{
		Type:      IRStreamContentDelta,
		Index:     0,
		DeltaText: "Hello, world!",
	}, state)
	if err != nil {
		t.Fatalf("EncodeStreamEvent() error = %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("len(payloads) = %d, want 1", len(payloads))
	}
	if payloads[0]["type"] != "response.output_text.delta" {
		t.Errorf("type = %v", payloads[0]["type"])
	}
	if payloads[0]["delta"] != "Hello, world!" {
		t.Errorf("delta = %v", payloads[0]["delta"])
	}
}

func TestOpenAIResponsesEncodeStreamEvent_ContentDelta_Tool(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	payloads, err := a.EncodeStreamEvent(&IRStreamEvent{
		Type:      IRStreamContentDelta,
		Index:     0,
		DeltaJSON: "{\"city\":\"Paris\"}",
	}, state)
	if err != nil {
		t.Fatalf("EncodeStreamEvent() error = %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("len(payloads) = %d, want 1", len(payloads))
	}
	if payloads[0]["type"] != "response.function_call_arguments.delta" {
		t.Errorf("type = %v", payloads[0]["type"])
	}
	if payloads[0]["delta"] != "{\"city\":\"Paris\"}" {
		t.Errorf("delta = %v", payloads[0]["delta"])
	}
}

func TestOpenAIResponsesEncodeStreamEvent_ContentStop_Text(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	// Start text and accumulate delta
	a.EncodeStreamEvent(&IRStreamEvent{Type: IRStreamContentStart, Index: 0, Part: &IRContentPart{Type: IRPartText}}, state)
	a.EncodeStreamEvent(&IRStreamEvent{Type: IRStreamContentDelta, Index: 0, DeltaText: "Hello"}, state)
	a.EncodeStreamEvent(&IRStreamEvent{Type: IRStreamContentDelta, Index: 0, DeltaText: " world!"}, state)

	payloads, err := a.EncodeStreamEvent(&IRStreamEvent{
		Type:  IRStreamContentStop,
		Index: 0,
	}, state)
	if err != nil {
		t.Fatalf("EncodeStreamEvent() error = %v", err)
	}
	if len(payloads) != 3 {
		t.Fatalf("len(payloads) = %d, want 3 (text.done + content_part.done + output_item.done)", len(payloads))
	}
	if payloads[0]["type"] != "response.output_text.done" {
		t.Errorf("payloads[0].type = %v", payloads[0]["type"])
	}
	if payloads[0]["text"] != "Hello world!" {
		t.Errorf("payloads[0].text = %v", payloads[0]["text"])
	}
	if payloads[1]["type"] != "response.content_part.done" {
		t.Errorf("payloads[1].type = %v", payloads[1]["type"])
	}
	if payloads[2]["type"] != "response.output_item.done" {
		t.Errorf("payloads[2].type = %v", payloads[2]["type"])
	}
}

func TestOpenAIResponsesEncodeStreamEvent_ContentStop_Tool(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	// Start tool and accumulate args
	a.EncodeStreamEvent(&IRStreamEvent{
		Type: IRStreamContentStart, Index: 0,
		Part: &IRContentPart{Type: IRPartToolCall, ID: "call_001", ToolCall: &IRToolCallPart{ID: "call_001", Name: "get_weather"}},
	}, state)
	a.EncodeStreamEvent(&IRStreamEvent{Type: IRStreamContentDelta, Index: 0, DeltaJSON: "{\"city\":\"Paris\"}"}, state)

	payloads, err := a.EncodeStreamEvent(&IRStreamEvent{
		Type:  IRStreamContentStop,
		Index: 0,
	}, state)
	if err != nil {
		t.Fatalf("EncodeStreamEvent() error = %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("len(payloads) = %d, want 2 (arguments.done + output_item.done)", len(payloads))
	}
	if payloads[0]["type"] != "response.function_call_arguments.done" {
		t.Errorf("payloads[0].type = %v", payloads[0]["type"])
	}
	if payloads[1]["type"] != "response.output_item.done" {
		t.Errorf("payloads[1].type = %v", payloads[1]["type"])
	}
	item, _ := payloads[1]["item"].(map[string]interface{})
	if item["arguments"] != "{\"city\":\"Paris\"}" {
		t.Errorf("item.arguments = %v", item["arguments"])
	}
	if item["status"] != "completed" {
		t.Errorf("item.status = %v", item["status"])
	}
}

func TestOpenAIResponsesEncodeStreamEvent_Done(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	// Set up state: message start
	a.EncodeStreamEvent(&IRStreamEvent{
		Type:          IRStreamMessageStart,
		ResponseID:    "resp_001",
		ResponseModel: "gpt-5",
	}, state)
	// Set usage and stop reason
	a.EncodeStreamEvent(&IRStreamEvent{
		Type:       IRStreamMessageDelta,
		StopReason: IRStopEndTurn,
		Usage:      &IRUsage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30, CacheReadInputTokens: 5},
	}, state)

	payloads, err := a.EncodeStreamEvent(&IRStreamEvent{
		Type: IRStreamDone,
	}, state)
	if err != nil {
		t.Fatalf("EncodeStreamEvent() error = %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("len(payloads) = %d, want 1", len(payloads))
	}
	if payloads[0]["type"] != "response.completed" {
		t.Errorf("type = %v", payloads[0]["type"])
	}
	resp, ok := payloads[0]["response"].(map[string]interface{})
	if !ok {
		t.Fatalf("response is not a map")
	}
	if resp["status"] != "completed" {
		t.Errorf("status = %v", resp["status"])
	}
	usage, ok := resp["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("usage is not a map")
	}
	if usage["input_tokens"] != float64(10) {
		t.Errorf("input_tokens = %v", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(20) {
		t.Errorf("output_tokens = %v", usage["output_tokens"])
	}
	if usage["total_tokens"] != float64(30) {
		t.Errorf("total_tokens = %v", usage["total_tokens"])
	}
}

func TestOpenAIResponsesEncodeStreamEvent_Error(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	payloads, err := a.EncodeStreamEvent(&IRStreamEvent{
		Type:         IRStreamError,
		ErrorMessage: "Rate limit exceeded",
		ErrorType:    "rate_limit",
	}, state)
	if err != nil {
		t.Fatalf("EncodeStreamEvent() error = %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("len(payloads) = %d, want 1", len(payloads))
	}
	if payloads[0]["type"] != "error" {
		t.Errorf("type = %v", payloads[0]["type"])
	}
	errMap, ok := payloads[0]["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error is not a map")
	}
	if errMap["type"] != "rate_limit" {
		t.Errorf("error.type = %v", errMap["type"])
	}
	if errMap["message"] != "Rate limit exceeded" {
		t.Errorf("error.message = %v", errMap["message"])
	}
}

func TestOpenAIResponsesEncodeStreamEvent_MessageDeltaIsNoop(t *testing.T) {
	a := newAdapter()
	state := a.NewStreamState()
	payloads, err := a.EncodeStreamEvent(&IRStreamEvent{
		Type:       IRStreamMessageDelta,
		StopReason: IRStopEndTurn,
		Usage:      &IRUsage{InputTokens: 50, OutputTokens: 100},
	}, state)
	if err != nil {
		t.Fatalf("EncodeStreamEvent() error = %v", err)
	}
	if payloads != nil {
		t.Errorf("MessageDelta should return nil, got %d payloads", len(payloads))
	}
}

// =============================================================================
// StreamLifecycle - Golden file test
// =============================================================================

func TestOpenAIResponsesStreamLifecycle_GoldenFile(t *testing.T) {
	// This test replays a full Responses API stream and verifies
	// decode + encode round-trip produces equivalent events.
	a := newAdapter()

	// Simulate a full stream: response.created -> output_item -> deltas -> done -> completed
	events := []map[string]interface{}{
		{"type": "response.created", "response": map[string]interface{}{"id": "resp_001", "model": "gpt-5"}},
		{"type": "response.output_item.added", "output_index": float64(0), "item": map[string]interface{}{"id": "msg_001", "type": "message"}},
		{"type": "response.content_part.added", "item_id": "msg_001", "output_index": float64(0), "content_index": float64(0)},
		{"type": "response.output_text.delta", "output_index": float64(0), "delta": "The"},
		{"type": "response.output_text.delta", "output_index": float64(0), "delta": " capital"},
		{"type": "response.output_text.delta", "output_index": float64(0), "delta": " of"},
		{"type": "response.output_text.delta", "output_index": float64(0), "delta": " France"},
		{"type": "response.output_text.delta", "output_index": float64(0), "delta": " is"},
		{"type": "response.output_text.delta", "output_index": float64(0), "delta": " Paris."},
		{"type": "response.output_text.done", "item_id": "msg_001", "output_index": float64(0), "text": "The capital of France is Paris."},
		{"type": "response.content_part.done", "item_id": "msg_001", "output_index": float64(0)},
		{"type": "response.output_item.done", "output_index": float64(0)},
		{"type": "response.completed", "response": map[string]interface{}{
			"status": "completed",
			"usage":  map[string]interface{}{"input_tokens": float64(15), "output_tokens": float64(7), "total_tokens": float64(22)},
		}},
	}

	// Phase 1: Decode all events (upstream -> IR)
	var irEvents []*IRStreamEvent
	state := a.NewStreamState()
	for i, raw := range events {
		evts, err := a.DecodeStreamEvent(raw, state)
		if err != nil {
			t.Fatalf("DecodeStreamEvent event %d error = %v", i, err)
		}
		irEvents = append(irEvents, evts...)
	}

	// Verify we got the right IR events
	if len(irEvents) < 5 {
		t.Fatalf("len(irEvents) = %d, want at least 5", len(irEvents))
	}
	// First should be message_start
	if irEvents[0].Type != IRStreamMessageStart {
		t.Errorf("irEvents[0].Type = %q", irEvents[0].Type)
	}
	// Last two should be message_delta + done
	lastTwo := irEvents[len(irEvents)-2:]
	if lastTwo[0].Type != IRStreamMessageDelta {
		t.Errorf("irEvents[-2].Type = %q, want message_delta", lastTwo[0].Type)
	}
	if lastTwo[1].Type != IRStreamDone {
		t.Errorf("irEvents[-1].Type = %q, want done", lastTwo[1].Type)
	}

	// Find the first content_start (skipping bookkeeping)
	var contentStarts int
	for _, e := range irEvents {
		if e.Type == IRStreamContentStart {
			contentStarts++
		}
	}
	if contentStarts != 1 {
		t.Errorf("content_starts = %d, want 1", contentStarts)
	}

	// Count deltas (excluding skipped events)
	j, _ := json.MarshalIndent(irEvents, "", "  ")
	t.Logf("IR events: %s", string(j))

	// Phase 2: Re-encode all IR events (IR -> upstream)
	var encodeState interface{} = a.NewStreamState()
	var encodedEvents []map[string]interface{}
	for _, irEvent := range irEvents {
		payloads, err := a.EncodeStreamEvent(irEvent, encodeState)
		if err != nil {
			t.Fatalf("EncodeStreamEvent error = %v", err)
		}
		encodedEvents = append(encodedEvents, payloads...)
	}

	// Verify we got a proper response.completed at the end
	if len(encodedEvents) == 0 {
		t.Fatal("No encoded events")
	}
	lastEncoded := encodedEvents[len(encodedEvents)-1]
	if lastEncoded["type"] != "response.completed" {
		t.Fatalf("Last encoded event type = %v, want response.completed", lastEncoded["type"])
	}
	resp, ok := lastEncoded["response"].(map[string]interface{})
	if !ok {
		t.Fatalf("response is not a map: %T", lastEncoded["response"])
	}
	if resp["status"] != "completed" {
		t.Errorf("response.status = %v", resp["status"])
	}
	usage, ok := resp["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("usage is not a map")
	}
	if usage["input_tokens"] != float64(15) {
		t.Errorf("input_tokens = %v", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(7) {
		t.Errorf("output_tokens = %v", usage["output_tokens"])
	}
}
