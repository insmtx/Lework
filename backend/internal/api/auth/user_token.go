package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
)

const (
	UserTokenIssuer   = "lework"
	UserTokenAudience = "user"

	LoginWayPassword = 1
	LoginWayPhone    = 2
)

var (
	ErrUserTokenSecretRequired = errors.New("user token secret is required")
	ErrInvalidUserToken        = errors.New("invalid user token")
)

// UserClaims carries the active organization identity for a signed-in user.
// JSON keys use short names matching the IAM token format.
type UserClaims struct {
	Uin       uint   `json:"c,omitempty"`
	IssuedAt  int64  `json:"t,omitempty"`
	ExpiresAt int64  `json:"e,omitempty"`
	Issuer    string `json:"i,omitempty"`
	Audience  string `json:"a,omitempty"`
	LoginWay  int    `json:"l,omitempty"`
}

func (c UserClaims) Valid() error {
	vErr := new(jwt.ValidationError)
	now := jwt.TimeFunc().Unix()
	if c.IssuedAt > now {
		vErr.Inner = fmt.Errorf("token used before issued")
		vErr.Errors |= jwt.ValidationErrorIssuedAt
	}
	if c.ExpiresAt < now {
		vErr.Inner = fmt.Errorf("token is expired")
		vErr.Errors |= jwt.ValidationErrorExpired
	}
	if vErr.Errors == 0 {
		return nil
	}
	return vErr
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
	claims.IssuedAt = now.Unix()
	claims.ExpiresAt = expiresAt
	claims.Issuer = UserTokenIssuer
	claims.Audience = UserTokenAudience

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
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
