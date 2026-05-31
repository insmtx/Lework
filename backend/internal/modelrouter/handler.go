package modelrouter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// ModelStore — minimal in-handler model config resolution
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ModelStore holds UpstreamConfig entries keyed by model name.
// It is safe for concurrent use.
type ModelStore struct {
	configs map[string]*UpstreamConfig
	mu      sync.RWMutex
}

// Put registers an upstream configuration for a model.
func (s *ModelStore) Put(cfg UpstreamConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.configs == nil {
		s.configs = make(map[string]*UpstreamConfig)
	}
	cp := cfg
	s.configs[cfg.ModelName] = &cp
}

// Resolve returns the UpstreamConfig for the given model name.
func (s *ModelStore) Resolve(model string) (*UpstreamConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.configs[model]
	if !ok {
		return nil, fmt.Errorf("modelrouter: no upstream config for model %q", model)
	}
	return cfg, nil
}

// defaultStoreV2 is the package-level singleton ModelStore.
// RegisterRoutes and lifecycle steps share this same instance.
var defaultStoreV2 = &ModelStore{configs: make(map[string]*UpstreamConfig)}

// DefaultStore returns the singleton ModelStore.
func DefaultStore() *ModelStore {
	return defaultStoreV2
}

// ResetStore replaces the singleton store with a fresh instance. Use only in tests.
func ResetStore() {
	defaultStoreV2 = &ModelStore{configs: make(map[string]*UpstreamConfig)}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Route Registration
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// RegisterRoutes registers v2 model routing endpoints on the given Gin router.
// Each endpoint supports all entry protocols and transparently converts
// between protocols when upstream targets a different protocol.
func RegisterRoutes(r gin.IRouter) {
	store := DefaultStore()

	// NOTE: caller (worker/router) already mounts under /v1/ prefix.
	// Register directly on the given router to avoid double-wrapping.
	r.POST("/chat/completions", handleModelRoute(store, ProtocolOpenAIChat))
	r.POST("/messages", handleModelRoute(store, ProtocolAnthropicMessages))
	r.POST("/responses", handleModelRoute(store, ProtocolOpenAIResponses))
	// Gemini: use wildcard because Gin cannot handle ":model:generateContent" in one segment
	r.POST("/models/*modelAction", handleModelRoute(store, ProtocolGemini))
}

// handleModelRoute returns a Gin handler that routes model requests through protocol conversion.
func handleModelRoute(store *ModelStore, entryProtocol Protocol) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryErrorV2(entryProtocol, "failed to read request body"))
			return
		}

		// 调试日志器 — 通过环境变量 LEROS_MODELROUTER_DEBUG=true 启用
		debugEnabled := os.Getenv("LEROS_MODELROUTER_DEBUG") == "true"
		dl := NewDebugLogger(debugEnabled)
		defer dl.Close()

		dl.LogOriginalRequest(body)

		model := extractModelFieldV2(body)
		// Gemini: model name may come from URL path instead of request body
		if model == "" && entryProtocol == ProtocolGemini {
			model = extractGeminiModelFromPath(c.Param("modelAction"))
		}
		if model == "" {
			c.JSON(http.StatusBadRequest, newEntryErrorV2(entryProtocol, "model field is required"))
			return
		}

		cfg, err := store.Resolve(model)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryErrorV2(entryProtocol, err.Error()))
			return
		}

		isStream := isStreamRequestV2(body)
		dl.LogRequestMeta(entryProtocol, cfg.Protocol, model, isStream)

		// ── Normalize request against target capabilities ──
		var raw map[string]interface{}
		if err := json.Unmarshal(body, &raw); err != nil {
			c.JSON(http.StatusBadRequest, newEntryErrorV2(entryProtocol, "invalid JSON request body"))
			return
		}

		entryAdapter, err := GetAdapter(entryProtocol)
		if err != nil {
			c.JSON(http.StatusInternalServerError, newEntryErrorV2(entryProtocol, "entry protocol adapter not available"))
			return
		}

		ir, err := entryAdapter.DecodeRequest(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryErrorV2(entryProtocol, fmt.Sprintf("decode request: %v", err)))
			return
		}
		dl.LogIRDecoded(ir)

		upstreamProtocol := cfg.Protocol
		targetCaps := capabilitiesForProtocol(upstreamProtocol)
		normalizedIR, _, err := NormalizeRequest(ir, targetCaps)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryErrorV2(entryProtocol, fmt.Sprintf("request incompatible with target protocol: %v", err)))
			return
		}
		dl.LogIRNormalized(normalizedIR)

		// Set upstream model name
		normalizedIR.Model = cfg.ModelName

		upstreamAdapter, err := GetAdapter(upstreamProtocol)
		if err != nil {
			c.JSON(http.StatusInternalServerError, newEntryErrorV2(entryProtocol, "upstream protocol adapter not available"))
			return
		}

		upstreamBody, err := upstreamAdapter.EncodeRequest(normalizedIR)
		if err != nil {
			c.JSON(http.StatusInternalServerError, newEntryErrorV2(entryProtocol, fmt.Sprintf("encode upstream request: %v", err)))
			return
		}

		upstreamBodyBytes, err := marshalJSON(upstreamBody)
		if err != nil {
			c.JSON(http.StatusInternalServerError, newEntryErrorV2(entryProtocol, "marshal upstream body failed"))
			return
		}
		dl.LogUpstreamRequest(upstreamBodyBytes)

		if isStream {
			handleStreamResponseV2(c, cfg, upstreamBodyBytes, entryProtocol, upstreamProtocol, entryAdapter, upstreamAdapter, dl)
		} else {
			handleNonStreamResponseV2(c, cfg, upstreamBodyBytes, entryProtocol, upstreamProtocol, entryAdapter, upstreamAdapter, dl)
		}
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Non-stream response handling
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func handleNonStreamResponseV2(
	c *gin.Context,
	cfg *UpstreamConfig,
	body []byte,
	entryProtocol, upstreamProtocol Protocol,
	entryAdapter, upstreamAdapter ProtocolAdapter,
	dl *DebugLogger,
) {
	respBody, err := doUpstreamCallV2(c.Request.Context(), cfg, body)
	if err != nil {
		dl.LogError("upstream_call", err)
		handleUpstreamErrorV2(c, entryProtocol, err)
		return
	}

	dl.LogUpstreamResponse(respBody)

	var rawResp map[string]interface{}
	if err := json.Unmarshal(respBody, &rawResp); err != nil {
		c.JSON(http.StatusBadGateway, newEntryErrorV2(entryProtocol, "invalid upstream response"))
		return
	}

	irResp, err := upstreamAdapter.DecodeResponse(rawResp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, newEntryErrorV2(entryProtocol, fmt.Sprintf("decode upstream response: %v", err)))
		return
	}

	entryBody, err := entryAdapter.EncodeResponse(irResp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, newEntryErrorV2(entryProtocol, fmt.Sprintf("encode entry response: %v", err)))
		return
	}

	entryBytes, err := marshalJSON(entryBody)
	if err != nil {
		c.JSON(http.StatusInternalServerError, newEntryErrorV2(entryProtocol, "marshal entry response failed"))
		return
	}

	dl.LogEntryResponse(entryBytes)
	c.Data(http.StatusOK, "application/json", entryBytes)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Stream response handling
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func handleStreamResponseV2(
	c *gin.Context,
	cfg *UpstreamConfig,
	body []byte,
	entryProtocol, upstreamProtocol Protocol,
	entryAdapter, upstreamAdapter ProtocolAdapter,
	dl *DebugLogger,
) {
	reader, err := doUpstreamStreamCallV2(c.Request.Context(), cfg, body)
	if err != nil {
		dl.LogError("upstream_stream_call", err)
		handleUpstreamErrorV2(c, entryProtocol, err)
		return
	}
	defer reader.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	c.Writer.WriteHeaderNow()
	c.Writer.Flush()

	if entryProtocol == cfg.Protocol {
		pipeRawSSEV2(c, reader, dl)
	} else {
		pipeConvertedSSEV2(c, reader, entryProtocol, upstreamProtocol, entryAdapter, upstreamAdapter, dl)
	}
}

