package middleware

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"

	localauth "github.com/insmtx/Lework/backend/internal/api/auth"
)

// ResponseRequestID adds the request correlation ID to JSON object responses.
func ResponseRequestID() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		_, trace := localauth.FromGinContext(ctx)
		if trace == nil || trace.RequestID == "" {
			ctx.Next()
			return
		}

		ctx.Writer = &requestIDResponseWriter{
			ResponseWriter: ctx.Writer,
			requestID:      trace.RequestID,
		}
		ctx.Next()
	}
}

type requestIDResponseWriter struct {
	gin.ResponseWriter
	requestID string
}

func (w *requestIDResponseWriter) Write(data []byte) (int, error) {
	if strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		data = injectRequestID(data, w.requestID)
	}
	return w.ResponseWriter.Write(data)
}

func (w *requestIDResponseWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func injectRequestID(data []byte, requestID string) []byte {
	if requestID == "" || !json.Valid(data) {
		return data
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return data
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return data
	}
	if _, exists := fields["req_id"]; exists {
		return data
	}

	encodedID, err := json.Marshal(requestID)
	if err != nil {
		return data
	}
	prefix := []byte(`{"req_id":`)
	result := make([]byte, 0, len(trimmed)+len(prefix)+len(encodedID)+1)
	result = append(result, prefix...)
	result = append(result, encodedID...)
	if len(trimmed) > 2 {
		result = append(result, ',')
		result = append(result, trimmed[1:]...)
	} else {
		result = append(result, '}')
	}
	return result
}
