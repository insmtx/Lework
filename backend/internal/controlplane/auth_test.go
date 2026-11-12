package controlplane

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// findSessionCookie 从响应中取出浏览器会话 cookie，没有则返回 nil。
func findSessionCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	return nil
}

// TestAuthHandler 覆盖鉴权中间件的全部凭据路径与豁免路径。
func TestAuthHandler(t *testing.T) {
	const token = "secret-token-value"

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := newAuthHandler(next, token)

	tests := []struct {
		name       string
		method     string
		target     string
		remoteAddr string
		header     http.Header
		cookie     *http.Cookie
		wantStatus int
		wantCookie bool
	}{
		{
			name:       "loopback passes",
			target:     "/api/nodes",
			remoteAddr: "127.0.0.1:5555",
			wantStatus: http.StatusOK,
		},
		{
			name:       "ipv6 loopback passes",
			target:     "/api/nodes",
			remoteAddr: "[::1]:5555",
			wantStatus: http.StatusOK,
		},
		{
			name:       "remote without credentials denied",
			target:     "/api/nodes",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "remote with cookie passes",
			target:     "/api/nodes",
			remoteAddr: "203.0.113.5:5555",
			cookie:     &http.Cookie{Name: sessionCookieName, Value: token},
			wantStatus: http.StatusOK,
		},
		{
			name:       "remote with wrong cookie denied",
			target:     "/api/nodes",
			remoteAddr: "203.0.113.5:5555",
			cookie:     &http.Cookie{Name: sessionCookieName, Value: "nope"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "remote with bearer passes",
			target:     "/api/nodes",
			remoteAddr: "203.0.113.5:5555",
			header:     http.Header{"Authorization": {"Bearer " + token}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "remote with wrong bearer denied",
			target:     "/api/nodes",
			remoteAddr: "203.0.113.5:5555",
			header:     http.Header{"Authorization": {"Bearer nope"}},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "loopback with forwarding header denied",
			target:     "/api/nodes",
			remoteAddr: "127.0.0.1:5555",
			header:     http.Header{"X-Forwarded-For": {"198.51.100.7"}},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "loopback with real ip header denied",
			target:     "/api/nodes",
			remoteAddr: "127.0.0.1:5555",
			header:     http.Header{"X-Real-Ip": {"198.51.100.7"}},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "loopback with forwarded header denied",
			target:     "/api/nodes",
			remoteAddr: "127.0.0.1:5555",
			header:     http.Header{"Forwarded": {"for=198.51.100.7"}},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "token query bootstraps cookie",
			target:     "/api/nodes?token=" + token,
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusFound,
			wantCookie: true,
		},
		{
			name:       "token query keeps other params",
			target:     "/api/nodes?limit=10&token=" + token,
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusFound,
			wantCookie: true,
		},
		{
			name:       "wrong token query denied",
			target:     "/api/nodes?token=nope",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "healthz exempt",
			target:     "/healthz",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusOK,
		},
		{
			name:       "agent gateway exempt",
			target:     "/api/agent/connect",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			r := httptest.NewRequest(method, tt.target, nil)
			r.RemoteAddr = tt.remoteAddr
			for k, vs := range tt.header {
				for _, v := range vs {
					r.Header.Add(k, v)
				}
			}
			if tt.cookie != nil {
				r.AddCookie(tt.cookie)
			}

			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body)
			}
			if got := findSessionCookie(w); (got != nil) != tt.wantCookie {
				t.Fatalf("cookie = %v, want present=%v", got, tt.wantCookie)
			}
			if w.Code == http.StatusFound {
				location := w.Header().Get("Location")
				if strings.Contains(location, "token=") {
					t.Fatalf("redirect still carries token: %q", location)
				}
				if !strings.Contains(location, "/api/nodes") {
					t.Fatalf("redirect lost the original path: %q", location)
				}
				if w.Header().Get("Referrer-Policy") != "no-referrer" {
					t.Fatalf("Referrer-Policy = %q, want no-referrer", w.Header().Get("Referrer-Policy"))
				}
			}
			if strings.Contains(w.Body.String(), token) {
				t.Fatalf("response leaked the token: %s", w.Body)
			}
			if w.Code == http.StatusUnauthorized && strings.Contains(w.Body.String(), token) {
				t.Fatalf("401 response leaked the token: %s", w.Body)
			}
		})
	}
}

// TestAuthCookieAttributes 校验会话 cookie 的安全属性。
func TestAuthCookieAttributes(t *testing.T) {
	const token = "secret-token-value"

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := newAuthHandler(next, token)

	r := httptest.NewRequest(http.MethodGet, "/api/nodes?token="+token, nil)
	r.RemoteAddr = "203.0.113.5:5555"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	cookie := findSessionCookie(w)
	if cookie == nil {
		t.Fatal("no session cookie was set")
	}
	if !cookie.HttpOnly {
		t.Error("cookie is not HttpOnly")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie SameSite = %v, want Strict", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("cookie Path = %q, want %q", cookie.Path, "/")
	}
	if cookie.Secure {
		t.Error("cookie is Secure over plain HTTP")
	}
}

// TestAuthCookieSecureOnTLS 校验 HTTPS 请求下 cookie 带 Secure 标记。
func TestAuthCookieSecureOnTLS(t *testing.T) {
	const token = "secret-token-value"

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := newAuthHandler(next, token)

	r := httptest.NewRequest(http.MethodGet, "/api/nodes?token="+token, nil)
	r.RemoteAddr = "203.0.113.5:5555"
	r.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	cookie := findSessionCookie(w)
	if cookie == nil {
		t.Fatal("no session cookie was set")
	}
	if !cookie.Secure {
		t.Error("cookie is not Secure over TLS")
	}
}

// TestAuthHandlerEmptyTokenDeniesRemote 确保未配置令牌时不会因空值比较而放行。
func TestAuthHandlerEmptyTokenDeniesRemote(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := newAuthHandler(next, "")

	r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	r.RemoteAddr = "203.0.113.5:5555"
	r.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestIsLocalRequest 校验来源判定，包括缺失端口与畸形地址。
func TestIsLocalRequest(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       bool
	}{
		{name: "ipv4 loopback", remoteAddr: "127.0.0.1:1234", want: true},
		{name: "ipv6 loopback", remoteAddr: "[::1]:1234", want: true},
		{name: "public ip", remoteAddr: "203.0.113.5:1234", want: false},
		{name: "private ip", remoteAddr: "192.168.1.10:1234", want: false},
		{name: "missing port", remoteAddr: "127.0.0.1", want: false},
		{name: "empty", remoteAddr: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
			r.RemoteAddr = tt.remoteAddr
			if got := isLocalRequest(r); got != tt.want {
				t.Fatalf("isLocalRequest(%q) = %v, want %v", tt.remoteAddr, got, tt.want)
			}
		})
	}
}

// TestIsAuthExemptPath 校验豁免路径为精确匹配，避免前缀绕过。
func TestIsAuthExemptPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/healthz", want: true},
		{path: "/api/agent/connect", want: true},
		{path: "/api/nodes", want: false},
		{path: "/healthz/extra", want: false},
		{path: "/api/agent/connect/extra", want: false},
		{path: "/", want: false},
	}

	for _, tt := range tests {
		if got := isAuthExemptPath(tt.path); got != tt.want {
			t.Errorf("isAuthExemptPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
