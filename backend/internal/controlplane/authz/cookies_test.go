package authz_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
)

// TestSetSessionCookie 固化会话 cookie 的属性：HttpOnly + Strict 防止脚本与
// 跨站请求带走会话，Secure 在 TLS 或可信代理声明 X-Forwarded-Proto: https
// 时开启（本机 HTTP 部署也要能用），MaxAge 跟随 TTL。属性集中在 authz 后，
// 登录、续期与登出不会再各写一份。
func TestSetSessionCookie(t *testing.T) {
	ttl := 12 * time.Hour

	t.Run("https", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "https://cp.example.com/api/auth/login", nil)
		authz.SetSessionCookie(rec, req, "raw-token", ttl)

		cookie := sessionCookieFrom(t, rec)
		if cookie.Value != "raw-token" {
			t.Fatalf("Value = %q, want the raw token", cookie.Value)
		}
		if !cookie.HttpOnly {
			t.Fatal("cookie is not HttpOnly")
		}
		if !cookie.Secure {
			t.Fatal("cookie is not Secure on TLS")
		}
		if cookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("SameSite = %v, want Strict", cookie.SameSite)
		}
		if cookie.Path != "/" {
			t.Fatalf("Path = %q, want /", cookie.Path)
		}
		if cookie.MaxAge != int(ttl.Seconds()) {
			t.Fatalf("MaxAge = %d, want %d", cookie.MaxAge, int(ttl.Seconds()))
		}
	})

	t.Run("plain http is not Secure", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/api/auth/login", nil)
		authz.SetSessionCookie(rec, req, "raw-token", ttl)

		if cookie := sessionCookieFrom(t, rec); cookie.Secure {
			t.Fatal("cookie is Secure on plain HTTP; local deployments would lose it")
		}
	})

	t.Run("forwarded https marks Secure", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://cp.example.com/api/auth/login", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		authz.SetSessionCookie(rec, req, "raw-token", ttl)

		if cookie := sessionCookieFrom(t, rec); !cookie.Secure {
			t.Fatal("cookie is not Secure behind a TLS-terminating proxy")
		}
	})

	t.Run("forwarded http stays not Secure", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://cp.example.com/api/auth/login", nil)
		req.Header.Set("X-Forwarded-Proto", "http")
		authz.SetSessionCookie(rec, req, "raw-token", ttl)

		if cookie := sessionCookieFrom(t, rec); cookie.Secure {
			t.Fatal("cookie is Secure although the proxy reports plain HTTP")
		}
	})

	t.Run("forwarded chain uses the first hop", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://cp.example.com/api/auth/login", nil)
		req.Header.Set("X-Forwarded-Proto", " HTTPS , http")
		authz.SetSessionCookie(rec, req, "raw-token", ttl)

		if cookie := sessionCookieFrom(t, rec); !cookie.Secure {
			t.Fatal("cookie is not Secure for a forwarded chain whose client hop is HTTPS")
		}
	})

	t.Run("tls wins over forwarded http", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "https://cp.example.com/api/auth/login", nil)
		req.Header.Set("X-Forwarded-Proto", "http")
		authz.SetSessionCookie(rec, req, "raw-token", ttl)

		if cookie := sessionCookieFrom(t, rec); !cookie.Secure {
			t.Fatal("cookie is not Secure on a direct TLS request")
		}
	})

	t.Run("non-positive ttl writes no MaxAge", func(t *testing.T) {
		for _, ttl := range []time.Duration{0, -time.Hour} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/", nil)
			authz.SetSessionCookie(rec, req, "raw-token", ttl)

			// MaxAge == 0 表示「不写 Max-Age」，浏览器按会话 cookie 处理；
			// 负数会被 net/http 序列化成 Max-Age=0（立即过期），语义相反。
			if cookie := sessionCookieFrom(t, rec); cookie.MaxAge != 0 {
				t.Fatalf("ttl=%s: MaxAge = %d, want 0", ttl, cookie.MaxAge)
			}
		}
	})
}

// TestClearSessionCookie 固化登出 cookie：同名、同路径、立即过期。
func TestClearSessionCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	authz.ClearSessionCookie(rec)

	cookie := sessionCookieFrom(t, rec)
	if cookie.Value != "" {
		t.Fatalf("Value = %q, want empty", cookie.Value)
	}
	if cookie.Path != "/" {
		t.Fatalf("Path = %q, want /", cookie.Path)
	}
	if !cookie.HttpOnly {
		t.Fatal("clearing cookie is not HttpOnly")
	}
	if cookie.MaxAge >= 0 {
		t.Fatalf("MaxAge = %d, want a negative value", cookie.MaxAge)
	}
}

// sessionCookieFrom 从响应里取出会话 cookie，并断言它确实存在。
func sessionCookieFrom(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == authz.DefaultCookieName {
			return cookie
		}
	}
	t.Fatalf("response has no %s cookie", authz.DefaultCookieName)
	return nil
}
