package db

import (
	"context"

	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/types"
)

// CreateLLMHistory persists an LLM history record.
func CreateLLMHistory(ctx context.Context, db *gorm.DB, record *types.LLMHistory) error {
	return db.WithContext(ctx).Create(record).Error
}

// ListLLMHistory queries LLM history records with optional filters.
// Results are ordered by started_at DESC.
func ListLLMHistory(ctx context.Context, db *gorm.DB, orgID uint, offset, limit int, modelID *uint, provider, callerType *string, success *bool) ([]*types.LLMHistory, int64, error) {
	var records []*types.LLMHistory
	var total int64

	query := db.WithContext(ctx).Model(&types.LLMHistory{}).Where("org_id = ?", orgID)

	if modelID != nil {
		query = query.Where("model_id = ?", *modelID)
	}
	if provider != nil && *provider != "" {
		query = query.Where("provider = ?", *provider)
	}
	if callerType != nil && *callerType != "" {
		query = query.Where("caller_type = ?", *callerType)
	}
	if success != nil {
		query = query.Where("success = ?", *success)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if limit > 0 {
		query = query.Limit(limit)
	} else {
		query = query.Limit(50)
	}
	query = query.Offset(offset).Order("started_at DESC")

	if err := query.Find(&records).Error; err != nil {
		return nil, 0, err
	}
	return records, total, nil
}
