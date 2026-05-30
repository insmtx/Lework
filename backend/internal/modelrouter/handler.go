package modelrouter

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ygpkg/yg-go/logs"
)

// RegisterRoutes registers model routing endpoints backed by the worker-local model store.
func RegisterRoutes(r gin.IRouter) {
	resolver := NewResolver()

	r.POST("/chat/completions", handleModelRoute(resolver, ProtocolOpenAIChat))
	r.POST("/messages", handleModelRoute(resolver, ProtocolAnthropicMessages))
	r.POST("/responses", handleModelRoute(resolver, ProtocolOpenAIResponses))

	logs.Info("modelrouter: model routing endpoints registered at /v1/chat/completions, /v1/messages, /v1/responses")
}

func handleModelRoute(resolver *Resolver, entryProtocol Protocol) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, "failed to read request body"))
			return
		}

		modelName := extractModelField(body)

		cfg, err := resolver.Resolve(c.Request.Context(), modelName)
		if err != nil {
			logs.Warnf("modelrouter: resolve model failed: %v", err)
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, err.Error()))
			return
		}

		isStream := isStreamRequest(body)

		// logs.Infof("modelrouter: request before protocol conversion entry_protocol=%s upstream_protocol=%s body=%s",
		// entryProtocol, cfg.Protocol, compactJSONForLog(body))

		upstreamBody, err := convertRequest(body, entryProtocol, cfg.Protocol, cfg.ModelName)
		if err != nil {
			logs.Errorf("modelrouter: convert request failed: %v", err)
			status := http.StatusInternalServerError
			if errors.Is(err, errInvalidRequestBody) {
				status = http.StatusBadRequest
			}
			c.JSON(status, newEntryError(entryProtocol, fmt.Sprintf("request conversion failed: %v", err)))
			return
		}

		// logs.Infof("modelrouter: request after protocol conversion entry_protocol=%s upstream_protocol=%s body=%s",
		// entryProtocol, cfg.Protocol, compactJSONForLog(upstreamBody))

		if isStream {
			handleStreamResponse(c, cfg, upstreamBody, entryProtocol)
		} else {
			handleNonStreamResponse(c, cfg, upstreamBody, entryProtocol)
		}
	}
}

func handleNonStreamResponse(c *gin.Context, cfg *UpstreamConfig, body []byte, entryProtocol Protocol) {
	respBody, err := doUpstreamCall(c.Request.Context(), cfg, body)
	if err != nil {
		handleUpstreamError(c, entryProtocol, err)
		return
	}

	converted, err := convertResponse(respBody, entryProtocol, cfg.Protocol)
	if err != nil {
		logs.Errorf("modelrouter: convert response failed: %v", err)
		c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, "response conversion failed"))
		return
	}

	c.Data(http.StatusOK, "application/json", converted)
}

func handleStreamResponse(c *gin.Context, cfg *UpstreamConfig, body []byte, entryProtocol Protocol) {
	reader, err := doUpstreamStreamCall(c.Request.Context(), cfg, body)
	if err != nil {
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
		pipeRawSSE(c, reader)
	} else {
		pipeConvertedSSE(c, reader, entryProtocol, cfg.Protocol)
	}
}

func pipeRawSSE(c *gin.Context, reader io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
				return
			}
			c.Writer.Flush()
		}
		if err != nil {
			return
		}
	}
}

