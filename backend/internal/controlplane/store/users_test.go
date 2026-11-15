package store

import (
	"context"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authn"
)

// newTestStore 返回用完即关的内存库；名字与计划里的调用保持一致，
// 让后续任务的测试无需关心底层装配差异。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	return openTestStore(t)
}

func TestCreateUserNormalizesEmailAndRejectsDuplicates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, User{Email: " Alice@Example.COM ", PasswordHash: "x", Role: "admin", Status: "active"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.Email != "alice@example.com" {
		t.Fatalf("email = %q", u.Email)
	}
	if u.ID == "" {
		t.Fatal("empty id")
	}
	if _, err := s.CreateUser(ctx, User{Email: "alice@example.com", PasswordHash: "y"}); err == nil {
		t.Fatal("duplicate email accepted")
	}
}

// TestCreateUserKeepsProvidedID 固化注册流程依赖：邀请码记录的未来用户 ID
// 必须与最终落库的 ID 一致，否则 used_by 指向一个不存在的账号。
func TestCreateUserKeepsProvidedID(t *testing.T) {
	s := newTestStore(t)
	const id = "user-preset-id"

	u, err := s.CreateUser(context.Background(), User{
		ID: id, Email: "preset@example.com", PasswordHash: "x", Role: "user", Status: "active",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID != id {
		t.Fatalf("id = %q, want the caller-provided %q", u.ID, id)
	}

	generated, err := s.CreateUser(context.Background(), User{Email: "generated@example.com", PasswordHash: "x"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if generated.ID == "" {
		t.Fatal("CreateUser did not generate an id when none was provided")
	}
}

func TestSessionLookupRejectsExpired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, User{Email: "a@b.c", PasswordHash: "x"})
	raw, hash, _ := authn.NewToken()
	_ = raw
	if _, err := s.CreateSession(ctx, u.ID, hash, "test", time.Minute); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, ok, _ := s.SessionByTokenHash(ctx, hash); !ok {
		t.Fatal("fresh session not found")
	}
	if _, err := s.CreateSession(ctx, u.ID, authn.HashToken("expired"), "test", -time.Minute); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, ok, _ := s.SessionByTokenHash(ctx, authn.HashToken("expired")); ok {
		t.Fatal("expired session accepted")
	}
}

// 改密/禁用依赖 DeleteUserSessions 一次清空该用户全部会话，这里连同
// 用户查询与单条会话删除一起覆盖，避免这些方法只被间接使用。
func TestUserLookupAndSessionDeletion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if n, err := s.CountUsers(ctx); err != nil || n != 0 {
		t.Fatalf("CountUsers on empty store = %d, %v", n, err)
	}

	u, err := s.CreateUser(ctx, User{Email: "Bob@Example.com", PasswordHash: "x"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if n, err := s.CountUsers(ctx); err != nil || n != 1 {
		t.Fatalf("CountUsers = %d, %v", n, err)
	}

	if got, ok, err := s.UserByEmail(ctx, " BOB@example.com "); err != nil || !ok || got.ID != u.ID {
		t.Fatalf("UserByEmail = %+v, %v, %v", got, ok, err)
	}
	if _, ok, err := s.UserByEmail(ctx, "nobody@example.com"); err != nil || ok {
		t.Fatalf("UserByEmail miss = %v, %v", ok, err)
	}
	if got, ok, err := s.UserByID(ctx, u.ID); err != nil || !ok || got.Email != "bob@example.com" {
		t.Fatalf("UserByID = %+v, %v, %v", got, ok, err)
	}
	if _, ok, err := s.UserByID(ctx, "missing"); err != nil || ok {
		t.Fatalf("UserByID miss = %v, %v", ok, err)
	}

	_, first, _ := authn.NewToken()
	_, second, _ := authn.NewToken()
	for _, hash := range []string{first, second} {
		if _, err := s.CreateSession(ctx, u.ID, hash, "test", time.Hour); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}

	session, ok, err := s.SessionByTokenHash(ctx, first)
	if err != nil || !ok {
		t.Fatalf("SessionByTokenHash = %v, %v", ok, err)
	}
	if session.UserID != u.ID {
		t.Fatalf("session user = %q, want %q", session.UserID, u.ID)
	}

	if err := s.DeleteSession(ctx, session.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, ok, _ := s.SessionByTokenHash(ctx, first); ok {
		t.Fatal("deleted session still visible")
	}
	if err := s.DeleteSession(ctx, session.ID); err != nil {
		t.Fatalf("DeleteSession on missing row should be harmless: %v", err)
	}

	if err := s.DeleteUserSessions(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUserSessions: %v", err)
	}
	if _, ok, _ := s.SessionByTokenHash(ctx, second); ok {
		t.Fatal("sessions survived DeleteUserSessions")
	}
}
