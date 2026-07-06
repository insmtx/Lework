package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/config"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

const (
	accessTokenExpire       = 24 * time.Hour
	refreshTokenExpire      = 7 * 24 * time.Hour
	loginAttemptWindow      = 5 * time.Minute
	loginAttemptMaxFailures = 5
	phoneCodeExpire         = 5 * time.Minute
	phoneCodeResendInterval = 2 * time.Minute
	defaultPhoneCode        = "123456"
	maxUserOrganizations    = 2
)

var (
	errAuthDatabaseRequired               = errors.New("数据库不可用")
	errAuthEmailRequired                  = errors.New("请输入邮箱")
	errAuthInvalidEmailFormat             = errors.New("请输入正确的邮箱")
	errAuthPasswordRequired               = errors.New("请输入密码")
	errAuthPasswordsDoNotMatch            = errors.New("密码不一致")
	errAuthPasswordTooShort               = errors.New("密码长度不能少于8位")
	errAuthPasswordTooLong                = errors.New("密码长度不能超过20位")
	errAuthPasswordContainsChinese        = errors.New("密码不能包含中文")
	errAuthPasswordContainsWhitespace     = errors.New("密码不能包含空格")
	errAuthPasswordMustContainLetterDigit = errors.New("8-20位，数字/大写字母/小写字母/字符至少3种")
	errAuthEmailAlreadyExists             = errors.New("该邮箱已注册")
	errAuthInvalidEmailOrPassword         = errors.New("邮箱或密码错误")
	errAuthLoginAttemptsExceeded          = errors.New("登录失败次数过多，请稍后再试")
	errAuthPhoneRequired                  = errors.New("请输入手机号")
	errAuthInvalidPhoneFormat             = errors.New("请输入正确的手机号")
	errAuthPhoneCodeRequired              = errors.New("请输入验证码")
	errAuthInvalidPhoneCode               = errors.New("验证码错误或已过期")
	errAuthPhoneCodeSendTooOften          = errors.New("验证码发送太频繁，请稍后再试")
	errAuthRefreshTokenRequired           = errors.New("刷新令牌不能为空")
	errAuthRefreshTokenInvalid            = errors.New("登录已过期，请重新登录")
	errAuthUserNotFound                   = errors.New("用户不存在")
	errAuthUserOrgNotFound                = errors.New("用户组织信息不存在")
	errAuthUserOrgNotAllowed              = errors.New("用户未加入该组织")
	errAuthLoginRequired                  = errors.New("请先登录")
	errAuthOrgNotFound                    = errors.New("用户组织信息不存在")
	errAuthJWTSecretRequired              = errors.New("登录配置缺失")
	errAuthOrganizationLimitExceeded      = errors.New("最多只能加入两个组织")
)

var _ contract.AuthService = (*authService)(nil)

type authService struct {
	db                 *gorm.DB
	jwtSecret          string
	smsSender          smsSender
	defaultPhoneCode   string
	workerProvisioning *WorkerProvisioningService
}

func NewAuthService(d *gorm.DB, jwtSecret string, aliyunCfg *config.AliyunConfig) contract.AuthService {
	return NewAuthServiceWithProvisioning(d, jwtSecret, aliyunCfg, nil)
}

// NewAuthServiceWithProvisioning creates an auth service that can provision organization defaults.
func NewAuthServiceWithProvisioning(d *gorm.DB, jwtSecret string, aliyunCfg *config.AliyunConfig, provisioning *WorkerProvisioningService) contract.AuthService {
	code := defaultPhoneCode
	if aliyunCfg != nil && strings.TrimSpace(aliyunCfg.DefaultCode) != "" {
		code = strings.TrimSpace(aliyunCfg.DefaultCode)
	}
	return &authService{
		db:                 d,
		jwtSecret:          strings.TrimSpace(jwtSecret),
		smsSender:          newSMSSender(aliyunCfg),
		defaultPhoneCode:   code,
		workerProvisioning: provisioning,
	}
}

