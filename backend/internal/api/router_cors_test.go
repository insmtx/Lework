package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Lework/backend/internal/api/middleware"
)

// TestCORS_MustBeAppliedBefore_V1Group 锁定 Gin 的语义陷阱：
// r.Use(CORS()) 必须在 r.Group("/v1") 之前调用，否则 v1 子路由的处理链
// 不含 CORS 中间件，浏览器跨域请求会因缺失 Access-Control-Allow-Origin
// 而判定失败，前端 fetch 抛 TypeError: Failed to fetch。
//
// 背景：feat/refactor-account 分支在 SetupRouter 重构时把 r.Use 挪到了
// r.Group 之后，导致 /v1/SendPhoneLoginCode 等路由响应缺少全部 CORS 头。
// gin@v1.10.1/routergroup.go: Group() 与 handle() 都通过 combineHandlers
// 值拷贝当前 Handlers 切片，后续的 r.Use 追加不会回灌到已注册路由。
func TestCORS_MustBeAppliedBefore_V1Group(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("correct order: Use before Group", func(t *testing.T) {
		t.Setenv("LEWORK_CORS_ALLOWED_ORIGINS", "http://127.0.0.1:3005")
		r := gin.New()
		r.Use(middleware.CORS()) // 先挂中间件

		v1 := r.Group("/v1") // 此时 r.Handlers 含 CORS，v1 快照继承
		v1.POST("/Echo", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/v1/Echo", strings.NewReader(`{}`))
		req.Header.Set("Origin", "http://127.0.0.1:3005")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		assertCORSHeaders(t, w)
	})

	t.Run("wrong order: Group before Use (regression guard)", func(t *testing.T) {
		t.Setenv("LEWORK_CORS_ALLOWED_ORIGINS", "http://127.0.0.1:3005")
		r := gin.New()
		v1 := r.Group("/v1") // 此时 r.Handlers 为空，v1 快照空副本

		r.Use(middleware.CORS()) // 晚了，v1 看不到这之后的追加

		v1.POST("/Echo", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/v1/Echo", strings.NewReader(`{}`))
		req.Header.Set("Origin", "http://127.0.0.1:3005")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		// 错误顺序下 CORS 头必须缺失；若出现则说明 gin 行为改变，需重新评估。
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("unexpected CORS Allow-Origin in wrong-order case: %q (gin behavior may have changed)", got)
		}
	})
}

func TestCORS_DoesNotEchoUnknownOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("LEWORK_CORS_ALLOWED_ORIGINS", "http://127.0.0.1:3005")

	r := gin.New()
	r.Use(middleware.CORS())
	r.GET("/v1/Echo", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/Echo", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unknown origin must not be echoed, got %q", got)
	}
}

func TestCORS_UsesPublicURLFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("LEWORK_CORS_ALLOWED_ORIGINS", "")
	t.Setenv("LEWORK_PUBLIC_URL", "https://lework.example.com")

	r := gin.New()
	r.Use(middleware.CORS())
	r.GET("/v1/Echo", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/Echo", nil)
	req.Header.Set("Origin", "https://lework.example.com")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://lework.example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want public URL origin", got)
	}
}

// assertCORSHeaders 断言响应包含 middleware.CORS() 注入的全套头。
func assertCORSHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()

	if got, want := w.Header().Get("Access-Control-Allow-Origin"), "http://127.0.0.1:3005"; got != want {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q (CORS middleware not on handler chain)", got, want)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want %q", got, "true")
	}
	if got := w.Header().Get("Access-Control-Expose-Headers"); got == "" {
		t.Errorf("Access-Control-Expose-Headers missing")
	}
	if got := w.Header().Get("Vary"); got == "" {
		t.Errorf("Vary header missing")
	}
}
