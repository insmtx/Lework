package modelrouter

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/pkg/llmprotocol"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// ModelStore — minimal in-handler model config resolution
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ModelStore holds UpstreamConfig entries keyed by model name.
// It is safe for concurrent use.
type ModelStore struct {
	configs    map[string]*UpstreamConfig
	httpClient *http.Client
	mu         sync.RWMutex
}

// NewModelStore creates an isolated model routing store.
func NewModelStore(httpClients ...*http.Client) *ModelStore {
	store := &ModelStore{configs: make(map[string]*UpstreamConfig)}
	if len(httpClients) > 0 {
		store.httpClient = httpClients[0]
	}
	return store
}

// Put registers an upstream configuration for a model.
// If cfg.Protocol is not explicitly set and cfg.Provider is non-empty,
// the protocol is inferred from the provider.
func (s *ModelStore) Put(cfg UpstreamConfig) {
	if cfg.Protocol == "" && cfg.Provider != "" {
		cfg.Protocol = protocolForProvider(cfg.Provider)
	}
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
	cp := *cfg
	cp.httpClient = s.httpClient
	return &cp, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Route Registration
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// RegisterRoutes registers model routing endpoints on the given Gin router.
// Each endpoint supports all entry protocols and transparently converts
// between protocols when upstream targets a different protocol.
func RegisterRoutes(r gin.IRouter, store *ModelStore) {
	if store == nil {
		return
	}
	// NOTE: caller (worker/router) already mounts under /v1/ prefix.
	// Register directly on the given router to avoid double-wrapping.
	r.POST("/chat/completions", handleModelRoute(store, llmprotocol.ProtocolOpenAIChat))
	r.POST("/messages", handleModelRoute(store, llmprotocol.ProtocolAnthropicMessages))
	r.POST("/responses", handleModelRoute(store, llmprotocol.ProtocolOpenAIResponses))
	// Gemini: use wildcard because Gin cannot handle ":model:generateContent" in one segment
	r.POST("/models/*modelAction", handleModelRoute(store, llmprotocol.ProtocolGemini))
}

// handleModelRoute returns a Gin handler that routes model requests through protocol conversion.
func handleModelRoute(store *ModelStore, entryProtocol llmprotocol.Protocol) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, "failed to read request body"))
			return
		}

		// 调试日志器 — 通过环境变量 LEROS_MODELROUTER_DEBUG=true 启用
		debugEnabled := os.Getenv("LEROS_MODELROUTER_DEBUG") == "true"
		dl := NewDebugLogger(debugEnabled)
		defer dl.Close()

		dl.LogOriginalRequest(body)

		model := extractModelField(body)
		// Gemini: model name may come from URL path instead of request body
		if model == "" && entryProtocol == llmprotocol.ProtocolGemini {
			model = extractGeminiModelFromPath(c.Param("modelAction"))
		}
		if model == "" {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, "model field is required"))
			return
		}

		cfg, err := store.Resolve(model)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, err.Error()))
			return
		}

		isStream := isStreamRequest(body)
		dl.LogRequestMeta(entryProtocol, cfg.Protocol, model, isStream)

		// ── Normalize request against target capabilities ──
		var raw map[string]interface{}
		if err := sonic.Unmarshal(body, &raw); err != nil {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, "invalid JSON request body"))
			return
		}

		entryAdapter, err := llmprotocol.GetAdapter(entryProtocol)
		if err != nil {
			c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, "entry protocol adapter not available"))
			return
		}

		ir, err := entryAdapter.DecodeRequest(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, fmt.Sprintf("decode request: %v", err)))
			return
		}
		dl.LogIRDecoded(ir)

		upstreamProtocol := cfg.Protocol
		targetCaps := llmprotocol.CapabilitiesForProtocol(upstreamProtocol)
		normalizedIR, _, err := llmprotocol.NormalizeRequest(ir, targetCaps)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, fmt.Sprintf("request incompatible with target protocol: %v", err)))
			return
		}
		dl.LogIRNormalized(normalizedIR)

		// Set upstream model name
		normalizedIR.Model = cfg.ModelName

		upstreamAdapter, err := llmprotocol.GetAdapter(upstreamProtocol)
		if err != nil {
			c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, "upstream protocol adapter not available"))
			return
		}

		upstreamBody, err := upstreamAdapter.EncodeRequest(normalizedIR)
		if err != nil {
			c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, fmt.Sprintf("encode upstream request: %v", err)))
			return
		}

		upstreamBodyBytes, err := marshalJSON(upstreamBody)
		if err != nil {
			c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, "marshal upstream body failed"))
			return
		}
		dl.LogUpstreamRequest(upstreamBodyBytes)

		if isStream {
			handleStreamResponse(c, cfg, upstreamBodyBytes, entryProtocol, upstreamProtocol, entryAdapter, upstreamAdapter, dl)
		} else {
			handleNonStreamResponse(c, cfg, upstreamBodyBytes, entryProtocol, upstreamProtocol, entryAdapter, upstreamAdapter, dl)
		}
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Non-stream response handling
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func handleNonStreamResponse(
	c *gin.Context,
	cfg *UpstreamConfig,
	body []byte,
	entryProtocol, upstreamProtocol llmprotocol.Protocol,
	entryAdapter, upstreamAdapter llmprotocol.ProtocolAdapter,
	dl *DebugLogger,
) {
	respBody, err := doUpstreamCall(c.Request.Context(), cfg, body)
	if err != nil {
		dl.LogError("upstream_call", err)
		handleUpstreamError(c, entryProtocol, err)
		return
	}

	dl.LogUpstreamResponse(respBody)

	var rawResp map[string]interface{}
	if err := sonic.Unmarshal(respBody, &rawResp); err != nil {
		c.JSON(http.StatusBadGateway, newEntryError(entryProtocol, "invalid upstream response"))
		return
	}

	irResp, err := upstreamAdapter.DecodeResponse(rawResp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, fmt.Sprintf("decode upstream response: %v", err)))
		return
	}

	entryBody, err := entryAdapter.EncodeResponse(irResp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, fmt.Sprintf("encode entry response: %v", err)))
		return
	}

	entryBytes, err := marshalJSON(entryBody)
	if err != nil {
		c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, "marshal entry response failed"))
		return
	}

	dl.LogEntryResponse(entryBytes)
	c.Data(http.StatusOK, "application/json", entryBytes)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Stream response handling
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func handleStreamResponse(
	c *gin.Context,
	cfg *UpstreamConfig,
	body []byte,
	entryProtocol, upstreamProtocol llmprotocol.Protocol,
	entryAdapter, upstreamAdapter llmprotocol.ProtocolAdapter,
	dl *DebugLogger,
) {
	reader, err := doUpstreamStreamCall(c.Request.Context(), cfg, body)
	if err != nil {
		dl.LogError("upstream_stream_call", err)
		handleUpstreamError(c, entryProtocol, err)
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
		pipeRawSSE(c, reader, dl)
	} else {
		pipeConvertedSSE(c, reader, entryProtocol, upstreamProtocol, entryAdapter, upstreamAdapter, dl)
	}
}

