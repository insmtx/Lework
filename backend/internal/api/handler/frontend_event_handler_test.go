package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCollectFrontendEvent_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterFrontendEventRoutes(router)

	body := map[string]interface{}{
		"fingerprint": "fp-abc123",
		"events": []map[string]interface{}{
			{
				"event_type": "click",
				"timestamp":  1700000000000,
				"page_url":   "/projects/123",
				"page_title": "项目详情",
				"event_name": "submit_btn",
			},
		},
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/CollectFrontendEvent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d", resp.Code)
	}
}

func TestCollectFrontendEvent_EmptyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterFrontendEventRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/CollectFrontendEvent", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCollectFrontendEvent_SkipEmptyType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterFrontendEventRoutes(router)

	body := map[string]interface{}{
		"events": []map[string]interface{}{
			{
				"event_type": "",
				"timestamp":  1700000000000,
			},
		},
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/CollectFrontendEvent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
