package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Lework/backend/internal/api/dto"
)

func TestResponseRequestIDAddsRequestIDAtResponseRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CallerMiddleware(newTestParser(nil), nil))
	router.Use(ResponseRequestID())
	router.GET("/test", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, dto.Success(gin.H{"value": "ok"}))
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/test", nil)
	request.Header.Set(headerKeyRequestID, "req-response-1")
	router.ServeHTTP(recorder, request)

	var body map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var requestID string
	if err := json.Unmarshal(body["req_id"], &requestID); err != nil {
		t.Fatalf("decode req_id: %v", err)
	}
	if requestID != "req-response-1" {
		t.Fatalf("req_id = %q, want req-response-1", requestID)
	}
	if _, ok := body["code"]; !ok {
		t.Fatal("code should remain at response root")
	}
}

func TestResponseRequestIDLeavesNonJSONResponseUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CallerMiddleware(newTestParser(nil), nil))
	router.Use(ResponseRequestID())
	router.GET("/text", func(ctx *gin.Context) {
		ctx.String(http.StatusOK, "plain response")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/text", nil))
	if recorder.Body.String() != "plain response" {
		t.Fatalf("body = %q, want plain response", recorder.Body.String())
	}
}
