package db

import (
	"context"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// BatchCreateMessageResources creates multiple message_resource records in a single transaction.
func BatchCreateMessageResources(ctx context.Context, db *gorm.DB, records []*types.MessageResource) error {
	if len(records) == 0 {
		return nil
	}

	return db.WithContext(ctx).Create(records).Error
}

// GetDistinctSkillCodes returns distinct skill codes from message_resource records
// ordered by most recently used first, filtered by org_id and uin.
func GetDistinctSkillCodes(ctx context.Context, db *gorm.DB, orgID, uin uint, limit int) ([]string, error) {
	var results []struct {
		ResourceKey string
	}
	err := db.WithContext(ctx).
		Model(&types.MessageResource{}).
		Select("resource_key, MAX(created_at) AS max_created_at").
		Where("resource_type = ? AND org_id = ? AND uin = ?", "skill", orgID, uin).
		Group("resource_key").
		Order("max_created_at DESC").
		Limit(limit).
		Find(&results).Error
	if err != nil {
		return nil, err
	}

	codes := make([]string, len(results))
	for i, r := range results {
		codes[i] = r.ResourceKey
	}
	return codes, nil
}
