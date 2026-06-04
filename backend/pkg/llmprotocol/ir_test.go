package llmprotocol

import (
	"github.com/bytedance/sonic"
	"testing"
)

func ptr[V float64 | int](v V) *V {
	return &v
}

// compile-time check: IRContentPart uses typed fields, not interface{}
func TestIRContentPartNoInterface(t *testing.T) {
	var p IRContentPart
	p = IRContentPart{Type: IRPartText, Text: "hello"}
	p = IRContentPart{Type: IRPartToolCall, ToolCall: &IRToolCallPart{ID: "call_1", Name: "get_weather"}}
	p = IRContentPart{Type: IRPartToolResult, ToolResult: &IRToolResultPart{ToolCallID: "call_1"}}
	p = IRContentPart{Type: IRPartReasoning, Reasoning: &IRReasoningPart{Content: "thinking..."}}
	p = IRContentPart{Type: IRPartRefusal, Refusal: &IRRefusalPart{Text: "I cannot do that"}}
	_ = p // just verify it compiles
}

func TestIRRequestJSONRoundTrip(t *testing.T) {
	temp := 0.8
	topP := 0.95
	seed := 42

	orig := IRRequest{
		ID:          "req_001",
		Model:       "gpt-4o",
		Stream:      true,
		User:        "user_abc",
		System:      "You are a helpful assistant.",
		MaxTokens:   4096,
		Temperature: &temp,
		TopP:        &topP,
		Stop:        []string{"\n", "END"},
		Seed:        &seed,
		Messages: []IRMessage{
			{ID: "msg_1", Role: IRRoleUser, Parts: []IRContentPart{
				{Type: IRPartText, Text: "Hello"},
			}},
			{ID: "msg_2", Role: IRRoleAssistant, Parts: []IRContentPart{
				{Type: IRPartText, Text: "Hi there!"},
			}},
		},
		Tools: []IRToolDecl{
			{Type: "function", Name: "get_weather", Description: "Get weather for a city"},
		},
		ToolChoice:     &IRToolChoice{Type: "auto"},
		ResponseFormat: &IRResponseFormat{Type: "text"},
		Instructions:   "Respond concisely.",
		Extensions: map[string]map[string]interface{}{
			"openai_chat": {"max_tokens": 100},
		},
	}

	b, err := sonic.Marshal(orig)
	if err != nil {
		t.Fatalf("sonic.Marshal(IRRequest) error = %v", err)
	}

	var got IRRequest
	if err := sonic.Unmarshal(b, &got); err != nil {
		t.Fatalf("sonic.Unmarshal(IRRequest) error = %v", err)
	}

	if got.ID != orig.ID {
		t.Errorf("ID = %q, want %q", got.ID, orig.ID)
	}
	if got.Model != orig.Model {
		t.Errorf("Model = %q, want %q", got.Model, orig.Model)
	}
	if got.Stream != orig.Stream {
		t.Errorf("Stream = %v, want %v", got.Stream, orig.Stream)
	}
	if got.User != orig.User {
		t.Errorf("User = %q, want %q", got.User, orig.User)
	}
	if got.System != orig.System {
		t.Errorf("System = %q, want %q", got.System, orig.System)
	}
	if got.MaxTokens != orig.MaxTokens {
		t.Errorf("MaxTokens = %d, want %d", got.MaxTokens, orig.MaxTokens)
	}
	if got.Temperature == nil || *got.Temperature != *orig.Temperature {
		t.Errorf("Temperature = %v, want %v", got.Temperature, orig.Temperature)
	}
	if got.TopP == nil || *got.TopP != *orig.TopP {
		t.Errorf("TopP = %v, want %v", got.TopP, orig.TopP)
	}
	if got.Seed == nil || *got.Seed != *orig.Seed {
		t.Errorf("Seed = %v, want %v", got.Seed, orig.Seed)
	}
	if len(got.Stop) != len(orig.Stop) || got.Stop[0] != orig.Stop[0] || got.Stop[1] != orig.Stop[1] {
		t.Errorf("Stop = %v, want %v", got.Stop, orig.Stop)
	}
	if len(got.Messages) != len(orig.Messages) {
		t.Fatalf("len(Messages) = %d, want %d", len(got.Messages), len(orig.Messages))
	}
	if got.Messages[0].Role != orig.Messages[0].Role {
		t.Errorf("Messages[0].Role = %q, want %q", got.Messages[0].Role, orig.Messages[0].Role)
	}
	if len(got.Tools) != len(orig.Tools) {
		t.Errorf("len(Tools) = %d, want %d", len(got.Tools), len(orig.Tools))
	}
	if got.ToolChoice == nil || got.ToolChoice.Type != orig.ToolChoice.Type {
		t.Errorf("ToolChoice = %v, want %v", got.ToolChoice, orig.ToolChoice)
	}
	if got.ResponseFormat == nil || got.ResponseFormat.Type != orig.ResponseFormat.Type {
		t.Errorf("ResponseFormat = %v, want %v", got.ResponseFormat, orig.ResponseFormat)
	}
	if got.Instructions != orig.Instructions {
		t.Errorf("Instructions = %q, want %q", got.Instructions, orig.Instructions)
	}

	// Extensions: verify NOT in JSON output due to json:"-"
	if containsJSONKey(t, b, "extensions") {
		t.Error("Extensions should NOT appear in serialized JSON due to json:\"-\" tag")
	}
	// Extensions should still be accessible on the original struct
	if orig.Extensions == nil {
		t.Error("Extensions should exist on the struct")
	}
	if v, ok := orig.Extensions["openai_chat"]; !ok {
		t.Error("Extensions[\"openai_chat\"] should be accessible on the struct")
	} else if maxTokens, ok := v["max_tokens"]; !ok {
		t.Error("Extensions[\"openai_chat\"][\"max_tokens\"] should be accessible on the struct")
	} else if _, isInt := maxTokens.(int); !isInt {
		t.Errorf("Extensions[\"openai_chat\"][\"max_tokens\"] type = %T, want int", maxTokens)
	}
}

