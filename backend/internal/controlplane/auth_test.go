package controlplane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authn"
	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

const (
	// authTestToken 是测试用 enrollment token，只参与比较。
	authTestToken = "secret-enrollment-token"

	// authTestPassword 满足账号密码的最小长度规则。
	authTestPassword = "secret-password"

	// unauthorizedBody 是未认证请求的固定响应体；任何令牌都不得出现在里面。
	unauthorizedBody = `{"code":"unauthorized","message":"unauthorized"}` + "\n"
)

// authFixture 是鉴权中间件的测试装配：真实的存储、真实账号与一条会话。
type authFixture struct {
	handler    http.Handler
	store      *store.Store
	user       store.User
	sessionRaw string
}

// newAuthFixture 组装一个带账号与会话的中间件；localAdmin 决定本机直通开关。
func newAuthFixture(t *testing.T, localAdmin bool) *authFixture {
	t.Helper()

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	hash, err := authn.HashPassword(authTestPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user, err := st.CreateUser(context.Background(), store.User{
		Email:        "a@b.c",
		PasswordHash: hash,
		Role:         "admin",
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	raw, tokenHash, err := authn.NewToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	if _, err := st.CreateSession(context.Background(), user.ID, tokenHash, "test", time.Hour); err != nil {
		t.Fatalf("create session: %v", err)
	}

	return &authFixture{
		handler:    newAuthMiddleware(principalRecorder(), newAuthResolver(st, localAdmin)),
		store:      st,
		user:       user,
		sessionRaw: raw,
	}
}

// newAuthResolver 按生产参数装配解析器；CookieName 留空以覆盖默认名。
func newAuthResolver(st *store.Store, localAdmin bool) authz.Resolver {
	return authz.Resolver{
		Store:           st,
		EnrollmentToken: authTestToken,
		LocalAdmin:      localAdmin,
	}
}

// principalRecorder 把中间件放进 context 的身份写进响应头，供断言读取。
func principalRecorder() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authz.PrincipalFrom(r.Context())
		if !ok {
			w.Header().Set("X-Principal-Kind", "none")
		} else {
			w.Header().Set("X-Principal-Kind", principal.Kind)
			w.Header().Set("X-Principal-User", principal.UserID)
		}
		w.WriteHeader(http.StatusOK)
	})
}

// TestAuthMiddleware 覆盖中间件的凭据路径、豁免路径与拒绝响应。
func TestAuthMiddleware(t *testing.T) {
	f := newAuthFixture(t, true)

	tests := []struct {
		name       string
		target     string
		remoteAddr string
		header     http.Header
		cookie     *http.Cookie
		wantStatus int
		wantKind   string
	}{
		{
			name:       "loopback passes",
			target:     "/api/nodes",
			remoteAddr: "127.0.0.1:5555",
			wantStatus: http.StatusOK,
			wantKind:   authz.KindUser,
		},
		{
			name:       "ipv6 loopback passes",
			target:     "/api/nodes",
			remoteAddr: "[::1]:5555",
			wantStatus: http.StatusOK,
			wantKind:   authz.KindUser,
		},
		{
			name:       "localhost passes",
			target:     "/api/nodes",
			remoteAddr: "localhost:5555",
			wantStatus: http.StatusOK,
			wantKind:   authz.KindUser,
		},
		{
			name:       "remote without credentials denied",
			target:     "/api/nodes",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "remote with session cookie passes",
			target:     "/api/nodes",
			remoteAddr: "203.0.113.5:5555",
			cookie:     &http.Cookie{Name: authz.DefaultCookieName, Value: f.sessionRaw},
			wantStatus: http.StatusOK,
			wantKind:   authz.KindUser,
		},
		{
			name:       "remote with wrong cookie denied",
			target:     "/api/nodes",
			remoteAddr: "203.0.113.5:5555",
			cookie:     &http.Cookie{Name: authz.DefaultCookieName, Value: "nope"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "remote with session bearer passes",
			target:     "/api/nodes",
			remoteAddr: "203.0.113.5:5555",
			header:     http.Header{"Authorization": {"Bearer " + f.sessionRaw}},
			wantStatus: http.StatusOK,
			wantKind:   authz.KindUser,
		},
		{
			name:       "remote with enrollment bearer passes",
			target:     "/api/nodes",
			remoteAddr: "203.0.113.5:5555",
			header:     http.Header{"Authorization": {"Bearer " + authTestToken}},
			wantStatus: http.StatusOK,
			wantKind:   authz.KindAdminToken,
		},
		{
			name:       "remote with wrong bearer denied",
			target:     "/api/nodes",
			remoteAddr: "203.0.113.5:5555",
			header:     http.Header{"Authorization": {"Bearer nope"}},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "loopback with forwarded header denied",
			target:     "/api/nodes",
			remoteAddr: "127.0.0.1:5555",
			header:     http.Header{"X-Forwarded-For": {"198.51.100.7"}},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "loopback with real ip header denied",
			target:     "/api/nodes",
			remoteAddr: "127.0.0.1:5555",
			header:     http.Header{"X-Real-IP": {"198.51.100.7"}},
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
			name:       "healthz exempt",
			target:     "/healthz",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusOK,
			wantKind:   "none",
		},
		{
			name:       "agent gateway exempt",
			target:     "/api/agent/connect",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusOK,
			wantKind:   "none",
		},
		{
			name:       "login exempt",
			target:     "/api/auth/login",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusOK,
			wantKind:   "none",
		},
		{
			name:       "auth methods exempt",
			target:     "/api/auth/methods",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusOK,
			wantKind:   "none",
		},
		{
			name:       "register exempt",
			target:     "/api/auth/register",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusOK,
			wantKind:   "none",
		},
		{
			name:       "static ui exempt",
			target:     "/",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusOK,
			wantKind:   "none",
		},
		{
			name:       "static asset exempt",
			target:     "/_next/static/app.js",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusOK,
			wantKind:   "none",
		},
		{
			name:       "me is protected",
			target:     "/api/me",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "logout is protected",
			target:     "/api/auth/logout",
			remoteAddr: "203.0.113.5:5555",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.target, nil)
			req.RemoteAddr = tt.remoteAddr
			for name, values := range tt.header {
				for _, value := range values {
					req.Header.Add(name, value)
				}
			}
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}

			rec := httptest.NewRecorder()
			f.handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantKind != "" {
				if got := rec.Header().Get("X-Principal-Kind"); got != tt.wantKind {
					t.Fatalf("principal kind = %q, want %q", got, tt.wantKind)
				}
			}
			if rec.Code == http.StatusUnauthorized {
				if got := rec.Body.String(); got != unauthorizedBody {
					t.Fatalf("401 body = %q, want %q", got, unauthorizedBody)
				}
				if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
					t.Fatalf("Content-Type = %q, want application/json", got)
				}
			}
			if strings.Contains(rec.Body.String(), authTestToken) || strings.Contains(rec.Body.String(), f.sessionRaw) {
				t.Fatalf("response leaked a credential: %s", rec.Body)
			}
		})
	}
}

