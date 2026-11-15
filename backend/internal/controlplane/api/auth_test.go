package api

import (
	"context"
	"crypto/tls"
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
	// testLoginPassword 满足创建账号时的最小长度，避免登录用例依赖灰色地带。
	testLoginPassword = "correct-horse-battery"

	// testInviteCode 是邀请码明文；库里只保存它的哈希，测试用它证明
	// 邀请码可以消费一次、不能重放。
	testInviteCode = "invite-code-for-tests"
)

// authBody 是登录、注册与初始化的响应体形状，测试按线上格式断言，
// 不依赖包内结构体。
type authBody struct {
	User *struct {
		ID     string `json:"id"`
		Email  string `json:"email"`
		Role   string `json:"role"`
		Status string `json:"status"`
	} `json:"user"`
	Token string `json:"token"`
}

// meBody 是 GET /api/me 的响应体形状；local/admin_token 没有对应用户，
// 因此 user 允许为 null。
type meBody struct {
	Kind string        `json:"kind"`
	Role string        `json:"role"`
	User *authBodyUser `json:"user"`
}

// authBodyUser 是 meBody 里内嵌的用户记录。
type authBodyUser struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

// sessionCookieFrom 从响应里取出会话 cookie，没有则返回 nil。
func sessionCookieFrom(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == authz.DefaultCookieName {
			return cookie
		}
	}
	return nil
}

// createTestUser 直接写库创建账号，绕过接口层的长度校验，
// 便于覆盖「老账号密码较短」的登录路径。
func createTestUser(t *testing.T, env *testEnv, email, password, role, status string) store.User {
	t.Helper()

	hash, err := authn.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user, err := env.store.CreateUser(context.Background(), store.User{
		Email:        email,
		PasswordHash: hash,
		Role:         role,
		Status:       status,
	})
	if err != nil {
		t.Fatalf("create user %q: %v", email, err)
	}
	return user
}

// createTestInvite 写一条仍有效的邀请码，返回明文供请求体使用。
func createTestInvite(t *testing.T, env *testEnv, code string, expiresAt time.Time) {
	t.Helper()

	if _, err := env.store.CreateInvite(context.Background(), store.Invite{
		CodeHash:  authn.HashToken(code),
		ExpiresAt: expiresAt,
	}); err != nil {
		t.Fatalf("create invite: %v", err)
	}
}

// resolvePrincipal 按生产顺序解析一次请求，模拟中间件交给 handler 的身份。
func resolvePrincipal(t *testing.T, env *testEnv, cookie *http.Cookie) authz.Principal {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	principal, ok, err := env.resolver.Resolve(req)
	if err != nil || !ok {
		t.Fatalf("resolve session principal = %+v, %v, %v", principal, ok, err)
	}
	return principal
}

// TestLoginSetsCookieAndWrongPasswordIsIndistinguishable 固化登录成功时的
// 会话下发，以及「账号不存在」与「密码错误」必须给出完全相同的响应。
func TestLoginSetsCookieAndWrongPasswordIsIndistinguishable(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "a@b.c", "secret", "admin", "active")

	rec := env.do(http.MethodPost, "/api/auth/login", `{"email":"a@b.c","password":"secret"}`)
	requireStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}

	cookie := sessionCookieFrom(t, rec)
	if cookie == nil {
		t.Fatal("no session cookie")
	}
	if !cookie.HttpOnly {
		t.Error("cookie is not HttpOnly")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie SameSite = %v, want Strict", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("cookie Path = %q, want /", cookie.Path)
	}
	if cookie.Secure {
		t.Error("cookie is Secure over plain HTTP")
	}
	if cookie.MaxAge <= 0 {
		t.Errorf("cookie MaxAge = %d, want a persistent session", cookie.MaxAge)
	}

	body := decodeBody[authBody](t, rec)
	if body.User == nil || body.User.ID != user.ID || body.User.Email != "a@b.c" || body.User.Role != "admin" {
		t.Fatalf("login user = %+v, want %s a@b.c admin", body.User, user.ID)
	}
	if body.Token == "" || body.Token != cookie.Value {
		t.Fatalf("body token does not match the cookie value")
	}
	if _, ok, err := env.store.SessionByTokenHash(context.Background(), authn.HashToken(body.Token)); err != nil || !ok {
		t.Fatalf("session lookup by token hash = %v, %v", ok, err)
	}
	if strings.Contains(rec.Body.String(), user.PasswordHash) {
		t.Fatal("login response leaked the password hash")
	}

	wrong := env.do(http.MethodPost, "/api/auth/login", `{"email":"a@b.c","password":"nope"}`)
	missing := env.do(http.MethodPost, "/api/auth/login", `{"email":"x@y.z","password":"nope"}`)
	requireStatus(t, wrong, http.StatusUnauthorized)
	requireStatus(t, missing, http.StatusUnauthorized)
	if wrong.Body.String() != missing.Body.String() {
		t.Fatalf("error bodies differ: %q vs %q", wrong.Body.String(), missing.Body.String())
	}
	if got := wrong.Body.String(); got != `{"code":"invalid_credentials","message":"invalid_credentials"}`+"\n" {
		t.Fatalf("error body = %q", got)
	}
}

