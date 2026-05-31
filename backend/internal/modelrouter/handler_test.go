package modelrouter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Test setup
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func setupTestRouter(store *ModelStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	v1 := r.Group("/v1")
	v1.POST("/chat/completions", handleModelRoute(store, ProtocolOpenAIChat))
	v1.POST("/messages", handleModelRoute(store, ProtocolAnthropicMessages))
	v1.POST("/responses", handleModelRoute(store, ProtocolOpenAIResponses))

	return r
}

// mockUpstreamServer returns an httptest server that echoes back configured responses.
type mockUpstreamServer struct {
	server *httptest.Server

	mu          sync.RWMutex
	statusCode  int
	respBody    []byte
	streamLines []string
	authHeader  string
	lastBody    []byte
}

func newMockUpstreamServer() *mockUpstreamServer {
	m := &mockUpstreamServer{statusCode: http.StatusOK}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.authHeader = r.Header.Get("Authorization")
		if m.authHeader == "" {
			m.authHeader = r.Header.Get("x-api-key")
		}
		body, _ := io.ReadAll(r.Body)
		m.lastBody = body
		m.mu.Unlock()

		m.mu.RLock()
		code := m.statusCode
		streamLines := m.streamLines
		respBody := m.respBody
		m.mu.RUnlock()

		if len(streamLines) > 0 {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for _, line := range streamLines {
				fmt.Fprint(w, line)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
			return
		}

		w.WriteHeader(code)
		if len(respBody) > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.Write(respBody)
		}
	}))
	return m
}

func (m *mockUpstreamServer) Close() { m.server.Close() }

func (m *mockUpstreamServer) setResponse(code int, body []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusCode = code
	m.respBody = body
	m.streamLines = nil
}

func (m *mockUpstreamServer) setStreamResponse(lines []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusCode = http.StatusOK
	m.streamLines = lines
	m.respBody = nil
}

func (m *mockUpstreamServer) getAuthHeader() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.authHeader
}

