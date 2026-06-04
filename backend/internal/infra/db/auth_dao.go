package db

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func CreateAuthRefreshToken(ctx context.Context, d *gorm.DB, token *types.AuthRefreshToken) error {
	return d.WithContext(ctx).Create(token).Error
}

func GetActiveAuthRefreshToken(ctx context.Context, d *gorm.DB, tokenHash string, now time.Time) (*types.AuthRefreshToken, error) {
	var token types.AuthRefreshToken
	err := d.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", tokenHash, now).
		First(&token).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &token, nil
}

func RevokeAuthRefreshToken(ctx context.Context, d *gorm.DB, tokenHash string, revokedAt time.Time) error {
	return d.WithContext(ctx).
		Model(&types.AuthRefreshToken{}).
		Where("token_hash = ? AND revoked_at IS NULL", tokenHash).
		Update("revoked_at", revokedAt).Error
}

func DeleteExpiredAuthRefreshTokens(ctx context.Context, d *gorm.DB, now time.Time) error {
	return d.WithContext(ctx).
		Unscoped().
		Where("expires_at <= ? OR revoked_at IS NOT NULL", now).
		Delete(&types.AuthRefreshToken{}).Error
}

func GetAuthLoginAttempt(ctx context.Context, d *gorm.DB, identifier string) (*types.AuthLoginAttempt, error) {
	var attempt types.AuthLoginAttempt
	err := d.WithContext(ctx).Where("identifier = ?", identifier).First(&attempt).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &attempt, nil
}

func SaveAuthLoginAttempt(ctx context.Context, d *gorm.DB, attempt *types.AuthLoginAttempt) error {
	return d.WithContext(ctx).Save(attempt).Error
}

func DeleteAuthLoginAttempt(ctx context.Context, d *gorm.DB, identifier string) error {
	return d.WithContext(ctx).
		Unscoped().
		Where("identifier = ?", identifier).
		Delete(&types.AuthLoginAttempt{}).Error
}

func DeleteExpiredAuthLoginAttempts(ctx context.Context, d *gorm.DB, now time.Time) error {
	return d.WithContext(ctx).
		Unscoped().
		Where("window_expires_at <= ?", now).
		Delete(&types.AuthLoginAttempt{}).Error
}
