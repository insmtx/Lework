//go:build enterprise && integration

package enterprise

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter/account"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/testutil"
	"github.com/insmtx/Leros/backend/pkg/accounterror"
	"github.com/insmtx/Leros/backend/types"
)

func iamTestEnv(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("IAM_TEST_ENV"); v != "" {
		return v
	}
	return "test"
}

func iamCfgFromEnvOrConfig(t *testing.T) *config.IAMConfig {
	t.Helper()

	baseURL := os.Getenv("IAM_BASE_URL")
	domainName := os.Getenv("IAM_DOMAIN_NAME")

	if baseURL == "" || domainName == "" {
		cfg := testutil.LoadTestConfig(t)
		if cfg.Auth != nil {
			if baseURL == "" {
				baseURL = cfg.Auth.BaseURL
			}
			if domainName == "" {
				domainName = cfg.Auth.DomainName
			}
		}
	}

	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080"
	}
	if domainName == "" {
		domainName = "leros.test.insmtx.com"
	}

	return &config.IAMConfig{BaseURL: baseURL, DomainName: domainName}
}

func testAccount(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("IAM_TEST_ACCOUNT"); v != "" {
		return v
	}
	t.Skip("IAM_TEST_ACCOUNT not set, skipping auth-required test")
	return ""
}

func testPassword(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("IAM_TEST_PASSWORD"); v != "" {
		return v
	}
	t.Skip("IAM_TEST_PASSWORD not set, skipping auth-required test")
	return ""
}

func testSMSPhone(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("IAM_TEST_SMS_PHONE"); v != "" {
		return v
	}
	t.Skip("IAM_TEST_SMS_PHONE not set, skipping SMS test")
	return ""
}

func testMemberPhone(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("IAM_TEST_MEMBER_PHONE"); v != "" {
		return v
	}
	t.Skip("IAM_TEST_MEMBER_PHONE not set, skipping member test")
	return ""
}

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.Setup(t)
}

func newTestIAMClient(t *testing.T) *iamClient {
	t.Helper()
	return newIAMClient(iamCfgFromEnvOrConfig(t), iamTestEnv(t))
}

func setupIAMAuth(t *testing.T) *auth {
	t.Helper()
	return NewAuth(nil, iamCfgFromEnvOrConfig(t), iamTestEnv(t), nil)
}

func setupIAMUser(t *testing.T) *user {
	t.Helper()
	return NewUser(newTestIAMClient(t), setupTestDB(t))
}

func setupIAMOrg(t *testing.T) (*org, *gorm.DB) {
	t.Helper()
	db := setupTestDB(t)
	client := newTestIAMClient(t)
	return NewOrg(db, client, nil), db
}

func setupIAMDepartment(t *testing.T) *department {
	t.Helper()
	return NewDepartment(newTestIAMClient(t))
}

func loginJWT(t *testing.T, svc *auth) (jwtToken string, tokens *account.AuthTokens) {
	t.Helper()

	loginResp, err := svc.LoginByEmail(context.Background(), &account.LoginByEmailInput{
		Email:    testAccount(t),
		Password: testPassword(t),
	})
	if err != nil {
		t.Fatalf("LoginByEmail: %v", err)
	}

	if loginResp.JwtToken != "" {
		return loginResp.JwtToken, loginResp
	}

	chosen, err := svc.ChooseUin(context.Background(), &account.ChooseUinInput{
		RefreshToken: loginResp.RefreshToken,
		Uin:          loginResp.Uin,
		UserID:       loginResp.UserID,
		LoginWay:     loginResp.LoginWay,
	})
	if err != nil {
		t.Fatalf("ChooseUin: %v", err)
	}
	if chosen.JwtToken == "" {
		t.Fatal("ChooseUin returned empty JwtToken")
	}
	return chosen.JwtToken, loginResp
}

func loginContext(t *testing.T) context.Context {
	t.Helper()
	svc := setupIAMAuth(t)
	jwt, _ := loginJWT(t, svc)
	ctx := localauth.WithBearerToken(context.Background(), jwt)
	return ctx
}

func loginContextWithCaller(t *testing.T) context.Context {
	t.Helper()
	svc := setupIAMAuth(t)
	jwt, loginResp := loginJWT(t, svc)
	ctx := context.Background()
	ctx = localauth.WithBearerToken(ctx, jwt)
	ctx = localauth.WithContext(ctx, &types.Caller{
		Uin:   loginResp.Uin,
		State: types.AuthStateSucc,
	}, nil)
	return ctx
}

func randSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func TestLoginByEmail_Success(t *testing.T) {
	svc := setupIAMAuth(t)
	tokens, err := svc.LoginByEmail(context.Background(), &account.LoginByEmailInput{
		Email:    testAccount(t),
		Password: testPassword(t),
	})
	if err != nil {
		t.Fatalf("LoginByEmail failed: %v", err)
	}
	if tokens.JwtToken == "" {
		// 多 UIN 场景 IAM 不直接签发 JWT，需后续调用 ChooseUin
		t.Log("JwtToken is empty (multi-UIN account)")
	}
	if tokens.RefreshToken == "" {
		t.Fatal("RefreshToken is empty")
	}
	if tokens.LoginStatus != "success" {
		t.Fatalf("LoginStatus = %q, want success", tokens.LoginStatus)
	}
	if tokens.Uin == 0 {
		t.Fatal("Uin is 0")
	}
	if tokens.UserInfo.ID == 0 {
		t.Log("UserInfo.ID is 0 (IAM does not return user id in LoginByPassword)")
	}
	if tokens.UserInfo.Name == "" {
		t.Fatal("UserInfo.Name is empty")
	}
	t.Logf("login success: user_id=%d uin=%d name=%s", tokens.UserInfo.ID, tokens.Uin, tokens.UserInfo.Name)
}

func TestLoginByEmail_InvalidPassword(t *testing.T) {
	svc := setupIAMAuth(t)
	_, err := svc.LoginByEmail(context.Background(), &account.LoginByEmailInput{
		Email:    testAccount(t),
		Password: "WRONG_PASSWORD_12345",
	})
	if !errors.Is(err, accounterror.ErrInvalidEmailOrPassword) {
		t.Fatalf("expected ErrInvalidEmailOrPassword, got %v", err)
	}
}

func TestLoginByEmail_EmptyEmail(t *testing.T) {
	svc := setupIAMAuth(t)
	_, err := svc.LoginByEmail(context.Background(), &account.LoginByEmailInput{
		Email:    "",
		Password: "anything",
	})
	if !errors.Is(err, accounterror.ErrEmailRequired) {
		t.Fatalf("expected ErrEmailRequired, got %v", err)
	}
}

func TestLoginByEmail_EmptyPassword(t *testing.T) {
	svc := setupIAMAuth(t)
	_, err := svc.LoginByEmail(context.Background(), &account.LoginByEmailInput{
		Email:    testAccount(t),
		Password: "",
	})
	if !errors.Is(err, accounterror.ErrPasswordRequired) {
		t.Fatalf("expected ErrPasswordRequired, got %v", err)
	}
}