func pipeRawSSEV2(c *gin.Context, reader io.Reader, dl *DebugLogger) {
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			dl.LogStreamChunkSeparator()
			dl.LogUpstreamStreamChunk(chunk)
			if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
				return
			}
			dl.LogEntryStreamChunk(chunk)
			c.Writer.Flush()
		}
		if err != nil {
			return
		}
	}
}

func pipeConvertedSSEV2(
	c *gin.Context,
	reader io.Reader,
	entryProtocol, upstreamProtocol Protocol,
	entryAdapter, upstreamAdapter ProtocolAdapter,
	dl *DebugLogger,
) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	upstreamState := upstreamAdapter.NewStreamState()
	entryState := entryAdapter.NewStreamState()

	var currentEventType string
	var currentData strings.Builder

	flushEvent := func() {
		if currentData.Len() == 0 {
			return
		}

		dataStr := currentData.String()
		currentData.Reset()

		var rawUpstream map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &rawUpstream); err != nil {
			return
		}

		irEvents, err := upstreamAdapter.DecodeStreamEvent(rawUpstream, upstreamState)
		if err != nil {
			return
		}

		for _, irEvt := range irEvents {
			if irEvt.Type == IRStreamDone {
				payloads, err := entryAdapter.EncodeStreamEvent(irEvt, entryState)
				if err == nil {
					for _, payload := range payloads {
						payloadBytes, err := marshalJSON(payload)
						if err != nil {
							continue
						}
						evtType := ""
						if v, ok := payload["type"].(string); ok {
							evtType = v
						}
						formatted := formatSSEV2(entryProtocol, evtType, payloadBytes)
						dl.LogEntryStreamChunk(formatted)
						_, _ = c.Writer.Write(formatted)
						c.Writer.Flush()
					}
				}
				// Anthropic 协议用 message_stop 终止，不需要 [DONE]
				// OpenAI Chat 协议需要 data: [DONE] 作为流结束标志
				if entryProtocol != ProtocolAnthropicMessages {
					dl.LogEntryStreamChunk([]byte("data: [DONE]\n\n"))
					dl.LogStreamChunkSeparator()
					_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
					c.Writer.Flush()
				}
				return
			}

			payloads, err := entryAdapter.EncodeStreamEvent(irEvt, entryState)
			if err != nil {
				continue
			}

			for _, payload := range payloads {
				payloadBytes, err := marshalJSON(payload)
				if err != nil {
					continue
				}

				evtType := currentEventType
				if evtType == "" {
					if v, ok := payload["type"].(string); ok {
						evtType = v
					}
				}

				formatted := formatSSEV2(entryProtocol, evtType, payloadBytes)
				dl.LogEntryStreamChunk(formatted)
				if _, err := c.Writer.Write(formatted); err != nil {
					return
				}
				c.Writer.Flush()
			}
		}

		currentEventType = ""
	}

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				dl.LogStreamChunkSeparator()
				dl.LogUpstreamStreamChunk([]byte("data: [DONE]\n\n"))
				dl.LogStreamChunkSeparator()
				flushEvent()
				// Emit terminal [DONE]
				dl.LogEntryStreamChunk([]byte("data: [DONE]\n\n"))
				dl.LogStreamChunkSeparator()
				_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
				c.Writer.Flush()
				return
			}

			currentData.WriteString(data)
			continue
		}

		if line == "" && currentData.Len() > 0 {
			dl.LogStreamChunkSeparator()
			dl.LogUpstreamStreamChunk([]byte("data: " + currentData.String() + "\n\n"))
			dl.LogStreamChunkSeparator()
			flushEvent()
			dl.LogStreamChunkSeparator()
		}
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SSE formatting
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// formatSSEV2 formats an SSE message according to the protocol.
func formatSSEV2(proto Protocol, eventType string, data []byte) []byte {
	switch proto {
	case ProtocolOpenAIChat:
		return []byte(fmt.Sprintf("data: %s\n\n", string(data)))
	default: // Anthropic, Responses, Gemini use event: header
		return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(data)))
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Upstream HTTP calls (v2-independent)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// setUpstreamRequestV2 creates an HTTP request for the upstream call.
func setUpstreamRequestV2(ctx context.Context, cfg *UpstreamConfig, body []byte) (*http.Request, error) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	apiPath := UpstreamAPIPath(cfg.Protocol, cfg.BaseURLHasV1)
	url := baseURL + apiPath

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	switch cfg.Protocol {
	case ProtocolAnthropicMessages:
		req.Header.Set("x-api-key", cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	return req, nil
}

// doUpstreamCallV2 executes a non-streaming upstream call.
func doUpstreamCallV2(ctx context.Context, cfg *UpstreamConfig, body []byte) ([]byte, error) {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	client := &http.Client{Timeout: timeout}
	req, err := setUpstreamRequestV2(ctx, cfg, body)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read upstream response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &upstreamErrorV2{
			StatusCode: resp.StatusCode,
			Body:       respBody,
		}
	}

	return respBody, nil
}

