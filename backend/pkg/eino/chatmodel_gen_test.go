package eino

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	einoschema "github.com/cloudwego/eino/schema"
)

// fakeOpenAICompletion 返回一个最小合法的 OpenAI chat completion 响应，供 fake server 使用。
func fakeOpenAICompletion(t *testing.T) string {
	t.Helper()
	resp := map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"model":   "test-model",
		"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}}},
		"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 1, "total_tokens": 11},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal fake response: %v", err)
	}
	return string(b)
}

func TestChatModelPassesSamplingParamsToOpenAICompatibleUpstream(t *testing.T) {
	var gotBody map[string]any
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, fakeOpenAICompletion(t))
	}))
	defer srv.Close()

	model, err := NewChatModel(context.Background(), &ChatModelConfig{
		Provider:         ProviderOpenAI,
		APIKey:           "sk-test",
		Model:            "test-model",
		BaseURL:          srv.URL,
		MaxTokens:        2048,
		Temperature:      fp32(0.3),
		TopP:             fp32(0.8),
		FrequencyPenalty: fp32(0.2),
		PresencePenalty:  fp32(0.4),
	})
	if err != nil {
		t.Fatalf("new chat model: %v", err)
	}

	if _, err := model.Generate(context.Background(), []*einoschema.Message{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if gotBody == nil {
		t.Fatalf("no request body captured")
	}
	if v, ok := gotBody["max_tokens"].(float64); !ok || v != 2048 {
		t.Fatalf("max_tokens = %v, want 2048", gotBody["max_tokens"])
	}
	if v, ok := gotBody["temperature"].(float64); !ok || v != 0.3 {
		t.Fatalf("temperature = %v, want 0.3", gotBody["temperature"])
	}
	if v, ok := gotBody["top_p"].(float64); !ok || v != 0.8 {
		t.Fatalf("top_p = %v, want 0.8", gotBody["top_p"])
	}
	if v, ok := gotBody["frequency_penalty"].(float64); !ok || v != 0.2 {
		t.Fatalf("frequency_penalty = %v, want 0.2", gotBody["frequency_penalty"])
	}
	if v, ok := gotBody["presence_penalty"].(float64); !ok || v != 0.4 {
		t.Fatalf("presence_penalty = %v, want 0.4", gotBody["presence_penalty"])
	}
	if gotPath == "" {
		t.Fatal("no request path captured")
	}
}

func TestOpenAICompatibleChatModelDefaultsMaxTokens(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, fakeOpenAICompletion(t))
	}))
	defer srv.Close()

	model, err := NewChatModel(context.Background(), &ChatModelConfig{
		Provider: ProviderOpenAI,
		APIKey:   "sk-test",
		Model:    "test-model",
		BaseURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("new chat model: %v", err)
	}

	if _, err := model.Generate(context.Background(), []*einoschema.Message{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if v, ok := gotBody["max_tokens"].(float64); !ok || v != float64(defaultMaxTokens) {
		t.Fatalf("max_tokens = %v, want default %d", gotBody["max_tokens"], defaultMaxTokens)
	}
}

func TestChatModelRejectsMissingAPIKey(t *testing.T) {
	_, err := NewChatModel(context.Background(), &ChatModelConfig{
		Provider: ProviderOpenAI,
		Model:    "test-model",
	})
	if err == nil || !strings.Contains(err.Error(), "api key is required") {
		t.Fatalf("expected api key required error, got %v", err)
	}
}

func fp32(v float32) *float32 { return &v }