// TestLoginSetsSecureCookieOnTLS 校验 HTTPS 下 cookie 带 Secure 标记。
func TestLoginSetsSecureCookieOnTLS(t *testing.T) {
	env := newTestEnv(t, time.Second)
	createTestUser(t, env, "a@b.c", testLoginPassword, "admin", "active")

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"email":"a@b.c","password":"`+testLoginPassword+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	env.mux.ServeHTTP(rec, req)

	requireStatus(t, rec, http.StatusOK)
	cookie := sessionCookieFrom(t, rec)
	if cookie == nil {
		t.Fatal("no session cookie")
	}
	if !cookie.Secure {
		t.Error("cookie is not Secure over TLS")
	}
}

// TestLoginRejectsMalformedInput 固化输入校验边界：空邮箱是调用方错误，
// 空密码则与密码错误共用同一条拒绝路径，避免变成账号存在性的旁路。
func TestLoginRejectsMalformedInput(t *testing.T) {
	env := newTestEnv(t, time.Second)
	createTestUser(t, env, "a@b.c", testLoginPassword, "admin", "active")

	blankEmail := env.do(http.MethodPost, "/api/auth/login", `{"email":"  ","password":"whatever1"}`)
	requireErrorCode(t, blankEmail, http.StatusBadRequest, "invalid_request")

	blankPassword := env.do(http.MethodPost, "/api/auth/login", `{"email":"a@b.c","password":""}`)
	requireErrorCode(t, blankPassword, http.StatusUnauthorized, "invalid_credentials")
}

// TestLoginRefusesDisabledAccount 确保停用账号拿不到会话，且响应与密码错误一致。
func TestLoginRefusesDisabledAccount(t *testing.T) {
	env := newTestEnv(t, time.Second)
	createTestUser(t, env, "off@example.com", testLoginPassword, "user", "disabled")

	rec := env.do(http.MethodPost, "/api/auth/login",
		`{"email":"off@example.com","password":"`+testLoginPassword+`"}`)
	requireErrorCode(t, rec, http.StatusUnauthorized, "invalid_credentials")
	if cookie := sessionCookieFrom(t, rec); cookie != nil {
		t.Fatalf("disabled account received a cookie: %+v", cookie)
	}
}

// TestBootstrapOnlyWorksWhenNoUsers 是任务书给出的验收用例：初始化只能用
// enrollment token 做一次，之后必须 409。
func TestBootstrapOnlyWorksWhenNoUsers(t *testing.T) {
	env := newTestEnv(t, time.Second)
	body := `{"token":"test-enrollment-token","email":"root@example.com","password":"secret-password"}`

	first := env.do(http.MethodPost, "/api/auth/bootstrap", body)
	requireStatus(t, first, http.StatusOK)
	created := decodeBody[authBody](t, first)
	if created.User == nil || created.User.Role != "admin" || created.User.Status != "active" {
		t.Fatalf("bootstrap user = %+v, want an active admin", created.User)
	}
	if created.User.Email != "root@example.com" {
		t.Fatalf("bootstrap email = %q", created.User.Email)
	}

	second := env.do(http.MethodPost, "/api/auth/bootstrap", body)
	requireStatus(t, second, http.StatusConflict)
	if cookie := sessionCookieFrom(t, second); cookie != nil {
		t.Fatal("second bootstrap must not issue a session")
	}

	// 已初始化时即使换一个合法令牌也只能得到 409，不能造出第二个管理员。
	wrong := env.do(http.MethodPost, "/api/auth/bootstrap",
		`{"token":"another-token","email":"other@example.com","password":"secret-password"}`)
	requireStatus(t, wrong, http.StatusConflict)
}