// TestAuthMiddlewareFirstRunLocalPassThrough 固化设计 §8 的本机直通条件：
// 没有账号时即使关闭 local_admin，本机请求仍视为管理员；一旦有了账号，
// 关闭 local_admin 就必须登录。
func TestAuthMiddlewareFirstRunLocalPassThrough(t *testing.T) {
	t.Run("empty user table passes without local admin", func(t *testing.T) {
		st, err := store.Open(":memory:")
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		t.Cleanup(func() { _ = st.Close() })

		handler := newAuthMiddleware(principalRecorder(), newAuthResolver(st, false))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, remoteRequest(http.MethodGet, "/api/nodes", "127.0.0.1:5555"))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body)
		}
		if got := rec.Header().Get("X-Principal-Kind"); got != authz.KindLocal {
			t.Fatalf("principal kind = %q, want local", got)
		}
	})

	t.Run("users exist and local admin disabled denies loopback", func(t *testing.T) {
		f := newAuthFixture(t, false)

		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, remoteRequest(http.MethodGet, "/api/nodes", "127.0.0.1:5555"))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (body=%s)", rec.Code, rec.Body)
		}
		if got := rec.Body.String(); got != unauthorizedBody {
			t.Fatalf("401 body = %q, want %q", got, unauthorizedBody)
		}
	})

	t.Run("local admin enabled resolves to the admin account", func(t *testing.T) {
		f := newAuthFixture(t, true)

		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, remoteRequest(http.MethodGet, "/api/nodes", "127.0.0.1:5555"))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body)
		}
		// 库里有 admin 时本机语义是一个真实账号，不再是部署级 KindLocal。
		if got := rec.Header().Get("X-Principal-Kind"); got != authz.KindUser {
			t.Fatalf("principal kind = %q, want user", got)
		}
	})

	t.Run("remote never gets the first-run pass", func(t *testing.T) {
		st, err := store.Open(":memory:")
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		t.Cleanup(func() { _ = st.Close() })

		handler := newAuthMiddleware(principalRecorder(), newAuthResolver(st, false))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, remoteRequest(http.MethodGet, "/api/nodes", "203.0.113.5:5555"))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (body=%s)", rec.Code, rec.Body)
		}
	})
}

