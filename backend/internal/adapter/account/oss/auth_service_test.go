//go:build !enterprise

package oss

import (
	"context"
	"errors"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/accounterror"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/sms"
	"github.com/insmtx/Leros/backend/internal/service"
	"github.com/insmtx/Leros/backend/types"
)

func setupAuthServiceTest(t *testing.T) (account.AuthProvider, *gorm.DB) {
	t.Helper()

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := database.AutoMigrate(
		&types.User{},
		&types.Organization{},
		&types.UserOrg{},
		&types.Department{},
		&types.MemberDepartment{},
		&types.AuthRefreshToken{},
		&types.AuthLoginAttempt{},
		&types.AuthPhoneVerificationCode{},
		&types.DigitalAssistant{},
		&types.WorkerDeployment{},
		&types.LLMModel{},
	); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	if err := database.Create(&types.LLMModel{
		OrgID:           1,
		Code:            "default",
		Name:            "Default",
		Provider:        "openai",
		ModelName:       "gpt-test",
		BaseURL:         "https://api.openai.com",
		BaseURLHasV1:    true,
		APIKeyEncrypted: "sk-test",
		Status:          string(types.LLMModelStatusActive),
		IsDefault:       true,
		IsSystem:        true,
	}).Error; err != nil {
		t.Fatalf("failed to seed default llm model: %v", err)
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
	// Seed default digital assistant required by worker provisioning
	if err := database.Create(&types.DigitalAssistant{
		PublicID:    "assistant_default_o2",
		Name:        "默认助手",
		Description: "系统默认助手",
		OrgID:       2,
		OwnerID:     1,
		Status:      string(types.DigitalAssistantStatusActive),
	}).Error; err != nil {
		t.Fatalf("failed to seed default assistant: %v", err)
	}

	return NewAuth(database, "test-secret", sms.NoopSMSSender{}, nil), database
}

func setupAuthServiceTestWithProvisioning(t *testing.T) (account.AuthProvider, *gorm.DB) {
	t.Helper()

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := database.AutoMigrate(
		&types.User{},
		&types.Organization{},
		&types.UserOrg{},
		&types.Department{},
		&types.MemberDepartment{},
		&types.AuthRefreshToken{},
		&types.AuthLoginAttempt{},
		&types.AuthPhoneVerificationCode{},
		&types.DigitalAssistant{},
		&types.WorkerDeployment{},
		&types.LLMModel{},
	); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	if err := database.Create(&types.LLMModel{
		OrgID:           1,
		Code:            "default",
		Name:            "Default",
		Provider:        "openai",
		ModelName:       "gpt-test",
		BaseURL:         "https://api.openai.com",
		BaseURLHasV1:    true,
		APIKeyEncrypted: "sk-test",
		Status:          string(types.LLMModelStatusActive),
		IsDefault:       true,
		IsSystem:        true,
	}).Error; err != nil {
		t.Fatalf("failed to seed default llm model: %v", err)
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

	provisioning := service.NewWorkerProvisioningService(database, nil)
	return NewAuth(database, "test-secret", sms.NoopSMSSender{}, provisioning), database
}

func TestAuthServiceRegisterByEmail(t *testing.T) {
	service, database := setupAuthServiceTest(t)
	ctx := context.Background()

	registered, err := service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "New.User@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "New User",
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
	if registered.LoginStatus != account.LoginStatusSuccess {
		t.Fatalf("expected login_status success with auto join default org, got %q", registered.LoginStatus)
	}
	if registered.UserInfo.Email != "new.user@example.com" {
		t.Fatalf("expected normalized email, got %q", registered.UserInfo.Email)
	}
	if len(registered.Organizations) != 1 {
		t.Fatalf("expected 1 organization from auto join default org, got %d", len(registered.Organizations))
	}

	var userOrgCount int64
	if err := database.Model(&types.UserOrg{}).Count(&userOrgCount).Error; err != nil {
		t.Fatalf("count user_orgs: %v", err)
	}
	if userOrgCount != 1 {
		t.Fatalf("expected 1 user_org record from auto join default org, got %d", userOrgCount)
	}

	// ChooseUin to get JWT
	chosen, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: registered.RefreshToken,
		Uin:          registered.Organizations[0].Uin,
		UserID:       registered.UserInfo.ID,
	})
	if err != nil {
		t.Fatalf("ChooseUin failed: %v", err)
	}
	if chosen.JwtToken == "" {
		t.Fatal("expected jwt token from ChooseUin")
	}
	if chosen.Uin != registered.Organizations[0].Uin {
		t.Fatalf("expected uin %d, got %d", registered.Organizations[0].Uin, chosen.Uin)
	}
	if len(chosen.Organizations) != 1 {
		t.Fatalf("expected 1 organization, got %d", len(chosen.Organizations))
	}

	// OSS 版本不支持创建组织
	_, err = service.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name: "新组织",
	})
	if !errors.Is(err, accounterror.ErrNotImplementedEdition) {
		t.Fatalf("expected CreateOrganization to be unsupported in OSS, got %v", err)
	}
}