func TestIRResponseJSONRoundTrip(t *testing.T) {
	orig := IRResponse{
		ID:      "resp_001",
		Model:   "gpt-4o",
		Created: 1700000000,
		Content: []IRContentPart{
			{Type: IRPartText, Text: "Hello, world!"},
		},
		Usage: &IRUsage{
			InputTokens:          10,
			OutputTokens:         20,
			TotalTokens:          30,
			CacheReadInputTokens: 5,
			ReasoningTokens:      3,
		},
		StopReason: IRStopEndTurn,
		Extensions: map[string]map[string]interface{}{
			"anthropic": {"thinking": "enabled"},
		},
	}

	b, err := sonic.Marshal(orig)
	if err != nil {
		t.Fatalf("sonic.Marshal(IRResponse) error = %v", err)
	}

	var got IRResponse
	if err := sonic.Unmarshal(b, &got); err != nil {
		t.Fatalf("sonic.Unmarshal(IRResponse) error = %v", err)
	}

	if got.ID != orig.ID {
		t.Errorf("ID = %q, want %q", got.ID, orig.ID)
	}
	if got.Model != orig.Model {
		t.Errorf("Model = %q, want %q", got.Model, orig.Model)
	}
	if got.Created != orig.Created {
		t.Errorf("Created = %d, want %d", got.Created, orig.Created)
	}
	if len(got.Content) != len(orig.Content) {
		t.Fatalf("len(Content) = %d, want %d", len(got.Content), len(orig.Content))
	}
	if got.Content[0].Type != orig.Content[0].Type || got.Content[0].Text != orig.Content[0].Text {
		t.Errorf("Content[0] = %+v, want %+v", got.Content[0], orig.Content[0])
	}
	if got.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if got.Usage.InputTokens != orig.Usage.InputTokens {
		t.Errorf("Usage.InputTokens = %d, want %d", got.Usage.InputTokens, orig.Usage.InputTokens)
	}
	if got.Usage.OutputTokens != orig.Usage.OutputTokens {
		t.Errorf("Usage.OutputTokens = %d, want %d", got.Usage.OutputTokens, orig.Usage.OutputTokens)
	}
	if got.Usage.TotalTokens != orig.Usage.TotalTokens {
		t.Errorf("Usage.TotalTokens = %d, want %d", got.Usage.TotalTokens, orig.Usage.TotalTokens)
	}
	if got.Usage.CacheReadInputTokens != orig.Usage.CacheReadInputTokens {
		t.Errorf("Usage.CacheReadInputTokens = %d, want %d", got.Usage.CacheReadInputTokens, orig.Usage.CacheReadInputTokens)
	}
	if got.Usage.ReasoningTokens != orig.Usage.ReasoningTokens {
		t.Errorf("Usage.ReasoningTokens = %d, want %d", got.Usage.ReasoningTokens, orig.Usage.ReasoningTokens)
	}
	if got.StopReason != orig.StopReason {
		t.Errorf("StopReason = %q, want %q", got.StopReason, orig.StopReason)
	}

	// Extensions should NOT appear in JSON
	if containsJSONKey(t, b, "extensions") {
		t.Error("Extensions should NOT appear in serialized JSON due to json:\"-\" tag")
	}
}