func (s *authService) RegisterByEmail(ctx context.Context, req *contract.RegisterByEmailRequest) (*contract.AuthTokenResponse, error) {
	if s.db == nil {
		return nil, errAuthDatabaseRequired
	}

	email, err := normalizeEmail(req.Email)
	if err != nil {
		return nil, err
	}
	if err := validateRegisterPassword(req.Password, req.ConfirmPassword); err != nil {
		return nil, err
	}

	existing, err := db.GetUserByEmail(ctx, s.db, email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errAuthEmailAlreadyExists
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	var user *types.User
	var userOrg *types.UserOrg
	var org *types.Organization
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user = &types.User{
			PublicID:    fmt.Sprintf("usr_%s", snowflake.GenerateIDBase58()),
			GithubLogin: fmt.Sprintf("email_%s", snowflake.GenerateIDBase58()),
			Password:    string(hashedPassword),
			Name:        name,
			Email:       email,
		}
		if err := db.CreateUser(ctx, tx, user); err != nil {
			if isUniqueConstraintError(err) {
				return errAuthEmailAlreadyExists
			}
			return err
		}

		var err error
		org, err = defaultAccountOrg(ctx, tx)
		if err != nil {
			return err
		}

		userOrg = &types.UserOrg{
			Uin:       user.ID,
			UserID:    user.ID,
			OrgID:     org.ID,
			IsDefault: true,
		}
		if err := db.CreateUserOrg(ctx, tx, userOrg); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return s.buildTokenResponse(ctx, user, userOrg, org)
}

func (s *authService) LoginByEmail(ctx context.Context, req *contract.LoginByEmailRequest) (*contract.AuthTokenResponse, error) {
	if s.db == nil {
		return nil, errAuthDatabaseRequired
	}

	email, err := normalizeEmail(req.Email)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Password) == "" {
		return nil, errAuthPasswordRequired
	}

	if err := s.ensureLoginAllowed(ctx, email); err != nil {
		return nil, err
	}

	user, err := db.GetUserByEmail(ctx, s.db, email)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Password == "" {
		s.recordLoginFailure(ctx, email)
		return nil, errAuthInvalidEmailOrPassword
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		s.recordLoginFailure(ctx, email)
		logs.WarnContextf(ctx, "LoginByEmail: password not match for email=%s: %v", email, err)
		return nil, errAuthInvalidEmailOrPassword
	}

	s.clearLoginFailures(ctx, email)
	userOrg, org, err := s.resolveLoginUserOrg(ctx, s.db, user.ID, req.OrgID)
	if err != nil {
		return nil, err
	}

	return s.buildTokenResponse(ctx, user, userOrg, org)
}

func (s *authService) SendPhoneLoginCode(ctx context.Context, req *contract.SendPhoneLoginCodeRequest) (*contract.SendPhoneLoginCodeResponse, error) {
	if s.db == nil {
		return nil, errAuthDatabaseRequired
	}

	phone, err := normalizePhone(req.Phone)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	s.cleanupExpiredAuthData(ctx, now)

	latestCode, err := db.GetActiveAuthPhoneVerificationCode(ctx, s.db, phone, now)
	if err != nil {
		return nil, err
	}
	if latestCode != nil && latestCode.CreatedAt.Add(phoneCodeResendInterval).After(now) {
		logs.WarnContextf(ctx, "SendPhoneLoginCode rejected by resend limit: phone=%s resend_after_seconds=%d",
			maskPhone(phone), int64(phoneCodeResendInterval.Seconds()))
		return nil, errAuthPhoneCodeSendTooOften
	}

	code, err := s.nextPhoneCode()
	if err != nil {
		return nil, err
	}
	logs.InfoContextf(ctx, "SendPhoneLoginCode started: phone=%s sms_enabled=%t expires_in_seconds=%d resend_after_seconds=%d",
		maskPhone(phone), s.smsSender.Enabled(), int64(phoneCodeExpire.Seconds()), int64(phoneCodeResendInterval.Seconds()))
	if err := s.smsSender.SendVerificationCode(ctx, phone, code); err != nil {
		logs.ErrorContextf(ctx, "SendPhoneLoginCode send failed: phone=%s sms_enabled=%t error=%v",
			maskPhone(phone), s.smsSender.Enabled(), err)
		return nil, err
	}

	if err := db.CreateAuthPhoneVerificationCode(ctx, s.db, &types.AuthPhoneVerificationCode{
		Phone:     phone,
		CodeHash:  hashPhoneCode(phone, code),
		ExpiresAt: now.Add(phoneCodeExpire),
	}); err != nil {
		logs.ErrorContextf(ctx, "SendPhoneLoginCode store failed: phone=%s error=%v", maskPhone(phone), err)
		return nil, err
	}
	logs.InfoContextf(ctx, "SendPhoneLoginCode completed: phone=%s sms_enabled=%t",
		maskPhone(phone), s.smsSender.Enabled())

	return &contract.SendPhoneLoginCodeResponse{
		Phone:       phone,
		ExpiresIn:   int64(phoneCodeExpire.Seconds()),
		ResendAfter: int64(phoneCodeResendInterval.Seconds()),
	}, nil
}

