//go:build !enterprise

package oss

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

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/sms"
	"github.com/insmtx/Leros/backend/pkg/accounterror"
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
	maxUserOrganizations    = 1
	defaultWorkerTokenTTL   = 24 * time.Hour
)

type authAdapter struct {
	db                 *gorm.DB
	jwtSecret          string
	smsSender          sms.SmsSender
	defaultPhoneCode   string
	workerProvisioning account.WorkerProvisioner
}

func NewAuth(d *gorm.DB, jwtSecret string, smsSender sms.SmsSender, provisioning account.WorkerProvisioner) *authAdapter {
	code := defaultPhoneCode
	return &authAdapter{
		db:                 d,
		jwtSecret:          strings.TrimSpace(jwtSecret),
		smsSender:          smsSender,
		defaultPhoneCode:   code,
		workerProvisioning: provisioning,
	}
}

func (s *authAdapter) RegisterByEmail(ctx context.Context, req *account.RegisterByEmailInput) (*account.AuthTokens, error) {
	if s.db == nil {
		return nil, accounterror.ErrDatabaseRequired
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
		return nil, accounterror.ErrEmailAlreadyExists
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
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user = &types.User{
			PublicID: fmt.Sprintf("usr_%s", snowflake.GenerateIDBase58()),
			Password: string(hashedPassword),
			Name:     name,
			Email:    email,
		}
		if err := db.CreateUser(ctx, tx, user); err != nil {
			if db.IsUniqueConstraintError(err) {
				return accounterror.ErrEmailAlreadyExists
			}
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}

	refreshToken, err := s.generateRefreshToken(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &account.AuthTokens{
		LoginStatus:  account.LoginStatusNeedCreateCompany,
		RefreshToken: refreshToken,
		UserInfo: account.AuthUserInfo{
			ID:        user.ID,
			PublicID:  user.PublicID,
			Name:      user.Name,
			Email:     user.Email,
			AvatarURL: user.AvatarURL,
		},
	}, nil
}

func (s *authAdapter) LoginByEmail(ctx context.Context, req *account.LoginByEmailInput) (*account.AuthTokens, error) {
	if s.db == nil {
		return nil, accounterror.ErrDatabaseRequired
	}

	email, err := normalizeEmail(req.Email)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Password) == "" {
		return nil, accounterror.ErrPasswordRequired
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
		return nil, accounterror.ErrInvalidEmailOrPassword
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		s.recordLoginFailure(ctx, email)
		logs.WarnContextf(ctx, "LoginByEmail: password not match for email=%s: %v", email, err)
		return nil, accounterror.ErrInvalidEmailOrPassword
	}

	s.clearLoginFailures(ctx, email)
	return s.buildLoginResponse(ctx, user)
}

func (s *authAdapter) SendPhoneLoginCode(ctx context.Context, req *account.SendPhoneLoginCodeInput) (*account.SendPhoneLoginCodeOutput, error) {
	if s.db == nil {
		return nil, accounterror.ErrDatabaseRequired
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
			sms.MaskPhone(phone), int64(phoneCodeResendInterval.Seconds()))
		return nil, accounterror.ErrPhoneCodeSendTooOften
	}

	code, err := s.nextPhoneCode()
	if err != nil {
		return nil, err
	}
	logs.InfoContextf(ctx, "SendPhoneLoginCode started: phone=%s sms_enabled=%t expires_in_seconds=%d resend_after_seconds=%d",
		sms.MaskPhone(phone), s.smsSender.Enabled(), int64(phoneCodeExpire.Seconds()), int64(phoneCodeResendInterval.Seconds()))
	if err := s.smsSender.SendVerificationCode(ctx, phone, code); err != nil {
		logs.ErrorContextf(ctx, "SendPhoneLoginCode send failed: phone=%s sms_enabled=%t error=%v",
			sms.MaskPhone(phone), s.smsSender.Enabled(), err)
		return nil, mapSMSSendError(err)
	}

	if err := db.CreateAuthPhoneVerificationCode(ctx, s.db, &types.AuthPhoneVerificationCode{
		Phone:     phone,
		CodeHash:  hashPhoneCode(phone, code),
		ExpiresAt: now.Add(phoneCodeExpire),
	}); err != nil {
		logs.ErrorContextf(ctx, "SendPhoneLoginCode store failed: phone=%s error=%v", sms.MaskPhone(phone), err)
		return nil, err
	}
	logs.InfoContextf(ctx, "SendPhoneLoginCode completed: phone=%s sms_enabled=%t",
		sms.MaskPhone(phone), s.smsSender.Enabled())

	return &account.SendPhoneLoginCodeOutput{
		Phone:       phone,
		ExpiresIn:   int64(phoneCodeExpire.Seconds()),
		ResendAfter: int64(phoneCodeResendInterval.Seconds()),
	}, nil
}

