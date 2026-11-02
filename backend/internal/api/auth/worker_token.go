package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
)

const (
	WorkerTokenIssuer   = "lework"
	WorkerTokenAudience = "worker"
	WorkerTokenKind     = "worker"
	WorkerTokenScopeAPI = "worker:server-api"
)

var (
	ErrWorkerTokenSecretRequired = errors.New("worker token secret is required")
	ErrInvalidWorkerToken        = errors.New("invalid worker token")
)

// GenerateBootstrapToken creates an opaque token used once by server-managed workers at startup.
func GenerateBootstrapToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// HashBootstrapToken returns the stored verifier for a bootstrap token.
func HashBootstrapToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// WorkerClaims carries the AI teammate identity used by a worker process.
type WorkerClaims struct {
	OrgID    uint   `json:"org_id"`
	WorkerID uint   `json:"worker_id"`
	Kind     string `json:"kind"`
	Scope    string `json:"scope"`
	jwt.StandardClaims
}

// GenerateWorkerToken creates a short-lived token for a worker/AI teammate.
func GenerateWorkerToken(orgID, workerID uint, secret string, ttl time.Duration) (string, int64, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", 0, ErrWorkerTokenSecretRequired
	}
	if orgID == 0 {
		return "", 0, fmt.Errorf("org_id is required")
	}
	if workerID == 0 {
		return "", 0, fmt.Errorf("worker_id is required")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	now := time.Now()
	expiresAt := now.Add(ttl).Unix()
	claims := &WorkerClaims{
		OrgID:    orgID,
		WorkerID: workerID,
		Kind:     WorkerTokenKind,
		Scope:    WorkerTokenScopeAPI,
		StandardClaims: jwt.StandardClaims{
			Subject:   fmt.Sprintf("worker:%d:%d", orgID, workerID),
			Issuer:    WorkerTokenIssuer,
			Audience:  WorkerTokenAudience,
			IssuedAt:  now.Unix(),
			ExpiresAt: expiresAt,
		},
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		return "", 0, fmt.Errorf("generate worker token: %w", err)
	}
	return token, expiresAt, nil
}

// ParseWorkerToken verifies a worker token and returns the AI teammate identity.
func ParseWorkerToken(tokenStr, secret string) (*WorkerClaims, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, ErrWorkerTokenSecretRequired
	}

	claims := &WorkerClaims{}
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
		return nil, ErrInvalidWorkerToken
	}
	if claims.Kind != WorkerTokenKind || claims.Audience != WorkerTokenAudience || claims.Scope != WorkerTokenScopeAPI {
		return nil, ErrInvalidWorkerToken
	}
	if claims.OrgID == 0 || claims.WorkerID == 0 {
		return nil, ErrInvalidWorkerToken
	}
	return claims, nil
}