func pipeConvertedSSE(c *gin.Context, reader io.Reader, entryProto, upstreamProto Protocol) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	state := newStreamConversionState()
	var currentEventType string
	var currentData strings.Builder

	flushEvent := func() {
		if currentData.Len() == 0 {
			return
		}

		data := []byte(currentData.String())
		currentData.Reset()

		converted, err := convertStreamEventWithState(data, entryProto, upstreamProto, state)
		if err != nil || len(converted) == 0 {
			return
		}

		var raw struct {
			Type string `json:"type"`
		}
		var evtType string
		if json.Unmarshal(data, &raw) == nil && raw.Type != "" {
			evtType = raw.Type
		} else if currentEventType != "" {
			evtType = currentEventType
		}
		currentEventType = ""

		for _, evt := range converted {
			formatted := formatSSE(entryProto, convertedEventType(evtType, evt), evt)
			if _, err := c.Writer.Write(formatted); err != nil {
				return
			}
			c.Writer.Flush()
		}
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
				flushEvent()
				if entryProto == ProtocolOpenAIResponses && upstreamProto != ProtocolOpenAIResponses {
					for _, evt := range encodeResponsesStreamEventWithState(&IRStreamEvent{Type: IRStreamDone}, state) {
						formatted := formatSSE(entryProto, convertedEventType("response.completed", mustMarshalStreamEvent(evt)), mustMarshalStreamEvent(evt))
						if _, err := c.Writer.Write(formatted); err != nil {
							return
						}
						c.Writer.Flush()
					}
				}
				if entryProto == ProtocolAnthropicMessages {
					for _, event := range state.closeAnthropicOpenBlocks() {
						encoded := encodeAnthropicStreamEvent(event)
						for _, evt := range encoded {
							payload := mustMarshalStreamEvent(evt)
							logs.Infof("modelrouter: stream converted anthropic done prelude data=%s", string(payload))
							formatted := formatSSE(entryProto, convertedEventType("content_block_stop", payload), payload)
							if _, err := c.Writer.Write(formatted); err != nil {
								return
							}
							c.Writer.Flush()
						}
					}
					messageStop := mustMarshalStreamEvent(map[string]interface{}{"type": "message_stop"})
					logs.Infof("modelrouter: stream converted anthropic message_stop data=%s", string(messageStop))
					formatted := formatSSE(entryProto, convertedEventType("message_stop", messageStop), messageStop)
					if _, err := c.Writer.Write(formatted); err != nil {
						return
					}
					c.Writer.Flush()
					return
				}
				_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
				c.Writer.Flush()
				return
			}

			currentData.WriteString(data)
			continue
		}

		if line == "" && currentData.Len() > 0 {
			flushEvent()
		}
	}
}

func mustMarshalStreamEvent(event map[string]interface{}) []byte {
	data, err := json.Marshal(event)
	if err != nil {
		return nil
	}
	return data
}

func convertedEventType(fallback string, data []byte) string {
	var raw struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &raw) == nil && raw.Type != "" {
		return raw.Type
	}
	return fallback
}

func formatSSE(proto Protocol, eventType string, data []byte) []byte {
	switch proto {
	case ProtocolOpenAIChat:
		return []byte(fmt.Sprintf("data: %s\n\n", string(data)))
	case ProtocolOpenAIResponses:
		return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(data)))
	case ProtocolAnthropicMessages:
		return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(data)))
	}
	return data
}

func extractModelField(body []byte) string {
	var raw struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	return strings.TrimSpace(raw.Model)
}

func isStreamRequest(body []byte) bool {
	var raw struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return false
	}
	return raw.Stream
}

func compactJSONForLog(body []byte) string {
	var raw interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return string(body)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return string(body)
	}
	return string(encoded)
}

func handleUpstreamError(c *gin.Context, entryProtocol Protocol, err error) {
	var upErr *upstreamError
	if !errors.As(err, &upErr) {
		c.JSON(http.StatusBadGateway, newEntryError(entryProtocol, fmt.Sprintf("upstream request failed: %v", err)))
		return
	}

	statusCode := upErr.StatusCode
	if statusCode >= 500 {
		statusCode = http.StatusBadGateway
	}

	// 将上游错误转换为入口协议格式
	irErr := parseUpstreamError(upErr.Body, upErr.StatusCode)
	entryBody := encodeIRError(irErr, entryProtocol)
	c.JSON(statusCode, entryBody)
}

// parseUpstreamError parses an upstream error body into a canonical IRError.
func parseUpstreamError(body []byte, statusCode int) *IRError {
	irErr := &IRError{
		StatusCode:  statusCode,
		Type:        IRErrorUpstreamError,
		UpstreamBody: body,
	}

	if len(body) == 0 {
		irErr.Message = fmt.Sprintf("upstream returned status %d", statusCode)
		return irErr
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		irErr.Message = string(body)
		return irErr
	}

	// Anthropic 格式: {"type": "error", "error": {"type": "...", "message": "..."}}
	if getString(raw, "type") == "error" {
		if errObj, ok := raw["error"].(map[string]interface{}); ok {
			irErr.Type = mapAnthropicErrorType(getString(errObj, "type"))
			irErr.Message = getString(errObj, "message")
			irErr.Code = getString(errObj, "type")
		} else {
			irErr.Message = getString(raw, "message")
		}
		return irErr
	}

	// OpenAI 格式: {"error": {"type": "...", "message": "...", "code": "..."}}
	if errObj, ok := raw["error"].(map[string]interface{}); ok {
		irErr.Type = mapOpenAIErrorType(getString(errObj, "type"))
		irErr.Message = getString(errObj, "message")
		irErr.Code = getString(errObj, "code")
		return irErr
	}

	// 兜底: 取 message 字段或序列化整个 body
	irErr.Message = getString(raw, "message")
	if irErr.Message == "" {
		irErr.Message = string(body)
	}
	return irErr
}

