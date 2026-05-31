package modelrouter

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func loadJSON(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", path, err)
	}
	return m
}

func loadJSONL(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var result []map[string]interface{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("failed to unmarshal line in %s: %v\nline: %s", path, err, line)
		}
		result = append(result, m)
	}
	return result
}


// TestGeminiDecodeRequest tests all aspects of DecodeRequest.
func TestGeminiDecodeRequest(t *testing.T) {
	adapter := &geminiAdapter{}

	t.Run("simple_text_request", func(t *testing.T) {
		raw := loadJSON(t, "testdata/chat_to_gemini_request.json")
		req, err := adapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest error = %v", err)
		}
		if req.System != "You are a helpful geography expert." {
			t.Errorf("System = %q", req.System)
		}
		if len(req.Messages) != 1 {
			t.Fatalf("len(Messages) = %d, want 1", len(req.Messages))
		}
		if req.Messages[0].Role != IRRoleUser {
			t.Errorf("Messages[0].Role = %q", req.Messages[0].Role)
		}
		if len(req.Messages[0].Parts) != 1 || req.Messages[0].Parts[0].Type != IRPartText {
			t.Errorf("Messages[0].Parts = %+v", req.Messages[0].Parts)
		}
		if req.Temperature == nil || *req.Temperature != 0.7 {
			t.Errorf("Temperature = %v, want 0.7", req.Temperature)
		}
		if req.MaxTokens != 4096 {
			t.Errorf("MaxTokens = %d, want 4096", req.MaxTokens)
		}
	})

	t.Run("with_tools", func(t *testing.T) {
		raw := map[string]interface{}{
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
		}
		req, err := adapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest error = %v", err)
		}
		if len(req.Tools) != 1 {
			t.Fatalf("len(Tools) = %d, want 1", len(req.Tools))
		}
		if req.Tools[0].Name != "search_project" {
			t.Errorf("Tools[0].Name = %q", req.Tools[0].Name)
		}
		if req.Tools[0].Description != "Search project records" {
			t.Errorf("Tools[0].Description = %q", req.Tools[0].Description)
		}
		if req.Tools[0].Parameters == nil {
			t.Error("Tools[0].Parameters is nil")
		}
	})
}