// doUpstreamStreamCallV2 executes a streaming upstream call.
func doUpstreamStreamCallV2(ctx context.Context, cfg *UpstreamConfig, body []byte) (io.ReadCloser, error) {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 180 * time.Second
	}

	client := &http.Client{Timeout: timeout}
	req, err := setUpstreamRequestV2(ctx, cfg, body)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream stream request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &upstreamErrorV2{
			StatusCode: resp.StatusCode,
			Body:       respBody,
		}
	}

	return resp.Body, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Error handling
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// upstreamErrorV2 represents an error from an upstream provider.
type upstreamErrorV2 struct {
	StatusCode int
	Body       []byte
}

func (e *upstreamErrorV2) Error() string {
	return fmt.Sprintf("upstream returned status %d: %s", e.StatusCode, string(e.Body))
}

// handleUpstreamErrorV2 maps upstream errors to entry protocol error responses.
func handleUpstreamErrorV2(c *gin.Context, entryProtocol Protocol, err error) {
	var upErr *upstreamErrorV2
	if !isUpstreamErrorV2(err, &upErr) {
		c.JSON(http.StatusBadGateway, newEntryErrorV2(entryProtocol, fmt.Sprintf("upstream request failed: %v", err)))
		return
	}

	statusCode := upErr.StatusCode
	if statusCode >= 500 {
		statusCode = http.StatusBadGateway
	}

	entryBody := parseAndEncodeErrorV2(upErr.Body, upErr.StatusCode, entryProtocol)
	c.JSON(statusCode, entryBody)
}