// encodeIRError encodes a canonical IRError into the entry protocol's error format.
func encodeIRError(irErr *IRError, entryProtocol Protocol) interface{} {
	switch entryProtocol {
	case ProtocolAnthropicMessages:
		return map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    anthropicErrorTypeFromIR(irErr.Type),
				"message": irErr.Message,
			},
		}
	default:
		body := map[string]interface{}{
			"error": map[string]interface{}{
				"message": irErr.Message,
				"type":    openAIErrorTypeFromIR(irErr.Type),
			},
		}
		if irErr.Code != "" {
			body["error"].(map[string]interface{})["code"] = irErr.Code
		}
		return body
	}
}

// mapAnthropicErrorType maps Anthropic error types to canonical IRErrorType.
func mapAnthropicErrorType(typ string) IRErrorType {
	switch typ {
	case "invalid_request_error":
		return IRErrorInvalidRequest
	case "authentication_error":
		return IRErrorAuthentication
	case "permission_error":
		return IRErrorPermission
	case "not_found_error":
		return IRErrorNotFound
	case "rate_limit_error":
		return IRErrorRateLimit
	case "api_error":
		return IRErrorServerError
	case "overloaded_error":
		return IRErrorServiceUnavailable
	default:
		return IRErrorUpstreamError
	}
}

// mapOpenAIErrorType maps OpenAI error types to canonical IRErrorType.
func mapOpenAIErrorType(typ string) IRErrorType {
	switch typ {
	case "invalid_request_error":
		return IRErrorInvalidRequest
	case "authentication_error":
		return IRErrorAuthentication
	case "permission_error":
		return IRErrorPermission
	case "not_found_error":
		return IRErrorNotFound
	case "rate_limit_error":
		return IRErrorRateLimit
	case "insufficient_quota":
		return IRErrorQuotaExceeded
	case "server_error":
		return IRErrorServerError
	case "service_unavailable_error":
		return IRErrorServiceUnavailable
	case "content_filter":
		return IRErrorContentFilter
	case "context_length_exceeded":
		return IRErrorContextLength
	default:
		return IRErrorUpstreamError
	}
}

// anthropicErrorTypeFromIR maps canonical IRErrorType back to Anthropic error type string.
func anthropicErrorTypeFromIR(typ IRErrorType) string {
	switch typ {
	case IRErrorInvalidRequest:
		return "invalid_request_error"
	case IRErrorAuthentication:
		return "authentication_error"
	case IRErrorPermission:
		return "permission_error"
	case IRErrorNotFound:
		return "not_found_error"
	case IRErrorRateLimit:
		return "rate_limit_error"
	case IRErrorServerError:
		return "api_error"
	case IRErrorServiceUnavailable:
		return "overloaded_error"
	default:
		return "invalid_request_error"
	}
}

// openAIErrorTypeFromIR maps canonical IRErrorType back to OpenAI error type string.
func openAIErrorTypeFromIR(typ IRErrorType) string {
	switch typ {
	case IRErrorInvalidRequest:
		return "invalid_request_error"
	case IRErrorAuthentication:
		return "authentication_error"
	case IRErrorPermission:
		return "permission_error"
	case IRErrorNotFound:
		return "not_found_error"
	case IRErrorRateLimit:
		return "rate_limit_error"
	case IRErrorQuotaExceeded:
		return "insufficient_quota"
	case IRErrorServerError:
		return "server_error"
	case IRErrorServiceUnavailable:
		return "service_unavailable_error"
	case IRErrorContentFilter:
		return "content_filter"
	case IRErrorContextLength:
		return "context_length_exceeded"
	default:
		return "invalid_request_error"
	}
}

func newEntryError(proto Protocol, message string) interface{} {
	return encodeIRError(&IRError{
		Type:    IRErrorInvalidRequest,
		Message: message,
	}, proto)
}
