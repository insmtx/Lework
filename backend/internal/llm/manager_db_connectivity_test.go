package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeOpenAIChatCompletion 返回一个最小合法的 OpenAI chat completion 响应体。
func fakeOpenAIChatCompletion(t *testing.T) string {
	t.Helper()
	payload := map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"model":   "test-model",
		"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal completion: %v", err)
	}
	return string(body)
}

// gatewayWithHTMLFallback 模拟 New API / one-api 这类网关：
// 未命中的路径回退到前端首页（HTTP 200 + text/html），只有 /v1/chat/completions 是真正的 API。
func gatewayWithHTMLFallback(t *testing.T, v1Hits, noV1Hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			atomic.AddInt32(v1Hits, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, fakeOpenAIChatCompletion(t))
		default:
			atomic.AddInt32(noV1Hits, 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<!doctype html><html><body>New API</body></html>")
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTestConnectivityFallsBackToV1Prefix 覆盖线上问题：
// 未落库的临时配置（无 id/code）此前 BaseURLHasV1 恒为 false，
// 对需要 /v1 前缀的网关必然打到首页 HTML，最后抛出难懂的 JSON 语法错误。
func TestTestConnectivityFallsBackToV1Prefix(t *testing.T) {
	var v1Hits, noV1Hits int32
	srv := gatewayWithHTMLFallback(t, &v1Hits, &noV1Hits)

	m, _ := setupManager(t)
	result, err := m.TestConnectivity(context.Background(), testOrgID, &TestRequest{
		Provider: "openai",
		Model:    "deepseek-flash",
		BaseURL:  srv.URL,
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("test connectivity: %v", err)
	}

	if !result.Success {
		t.Fatalf("expected success after falling back to /v1, got message %q", result.Message)
	}
	if !result.BaseURLHasV1 {
		t.Errorf("BaseURLHasV1 = false, want true")
	}
	if want := srv.URL + "/v1"; result.Endpoint != want {
		t.Errorf("Endpoint = %q, want %q", result.Endpoint, want)
	}
	if result.Message != "ok" {
		t.Errorf("Message = %q, want %q", result.Message, "ok")
	}
	if atomic.LoadInt32(&noV1Hits) == 0 {
		t.Errorf("expected at least one attempt without /v1")
	}
	if atomic.LoadInt32(&v1Hits) == 0 {
		t.Errorf("expected the retry to hit /v1/chat/completions")
	}
}

// TestTestConnectivityReportsReadableErrorWhenBothPrefixesFail 断言两种前缀都失败时
// 既不误报成功，也返回可读错误（而不是 "invalid character '<'"）。
func TestTestConnectivityReportsReadableErrorWhenBothPrefixesFail(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><html><body>New API</body></html>")
	}))
	defer srv.Close()

	m, _ := setupManager(t)
	result, err := m.TestConnectivity(context.Background(), testOrgID, &TestRequest{
		Provider: "openai",
		Model:    "deepseek-flash",
		BaseURL:  srv.URL,
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("test connectivity: %v", err)
	}

	if result.Success {
		t.Fatalf("expected failure when every prefix serves HTML")
	}
	if atomic.LoadInt32(&hits) < 2 {
		t.Errorf("expected attempts on both prefixes, got %d", hits)
	}
	if strings.Contains(result.Message, "invalid character") {
		t.Errorf("message should be human readable, got %q", result.Message)
	}
	if strings.Contains(result.Message, "node path:") {
		t.Errorf("message should not carry eino wrapper noise, got %q", result.Message)
	}
	if !strings.Contains(result.Message, "/v1") {
		t.Errorf("message should hint at the /v1 prefix, got %q", result.Message)
	}
}