// TestBootstrapRejectsWrongTokenWithoutLeaking 校验错误令牌的固定响应体，
// 既不带出提交值，也不带出真实令牌。
func TestBootstrapRejectsWrongTokenWithoutLeaking(t *testing.T) {
	env := newTestEnv(t, time.Second)

	rec := env.do(http.MethodPost, "/api/auth/bootstrap",
		`{"token":"guess-token","email":"root@example.com","password":"secret-password"}`)
	requireErrorCode(t, rec, http.StatusUnauthorized, "invalid_token")
	if strings.Contains(rec.Body.String(), "guess-token") || strings.Contains(rec.Body.String(), "test-enrollment-token") {
		t.Fatalf("bootstrap error leaked a token: %s", rec.Body.String())
	}

	count, err := env.store.CountUsers(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("CountUsers = %d, %v; want 0 after a rejected bootstrap", count, err)
	}
}

// TestBootstrapValidatesInput 校验邮箱与密码规则；校验失败不能落库。
func TestBootstrapValidatesInput(t *testing.T) {
	env := newTestEnv(t, time.Second)

	blankEmail := env.do(http.MethodPost, "/api/auth/bootstrap",
		`{"token":"test-enrollment-token","email":" ","password":"secret-password"}`)
	requireErrorCode(t, blankEmail, http.StatusBadRequest, "invalid_request")

	shortPassword := env.do(http.MethodPost, "/api/auth/bootstrap",
		`{"token":"test-enrollment-token","email":"root@example.com","password":"short"}`)
	requireErrorCode(t, shortPassword, http.StatusBadRequest, "invalid_request")

	count, err := env.store.CountUsers(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("CountUsers = %d, %v; want 0 after rejected input", count, err)
	}
}

