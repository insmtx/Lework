package service

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

func setupAuthServiceTest(t *testing.T) (contract.AuthService, *gorm.DB) {
	t.Helper()

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := database.AutoMigrate(
		&types.User{},
		&types.Organization{},
		&types.UserOrg{},
		&types.AuthRefreshToken{},
		&types.AuthLoginAttempt{},
	); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	return NewAuthService(database, "test-secret"), database
}

func TestAuthServiceRegisterLoginAndRefreshByEmail(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	registered, err := service.RegisterByEmail(ctx, &contract.RegisterByEmailRequest{
		Email:           "New.User@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "New User",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}
	if registered.JwtToken == "" {
		t.Fatal("expected jwt token")
	}
	if registered.RefreshToken == "" {
		t.Fatal("expected refresh token")
	}
	if registered.UserInfo.Email != "new.user@example.com" {
		t.Fatalf("expected normalized email, got %q", registered.UserInfo.Email)
	}
	if registered.Uin == 0 || registered.Org.ID == 0 {
		t.Fatalf("expected uin and org in response: %+v", registered)
	}
	if registered.Org.Name != "New User" {
		t.Fatalf("expected org name to use user name, got %q", registered.Org.Name)
	}
	if registered.Org.Code == "default_org" {
		t.Fatal("expected a newly created account org, got default_org")
	}

	loggedIn, err := service.LoginByEmail(ctx, &contract.LoginByEmailRequest{
		Email:    "new.user@example.com",
		Password: "Password123",
	})
	if err != nil {
		t.Fatalf("LoginByEmail failed: %v", err)
	}
	if loggedIn.JwtToken == "" || loggedIn.RefreshToken == "" {
		t.Fatalf("expected login tokens: %+v", loggedIn)
	}
	if loggedIn.Uin != registered.Uin {
		t.Fatalf("expected same uin, got %d want %d", loggedIn.Uin, registered.Uin)
	}

	refreshed, err := service.RefreshToken(ctx, &contract.RefreshTokenRequest{RefreshToken: loggedIn.RefreshToken})
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if refreshed.JwtToken == "" || refreshed.RefreshToken == "" {
		t.Fatalf("expected refreshed tokens: %+v", refreshed)
	}
	if refreshed.RefreshToken == loggedIn.RefreshToken {
		t.Fatal("expected refresh token rotation")
	}
}

func TestAuthServiceLoginAttemptLimit(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	_, err := service.RegisterByEmail(ctx, &contract.RegisterByEmailRequest{
		Email:           "limit@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "Limit User",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}

	for i := 0; i < loginAttemptMaxFailures; i++ {
		_, err = service.LoginByEmail(ctx, &contract.LoginByEmailRequest{
			Email:    "limit@example.com",
			Password: "WrongPassword123",
		})
		if !errors.Is(err, errAuthInvalidEmailOrPassword) {
			t.Fatalf("expected invalid password error on attempt %d, got %v", i+1, err)
		}
	}

	_, err = service.LoginByEmail(ctx, &contract.LoginByEmailRequest{
		Email:    "limit@example.com",
		Password: "Password123",
	})
	if !errors.Is(err, errAuthLoginAttemptsExceeded) {
		t.Fatalf("expected login attempts exceeded, got %v", err)
	}
}

func TestAuthServiceRegisterRejectsInvalidEmailAndPassword(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	_, err := service.RegisterByEmail(ctx, &contract.RegisterByEmailRequest{
		Email:           "not-an-email",
		Password:        "Password123",
		ConfirmPassword: "Password123",
	})
	if !errors.Is(err, errAuthInvalidEmailFormat) {
		t.Fatalf("expected invalid email format, got %v", err)
	}

	_, err = service.RegisterByEmail(ctx, &contract.RegisterByEmailRequest{
		Email:           "valid@example.com",
		Password:        "short",
		ConfirmPassword: "short",
	})
	if !errors.Is(err, errAuthPasswordTooShort) {
		t.Fatalf("expected password too short, got %v", err)
	}

	_, err = service.RegisterByEmail(ctx, &contract.RegisterByEmailRequest{
		Email:           "valid@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password456",
	})
	if !errors.Is(err, errAuthPasswordsDoNotMatch) {
		t.Fatalf("expected passwords do not match, got %v", err)
	}

	_, err = service.RegisterByEmail(ctx, &contract.RegisterByEmailRequest{
		Email:           "valid@example.com",
		Password:        "PasswordOnly",
		ConfirmPassword: "PasswordOnly",
	})
	if !errors.Is(err, errAuthPasswordMustContainLetterDigit) {
		t.Fatalf("expected password strength error, got %v", err)
	}
}
