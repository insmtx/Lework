package authz_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authn"
	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

var ctx = context.Background()

// newTestStore 打开一个隔离的内存库。
//
// 单连接是与共享缓存 DSN 绑定在一起的（见 store.Open 的 pinMemoryConnection），
// 因此这里只需 Open + 关闭，不必重复设置连接池。
func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open(:memory:): %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("store.Close(): %v", err)
		}
	})
	return s
}

func createUser(t *testing.T, s *store.Store, email, role, status string) store.User {
	t.Helper()

	u, err := s.CreateUser(ctx, store.User{
		Email:        email,
		PasswordHash: "x",
		Role:         role,
		Status:       status,
	})
	if err != nil {
		t.Fatalf("CreateUser(%q): %v", email, err)
	}
	return u
}

func createSession(t *testing.T, s *store.Store, userID string, ttl time.Duration) string {
	t.Helper()

	raw, hash, err := authn.NewToken()
	if err != nil {
		t.Fatalf("authn.NewToken(): %v", err)
	}
	if _, err := s.CreateSession(ctx, userID, hash, "test", ttl); err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}
	return raw
}

func TestResolverPrefersCookieOverBearer(t *testing.T) {
	s := newTestStore(t)
	u := createUser(t, s, "a@b.c", "user", "active")
	raw := createSession(t, s, u.ID, time.Hour)

	v := authz.Resolver{Store: s, EnrollmentToken: "admin-token", CookieName: "lework_session"}
	r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	r.RemoteAddr = "203.0.113.7:1234"
	r.AddCookie(&http.Cookie{Name: "lework_session", Value: raw})
	r.Header.Set("Authorization", "Bearer admin-token")

	got, ok, err := v.Resolve(r)
	if err != nil || !ok {
		t.Fatalf("Resolve = %v, %v", ok, err)
	}
	if got.Kind != "user" || got.UserID != u.ID {
		t.Fatalf("principal = %+v", got)
	}
	if got.Role != "user" {
		t.Fatalf("role = %q, want user", got.Role)
	}
}

func TestNodeAllowedHidesForeignNode(t *testing.T) {
	s := newTestStore(t)
	_ = s.AddNodeMember(ctx, store.NodeMember{NodeID: "n1", UserID: "u1", Role: "owner"})
	allowed, _ := authz.NodeAllowed(ctx, s, authz.Principal{Kind: "user", UserID: "u2"}, "n1")
	if allowed {
		t.Fatal("foreign node allowed")
	}
	admin, _ := authz.NodeAllowed(ctx, s, authz.Principal{Kind: "admin_token"}, "n1")
	if !admin {
		t.Fatal("admin denied")
	}
}

// TestResolverTreatsLoopbackAsLocal 钉住本机直通的三种写法：IPv4、IPv6 与
// localhost。任何漏掉一种都会让本机浏览器莫名其妙要求登录。
func TestResolverTreatsLoopbackAsLocal(t *testing.T) {
	s := newTestStore(t)
	v := authz.Resolver{Store: s, LocalAdmin: true}

	for _, addr := range []string{"127.0.0.1:8787", "[::1]:8787", "localhost:8787"} {
		r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
		r.RemoteAddr = addr

		got, ok, err := v.Resolve(r)
		if err != nil || !ok {
			t.Fatalf("Resolve(%q) = %v, %v", addr, ok, err)
		}
		if got.Kind != authz.KindLocal {
			t.Fatalf("Resolve(%q).Kind = %q, want %q", addr, got.Kind, authz.KindLocal)
		}
		if got.UserID != "" {
			t.Fatalf("Resolve(%q).UserID = %q, want empty", addr, got.UserID)
		}
		if !got.IsAdmin() {
			t.Fatalf("Resolve(%q) local principal must be admin", addr)
		}
	}
}