// TestGeminiDecodeRequest_Additional tests additional Gemini decode scenarios.
func TestGeminiDecodeRequest_Additional(t *testing.T) {
	adapter := &geminiAdapter{}

	t.Run("with_function_call_part", func(t *testing.T) {
		raw := map[string]interface{}{
			"contents": []interface{}{
				map[string]interface{}{
					"role": "model",
					"parts": []interface{}{
						map[string]interface{}{
							"functionCall": map[string]interface{}{
								"name": "get_weather",
								"args": map[string]interface{}{"city": "Tokyo"},
							},
						},
					},
				},
			},
		}
		req, err := adapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest error = %v", err)
		}
		msg := req.Messages[0]
		if msg.Role != IRRoleAssistant {
			t.Errorf("role = %q, want assistant", msg.Role)
		}
		if len(msg.Parts) != 1 {
			t.Fatalf("len(Parts) = %d, want 1", len(msg.Parts))
		}
		part := msg.Parts[0]
		if part.Type != IRPartToolCall {
			t.Errorf("part.Type = %q, want tool_call", part.Type)
		}
		if part.ToolCall == nil {
			t.Fatal("ToolCall is nil")
		}
		if part.ToolCall.Name != "get_weather" {
			t.Errorf("ToolCall.Name = %q", part.ToolCall.Name)
		}
		if part.ToolCall.ArgumentsJSON == nil {
			t.Error("ArgumentsJSON is nil")
		}
	})

	t.Run("with_function_response_part", func(t *testing.T) {
		raw := map[string]interface{}{
			"contents": []interface{}{
				map[string]interface{}{
					"role": "user",
					"parts": []interface{}{
						map[string]interface{}{
							"functionResponse": map[string]interface{}{
								"name":     "get_weather",
								"response": map[string]interface{}{"temperature": 25},
							},
						},
					},
				},
			},
		}
		req, err := adapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest error = %v", err)
		}
		part := req.Messages[0].Parts[0]
		if part.Type != IRPartToolResult {
			t.Errorf("part.Type = %q, want tool_result", part.Type)
		}
		if part.ToolResult == nil {
			t.Fatal("ToolResult is nil")
		}
	})

	t.Run("with_inline_data_image", func(t *testing.T) {
		raw := map[string]interface{}{
			"contents": []interface{}{
				map[string]interface{}{
					"role": "user",
					"parts": []interface{}{
						map[string]interface{}{
							"inlineData": map[string]interface{}{
								"mimeType": "image/png",
								"data":     "base64abc==",
							},
						},
					},
				},
			},
		}
		req, err := adapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest error = %v", err)
		}
		part := req.Messages[0].Parts[0]
		if part.Type != IRPartImage {
			t.Errorf("part.Type = %q, want image", part.Type)
		}
		if part.Metadata["mime_type"] != "image/png" {
			t.Errorf("mime_type = %q", part.Metadata["mime_type"])
		}
	})

	t.Run("with_inline_data_audio", func(t *testing.T) {
		raw := map[string]interface{}{
			"contents": []interface{}{
				map[string]interface{}{
					"role": "user",
					"parts": []interface{}{
						map[string]interface{}{
							"inlineData": map[string]interface{}{
								"mimeType": "audio/mp3",
								"data":     "base64xyz==",
							},
						},
					},
				},
			},
		}
		req, err := adapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest error = %v", err)
		}
		part := req.Messages[0].Parts[0]
		if part.Type != IRPartAudio {
			t.Errorf("part.Type = %q, want audio", part.Type)
		}
	})

	t.Run("with_file_data", func(t *testing.T) {
		raw := map[string]interface{}{
			"contents": []interface{}{
				map[string]interface{}{
					"role": "user",
					"parts": []interface{}{
						map[string]interface{}{
							"fileData": map[string]interface{}{
								"mimeType": "application/pdf",
								"fileUri":  "gs://bucket/doc.pdf",
							},
						},
					},
				},
			},
		}
		req, err := adapter.DecodeRequest(raw)
		if err != nil {
			t.Fatalf("DecodeRequest error = %v", err)
		}
		part := req.Messages[0].Parts[0]
		if part.Type != IRPartFile {
			t.Errorf("part.Type = %q, want file", part.Type)
		}
		if part.Metadata["file_uri"] != "gs://bucket/doc.pdf" {
			t.Errorf("file_uri = %q", part.Metadata["file_uri"])
		}
	})
}


func TestGeminiDecodeRequest_SystemInstruction(t *testing.T) {
	adapter := &geminiAdapter{}
	raw := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{"text": "hello"},
				},
			},
		},
		"systemInstruction": map[string]interface{}{
			"parts": []interface{}{
				map[string]interface{}{"text": "You are helpful."},
				map[string]interface{}{"text": " Be concise."},
			},
		},
	}
	req, err := adapter.DecodeRequest(raw)
	if err != nil {
		t.Fatalf("DecodeRequest error = %v", err)
	}
	if req.System != "You are helpful. Be concise." {
		t.Errorf("System = %q", req.System)
	}
}

func TestGeminiDecodeRequest_ToolConfig(t *testing.T) {
	adapter := &geminiAdapter{}
	raw := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{"text": "hi"},
				},
			},
		},
		"toolConfig": map[string]interface{}{
			"functionCallingConfig": map[string]interface{}{
				"mode": "ANY",
			},
		},
	}
	req, err := adapter.DecodeRequest(raw)
	if err != nil {
		t.Fatalf("DecodeRequest error = %v", err)
	}
	if req.ToolChoice == nil {
		t.Fatal("ToolChoice is nil")
	}
	if req.ToolChoice.Type != "any" {
		t.Errorf("ToolChoice.Type = %q, want any", req.ToolChoice.Type)
	}
}

