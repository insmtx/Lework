//go:build !enterprise

package oss

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

type authRepo struct {
	db *gorm.DB
}

func newAuthRepo(d *gorm.DB) *authRepo {
	return &authRepo{db: d}
}

func (r *authRepo) withTx(tx *gorm.DB) *authRepo {
	return &authRepo{db: tx}
}

// --- Phone Verification Codes ---

func (r *authRepo) CreatePhoneCode(ctx context.Context, code *types.AuthPhoneVerificationCode) error {
	return db.CreateAuthPhoneVerificationCode(ctx, r.db, code)
}

func (r *authRepo) GetActivePhoneCode(ctx context.Context, phone string, now time.Time) (*types.AuthPhoneVerificationCode, error) {
	return db.GetActiveAuthPhoneVerificationCode(ctx, r.db, phone, now)
}

func (r *authRepo) MarkPhoneCodeUsed(ctx context.Context, id uint, usedAt time.Time) error {
	return db.MarkAuthPhoneVerificationCodeUsed(ctx, r.db, id, usedAt)
}

func (r *authRepo) DeleteExpiredPhoneCodes(ctx context.Context, now time.Time) error {
	return db.DeleteExpiredAuthPhoneVerificationCodes(ctx, r.db, now)
}

// --- Refresh Tokens ---

func (r *authRepo) CreateRefreshToken(ctx context.Context, token *types.AuthRefreshToken) error {
	return db.CreateAuthRefreshToken(ctx, r.db, token)
}

func (r *authRepo) GetActiveRefreshToken(ctx context.Context, tokenHash string, now time.Time) (*types.AuthRefreshToken, error) {
	return db.GetActiveAuthRefreshToken(ctx, r.db, tokenHash, now)
}

func (r *authRepo) RevokeRefreshToken(ctx context.Context, tokenHash string, now time.Time) error {
	return db.RevokeAuthRefreshToken(ctx, r.db, tokenHash, now)
}

func (r *authRepo) DeleteExpiredRefreshTokens(ctx context.Context, now time.Time) error {
	return db.DeleteExpiredAuthRefreshTokens(ctx, r.db, now)
}

// --- Login Attempts ---

func (r *authRepo) GetLoginAttempt(ctx context.Context, identifier string) (*types.AuthLoginAttempt, error) {
	return db.GetAuthLoginAttempt(ctx, r.db, identifier)
}

func (r *authRepo) SaveLoginAttempt(ctx context.Context, attempt *types.AuthLoginAttempt) error {
	return db.SaveAuthLoginAttempt(ctx, r.db, attempt)
}

func (r *authRepo) DeleteLoginAttempt(ctx context.Context, identifier string) error {
	return db.DeleteAuthLoginAttempt(ctx, r.db, identifier)
}

func (r *authRepo) DeleteExpiredLoginAttempts(ctx context.Context, now time.Time) error {
	return db.DeleteExpiredAuthLoginAttempts(ctx, r.db, now)
}
