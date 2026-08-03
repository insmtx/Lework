//go:build enterprise

package enterprise

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter/account"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/pkg/accounterror"
	"github.com/ygpkg/yg-go/logs"
)

type auth struct {
	db           *gorm.DB
	iamCfg       *config.IAMConfig
	client       *iamClient
	provisioning account.WorkerProvisioner
}

func NewAuth(db *gorm.DB, iamCfg *config.IAMConfig, env string, provisioning account.WorkerProvisioner) *auth {
	return &auth{
		db:           db,
		iamCfg:       iamCfg,
		client:       newIAMClient(iamCfg, env),
		provisioning: provisioning,
	}
}

func (s *auth) RegisterByEmail(ctx context.Context, req *account.RegisterByEmailInput) (*account.AuthTokens, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		return nil, accounterror.ErrEmailRequired
	}
	if req.Password != req.ConfirmPassword {
		return nil, accounterror.ErrPasswordsDoNotMatch
	}

	var resp iamLoginThirdResponseBody
	if err := s.client.call(ctx, "account.RegisterByEmail", &iamRegisterByEmailReq{
		Email:    email,
		Password: req.Password,
		Name:     req.Name,
		Issuer:   domainName(s.iamCfg),
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}
	return mapLoginThirdToAuthTokenResponse(&resp)
}