func (s *authAdapter) LoginByPhoneCode(ctx context.Context, req *account.LoginByPhoneCodeInput) (*account.AuthTokens, error) {
	if s.db == nil {
		return nil, accounterror.ErrDatabaseRequired
	}

	phone, err := normalizePhone(req.Phone)
	if err != nil {
		return nil, err
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		return nil, accounterror.ErrPhoneCodeRequired
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
		return nil, accounterror.ErrInvalidPhoneCode
	}

	var (
		user  *types.User
		isNew bool
	)
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
				PublicID: fmt.Sprintf("usr_%s", snowflake.GenerateIDBase58()),
				Name:     phone,
				Phone:    phone,
			}
			if err := db.CreateUser(ctx, tx, user); err != nil {
				return err
			}
			isNew = true
		}
		return nil
	}); err != nil {
		return nil, err
	}

	s.clearLoginFailures(ctx, phone)

	if isNew {
		refreshToken, err := s.generateRefreshToken(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		return &account.AuthTokens{
			LoginStatus:  account.LoginStatusNeedCreateCompany,
			RefreshToken: refreshToken,
			UserInfo: account.AuthUserInfo{
				ID:        user.ID,
				PublicID:  user.PublicID,
				Name:      user.Name,
				Phone:     user.Phone,
				AvatarURL: user.AvatarURL,
			},
			Edition: account.EditionOSS,
		}, nil
	}

	result, err := s.buildLoginResponse(ctx, user)
	if err != nil {
		return nil, err
	}
	result.Edition = account.EditionOSS
	return result, nil
}

func (s *authAdapter) RefreshToken(ctx context.Context, req *account.RefreshTokenInput) (*account.AuthTokens, error) {
	if s.db == nil {
		return nil, accounterror.ErrDatabaseRequired
	}
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		return nil, accounterror.ErrRefreshTokenRequired
	}

	now := time.Now()
	tokenHash := hashRefreshToken(refreshToken)
	s.cleanupExpiredAuthData(ctx, now)

	savedToken, err := db.GetActiveAuthRefreshToken(ctx, s.db, tokenHash, now)
	if err != nil {
		return nil, err
	}
	if savedToken == nil {
		return nil, accounterror.ErrRefreshTokenInvalid
	}
	if savedToken.Uin == 0 {
		return nil, accounterror.ErrRefreshTokenInvalid
	}

	userOrg, err := db.GetUserOrgByUin(ctx, s.db, savedToken.Uin)
	if err != nil {
		return nil, err
	}
	if userOrg == nil {
		return nil, accounterror.ErrUserOrgNotFound
	}

	user, err := db.GetUserByID(ctx, s.db, userOrg.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, accounterror.ErrUserNotFound
	}
	org, err := db.GetOrgByID(ctx, s.db, userOrg.OrgID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, accounterror.ErrOrgNotFound
	}

	if err := db.RevokeAuthRefreshToken(ctx, s.db, tokenHash, now); err != nil {
		return nil, err
	}
	return s.buildTokenResponse(ctx, user, userOrg, org)
}

func (s *authAdapter) ChooseUin(ctx context.Context, req *account.ChooseUinInput) (*account.AuthTokens, error) {
	if s.db == nil {
		return nil, accounterror.ErrDatabaseRequired
	}
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		return nil, accounterror.ErrRefreshTokenRequired
	}
	if req.Uin == 0 {
		return nil, accounterror.ErrUserOrgNotFound
	}

	now := time.Now()
	tokenHash := hashRefreshToken(refreshToken)
	s.cleanupExpiredAuthData(ctx, now)

	savedToken, err := db.GetActiveAuthRefreshToken(ctx, s.db, tokenHash, now)
	if err != nil {
		return nil, err
	}
	if savedToken == nil {
		return nil, accounterror.ErrRefreshTokenInvalid
	}

	user, err := s.resolveUserByRefreshTokenKey(ctx, savedToken.Uin)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, accounterror.ErrUserNotFound
	}

	targetUserOrg, err := db.GetUserOrgByUin(ctx, s.db, req.Uin)
	if err != nil {
		return nil, err
	}
	if targetUserOrg == nil {
		return nil, accounterror.ErrUserOrgNotFound
	}
	if targetUserOrg.UserID != user.ID {
		return nil, accounterror.ErrUserOrgNotAllowed
	}

	org, err := db.GetOrgByID(ctx, s.db, targetUserOrg.OrgID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, accounterror.ErrOrgNotFound
	}

	if err := db.RevokeAuthRefreshToken(ctx, s.db, tokenHash, now); err != nil {
		return nil, err
	}
	return s.buildTokenResponse(ctx, user, targetUserOrg, org)
}