func TestAuthServiceLoginByPassword(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	registered, err := service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "login.user@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "Login User",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}
	if len(registered.Organizations) != 1 {
		t.Fatalf("expected 1 organization from auto join default org, got %d", len(registered.Organizations))
	}

	// ChooseUin to get JWT
	chosen, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: registered.RefreshToken,
		Uin:          registered.Organizations[0].Uin,
		UserID:       registered.UserInfo.ID,
	})
	if err != nil {
		t.Fatalf("ChooseUin failed: %v", err)
	}

	// Now login by password - should return refreshtoken
	loggedIn, err := service.LoginByPassword(ctx, &account.LoginByPasswordInput{
		Account:  "login.user@example.com",
		Password: "Password123",
	})
	if err != nil {
		t.Fatalf("LoginByPassword failed: %v", err)
	}
	if loggedIn.RefreshToken == "" {
		t.Fatal("expected refresh token from LoginByPassword")
	}
	if len(loggedIn.Organizations) != 1 {
		t.Fatalf("expected 1 organization, got %d", len(loggedIn.Organizations))
	}

	// Use the new refresh token to ChooseUin and get JWT
	chosen2, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: loggedIn.RefreshToken,
		Uin:          registered.Organizations[0].Uin,
		UserID:       loggedIn.UserInfo.ID,
	})
	if err != nil {
		t.Fatalf("ChooseUin after login failed: %v", err)
	}
	if chosen2.JwtToken == "" {
		t.Fatal("expected jwt token")
	}

	// RefreshToken should work
	refreshed, err := service.RefreshToken(ctx, &account.RefreshTokenInput{RefreshToken: chosen2.RefreshToken})
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if refreshed.JwtToken == "" || refreshed.RefreshToken == "" {
		t.Fatalf("expected refreshed tokens: %+v", refreshed)
	}
	if refreshed.RefreshToken == chosen2.RefreshToken {
		t.Fatal("expected refresh token rotation")
	}

	_ = chosen
}

func TestAuthServiceLoginByPasswordRejectsWrongPassword(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	if _, err := service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "wrong.pass@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
	}); err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}

	_, err := service.LoginByPassword(ctx, &account.LoginByPasswordInput{
		Account:  "wrong.pass@example.com",
		Password: "WrongPassword999",
	})
	if !errors.Is(err, accounterror.ErrInvalidAccountOrPassword) {
		t.Fatalf("expected invalid account or password, got %v", err)
	}
}