func isUpstreamErrorV2(err error, target **upstreamErrorV2) bool {
	if target == nil {
		return false
	}
	var ue *upstreamErrorV2
	ok := fmt.Sprintf("%T", err) == "*modelrouter.upstreamErrorV2"
	if !ok {
		return false
	}
	ue = err.(*upstreamErrorV2)
	*target = ue
	return true
}

// parseAndEncodeErrorV2 parses an upstream error body and encodes it for the entry protocol.
func parseAndEncodeErrorV2(body []byte, statusCode int, entryProtocol Protocol) interface{} {
	message := fmt.Sprintf("upstream returned status %d", statusCode)
	errType := "upstream_error"

	if len(body) > 0 {
		var raw map[string]interface{}
		if err := json.Unmarshal(body, &raw); err == nil {
			// Anthropic format: {"type": "error", "error": {"type": "...", "message": "..."}}
			if getString(raw, "type") == "error" {
				if errObj, ok := raw["error"].(map[string]interface{}); ok {
					message = getString(errObj, "message")
					errType = getString(errObj, "type")
				}
			} else if errObj, ok := raw["error"].(map[string]interface{}); ok {
				// OpenAI format: {"error": {"type": "...", "message": "...", "code": "..."}}
				message = getString(errObj, "message")
				errType = getString(errObj, "type")
			} else if msg := getString(raw, "message"); msg != "" {
				message = msg
			}
		}
	}

	if message == "" {
		message = string(body)
	}
	if errType == "" {
		errType = "upstream_error"
	}

	return encodeErrorForProtocolV2(message, errType, entryProtocol)
}

// encodeErrorForProtocolV2 encodes an error message and type into the entry protocol's error format.
func encodeErrorForProtocolV2(message, errType string, proto Protocol) interface{} {
	switch proto {
	case ProtocolAnthropicMessages:
		return map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    errType,
				"message": message,
			},
		}
	default:
		return map[string]interface{}{
			"error": map[string]interface{}{
				"message": message,
				"type":    errType,
			},
		}
	}
}

// newEntryErrorV2 creates an entry protocol error response.
func newEntryErrorV2(proto Protocol, message string) interface{} {
	return encodeErrorForProtocolV2(message, "invalid_request_error", proto)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Request parsing helpers
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func extractModelFieldV2(body []byte) string {
	var raw struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	return strings.TrimSpace(raw.Model)
}

// extractGeminiModelFromPath extracts the model name from a Gemini URL action parameter.
// e.g., "/gemini-2.0-flash:generateContent" → "gemini-2.0-flash"
func extractGeminiModelFromPath(action string) string {
	action = strings.TrimPrefix(action, "/")
	colonIdx := strings.LastIndex(action, ":")
	if colonIdx < 0 {
		return ""
	}
	return action[:colonIdx]
}

func isStreamRequestV2(body []byte) bool {
	var raw struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return false
	}
	return raw.Stream
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Protocol capability helpers
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func capabilitiesForProtocol(proto Protocol) CapabilitySet {
	switch proto {
	case ProtocolOpenAIChat:
		return OpenAIChatCapabilities
	case ProtocolOpenAIResponses:
		return OpenAIResponsesCapabilities
	case ProtocolAnthropicMessages:
		return AnthropicMessagesCapabilities
	case ProtocolGemini:
		return GeminiCapabilities
	default:
		return OpenAIChatCapabilities
	}
}