func TestGeminiDecodeRequest_GenerationConfig(t *testing.T) {
	adapter := &geminiAdapter{}
	raw := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{"text": "hello"},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":     0.5,
			"topP":            0.9,
			"maxOutputTokens": 2048,
			"stopSequences":   []interface{}{"\n", "STOP"},
		},
	}
	req, err := adapter.DecodeRequest(raw)
	if err != nil {
		t.Fatalf("DecodeRequest error = %v", err)
	}
	if req.Temperature == nil || *req.Temperature != 0.5 {
		t.Errorf("Temperature = %v", req.Temperature)
	}
	if req.TopP == nil || *req.TopP != 0.9 {
		t.Errorf("TopP = %v", req.TopP)
	}
	if req.MaxTokens != 2048 {
		t.Errorf("MaxTokens = %d", req.MaxTokens)
	}
	if len(req.Stop) != 2 || req.Stop[0] != "\n" || req.Stop[1] != "STOP" {
		t.Errorf("Stop = %v", req.Stop)
	}
}


// TestGeminiEncodeRequest tests EncodeRequest.
func TestGeminiEncodeRequest(t *testing.T) {
	adapter := &geminiAdapter{}

	t.Run("round_trip", func(t *testing.T) {
		orig := loadJSON(t, "testdata/chat_to_gemini_request.json")
		req, err := adapter.DecodeRequest(orig)
		if err != nil {
			t.Fatalf("DecodeRequest error = %v", err)
		}
		encoded, err := adapter.EncodeRequest(req)
		if err != nil {
			t.Fatalf("EncodeRequest error = %v", err)
		}
		req2, err := adapter.DecodeRequest(encoded)
		if err != nil {
			t.Fatalf("second DecodeRequest error = %v", err)
		}
		if req2.System != req.System {
			t.Errorf("round-trip System = %q, want %q", req2.System, req.System)
		}
		if len(req2.Messages) != len(req.Messages) {
			t.Fatalf("round-trip len(Messages) = %d, want %d", len(req2.Messages), len(req.Messages))
		}
		if req2.Temperature == nil || *req2.Temperature != *req.Temperature {
			t.Errorf("round-trip Temperature mismatch")
		}
		if req2.MaxTokens != req.MaxTokens {
			t.Errorf("round-trip MaxTokens = %d, want %d", req2.MaxTokens, req.MaxTokens)
		}
	})

	t.Run("system_to_systemInstruction", func(t *testing.T) {
		temp := 0.5
		ir := &IRRequest{
			System: "You are an expert.",
			Messages: []IRMessage{
				{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "hello"}}},
			},
			Temperature: &temp,
		}
		encoded, err := adapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest error = %v", err)
		}
		si, ok := encoded["systemInstruction"].(map[string]interface{})
		if !ok {
			t.Fatal("systemInstruction missing")
		}
		siParts, ok := si["parts"].([]interface{})
		if !ok || len(siParts) != 1 {
			t.Fatalf("systemInstruction.parts len = %d, want 1", len(siParts))
		}
		siPart := siParts[0].(map[string]interface{})
		if siPart["text"] != "You are an expert." {
			t.Errorf("system text = %q", siPart["text"])
		}
	})

	t.Run("generation_config", func(t *testing.T) {
		temp := 0.8
		topP := 0.9
		ir := &IRRequest{
			Messages:    []IRMessage{{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "hi"}}}},
			Temperature: &temp,
			TopP:        &topP,
			MaxTokens:   1024,
			Stop:        []string{"\n", "END"},
		}
		encoded, err := adapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest error = %v", err)
		}
		gc, ok := encoded["generationConfig"].(map[string]interface{})
		if !ok {
			t.Fatal("generationConfig missing")
		}
		if gc["temperature"] != 0.8 {
			t.Errorf("temperature = %v", gc["temperature"])
		}
		if gc["topP"] != 0.9 {
			t.Errorf("topP = %v", gc["topP"])
		}
		if gc["maxOutputTokens"] != 1024 {
			t.Errorf("maxOutputTokens = %v", gc["maxOutputTokens"])
		}
	})

	t.Run("tools_to_function_declarations", func(t *testing.T) {
		ir := &IRRequest{
			Messages: []IRMessage{{Role: IRRoleUser, Parts: []IRContentPart{{Type: IRPartText, Text: "search"}}}},
			Tools: []IRToolDecl{
				{
					Type:        "function",
					Name:        "search_project",
					Description: "Search for projects",
					Parameters: map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}},
					},
				},
			},
		}
		encoded, err := adapter.EncodeRequest(ir)
		if err != nil {
			t.Fatalf("EncodeRequest error = %v", err)
		}
		tools, ok := encoded["tools"].([]interface{})
		if !ok || len(tools) != 1 {
			t.Fatalf("tools missing or wrong length")
		}
		toolObj := tools[0].(map[string]interface{})
		decls, ok := toolObj["functionDeclarations"].([]interface{})
		if !ok || len(decls) != 1 {
			t.Fatalf("functionDeclarations missing or wrong length")
		}
		decl := decls[0].(map[string]interface{})
		if decl["name"] != "search_project" {
			t.Errorf("name = %q", decl["name"])
		}
	})
}