func TestAuthServiceCreateOrganizationWithRefreshToken(t *testing.T) {
	service, database := setupAuthServiceTest(t)
	ctx := context.Background()

	_, err := service.SendPhoneLoginCode(ctx, &account.SendPhoneLoginCodeInput{
		Phone: "13900139001",
	})
	if err != nil {
		t.Fatalf("SendPhoneLoginCode failed: %v", err)
	}

	loggedIn, err := service.LoginByPhoneCode(ctx, &account.LoginByPhoneCodeInput{
		Phone: "13900139001",
		Code:  defaultPhoneCode,
	})
	if err != nil {
		t.Fatalf("LoginByPhoneCode failed: %v", err)
	}
	if loggedIn.LoginStatus != account.LoginStatusSuccess {
		t.Fatalf("expected success with auto join default org, got %q", loggedIn.LoginStatus)
	}
	if loggedIn.JwtToken != "" {
		t.Fatal("expected no jwt token for new user")
	}
	if len(loggedIn.Organizations) != 1 {
		t.Fatalf("expected 1 organization from auto join default org, got %d", len(loggedIn.Organizations))
	}

	// OSS 版本不支持创建组织
	_, err = service.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name: "手机登录组织",
	})
	if !errors.Is(err, accounterror.ErrNotImplementedEdition) {
		t.Fatalf("expected CreateOrganization to be unsupported, got %v", err)
	}

	// Verify user_org was auto-created for default org
	defaultOrg, err := db.NewOrgEntityDao(database).GetByCond(ctx, &db.OrgCond{Code: "default_org"})
	if err != nil {
		t.Fatalf("GetOrgByCode failed: %v", err)
	}
	userOrgs, err := db.NewUserOrgEntityDao(database).ListByCond(ctx, &db.UserOrgCond{UserID: loggedIn.UserInfo.ID, OrgID: defaultOrg.ID})
	if err != nil {
		t.Fatalf("ListUserOrgs failed: %v", err)
	}
	if len(userOrgs) != 1 {
		t.Fatalf("expected 1 user_org record for default org, got %d", len(userOrgs))
	}
	userOrg := userOrgs[0]
	if !userOrg.IsDefault {
		t.Fatal("expected IsDefault true for default org")
	}

	// Verify department was created
	department, err := db.NewDepartmentEntityDao(database).GetByCond(ctx, &db.DepartmentCond{OrgID: defaultOrg.ID, Name: defaultOrg.Name})
	if err != nil {
		t.Fatalf("GetDepartmentByName failed: %v", err)
	}
	if department == nil {
		t.Fatalf("expected default department with name %q", defaultOrg.Name)
	}

	// Verify member department relation
	relations, err := db.ListMemberDepartmentsByUinAndOrgID(ctx, database, userOrg.ID, userOrg.OrgID)
	if err != nil {
		t.Fatalf("ListMemberDepartmentsByUinAndOrgID failed: %v", err)
	}
	if len(relations) != 1 || relations[0].DepartmentID != department.ID || !relations[0].IsPrimary {
		t.Fatalf("unexpected department relation: %#v", relations)
	}
}

func TestAuthServiceCreateOrganizationWithJWT(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	// Register, auto-join default org, choose uin to get a JWT caller
	registered, err := service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "jwt.create@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "JWT Create",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}
	if len(registered.Organizations) != 1 {
		t.Fatalf("expected 1 organization from auto join default org, got %d", len(registered.Organizations))
	}

	// OSS 版本不支持 CreateOrganization
	_, err = service.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name: "第一个组织",
	})
	if !errors.Is(err, accounterror.ErrNotImplementedEdition) {
		t.Fatalf("expected CreateOrganization to be unsupported in OSS, got %v", err)
	}

	chosen, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: registered.RefreshToken,
		Uin:          registered.Organizations[0].Uin,
	})
	if err != nil {
		t.Fatalf("ChooseUin failed: %v", err)
	}

	// Single tenant: OSS 不支持创建组织，无论怎样
	authCtx := localauth.WithContext(ctx, &types.Caller{
		Uin:   chosen.Uin,
		OrgID: chosen.Org.ID,
		Kind:  types.CallerKindUser,
		State: types.AuthStateSucc,
	}, &types.Trace{})
	_, err = service.CreateOrganization(authCtx, &account.CreateOrganizationInput{Name: "第二个组织"})
	if !errors.Is(err, accounterror.ErrNotImplementedEdition) {
		t.Fatalf("expected CreateOrganization to be unsupported, got %v", err)
	}
}

