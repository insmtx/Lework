package eino

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const htmlGatewayPage = "<!doctype html>\n<html lang=\"en\"><head><title>New API</title></head>" +
	"<body><div id=\"root\"></div></body></html>"

func TestJSONGuardTurnsHTMLResponseIntoReadableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, htmlGatewayPage)
	}))
	defer srv.Close()

	// query 里塞一个假 key，验证错误文本不会把它带出去。
	resp, err := newJSONGuardingHTTPClient().Get(srv.URL + "/chat/completions?api_key=sk-super-secret")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("expected error, got status %d", resp.StatusCode)
	}
	if !IsUpstreamNotJSON(err) {
		t.Fatalf("expected ErrUpstreamNotJSON in chain, got %v", err)
	}

	var target *UpstreamNotJSONError
	if !errors.As(err, &target) {
		t.Fatalf("expected *UpstreamNotJSONError, got %T", err)
	}
	if target.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", target.StatusCode, http.StatusOK)
	}
	if !strings.Contains(target.URL, "/chat/completions") {
		t.Errorf("URL = %q, want it to contain the request path", target.URL)
	}
	if strings.Contains(target.URL, "sk-super-secret") {
		t.Errorf("URL = %q must not leak credentials", target.URL)
	}

	message := err.Error()
	if strings.Contains(message, "sk-super-secret") {
		t.Errorf("error message must not leak credentials: %s", message)
	}
	if strings.Contains(message, "invalid character") {
		t.Errorf("error message should be human readable, got: %s", message)
	}
	if !strings.Contains(message, "text/html") {
		t.Errorf("error message should mention the content type: %s", message)
	}
	if !strings.Contains(message, "/v1") {
		t.Errorf("error message should hint at the missing /v1 prefix: %s", message)
	}
}

func TestJSONGuardPassesThroughJSONResponse(t *testing.T) {
	// 响应体故意超过窥探窗口，验证被读走的字节会被完整放回。
	payload := `{"id":"chatcmpl-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"` +
		strings.Repeat("ok ", 200) + `"}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	resp, err := newJSONGuardingHTTPClient().Get(srv.URL + "/v1/chat/completions")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != payload {
		t.Fatalf("body was altered by the guard: got %d bytes, want %d", len(body), len(payload))
	}
}

func TestJSONGuardPassesThroughSSEStream(t *testing.T) {
	payload := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	resp, err := newJSONGuardingHTTPClient().Get(srv.URL + "/v1/chat/completions")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != payload {
		t.Fatalf("SSE body was altered: got %q", string(body))
	}
}