// TestGeminiDecodeResponse tests DecodeResponse.
func TestGeminiDecodeResponse(t *testing.T) {
	adapter := &geminiAdapter{}

	t.Run("simple_text_response", func(t *testing.T) {
		raw := map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"parts": []interface{}{map[string]interface{}{"text": "The capital is Paris."}},
						"role":  "model",
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]interface{}{
				"promptTokenCount":     15,
				"candidatesTokenCount": 8,
				"totalTokenCount":      23,
			},
		}
		resp, err := adapter.DecodeResponse(raw)
		if err != nil {
			t.Fatalf("DecodeResponse error = %v", err)
		}
		if len(resp.Content) != 1 || resp.Content[0].Text != "The capital is Paris." {
			t.Errorf("Content = %+v", resp.Content)
		}
		if resp.StopReason != IRStopEndTurn {
			t.Errorf("StopReason = %q, want end_turn", resp.StopReason)
		}
		if resp.Usage == nil {
			t.Fatal("Usage is nil")
		}
		if resp.Usage.InputTokens != 15 {
			t.Errorf("InputTokens = %d", resp.Usage.InputTokens)
		}
		if resp.Usage.OutputTokens != 8 {
			t.Errorf("OutputTokens = %d", resp.Usage.OutputTokens)
		}
		if resp.Usage.TotalTokens != 23 {
			t.Errorf("TotalTokens = %d", resp.Usage.TotalTokens)
		}
	})

	t.Run("finish_reason_mapping", func(t *testing.T) {
		tests := []struct {
			geminiReason string
			irReason     IRStopReason
		}{
			{"STOP", IRStopEndTurn},
			{"MAX_TOKENS", IRStopMaxTokens},
			{"SAFETY", IRStopContentFilter},
			{"RECITATION", IRStopContentFilter},
		}
		for _, tt := range tests {
			raw := map[string]interface{}{
				"candidates": []interface{}{
					map[string]interface{}{
						"content": map[string]interface{}{
							"parts": []interface{}{map[string]interface{}{"text": "ok"}},
						},
						"finishReason": tt.geminiReason,
					},
				},
			}
			resp, err := adapter.DecodeResponse(raw)
			if err != nil {
				t.Fatalf("DecodeResponse(%s) error = %v", tt.geminiReason, err)
			}
			if resp.StopReason != tt.irReason {
				t.Errorf("StopReason = %q, want %q (gemini=%s)", resp.StopReason, tt.irReason, tt.geminiReason)
			}
		}
	})

	t.Run("with_function_call", func(t *testing.T) {
		raw := map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"parts": []interface{}{
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "get_weather",
									"args": map[string]interface{}{"city": "Tokyo"},
								},
							},
						},
						"role": "model",
					},
					"finishReason": "STOP",
				},
			},
		}
		resp, err := adapter.DecodeResponse(raw)
		if err != nil {
			t.Fatalf("DecodeResponse error = %v", err)
		}
		if len(resp.Content) != 1 {
			t.Fatalf("len(Content) = %d, want 1", len(resp.Content))
		}
		part := resp.Content[0]
		if part.Type != IRPartToolCall {
			t.Errorf("part.Type = %q, want tool_call", part.Type)
		}
		if part.ToolCall == nil || part.ToolCall.Name != "get_weather" {
			t.Errorf("ToolCall = %+v", part.ToolCall)
		}
	})
}