func TestIRContentPartTypes(t *testing.T) {
	tests := []struct {
		name    string
		part    IRContentPart
		checkFn func(t *testing.T, got IRContentPart)
	}{
		{
			name: "text",
			part: IRContentPart{Type: IRPartText, Text: "hello world"},
			checkFn: func(t *testing.T, got IRContentPart) {
				if got.Text != "hello world" {
					t.Errorf("Text = %q, want %q", got.Text, "hello world")
				}
			},
		},
		{
			name: "tool_call",
			part: IRContentPart{
				Type:     IRPartToolCall,
				ID:       "call_abc",
				ToolCall: &IRToolCallPart{ID: "call_abc", Name: "get_weather", ArgumentsRaw: `{"city":"Tokyo"}`},
			},
			checkFn: func(t *testing.T, got IRContentPart) {
				if got.ID != "call_abc" {
					t.Errorf("ID = %q, want %q", got.ID, "call_abc")
				}
				if got.ToolCall == nil {
					t.Fatal("ToolCall is nil")
				}
				if got.ToolCall.Name != "get_weather" {
					t.Errorf("ToolCall.Name = %q, want %q", got.ToolCall.Name, "get_weather")
				}
				if got.ToolCall.ArgumentsRaw != `{"city":"Tokyo"}` {
					t.Errorf("ToolCall.ArgumentsRaw = %q", got.ToolCall.ArgumentsRaw)
				}
			},
		},
		{
			name: "tool_result",
			part: IRContentPart{
				Type: IRPartToolResult,
				ToolResult: &IRToolResultPart{ToolCallID: "call_abc", Status: "success", Content: []IRContentPart{
					{Type: IRPartText, Text: "25°C"},
				}},
			},
			checkFn: func(t *testing.T, got IRContentPart) {
				if got.ToolResult == nil {
					t.Fatal("ToolResult is nil")
				}
				if got.ToolResult.ToolCallID != "call_abc" {
					t.Errorf("ToolResult.ToolCallID = %q", got.ToolResult.ToolCallID)
				}
				if got.ToolResult.Status != "success" {
					t.Errorf("ToolResult.Status = %q", got.ToolResult.Status)
				}
				if len(got.ToolResult.Content) != 1 || got.ToolResult.Content[0].Text != "25°C" {
					t.Errorf("ToolResult.Content = %+v", got.ToolResult.Content)
				}
			},
		},
		{
			name: "reasoning",
			part: IRContentPart{
				Type:      IRPartReasoning,
				Reasoning: &IRReasoningPart{Content: "thinking about it...", Signature: "sig_001"},
			},
			checkFn: func(t *testing.T, got IRContentPart) {
				if got.Reasoning == nil {
					t.Fatal("Reasoning is nil")
				}
				if got.Reasoning.Content != "thinking about it..." {
					t.Errorf("Reasoning.Content = %q", got.Reasoning.Content)
				}
				if got.Reasoning.Signature != "sig_001" {
					t.Errorf("Reasoning.Signature = %q", got.Reasoning.Signature)
				}
			},
		},
		{
			name: "refusal",
			part: IRContentPart{
				Type:    IRPartRefusal,
				Refusal: &IRRefusalPart{Text: "I cannot answer that"},
			},
			checkFn: func(t *testing.T, got IRContentPart) {
				if got.Refusal == nil {
					t.Fatal("Refusal is nil")
				}
				if got.Refusal.Text != "I cannot answer that" {
					t.Errorf("Refusal.Text = %q", got.Refusal.Text)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := sonic.Marshal(tt.part)
			if err != nil {
				t.Fatalf("sonic.Marshal error = %v", err)
			}
			var got IRContentPart
			if err := sonic.Unmarshal(b, &got); err != nil {
				t.Fatalf("sonic.Unmarshal error = %v, json=%s", err, string(b))
			}
			if got.Type != tt.part.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.part.Type)
			}
			tt.checkFn(t, got)
		})
	}
}

