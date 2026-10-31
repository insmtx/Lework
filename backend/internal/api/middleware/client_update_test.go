package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Lework/backend/config"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{name: "equal", left: "0.1.12", right: "0.1.12", want: 0},
		{name: "numeric segment", left: "0.1.12", right: "0.1.3", want: 1},
		{name: "missing patch", left: "1.2", right: "1.2.0", want: 0},
		{name: "v prefix", left: "v1.2.3", right: "1.2.4", want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareVersions(tt.left, tt.right)
			if got != tt.want {
				t.Fatalf("CompareVersions(%q, %q) = %d, want %d", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

func TestClientUpdateMiddlewareBlocksOldDesktop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(ClientUpdateMiddleware(&config.ClientUpdateConfig{
		Desktop: config.ClientUpdatePolicy{
			MinSupportedVersion: "0.1.13",
			LatestVersion:       "0.1.14",
			UpdateURL:           "https://example.test/update",
			ForceMessage:        "请更新客户端",
		},
	}))
	router.GET("/v1/projects", func(ctx *gin.Context) {
		ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	req.Header.Set("X-Lework-Client-App", "desktop")
	req.Header.Set("X-Lework-Client-Version", "0.1.12")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUpgradeRequired, rec.Body.String())
	}
}

func TestClientUpdateMiddlewareAllowsCurrentDesktop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(ClientUpdateMiddleware(&config.ClientUpdateConfig{
		Desktop: config.ClientUpdatePolicy{MinSupportedVersion: "0.1.13"},
	}))
	router.GET("/v1/projects", func(ctx *gin.Context) {
		ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	req.Header.Set("X-Lework-Client-App", "desktop")
	req.Header.Set("X-Lework-Client-Version", "0.1.13")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