func (s *authService) LoginByPhoneCode(ctx context.Context, req *contract.LoginByPhoneCodeRequest) (*contract.AuthTokenResponse, error) {
	if s.db == nil {
		return nil, errAuthDatabaseRequired
	}

	phone, err := normalizePhone(req.Phone)
	if err != nil {
		return nil, err
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		return nil, errAuthPhoneCodeRequired
	}
	if err := s.ensureLoginAllowed(ctx, phone); err != nil {
		return nil, err
	}

	now := time.Now()
	s.cleanupExpiredAuthData(ctx, now)
	savedCode, err := db.GetActiveAuthPhoneVerificationCode(ctx, s.db, phone, now)
	if err != nil {
		return nil, err
	}
	if savedCode == nil || savedCode.CodeHash != hashPhoneCode(phone, code) {
		s.recordLoginFailure(ctx, phone)
		return nil, errAuthInvalidPhoneCode
	}

	var user *types.User
	var userOrg *types.UserOrg
	var org *types.Organization
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := db.MarkAuthPhoneVerificationCodeUsed(ctx, tx, savedCode.ID, now); err != nil {
			return err
		}

		var err error
		user, err = db.GetUserByPhone(ctx, tx, phone)
		if err != nil {
			return err
		}
		if user == nil {
			user = &types.User{
				PublicID:    fmt.Sprintf("usr_%s", snowflake.GenerateIDBase58()),
				GithubLogin: fmt.Sprintf("phone_%s", snowflake.GenerateIDBase58()),
				Name:        phone,
				Phone:       phone,
			}
			if err := db.CreateUser(ctx, tx, user); err != nil {
				return err
			}

			org, err = defaultAccountOrg(ctx, tx)
			if err != nil {
				return err
			}
			userOrg = &types.UserOrg{
				Uin:       user.ID,
				UserID:    user.ID,
				OrgID:     org.ID,
				IsDefault: true,
			}
			if err := db.CreateUserOrg(ctx, tx, userOrg); err != nil {
				return err
			}
			if req.OrgID > 0 && req.OrgID != org.ID {
				return errAuthUserOrgNotAllowed
			}
			return nil
		}

		userOrg, org, err = s.resolveLoginUserOrg(ctx, tx, user.ID, req.OrgID)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}

	s.clearLoginFailures(ctx, phone)
	return s.buildTokenResponse(ctx, user, userOrg, org)
}