func TestIRStreamEventConstruction(t *testing.T) {
	usage := &IRUsage{InputTokens: 5, OutputTokens: 10}

	tests := []struct {
		name  string
		event IRStreamEvent
	}{
		{
			name:  "message_start",
			event: IRStreamEvent{Type: IRStreamMessageStart, ResponseID: "resp_001", ResponseModel: "gpt-4o"},
		},
		{
			name:  "content_part_start",
			event: IRStreamEvent{Type: IRStreamContentStart, Index: 0, Part: &IRContentPart{Type: IRPartText}},
		},
		{
			name:  "content_part_delta",
			event: IRStreamEvent{Type: IRStreamContentDelta, Index: 0, DeltaText: "hello"},
		},
		{
			name:  "content_part_stop",
			event: IRStreamEvent{Type: IRStreamContentStop, Index: 0},
		},
		{
			name:  "message_delta",
			event: IRStreamEvent{Type: IRStreamMessageDelta, StopReason: IRStopEndTurn, Usage: usage},
		},
		{
			name:  "done",
			event: IRStreamEvent{Type: IRStreamDone, ResponseID: "resp_001", Usage: usage},
		},
		{
			name:  "error",
			event: IRStreamEvent{Type: IRStreamError, ErrorMessage: "rate limit exceeded", ErrorType: "rate_limit"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := sonic.Marshal(tt.event)
			if err != nil {
				t.Fatalf("sonic.Marshal error = %v", err)
			}
			var got IRStreamEvent
			if err := sonic.Unmarshal(b, &got); err != nil {
				t.Fatalf("sonic.Unmarshal error = %v, json=%s", err, string(b))
			}
			if got.Type != tt.event.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.event.Type)
			}

			// Check type-specific fields
			switch tt.event.Type {
			case IRStreamMessageStart:
				got, want := got.ResponseID, tt.event.ResponseID
				if got != want {
					t.Errorf("ResponseID = %q, want %q", got, want)
				}
			case IRStreamContentStart:
				if got.Index != tt.event.Index {
					t.Errorf("Index = %d, want %d", got.Index, tt.event.Index)
				}
				if got.Part == nil || got.Part.Type != tt.event.Part.Type {
					t.Errorf("Part = %+v, want %+v", got.Part, tt.event.Part)
				}
			case IRStreamContentDelta:
				if got.DeltaText != tt.event.DeltaText {
					t.Errorf("DeltaText = %q, want %q", got.DeltaText, tt.event.DeltaText)
				}
			case IRStreamContentStop:
				if got.Index != tt.event.Index {
					t.Errorf("Index = %d, want %d", got.Index, tt.event.Index)
				}
			case IRStreamMessageDelta:
				if got.StopReason != tt.event.StopReason {
					t.Errorf("StopReason = %q, want %q", got.StopReason, tt.event.StopReason)
				}
				if got.Usage == nil || got.Usage.InputTokens != usage.InputTokens {
					t.Errorf("Usage mismatch")
				}
			case IRStreamDone:
				if got.ResponseID != tt.event.ResponseID {
					t.Errorf("ResponseID = %q, want %q", got.ResponseID, tt.event.ResponseID)
				}
			case IRStreamError:
				if got.ErrorMessage != tt.event.ErrorMessage {
					t.Errorf("ErrorMessage = %q, want %q", got.ErrorMessage, tt.event.ErrorMessage)
				}
				if got.ErrorType != tt.event.ErrorType {
					t.Errorf("ErrorType = %q, want %q", got.ErrorType, tt.event.ErrorType)
				}
			}
		})
	}
}

