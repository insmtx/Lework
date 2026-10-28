package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
)

// TestSamplingParamsReachRequestBody 用本地 OpenAI 兼容 mock 抓请求体，
// 验证 v1 config 的 sampling options 在 v2 归一时确实进入模型请求体（C-1）。
//
//	LEROS_ACP_PROBE=1 go test ./backend/agent/runtime/opencode -run TestSamplingParamsReachRequestBody -v
func TestSamplingParamsReachRequestBody(t *testing.T) {
	if os.Getenv("LEROS_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEROS_ACP_PROBE=1 (needs opencode 2.1.x)")
	}

	var mu sync.Mutex
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()

		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, sseChunk(`{"id":"mock-1","object":"chat.completion.chunk","created":1,"model":"mock-model","choices":[{"index":0,"delta":{"role":"assistant","content":"收到"},"finish_reason":null}]}`))
			fmt.Fprint(w, sseChunk(`{"id":"mock-1","object":"chat.completion.chunk","created":1,"model":"mock-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":2,"total_tokens":14}}`))
			fmt.Fprint(w, "data: [DONE]\n\n")
		case strings.HasSuffix(r.URL.Path, "/models"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-model","object":"model"}]}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	topP := 0.5
	frequencyPenalty := 0.1
	presencePenalty := 0.2
	model := agent.ModelConfig{
		Model:            "mock-model",
		APIKey:           "test-key",
		BaseURL:          server.URL,
		TopP:             &topP,
		FrequencyPenalty: &frequencyPenalty,
		PresencePenalty:  &presencePenalty,
	}

	root := t.TempDir()
	if err := os.MkdirAll(root+"/workspace", 0o755); err != nil {
		t.Fatal(err)
	}
	invoker := NewACPInvoker("opencode", nil, root+"/data", 2*time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	handle, err := invoker.Invoke(ctx, cli.InvocationRequest{
		ExecutionID: "sampling-probe",
		WorkDir:     root + "/workspace",
		Prompt:      "只回答两个字：收到",
		Model:       model,
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if handle.Process != nil {
		defer handle.Process.Stop()
	}
	for range handle.Events {
	}
	result := <-handle.Result
	if result.Err != nil {
		t.Fatalf("result error: %v", result.Err)
	}

	mu.Lock()
	defer mu.Unlock()
	var target map[string]any
	for _, body := range bodies {
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			continue
		}
		if _, ok := payload["messages"]; ok {
			target = payload
			break
		}
	}
	if target == nil {
		t.Fatalf("no chat/completions request captured; bodies=%d", len(bodies))
	}
	if got := target["model"]; got != "mock-model" {
		t.Fatalf("model = %v, want mock-model", got)
	}
	assertNumber := func(key string, want float64) {
		t.Helper()
		got, ok := target[key].(float64)
		if !ok || got != want {
			raw, _ := json.Marshal(target)
			t.Fatalf("%s = %v, want %v; body=%s", key, target[key], want, raw)
		}
	}
	assertNumber("top_p", topP)
	assertNumber("frequency_penalty", frequencyPenalty)
	assertNumber("presence_penalty", presencePenalty)
	t.Logf("sampling params reached request body: top_p=%v frequency_penalty=%v presence_penalty=%v",
		target["top_p"], target["frequency_penalty"], target["presence_penalty"])
}

func sseChunk(payload string) string {
	return "data: " + payload + "\n\n"
}