func pipeRawSSE(c *gin.Context, reader io.Reader, dl *DebugLogger) {
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

func pipeConvertedSSE(
	c *gin.Context,
	reader io.Reader,
	entryProtocol, upstreamProtocol llmprotocol.Protocol,
	entryAdapter, upstreamAdapter llmprotocol.ProtocolAdapter,
	dl *DebugLogger,
) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	upstreamState := upstreamAdapter.NewStreamState()
	entryState := entryAdapter.NewStreamState()
	aggregator := llmprotocol.NewStreamAggregator()

	var currentEventType string
	var currentData strings.Builder

	// writeIREvents encodes a slice of IR stream events through the entry
	// adapter and writes the resulting SSE payloads to the client.
	// All writes are logged via the debug logger.  If an llmprotocol.IRStreamDone event
	// is encountered and the entry protocol is Chat, a trailing [DONE] is
	// appended automatically.
	writeIREvents := func(events []*llmprotocol.IRStreamEvent) {
		for _, evt := range events {
			payloads, err := entryAdapter.EncodeStreamEvent(evt, entryState)
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
				formatted := formatSSE(entryProtocol, evtType, payloadBytes)
				dl.LogEntryStreamChunk(formatted)
				if _, err := c.Writer.Write(formatted); err != nil {
					return
				}
				c.Writer.Flush()
			}

			if evt.Type == llmprotocol.IRStreamDone && entryProtocol == llmprotocol.ProtocolOpenAIChat {
				dl.LogEntryStreamChunk([]byte("data: [DONE]\n\n"))
				_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
				c.Writer.Flush()
			}
		}
	}

	flushEvent := func() {
		if currentData.Len() == 0 {
			return
		}

		dataStr := currentData.String()
		currentData.Reset()

		var rawUpstream map[string]interface{}
		if err := sonic.Unmarshal([]byte(dataStr), &rawUpstream); err != nil {
			return
		}

		irEvents, err := upstreamAdapter.DecodeStreamEvent(rawUpstream, upstreamState)
		if err != nil {
			return
		}

		for _, irEvt := range irEvents {
			fixedEvents := aggregator.ProcessIREvent(irEvt)
			writeIREvents(fixedEvents)
		}

		currentEventType = ""
	}

	// finalizeStream commits the target protocol stream.
	// The [DONE] marker (if needed) is emitted by writeIREvents when it
	// encounters llmprotocol.IRStreamDone for a Chat entry protocol.
	finalizeStream := func() {
		writeIREvents(aggregator.Finalize())
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
				finalizeStream()

				// Upstream is Chat SSE — client expects [DONE] regardless
				// of entry protocol.
				if upstreamProtocol == llmprotocol.ProtocolOpenAIChat {
					dl.LogEntryStreamChunk([]byte("data: [DONE]\n\n"))
					_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
					c.Writer.Flush()
				}
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

	// Upstream stream ended without [DONE] (e.g. connection dropped).
	// Finalize is idempotent — safe to call even if [DONE] was already processed.
	if !aggregator.IsDone() {
		finalizeStream()
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SSE formatting
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// formatSSE formats an SSE message according to the protocol.
func formatSSE(proto llmprotocol.Protocol, eventType string, data []byte) []byte {
	switch proto {
	case llmprotocol.ProtocolOpenAIChat:
		return []byte(fmt.Sprintf("data: %s\n\n", string(data)))
	default: // Anthropic, Responses, Gemini use event: header
		return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(data)))
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Upstream HTTP calls
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// setUpstreamRequest creates an HTTP request for the upstream call.
func setUpstreamRequest(ctx context.Context, cfg *UpstreamConfig, body []byte) (*http.Request, error) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	apiPath := llmprotocol.UpstreamAPIPath(cfg.Protocol, cfg.BaseURLHasV1)
	url := baseURL + apiPath

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	switch cfg.Protocol {
	case llmprotocol.ProtocolAnthropicMessages:
		req.Header.Set("x-api-key", cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	return req, nil
}

// doUpstreamCall executes a non-streaming upstream call.
func doUpstreamCall(ctx context.Context, cfg *UpstreamConfig, body []byte) ([]byte, error) {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	client := cfg.httpClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	req, err := setUpstreamRequest(ctx, cfg, body)
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
		return nil, &upstreamError{
			StatusCode: resp.StatusCode,
			Body:       respBody,
		}
	}

	return respBody, nil
}

// doUpstreamStreamCall executes a streaming upstream call.
func doUpstreamStreamCall(ctx context.Context, cfg *UpstreamConfig, body []byte) (io.ReadCloser, error) {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 180 * time.Second
	}

	client := cfg.httpClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	req, err := setUpstreamRequest(ctx, cfg, body)
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
		return nil, &upstreamError{
			StatusCode: resp.StatusCode,
			Body:       respBody,
		}
	}

	return resp.Body, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Error handling
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// upstreamError represents an error from an upstream provider.
type upstreamError struct {
	StatusCode int
	Body       []byte
}

func (e *upstreamError) Error() string {
	return fmt.Sprintf("upstream returned status %d: %s", e.StatusCode, string(e.Body))
}

// handleUpstreamError maps upstream errors to entry protocol error responses.
func handleUpstreamError(c *gin.Context, entryProtocol llmprotocol.Protocol, err error) {
	var upErr *upstreamError
	if !isUpstreamError(err, &upErr) {
		c.JSON(http.StatusBadGateway, newEntryError(entryProtocol, fmt.Sprintf("upstream request failed: %v", err)))
		return
	}

	statusCode := upErr.StatusCode
	if statusCode >= 500 {
		statusCode = http.StatusBadGateway
	}

	entryBody := parseAndEncodeError(upErr.Body, upErr.StatusCode, entryProtocol)
	c.JSON(statusCode, entryBody)
}

func isUpstreamError(err error, target **upstreamError) bool {
	if target == nil {
		return false
	}
	var ue *upstreamError
	ok := fmt.Sprintf("%T", err) == "*modelrouter.upstreamError"
	if !ok {
		return false
	}
	ue = err.(*upstreamError)
	*target = ue
	return true
}

// parseAndEncodeError parses an upstream error body and encodes it for the entry protocol.
func parseAndEncodeError(body []byte, statusCode int, entryProtocol llmprotocol.Protocol) interface{} {
	message := fmt.Sprintf("upstream returned status %d", statusCode)
	errType := "upstream_error"

	if len(body) > 0 {
		var raw map[string]interface{}
		if err := sonic.Unmarshal(body, &raw); err == nil {
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

	return encodeErrorForProtocol(message, errType, entryProtocol)
}

// encodeErrorForProtocol encodes an error message and type into the entry protocol's error format.
func encodeErrorForProtocol(message, errType string, proto llmprotocol.Protocol) interface{} {
	switch proto {
	case llmprotocol.ProtocolAnthropicMessages:
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

// newEntryError creates an entry protocol error response.
func newEntryError(proto llmprotocol.Protocol, message string) interface{} {
	return encodeErrorForProtocol(message, "invalid_request_error", proto)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Request parsing helpers
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func extractModelField(body []byte) string {
	var raw struct {
		Model string `json:"model"`
	}
	if err := sonic.Unmarshal(body, &raw); err != nil {
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

func isStreamRequest(body []byte) bool {
	var raw struct {
		Stream bool `json:"stream"`
	}
	if err := sonic.Unmarshal(body, &raw); err != nil {
		return false
	}
	return raw.Stream
}

func marshalJSON(v interface{}) ([]byte, error) {
	return sonic.ConfigStd.Marshal(v)
}

func getString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