func TestIRUsageTokens(t *testing.T) {
	tests := []struct {
		name  string
		usage IRUsage
	}{
		{
			name:  "all_fields",
			usage: IRUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150, CacheReadInputTokens: 20, ReasoningTokens: 10},
		},
		{
			name:  "only_required",
			usage: IRUsage{InputTokens: 5, OutputTokens: 10},
		},
		{
			name:  "zero_values",
			usage: IRUsage{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := sonic.Marshal(tt.usage)
			if err != nil {
				t.Fatalf("sonic.Marshal error = %v", err)
			}
			var got IRUsage
			if err := sonic.Unmarshal(b, &got); err != nil {
				t.Fatalf("sonic.Unmarshal error = %v, json=%s", err, string(b))
			}
			if got.InputTokens != tt.usage.InputTokens {
				t.Errorf("InputTokens = %d, want %d", got.InputTokens, tt.usage.InputTokens)
			}
			if got.OutputTokens != tt.usage.OutputTokens {
				t.Errorf("OutputTokens = %d, want %d", got.OutputTokens, tt.usage.OutputTokens)
			}
			if got.TotalTokens != tt.usage.TotalTokens {
				t.Errorf("TotalTokens = %d, want %d", got.TotalTokens, tt.usage.TotalTokens)
			}
			if got.CacheReadInputTokens != tt.usage.CacheReadInputTokens {
				t.Errorf("CacheReadInputTokens = %d, want %d", got.CacheReadInputTokens, tt.usage.CacheReadInputTokens)
			}
			if got.ReasoningTokens != tt.usage.ReasoningTokens {
				t.Errorf("ReasoningTokens = %d, want %d", got.ReasoningTokens, tt.usage.ReasoningTokens)
			}
		})
	}
}

