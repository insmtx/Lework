package modelrouter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegisterRoutesDoesNotExposeModelsEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	v1 := r.Group("/v1")
	RegisterRoutes(v1)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}
}

func TestResolverUsesWorkerCache(t *testing.T) {
	resetDefaultStoreForTest()
	DefaultStore().Put(UpstreamConfig{
		ModelName:    "gpt-4.1",
		Provider:     "openai",
		BaseURL:      "https://api.openai.com",
		BaseURLHasV1: true,
		APIKey:       "sk-test",
	})

	cfg, err := NewResolver().Resolve(t.Context(), 1, "gpt-4.1")
	if err != nil {
		t.Fatalf("resolve cached model: %v", err)
	}
	if cfg.ModelName != "gpt-4.1" || cfg.Provider != "openai" || cfg.APIKey != "sk-test" {
		t.Fatalf("unexpected cached config: %#v", cfg)
	}
}

func TestResolverUsesCurrentCachedModelWhenRequestModelIsEmpty(t *testing.T) {
	resetDefaultStoreForTest()
	DefaultStore().Put(UpstreamConfig{
		ModelName: "claude-sonnet-4",
		Provider:  "anthropic",
		APIKey:    "anthropic-test",
	})

	cfg, err := NewResolver().Resolve(t.Context(), 1, "")
	if err != nil {
		t.Fatalf("resolve current cached model: %v", err)
	}
	if cfg.ModelName != "claude-sonnet-4" {
		t.Fatalf("expected current cached model, got %#v", cfg)
	}
	if cfg.BaseURL == "" {
		t.Fatalf("expected default base url to be filled")
	}
	if cfg.Protocol != ProtocolAnthropicMessages {
		t.Fatalf("expected anthropic protocol, got %s", cfg.Protocol)
	}
}
