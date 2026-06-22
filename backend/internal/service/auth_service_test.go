package service

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
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
		&types.AuthPhoneVerificationCode{},
	); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	if err := database.Create(&types.Organization{
		PublicID: "org_default",
		Code:     "default_org",
		Name:     "默认组织",
		Type:     "company",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("failed to seed default org: %v", err)
	}

	return NewAuthService(database, "test-secret", nil), database
}

func TestAuthServiceRegisterLoginAndRefreshByEmail(t *testing.T) {
	service, database := setupAuthServiceTest(t)
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
	if registered.Org.ID != types.SystemOrgID {
		t.Fatalf("expected default org ID %d, got %d", types.SystemOrgID, registered.Org.ID)
	}
	if registered.Org.Name != "默认组织" {
		t.Fatalf("expected default org name, got %q", registered.Org.Name)
	}
	if registered.Org.Code != "default_org" {
		t.Fatalf("expected default org code, got %q", registered.Org.Code)
	}
	var orgCount int64
	if err := database.Model(&types.Organization{}).Count(&orgCount).Error; err != nil {
		t.Fatalf("count organizations: %v", err)
	}
	if orgCount != 1 {
		t.Fatalf("expected registration not to create organization, got %d organizations", orgCount)
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

func TestAuthServicePhoneCodeLoginAutoRegisters(t *testing.T) {
	service, database := setupAuthServiceTest(t)
	ctx := context.Background()

	sent, err := service.SendPhoneLoginCode(ctx, &contract.SendPhoneLoginCodeRequest{
		Phone: "13800138000",
	})
	if err != nil {
		t.Fatalf("SendPhoneLoginCode failed: %v", err)
	}
	if sent.Phone != "13800138000" || sent.ExpiresIn == 0 {
		t.Fatalf("unexpected send response: %+v", sent)
	}

	loggedIn, err := service.LoginByPhoneCode(ctx, &contract.LoginByPhoneCodeRequest{
		Phone: "13800138000",
		Code:  defaultPhoneCode,
	})
	if err != nil {
		t.Fatalf("LoginByPhoneCode failed: %v", err)
	}
	if loggedIn.JwtToken == "" || loggedIn.RefreshToken == "" {
		t.Fatalf("expected login tokens: %+v", loggedIn)
	}
	if loggedIn.UserInfo.Phone != "13800138000" {
		t.Fatalf("expected phone in user info, got %q", loggedIn.UserInfo.Phone)
	}
	if loggedIn.UserInfo.Name != "13800138000" {
		t.Fatalf("expected default name to use phone, got %q", loggedIn.UserInfo.Name)
	}

	user, err := db.GetUserByPhone(ctx, database, "13800138000")
	if err != nil {
		t.Fatalf("GetUserByPhone failed: %v", err)
	}
	if user == nil {
		t.Fatal("expected user to be auto registered")
	}
}

func TestAuthServicePhoneCodeRejectsInvalidCode(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	if _, err := service.SendPhoneLoginCode(ctx, &contract.SendPhoneLoginCodeRequest{
		Phone: "13900139000",
	}); err != nil {
		t.Fatalf("SendPhoneLoginCode failed: %v", err)
	}

	_, err := service.LoginByPhoneCode(ctx, &contract.LoginByPhoneCodeRequest{
		Phone: "13900139000",
		Code:  "000000",
	})
	if !errors.Is(err, errAuthInvalidPhoneCode) {
		t.Fatalf("expected invalid phone code, got %v", err)
	}
}

func TestAuthServicePhoneCodeRejectsResendWithinTwoMinutes(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	first, err := service.SendPhoneLoginCode(ctx, &contract.SendPhoneLoginCodeRequest{
		Phone: "13700137000",
	})
	if err != nil {
		t.Fatalf("first SendPhoneLoginCode failed: %v", err)
	}
	if first.ResendAfter != int64(phoneCodeResendInterval.Seconds()) {
		t.Fatalf("resend_after = %d, want %d", first.ResendAfter, int64(phoneCodeResendInterval.Seconds()))
	}

	_, err = service.SendPhoneLoginCode(ctx, &contract.SendPhoneLoginCodeRequest{
		Phone: "13700137000",
	})
	if !errors.Is(err, errAuthPhoneCodeSendTooOften) {
		t.Fatalf("expected resend-too-often error, got %v", err)
	}
}