// TestRegisterModes 覆盖 invite / open / closed 与未知模式的注册策略。
func TestRegisterModes(t *testing.T) {
	t.Run("closed", func(t *testing.T) {
		env := newTestEnvWithAuth(t, time.Second, AuthConfig{SessionTTL: time.Hour, RegistrationMode: "closed"})

		rec := env.do(http.MethodPost, "/api/auth/register",
			`{"email":"new@example.com","password":"secret-password"}`)
		requireErrorCode(t, rec, http.StatusForbidden, "registration_closed")
	})

	t.Run("invite requires a code", func(t *testing.T) {
		env := newTestEnv(t, time.Second)

		rec := env.do(http.MethodPost, "/api/auth/register",
			`{"email":"new@example.com","password":"secret-password"}`)
		requireErrorCode(t, rec, http.StatusForbidden, "invalid_invite")
	})

	t.Run("invite rejects unknown code", func(t *testing.T) {
		env := newTestEnv(t, time.Second)

		rec := env.do(http.MethodPost, "/api/auth/register",
			`{"email":"new@example.com","password":"secret-password","invite_code":"nope"}`)
		requireErrorCode(t, rec, http.StatusForbidden, "invalid_invite")
	})

	t.Run("invite rejects expired code", func(t *testing.T) {
		env := newTestEnv(t, time.Second)
		createTestInvite(t, env, testInviteCode, time.Now().Add(-time.Minute))

		rec := env.do(http.MethodPost, "/api/auth/register",
			`{"email":"new@example.com","password":"secret-password","invite_code":"`+testInviteCode+`"}`)
		requireErrorCode(t, rec, http.StatusForbidden, "invalid_invite")
	})

	t.Run("invite registers once", func(t *testing.T) {
		env := newTestEnv(t, time.Second)
		createTestInvite(t, env, testInviteCode, time.Now().Add(time.Hour))

		rec := env.do(http.MethodPost, "/api/auth/register",
			`{"email":"New@Example.com","password":"secret-password","invite_code":"`+testInviteCode+`"}`)
		requireStatus(t, rec, http.StatusOK)

		body := decodeBody[authBody](t, rec)
		if body.User == nil || body.User.Role != "user" || body.User.Status != "active" {
			t.Fatalf("register user = %+v, want an active user", body.User)
		}
		if body.User.Email != "new@example.com" {
			t.Fatalf("register email = %q, want the normalized form", body.User.Email)
		}
		if cookie := sessionCookieFrom(t, rec); cookie == nil {
			t.Fatal("register did not issue a session cookie")
		}
		if body.Token == "" {
			t.Fatal("register did not return a session token")
		}

		replay := env.do(http.MethodPost, "/api/auth/register",
			`{"email":"second@example.com","password":"secret-password","invite_code":"`+testInviteCode+`"}`)
		requireErrorCode(t, replay, http.StatusForbidden, "invalid_invite")
	})

	t.Run("open needs no code", func(t *testing.T) {
		env := newTestEnvWithAuth(t, time.Second, AuthConfig{SessionTTL: time.Hour, RegistrationMode: "open"})

		rec := env.do(http.MethodPost, "/api/auth/register",
			`{"email":"new@example.com","password":"secret-password"}`)
		requireStatus(t, rec, http.StatusOK)
		if cookie := sessionCookieFrom(t, rec); cookie == nil {
			t.Fatal("register did not issue a session cookie")
		}
	})

	t.Run("empty mode defaults to invite", func(t *testing.T) {
		env := newTestEnvWithAuth(t, time.Second, AuthConfig{SessionTTL: time.Hour})

		rec := env.do(http.MethodPost, "/api/auth/register",
			`{"email":"new@example.com","password":"secret-password"}`)
		requireErrorCode(t, rec, http.StatusForbidden, "invalid_invite")
	})
}

// TestRegisterDuplicateEmailDoesNotLeakWithoutValidInvite 固化：没有有效邀请码
// 时，重复邮箱与未知邮箱得到完全相同的 403，不能用来枚举已注册账号。
func TestRegisterDuplicateEmailDoesNotLeakWithoutValidInvite(t *testing.T) {
	env := newTestEnv(t, time.Second)
	createTestUser(t, env, "taken@example.com", testLoginPassword, "user", "active")

	existing := env.do(http.MethodPost, "/api/auth/register",
		`{"email":"taken@example.com","password":"secret-password","invite_code":"bogus"}`)
	unknown := env.do(http.MethodPost, "/api/auth/register",
		`{"email":"fresh@example.com","password":"secret-password","invite_code":"bogus"}`)

	requireErrorCode(t, existing, http.StatusForbidden, "invalid_invite")
	requireErrorCode(t, unknown, http.StatusForbidden, "invalid_invite")
	if existing.Body.String() != unknown.Body.String() {
		t.Fatalf("bodies differ: %q vs %q", existing.Body.String(), unknown.Body.String())
	}
}

// TestRegisterRejectsDuplicateEmailWithValidInvite 校验持有有效邀请码时重复
// 邮箱得到 409；邀请码已因这次尝试作废，这是「不泄漏账号存在性」的代价。
func TestRegisterRejectsDuplicateEmailWithValidInvite(t *testing.T) {
	env := newTestEnv(t, time.Second)
	createTestUser(t, env, "taken@example.com", testLoginPassword, "user", "active")
	createTestInvite(t, env, testInviteCode, time.Now().Add(time.Hour))

	rec := env.do(http.MethodPost, "/api/auth/register",
		`{"email":"taken@example.com","password":"secret-password","invite_code":"`+testInviteCode+`"}`)
	requireErrorCode(t, rec, http.StatusConflict, "email_taken")

	replay := env.do(http.MethodPost, "/api/auth/register",
		`{"email":"fresh@example.com","password":"secret-password","invite_code":"`+testInviteCode+`"}`)
	requireErrorCode(t, replay, http.StatusForbidden, "invalid_invite")
}

