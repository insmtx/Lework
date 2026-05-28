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

	"github.com/insmtx/Leros/backend/internal/worker/identity"
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
		orgID := identity.OrgID()
		if orgID == 0 {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, "organization not configured"))
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, "failed to read request body"))
			return
		}

		modelName := extractModelField(body)

		cfg, err := resolver.Resolve(c.Request.Context(), orgID, modelName)
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

	if len(upErr.Body) > 0 {
		var respBody map[string]interface{}
		if json.Unmarshal(upErr.Body, &respBody) == nil {
			c.JSON(statusCode, respBody)
			return
		}
	}

	c.JSON(statusCode, newEntryError(entryProtocol, fmt.Sprintf("upstream returned status %d", upErr.StatusCode)))
}

func newEntryError(proto Protocol, message string) interface{} {
	switch proto {
	case ProtocolAnthropicMessages:
		return map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "invalid_request_error",
				"message": message,
			},
		}
	default:
		return map[string]interface{}{
			"error": map[string]interface{}{
				"message": message,
				"type":    "invalid_request_error",
			},
		}
	}
}
