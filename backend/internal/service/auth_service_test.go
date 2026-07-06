package service

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
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
		&types.Department{},
		&types.MemberDepartment{},
		&types.AuthRefreshToken{},
		&types.AuthLoginAttempt{},
		&types.AuthPhoneVerificationCode{},
		&types.DigitalAssistant{},
		&types.WorkerDeployment{},
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

func setupAuthServiceTestWithProvisioning(t *testing.T) (contract.AuthService, *gorm.DB) {
	t.Helper()

	_, database := setupAuthServiceTest(t)
	provisioning := NewWorkerProvisioningService(database, nil)
	return NewAuthServiceWithProvisioning(database, "test-secret", nil, provisioning), database
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

func TestAuthServiceLoginByEmailWithOrganizationSelection(t *testing.T) {
	service, database := setupAuthServiceTest(t)
	ctx := context.Background()

	registered, err := service.RegisterByEmail(ctx, &contract.RegisterByEmailRequest{
		Email:           "multi.org@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "Multi Org",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}
	user, err := db.GetUserByEmail(ctx, database, "multi.org@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	secondOrg, secondUserOrg := seedAuthServiceUserOrg(t, database, user.ID, 10001, "second_org", "第二组织", false)

	loggedIn, err := service.LoginByEmail(ctx, &contract.LoginByEmailRequest{
		Email:    "multi.org@example.com",
		Password: "Password123",
		OrgID:    secondOrg.ID,
	})
	if err != nil {
		t.Fatalf("LoginByEmail with org failed: %v", err)
	}
	if loggedIn.Org.ID != secondOrg.ID {
		t.Fatalf("expected selected org %d, got %d", secondOrg.ID, loggedIn.Org.ID)
	}
	if loggedIn.Uin != secondUserOrg.Uin {
		t.Fatalf("expected selected user org uin %d, got %d", secondUserOrg.Uin, loggedIn.Uin)
	}
	if len(loggedIn.Organizations) != 2 {
		t.Fatalf("expected two organizations, got %+v", loggedIn.Organizations)
	}

	refreshed, err := service.RefreshToken(ctx, &contract.RefreshTokenRequest{RefreshToken: loggedIn.RefreshToken})
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if refreshed.Org.ID != secondOrg.ID || refreshed.Uin != secondUserOrg.Uin {
		t.Fatalf("expected refresh to preserve selected org, got org=%d uin=%d", refreshed.Org.ID, refreshed.Uin)
	}

	switchCtx := localauth.WithContext(ctx, &types.Caller{
		Uin:   registered.Uin,
		OrgID: registered.Org.ID,
		Kind:  types.CallerKindUser,
		State: types.AuthStateSucc,
	}, &types.Trace{})
	switched, err := service.SwitchOrganization(switchCtx, &contract.SwitchOrganizationRequest{OrgID: secondOrg.ID})
	if err != nil {
		t.Fatalf("SwitchOrganization failed: %v", err)
	}
	if switched.Org.ID != secondOrg.ID || switched.Uin != secondUserOrg.Uin {
		t.Fatalf("expected switch to selected org, got org=%d uin=%d", switched.Org.ID, switched.Uin)
	}

	sessionCtx := localauth.WithContext(ctx, &types.Caller{
		Uin:   secondUserOrg.Uin,
		OrgID: secondOrg.ID,
		Kind:  types.CallerKindUser,
		State: types.AuthStateSucc,
	}, &types.Trace{})
	session, err := service.AuthSession(sessionCtx)
	if err != nil {
		t.Fatalf("AuthSession failed: %v", err)
	}
	if session.Org.ID != secondOrg.ID {
		t.Fatalf("expected session org %d, got %d", secondOrg.ID, session.Org.ID)
	}
	if len(session.Organizations) != 2 {
		t.Fatalf("expected two session organizations, got %+v", session.Organizations)
	}
}

func TestAuthServiceLoginByEmailRejectsUnjoinedOrganization(t *testing.T) {
	service, database := setupAuthServiceTest(t)
	ctx := context.Background()

	if _, err := service.RegisterByEmail(ctx, &contract.RegisterByEmailRequest{
		Email:           "not.member@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "Not Member",
	}); err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}
	foreignOrg := &types.Organization{
		PublicID: "org_foreign",
		Code:     "foreign_org",
		Name:     "外部组织",
		Type:     "company",
		Status:   "active",
	}
	if err := db.CreateOrg(ctx, database, foreignOrg); err != nil {
		t.Fatalf("CreateOrg failed: %v", err)
	}

	_, err := service.LoginByEmail(ctx, &contract.LoginByEmailRequest{
		Email:    "not.member@example.com",
		Password: "Password123",
		OrgID:    foreignOrg.ID,
	})
	if !errors.Is(err, errAuthUserOrgNotAllowed) {
		t.Fatalf("expected user org not allowed, got %v", err)
	}
}

func TestAuthServiceCreateOrganizationSwitchesSession(t *testing.T) {
	service, database := setupAuthServiceTest(t)
	ctx := context.Background()

	registered, err := service.RegisterByEmail(ctx, &contract.RegisterByEmailRequest{
		Email:           "create.org@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "Create Org",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}

	authCtx := localauth.WithContext(ctx, &types.Caller{
		Uin:   registered.Uin,
		OrgID: registered.Org.ID,
		Kind:  types.CallerKindUser,
		State: types.AuthStateSucc,
	}, &types.Trace{})
	created, err := service.CreateOrganization(authCtx, &contract.CreateOrganizationRequest{Name: "新组织"})
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}
	if created.Org.Name != "新组织" || created.Org.ID == registered.Org.ID {
		t.Fatalf("unexpected created organization response: %+v", created)
	}
	if created.Uin == 0 || created.Uin == registered.Uin {
		t.Fatalf("expected new user org uin, got %d registered %d", created.Uin, registered.Uin)
	}
	if len(created.Organizations) != 2 {
		t.Fatalf("expected two organizations after create, got %+v", created.Organizations)
	}

	userOrg, err := db.GetUserOrgByUin(ctx, database, created.Uin)
	if err != nil {
		t.Fatalf("GetUserOrgByUin failed: %v", err)
	}
	if userOrg == nil || userOrg.OrgID != created.Org.ID {
		t.Fatalf("unexpected created user org: %#v", userOrg)
	}
	if created.Uin != userOrg.ID || userOrg.Uin != userOrg.ID {
		t.Fatalf("expected created user org id as uin, got response=%d user_org=%#v", created.Uin, userOrg)
	}

	department, err := db.GetDepartmentByName(ctx, database, created.Org.ID, "默认部门")
	if err != nil {
		t.Fatalf("GetDepartmentByName failed: %v", err)
	}
	if department == nil {
		t.Fatal("expected default department")
	}
	if len(department.ParentIDs) != 0 {
		t.Fatalf("expected default department parent_ids to be empty, got %#v", department.ParentIDs)
	}

	relations, err := db.ListMemberDepartmentsByUin(ctx, database, userOrg.Uin)
	if err != nil {
		t.Fatalf("ListMemberDepartmentsByUin failed: %v", err)
	}
	if len(relations) != 1 || relations[0].DepartmentID != department.ID || !relations[0].IsPrimary {
		t.Fatalf("unexpected department relation: %#v", relations)
	}

	claims, err := localauth.ParseUserToken(created.JwtToken, "test-secret")
	if err != nil {
		t.Fatalf("ParseUserToken failed: %v", err)
	}
	if claims.Uin != created.Uin {
		t.Fatalf("expected token uin %d, got %d", created.Uin, claims.Uin)
	}

	_, err = service.CreateOrganization(authCtx, &contract.CreateOrganizationRequest{Name: "第三个组织"})
	if !errors.Is(err, errAuthOrganizationLimitExceeded) {
		t.Fatalf("expected organization limit error, got %v", err)
	}
}

func TestAuthServiceCreateOrganizationEnsuresDefaultWorker(t *testing.T) {
	service, database := setupAuthServiceTestWithProvisioning(t)
	ctx := context.Background()

	registered, err := service.RegisterByEmail(ctx, &contract.RegisterByEmailRequest{
		Email:           "create.worker.org@example.com",
		Password:        "Password123",
		ConfirmPassword: "Password123",
		Name:            "Create Worker Org",
	})
	if err != nil {
		t.Fatalf("RegisterByEmail failed: %v", err)
	}

	authCtx := localauth.WithContext(ctx, &types.Caller{
		Uin:   registered.Uin,
		OrgID: registered.Org.ID,
		Kind:  types.CallerKindUser,
		State: types.AuthStateSucc,
	}, &types.Trace{})
	created, err := service.CreateOrganization(authCtx, &contract.CreateOrganizationRequest{Name: "新工作组织"})
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}

	deployment, err := db.GetDefaultWorkerDeployment(ctx, database, created.Org.ID)
	if err != nil {
		t.Fatalf("GetDefaultWorkerDeployment failed: %v", err)
	}
	if deployment == nil {
		t.Fatal("expected default worker deployment")
	}
	if deployment.OrgID != created.Org.ID || deployment.WorkerID == 0 {
		t.Fatalf("unexpected worker deployment: %#v", deployment)
	}

	assistant, err := db.GetDigitalAssistantByCode(ctx, database, defaultWorkerCode(created.Org.ID))
	if err != nil {
		t.Fatalf("GetDigitalAssistantByCode failed: %v", err)
	}
	if assistant == nil || assistant.OwnerID != registered.Uin {
		t.Fatalf("unexpected default assistant: %#v", assistant)
	}
	if deployment.DigitalAssistantID != assistant.ID {
		t.Fatalf("expected deployment assistant %d, got %d", assistant.ID, deployment.DigitalAssistantID)
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

func seedAuthServiceUserOrg(t *testing.T, database *gorm.DB, userID, uin uint, code, name string, isDefault bool) (*types.Organization, *types.UserOrg) {
	t.Helper()

	ctx := context.Background()
	org := &types.Organization{
		PublicID: code,
		Code:     code,
		Name:     name,
		Type:     "company",
		Status:   "active",
	}
	if err := db.CreateOrg(ctx, database, org); err != nil {
		t.Fatalf("CreateOrg failed: %v", err)
	}
	userOrg := &types.UserOrg{
		Uin:       uin,
		UserID:    userID,
		OrgID:     org.ID,
		IsDefault: isDefault,
	}
	if err := db.CreateUserOrg(ctx, database, userOrg); err != nil {
		t.Fatalf("CreateUserOrg failed: %v", err)
	}
	return org, userOrg
}
