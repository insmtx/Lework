package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	ygauth "github.com/ygpkg/yg-go/apis/runtime/auth"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

const (
	authIssuer              = "leros"
	authAudience            = "user"
	accessTokenExpire       = 24 * time.Hour
	refreshTokenExpire      = 7 * 24 * time.Hour
	loginAttemptWindow      = 5 * time.Minute
	loginAttemptMaxFailures = 5
)

var _ contract.AuthService = (*authService)(nil)

type authService struct {
	db        *gorm.DB
	jwtSecret string
}

func NewAuthService(d *gorm.DB, jwtSecret string) contract.AuthService {
	return &authService{
		db:        d,
		jwtSecret: strings.TrimSpace(jwtSecret),
	}
}

func (s *authService) RegisterByEmail(ctx context.Context, req *contract.RegisterByEmailRequest) (*contract.AuthTokenResponse, error) {
	if s.db == nil {
		return nil, errors.New("database is required")
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
		return nil, errors.New("email already exists")
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
				return errors.New("email already exists")
			}
			return err
		}

		var err error
		org, err = createAccountOrg(ctx, tx, name)
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
		return nil, errors.New("database is required")
	}

	email, err := normalizeEmail(req.Email)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Password) == "" {
		return nil, errors.New("password is required")
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
		return nil, errors.New("invalid email or password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		s.recordLoginFailure(ctx, email)
		logs.WarnContextf(ctx, "LoginByEmail: password not match for email=%s: %v", email, err)
		return nil, errors.New("invalid email or password")
	}

	s.clearLoginFailures(ctx, email)
	userOrg, org, err := s.defaultUserOrg(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return s.buildTokenResponse(ctx, user, userOrg, org)
}

func (s *authService) RefreshToken(ctx context.Context, req *contract.RefreshTokenRequest) (*contract.AuthTokenResponse, error) {
	if s.db == nil {
		return nil, errors.New("database is required")
	}
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		return nil, errors.New("refresh_token is required")
	}

	now := time.Now()
	tokenHash := hashRefreshToken(refreshToken)
	s.cleanupExpiredAuthData(ctx, now)

	savedToken, err := db.GetActiveAuthRefreshToken(ctx, s.db, tokenHash, now)
	if err != nil {
		return nil, err
	}
	if savedToken == nil {
		return nil, errors.New("refresh token invalid")
	}

	user, err := db.GetUserByID(ctx, s.db, savedToken.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("user not found")
	}
	userOrg, org, err := s.defaultUserOrg(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	if err := db.RevokeAuthRefreshToken(ctx, s.db, tokenHash, now); err != nil {
		return nil, err
	}
	return s.buildTokenResponse(ctx, user, userOrg, org)
}

func (s *authService) buildTokenResponse(ctx context.Context, user *types.User, userOrg *types.UserOrg, org *types.Organization) (*contract.AuthTokenResponse, error) {
	token, expiredAt, err := s.generateJWT(userOrg.Uin)
	if err != nil {
		return nil, err
	}
	refreshToken, err := s.generateRefreshToken(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &contract.AuthTokenResponse{
		LoginStatus:  "success",
		JwtToken:     token,
		RefreshToken: refreshToken,
		ExpiredAt:    expiredAt,
		Uin:          userOrg.Uin,
		UserInfo: contract.AuthUserInfo{
			ID:          user.ID,
			PublicID:    user.PublicID,
			Name:        user.Name,
			Email:       user.Email,
			GithubLogin: user.GithubLogin,
			AvatarURL:   user.AvatarURL,
		},
		Org: contract.AuthOrgInfo{
			ID:       org.ID,
			PublicID: org.PublicID,
			Code:     org.Code,
			Name:     org.Name,
		},
	}, nil
}

func (s *authService) generateJWT(uin uint) (string, int64, error) {
	if s.jwtSecret == "" {
		return "", 0, errors.New("jwt secret is required")
	}
	expiredAt := jwt.TimeFunc().Add(accessTokenExpire).Unix()
	claims := ygauth.UserClaims{
		Uin:       uin,
		Issuer:    authIssuer,
		IssuedAt:  jwt.TimeFunc().Unix(),
		ExpiresAt: expiredAt,
		LoginWay:  ygauth.LoginWayEmail,
		Audience:  authAudience,
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.jwtSecret))
	if err != nil {
		return "", 0, fmt.Errorf("generate jwt token: %w", err)
	}
	return token, expiredAt, nil
}

func (s *authService) generateRefreshToken(ctx context.Context, userID uint) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}

	now := time.Now()
	s.cleanupExpiredAuthData(ctx, now)
	if err := db.CreateAuthRefreshToken(ctx, s.db, &types.AuthRefreshToken{
		TokenHash: hashRefreshToken(token),
		UserID:    userID,
		ExpiresAt: now.Add(refreshTokenExpire),
	}); err != nil {
		return "", fmt.Errorf("store refresh token: %w", err)
	}
	return token, nil
}

func (s *authService) defaultUserOrg(ctx context.Context, userID uint) (*types.UserOrg, *types.Organization, error) {
	userOrg, err := db.GetUserOrgByUserID(ctx, s.db, userID)
	if err != nil {
		return nil, nil, err
	}
	if userOrg == nil {
		return nil, nil, errors.New("user org not found")
	}
	org, err := db.GetOrgByID(ctx, s.db, userOrg.OrgID)
	if err != nil {
		return nil, nil, err
	}
	if org == nil {
		return nil, nil, errors.New("org not found")
	}
	return userOrg, org, nil
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
		return errors.New("login attempts exceeded")
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
}

func createAccountOrg(ctx context.Context, tx *gorm.DB, name string) (*types.Organization, error) {
	org := &types.Organization{
		PublicID: fmt.Sprintf("org_%s", snowflake.GenerateIDBase58()),
		Type:     "company",
		Code:     fmt.Sprintf("org_%s", snowflake.GenerateIDBase58()),
		Name:     name,
		Status:   "active",
	}
	if err := db.CreateOrg(ctx, tx, org); err != nil {
		return nil, err
	}
	return org, nil
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", errors.New("email is required")
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || !strings.Contains(email, "@") {
		return "", errors.New("invalid email format")
	}
	return email, nil
}

func validateRegisterPassword(password, confirmPassword string) error {
	if password != confirmPassword {
		return errors.New("passwords do not match")
	}
	return validatePasswordStrength(password)
}

func validatePasswordStrength(password string) error {
	if strings.TrimSpace(password) == "" {
		return errors.New("password is required")
	}
	if len(password) < 8 {
		return errors.New("password too short")
	}
	if len(password) > 36 {
		return errors.New("password too long")
	}
	hasLetter := false
	hasDigit := false
	for _, r := range password {
		if r >= '\u4e00' && r <= '\u9fff' {
			return errors.New("password cannot contain chinese characters")
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return errors.New("password cannot contain whitespace")
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			hasLetter = true
		}
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return errors.New("password must contain letters and digits")
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

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
