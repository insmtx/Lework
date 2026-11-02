package opencode

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/Lework/backend/agent"
	"github.com/insmtx/Lework/backend/agent/runtime/internal/cli"
)

const onePixelPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestMultimodalImageReachesRequestBody(t *testing.T) {
	if os.Getenv("LEWORK_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEWORK_ACP_PROBE=1 (needs opencode 2.1.x)")
	}
	png, err := base64.StdEncoding.DecodeString(onePixelPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	body := runMultimodalPrompt(t, true, png)
	if !strings.Contains(body, `"image_url"`) {
		t.Fatalf("request body has no image_url: %.500s", body)
	}
	if !strings.Contains(body, "data:image/png;base64,") {
		t.Fatalf("request body has no png data url: %.500s", body)
	}
	t.Logf("small image forwarded, body bytes=%d", len(body))
}

func TestFiveMiBImageReachesRequestBody(t *testing.T) {
	if os.Getenv("LEWORK_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEWORK_ACP_PROBE=1 (needs opencode 2.1.x)")
	}
	// 必须是合法 PNG：opencode 会对附件做重采样，非法字节会被直接丢弃。
	data := encodeNoisyPNG(t, 1600, 1300)
	if len(data) < 5*1024*1024 {
		t.Fatalf("generated png too small: %d bytes", len(data))
	}
	body := runMultimodalPrompt(t, true, data)
	if !strings.Contains(body, `"image_url"`) {
		t.Fatalf("5MiB request body has no image_url (body=%d bytes)", len(body))
	}
	t.Logf("5MiB png (%d bytes) forwarded, request body=%d bytes", len(data), len(body))
}

func TestNonVisionModelDegradesImage(t *testing.T) {
	if os.Getenv("LEWORK_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEWORK_ACP_PROBE=1 (needs opencode 2.1.x)")
	}
	png, err := base64.StdEncoding.DecodeString(onePixelPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	// Vision=false 时不能硬失败；opencode 会把图片降级为文本或丢弃。
	body := runMultimodalPrompt(t, false, png)
	t.Logf("non-vision model completed, image_url present=%v body=%d bytes",
		strings.Contains(body, `"image_url"`), len(body))
}

func encodeNoisyPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	image := image.NewRGBA(image.Rect(0, 0, width, height))
	state := uint32(1)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			state = state*1664525 + 1013904223
			image.SetRGBA(x, y, color.RGBA{R: uint8(state >> 24), G: uint8(state >> 16), B: uint8(state >> 8), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func runMultimodalPrompt(t *testing.T, vision bool, data []byte) string {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, sseChunk(`{"id":"mock-mm","object":"chat.completion.chunk","created":1,"model":"mock-mm","choices":[{"index":0,"delta":{"role":"assistant","content":"收到"},"finish_reason":null}]}`))
			fmt.Fprint(w, sseChunk(`{"id":"mock-mm","object":"chat.completion.chunk","created":1,"model":"mock-mm","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":2,"total_tokens":14}}`))
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-mm","object":"model"}]}`))
	}))
	defer server.Close()

	root := t.TempDir()
	if err := os.MkdirAll(root+"/workspace", 0o755); err != nil {
		t.Fatal(err)
	}
	model := agent.ModelConfig{
		Model:   "mock-mm",
		APIKey:  "test-key",
		BaseURL: server.URL,
		Vision:  vision,
	}
	invoker := NewACPInvoker("opencode", nil, root+"/data", time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	handle, err := invoker.Invoke(ctx, cli.InvocationRequest{
		ExecutionID: "multimodal-probe",
		WorkDir:     root + "/workspace",
		Prompt:      "这张图里是什么？一句话回答。",
		Model:       model,
		Attachments: []agent.Attachment{{MIME: "image/png", Name: "probe.png", Data: data}},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if handle.Process != nil {
		defer handle.Process.Stop()
	}
	for range handle.Events {
	}
	if result := <-handle.Result; result.Err != nil {
		t.Fatalf("result error: %v", result.Err)
	}

	mu.Lock()
	defer mu.Unlock()
	// 会话标题生成也会调用一次模型；优先找带 image_url 的那次主请求。
	largest := ""
	for _, body := range bodies {
		if strings.Contains(body, `"image_url"`) {
			return body
		}
		var payload map[string]any
		if json.Unmarshal([]byte(body), &payload) == nil {
			if _, ok := payload["messages"]; ok && len(body) > len(largest) {
				largest = body
			}
		}
	}
	if largest != "" {
		return largest
	}
	t.Fatalf("no chat/completions request captured (%d bodies)", len(bodies))
	return ""
}