// TestResolverLocalAdminRequiresLoopbackAndOptIn 覆盖本机判定的所有负例：
// 关掉开关、外部地址、缺端口、带转发头都必须不认证。
func TestResolverLocalAdminRequiresLoopbackAndOptIn(t *testing.T) {
	cases := []struct {
		name       string
		localAdmin bool
		remoteAddr string
		forwarded  bool
	}{
		{name: "opt out", localAdmin: false, remoteAddr: "127.0.0.1:8787"},
		{name: "remote address", localAdmin: true, remoteAddr: "203.0.113.7:1234"},
		{name: "missing port", localAdmin: true, remoteAddr: "127.0.0.1"},
		{name: "forwarded", localAdmin: true, remoteAddr: "127.0.0.1:8787", forwarded: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := authz.Resolver{Store: newTestStore(t), LocalAdmin: tc.localAdmin}
			r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
			r.RemoteAddr = tc.remoteAddr
			if tc.forwarded {
				r.Header.Set("X-Forwarded-For", "203.0.113.7")
			}

			got, ok, err := v.Resolve(r)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if ok {
				t.Fatalf("Resolve() = %+v, true; want unauthenticated", got)
			}
			if got != (authz.Principal{}) {
				t.Fatalf("principal = %+v, want zero", got)
			}
		})
	}
}

func TestResolverAcceptsEnrollmentBearerToken(t *testing.T) {
	v := authz.Resolver{Store: newTestStore(t), EnrollmentToken: "admin-token"}
	r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	r.RemoteAddr = "203.0.113.7:1234"
	r.Header.Set("Authorization", "Bearer admin-token")

	got, ok, err := v.Resolve(r)
	if err != nil || !ok {
		t.Fatalf("Resolve = %v, %v", ok, err)
	}
	if got.Kind != authz.KindAdminToken {
		t.Fatalf("Kind = %q, want %q", got.Kind, authz.KindAdminToken)
	}
	if !got.IsAdmin() {
		t.Fatal("admin token principal must be admin")
	}
	if got.UserID != "" {
		t.Fatalf("UserID = %q, want empty", got.UserID)
	}
}

func TestResolverRejectsWrongEnrollmentToken(t *testing.T) {
	s := newTestStore(t)

	cases := []struct {
		name      string
		configure string
		header    string
	}{
		{name: "wrong token", configure: "admin-token", header: "Bearer admin-token2"},
		{name: "no header", configure: "admin-token"},
		{name: "wrong scheme", configure: "admin-token", header: "Basic admin-token"},
		{name: "empty bearer", configure: "admin-token", header: "Bearer "},
		{name: "no token configured", configure: "", header: "Bearer admin-token"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := authz.Resolver{Store: s, EnrollmentToken: tc.configure}
			r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
			r.RemoteAddr = "203.0.113.7:1234"
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}

			got, ok, err := v.Resolve(r)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if ok {
				t.Fatalf("Resolve() = %+v, true; want unauthenticated", got)
			}
		})
	}
}

func TestResolverRejectsUnknownSessionCookie(t *testing.T) {
	s := newTestStore(t)
	v := authz.Resolver{Store: s, EnrollmentToken: "admin-token", CookieName: "lework_session"}
	r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	r.RemoteAddr = "203.0.113.7:1234"
	r.AddCookie(&http.Cookie{Name: "lework_session", Value: "not-a-real-session"})

	got, ok, err := v.Resolve(r)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if ok {
		t.Fatalf("Resolve() = %+v, true; want unauthenticated", got)
	}
	if got != (authz.Principal{}) {
		t.Fatalf("principal = %+v, want zero", got)
	}
}

func TestResolverRejectsExpiredSession(t *testing.T) {
	s := newTestStore(t)
	u := createUser(t, s, "expired@b.c", "user", "active")
	raw := createSession(t, s, u.ID, -time.Minute)

	v := authz.Resolver{Store: s, CookieName: "lework_session"}
	r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	r.RemoteAddr = "203.0.113.7:1234"
	r.AddCookie(&http.Cookie{Name: "lework_session", Value: raw})

	if _, ok, err := v.Resolve(r); err != nil || ok {
		t.Fatalf("Resolve() = %v, %v; want unauthenticated", ok, err)
	}
}

// TestResolverRejectsInactiveUser 钉住「空 Status 也不等于可登录」：CreateUser
// 不填默认状态，漏填的账号必须被拒绝而不是默默当成 active。
func TestResolverRejectsInactiveUser(t *testing.T) {
	for _, status := range []string{"disabled", ""} {
		t.Run("status="+status, func(t *testing.T) {
			s := newTestStore(t)
			u := createUser(t, s, "idle@b.c", "user", status)
			raw := createSession(t, s, u.ID, time.Hour)

			v := authz.Resolver{Store: s, CookieName: "lework_session"}
			r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
			r.RemoteAddr = "203.0.113.7:1234"
			r.AddCookie(&http.Cookie{Name: "lework_session", Value: raw})

			if _, ok, err := v.Resolve(r); err != nil || ok {
				t.Fatalf("Resolve() = %v, %v; want unauthenticated", ok, err)
			}
		})
	}
}