func TestAuthServiceCreateOrganizationEnsuresDefaultWorker(t *testing.T) {
	t.Skip("requires postgres infrastructure for worker provisioning")

	service, database := setupAuthServiceTestWithProvisioning(t)
	ctx := context.Background()

	registered, err := service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "create.worker.org@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "Create Worker Org",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}

	created, err := service.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name:         "新工作组织",
		RefreshToken: registered.RefreshToken,
	})
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}

	// Verify worker deployment was created
	if err := database.First(&types.WorkerDeployment{}, "org_id = ? AND is_default = ?", created.Org.ID, true).Error; err != nil {
		t.Fatalf("expected default worker deployment for org %d: %v", created.Org.ID, err)
	}
}

func TestAuthServiceSwitchOrganizationNotSupported(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	_, err := service.SwitchOrganization(ctx, &account.SwitchOrganizationInput{Uin: 1})
	if !errors.Is(err, accounterror.ErrNotImplementedEdition) {
		t.Fatalf("expected ErrNotImplementedEdition, got %v", err)
	}
}

func TestAuthServiceLoginAttemptLimit(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	_, err := service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "limit@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "Limit User",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}

	for i := 0; i < loginAttemptMaxFailures; i++ {
		_, err = service.LoginByPassword(ctx, &account.LoginByPasswordInput{
			Account:  "limit@example.com",
			Password: "WrongPassword123",
		})
		if !errors.Is(err, accounterror.ErrInvalidAccountOrPassword) {
			t.Fatalf("expected invalid password error on attempt %d, got %v", i+1, err)
		}
	}

	_, err = service.LoginByPassword(ctx, &account.LoginByPasswordInput{
		Account:  "limit@example.com",
		Password: "Password123",
	})
	if !errors.Is(err, accounterror.ErrLoginAttemptsExceeded) {
		t.Fatalf("expected login attempts exceeded, got %v", err)
	}
}

func TestAuthServiceRegisterRejectsInvalidEmailAndPassword(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	_, err := service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "not-an-email",
		Password:        "Password123",
		ConfirmPassword: "Password123",
	})
	if !errors.Is(err, accounterror.ErrInvalidEmailFormat) {
		t.Fatalf("expected invalid email format, got %v", err)
	}

	_, err = service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "valid@example.com",
		Password:        "short",
		ConfirmPassword: "short",
	})
	if !errors.Is(err, accounterror.ErrPasswordTooShort) {
		t.Fatalf("expected password too short, got %v", err)
	}

	_, err = service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "valid@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password456",
	})
	if !errors.Is(err, accounterror.ErrPasswordsDoNotMatch) {
		t.Fatalf("expected passwords do not match, got %v", err)
	}

	_, err = service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "valid@example.com",
		Password:        "PasswordOnly",
		ConfirmPassword: "PasswordOnly",
	})
	if !errors.Is(err, accounterror.ErrPasswordStrength) {
		t.Fatalf("expected password strength error, got %v", err)
	}
}