// TestRegisterValidatesInputBeforeConsumingInvite 校验失败不能消耗邀请码。
func TestRegisterValidatesInputBeforeConsumingInvite(t *testing.T) {
	env := newTestEnv(t, time.Second)
	createTestInvite(t, env, testInviteCode, time.Now().Add(time.Hour))

	blankEmail := env.do(http.MethodPost, "/api/auth/register",
		`{"email":" ","password":"secret-password","invite_code":"`+testInviteCode+`"}`)
	requireErrorCode(t, blankEmail, http.StatusBadRequest, "invalid_request")

	shortPassword := env.do(http.MethodPost, "/api/auth/register",
		`{"email":"new@example.com","password":"short","invite_code":"`+testInviteCode+`"}`)
	requireErrorCode(t, shortPassword, http.StatusBadRequest, "invalid_request")

	ok := env.do(http.MethodPost, "/api/auth/register",
		`{"email":"new@example.com","password":"secret-password","invite_code":"`+testInviteCode+`"}`)
	requireStatus(t, ok, http.StatusOK)
}

// TestLogoutDeletesOnlyTheCurrentSession 校验登出精确吊销当前会话并清 cookie。
func TestLogoutDeletesOnlyTheCurrentSession(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "a@b.c", testLoginPassword, "admin", "active")

	login := env.do(http.MethodPost, "/api/auth/login",
		`{"email":"a@b.c","password":"`+testLoginPassword+`"}`)
	requireStatus(t, login, http.StatusOK)
	cookie := sessionCookieFrom(t, login)
	if cookie == nil {
		t.Fatal("no session cookie from login")
	}
	principal := resolvePrincipal(t, env, cookie)

	// 另一台设备的会话必须保留，登出不是「踢掉所有设备」。
	otherRaw, otherHash, err := authn.NewToken()
	if err != nil || otherRaw == "" {
		t.Fatalf("new token: %v", err)
	}
	if _, err := env.store.CreateSession(context.Background(), user.ID, otherHash, "other-device", time.Hour); err != nil {
		t.Fatalf("create other session: %v", err)
	}

	rec := env.doAs(principal, http.MethodPost, "/api/auth/logout", "")
	requireStatus(t, rec, http.StatusNoContent)
	if rec.Body.Len() != 0 {
		t.Fatalf("logout body = %q, want empty", rec.Body.String())
	}

	if _, ok, err := env.store.SessionByTokenHash(context.Background(), authn.HashToken(cookie.Value)); err != nil || ok {
		t.Fatalf("current session still valid = %v, %v", ok, err)
	}
	if _, ok, err := env.store.SessionByTokenHash(context.Background(), otherHash); err != nil || !ok {
		t.Fatalf("other session was deleted = %v, %v", ok, err)
	}

	cleared := sessionCookieFrom(t, rec)
	if cleared == nil {
		t.Fatal("logout did not clear the session cookie")
	}
	if cleared.MaxAge >= 0 && cleared.Value != "" {
		t.Fatalf("cookie = %+v, want an expired cookie", cleared)
	}
}

// TestLogoutForLocalPrincipalKeepsSessions 校验本机直通没有会话可删，
// 只负责清掉浏览器里可能残留的 cookie。
func TestLogoutForLocalPrincipalKeepsSessions(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "a@b.c", testLoginPassword, "admin", "active")
	_, hash, err := authn.NewToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	if _, err := env.store.CreateSession(context.Background(), user.ID, hash, "test", time.Hour); err != nil {
		t.Fatalf("create session: %v", err)
	}

	rec := env.doAs(authz.Principal{Kind: authz.KindLocal}, http.MethodPost, "/api/auth/logout", "")
	requireStatus(t, rec, http.StatusNoContent)
	if _, ok, err := env.store.SessionByTokenHash(context.Background(), hash); err != nil || !ok {
		t.Fatalf("local logout deleted a session = %v, %v", ok, err)
	}
	if cleared := sessionCookieFrom(t, rec); cleared == nil {
		t.Fatal("logout did not clear the session cookie")
	}
}