// TestGeminiEncodeResponse tests EncodeResponse round-trip.
func TestGeminiEncodeResponse(t *testing.T) {
	adapter := &geminiAdapter{}

	t.Run("round_trip", func(t *testing.T) {
		ir := &IRResponse{
			ID:    "gem-001",
			Model: "gemini-2.0-flash",
			Content: []IRContentPart{
				{Type: IRPartText, Text: "The capital of France is Paris."},
			},
			StopReason: IRStopEndTurn,
			Usage: &IRUsage{
				InputTokens:  15,
				OutputTokens: 8,
				TotalTokens:  23,
			},
		}
		encoded, err := adapter.EncodeResponse(ir)
		if err != nil {
			t.Fatalf("EncodeResponse error = %v", err)
		}
		decoded, err := adapter.DecodeResponse(encoded)
		if err != nil {
			t.Fatalf("DecodeResponse error = %v", err)
		}
		if len(decoded.Content) != 1 || decoded.Content[0].Text != ir.Content[0].Text {
			t.Errorf("Content mismatch: got %+v", decoded.Content)
		}
		if decoded.StopReason != ir.StopReason {
			t.Errorf("StopReason = %q, want %q", decoded.StopReason, ir.StopReason)
		}
		if decoded.Usage == nil || decoded.Usage.InputTokens != ir.Usage.InputTokens {
			t.Errorf("Usage mismatch: got %+v, want %+v", decoded.Usage, ir.Usage)
		}
	})
}
// TestGeminiDecodeStreamEvent tests streaming event decoding.
func TestGeminiDecodeStreamEvent(t *testing.T) {
	adapter := &geminiAdapter{}

	t.Run("text_delta", func(t *testing.T) {
		raw := map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{"text": "Hello"},
						},
					},
					"finishReason": nil,
				},
			},
		}
		state := adapter.NewStreamState()
		events, err := adapter.DecodeStreamEvent(raw, state)
		if err != nil {
			t.Fatalf("DecodeStreamEvent error = %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("len(events) = %d, want 2", len(events))
		}
		if events[0].Type != IRStreamContentStart || events[0].Index != 0 {
			t.Errorf("events[0] = %+v, want content_part_start index=0", events[0])
		}
		if events[1].Type != IRStreamContentDelta || events[1].DeltaText != "Hello" {
			t.Errorf("events[1] = %+v", events[1])
		}
	})

	t.Run("finish_reason_triggers_stop_and_done", func(t *testing.T) {
		raw := map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{"text": "Paris."},
						},
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]interface{}{
				"promptTokenCount":     5,
				"candidatesTokenCount": 3,
				"totalTokenCount":      8,
			},
		}
		state := adapter.NewStreamState()
		events, err := adapter.DecodeStreamEvent(raw, state)
		if err != nil {
			t.Fatalf("DecodeStreamEvent error = %v", err)
		}
		if len(events) != 5 {
			t.Fatalf("len(events) = %d, want 5", len(events))
		}
		if events[0].Type != IRStreamContentStart {
			t.Errorf("events[0].Type = %q", events[0].Type)
		}
		if events[1].Type != IRStreamContentDelta || events[1].DeltaText != "Paris." {
			t.Errorf("events[1] = %+v", events[1])
		}
		if events[2].Type != IRStreamContentStop {
			t.Errorf("events[2].Type = %q, want content_part_stop", events[2].Type)
		}
		if events[3].Type != IRStreamMessageDelta {
			t.Errorf("events[3].Type = %q, want message_delta", events[3].Type)
		}
		if events[3].StopReason != IRStopEndTurn {
			t.Errorf("events[3].StopReason = %q", events[3].StopReason)
		}
		if events[4].Type != IRStreamDone {
			t.Errorf("events[4].Type = %q, want done", events[4].Type)
		}
		if events[4].Usage == nil || events[4].Usage.TotalTokens != 8 {
			t.Errorf("events[4].Usage = %+v", events[4].Usage)
		}
	})

	t.Run("done_flag_prevents_subsequent_events", func(t *testing.T) {
		state := &geminiStreamState{
			partsStarted: map[int]bool{0: true},
			lastPartIdx:  0,
			done:         true,
		}
		raw := map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{"text": "should not appear"},
						},
					},
					"finishReason": "STOP",
				},
			},
		}
		events, err := adapter.DecodeStreamEvent(raw, state)
		if err != nil {
			t.Fatalf("DecodeStreamEvent error = %v", err)
		}
		if len(events) != 0 {
			t.Errorf("got %d events after done flag, want 0", len(events))
		}
	})
}