func (s *auth) LoginByPassword(ctx context.Context, req *account.LoginByPasswordInput) (*account.LoginByPasswordOutput, error) {
	accountStr := strings.TrimSpace(req.Account)
	if accountStr == "" {
		return nil, accounterror.ErrAccountRequired
	}
	if strings.TrimSpace(req.Password) == "" {
		return nil, accounterror.ErrPasswordRequired
	}

	var resp iamLoginThirdResponseBody
	if err := s.client.call(ctx, "account.LoginByPassword", &iamLoginByPasswordReq{
		Account:    accountStr,
		Password:   req.Password,
		DomainName: domainName(s.iamCfg),
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}
	if resp.LoginStatus != "success" {
		return nil, accounterror.ErrInvalidAccountOrPassword
	}
	return mapLoginPasswordToOutput(&resp), nil
}

func (s *auth) SendPhoneLoginCode(ctx context.Context, req *account.SendPhoneLoginCodeInput) (*account.SendPhoneLoginCodeOutput, error) {
	phone := strings.TrimSpace(req.Phone)
	if phone == "" {
		return nil, accounterror.ErrPhoneRequired
	}

	var resp iamSendSmsCodeResponseBody
	if err := s.client.call(ctx, "account.SendSmsCode", &iamSendSmsCodeReq{
		Phone: phone,
		Scene: "login_by_phone",
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}
	return &account.SendPhoneLoginCodeOutput{
		Phone:       resp.Phone,
		ExpiresIn:   resp.ExpiresIn,
		ResendAfter: resp.ResendAfter,
	}, nil
}

func (s *auth) LoginByPhoneCode(ctx context.Context, req *account.LoginByPhoneCodeInput) (*account.AuthTokens, error) {
	phone := strings.TrimSpace(req.Phone)
	if phone == "" {
		return nil, accounterror.ErrPhoneRequired
	}
	if strings.TrimSpace(req.Code) == "" {
		return nil, accounterror.ErrPhoneCodeRequired
	}

	var resp iamLoginThirdResponseBody
	if err := s.client.call(ctx, "account.LoginByPhoneCode", &iamLoginByPhoneCodeReq{
		Phone:      phone,
		Code:       strings.TrimSpace(req.Code),
		DomainName: domainName(s.iamCfg),
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}

	result, err := mapLoginThirdToAuthTokenResponse(&resp)
	if err != nil {
		return nil, err
	}
	result.Edition = account.EditionEnterprise
	return result, nil
}

func (s *auth) RefreshToken(ctx context.Context, req *account.RefreshTokenInput) (*account.AuthTokens, error) {
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		return nil, accounterror.ErrRefreshTokenRequired
	}

	loginWay := req.LoginWay
	if loginWay == 0 {
		loginWay = 8
	}

	var resp iamChooseUinResponseBody
	if err := s.client.call(ctx, "account.ChooseUin", &iamChooseUinReq{
		RefreshToken: refreshToken,
		UinID:        req.UinID,
		UserID:       req.UserID,
		LoginWay:     loginWay,
		Issuer:       domainName(s.iamCfg),
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}
	return &account.AuthTokens{
		JwtToken:      resp.JWTToken,
		Uin:           resp.UIN,
		Organizations: make([]account.AuthOrgInfo, 0),
	}, nil
}

func (s *auth) SwitchOrganization(ctx context.Context, req *account.SwitchOrganizationInput) (*account.AuthTokens, error) {
	if req.Uin == 0 {
		return nil, accounterror.ErrOrgNotFound
	}

	loginWay := req.LoginWay
	if loginWay == 0 {
		loginWay = 8
	}

	var resp iamSwitchLoginResponseBody
	if err := s.client.callWithAuth(ctx, "account.SwitchLogin", &iamSwitchLoginReq{
		Uin:      req.Uin,
		LoginWay: loginWay,
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}

	if s.provisioning != nil {
		session, err := s.fetchAuthSessionWithToken(ctx, resp.JWTToken)
		if err != nil {
			logs.WarnContextf(ctx, "SwitchOrganization: fetch auth session failed: %v", err)
		} else if session.Org.ID != 0 {
			if _, err := s.provisioning.EnsureDefaultWorkerForOrg(ctx, session.Org.ID, req.Uin); err != nil {
				logs.WarnContextf(ctx, "SwitchOrganization: ensure default worker for org %d: %v", session.Org.ID, err)
			}
		}
	}

	return &account.AuthTokens{
		JwtToken: resp.JWTToken,
	}, nil
}

func (s *auth) ChooseUin(ctx context.Context, req *account.ChooseUinInput) (*account.AuthTokens, error) {
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		return nil, accounterror.ErrRefreshTokenRequired
	}

	loginWay := req.LoginWay
	if loginWay == 0 {
		loginWay = 8
	}

	var resp iamChooseUinResponseBody
	if err := s.client.call(ctx, "account.ChooseUin", &iamChooseUinReq{
		RefreshToken: refreshToken,
		UinID:        req.Uin,
		UserID:       req.UserID,
		LoginWay:     loginWay,
		Issuer:       domainName(s.iamCfg),
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}

	session, err := s.fetchAuthSessionWithToken(ctx, resp.JWTToken)
	if err != nil {
		logs.WarnContextf(ctx, "ChooseUin: fetch AuthSession failed, err=%v", err)
		return nil, err
	}

	org := account.AuthOrgInfo{}
	if len(session.Organizations) > 0 {
		org = session.Organizations[0]
	}

	if s.provisioning != nil {
		for _, o := range session.Organizations {
			if o.Uin == resp.UIN && o.ID != 0 {
				if _, err := s.provisioning.EnsureDefaultWorkerForOrg(ctx, o.ID, resp.UIN); err != nil {
					logs.WarnContextf(ctx, "ChooseUin: ensure default worker for org %d: %v", o.ID, err)
				}
				break
			}
		}
	}

	return &account.AuthTokens{
		JwtToken:      resp.JWTToken,
		Uin:           resp.UIN,
		UserInfo:      session.UserInfo,
		Org:           org,
		Organizations: session.Organizations,
	}, nil
}

func (s *auth) fetchAuthSessionWithToken(ctx context.Context, jwtToken string) (*account.AuthSessionOutput, error) {
	authCtx := localauth.WithBearerToken(ctx, jwtToken)
	return s.AuthSession(authCtx)
}

func (s *auth) CreateOrganization(ctx context.Context, req *account.CreateOrganizationInput) (*account.AuthTokens, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("organization name is required")
	}

	token := extractAuthToken(ctx)
	if token == "" && req.RefreshToken == "" {
		return nil, accounterror.ErrLoginRequired
	}

	var resp iamCreateCompanyResponseBody
	var err error

	iamReq := &iamCreateCompanyReq{
		DomainName:      domainName(s.iamCfg),
		CompanyName:     name,
		UserDisplayName: req.UserDisplayName,
	}

	if token != "" {
		err = s.client.callWithAuth(ctx, "account.CreateCompany", iamReq, &resp)
	} else {
		iamReq.RefreshToken = req.RefreshToken
		iamReq.UserID = req.UserID
		err = s.client.call(ctx, "account.CreateCompany", iamReq, &resp)
	}
	if err != nil {
		return nil, mapIAMError(err)
	}

	if s.db != nil && resp.Uin.SubjectID != 0 {
		if err := db.CloneSystemLLMModelsByOrg(ctx, s.db, 1, resp.Uin.SubjectID); err != nil {
			logs.WarnContextf(ctx, "CreateOrganization: clone system llm models for org %d: %v", resp.Uin.SubjectID, err)
		}
	}

	return &account.AuthTokens{
		Uin: resp.Uin.ID,
		Org: account.AuthOrgInfo{
			ID:   resp.Uin.SubjectID,
			Name: resp.CompanyName,
			Logo: resp.CompanyLogo,
		},
	}, nil
}

func (s *auth) AuthSession(ctx context.Context) (*account.AuthSessionOutput, error) {
	var resp iamAuthSessionResp
	if err := s.client.callWithAuth(ctx, "account.AuthSession", nil, &resp); err != nil {
		return nil, mapIAMError(err)
	}

	var orgUinName string
	userInfo := mapIAMUserInfoToAuthUserInfo(&resp.UserInfo)
	if account.IsFilePublicID(userInfo.AvatarURL) {
		if resolved := resolveEnterpriseAvatar(ctx, s.db, userInfo.AvatarURL); resolved != "" {
			userInfo.AvatarURL = resolved
		}
	}
	for _, uin := range resp.UinList {
		if uin.Uin.ID == resp.EmployeeDetail.Uin {
			userInfo.UinName = uin.Uin.Name
			orgUinName = uin.Uin.Name
			break
		}
	}

	var orgInfo account.AuthOrgInfo
	if resp.CompanyInfo.ID != 0 {
		orgInfo = account.AuthOrgInfo{
			ID:              resp.CompanyInfo.ID,
			PublicID:        strconv.FormatUint(uint64(resp.CompanyInfo.ID), 10),
			Name:            resp.CompanyInfo.Name,
			Code:            resp.CompanyInfo.Alias,
			Logo:            resp.CompanyInfo.Logo,
			IsDefault:       true,
			CreatedByUin:    resp.CompanyInfo.CreatedByUin,
			CreatedByUserID: resp.CompanyInfo.UserID,
			UserName:        orgUinName,
		}
	}

	return &account.AuthSessionOutput{
		UserInfo:      userInfo,
		Org:           orgInfo,
		Organizations: mapUinListToAuthOrgInfos(resp.UinList),
	}, nil
}

func domainName(cfg *config.IAMConfig) string {
	if cfg != nil {
		return strings.TrimSpace(cfg.DomainName)
	}
	return ""
}

func mapIAMError(err error) error {
	var iamErr *iamError
	if errors.As(err, &iamErr) {
		msg := iamErr.Message
		switch {
		case msg == "account_invalid_user_or_password" || msg == "account_invalid_password_or_username" ||
			msg == "用户或密码错误" || msg == "用户不存在或密码错误":
			return accounterror.ErrInvalidEmailOrPassword
		case msg == "account_invalid_code_or_way" || msg == "account_sms_send_failed":
			return accounterror.ErrInvalidPhoneCode
		case msg == "account_login_attempts_exceeded":
			return accounterror.ErrLoginAttemptsExceeded
		case msg == "account_refresh_token_mismatch" || msg == "account_no_permission_update_resource":
			return accounterror.ErrPermissionDenied
		case msg == "account_user_not_exist" || msg == "account_user_not_found" || msg == "account_invalid_token":
			return accounterror.ErrUserNotFound
		case msg == "account_select_login_identity" || msg == "account_company_quota_exceeded":
			return accounterror.ErrOrgNotFound
		case msg == "account_name_already_exists" || msg == "account_email_already_exists" ||
			msg == "account_phone_already_exists" || msg == "account_user_info_already_exists":
			return accounterror.ErrEmailAlreadyExists
		case msg == "account_phone_empty" || msg == "手机号不能为空":
			return accounterror.ErrPhoneRequired
		case msg == "account_phone_or_email_required":
			return accounterror.ErrPhoneOrEmailRequired
		case msg == "account_company_name_empty" || msg == "公司名称不能为空":
			return accounterror.ErrInvalidArg
		default:
			return fmt.Errorf("iam: %s (code=%d)", iamErr.Message, iamErr.Code)
		}
	}
	return err
}

func resolveEnterpriseAvatar(ctx context.Context, gdb *gorm.DB, avatar string) string {
	if !account.IsFilePublicID(avatar) {
		return ""
	}
	fileUpload, err := db.GetFileUploadByPublicID(ctx, gdb, 0, avatar)
	if err != nil || fileUpload == nil {
		return ""
	}
	url, err := filestore.ResolvePublicURL(ctx, fileUpload.StorageURI)
	if err != nil {
		return ""
	}
	return url
}