func (s *authService) RefreshToken(ctx context.Context, req *contract.RefreshTokenRequest) (*contract.AuthTokenResponse, error) {
	if s.db == nil {
		return nil, errAuthDatabaseRequired
	}
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		return nil, errAuthRefreshTokenRequired
	}

	now := time.Now()
	tokenHash := hashRefreshToken(refreshToken)
	s.cleanupExpiredAuthData(ctx, now)

	savedToken, err := db.GetActiveAuthRefreshToken(ctx, s.db, tokenHash, now)
	if err != nil {
		return nil, err
	}
	if savedToken == nil {
		return nil, errAuthRefreshTokenInvalid
	}
	if savedToken.Uin == 0 {
		return nil, errAuthRefreshTokenInvalid
	}

	userOrg, err := db.GetUserOrgByUin(ctx, s.db, savedToken.Uin)
	if err != nil {
		return nil, err
	}
	if userOrg == nil {
		return nil, errAuthUserOrgNotFound
	}

	user, err := db.GetUserByID(ctx, s.db, userOrg.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errAuthUserNotFound
	}
	org, err := db.GetOrgByID(ctx, s.db, userOrg.OrgID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, errAuthOrgNotFound
	}

	if err := db.RevokeAuthRefreshToken(ctx, s.db, tokenHash, now); err != nil {
		return nil, err
	}
	return s.buildTokenResponse(ctx, user, userOrg, org)
}

func (s *authService) SwitchOrganization(ctx context.Context, req *contract.SwitchOrganizationRequest) (*contract.AuthTokenResponse, error) {
	if s.db == nil {
		return nil, errAuthDatabaseRequired
	}
	if req.OrgID == 0 {
		return nil, errAuthOrgNotFound
	}

	caller, _ := localauth.FromContext(ctx)
	if caller == nil || caller.State != types.AuthStateSucc || caller.Uin == 0 {
		return nil, errAuthLoginRequired
	}

	currentUserOrg, err := db.GetUserOrgByUin(ctx, s.db, caller.Uin)
	if err != nil {
		return nil, err
	}
	if currentUserOrg == nil {
		return nil, errAuthUserOrgNotFound
	}

	user, err := db.GetUserByID(ctx, s.db, currentUserOrg.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errAuthUserNotFound
	}

	targetUserOrg, targetOrg, err := s.resolveLoginUserOrg(ctx, s.db, user.ID, req.OrgID)
	if err != nil {
		return nil, err
	}
	return s.buildTokenResponse(ctx, user, targetUserOrg, targetOrg)
}

func (s *authService) CreateOrganization(ctx context.Context, req *contract.CreateOrganizationRequest) (*contract.AuthTokenResponse, error) {
	if s.db == nil {
		return nil, errAuthDatabaseRequired
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("组织名称不能为空")
	}

	caller, _ := localauth.FromContext(ctx)
	if caller == nil || caller.State != types.AuthStateSucc || caller.Uin == 0 {
		return nil, errAuthLoginRequired
	}

	currentUserOrg, err := db.GetUserOrgByUin(ctx, s.db, caller.Uin)
	if err != nil {
		return nil, err
	}
	if currentUserOrg == nil {
		return nil, errAuthUserOrgNotFound
	}

	user, err := db.GetUserByID(ctx, s.db, currentUserOrg.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errAuthUserNotFound
	}

	var (
		org     *types.Organization
		userOrg *types.UserOrg
	)
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lockedUser types.User
		if err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&lockedUser, user.ID).Error; err != nil {
			return err
		}

		orgCount, err := db.CountUserOrgsByUserID(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		if orgCount >= maxUserOrganizations {
			return errAuthOrganizationLimitExceeded
		}

		orgCode := fmt.Sprintf("org_%s", snowflake.GenerateIDBase58())
		org = &types.Organization{
			PublicID: fmt.Sprintf("org_%s", snowflake.GenerateIDBase58()),
			Type:     "company",
			Code:     orgCode,
			Name:     name,
			Status:   "active",
		}
		if err := db.CreateOrg(ctx, tx, org); err != nil {
			return err
		}

		userOrg = &types.UserOrg{
			UserID:    user.ID,
			OrgID:     org.ID,
			IsDefault: false,
		}
		if err := db.CreateUserOrg(ctx, tx, userOrg); err != nil {
			return err
		}
		userOrg.Uin = userOrg.ID
		if err := db.UpdateUserOrg(ctx, tx, userOrg); err != nil {
			return err
		}

		org.CreatedByUin = userOrg.Uin
		if err := db.UpdateOrg(ctx, tx, org); err != nil {
			return err
		}

		department := &types.Department{
			Name:     "默认部门",
			ParentID: 0,
			Sort:     db.DepartmentSortGap,
			OrgID:    org.ID,
		}
		if err := db.CreateDepartment(ctx, tx, department); err != nil {
			return err
		}

		return db.CreateMemberDepartment(ctx, tx, &types.MemberDepartment{
			Uin:          userOrg.Uin,
			OrgID:        org.ID,
			DepartmentID: department.ID,
			IsPrimary:    true,
		})
	}); err != nil {
		return nil, err
	}
	if s.workerProvisioning != nil {
		if _, err := s.workerProvisioning.EnsureDefaultWorkerForOrg(ctx, org.ID, userOrg.Uin); err != nil {
			return nil, fmt.Errorf("ensure default worker deployment: %w", err)
		}
	}

	return s.buildTokenResponse(ctx, user, userOrg, org)
}