func TestAuthServicePhoneCodeLoginNewUser(t *testing.T) {
	service, database := setupAuthServiceTest(t)
	ctx := context.Background()

	sent, err := service.SendPhoneLoginCode(ctx, &account.SendPhoneLoginCodeInput{
		Phone: "13800138000",
	})
	if err != nil {
		t.Fatalf("SendPhoneLoginCode failed: %v", err)
	}
	if sent.Phone != "13800138000" || sent.ExpiresIn == 0 {
		t.Fatalf("unexpected send response: %+v", sent)
	}

	loggedIn, err := service.LoginByPhoneCode(ctx, &account.LoginByPhoneCodeInput{
		Phone: "13800138000",
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
	if loggedIn.LoginStatus != account.LoginStatusSuccess {
		t.Fatalf("expected success with auto join default org, got %q", loggedIn.LoginStatus)
	}
	if loggedIn.UserInfo.Phone != "13800138000" {
		t.Fatalf("expected phone in user info, got %q", loggedIn.UserInfo.Phone)
	}
	if loggedIn.UserInfo.Name != "13800138000" {
		t.Fatalf("expected default name to use phone, got %q", loggedIn.UserInfo.Name)
	}
	if len(loggedIn.Organizations) != 1 {
		t.Fatalf("expected 1 organization from auto join default org, got %d", len(loggedIn.Organizations))
	}

	user, err := db.NewUserEntityDao(database).GetByCond(ctx, &db.UserCond{Phone: "13800138000"})
	if err != nil {
		t.Fatalf("GetUserByPhone failed: %v", err)
	}
	if user == nil {
		t.Fatal("expected user to be auto registered")
	}

	// Full flow: ChooseUin directly (already has default org)
	chosen, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: loggedIn.RefreshToken,
		Uin:          loggedIn.Organizations[0].Uin,
		UserID:       loggedIn.UserInfo.ID,
	})
	if err != nil {
		t.Fatalf("ChooseUin failed: %v", err)
	}
	if chosen.JwtToken == "" {
		t.Fatal("expected jwt token from ChooseUin")
	}
	if chosen.RefreshToken == "" {
		t.Fatal("expected new refresh token")
	}
	if len(chosen.Organizations) != 1 {
		t.Fatalf("expected 1 organization, got %d", len(chosen.Organizations))
	}
}

func TestAuthServicePhoneCodeRejectsInvalidCode(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	if _, err := service.SendPhoneLoginCode(ctx, &account.SendPhoneLoginCodeInput{
		Phone: "13900139000",
	}); err != nil {
		t.Fatalf("SendPhoneLoginCode failed: %v", err)
	}

	_, err := service.LoginByPhoneCode(ctx, &account.LoginByPhoneCodeInput{
		Phone: "13900139000",
		Code:  "000000",
	})
	if !errors.Is(err, accounterror.ErrInvalidPhoneCode) {
		t.Fatalf("expected invalid phone code, got %v", err)
	}
}

func TestAuthServicePhoneCodeRejectsResendWithinTwoMinutes(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	first, err := service.SendPhoneLoginCode(ctx, &account.SendPhoneLoginCodeInput{
		Phone: "13700137000",
	})
	if err != nil {
		t.Fatalf("first SendPhoneLoginCode failed: %v", err)
	}
	if first.ResendAfter != int64(phoneCodeResendInterval.Seconds()) {
		t.Fatalf("resend_after = %d, want %d", first.ResendAfter, int64(phoneCodeResendInterval.Seconds()))
	}

	_, err = service.SendPhoneLoginCode(ctx, &account.SendPhoneLoginCodeInput{
		Phone: "13700137000",
	})
	if !errors.Is(err, accounterror.ErrPhoneCodeSendTooOften) {
		t.Fatalf("expected resend-too-often error, got %v", err)
	}
}

func TestAuthServiceRefreshTokenAfterChooseUin(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	_, err := service.SendPhoneLoginCode(ctx, &account.SendPhoneLoginCodeInput{
		Phone: "13900139002",
	})
	if err != nil {
		t.Fatalf("SendPhoneLoginCode failed: %v", err)
	}

	loggedIn, err := service.LoginByPhoneCode(ctx, &account.LoginByPhoneCodeInput{
		Phone: "13900139002",
		Code:  defaultPhoneCode,
	})
	if err != nil {
		t.Fatalf("LoginByPhoneCode failed: %v", err)
	}
	if len(loggedIn.Organizations) != 1 {
		t.Fatalf("expected 1 organization from auto join default org, got %d", len(loggedIn.Organizations))
	}

	chosen, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: loggedIn.RefreshToken,
		Uin:          loggedIn.Organizations[0].Uin,
	})
	if err != nil {
		t.Fatalf("ChooseUin failed: %v", err)
	}

	refreshed, err := service.RefreshToken(ctx, &account.RefreshTokenInput{RefreshToken: chosen.RefreshToken})
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if refreshed.JwtToken == "" {
		t.Fatal("expected jwt token from RefreshToken")
	}
	if refreshed.Uin != chosen.Uin {
		t.Fatalf("expected uin %d, got %d", chosen.Uin, refreshed.Uin)
	}
}