// TestLogoutAliases 覆盖两个登出路径：任务书的 POST /api/auth/logout 与设计
// §8 的 DELETE /api/auth/session，二者必须都能吊销当前会话并清 cookie。
func TestLogoutAliases(t *testing.T) {
	paths := []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/auth/logout"},
		{method: http.MethodDelete, path: "/api/auth/session"},
	}

	for _, tc := range paths {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			env := newTestEnv(t, time.Second)
			createTestUser(t, env, "a@b.c", testLoginPassword, "admin", "active")

			login := env.do(http.MethodPost, "/api/auth/login",
				`{"email":"a@b.c","password":"`+testLoginPassword+`"}`)
			requireStatus(t, login, http.StatusOK)
			cookie := sessionCookieFrom(t, login)
			if cookie == nil {
				t.Fatal("no session cookie from login")
			}
			principal := resolvePrincipal(t, env, cookie)

			rec := env.doAs(principal, tc.method, tc.path, "")
			requireStatus(t, rec, http.StatusNoContent)
			if cleared := sessionCookieFrom(t, rec); cleared == nil {
				t.Fatal("logout did not clear the session cookie")
			}
			if _, ok, err := env.store.SessionByTokenHash(context.Background(), authn.HashToken(cookie.Value)); err != nil || ok {
				t.Fatalf("session still valid = %v, %v", ok, err)
			}
		})
	}
}

// TestRequireUserRejectsMissingPrincipal 固化防御路径：没有经过中间件的请求
// 不能因为「读不到身份」被当成匿名放行。
func TestRequireUserRejectsMissingPrincipal(t *testing.T) {
	env := newTestEnv(t, time.Second)

	rec := env.do(http.MethodPost, "/api/auth/logout", "")
	requireErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

// TestMeReturnsUserOrKind 覆盖 /api/me 对三种身份的输出。
func TestMeReturnsUserOrKind(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "a@b.c", testLoginPassword, "admin", "active")

	t.Run("user", func(t *testing.T) {
		login := env.do(http.MethodPost, "/api/auth/login",
			`{"email":"a@b.c","password":"`+testLoginPassword+`"}`)
		requireStatus(t, login, http.StatusOK)
		principal := resolvePrincipal(t, env, sessionCookieFrom(t, login))

		rec := env.doAs(principal, http.MethodGet, "/api/me", "")
		requireStatus(t, rec, http.StatusOK)

		body := decodeBody[meBody](t, rec)
		if body.Kind != authz.KindUser || body.Role != "admin" {
			t.Fatalf("me = %+v, want a user principal with the admin role", body)
		}
		if body.User == nil || body.User.ID != user.ID || body.User.Email != "a@b.c" ||
			body.User.Role != "admin" || body.User.Status != "active" {
			t.Fatalf("me user = %+v", body.User)
		}
		if strings.Contains(rec.Body.String(), user.PasswordHash) {
			t.Fatal("/api/me leaked the password hash")
		}
	})

	for _, kind := range []string{authz.KindLocal, authz.KindAdminToken} {
		t.Run(kind, func(t *testing.T) {
			rec := env.doAs(authz.Principal{Kind: kind}, http.MethodGet, "/api/me", "")
			requireStatus(t, rec, http.StatusOK)

			body := decodeBody[meBody](t, rec)
			if body.Kind != kind || body.Role != "admin" {
				t.Fatalf("me = %+v, want kind %s role admin", body, kind)
			}
			if body.User != nil {
				t.Fatalf("me user = %+v, want null", body.User)
			}
			if !strings.Contains(rec.Body.String(), `"user":null`) {
				t.Fatalf("body = %q, want user null", rec.Body.String())
			}
		})
	}

	t.Run("deleted user", func(t *testing.T) {
		rec := env.doAs(authz.Principal{Kind: authz.KindUser, UserID: "missing", Role: "user"},
			http.MethodGet, "/api/me", "")
		requireErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
	})

	t.Run("no principal", func(t *testing.T) {
		rec := env.do(http.MethodGet, "/api/me", "")
		requireErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
	})
}