func TestResolverReportsSessionLookupFailure(t *testing.T) {
	s := newTestStore(t)
	u := createUser(t, s, "broken@b.c", "user", "active")
	raw := createSession(t, s, u.ID, time.Hour)
	if err := s.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	v := authz.Resolver{Store: s, CookieName: "lework_session"}
	r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	r.RemoteAddr = "203.0.113.7:1234"
	r.AddCookie(&http.Cookie{Name: "lework_session", Value: raw})

	got, ok, err := v.Resolve(r)
	if err == nil {
		t.Fatal("Resolve() error = nil, want infrastructure error")
	}
	if ok {
		t.Fatalf("Resolve() = %+v, true; want unauthenticated", got)
	}
}

func TestResolverWithoutStoreFailsClosed(t *testing.T) {
	v := authz.Resolver{CookieName: "lework_session"}
	r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	r.RemoteAddr = "203.0.113.7:1234"
	r.AddCookie(&http.Cookie{Name: "lework_session", Value: "not-a-real-session"})

	if _, ok, err := v.Resolve(r); err == nil || ok {
		t.Fatalf("Resolve() = %v, %v; want an error and no principal", ok, err)
	}
}

func TestPrincipalIsAdmin(t *testing.T) {
	cases := []struct {
		name string
		p    authz.Principal
		want bool
	}{
		{name: "admin role", p: authz.Principal{Kind: authz.KindUser, UserID: "u1", Role: "admin"}, want: true},
		{name: "plain user", p: authz.Principal{Kind: authz.KindUser, UserID: "u1", Role: "user"}},
		{name: "empty role", p: authz.Principal{Kind: authz.KindUser, UserID: "u1"}},
		{name: "admin token", p: authz.Principal{Kind: authz.KindAdminToken}, want: true},
		{name: "local", p: authz.Principal{Kind: authz.KindLocal}, want: true},
		{name: "zero", p: authz.Principal{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.IsAdmin(); got != tc.want {
				t.Fatalf("IsAdmin() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNodeAllowedAllowsOwnerAndMember(t *testing.T) {
	s := newTestStore(t)
	members := []store.NodeMember{
		{NodeID: "n1", UserID: "owner", Role: store.NodeRoleOwner},
		{NodeID: "n1", UserID: "member", Role: store.NodeRoleMember},
	}
	for _, m := range members {
		if err := s.AddNodeMember(ctx, m); err != nil {
			t.Fatalf("AddNodeMember(%q): %v", m.UserID, err)
		}
	}

	for _, userID := range []string{"owner", "member"} {
		allowed, err := authz.NodeAllowed(ctx, s, authz.Principal{Kind: authz.KindUser, UserID: userID}, "n1")
		if err != nil {
			t.Fatalf("NodeAllowed(%q) error = %v", userID, err)
		}
		if !allowed {
			t.Fatalf("NodeAllowed(%q) = false, want true", userID)
		}
	}
}

// TestNodeAllowedFailsClosed 覆盖身份不完整与未知 Kind：两者都必须拒绝，
// 且管理员分支不能因为 Store 为空而 panic。
func TestNodeAllowedFailsClosed(t *testing.T) {
	s := newTestStore(t)

	cases := []struct {
		name string
		st   *store.Store
		p    authz.Principal
	}{
		{name: "user without id", st: s, p: authz.Principal{Kind: authz.KindUser}},
		{name: "unknown kind", st: s, p: authz.Principal{Kind: "robot"}},
		{name: "zero principal", st: s, p: authz.Principal{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed, err := authz.NodeAllowed(ctx, tc.st, tc.p, "n1")
			if err != nil {
				t.Fatalf("NodeAllowed() error = %v", err)
			}
			if allowed {
				t.Fatalf("NodeAllowed(%+v) = true, want false", tc.p)
			}
		})
	}

	for _, k := range []string{authz.KindAdminToken, authz.KindLocal} {
		allowed, err := authz.NodeAllowed(ctx, nil, authz.Principal{Kind: k}, "n1")
		if err != nil {
			t.Fatalf("NodeAllowed(%q) error = %v", k, err)
		}
		if !allowed {
			t.Fatalf("NodeAllowed(%q) = false, want true", k)
		}
	}
}

func TestNodeAllowedWithoutStoreFailsClosed(t *testing.T) {
	if _, err := authz.NodeAllowed(ctx, nil, authz.Principal{Kind: authz.KindUser, UserID: "u1"}, "n1"); err == nil {
		t.Fatal("NodeAllowed() with nil store: want error")
	}
}

// TestVisibleNodeIDsForUser 只返回成员节点：即使账号角色是 admin，也不能看到
// 别人的节点或未认领的历史节点。全量访问必须走 admin_token 或本机直通。
func TestVisibleNodeIDsForUser(t *testing.T) {
	s := newTestStore(t)
	owned := mustNode(t, s, "owned")
	mustNode(t, s, "someone-else")
	mustNode(t, s, "unclaimed")

	if err := s.AddNodeMember(ctx, store.NodeMember{
		NodeID: owned.ID, UserID: "u1", Role: store.NodeRoleOwner,
	}); err != nil {
		t.Fatalf("AddNodeMember(): %v", err)
	}

	got, err := authz.VisibleNodeIDs(ctx, s, authz.Principal{Kind: authz.KindUser, UserID: "u1", Role: "admin"})
	if err != nil {
		t.Fatalf("VisibleNodeIDs() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("VisibleNodeIDs() = %v, want only %q", got, owned.ID)
	}
	if _, ok := got[owned.ID]; !ok {
		t.Fatalf("VisibleNodeIDs() = %v, missing %q", got, owned.ID)
	}

	other, err := authz.VisibleNodeIDs(ctx, s, authz.Principal{Kind: authz.KindUser, UserID: "u2"})
	if err != nil {
		t.Fatalf("VisibleNodeIDs() error = %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("VisibleNodeIDs(u2) = %v, want empty", other)
	}
}

func TestVisibleNodeIDsForAdminsIncludesUnclaimed(t *testing.T) {
	s := newTestStore(t)
	claimed := mustNode(t, s, "claimed")
	unclaimed := mustNode(t, s, "unclaimed")
	if err := s.AddNodeMember(ctx, store.NodeMember{
		NodeID: claimed.ID, UserID: "u1", Role: store.NodeRoleOwner,
	}); err != nil {
		t.Fatalf("AddNodeMember(): %v", err)
	}

	for _, kind := range []string{authz.KindAdminToken, authz.KindLocal} {
		got, err := authz.VisibleNodeIDs(ctx, s, authz.Principal{Kind: kind})
		if err != nil {
			t.Fatalf("VisibleNodeIDs(%q) error = %v", kind, err)
		}
		if len(got) != 2 {
			t.Fatalf("VisibleNodeIDs(%q) = %v, want both nodes", kind, got)
		}
		for _, id := range []string{claimed.ID, unclaimed.ID} {
			if _, ok := got[id]; !ok {
				t.Fatalf("VisibleNodeIDs(%q) = %v, missing %q", kind, got, id)
			}
		}
	}
}

func TestVisibleNodeIDsUnknownKindIsEmpty(t *testing.T) {
	s := newTestStore(t)
	mustNode(t, s, "n1")

	got, err := authz.VisibleNodeIDs(ctx, s, authz.Principal{Kind: "robot"})
	if err != nil {
		t.Fatalf("VisibleNodeIDs() error = %v", err)
	}
	if got == nil {
		t.Fatal("VisibleNodeIDs() = nil, want empty map")
	}
	if len(got) != 0 {
		t.Fatalf("VisibleNodeIDs() = %v, want empty", got)
	}
}

func TestVisibleNodeIDsWithoutStoreFailsClosed(t *testing.T) {
	if _, err := authz.VisibleNodeIDs(ctx, nil, authz.Principal{Kind: authz.KindAdminToken}); err == nil {
		t.Fatal("VisibleNodeIDs() with nil store: want error")
	}
}

func mustNode(t *testing.T, s *store.Store, name string) store.Node {
	t.Helper()

	n, err := s.UpsertLegacyNode(ctx, store.Node{Name: name})
	if err != nil {
		t.Fatalf("UpsertLegacyNode(%q): %v", name, err)
	}
	return n
}