func TestRefreshToken_Success(t *testing.T) {
	svc := setupIAMAuth(t)
	initial, err := svc.LoginByEmail(context.Background(), &account.LoginByEmailInput{
		Email:    testAccount(t),
		Password: testPassword(t),
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if initial.RefreshToken == "" {
		t.Fatal("RefreshToken is empty after login")
	}

	refreshed, err := svc.RefreshToken(context.Background(), &account.RefreshTokenInput{
		RefreshToken: initial.RefreshToken,
		UinID:        initial.Uin,
		UserID:       initial.UserID,
		LoginWay:     initial.LoginWay,
	})
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if refreshed.JwtToken == "" {
		t.Fatal("refreshed JwtToken is empty")
	}
	if refreshed.Uin == 0 {
		t.Fatal("refreshed Uin is 0")
	}
	t.Logf("refresh token success: new_uin=%d", refreshed.Uin)
}

func TestRefreshToken_EmptyRefreshToken(t *testing.T) {
	svc := setupIAMAuth(t)
	_, err := svc.RefreshToken(context.Background(), &account.RefreshTokenInput{
		RefreshToken: "",
	})
	if !errors.Is(err, accounterror.ErrRefreshTokenRequired) {
		t.Fatalf("expected ErrRefreshTokenRequired, got %v", err)
	}
}

func TestSendPhoneLoginCode_Success(t *testing.T) {
	phone := testSMSPhone(t)
	svc := setupIAMAuth(t)
	resp, err := svc.SendPhoneLoginCode(context.Background(), &account.SendPhoneLoginCodeInput{
		Phone: phone,
	})
	if err != nil {
		t.Fatalf("SendPhoneLoginCode failed: %v", err)
	}
	if !strings.Contains(resp.Phone, "****") {
		t.Fatalf("Phone = %q, want masked phone with ****", resp.Phone)
	}
	if resp.ExpiresIn <= 0 {
		t.Fatalf("ExpiresIn = %d, want > 0", resp.ExpiresIn)
	}
	t.Logf("send sms code success: expires_in=%d resend_after=%d", resp.ExpiresIn, resp.ResendAfter)
}

func TestAuthSession_Success(t *testing.T) {
	ctx := loginContext(t)
	svc := setupIAMAuth(t)
	session, err := svc.AuthSession(ctx)
	if err != nil {
		t.Fatalf("AuthSession failed: %v", err)
	}
	if session.UserInfo.ID == 0 {
		t.Fatal("UserInfo.ID is 0")
	}
	if session.UserInfo.Name == "" {
		t.Fatal("UserInfo.Name is empty")
	}
	if len(session.Organizations) == 0 {
		t.Fatal("Organizations list is empty")
	}
	t.Logf("auth session: user_id=%d name=%s orgs=%d", session.UserInfo.ID, session.UserInfo.Name, len(session.Organizations))
}

func TestCreateOrganization_Success(t *testing.T) {
	ctx := loginContextWithCaller(t)
	svc := setupIAMAuth(t)
	orgName := "test-enterprise-org-" + randSuffix()
	tokens, err := svc.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name: orgName,
	})
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}
	if tokens.Uin == 0 {
		t.Fatal("Uin is 0")
	}
	if tokens.Org.Name != orgName {
		t.Fatalf("Org.Name = %q, want %q", tokens.Org.Name, orgName)
	}
	t.Logf("create org success: uin=%d org_id=%d name=%s", tokens.Uin, tokens.Org.ID, tokens.Org.Name)
}