func (s *authService) AuthSession(ctx context.Context) (*contract.AuthSessionResponse, error) {
	if s.db == nil {
		return nil, errAuthDatabaseRequired
	}

	caller, _ := localauth.FromContext(ctx)
	if caller == nil || caller.State != types.AuthStateSucc || caller.Uin == 0 {
		return nil, errAuthLoginRequired
	}

	userOrg, err := db.GetUserOrgByUin(ctx, s.db, caller.Uin)
	if err != nil {
		return nil, err
	}
	if userOrg == nil {
		return nil, errAuthUserOrgNotFound
	}

	user, err := db.GetUserByID(ctx, s.db, userOrg.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errAuthUserNotFound
	}

	_, org, err := s.userOrgWithOrganization(ctx, s.db, userOrg)
	if err != nil {
		return nil, err
	}
	return s.buildAuthSessionResponse(ctx, user, userOrg, org)
}

func (s *authService) buildTokenResponse(ctx context.Context, user *types.User, userOrg *types.UserOrg, org *types.Organization) (*contract.AuthTokenResponse, error) {
	token, expiredAt, err := s.generateJWT(userOrg)
	if err != nil {
		return nil, err
	}
	refreshToken, err := s.generateRefreshToken(ctx, userOrg.Uin)
	if err != nil {
		return nil, err
	}
	session, err := s.buildAuthSessionResponse(ctx, user, userOrg, org)
	if err != nil {
		return nil, err
	}

	return &contract.AuthTokenResponse{
		LoginStatus:   "success",
		JwtToken:      token,
		RefreshToken:  refreshToken,
		ExpiredAt:     expiredAt,
		Uin:           userOrg.Uin,
		UserInfo:      session.UserInfo,
		Org:           session.Org,
		Organizations: session.Organizations,
	}, nil
}

func (s *authService) buildAuthSessionResponse(ctx context.Context, user *types.User, userOrg *types.UserOrg, org *types.Organization) (*contract.AuthSessionResponse, error) {
	organizations, err := s.userOrganizationInfos(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return &contract.AuthSessionResponse{
		UserInfo: contract.AuthUserInfo{
			ID:          user.ID,
			PublicID:    user.PublicID,
			Name:        user.Name,
			Email:       user.Email,
			Phone:       user.Phone,
			GithubLogin: user.GithubLogin,
			AvatarURL:   user.AvatarURL,
		},
		Org:           authOrgInfo(org, userOrg.IsDefault),
		Organizations: organizations,
	}, nil
}

func (s *authService) generateJWT(userOrg *types.UserOrg) (string, int64, error) {
	if s.jwtSecret == "" {
		return "", 0, errAuthJWTSecretRequired
	}
	return localauth.GenerateUserToken(localauth.UserClaims{
		Uin: userOrg.Uin,
	}, s.jwtSecret, accessTokenExpire)
}

func (s *authService) generateRefreshToken(ctx context.Context, uin uint) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}

	now := time.Now()
	s.cleanupExpiredAuthData(ctx, now)
	if err := db.CreateAuthRefreshToken(ctx, s.db, &types.AuthRefreshToken{
		TokenHash: hashRefreshToken(token),
		Uin:       uin,
		ExpiresAt: now.Add(refreshTokenExpire),
	}); err != nil {
		return "", fmt.Errorf("store refresh token: %w", err)
	}
	return token, nil
}

