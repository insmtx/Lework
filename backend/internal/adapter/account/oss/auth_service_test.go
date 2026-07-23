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
	if registered.LoginStatus != account.LoginStatusNeedCreateCompany {
		t.Fatalf("expected login_status %q, got %q", account.LoginStatusNeedCreateCompany, registered.LoginStatus)
	}
	if registered.UserInfo.Email != "new.user@example.com" {
		t.Fatalf("expected normalized email, got %q", registered.UserInfo.Email)
	}

	var orgCount int64
	if err := database.Model(&types.Organization{}).Count(&orgCount).Error; err != nil {
		t.Fatalf("count organizations: %v", err)
	}
	if orgCount != 1 {
		t.Fatalf("expected registration not to create organization, got %d organizations", orgCount)
	}

	var userOrgCount int64
	if err := database.Model(&types.UserOrg{}).Count(&userOrgCount).Error; err != nil {
		t.Fatalf("count user_orgs: %v", err)
	}
	if userOrgCount != 0 {
		t.Fatalf("expected no user_org records, got %d", userOrgCount)
	}

	// Create organization via refresh_token
	created, err := service.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name:         "新组织",
		RefreshToken: registered.RefreshToken,
	})
	if err != nil {
		t.Fatalf("CreateOrganization with refresh token failed: %v", err)
	}
	if created.Org.Name != "新组织" {
		t.Fatalf("expected org name '新组织', got %q", created.Org.Name)
	}
	if created.JwtToken != "" {
		t.Fatal("expected no jwt token from CreateOrganization")
	}
	if created.Uin == 0 {
		t.Fatal("expected uin in create response")
	}

	// ChooseUin to get JWT
	chosen, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: registered.RefreshToken,
		Uin:          created.Uin,
		UserID:       registered.UserInfo.ID,
	})
	if err != nil {
		t.Fatalf("ChooseUin failed: %v", err)
	}
	if chosen.JwtToken == "" {
		t.Fatal("expected jwt token from ChooseUin")
	}
	if chosen.Uin != created.Uin {
		t.Fatalf("expected uin %d, got %d", created.Uin, chosen.Uin)
	}
	if len(chosen.Organizations) != 1 {
		t.Fatalf("expected 1 organization, got %d", len(chosen.Organizations))
	}
}

func TestAuthServiceLoginByEmail(t *testing.T) {
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

	// Create organization first
	created, err := service.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name:         "我的组织",
		RefreshToken: registered.RefreshToken,
	})
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}

	// ChooseUin to enter
	chosen, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: registered.RefreshToken,
		Uin:          created.Uin,
		UserID:       registered.UserInfo.ID,
	})
	if err != nil {
		t.Fatalf("ChooseUin failed: %v", err)
	}

	// Now login by email - should return refreshtoken, no JWT
	loggedIn, err := service.LoginByEmail(ctx, &account.LoginByEmailInput{
		Email:    "login.user@example.com",
		Password: "Password123",
	})
	if err != nil {
		t.Fatalf("LoginByEmail failed: %v", err)
	}
	if loggedIn.JwtToken != "" {
		t.Fatal("expected no jwt token from LoginByEmail")
	}
	if loggedIn.RefreshToken == "" {
		t.Fatal("expected refresh token from LoginByEmail")
	}
	if loggedIn.LoginStatus != account.LoginStatusSuccess {
		t.Fatalf("expected login_status success, got %q", loggedIn.LoginStatus)
	}
	if len(loggedIn.Organizations) != 1 {
		t.Fatalf("expected 1 organization, got %d", len(loggedIn.Organizations))
	}

	// Use the new refresh token to ChooseUin and get JWT
	chosen2, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: loggedIn.RefreshToken,
		Uin:          created.Uin,
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

