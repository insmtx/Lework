//go:build integration

package oss

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/pkg/accounterror"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/testutil"
	"github.com/insmtx/Leros/backend/types"
)

func setupIntegrationTest(t *testing.T) (account.AuthProvider, *gorm.DB) {
	t.Helper()
	database := testutil.Setup(t)
	return NewAuth(database, "test-secret", NoopSMSSender{}, nil), database
}

func TestSendPhoneLoginCode_Success_Integration(t *testing.T) {
	svc, database := setupIntegrationTest(t)
	ctx := context.Background()

	resp, err := svc.SendPhoneLoginCode(ctx, &account.SendPhoneLoginCodeInput{
		Phone: "13800138000",
	})
	if err != nil {
		t.Fatalf("SendPhoneLoginCode failed: %v", err)
	}
	if resp.Phone != "13800138000" {
		t.Fatalf("expected phone 13800138000, got %q", resp.Phone)
	}
	if resp.ExpiresIn != int64(phoneCodeExpire.Seconds()) {
		t.Fatalf("expected expires_in %d, got %d", int64(phoneCodeExpire.Seconds()), resp.ExpiresIn)
	}
	if resp.ResendAfter != int64(phoneCodeResendInterval.Seconds()) {
		t.Fatalf("expected resend_after %d, got %d", int64(phoneCodeResendInterval.Seconds()), resp.ResendAfter)
	}

	code, err := db.GetActiveAuthPhoneVerificationCode(ctx, database, "13800138000", time.Now())
	if err != nil {
		t.Fatalf("GetActiveAuthPhoneVerificationCode failed: %v", err)
	}
	if code == nil {
		t.Fatal("expected verification code to be persisted")
	}
}

func TestSendPhoneLoginCode_InvalidPhone_Integration(t *testing.T) {
	svc, _ := setupIntegrationTest(t)
	ctx := context.Background()

	_, err := svc.SendPhoneLoginCode(ctx, &account.SendPhoneLoginCodeInput{
		Phone: "123",
	})
	if !errors.Is(err, accounterror.ErrInvalidPhoneFormat) {
		t.Fatalf("expectedaccounterror.ErrInvalidPhoneFormat, got %v", err)
	}
}

func TestSendPhoneLoginCode_ResendTooFrequent_Integration(t *testing.T) {
	svc, _ := setupIntegrationTest(t)
	ctx := context.Background()

	_, err := svc.SendPhoneLoginCode(ctx, &account.SendPhoneLoginCodeInput{
		Phone: "13700137000",
	})
	if err != nil {
		t.Fatalf("first SendPhoneLoginCode failed: %v", err)
	}

	_, err = svc.SendPhoneLoginCode(ctx, &account.SendPhoneLoginCodeInput{
		Phone: "13700137000",
	})
	if !errors.Is(err, accounterror.ErrPhoneCodeSendTooOften) {
		t.Fatalf("expectedaccounterror.ErrPhoneCodeSendTooOften, got %v", err)
	}
}

func TestSendPhoneLoginCode_ExpiredCode_Integration(t *testing.T) {
	svc, database := setupIntegrationTest(t)
	ctx := context.Background()

	_, err := svc.SendPhoneLoginCode(ctx, &account.SendPhoneLoginCodeInput{
		Phone: "13600136000",
	})
	if err != nil {
		t.Fatalf("SendPhoneLoginCode failed: %v", err)
	}

	if err := database.Model(&types.AuthPhoneVerificationCode{}).
		Where("phone = ?", "13600136000").
		Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("update expires_at failed: %v", err)
	}

	_, err = svc.LoginByPhoneCode(ctx, &account.LoginByPhoneCodeInput{
		Phone: "13600136000",
		Code:  defaultPhoneCode,
	})
	if !errors.Is(err, accounterror.ErrInvalidPhoneCode) {
		t.Fatalf("expectedaccounterror.ErrInvalidPhoneCode, got %v", err)
	}
}

func TestSendPhoneLoginAndLogin_Integration(t *testing.T) {
	svc, database := setupIntegrationTest(t)
	ctx := context.Background()

	sent, err := svc.SendPhoneLoginCode(ctx, &account.SendPhoneLoginCodeInput{
		Phone: "13900139000",
	})
	if err != nil {
		t.Fatalf("SendPhoneLoginCode failed: %v", err)
	}
	if sent.Phone != "13900139000" || sent.ExpiresIn == 0 {
		t.Fatalf("unexpected send response: %+v", sent)
	}

	loggedIn, err := svc.LoginByPhoneCode(ctx, &account.LoginByPhoneCodeInput{
		Phone: "13900139000",
		Code:  defaultPhoneCode,
	})
	if err != nil {
		t.Fatalf("LoginByPhoneCode failed: %v", err)
	}
	if loggedIn.JwtToken != "" {
		t.Fatal("expected no jwt token for new user")
	}
	if loggedIn.RefreshToken == "" {
		t.Fatal("expected refresh token")
	}
	if loggedIn.LoginStatus != account.LoginStatusNeedCreateCompany {
		t.Fatalf("expected login_status need_create_company, got %q", loggedIn.LoginStatus)
	}
	if loggedIn.UserInfo.Phone != "13900139000" {
		t.Fatalf("expected phone in user info, got %q", loggedIn.UserInfo.Phone)
	}
	if loggedIn.UserInfo.Name != "13900139000" {
		t.Fatalf("expected default name to use phone, got %q", loggedIn.UserInfo.Name)
	}

	user, err := db.GetUserByPhone(ctx, database, "13900139000")
	if err != nil {
		t.Fatalf("GetUserByPhone failed: %v", err)
	}
	if user == nil {
		t.Fatal("expected user to be auto registered")
	}
}
