//go:build integration && !enterprise

package service

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/adapter/account/oss"
	"github.com/insmtx/Leros/backend/internal/infra/sms"
	"github.com/insmtx/Leros/backend/internal/testutil"
	"github.com/insmtx/Leros/backend/types"
)

func TestAuthServiceRegisterByEmail_Integration(t *testing.T) {
	db := testutil.Setup(t)
	svc := oss.NewAuth(db, "test-secret", sms.NoopSMSSender{}, nil)

	registered, err := svc.RegisterByEmail(context.Background(), &account.RegisterByEmailInput{
		Email:           "integration.test@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "Integration Test",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}
	if registered.JwtToken != "" {
		t.Fatal("expected no jwt token on registration without organization")
	}
	if registered.RefreshToken == "" {
		t.Fatal("expected refresh token")
	}
	if registered.Uin == 0 {
		t.Fatal("expected uin from auto join default org")
	}
	if registered.Org.ID != types.SystemOrgID {
		t.Fatalf("expected org ID %d, got %d", types.SystemOrgID, registered.Org.ID)
	}
}

func TestAuthServiceLoginByPassword_Integration(t *testing.T) {
	db := testutil.Setup(t)
	svc := oss.NewAuth(db, "test-secret", sms.NoopSMSSender{}, nil)
	ctx := context.Background()

	_, err := svc.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "login.test@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "Login Test",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}

	loginResp, err := svc.LoginByPassword(ctx, &account.LoginByPasswordInput{
		Account:  "login.test@example.com",
		Password: "Password123",
	})
	if err != nil {
		t.Fatalf("LoginByPassword failed: %v", err)
	}
	if loginResp.RefreshToken == "" {
		t.Fatal("expected refresh token")
	}
}

func TestAuthServiceRegisterByEmail_DuplicateEmail_Integration(t *testing.T) {
	db := testutil.Setup(t)
	svc := oss.NewAuth(db, "test-secret", sms.NoopSMSSender{}, nil)
	ctx := context.Background()

	_, err := svc.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "duplicate@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "Dup User",
	})
	if err != nil {
		t.Fatalf("first register failed: %v", err)
	}

	_, err = svc.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "duplicate@example.com",
		Password:        "Password456",
		ConfirmPassword: "Password456",
		Name:            "Dup User 2",
	})
	if err == nil {
		t.Fatal("expected error for duplicate email registration")
	}
}
