package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Lework/backend/internal/api/dto"
)

type userInfo struct {
	PublicID string `json:"public_id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Role     string `json:"role"`
}

type createUserResult struct {
	UserID uint   `json:"user_id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Phone  string `json:"phone"`
	Role   string `json:"role"`
	IsNew  bool   `json:"is_new"`
}

type userListResult struct {
	Total  int64      `json:"total"`
	Offset int        `json:"offset"`
	Limit  int        `json:"limit"`
	Items  []userInfo `json:"items"`
}

func doRequest(t *testing.T, router *gin.Engine, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// 让本地调试免鉴权开关（LEWORK_DEV_ALLOW_UNAUTH）在 httptest 中也被视为回环请求。
	req.RemoteAddr = "127.0.0.1:12345"
	router.ServeHTTP(w, req)
	return w
}

func parseResponse(t *testing.T, w *httptest.ResponseRecorder) dto.Response {
	t.Helper()
	var resp dto.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

func parseData[T any](t *testing.T, resp dto.Response) T {
	t.Helper()
	data, _ := json.Marshal(resp.Data)
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	return result
}