func (s *authAdapter) SwitchOrganization(ctx context.Context, req *account.SwitchOrganizationInput) (*account.AuthTokens, error) {
	return nil, accounterror.ErrNotImplementedEdition
}

func (s *authAdapter) CreateOrganization(ctx context.Context, req *account.CreateOrganizationInput) (*account.AuthTokens, error) {
	if s.db == nil {
		return nil, accounterror.ErrDatabaseRequired
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("组织名称不能为空")
	}

	caller, _ := localauth.FromContext(ctx)
	hasJWT := caller != nil && caller.State == types.AuthStateSucc && caller.Uin != 0

	var user *types.User
	if hasJWT {
		var err error
		user, err = s.resolveUserByCaller(ctx, caller)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		user, err = s.resolveUserByRefreshToken(ctx, req.RefreshToken)
		if err != nil {
			return nil, err
		}
	}
	if user == nil {
		return nil, accounterror.ErrUserNotFound
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
			return accounterror.ErrOrganizationLimitExceeded
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
		if err := db.CloneSystemLLMModelsByOrg(ctx, tx, 1, org.ID); err != nil {
			return fmt.Errorf("clone system llm models: %w", err)
		}

		userOrg = &types.UserOrg{
			UserID:    user.ID,
			OrgID:     org.ID,
			IsDefault: true,
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
			Name:     org.Name,
			ParentID: 0,
			Sort:     db.DepartmentSortGap,
			OrgID:    org.ID,
		}
		if err := db.CreateDepartment(ctx, tx, department); err != nil {
			return err
		}

		existing, err := db.ListMemberDepartmentsByUinAndOrgID(ctx, tx, userOrg.Uin, org.ID)
		if err != nil {
			return err
		}
		for _, rel := range existing {
			if rel.DepartmentID == department.ID {
				return errors.New("组织成员部门关联已存在")
			}
		}

		if err := db.CreateMemberDepartment(ctx, tx, &types.MemberDepartment{
			Uin:          userOrg.Uin,
			OrgID:        org.ID,
			DepartmentID: department.ID,
			IsPrimary:    true,
		}); err != nil {
			return err
		}
		if s.workerProvisioning != nil {
			if _, err := s.workerProvisioning.EnsureDefaultWorkerForOrg(ctx, org.ID, userOrg.Uin); err != nil {
				return fmt.Errorf("ensure default worker deployment: %w", err)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	organizations, err := s.userOrganizationInfos(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	orgInfo := authOrgInfo(org, userOrg.IsDefault)
	if account.IsFilePublicID(orgInfo.Logo) {
		if logoMap, err := resolveSingleOrgLogoMap(ctx, s.db, userOrg.OrgID, orgInfo.Logo); err == nil && logoMap != nil {
			orgInfo.Logo = logoMap[orgInfo.Logo]
		}
	}
	return &account.AuthTokens{
		LoginStatus:   account.LoginStatusSuccess,
		Uin:           userOrg.Uin,
		RefreshToken:  req.RefreshToken,
		UserInfo:      authUserInfoFromModel(user),
		Org:           orgInfo,
		Organizations: organizations,
	}, nil
}

func (s *authAdapter) AuthSession(ctx context.Context) (*account.AuthSessionOutput, error) {
	if s.db == nil {
		return nil, accounterror.ErrDatabaseRequired
	}

	caller, _ := localauth.FromContext(ctx)
	if caller == nil || caller.State != types.AuthStateSucc || caller.Uin == 0 {
		return nil, accounterror.ErrLoginRequired
	}

	userOrg, err := db.GetUserOrgByUinAndOrgID(ctx, s.db, caller.Uin, caller.OrgID)
	if err != nil {
		return nil, err
	}
	if userOrg == nil {
		userOrg, err = db.GetUserOrgByUin(ctx, s.db, caller.Uin)
		if err != nil {
			return nil, err
		}
	}
	if userOrg == nil {
		return nil, accounterror.ErrUserOrgNotFound
	}

	user, err := db.GetUserByID(ctx, s.db, userOrg.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, accounterror.ErrUserNotFound
	}

	_, org, err := s.userOrgWithOrganization(ctx, s.db, userOrg)
	if err != nil {
		return nil, err
	}
	return s.buildAuthSessionResponse(ctx, user, userOrg, org)
}

func (s *authAdapter) buildTokenResponse(ctx context.Context, user *types.User, userOrg *types.UserOrg, org *types.Organization) (*account.AuthTokens, error) {
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

	return &account.AuthTokens{
		LoginStatus:   account.LoginStatusSuccess,
		JwtToken:      token,
		RefreshToken:  refreshToken,
		ExpiredAt:     expiredAt,
		Uin:           userOrg.Uin,
		UserInfo:      session.UserInfo,
		Org:           session.Org,
		Organizations: session.Organizations,
	}, nil
}

func (s *authAdapter) buildLoginResponse(ctx context.Context, user *types.User) (*account.AuthTokens, error) {
	refreshToken, err := s.generateRefreshToken(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	organizations, err := s.userOrganizationInfos(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &account.AuthTokens{
		LoginStatus:   account.LoginStatusSuccess,
		RefreshToken:  refreshToken,
		UserInfo:      authUserInfoFromModel(user),
		Organizations: organizations,
	}, nil
}

func (s *authAdapter) resolveUserByCaller(ctx context.Context, caller *types.Caller) (*types.User, error) {
	userOrg, err := db.GetUserOrgByUin(ctx, s.db, caller.Uin)
	if err != nil {
		return nil, err
	}
	if userOrg == nil {
		return nil, accounterror.ErrUserOrgNotFound
	}
	return db.GetUserByID(ctx, s.db, userOrg.UserID)
}

func (s *authAdapter) resolveUserByRefreshToken(ctx context.Context, refreshToken string) (*types.User, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, accounterror.ErrRefreshTokenRequired
	}
	tokenHash := hashRefreshToken(refreshToken)
	savedToken, err := db.GetActiveAuthRefreshToken(ctx, s.db, tokenHash, time.Now())
	if err != nil {
		return nil, err
	}
	if savedToken == nil {
		return nil, accounterror.ErrRefreshTokenInvalid
	}
	return s.resolveUserByRefreshTokenKey(ctx, savedToken.Uin)
}

func (s *authAdapter) resolveUserByRefreshTokenKey(ctx context.Context, key uint) (*types.User, error) {
	if key == 0 {
		return nil, accounterror.ErrRefreshTokenInvalid
	}

	user, err := db.GetUserByID(ctx, s.db, key)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return user, nil
	}

	userOrg, err := db.GetUserOrgByUin(ctx, s.db, key)
	if err != nil {
		return nil, err
	}
	if userOrg != nil {
		return db.GetUserByID(ctx, s.db, userOrg.UserID)
	}

	return nil, accounterror.ErrUserNotFound
}

func authUserInfoFromModel(user *types.User) account.AuthUserInfo {
	return account.AuthUserInfo{
		ID:        user.ID,
		PublicID:  user.PublicID,
		Name:      user.Name,
		Email:     user.Email,
		Phone:     user.Phone,
		AvatarURL: user.AvatarURL,
	}
}

func (s *authAdapter) buildAuthSessionResponse(ctx context.Context, user *types.User, userOrg *types.UserOrg, org *types.Organization) (*account.AuthSessionOutput, error) {
	organizations, err := s.userOrganizationInfos(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	orgInfo := authOrgInfo(org, userOrg.IsDefault)
	if account.IsFilePublicID(orgInfo.Logo) {
		if logoMap, err := resolveSingleOrgLogoMap(ctx, s.db, userOrg.OrgID, orgInfo.Logo); err == nil && logoMap != nil {
			orgInfo.Logo = logoMap[orgInfo.Logo]
		}
	}
	return &account.AuthSessionOutput{
		UserInfo: account.AuthUserInfo{
			ID:        user.ID,
			PublicID:  user.PublicID,
			Name:      user.Name,
			Email:     user.Email,
			Phone:     user.Phone,
			AvatarURL: user.AvatarURL,
		},
		Org:           orgInfo,
		Organizations: organizations,
	}, nil
}

func (s *authAdapter) generateJWT(userOrg *types.UserOrg) (string, int64, error) {
	if s.jwtSecret == "" {
		return "", 0, accounterror.ErrJWTSecretRequired
	}
	return localauth.GenerateUserToken(localauth.UserClaims{
		Uin: userOrg.Uin,
	}, s.jwtSecret, accessTokenExpire)
}

func (s *authAdapter) generateRefreshToken(ctx context.Context, uin uint) (string, error) {
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

func (s *authAdapter) userOrgWithOrganization(ctx context.Context, database *gorm.DB, userOrg *types.UserOrg) (*types.UserOrg, *types.Organization, error) {
	org, err := db.GetOrgByID(ctx, database, userOrg.OrgID)
	if err != nil {
		return nil, nil, err
	}
	if org == nil {
		return nil, nil, accounterror.ErrOrgNotFound
	}
	return userOrg, org, nil
}

func (s *authAdapter) userOrganizationInfos(ctx context.Context, userID uint) ([]account.AuthOrgInfo, error) {
	userOrgs, err := db.GetUserOrgsByUserID(ctx, s.db, userID)
	if err != nil {
		return nil, err
	}
	if len(userOrgs) == 0 {
		return make([]account.AuthOrgInfo, 0), nil
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

	infos := make([]account.AuthOrgInfo, 0, len(userOrgs))
	for _, userOrg := range userOrgs {
		org := orgByID[userOrg.OrgID]
		if org == nil {
			continue
		}
		info := authOrgInfo(org, userOrg.IsDefault)
		if account.IsFilePublicID(info.Logo) {
			if logoMap, err := resolveSingleOrgLogoMap(ctx, s.db, userOrg.OrgID, info.Logo); err == nil && logoMap != nil {
				info.Logo = logoMap[info.Logo]
			}
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func authOrgInfo(org *types.Organization, isDefault bool) account.AuthOrgInfo {
	return account.AuthOrgInfo{
		ID:           org.ID,
		PublicID:     org.PublicID,
		Code:         org.Code,
		Name:         org.Name,
		Logo:         org.Logo,
		IsDefault:    isDefault,
		CreatedByUin: org.CreatedByUin,
		Uin:          org.ID,
	}
}

func (s *authAdapter) ensureLoginAllowed(ctx context.Context, email string) error {
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
		return accounterror.ErrLoginAttemptsExceeded
	}
	return nil
}

func (s *authAdapter) recordLoginFailure(ctx context.Context, email string) {
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

func (s *authAdapter) clearLoginFailures(ctx context.Context, email string) {
	if err := db.DeleteAuthLoginAttempt(ctx, s.db, email); err != nil {
		logs.WarnContextf(ctx, "LoginByEmail: clear login attempt counter failed: %v", err)
	}
}

func (s *authAdapter) cleanupExpiredAuthData(ctx context.Context, now time.Time) {
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

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", accounterror.ErrEmailRequired
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || !strings.Contains(email, "@") {
		return "", accounterror.ErrInvalidEmailFormat
	}
	return email, nil
}

func normalizePhone(phone string) (string, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", accounterror.ErrPhoneRequired
	}
	phone = strings.TrimPrefix(phone, "+86")
	phone = strings.TrimPrefix(phone, "86")
	if !regexp.MustCompile(`^1[3-9]\d{9}$`).MatchString(phone) {
		return "", accounterror.ErrInvalidPhoneFormat
	}
	return phone, nil
}

func validateRegisterPassword(password, confirmPassword string) error {
	if password != confirmPassword {
		return accounterror.ErrPasswordsDoNotMatch
	}
	return validatePasswordStrength(password)
}

func validatePasswordStrength(password string) error {
	if strings.TrimSpace(password) == "" {
		return accounterror.ErrPasswordRequired
	}
	if len(password) < 8 {
		return accounterror.ErrPasswordTooShort
	}
	if len(password) > 20 {
		return accounterror.ErrPasswordTooLong
	}
	categoryCount := 0
	hasLower := false
	hasUpper := false
	hasDigit := false
	hasSpecial := false
	for _, r := range password {
		if r >= '\u4e00' && r <= '\u9fff' {
			return accounterror.ErrPasswordContainsChinese
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return accounterror.ErrPasswordContainsWhitespace
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
		return accounterror.ErrPasswordStrength
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

func mapSMSSendError(err error) error {
	return accounterror.ErrSMSDeliveryFailed
}

func (s *authAdapter) nextPhoneCode() (string, error) {
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