func TestCreateOrganization_EmptyName(t *testing.T) {
	ctx := loginContextWithCaller(t)
	svc := setupIAMAuth(t)
	_, err := svc.CreateOrganization(ctx, &account.CreateOrganizationInput{
		Name: "",
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCreateOrganization_NoCaller(t *testing.T) {
	svc := setupIAMAuth(t)
	_, err := svc.CreateOrganization(context.Background(), &account.CreateOrganizationInput{
		Name: "should-fail",
	})
	if !errors.Is(err, accounterror.ErrLoginRequired) {
		t.Fatalf("expected ErrLoginRequired, got %v", err)
	}
}

func TestSwitchOrganization_Success(t *testing.T) {
	createCtx := loginContextWithCaller(t)
	svc := setupIAMAuth(t)

	switchName := "test-switch-org-" + randSuffix()
	created, err := svc.CreateOrganization(createCtx, &account.CreateOrganizationInput{
		Name: switchName,
	})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}

	loginCtx := loginContextWithCaller(t)
	tokens, err := svc.SwitchOrganization(loginCtx, &account.SwitchOrganizationInput{
		Uin: created.Uin,
	})
	if err != nil {
		t.Fatalf("SwitchOrganization failed: %v", err)
	}
	if tokens.JwtToken == "" {
		t.Fatal("expected JwtToken after switch")
	}
	t.Logf("switch org success: new_token_len=%d", len(tokens.JwtToken))
}

func TestSwitchOrganization_ZeroOrgID(t *testing.T) {
	ctx := loginContext(t)
	svc := setupIAMAuth(t)
	_, err := svc.SwitchOrganization(ctx, &account.SwitchOrganizationInput{
		Uin: 0,
	})
	if !errors.Is(err, accounterror.ErrOrgNotFound) {
		t.Fatalf("expected ErrOrgNotFound, got %v", err)
	}
}

// ── User ─────────────────────────────────────────────────────────────────────

func TestGetUser_Success(t *testing.T) {
	ctx := loginContext(t)
	svc := setupIAMUser(t)
	user, err := svc.GetUser(ctx, "", "")
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if user.ID == 0 {
		t.Fatal("User ID is 0")
	}
	if user.Name == "" {
		t.Fatal("User Name is empty")
	}
	t.Logf("get user: id=%d name=%s email=%s", user.ID, user.Name, user.Email)
}

func TestUpdateUser_Success(t *testing.T) {
	ctx := loginContext(t)
	svc := setupIAMUser(t)

	newName := "updated-test-name"
	updated, err := svc.UpdateUser(ctx, "", &account.UpdateUserInput{
		Name: newName,
	})
	if err != nil {
		t.Fatalf("UpdateUser failed: %v", err)
	}
	if updated.Name != newName {
		t.Fatalf("Name = %q, want %q", updated.Name, newName)
	}
	t.Logf("update user success: name=%s", updated.Name)
}

func TestCreateUser_Success(t *testing.T) {
	ctx := loginContext(t)
	svc := setupIAMUser(t)
	created, err := svc.CreateUser(ctx, &account.CreateUserInput{
		Email: "integration-test@yygu.cn",
		Name:  "Integration Test User",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if created.Email != "integration-test@yygu.cn" {
		t.Fatalf("Email = %q, want %q", created.Email, "integration-test@yygu.cn")
	}
	t.Logf("create user success: name=%s email=%s", created.Name, created.Email)
}

func TestGetUserByUin(t *testing.T) {
	ctx := loginContextWithCaller(t)
	authSvc := setupIAMAuth(t)
	session, err := authSvc.AuthSession(ctx)
	if err != nil {
		t.Fatalf("AuthSession: %v", err)
	}
	if len(session.Organizations) == 0 {
		t.Skip("no organizations found")
	}
	uin := session.Organizations[0].ID
	if uin == 0 {
		t.Skip("organization ID is 0")
	}

	_, loginResp := loginJWT(t, setupIAMAuth(t))
	uin = loginResp.Uin
	if uin == 0 {
		t.Skip("login Uin is 0")
	}

	ctx2 := loginContext(t)
	userSvc := setupIAMUser(t)
	user, err := userSvc.GetUserByUin(ctx2, uin)
	if err != nil {
		t.Fatalf("GetUserByUin failed: %v", err)
	}
	if user.Uin == 0 {
		t.Fatal("User Uin is 0")
	}
	t.Logf("get user by uin %d: name=%s email=%s", uin, user.Name, user.Email)
}

func TestListUser(t *testing.T) {
	ctx := loginContext(t)
	svc := setupIAMUser(t)
	users, err := svc.ListUser(ctx, &account.ListUserInput{
		Pagination: types.Pagination{Offset: 0, Limit: 10},
	})
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if len(users.Items) == 0 {
		t.Fatal("user list is empty")
	}
	t.Logf("list users: total=%d items=%d", users.Total, len(users.Items))
}

func TestGetUserByGithubID_NotImplemented(t *testing.T) {
	ctx := loginContext(t)
	svc := setupIAMUser(t)
	_, err := svc.GetUserByGithubID(ctx, 12345)
	if !errors.Is(err, accounterror.ErrNotImplementedEdition) {
		t.Fatalf("expected ErrNotImplementedEdition, got %v", err)
	}
}

func TestGetUsersByPublicIDs_NotImplemented(t *testing.T) {
	ctx := loginContext(t)
	svc := setupIAMUser(t)
	_, err := svc.GetUsersByPublicIDs(ctx, []string{"user-1"})
	if !errors.Is(err, accounterror.ErrNotImplementedEdition) {
		t.Fatalf("expected ErrNotImplementedEdition, got %v", err)
	}
}

// ── Org ──────────────────────────────────────────────────────────────────────

func TestOrgRepo_GetOrg(t *testing.T) {
	ctx := loginContext(t)
	svc, _ := setupIAMOrg(t)

	org, err := svc.GetOrg(ctx, "", "")
	if err != nil {
		t.Fatalf("GetOrg failed: %v", err)
	}
	if org == nil {
		t.Fatal("org is nil")
	}
	if org.Name == "" {
		t.Fatal("org name is empty")
	}
	if org.PublicID == "" {
		t.Fatal("org public_id is empty")
	}
	t.Logf("get org: name=%s public_id=%s", org.Name, org.PublicID)
}

func TestOrgRepo_UpdateOrg(t *testing.T) {
	ctx := loginContext(t)
	svc, _ := setupIAMOrg(t)

	newDesc := "updated-by-integration-test"
	org, err := svc.UpdateOrg(ctx, "", &account.UpdateOrgInput{
		Description: &newDesc,
	})
	if err != nil {
		t.Fatalf("UpdateOrg failed: %v", err)
	}
	if org.Description != newDesc {
		t.Fatalf("Description = %q, want %q", org.Description, newDesc)
	}
	t.Logf("update org success: desc=%s", org.Description)
}

func TestOrgRepo_ListOrgs(t *testing.T) {
	ctx := loginContext(t)
	svc, _ := setupIAMOrg(t)

	orgs, err := svc.ListOrgs(ctx, &account.ListOrgsInput{
		Pagination: types.Pagination{Offset: 0, Limit: 10},
	})
	if err != nil {
		t.Fatalf("ListOrgs failed: %v", err)
	}
	if len(orgs.Items) == 0 {
		t.Fatal("org list is empty")
	}
	t.Logf("list orgs: total=%d items=%d", orgs.Total, len(orgs.Items))
}

func TestOrgRepo_CreateOrgMember(t *testing.T) {
	ctx := loginContext(t)
	svc, _ := setupIAMOrg(t)

	memberPhone := testMemberPhone(t)
	memberName := "test-member-" + randSuffix()
	member, err := svc.CreateOrgMember(ctx, &account.CreateOrgMemberInput{
		Name:  memberName,
		Phone: memberPhone,
	})
	if err != nil {
		t.Fatalf("CreateOrgMember failed: %v", err)
	}
	if member.Uin == 0 {
		t.Fatal("member Uin is 0")
	}
	t.Logf("create org member success: uin=%d name=%s", member.Uin, member.UserName)
}

func TestOrgRepo_ListOrgMembers(t *testing.T) {
	ctx := loginContext(t)
	svc, _ := setupIAMOrg(t)

	members, err := svc.ListOrgMembers(ctx, &account.ListOrgMembersInput{
		Pagination: types.Pagination{Offset: 0, Limit: 10},
	})
	if err != nil {
		t.Fatalf("ListOrgMembers failed: %v", err)
	}
	if len(members.Items) == 0 {
		t.Fatal("org member list is empty")
	}
	t.Logf("list org members: total=%d items=%d", members.Total, len(members.Items))
}

// ── Department ───────────────────────────────────────────────────────────────

func TestDepartment_Create(t *testing.T) {
	ctx := loginContext(t)
	svc := setupIAMDepartment(t)

	deptName := "test-dept-create-" + randSuffix()
	dept, err := svc.CreateDepartment(ctx, &account.CreateDepartmentInput{
		Name:     deptName,
		ParentID: 0,
		Sort:     1000,
	})
	if err != nil {
		t.Fatalf("CreateDepartment failed: %v", err)
	}
	if dept.ID == 0 {
		t.Fatal("Department ID is 0")
	}
	if dept.Name != deptName {
		t.Fatalf("Name = %q, want %q", dept.Name, deptName)
	}
	t.Logf("create department success: id=%d name=%s", dept.ID, dept.Name)
}

func TestDepartment_Get(t *testing.T) {
	ctx := loginContext(t)
	svc := setupIAMDepartment(t)

	getDeptName := "test-dept-get-" + randSuffix()
	created, err := svc.CreateDepartment(ctx, &account.CreateDepartmentInput{
		Name:     getDeptName,
		ParentID: 0,
		Sort:     1000,
	})
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}

	found, err := svc.GetDepartment(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDepartment failed: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("ID = %d, want %d", found.ID, created.ID)
	}
	if found.Name != getDeptName {
		t.Fatalf("Name = %q, want %q", found.Name, getDeptName)
	}
}

func TestDepartment_Update(t *testing.T) {
	ctx := loginContext(t)
	svc := setupIAMDepartment(t)

	updDeptName := "test-dept-update-" + randSuffix()
	created, err := svc.CreateDepartment(ctx, &account.CreateDepartmentInput{
		Name:     updDeptName,
		ParentID: 0,
		Sort:     1000,
	})
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}

	newName := updDeptName + "-changed"
	updated, err := svc.UpdateDepartment(ctx, created.ID, &account.UpdateDepartmentInput{
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("UpdateDepartment failed: %v", err)
	}
	if updated.Name != newName {
		t.Fatalf("Name = %q, want %q", updated.Name, newName)
	}
}

func TestDepartment_List(t *testing.T) {
	ctx := loginContext(t)
	svc := setupIAMDepartment(t)

	depts, err := svc.ListDepartment(ctx, &account.ListDepartmentInput{
		Pagination: types.Pagination{Offset: 0, Limit: 20},
	})
	if err != nil {
		t.Fatalf("ListDepartment failed: %v", err)
	}
	if len(depts.Items) == 0 {
		t.Fatal("department list is empty")
	}
	t.Logf("list departments: total=%d items=%d", depts.Total, len(depts.Items))
}

func TestDepartment_Delete(t *testing.T) {
	ctx := loginContext(t)
	svc := setupIAMDepartment(t)

	delDeptName := "test-dept-delete-" + randSuffix()
	created, err := svc.CreateDepartment(ctx, &account.CreateDepartmentInput{
		Name:     delDeptName,
		ParentID: 0,
		Sort:     1000,
	})
	if err != nil {
		t.Fatalf("CreateDepartment: %v", err)
	}

	if err := svc.DeleteDepartment(ctx, created.ID); err != nil {
		t.Fatalf("DeleteDepartment failed: %v", err)
	}

	_, err = svc.GetDepartment(ctx, created.ID)
	if err == nil {
		t.Fatal("expected error getting deleted department")
	}
	t.Logf("delete department success: id=%d", created.ID)
}

// ── TokenParser ──────────────────────────────────────────────────────────────

func TestIssueWorker_And_ParseWorker(t *testing.T) {
	db := setupTestDB(t)
	workerSecret := "test-worker-jwt-secret"
	workerCfg := &config.WorkerAuthConfig{
		BootstrapTokens: []config.WorkerBootstrapToken{
			{OrgID: 1, WorkerID: 1, Token: "test-bootstrap-token"},
		},
		TokenTTLSeconds: 3600,
	}

	parser := NewTokenParser(db, nil, iamTestEnv(t), workerSecret, workerCfg)
	token, expiredAt, err := parser.IssueWorker(context.Background(), 1, 1, "test-bootstrap-token")
	if err != nil {
		t.Fatalf("IssueWorker failed: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}
	if expiredAt <= 0 {
		t.Fatal("expiredAt is 0")
	}

	caller, err := parser.ParseWorker(context.Background(), token)
	if err != nil {
		t.Fatalf("ParseWorker failed: %v", err)
	}
	if caller.OrgID != 1 || caller.WorkerID != 1 {
		t.Fatalf("caller org=%d worker=%d, want 1/1", caller.OrgID, caller.WorkerID)
	}
	if caller.Kind != types.CallerKindWorker {
		t.Fatalf("caller.Kind = %q, want worker", caller.Kind)
	}
	if caller.State != types.AuthStateSucc {
		t.Fatalf("caller.State = %d, want AuthStateSucc(1)", caller.State)
	}
}

func TestIssueWorker_EmptySecret(t *testing.T) {
	db := setupTestDB(t)
	workerCfg := &config.WorkerAuthConfig{
		BootstrapTokens: []config.WorkerBootstrapToken{
			{OrgID: 1, WorkerID: 1, Token: "test-token"},
		},
	}

	parser := NewTokenParser(db, nil, iamTestEnv(t), "", workerCfg)
	_, _, err := parser.IssueWorker(context.Background(), 1, 1, "test-token")
	if !errors.Is(err, accounterror.ErrJWTSecretRequired) {
		t.Fatalf("expected ErrJWTSecretRequired, got %v", err)
	}
}

func TestParseUser_InvalidToken(t *testing.T) {
	db := setupTestDB(t)
	cfg := iamCfgFromEnvOrConfig(t)
	parser := NewTokenParser(db, cfg, iamTestEnv(t), "worker-secret", nil)

	_, err := parser.ParseUser(context.Background(), "invalid-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	t.Logf("parse user invalid token: %v", err)
}

// ── Mapper ───────────────────────────────────────────────────────────────────

func TestMapLoginThirdToAuthTokenResponse_Complete(t *testing.T) {
	resp := &iamLoginThirdResponseBody{
		UserID:       100,
		LoginStatus:  "success",
		JwtToken:     "jwt-xxx",
		RefreshToken: "refresh-yyy",
		UserInfo: &iamUserInfo{
			ID:        100,
			Name:      "Test User",
			Email:     "test@yygu.cn",
			Phone:     "13800000000",
			AvatarURL: "https://example.com/avatar.png",
		},
		Uin: []iamLoginUin{
			{
				Uin:         iamUinInfo{ID: 200, SubjectID: 300},
				CompanyName: "Test Company",
				CompanyLogo: "https://example.com/logo.png",
			},
		},
	}
	result, err := mapLoginThirdToAuthTokenResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.LoginStatus != "success" {
		t.Fatalf("LoginStatus = %q", result.LoginStatus)
	}
	if result.JwtToken != "jwt-xxx" {
		t.Fatalf("JwtToken = %q", result.JwtToken)
	}
	if result.RefreshToken != "refresh-yyy" {
		t.Fatalf("RefreshToken = %q", result.RefreshToken)
	}
	if result.Uin != 200 {
		t.Fatalf("Uin = %d", result.Uin)
	}
	if result.UserID != 100 {
		t.Fatalf("UserID = %d", result.UserID)
	}
	if result.UserInfo.ID != 100 {
		t.Fatalf("UserInfo.ID = %d", result.UserInfo.ID)
	}
	if len(result.Organizations) != 1 {
		t.Fatalf("len(Organizations) = %d", len(result.Organizations))
	}
	if result.Org.ID != 300 {
		t.Fatalf("Org.ID = %d", result.Org.ID)
	}
}

func TestMapLoginThirdToAuthTokenResponse_NoUin(t *testing.T) {
	resp := &iamLoginThirdResponseBody{
		UserID:       100,
		LoginStatus:  "success",
		JwtToken:     "jwt-xxx",
		RefreshToken: "refresh-yyy",
		UserInfo: &iamUserInfo{
			ID:    100,
			Name:  "Test",
			Email: "t@t.com",
		},
	}
	result, err := mapLoginThirdToAuthTokenResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Uin != 0 {
		t.Fatalf("Uin = %d, want 0", result.Uin)
	}
	if len(result.Organizations) != 0 {
		t.Fatalf("Organizations = %d, want 0", len(result.Organizations))
	}
	if result.Org.ID != 0 {
		t.Fatalf("Org.ID = %d, want 0", result.Org.ID)
	}
}

func TestMapLoginThirdToAuthTokenResponse_NoUserInfo(t *testing.T) {
	resp := &iamLoginThirdResponseBody{
		UserID:       100,
		LoginStatus:  "success",
		JwtToken:     "jwt-xxx",
		RefreshToken: "refresh-yyy",
		Uin: []iamLoginUin{
			{Uin: iamUinInfo{ID: 200, SubjectID: 300}, CompanyName: "C"},
		},
	}
	result, err := mapLoginThirdToAuthTokenResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.UserInfo.ID != 0 {
		t.Fatalf("UserInfo.ID = %d, want 0", result.UserInfo.ID)
	}
	if result.UserInfo.Name != "" {
		t.Fatalf("UserInfo.Name = %q, want empty", result.UserInfo.Name)
	}
}

func TestMapIAMUserInfoToAuthUserInfo(t *testing.T) {
	iamInfo := &iamUserInfo{
		ID:        42,
		Name:      "Alice",
		Email:     "alice@yygu.cn",
		Phone:     "13811112222",
		AvatarURL: "https://img.example.com/a.png",
	}
	result := mapIAMUserInfoToAuthUserInfo(iamInfo)
	if result.ID != 42 {
		t.Errorf("ID = %d", result.ID)
	}
	if result.Name != "Alice" {
		t.Errorf("Name = %q", result.Name)
	}
	if result.Email != "alice@yygu.cn" {
		t.Errorf("Email = %q", result.Email)
	}
	if result.AvatarURL != "https://img.example.com/a.png" {
		t.Errorf("AvatarURL = %q", result.AvatarURL)
	}
}

func TestMapIAMUserInfoToAuthUserInfo_Nil(t *testing.T) {
	result := mapIAMUserInfoToAuthUserInfo(nil)
	if result.ID != 0 || result.Name != "" {
		t.Fatalf("expected zero value for nil input, got %+v", result)
	}
}

func TestMapUinListToAuthOrgInfos(t *testing.T) {
	uins := []iamLoginUin{
		{
			Uin:         iamUinInfo{SubjectID: 10},
			CompanyName: "Org-A",
			CompanyLogo: "logo-a.png",
		},
		{
			Uin:         iamUinInfo{SubjectID: 20},
			CompanyName: "Org-B",
			CompanyLogo: "",
		},
	}
	orgs := mapUinListToAuthOrgInfos(uins)
	if len(orgs) != 2 {
		t.Fatalf("len = %d", len(orgs))
	}
	if orgs[0].ID != 10 || orgs[0].Name != "Org-A" || orgs[0].Logo != "logo-a.png" {
		t.Fatalf("orgs[0] = %+v", orgs[0])
	}
	if orgs[1].ID != 20 || orgs[1].Name != "Org-B" || orgs[1].Logo != "" {
		t.Fatalf("orgs[1] = %+v", orgs[1])
	}
}

func TestMapDetailPersonalCenterToUserInfo(t *testing.T) {
	resp := &iamDetailPersonalCenterResponseBody{
		UserInfo: iamUserInfo{
			ID:        55,
			Name:      "Bob",
			Email:     "bob@yygu.cn",
			Phone:     "13900001111",
			AvatarURL: "https://img.example.com/b.png",
		},
	}
	result := mapDetailPersonalCenterToUserInfo(resp)
	if result.ID != 55 {
		t.Errorf("ID = %d", result.ID)
	}
	if result.PublicID != "55" {
		t.Errorf("PublicID = %q, want %q", result.PublicID, "55")
	}
	if result.Name != "Bob" {
		t.Errorf("Name = %q", result.Name)
	}
	if result.Email != "bob@yygu.cn" {
		t.Errorf("Email = %q", result.Email)
	}
}

func TestMapDepartmentTreeEmployeeToUserInfo(t *testing.T) {
	emp := iamDepartmentTreeEmployee{
		Uin:        100,
		EmployeeID: 10,
		UserID:     55,
		Name:       "Test Emp",
		Email:      "emp@yygu.cn",
		Phone:      "13800002222",
	}
	result := mapDepartmentTreeEmployeeToUserInfo(emp)
	if result.ID != 55 {
		t.Errorf("ID = %d, want 55", result.ID)
	}
	if result.PublicID != "55" {
		t.Errorf("PublicID = %q, want %q", result.PublicID, "55")
	}
	if result.Uin != 100 {
		t.Errorf("Uin = %d", result.Uin)
	}
	if result.Name != "Test Emp" {
		t.Errorf("Name = %q", result.Name)
	}
	if result.Email != "emp@yygu.cn" {
		t.Errorf("Email = %q", result.Email)
	}
}

func TestMapIAMCompanyToOrg(t *testing.T) {
	company := &iamCompanyInfo{
		ID:            5,
		Name:          "Acme Corp",
		Description:   "A test company",
		Logo:          "logo.png",
		Address:       "123 Main St",
		Website:       "https://acme.com",
		CompanyStatus: "passed",
	}
	result := mapIAMCompanyToOrg(company)
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.PublicID != "5" {
		t.Errorf("PublicID = %q, want %q", result.PublicID, "5")
	}
	if result.Name != "Acme Corp" {
		t.Errorf("Name = %q", result.Name)
	}
	if result.Status != "active" {
		t.Errorf("Status = %q, want active", result.Status)
	}
	if result.Description != "A test company" {
		t.Errorf("Description = %q", result.Description)
	}
}

func TestMapIAMCompanyToOrg_Nil(t *testing.T) {
	result := mapIAMCompanyToOrg(nil)
	if result != nil {
		t.Fatalf("expected nil, got %+v", result)
	}
}

func TestMapCompanyStatus(t *testing.T) {
	if s := mapCompanyStatus("passed"); s != "active" {
		t.Errorf("passed -> %q, want active", s)
	}
	if s := mapCompanyStatus("pending"); s != "pending" {
		t.Errorf("pending -> %q, want pending", s)
	}
	if s := mapCompanyStatus(""); s != "" {
		t.Errorf("empty -> %q, want empty", s)
	}
}

func TestMapIAMDepartmentToContract(t *testing.T) {
	dept := iamDepartmentPayload{
		ID:        100,
		Name:      "Engineering",
		ParentID:  0,
		Sort:      1000,
		CompanyID: 5,
	}
	result := mapIAMDepartmentToContract(dept)
	if result.ID != 100 || result.Name != "Engineering" || result.OrgID != 5 {
		t.Fatalf("department = %+v", result)
	}
}

func TestMapIAMCompanyPayloadToOrg(t *testing.T) {
	c := iamCompanyPayload{
		ID:            3,
		Name:          "MyOrg",
		CompanyStatus: "passed",
	}
	result := mapIAMCompanyPayloadToOrg(c)
	if result.Name != "MyOrg" || result.Status != "active" {
		t.Fatalf("org = %+v", result)
	}
}

func TestMapListDepartmentToContract(t *testing.T) {
	dept := iamDepartmentPayloadWithPaths{
		ID:        42,
		Name:      "QA",
		ParentID:  10,
		ParentIDs: []uint{10},
		Sort:      2000,
		CompanyID: 7,
	}
	result := mapListDepartmentToContract(dept)
	if result.ID != 42 || result.Name != "QA" || result.OrgID != 7 || len(result.ParentIDs) != 1 {
		t.Fatalf("department = %+v", result)
	}
}
