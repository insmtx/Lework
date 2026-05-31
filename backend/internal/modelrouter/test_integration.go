package modelrouter

import (
	"encoding/json"
	"strings"
	"testing"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Integration tests — cross-protocol request/response/stream conversion
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestIntegration_ChatToAnthropic_NonStream(t *testing.T) {
	chatRaw := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "What is the weather in Tokyo?",
			},
		},
		"temperature": 0.5,
	}

	chatAdapter := &openAIChatAdapter{}
	ir, err := chatAdapter.DecodeRequest(chatRaw)
	if err != nil {
		t.Fatalf("decode chat: %v", err)
	}

	antAdapter := &anthropicMessagesAdapter{}
	antRaw, err := antAdapter.EncodeRequest(ir)
	if err != nil {
		t.Fatalf("encode anthropic: %v", err)
	}

	if antRaw["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("model mismatch: %v", antRaw["model"])
	}
	if v, ok := antRaw["max_tokens"].(int); !ok || v == 0 {
		t.Error("max_tokens should be set")
	}
	messagesRaw, ok := antRaw["messages"]
	if !ok {
		t.Fatal("expected messages field")
	}
	msgList, _ := getListForTest(messagesRaw)
	if len(msgList) < 1 {
		t.Fatal("expected at least 1 message")
	}
	msg0, _ := msgList[0].(map[string]interface{})
	contentList, _ := getListForTest(msg0["content"])
	if len(contentList) < 1 {
		t.Fatal("expected content blocks")
	}
	block0, _ := contentList[0].(map[string]interface{})
	if getString(block0, "text") != "What is the weather in Tokyo?" {
		t.Errorf("text mismatch: %v", getString(block0, "text"))
	}
}

func TestIntegration_ChatToResponses_NonStream(t *testing.T) {
	chatRaw := map[string]interface{}{
		"model": "gpt-5",
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "system",
				"content": "You are a helpful geography expert.",
			},
			map[string]interface{}{
				"role":    "user",
				"content": "What is the capital of France?",
			},
		},
		"temperature": 0.7,
		"max_tokens":  4096,
	}

	chatAdapter := &openAIChatAdapter{}
	ir, err := chatAdapter.DecodeRequest(chatRaw)
	if err != nil {
		t.Fatalf("decode chat: %v", err)
	}

	respAdapter := &openAIResponsesAdapter{}
	respRaw, err := respAdapter.EncodeRequest(ir)
	if err != nil {
		t.Fatalf("encode responses: %v", err)
	}

	if respRaw["model"] != "gpt-5" {
		t.Errorf("model mismatch: %v", respRaw["model"])
	}
	if respRaw["instructions"] != "You are a helpful geography expert." {
		t.Errorf("instructions mismatch: %v", respRaw["instructions"])
	}
}

func TestIntegration_ChatToGemini_NonStream(t *testing.T) {
	chatRaw := map[string]interface{}{
		"model": "gpt-5",
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "system",
				"content": "You are a helpful geography expert.",
			},
			map[string]interface{}{
				"role":    "user",
				"content": "What is the capital of France?",
			},
		},
		"temperature": 0.7,
		"max_tokens":  4096,
	}

	chatAdapter := &openAIChatAdapter{}
	ir, err := chatAdapter.DecodeRequest(chatRaw)
	if err != nil {
		t.Fatalf("decode chat: %v", err)
	}

	ir.Model = ""

	geminiAdapter := &geminiAdapter{}
	geminiRaw, err := geminiAdapter.EncodeRequest(ir)
	if err != nil {
		t.Fatalf("encode gemini: %v", err)
	}

	if si, ok := geminiRaw["systemInstruction"].(map[string]interface{}); ok {
		parts, _ := si["parts"].([]interface{})
		if len(parts) < 1 {
			t.Fatal("expected systemInstruction parts")
		}
	}
	contents, _ := geminiRaw["contents"].([]interface{})
	if len(contents) < 1 {
		t.Fatal("expected contents")
	}
}

func TestIntegration_AnthropicToChat_NonStream(t *testing.T) {
	antRaw := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": float64(4096),
		"system":     "You are a helpful assistant.",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "Search for project status",
					},
				},
			},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"name":        "search_project",
				"description": "Search project records",
				"input_schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{"type": "string"},
					},
					"required": []interface{}{"query"},
				},
			},
		},
		"tool_choice": map[string]interface{}{"type": "any"},
	}

	antAdapter := &anthropicMessagesAdapter{}
	ir, err := antAdapter.DecodeRequest(antRaw)
	if err != nil {
		t.Fatalf("decode anthropic: %v", err)
	}

	ir.Model = "gpt-5"

	chatAdapter := &openAIChatAdapter{}
	chatRaw, err := chatAdapter.EncodeRequest(ir)
	if err != nil {
		t.Fatalf("encode chat: %v", err)
	}

	messages, _ := chatRaw["messages"].([]interface{})
	if len(messages) < 2 {
		t.Fatalf("expected system + user messages, got %d", len(messages))
	}
	msg0, _ := messages[0].(map[string]interface{})
	if getString(msg0, "role") != "system" {
		t.Errorf("expected system role, got %v", getString(msg0, "role"))
	}
}