func (s *authService) resolveLoginUserOrg(ctx context.Context, database *gorm.DB, userID, requestedOrgID uint) (*types.UserOrg, *types.Organization, error) {
	var (
		userOrg *types.UserOrg
		err     error
	)
	if requestedOrgID > 0 {
		userOrg, err = db.GetUserOrgByUserIDAndOrgID(ctx, database, userID, requestedOrgID)
		if err != nil {
			return nil, nil, err
		}
		if userOrg == nil {
			return nil, nil, errAuthUserOrgNotAllowed
		}
		return s.userOrgWithOrganization(ctx, database, userOrg)
	}

	userOrg, err = db.GetUserOrgByUserID(ctx, database, userID)
	if err != nil {
		return nil, nil, err
	}
	if userOrg == nil {
		return nil, nil, errAuthUserOrgNotFound
	}
	return s.userOrgWithOrganization(ctx, database, userOrg)
}

func (s *authService) userOrgWithOrganization(ctx context.Context, database *gorm.DB, userOrg *types.UserOrg) (*types.UserOrg, *types.Organization, error) {
	org, err := db.GetOrgByID(ctx, database, userOrg.OrgID)
	if err != nil {
		return nil, nil, err
	}
	if org == nil {
		return nil, nil, errAuthOrgNotFound
	}
	return userOrg, org, nil
}

func (s *authService) userOrganizationInfos(ctx context.Context, userID uint) ([]contract.AuthOrgInfo, error) {
	userOrgs, err := db.GetUserOrgsByUserID(ctx, s.db, userID)
	if err != nil {
		return nil, err
	}
	if len(userOrgs) == 0 {
		return nil, nil
	}

	orgIDs := make([]uint, 0, len(userOrgs))
	for _, userOrg := range userOrgs {
		orgIDs = append(orgIDs, userOrg.OrgID)
	}
	orgs, err := db.GetOrgsByIDs(ctx, s.db, orgIDs)
	if err != nil {
		return nil, err
	}
	orgByID := make(map[uint]*types.Organization, len(orgs))
	for _, org := range orgs {
		orgByID[org.ID] = org
	}

	infos := make([]contract.AuthOrgInfo, 0, len(userOrgs))
	for _, userOrg := range userOrgs {
		org := orgByID[userOrg.OrgID]
		if org == nil {
			continue
		}
		infos = append(infos, authOrgInfo(org, userOrg.IsDefault))
	}
	return infos, nil
}

func authOrgInfo(org *types.Organization, isDefault bool) contract.AuthOrgInfo {
	return contract.AuthOrgInfo{
		ID:        org.ID,
		PublicID:  org.PublicID,
		Code:      org.Code,
		Name:      org.Name,
		Logo:      org.Logo,
		IsDefault: isDefault,
	}
}

func (s *authService) ensureLoginAllowed(ctx context.Context, email string) error {
	now := time.Now()
	s.cleanupExpiredAuthData(ctx, now)

	attempt, err := db.GetAuthLoginAttempt(ctx, s.db, email)
	if err != nil {
		return err
	}
	if attempt == nil || !attempt.WindowExpiresAt.After(now) {
		return nil
	}
	if attempt.FailureCount >= loginAttemptMaxFailures {
		return errAuthLoginAttemptsExceeded
	}
	return nil
}