func TestIRMessageGetTextContent(t *testing.T) {
	tests := []struct {
		name     string
		msg      IRMessage
		expected string
	}{
		{
			name: "multiple_text_parts",
			msg: IRMessage{
				Role: IRRoleUser,
				Parts: []IRContentPart{
					{Type: IRPartText, Text: "Hello"},
					{Type: IRPartText, Text: " "},
					{Type: IRPartText, Text: "World"},
				},
			},
			expected: "Hello World",
		},
		{
			name: "mixed_parts",
			msg: IRMessage{
				Role: IRRoleAssistant,
				Parts: []IRContentPart{
					{Type: IRPartText, Text: "The answer is "},
					{Type: IRPartReasoning, Reasoning: &IRReasoningPart{Content: "thinking..."}},
					{Type: IRPartText, Text: "42"},
				},
			},
			expected: "The answer is 42",
		},
		{
			name: "no_text_parts",
			msg: IRMessage{
				Role: IRRoleTool,
				Parts: []IRContentPart{
					{Type: IRPartToolResult, ToolResult: &IRToolResultPart{ToolCallID: "call_1"}},
				},
			},
			expected: "",
		},
		{
			name:     "empty_parts",
			msg:      IRMessage{Role: IRRoleUser},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.msg.GetTextContent()
			if got != tt.expected {
				t.Errorf("GetTextContent() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestIRExtensionsIsolation(t *testing.T) {
	req := IRRequest{
		Model: "gpt-4o",
		Messages: []IRMessage{
			{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "hi"}}},
		},
		Extensions: map[string]map[string]interface{}{
			"openai_chat": {"user": "abc123"},
		},
	}

	b, err := sonic.Marshal(req)
	if err != nil {
		t.Fatalf("sonic.Marshal error = %v", err)
	}

	// Verify Extensions key is NOT in JSON
	if containsJSONKey(t, b, "extensions") {
		t.Error("Extensions should NOT appear in JSON output (json:\"-\")")
	}

	// Verify Extensions still accessible on struct
	if req.Extensions == nil {
		t.Fatal("Extensions should be accessible on the struct")
	}
	if _, ok := req.Extensions["openai_chat"]; !ok {
		t.Error("Extensions['openai_chat'] should be accessible")
	}
	if req.Extensions["openai_chat"]["user"] != "abc123" {
		t.Error("Extensions['openai_chat']['user'] should be 'abc123'")
	}

	// Unmarshal JSON that does NOT have Extensions (since it wasn't serialized)
	var got IRRequest
	if err := sonic.Unmarshal(b, &got); err != nil {
		t.Fatalf("sonic.Unmarshal error = %v", err)
	}

	// After unmarshal, Extensions should be nil (not present in JSON)
	if got.Extensions != nil {
		t.Error("Extensions should be nil after unmarshal (not in JSON)")
	}
}

func TestIRRequestEmpty(t *testing.T) {
	req := IRRequest{}
	b, err := sonic.Marshal(req)
	if err != nil {
		t.Fatalf("sonic.Marshal empty IRRequest error = %v", err)
	}

	var got IRRequest
	if err := sonic.Unmarshal(b, &got); err != nil {
		t.Fatalf("sonic.Unmarshal empty IRRequest error = %v", err)
	}

	// All zero-value fields should remain zero
	if got.Model != "" {
		t.Errorf("Model = %q, want empty", got.Model)
	}
	if got.Messages != nil {
		t.Errorf("Messages = %v, want nil", got.Messages)
	}
	if got.Tools != nil {
		t.Errorf("Tools = %v, want nil", got.Tools)
	}
	if got.Stop != nil {
		t.Errorf("Stop = %v, want nil", got.Stop)
	}
	if got.Temperature != nil {
		t.Errorf("Temperature = %v, want nil", got.Temperature)
	}
	if got.TopP != nil {
		t.Errorf("TopP = %v, want nil", got.TopP)
	}
	if got.Seed != nil {
		t.Errorf("Seed = %v, want nil", got.Seed)
	}
	if got.ToolChoice != nil {
		t.Errorf("ToolChoice = %v, want nil", got.ToolChoice)
	}
	if got.ResponseFormat != nil {
		t.Errorf("ResponseFormat = %v, want nil", got.ResponseFormat)
	}
}

func TestIRResponseNilSlices(t *testing.T) {
	resp := IRResponse{
		ID:         "resp_001",
		Model:      "gpt-4o",
		Created:    1700000000,
		Content:    nil,
		Usage:      nil,
		Extensions: nil,
	}

	b, err := sonic.Marshal(resp)
	if err != nil {
		t.Fatalf("sonic.Marshal error = %v", err)
	}

	var got IRResponse
	if err := sonic.Unmarshal(b, &got); err != nil {
		t.Fatalf("sonic.Unmarshal error = %v", err)
	}

	if got.Content != nil {
		t.Errorf("Content = %v, want nil", got.Content)
	}
	if got.Usage != nil {
		t.Errorf("Usage = %v, want nil", got.Usage)
	}
}

// containsJSONKey checks if a marshaled JSON contains a specific key.
func containsJSONKey(t *testing.T, data []byte, key string) bool {
	t.Helper()
	var raw map[string]interface{}
	if err := sonic.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal into map: %v", err)
	}
	_, ok := raw[key]
	return ok
}