func TestIntegration_ResponsesToChat_NonStream(t *testing.T) {
	respRaw := map[string]interface{}{
		"model":        "gpt-5",
		"instructions": "You are a helpful assistant.",
		"input": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type": "input_text",
						"text": "Search for project status",
					},
				},
			},
		},
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
		"tool_choice": "required",
		"temperature": 0.3,
	}

	respAdapter := &openAIResponsesAdapter{}
	ir, err := respAdapter.DecodeRequest(respRaw)
	if err != nil {
		t.Fatalf("decode responses: %v", err)
	}

	ir.Model = "gpt-5"

	chatAdapter := &openAIChatAdapter{}
	chatRaw, err := chatAdapter.EncodeRequest(ir)
	if err != nil {
		t.Fatalf("encode chat: %v", err)
	}

	if chatRaw["model"] != "gpt-5" {
		t.Errorf("model mismatch: %v", chatRaw["model"])
	}
	messages, _ := chatRaw["messages"].([]interface{})
	if len(messages) < 2 {
		t.Fatalf("expected system + user messages, got %d", len(messages))
	}
}

func TestIntegration_GeminiToChat_NonStream(t *testing.T) {
	geminiRaw := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{"text": "Search for project status"},
				},
			},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"functionDeclarations": []interface{}{
					map[string]interface{}{
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
			},
		},
		"toolConfig": map[string]interface{}{
			"functionCallingConfig": map[string]interface{}{
				"mode": "ANY",
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":    float64(0.7),
			"maxOutputTokens": float64(4096),
		},
	}

	geminiAdapter := &geminiAdapter{}
	ir, err := geminiAdapter.DecodeRequest(geminiRaw)
	if err != nil {
		t.Fatalf("decode gemini: %v", err)
	}

	ir.Model = "gpt-5"
	ir.MaxTokens = 4096

	chatAdapter := &openAIChatAdapter{}
	chatRaw, err := chatAdapter.EncodeRequest(ir)
	if err != nil {
		t.Fatalf("encode chat: %v", err)
	}

	if chatRaw["model"] != "gpt-5" {
		t.Errorf("model mismatch: %v", chatRaw["model"])
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Stream conversion integration tests
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestIntegration_ChatToAnthropic_StreamText(t *testing.T) {
	chatLines := strings.Split(`{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}
{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"content":"The"},"finish_reason":null}]}
{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"content":" capital"},"finish_reason":null}]}
{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"content":" of"},"finish_reason":null}]}
{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"content":" France"},"finish_reason":null}]}
{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"content":" is"},"finish_reason":null}]}
{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{"content":" Paris."},"finish_reason":null}]}
{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":15,"completion_tokens":7,"total_tokens":22}}`, "\n")

	chatAdapter := &openAIChatAdapter{}
	antAdapter := &anthropicMessagesAdapter{}

	chatState := chatAdapter.NewStreamState()
	antState := antAdapter.NewStreamState()

	var irCount, antCount int

	for _, line := range chatLines {
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		irEvents, err := chatAdapter.DecodeStreamEvent(raw, chatState)
		if err != nil {
			t.Fatalf("decode chat: %v", err)
		}
		for _, irEvt := range irEvents {
			irCount++
			payloads, _ := antAdapter.EncodeStreamEvent(irEvt, antState)
			antCount += len(payloads)
		}
	}

	if irCount == 0 {
		t.Error("expected IR events from Chat stream")
	}
	if antCount == 0 {
		t.Error("expected Anthropic events from conversion")
	}
	t.Logf("Chat→Anthropic stream: IR=%d, ant=%d", irCount, antCount)
}

func TestIntegration_AnthropicToChat_StreamText(t *testing.T) {
	antLines := strings.Split(`{"type":"message_start","message":{"id":"msg_ant_001","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[],"usage":{"input_tokens":15,"output_tokens":0}}}
{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}
{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"The"}}
{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" capital"}}
{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" of"}}
{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" France"}}
{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" is"}}
{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" Paris."}}
{"type":"content_block_stop","index":0}
{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":15}}
{"type":"message_stop"}`, "\n")

	antAdapter := &anthropicMessagesAdapter{}
	chatAdapter := &openAIChatAdapter{}

	antState := antAdapter.NewStreamState()
	chatState := chatAdapter.NewStreamState()

	var irCount, chatCount int

	for _, line := range antLines {
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		irEvents, err := antAdapter.DecodeStreamEvent(raw, antState)
		if err != nil {
			t.Fatalf("decode anthropic: %v", err)
		}
		for _, irEvt := range irEvents {
			irCount++
			payloads, _ := chatAdapter.EncodeStreamEvent(irEvt, chatState)
			chatCount += len(payloads)
		}
	}

	if irCount == 0 {
		t.Error("expected IR events from Anthropic stream")
	}
	if chatCount == 0 {
		t.Error("expected Chat events from conversion")
	}
	t.Logf("Anthropic→Chat stream: IR=%d, chat=%d", irCount, chatCount)
}

func TestIntegration_ResponsesToChat_StreamText(t *testing.T) {
	respLines := strings.Split(`{"type":"response.created","response":{"id":"resp_stream","object":"response","created_at":1700000000,"model":"gpt-5","status":"in_progress","output":[]}}
{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_item_0","type":"message","status":"in_progress","role":"assistant","content":[]}}
{"type":"response.content_part.added","item_id":"msg_item_0","output_index":0,"content_index":0,"part":{"type":"output_text","text":"","annotations":[]}}
{"type":"response.output_text.delta","item_id":"msg_item_0","output_index":0,"content_index":0,"delta":"The"}
{"type":"response.output_text.delta","item_id":"msg_item_0","output_index":0,"content_index":0,"delta":" capital"}
{"type":"response.output_text.delta","item_id":"msg_item_0","output_index":0,"content_index":0,"delta":" of"}
{"type":"response.output_text.done","item_id":"msg_item_0","output_index":0,"content_index":0,"text":"The capital of France is Paris."}
{"type":"response.content_part.done","item_id":"msg_item_0","output_index":0,"content_index":0,"part":{"type":"output_text","text":"The capital of France is Paris.","annotations":[]}}
{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_item_0","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"The capital of France is Paris.","annotations":[]}]}}
{"type":"response.completed","response":{"id":"resp_stream","object":"response","created_at":1700000000,"model":"gpt-5","status":"completed","output":[],"usage":{"input_tokens":15,"output_tokens":8,"total_tokens":23}}}`, "\n")

	respAdapter := &openAIResponsesAdapter{}
	chatAdapter := &openAIChatAdapter{}

	respState := respAdapter.NewStreamState()
	chatState := chatAdapter.NewStreamState()

	var irCount, chatCount int

	for _, line := range respLines {
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		irEvents, err := respAdapter.DecodeStreamEvent(raw, respState)
		if err != nil {
			t.Fatalf("decode responses: %v", err)
		}
		for _, irEvt := range irEvents {
			irCount++
			payloads, _ := chatAdapter.EncodeStreamEvent(irEvt, chatState)
			chatCount += len(payloads)
		}
	}

	if irCount == 0 {
		t.Error("expected IR events from Responses stream")
	}
	if chatCount == 0 {
		t.Error("expected Chat events from conversion")
	}
	t.Logf("Responses→Chat stream: IR=%d, chat=%d", irCount, chatCount)
}

func TestIntegration_GeminiToChat_StreamText(t *testing.T) {
	geminiLines := strings.Split(`{"candidates":[{"content":{"role":"model","parts":[{"text":"The"}]}}]}
{"candidates":[{"content":{"role":"model","parts":[{"text":" capital"}]}}]}
{"candidates":[{"content":{"role":"model","parts":[{"text":" of"}]}}]}
{"candidates":[{"content":{"role":"model","parts":[{"text":" France"}]}}]}
{"candidates":[{"content":{"role":"model","parts":[{"text":" is"}]}}]}
{"candidates":[{"content":{"role":"model","parts":[{"text":" Paris."}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":6,"totalTokenCount":11}}`, "\n")

	geminiAdapter := &geminiAdapter{}
	chatAdapter := &openAIChatAdapter{}

	geminiState := geminiAdapter.NewStreamState()
	chatState := chatAdapter.NewStreamState()

	var irCount, chatCount int

	for _, line := range geminiLines {
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		irEvents, err := geminiAdapter.DecodeStreamEvent(raw, geminiState)
		if err != nil {
			t.Fatalf("decode gemini: %v", err)
		}
		for _, irEvt := range irEvents {
			irCount++
			payloads, _ := chatAdapter.EncodeStreamEvent(irEvt, chatState)
			chatCount += len(payloads)
		}
	}

	if irCount == 0 {
		t.Error("expected IR events from Gemini stream")
	}
	if chatCount == 0 {
		t.Error("expected Chat events from conversion")
	}
	t.Logf("Gemini→Chat stream: IR=%d, chat=%d", irCount, chatCount)
}

// getListForTest converts various slice types to []interface{} for test assertions.
func getListForTest(v interface{}) ([]interface{}, bool) {
	switch vals := v.(type) {
	case []interface{}:
		return vals, true
	case []map[string]interface{}:
		result := make([]interface{}, len(vals))
		for i, m := range vals {
			result[i] = m
		}
		return result, true
	default:
		return nil, false
	}
}