func TestAuthServiceChooseUinRejectsInvalidToken(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	_, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: "invalid-token",
		Uin:          1,
	})
	if !errors.Is(err, accounterror.ErrRefreshTokenInvalid) {
		t.Fatalf("expected refresh token invalid, got %v", err)
	}
}

func TestAuthServiceChooseUinRejectsWrongUser(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	_, err := service.SendPhoneLoginCode(ctx, &account.SendPhoneLoginCodeInput{
		Phone: "13900139003",
	})
	if err != nil {
		t.Fatalf("SendPhoneLoginCode failed: %v", err)
	}
	loggedIn, err := service.LoginByPhoneCode(ctx, &account.LoginByPhoneCodeInput{
		Phone: "13900139003",
		Code:  defaultPhoneCode,
	})
	if err != nil {
		t.Fatalf("LoginByPhoneCode failed: %v", err)
	}
	if len(loggedIn.Organizations) != 1 {
		t.Fatalf("expected 1 organization from auto join default org, got %d", len(loggedIn.Organizations))
	}

	// Try ChooseUin with a non-existent Uin
	_, err = service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: loggedIn.RefreshToken,
		Uin:          99999,
	})
	if !errors.Is(err, accounterror.ErrUserOrgNotFound) {
		t.Fatalf("expected ErrUserOrgNotFound for non-existent uin, got %v", err)
	}
}

func TestAuthServiceAuthSession(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	registered, err := service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "session@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "Session User",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}
	if len(registered.Organizations) != 1 {
		t.Fatalf("expected 1 organization from auto join default org, got %d", len(registered.Organizations))
	}

	chosen, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: registered.RefreshToken,
		Uin:          registered.Organizations[0].Uin,
	})
	if err != nil {
		t.Fatalf("ChooseUin failed: %v", err)
	}

	sessionCtx := localauth.WithContext(ctx, &types.Caller{
		Uin:   chosen.Uin,
		OrgID: chosen.Org.ID,
		Kind:  types.CallerKindUser,
		State: types.AuthStateSucc,
	}, &types.Trace{})

	session, err := service.AuthSession(sessionCtx)
	if err != nil {
		t.Fatalf("AuthSession failed: %v", err)
	}
	if session.Org.ID != chosen.Org.ID {
		t.Fatalf("expected session org %d, got %d", chosen.Org.ID, session.Org.ID)
	}
	if len(session.Organizations) != 1 {
		t.Fatalf("expected 1 organization in session, got %d", len(session.Organizations))
	}
}

func TestAuthServiceLoginByPasswordWithPhone(t *testing.T) {
	service, database := setupAuthServiceTest(t)
	ctx := context.Background()

	if err := database.Create(&types.User{
		PublicID: "usr_phone_login_test",
		Password: "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi",
		Name:     "Phone Login User",
		Phone:    "13900139010",
	}).Error; err != nil {
		t.Fatalf("failed to insert user with phone: %v", err)
	}

	loggedIn, err := service.LoginByPassword(ctx, &account.LoginByPasswordInput{
		Account:  "13900139010",
		Password: "password",
	})
	if err != nil {
		t.Fatalf("LoginByPassword with phone failed: %v", err)
	}
	if loggedIn.RefreshToken == "" {
		t.Fatal("expected refresh token from LoginByPassword with phone")
	}
	if loggedIn.UserInfo.Phone != "13900139010" {
		t.Fatalf("expected phone 13900139010, got %q", loggedIn.UserInfo.Phone)
	}
}
