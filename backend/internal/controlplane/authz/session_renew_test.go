package authz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authn"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// TestResolveSessionToleratesRenewalFailure 固化「续期尽力而为」：写库失败
// 时本次请求照常认证通过，而不是变成 500 把所有在线用户当成会话失效。
//
// 这条用例放在包内：需要注入一个必定失败的续期写入。真实存储一旦不可写，
// 连会话读取都会失败，无法构造出「读成功、续期失败」的窗口。
func TestResolveSessionToleratesRenewalFailure(t *testing.T) {
	ctx := context.Background()

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	user, err := st.CreateUser(ctx, store.User{
		Email: "renew@example.com", PasswordHash: "x", Role: KindUser, Status: "active",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	raw, tokenHash, err := authn.NewToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	session, err := st.CreateSession(ctx, user.ID, tokenHash, "test", 5*time.Minute)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.TouchSession(ctx, session.ID, session.ExpiresAt, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatalf("make session stale: %v", err)
	}

	v := Resolver{Store: st, SessionTTL: time.Hour}
	v.touchSession = func(context.Context, string, time.Time, time.Time) error {
		return errors.New("续期写入失败")
	}

	principal, ok, err := v.resolveSession(ctx, raw)
	if err != nil {
		t.Fatalf("resolveSession returned an error after a failed renewal: %v", err)
	}
	if !ok || principal.Kind != KindUser || principal.UserID != user.ID {
		t.Fatalf("principal = %+v, %v; want the authenticated user", principal, ok)
	}
}

// TestRenewSessionIgnoresStoreFailure 直接覆盖续期入口的容错：存储不可用时
// 整个调用安静返回，不 panic 也不写半条记录。
func TestRenewSessionIgnoresStoreFailure(t *testing.T) {
	ctx := context.Background()

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	v := Resolver{Store: st, SessionTTL: time.Hour}
	v.renewSession(ctx, store.UserSession{ID: "ses_missing", LastSeenAt: time.Now().Add(-2 * time.Hour)})

	v.Store = nil
	v.renewSession(ctx, store.UserSession{ID: "ses_missing"})
}

// TestResolveSessionRenewsThroughHTTP 用一次完整的 Resolve 证明续期发生在上层
// 可观察的位置（过期时间被推后），并保留原有身份。
func TestResolveSessionRenewsThroughHTTP(t *testing.T) {
	ctx := context.Background()

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	user, err := st.CreateUser(ctx, store.User{
		Email: "renew-http@example.com", PasswordHash: "x", Role: KindUser, Status: "active",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	raw, tokenHash, err := authn.NewToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	session, err := st.CreateSession(ctx, user.ID, tokenHash, "test", 5*time.Minute)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.TouchSession(ctx, session.ID, session.ExpiresAt, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatalf("make session stale: %v", err)
	}

	v := Resolver{Store: st, SessionTTL: 24 * time.Hour}
	r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	r.RemoteAddr = "203.0.113.7:1234"
	r.Header.Set("Authorization", "Bearer "+raw)

	principal, ok, err := v.Resolve(r)
	if err != nil || !ok || principal.UserID != user.ID {
		t.Fatalf("Resolve = %+v, %v, %v", principal, ok, err)
	}

	renewed, ok, err := st.SessionByTokenHash(ctx, tokenHash)
	if err != nil || !ok {
		t.Fatalf("SessionByTokenHash = %v, %v", ok, err)
	}
	if !renewed.ExpiresAt.After(time.Now().Add(23 * time.Hour)) {
		t.Fatalf("ExpiresAt = %s, want a value near now+24h", renewed.ExpiresAt)
	}
}