func (s *authService) recordLoginFailure(ctx context.Context, email string) {
	now := time.Now()
	attempt, err := db.GetAuthLoginAttempt(ctx, s.db, email)
	if err != nil {
		logs.WarnContextf(ctx, "LoginByEmail: get login attempt failed: %v", err)
		return
	}

	if attempt == nil || !attempt.WindowExpiresAt.After(now) {
		attempt = &types.AuthLoginAttempt{
			Identifier:      email,
			FailureCount:    1,
			WindowExpiresAt: now.Add(loginAttemptWindow),
		}
	} else {
		attempt.FailureCount++
	}

	if err := db.SaveAuthLoginAttempt(ctx, s.db, attempt); err != nil {
		logs.WarnContextf(ctx, "LoginByEmail: save login attempt failed: %v", err)
	}
}

func (s *authService) clearLoginFailures(ctx context.Context, email string) {
	if err := db.DeleteAuthLoginAttempt(ctx, s.db, email); err != nil {
		logs.WarnContextf(ctx, "LoginByEmail: clear login attempt counter failed: %v", err)
	}
}

func (s *authService) cleanupExpiredAuthData(ctx context.Context, now time.Time) {
	if s.db == nil {
		return
	}
	if err := db.DeleteExpiredAuthRefreshTokens(ctx, s.db, now); err != nil {
		logs.WarnContextf(ctx, "cleanup expired auth refresh tokens failed: %v", err)
	}
	if err := db.DeleteExpiredAuthLoginAttempts(ctx, s.db, now); err != nil {
		logs.WarnContextf(ctx, "cleanup expired auth login attempts failed: %v", err)
	}
	if err := db.DeleteExpiredAuthPhoneVerificationCodes(ctx, s.db, now); err != nil {
		logs.WarnContextf(ctx, "cleanup expired auth phone verification codes failed: %v", err)
	}
}

func defaultAccountOrg(ctx context.Context, tx *gorm.DB) (*types.Organization, error) {
	org, err := db.GetOrgByID(ctx, tx, types.SystemOrgID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, errAuthOrgNotFound
	}
	return org, nil
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", errAuthEmailRequired
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || !strings.Contains(email, "@") {
		return "", errAuthInvalidEmailFormat
	}
	return email, nil
}

func normalizePhone(phone string) (string, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", errAuthPhoneRequired
	}
	phone = strings.TrimPrefix(phone, "+86")
	phone = strings.TrimPrefix(phone, "86")
	if !regexp.MustCompile(`^1[3-9]\d{9}$`).MatchString(phone) {
		return "", errAuthInvalidPhoneFormat
	}
	return phone, nil
}

func validateRegisterPassword(password, confirmPassword string) error {
	if password != confirmPassword {
		return errAuthPasswordsDoNotMatch
	}
	return validatePasswordStrength(password)
}

func validatePasswordStrength(password string) error {
	if strings.TrimSpace(password) == "" {
		return errAuthPasswordRequired
	}
	if len(password) < 8 {
		return errAuthPasswordTooShort
	}
	if len(password) > 20 {
		return errAuthPasswordTooLong
	}
	categoryCount := 0
	hasLower := false
	hasUpper := false
	hasDigit := false
	hasSpecial := false
	for _, r := range password {
		if r >= '\u4e00' && r <= '\u9fff' {
			return errAuthPasswordContainsChinese
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return errAuthPasswordContainsWhitespace
		}
		if r >= 'a' && r <= 'z' {
			hasLower = true
			continue
		}
		if r >= 'A' && r <= 'Z' {
			hasUpper = true
			continue
		}
		if r >= '0' && r <= '9' {
			hasDigit = true
			continue
		}
		hasSpecial = true
	}
	for _, matched := range []bool{hasLower, hasUpper, hasDigit, hasSpecial} {
		if matched {
			categoryCount++
		}
	}
	if categoryCount < 3 {
		return errAuthPasswordMustContainLetterDigit
	}
	return nil
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func hashPhoneCode(phone string, code string) string {
	sum := sha256.Sum256([]byte(phone + ":" + code))
	return hex.EncodeToString(sum[:])
}

func maskPhone(phone string) string {
	if len(phone) < 7 {
		return phone
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}

func (s *authService) nextPhoneCode() (string, error) {
	if !s.smsSender.Enabled() {
		return s.defaultPhoneCode, nil
	}
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("generate phone verification code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