func TestAuthServiceLoginByEmailRejectsWrongPassword(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	if _, err := service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "wrong.pass@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
	}); err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}

	_, err := service.LoginByEmail(ctx, &account.LoginByEmailInput{
		Email:    "wrong.pass@example.com",
		Password: "WrongPassword999",
	})
	if !errors.Is(err, accounterror.ErrInvalidEmailOrPassword) {
		t.Fatalf("expected invalid email or password, got %v", err)
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
	if loggedIn.LoginStatus != account.LoginStatusNeedCreateCompany {
		t.Fatalf("expected need_create_company, got %q", loggedIn.LoginStatus)
	}
	if loggedIn.JwtToken != "" {
		t.Fatal("expected no jwt token for new user")
	}

	created, err := service.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name:         "手机登录组织",
		RefreshToken: loggedIn.RefreshToken,
	})
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}
	if created.Org.Name != "手机登录组织" {
		t.Fatalf("unexpected org name: %q", created.Org.Name)
	}
	if created.JwtToken != "" {
		t.Fatal("expected no jwt token from CreateOrganization")
	}

	userOrg, err := db.GetUserOrgByUin(ctx, database, created.Uin)
	if err != nil {
		t.Fatalf("GetUserOrgByUin failed: %v", err)
	}
	if userOrg == nil || userOrg.OrgID != created.Org.ID {
		t.Fatalf("unexpected user org: %#v", userOrg)
	}
	if !userOrg.IsDefault {
		t.Fatal("expected IsDefault true for first org")
	}

	// Verify department was created
	department, err := db.GetDepartmentByName(ctx, database, created.Org.ID, created.Org.Name)
	if err != nil {
		t.Fatalf("GetDepartmentByName failed: %v", err)
	}
	if department == nil {
		t.Fatalf("expected default department with name %q", created.Org.Name)
	}

	// Verify member department relation
	relations, err := db.ListMemberDepartmentsByUin(ctx, database, userOrg.Uin)
	if err != nil {
		t.Fatalf("ListMemberDepartmentsByUin failed: %v", err)
	}
	if len(relations) != 1 || relations[0].DepartmentID != department.ID || !relations[0].IsPrimary {
		t.Fatalf("unexpected department relation: %#v", relations)
	}
}

func TestAuthServiceCreateOrganizationWithJWT(t *testing.T) {
	service, _ := setupAuthServiceTest(t)
	ctx := context.Background()

	// Register, create org, choose uin to get a JWT caller
	registered, err := service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "jwt.create@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "JWT Create",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}

	created, err := service.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name:         "第一个组织",
		RefreshToken: registered.RefreshToken,
	})
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}

	chosen, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: registered.RefreshToken,
		Uin:          created.Uin,
	})
	if err != nil {
		t.Fatalf("ChooseUin failed: %v", err)
	}

	// Single tenant: already has 1 org, creating another should hit limit
	authCtx := localauth.WithContext(ctx, &types.Caller{
		Uin:   chosen.Uin,
		OrgID: chosen.Org.ID,
		Kind:  types.CallerKindUser,
		State: types.AuthStateSucc,
	}, &types.Trace{})
	_, err = service.CreateOrganization(authCtx, &account.CreateOrganizationInput{Name: "第二个组织"})
	if !errors.Is(err, accounterror.ErrOrganizationLimitExceeded) {
		t.Fatalf("expected organization limit error, got %v", err)
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
		_, err = service.LoginByEmail(ctx, &account.LoginByEmailInput{
			Email:    "limit@example.com",
			Password: "WrongPassword123",
		})
		if !errors.Is(err, accounterror.ErrInvalidEmailOrPassword) {
			t.Fatalf("expected invalid password error on attempt %d, got %v", i+1, err)
		}
	}

	_, err = service.LoginByEmail(ctx, &account.LoginByEmailInput{
		Email:    "limit@example.com",
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
	if loggedIn.LoginStatus != account.LoginStatusNeedCreateCompany {
		t.Fatalf("expected need_create_company, got %q", loggedIn.LoginStatus)
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

	// Full flow: create org + ChooseUin
	created, err := service.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name:         "手机组织",
		RefreshToken: loggedIn.RefreshToken,
	})
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}

	chosen, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: loggedIn.RefreshToken,
		Uin:          created.Uin,
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
	service, database := setupAuthServiceTest(t)
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

	created, err := service.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name:         "刷令牌组织",
		RefreshToken: loggedIn.RefreshToken,
	})
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}

	chosen, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: loggedIn.RefreshToken,
		Uin:          created.Uin,
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

	_ = database
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

	// Register another user and create org, then try to ChooseUin their org
	reg2, err := service.RegisterByEmail(ctx, &account.RegisterByEmailInput{
		Email:           "other.user@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "Other User",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}
	created2, err := service.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name:         "别人的组织",
		RefreshToken: reg2.RefreshToken,
	})
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}

	_, err = service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: loggedIn.RefreshToken,
		Uin:          created2.Uin,
	})
	if err == nil {
		t.Fatalf("expected user org not allowed error, got nil, created2.Uin=%d", created2.Uin)
	}
	if !errors.Is(err, accounterror.ErrUserOrgNotAllowed) {
		t.Fatalf("expected user org not allowed, got %v, created2.Uin=%d", err, created2.Uin)
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

	created, err := service.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name:         "会话组织",
		RefreshToken: registered.RefreshToken,
	})
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}

	chosen, err := service.ChooseUin(ctx, &account.ChooseUinInput{
		RefreshToken: registered.RefreshToken,
		Uin:          created.Uin,
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