func (m *mockUpstreamServer) getLastBody() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastBody
}
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Handler Tests — Non-stream
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestHandler_ChatToChat_NonStream(t *testing.T) {
	mock := newMockUpstreamServer()
	defer mock.Close()

	chatResp := []byte(`{"id":"chatcmpl-001","object":"chat.completion","created":1700000000,"model":"gpt-5","choices":[{"index":0,"message":{"role":"assistant","content":"Hello from GPT!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	mock.setResponse(200, chatResp)

	store := &ModelStore{}
	store.Put(UpstreamConfig{
		ModelName:    "gpt-5",
		Provider:     "openai",
		BaseURL:      mock.server.URL,
		BaseURLHasV1: true,
		APIKey:       "test-key",
		Protocol:     ProtocolOpenAIChat,
		MaxTokens:    4096,
		TimeoutSec:   30,
	})

	router := setupTestRouter(store)
	reqBody := `{"model":"gpt-5","messages":[{"role":"user","content":"Hi"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}

	if resp["id"] != "chatcmpl-001" {
		t.Errorf("expected id chatcmpl-001, got %v", resp["id"])
	}
}

func TestHandler_ChatToChat_AuthHeader(t *testing.T) {
	mock := newMockUpstreamServer()
	defer mock.Close()

	chatResp := []byte(`{"id":"chatcmpl-auth","object":"chat.completion","created":1700000000,"model":"gpt-5","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"OK"}}]}`)
	mock.setResponse(200, chatResp)

	store := &ModelStore{}
	store.Put(UpstreamConfig{
		ModelName:    "gpt-5",
		Provider:     "openai",
		BaseURL:      mock.server.URL,
		BaseURLHasV1: true,
		APIKey:       "sk-test-key",
		Protocol:     ProtocolOpenAIChat,
		MaxTokens:    4096,
		TimeoutSec:   30,
	})

	router := setupTestRouter(store)
	reqBody := `{"model":"gpt-5","messages":[{"role":"user","content":"Hi"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if auth := mock.getAuthHeader(); auth != "Bearer sk-test-key" {
		t.Errorf("expected Bearer sk-test-key, got %q", auth)
	}
}

func TestHandler_ChatToAnthropic_AuthHeader(t *testing.T) {
	mock := newMockUpstreamServer()
	defer mock.Close()

	antResp := []byte(`{"id":"msg_auth_ant","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"Hello from Claude!"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5}}`)
	mock.setResponse(200, antResp)

	store := &ModelStore{}
	store.Put(UpstreamConfig{
		ModelName:    "gpt-5",
		Provider:     "anthropic",
		BaseURL:      mock.server.URL,
		BaseURLHasV1: true,
		APIKey:       "sk-ant-test",
		Protocol:     ProtocolAnthropicMessages,
		MaxTokens:    4096,
		TimeoutSec:   30,
	})

	router := setupTestRouter(store)
	reqBody := `{"model":"gpt-5","messages":[{"role":"user","content":"Hi"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if auth := mock.getAuthHeader(); auth != "sk-ant-test" {
		t.Errorf("expected x-api-key header sk-ant-test, got %q", auth)
	}
}

func TestHandler_ChatToAnthropic_NonStream(t *testing.T) {
	mock := newMockUpstreamServer()
	defer mock.Close()

	antResp := []byte(`{"id":"msg_001","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"Hello from Claude!"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5}}`)
	mock.setResponse(200, antResp)

	store := &ModelStore{}
	store.Put(UpstreamConfig{
		ModelName:    "gpt-5",
		Provider:     "anthropic",
		BaseURL:      mock.server.URL,
		BaseURLHasV1: true,
		APIKey:       "sk-ant-test",
		Protocol:     ProtocolAnthropicMessages,
		MaxTokens:    4096,
		TimeoutSec:   30,
	})

	router := setupTestRouter(store)
	reqBody := `{"model":"gpt-5","messages":[{"role":"user","content":"Hi"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}

	if resp["object"] != "chat.completion" {
		t.Errorf("expected chat.completion, got %v", resp["object"])
	}

	choices, ok := resp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		t.Fatal("expected choices in response")
	}
	choice := choices[0].(map[string]interface{})
	msg := choice["message"].(map[string]interface{})
	if msg["role"] != "assistant" {
		t.Errorf("expected assistant role, got %v", msg["role"])
	}
	if msg["content"] != "Hello from Claude!" {
		t.Errorf("expected 'Hello from Claude!', got %v", msg["content"])
	}
}

func TestHandler_AnthropicToChat_NonStream(t *testing.T) {
	mock := newMockUpstreamServer()
	defer mock.Close()

	chatResp := []byte(`{"id":"chatcmpl-002","object":"chat.completion","created":1700000000,"model":"gpt-5","choices":[{"index":0,"message":{"role":"assistant","content":"Hello from GPT via Anthropic!"},"finish_reason":"stop"}]}`)
	mock.setResponse(200, chatResp)

	store := &ModelStore{}
	store.Put(UpstreamConfig{
		ModelName:    "claude-sonnet-4-20250514",
		Provider:     "openai",
		BaseURL:      mock.server.URL,
		BaseURLHasV1: true,
		APIKey:       "sk-test",
		Protocol:     ProtocolOpenAIChat,
		MaxTokens:    4096,
		TimeoutSec:   30,
	})

	router := setupTestRouter(store)
	reqBody := `{"model":"claude-sonnet-4-20250514","max_tokens":4096,"messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}

	if resp["type"] != "message" {
		t.Errorf("expected type message, got %v", resp["type"])
	}
	if resp["model"] != "gpt-5" {
		t.Errorf("expected model gpt-5, got %v", resp["model"])
	}
}

func TestHandler_ResponsesToChat_NonStream(t *testing.T) {
	mock := newMockUpstreamServer()
	defer mock.Close()

	chatResp := []byte(`{"id":"chatcmpl-resp","object":"chat.completion","created":1700000000,"model":"gpt-5","choices":[{"index":0,"message":{"role":"assistant","content":"Hello from Chat!"},"finish_reason":"stop"}]}`)
	mock.setResponse(200, chatResp)

	store := &ModelStore{}
	store.Put(UpstreamConfig{
		ModelName:    "gpt-5",
		Provider:     "openai",
		BaseURL:      mock.server.URL,
		BaseURLHasV1: true,
		APIKey:       "sk-test",
		Protocol:     ProtocolOpenAIChat,
		MaxTokens:    4096,
		TimeoutSec:   30,
	})

	router := setupTestRouter(store)
	reqBody := `{"model":"gpt-5","input":"Hello from Responses!"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}

	if resp["object"] != "response" {
		t.Errorf("expected object=response, got %v", resp["object"])
	}
}

func TestHandler_InvalidJSON(t *testing.T) {
	store := &ModelStore{}
	store.Put(UpstreamConfig{
		ModelName:    "gpt-5",
		Provider:     "openai",
		BaseURL:      "http://localhost:1",
		BaseURLHasV1: true,
		APIKey:       "sk-test",
		Protocol:     ProtocolOpenAIChat,
		MaxTokens:    4096,
		TimeoutSec:   30,
	})

	router := setupTestRouter(store)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestHandler_ModelNotFound(t *testing.T) {
	store := &ModelStore{}
	router := setupTestRouter(store)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"unknown-model","messages":[{"role":"user","content":"Hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown model, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_UpstreamError(t *testing.T) {
	mock := newMockUpstreamServer()
	defer mock.Close()

	errBody := []byte(`{"error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`)

	mock.mu.Lock()
	mock.statusCode = 429
	mock.respBody = errBody
	mock.streamLines = nil
	mock.mu.Unlock()

	store := &ModelStore{}
	store.Put(UpstreamConfig{
		ModelName:    "gpt-5",
		Provider:     "openai",
		BaseURL:      mock.server.URL,
		BaseURLHasV1: true,
		APIKey:       "sk-test",
		Protocol:     ProtocolOpenAIChat,
		MaxTokens:    4096,
		TimeoutSec:   30,
	})

	router := setupTestRouter(store)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-5","messages":[{"role":"user","content":"Hi"}],"stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	errObj := resp["error"].(map[string]interface{})
	if errObj["message"] != "Rate limit exceeded" {
		t.Errorf("expected 'Rate limit exceeded', got %v", errObj["message"])
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Handler Tests — Stream
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestHandler_ChatToChat_StreamRaw(t *testing.T) {
	mock := newMockUpstreamServer()
	defer mock.Close()

	streamLines := []string{
		"data: {\"id\":\"chatcmpl-str-001\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n",
		"data: {\"id\":\"chatcmpl-str-001\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
		"data: [DONE]\n\n",
	}
	mock.setStreamResponse(streamLines)

	store := &ModelStore{}
	store.Put(UpstreamConfig{
		ModelName:    "gpt-5",
		Provider:     "openai",
		BaseURL:      mock.server.URL,
		BaseURLHasV1: true,
		APIKey:       "sk-test",
		Protocol:     ProtocolOpenAIChat,
		MaxTokens:    4096,
		TimeoutSec:   30,
	})

	router := setupTestRouter(store)
	reqBody := `{"model":"gpt-5","messages":[{"role":"user","content":"Hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "chat.completion.chunk") {
		t.Errorf("expected stream chunks in response, got: %s", body)
	}
	if !strings.Contains(body, "[DONE]") {
		t.Error("expected [DONE] in stream response")
	}
}

func TestHandler_ChatToAnthropic_Stream(t *testing.T) {
	mock := newMockUpstreamServer()
	defer mock.Close()

	streamLines := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-20250514\",\"content\":[],\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Bonjour\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":5}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	mock.setStreamResponse(streamLines)

	store := &ModelStore{}
	store.Put(UpstreamConfig{
		ModelName:    "gpt-5",
		Provider:     "anthropic",
		BaseURL:      mock.server.URL,
		BaseURLHasV1: true,
		APIKey:       "sk-ant-stream",
		Protocol:     ProtocolAnthropicMessages,
		MaxTokens:    4096,
		TimeoutSec:   30,
	})

	router := setupTestRouter(store)
	reqBody := `{"model":"gpt-5","messages":[{"role":"user","content":"Salut"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "[DONE]") {
		t.Error("expected [DONE] in stream response")
	}
	if !strings.Contains(body, "data:") {
		t.Error("expected data: prefix for Chat SSE")
	}
}

func TestHandler_AnthropicToChat_Stream(t *testing.T) {
	mock := newMockUpstreamServer()
	defer mock.Close()

	streamLines := []string{
		"data: {\"id\":\"chatcmpl-str-002\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\n",
		"data: {\"id\":\"chatcmpl-str-002\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n",
		"data: {\"id\":\"chatcmpl-str-002\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\" World\"}}]}\n\n",
		"data: {\"id\":\"chatcmpl-str-002\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
		"data: [DONE]\n\n",
	}
	mock.setStreamResponse(streamLines)

	store := &ModelStore{}
	store.Put(UpstreamConfig{
		ModelName:    "claude-sonnet-4-20250514",
		Provider:     "openai",
		BaseURL:      mock.server.URL,
		BaseURLHasV1: true,
		APIKey:       "sk-test",
		Protocol:     ProtocolOpenAIChat,
		MaxTokens:    4096,
		TimeoutSec:   30,
	})

	router := setupTestRouter(store)
	reqBody := `{"model":"claude-sonnet-4-20250514","max_tokens":4096,"messages":[{"role":"user","content":"Hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "[DONE]") {
		t.Error("expected [DONE] in stream response")
	}
	if !strings.Contains(body, "event:") {
		t.Error("expected event: prefix for Anthropic SSE")
	}
}

func TestHandler_ResponsesToChat_Stream(t *testing.T) {
	mock := newMockUpstreamServer()
	defer mock.Close()

	streamLines := []string{
		"data: {\"id\":\"chatcmpl-str-003\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\n",
		"data: {\"id\":\"chatcmpl-str-003\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"Qubits\"}}]}\n\n",
		"data: {\"id\":\"chatcmpl-str-003\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
		"data: [DONE]\n\n",
	}
	mock.setStreamResponse(streamLines)

	store := &ModelStore{}
	store.Put(UpstreamConfig{
		ModelName:    "gpt-5",
		Provider:     "openai",
		BaseURL:      mock.server.URL,
		BaseURLHasV1: true,
		APIKey:       "sk-test",
		Protocol:     ProtocolOpenAIChat,
		MaxTokens:    4096,
		TimeoutSec:   30,
	})

	router := setupTestRouter(store)
	reqBody := `{"model":"gpt-5","input":"Explain quantum computing","stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "[DONE]") {
		t.Error("expected [DONE] in stream response")
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// ModelStore tests
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestModelStore_PutAndResolve(t *testing.T) {
	store := &ModelStore{}
	cfg := UpstreamConfig{
		ModelName: "gpt-5",
		Provider:  "openai",
		BaseURL:   "https://api.openai.com",
		APIKey:    "sk-test",
		Protocol:  ProtocolOpenAIChat,
		MaxTokens: 4096,
	}
	store.Put(cfg)

	resolved, err := store.Resolve("gpt-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Provider != "openai" {
		t.Errorf("expected provider openai, got %s", resolved.Provider)
	}
	if resolved.Protocol != ProtocolOpenAIChat {
		t.Errorf("expected protocol openai_chat, got %s", resolved.Protocol)
	}
}

func TestModelStore_ResolveNotFound(t *testing.T) {
	store := &ModelStore{}
	_, err := store.Resolve("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent model")
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Format test
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestFormatSSE(t *testing.T) {
	tests := []struct {
		name      string
		proto     Protocol
		eventType string
		data      []byte
		expected  string
	}{
		{
			name:      "OpenAI Chat format",
			proto:     ProtocolOpenAIChat,
			eventType: "",
			data:      []byte(`{"choices":[]}`),
			expected:  "data: {\"choices\":[]}\n\n",
		},
		{
			name:      "Anthropic format",
			proto:     ProtocolAnthropicMessages,
			eventType: "content_block_delta",
			data:      []byte(`{"delta":{"text":"hi"}}`),
			expected:  "event: content_block_delta\ndata: {\"delta\":{\"text\":\"hi\"}}\n\n",
		},
		{
			name:      "Responses format",
			proto:     ProtocolOpenAIResponses,
			eventType: "response.output_text.delta",
			data:      []byte(`{"delta":"hi"}`),
			expected:  "event: response.output_text.delta\ndata: {\"delta\":\"hi\"}\n\n",
		},
		{
			name:      "Gemini format",
			proto:     ProtocolGemini,
			eventType: "gemini_event",
			data:      []byte(`{}`),
			expected:  "event: gemini_event\ndata: {}\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatSSE(tt.proto, tt.eventType, tt.data)
			if string(result) != tt.expected {
				t.Errorf("formatSSE() = %q, want %q", string(result), tt.expected)
			}
		})
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Gemini path extraction tests
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestExtractGeminiModelFromPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/gemini-2.0-flash:generateContent", "gemini-2.0-flash"},
		{"/gemini-pro:streamGenerateContent", "gemini-pro"},
		{"/models/gemini-1.5-pro:generateContent", "models/gemini-1.5-pro"},
		{"noColon", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractGeminiModelFromPath(tt.input)
		if got != tt.want {
			t.Errorf("extractGeminiModelFromPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
