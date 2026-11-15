package bootstrap

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/insmtx/lework/backend/internal/controlplane/authn"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// openStore 用 SQLite 打开一个临时控制面库（与 store 包测试同一套构造路径）。
func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "controlplane.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestEnsureAdminCreatesDefaultAccount(t *testing.T) {
	st := openStore(t)
	user, created, err := EnsureAdmin(context.Background(), st)
	if err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true on an empty store")
	}
	if user.Email != DefaultAdminEmail || user.Role != "admin" || user.Status != "active" {
		t.Fatalf("user = %+v, want admin/admin/active", user)
	}
	if !authn.VerifyPassword(user.PasswordHash, DefaultAdminPassword) {
		t.Fatal("default password does not verify")
	}
}

func TestEnsureAdminDoesNothingWhenUsersExist(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	hash, _ := authn.HashPassword("other-password")
	if _, err := st.CreateUser(ctx, store.User{Email: "owner@example.com", PasswordHash: hash, Role: "admin", Status: "active"}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, created, err := EnsureAdmin(ctx, st); err != nil || created {
		t.Fatalf("EnsureAdmin = created %v, err %v; want false, nil", created, err)
	}
	if _, found, _ := st.UserByEmail(ctx, DefaultAdminEmail); found {
		t.Fatal("admin must not be created when users already exist")
	}
}

func TestEnsureAdminIsIdempotent(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	if _, _, err := EnsureAdmin(ctx, st); err != nil {
		t.Fatalf("first EnsureAdmin: %v", err)
	}
	if _, created, err := EnsureAdmin(ctx, st); err != nil || created {
		t.Fatalf("second EnsureAdmin = created %v, err %v; want false, nil", created, err)
	}
}
