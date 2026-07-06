package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
)

const (
	UserTokenIssuer   = "leros"
	UserTokenAudience = "user"
)

var (
	ErrUserTokenSecretRequired = errors.New("user token secret is required")
	ErrInvalidUserToken        = errors.New("invalid user token")
)

// UserClaims carries the active organization identity for a signed-in user.
type UserClaims struct {
	Uin uint `json:"uin"`
	jwt.StandardClaims
}

// GenerateUserToken creates an access token bound to a user's active organization.
func GenerateUserToken(claims UserClaims, secret string, ttl time.Duration) (string, int64, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", 0, ErrUserTokenSecretRequired
	}
	if ttl <= 0 {
		return "", 0, fmt.Errorf("token ttl must be positive")
	}
	if claims.Uin == 0 {
		return "", 0, ErrInvalidUserToken
	}

	now := time.Now()
	expiresAt := now.Add(ttl).Unix()
	claims.StandardClaims = jwt.StandardClaims{
		Subject:   fmt.Sprintf("user:uin:%d", claims.Uin),
		Issuer:    UserTokenIssuer,
		Audience:  UserTokenAudience,
		IssuedAt:  now.Unix(),
		ExpiresAt: expiresAt,
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &claims).SignedString([]byte(secret))
	if err != nil {
		return "", 0, fmt.Errorf("generate user token: %w", err)
	}
	return token, expiresAt, nil
}

// ParseUserToken verifies a user token and returns the active organization identity.
func ParseUserToken(tokenStr, secret string) (*UserClaims, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, ErrUserTokenSecretRequired
	}

	claims := &UserClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	if token == nil || !token.Valid {
		return nil, ErrInvalidUserToken
	}
	if claims.Audience != UserTokenAudience {
		return nil, ErrInvalidUserToken
	}
	if claims.Uin == 0 {
		return nil, ErrInvalidUserToken
	}
	return claims, nil
}