// TestGeminiMultiPartChunk tests a single chunk with multiple parts.
func TestGeminiMultiPartChunk(t *testing.T) {
	adapter := &geminiAdapter{}

	raw := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"role": "model",
					"parts": []interface{}{
						map[string]interface{}{"text": "First part text"},
						map[string]interface{}{"text": "Second part text"},
					},
				},
				"finishReason": nil,
			},
		},
	}

	state := adapter.NewStreamState()
	events, err := adapter.DecodeStreamEvent(raw, state)
	if err != nil {
		t.Fatalf("DecodeStreamEvent error = %v", err)
	}

	if len(events) != 4 {
		t.Fatalf("len(events) = %d, want 4", len(events))
	}
	if events[0].Type != IRStreamContentStart || events[0].Index != 0 {
		t.Errorf("events[0] = %+v, want content_part_start index=0", events[0])
	}
	if events[1].Type != IRStreamContentDelta || events[1].DeltaText != "First part text" {
		t.Errorf("events[1] = %+v", events[1])
	}
	if events[2].Type != IRStreamContentStart || events[2].Index != 1 {
		t.Errorf("events[2] = %+v, want content_part_start index=1", events[2])
	}
	if events[3].Type != IRStreamContentDelta || events[3].DeltaText != "Second part text" {
		t.Errorf("events[3] = %+v", events[3])
	}
}