// TestAuthMiddlewareStoreFailureIs500 校验基础设施故障不被伪装成「未登录」，
// 否则一次数据库抖动会让所有人以为会话失效。
func TestAuthMiddlewareStoreFailureIs500(t *testing.T) {
	handler := newAuthMiddleware(principalRecorder(), authz.Resolver{EnrollmentToken: authTestToken})

	req := remoteRequest(http.MethodGet, "/api/nodes", "203.0.113.5:5555")
	req.AddCookie(&http.Cookie{Name: authz.DefaultCookieName, Value: "anything"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

// TestAuthExemptPath 校验豁免判定：静态资源全部放行，/api 下只放行显式端点。
func TestAuthExemptPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/", want: true},
		{path: "/login", want: true},
		{path: "/_next/static/app.js", want: true},
		{path: "/favicon.ico", want: true},
		{path: "/healthz", want: true},
		{path: "/api/agent/connect", want: true},
		{path: "/api/auth/methods", want: true},
		{path: "/api/auth/login", want: true},
		{path: "/api/auth/register", want: true},
		{path: "/api/auth/logout"},
		{path: "/api/me"},
		{path: "/api/nodes"},
		{path: "/api/events"},
		{path: "/api/auth/login/extra"},
		// 非 /api 路径整体豁免：它们由静态资源处理器兜底，不存在 /api 下的
		// 前缀绕过面。
		{path: "/healthz/extra", want: true},
		{path: "/api/agent/connect/extra"},
	}

	for _, tt := range tests {
		if got := authExemptPath(tt.path); got != tt.want {
			t.Errorf("authExemptPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// remoteRequest 构造一个来源可指定的请求；RemoteAddr 由服务器填写，
// 测试里直接改写以模拟局域网或公网来源。
func remoteRequest(method, target, remoteAddr string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = remoteAddr
	return req
}

// sessionCookieFromResponse 从响应里取出会话 cookie，没有则返回 nil；
// 名称必须与解析器一致，否则登录成功也会一直处于未认证状态。
func sessionCookieFromResponse(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == authz.DefaultCookieName {
			return cookie
		}
	}
	return nil
}

// TestAuthMiddlewareRefreshesSessionCookie 固化「浏览器也真正滑动」：用 cookie
// 认证成功时，中间件原样回写同一个 cookie 并把 MaxAge 挪到 SessionTTL，否则
// 服务端续期了 expires_at，浏览器却仍在登录 30 天后被清掉 cookie。
func TestAuthMiddlewareRefreshesSessionCookie(t *testing.T) {
	const ttl = 12 * time.Hour

	newHandler := func(t *testing.T, sessionTTL time.Duration) *authFixture {
		t.Helper()
		f := newAuthFixture(t, false)
		resolver := newAuthResolver(f.store, false)
		resolver.SessionTTL = sessionTTL
		f.handler = newAuthMiddleware(principalRecorder(), resolver)
		return f
	}

	t.Run("cookie auth re-issues the same value", func(t *testing.T) {
		f := newHandler(t, ttl)
		req := remoteRequest(http.MethodGet, "/api/nodes", "203.0.113.5:5555")
		req.AddCookie(&http.Cookie{Name: authz.DefaultCookieName, Value: f.sessionRaw})

		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		cookie := sessionCookieFromResponse(rec)
		if cookie == nil {
			t.Fatal("cookie-authenticated request did not refresh the session cookie")
		}
		if cookie.Value != f.sessionRaw {
			t.Fatalf("refreshed cookie value = %q, want the original token", cookie.Value)
		}
		if cookie.MaxAge != int(ttl.Seconds()) {
			t.Fatalf("MaxAge = %d, want %d", cookie.MaxAge, int(ttl.Seconds()))
		}
		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
			t.Fatalf("refreshed cookie lost its attributes: %+v", cookie)
		}
	})

	t.Run("bearer session does not get a cookie", func(t *testing.T) {
		f := newHandler(t, ttl)
		req := remoteRequest(http.MethodGet, "/api/nodes", "203.0.113.5:5555")
		req.Header.Set("Authorization", "Bearer "+f.sessionRaw)

		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if cookie := sessionCookieFromResponse(rec); cookie != nil {
			t.Fatalf("bearer request received a session cookie: %+v", cookie)
		}
	})

	t.Run("local pass-through does not get a cookie", func(t *testing.T) {
		f := newAuthFixture(t, true)
		resolver := newAuthResolver(f.store, true)
		resolver.SessionTTL = ttl
		f.handler = newAuthMiddleware(principalRecorder(), resolver)

		req := remoteRequest(http.MethodGet, "/api/nodes", "127.0.0.1:5555")

		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if cookie := sessionCookieFromResponse(rec); cookie != nil {
			t.Fatalf("local request received a session cookie: %+v", cookie)
		}
	})

	t.Run("renewal disabled leaves the cookie alone", func(t *testing.T) {
		f := newHandler(t, 0)
		req := remoteRequest(http.MethodGet, "/api/nodes", "203.0.113.5:5555")
		req.AddCookie(&http.Cookie{Name: authz.DefaultCookieName, Value: f.sessionRaw})

		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if cookie := sessionCookieFromResponse(rec); cookie != nil {
			t.Fatalf("SessionTTL=0 re-issued a cookie: %+v", cookie)
		}
	})
}
