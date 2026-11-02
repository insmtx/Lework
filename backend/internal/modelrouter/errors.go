package modelrouter

import (
	"fmt"
	"net/http"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"

	"github.com/insmtx/Lework/backend/pkg/llmprotocol"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Error handling
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func handleCallError(c *gin.Context, entryProtocol llmprotocol.Protocol, err error) {
	c.JSON(http.StatusBadGateway, newEntryError(entryProtocol, err.Error()))
}

// newEntryError creates an entry protocol error response.
func newEntryError(proto llmprotocol.Protocol, message string) interface{} {
	return encodeErrorForProtocol(message, "invalid_request_error", proto)
}

// parseUpstreamErrorBody parses the raw upstream error body and encodes it for the entry protocol.
func parseUpstreamErrorBody(body []byte, entryProtocol llmprotocol.Protocol) interface{} {
	if len(body) == 0 {
		return newEntryError(entryProtocol, "upstream returned an error")
	}

	var raw map[string]interface{}
	if err := sonic.Unmarshal(body, &raw); err != nil {
		return newEntryError(entryProtocol, fmt.Sprintf("upstream error: %s", string(body)))
	}

	message := ""
	errType := "upstream_error"

	if getString(raw, "type") == "error" {
		if errObj, ok := raw["error"].(map[string]interface{}); ok {
			message = getString(errObj, "message")
			errType = getString(errObj, "type")
		}
	} else if errObj, ok := raw["error"].(map[string]interface{}); ok {
		message = getString(errObj, "message")
		errType = getString(errObj, "type")
	} else if msg := getString(raw, "message"); msg != "" {
		message = msg
	}

	if message == "" {
		message = string(body)
	}
	if errType == "" {
		errType = "upstream_error"
	}

	return encodeErrorForProtocol(message, errType, entryProtocol)
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