// TestGeminiStreamLifecycle tests full lifecycle from golden file.
func TestGeminiStreamLifecycle(t *testing.T) {
	adapter := &geminiAdapter{}
	chunks := loadJSONL(t, "testdata/gemini_stream_text.jsonl")

	var allEvents []*IRStreamEvent
	state := adapter.NewStreamState()

	for i, chunk := range chunks {
		events, err := adapter.DecodeStreamEvent(chunk, state)
		if err != nil {
			t.Fatalf("chunk[%d]: DecodeStreamEvent error = %v", i, err)
		}
		allEvents = append(allEvents, events...)
	}

	counts := map[IRStreamEventType]int{}
	for _, e := range allEvents {
		counts[e.Type]++
	}

	if counts[IRStreamContentStart] != 1 {
		t.Errorf("content_part_start count = %d, want 1", counts[IRStreamContentStart])
	}
	deltaCount := counts[IRStreamContentDelta]
	if deltaCount < 1 {
		t.Errorf("content_part_delta count = %d, want >= 1", deltaCount)
	}
	if counts[IRStreamContentStop] != 1 {
		t.Errorf("content_part_stop count = %d, want 1", counts[IRStreamContentStop])
	}
	if counts[IRStreamMessageDelta] != 1 {
		t.Errorf("message_delta count = %d, want 1", counts[IRStreamMessageDelta])
	}
	if counts[IRStreamDone] != 1 {
		t.Errorf("done count = %d, want 1", counts[IRStreamDone])
	}

	var fullText string
	for _, e := range allEvents {
		if e.Type == IRStreamContentDelta {
			fullText += e.DeltaText
		}
	}
	expectedText := "The capital of France is Paris."
	if fullText != expectedText {
		t.Errorf("assembled text = %q, want %q", fullText, expectedText)
	}

	lastEvent := allEvents[len(allEvents)-1]
	if lastEvent.Type != IRStreamDone {
		t.Errorf("last event = %q, want done", lastEvent.Type)
	}
	if lastEvent.Usage == nil || lastEvent.Usage.TotalTokens != 8 {
		t.Errorf("last event usage = %+v", lastEvent.Usage)
	}

	emptyEvents, err := adapter.DecodeStreamEvent(chunks[0], state)
	if err != nil {
		t.Fatalf("post-done DecodeStreamEvent error = %v", err)
	}
	if len(emptyEvents) != 0 {
		t.Errorf("post-done events = %d, want 0", len(emptyEvents))
	}
}

// TestGeminiToChatRoundTrip verifies Gemini decode/encode structure.
func TestGeminiToChatRoundTrip(t *testing.T) {
	adapter := &geminiAdapter{}

	geminiRaw := loadJSON(t, "testdata/chat_to_gemini_request.json")
	req, err := adapter.DecodeRequest(geminiRaw)
	if err != nil {
		t.Fatalf("DecodeRequest error = %v", err)
	}

	if req.System != "You are a helpful geography expert." {
		t.Errorf("System = %q", req.System)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(req.Messages))
	}
	msg := req.Messages[0]
	if msg.Role != IRRoleUser {
		t.Errorf("role = %q, want user", msg.Role)
	}
	if len(msg.Parts) != 1 || msg.Parts[0].Type != IRPartText || msg.Parts[0].Text != "What is the capital of France?" {
		t.Errorf("parts = %+v", msg.Parts)
	}

	encoded, err := adapter.EncodeRequest(req)
	if err != nil {
		t.Fatalf("EncodeRequest error = %v", err)
	}
	contents, ok := encoded["contents"].([]interface{})
	if !ok || len(contents) != 1 {
		t.Fatalf("contents = %v", encoded["contents"])
	}
	sysInst, ok := encoded["systemInstruction"].(map[string]interface{})
	if !ok {
		t.Fatal("systemInstruction missing")
	}
	sysParts := sysInst["parts"].([]interface{})
	if len(sysParts) != 1 {
		t.Fatalf("systemInstruction.parts len = %d, want 1", len(sysParts))
	}
	gc, ok := encoded["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("generationConfig missing")
	}
	if gc["temperature"] != 0.7 {
		t.Errorf("temperature = %v, want 0.7", gc["temperature"])
	}
	if gc["maxOutputTokens"] != 4096 {
		t.Errorf("maxOutputTokens = %v, want 4096", gc["maxOutputTokens"])
	}
}

// TestGeminiAdapterRegistration verifies the adapter is registered.
func TestGeminiAdapterRegistration(t *testing.T) {
	adapter, err := GetAdapter(ProtocolGemini)
	if err != nil {
		t.Fatalf("GetAdapter error = %v", err)
	}
	if adapter == nil {
		t.Fatal("adapter is nil")
	}
	if adapter.Protocol() != ProtocolGemini {
		t.Errorf("Protocol = %q, want gemini", adapter.Protocol())
	}
}
