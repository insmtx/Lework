package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/tools"
	skillmanagetools "github.com/insmtx/Leros/backend/tools/skill_manage"
)

func TestNewServerRegistersPublicTools(t *testing.T) {
	srv := NewServer()

	if srv.GetTool(skillmanagetools.ToolNameSkillManage) == nil {
		t.Fatalf("expected %s to be registered", skillmanagetools.ToolNameSkillManage)
	}
	if srv.GetTool("node_shell") != nil {
		t.Fatalf("node tools must not be registered in the MCP server")
	}
	if srv.GetTool("skill_use") != nil {
		t.Fatalf("skill tools must not be registered in the MCP server")
	}
}

func TestNewServerWithToolsUsesBackendToolInterface(t *testing.T) {
	srv := NewServerWithTools(&testPublicTool{
		BaseTool: tools.NewBaseTool("public_test", "public test tool", tools.Schema{Type: "object"}),
	})

	if srv.GetTool("public_test") == nil {
		t.Fatalf("expected backend/tools.Tool to be adapted into MCP")
	}
}

type testPublicTool struct {
	tools.BaseTool
}

func (t *testPublicTool) Execute(context.Context, json.RawMessage) (string, error) {
	return `{"ok":true}`, nil
}

func TestRegisterRoutesMountsMCPHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/v1")
	RegisterRoutes(group, NewServer())

	req := httptest.NewRequest(http.MethodPost, "/v1/mcp", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusNotFound {
		t.Fatalf("expected /v1/mcp to be mounted")
	}
}

func TestRegisterRoutesRequiresTokenWhenConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	token := "test-auth-token"
	router := gin.New()
	group := router.Group("/v1")
	RegisterRoutes(group, NewServerWithToken(token))

	req := httptest.NewRequest(http.MethodPost, "/v1/mcp", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", recorder.Code)
	}
}

func TestRegisterRoutesAcceptsBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	token := "test-auth-token"
	router := gin.New()
	group := router.Group("/v1")
	RegisterRoutes(group, NewServerWithToken(token))

	req := httptest.NewRequest(http.MethodPost, "/v1/mcp", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(authorizationHeader, "Bearer "+token)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusUnauthorized {
		t.Fatalf("expected bearer token to pass auth")
	}
}

func TestRegisterRoutesAcceptsAPIKeyHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	token := "test-auth-token"
	router := gin.New()
	group := router.Group("/v1")
	RegisterRoutes(group, NewServerWithToken(token))

	req := httptest.NewRequest(http.MethodPost, "/v1/mcp", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(apiKeyHeader, token)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusUnauthorized {
		t.Fatalf("expected X-API-Key token to pass auth")
	}
}
