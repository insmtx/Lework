package db

import (
	"context"

	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/types"
)

// GetAssistantsByIDs 批量根据ID查询数字助手
func GetAssistantsByIDs(ctx context.Context, db *gorm.DB, ids []uint) ([]*types.DigitalAssistant, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var entities []*types.DigitalAssistant
	err := db.WithContext(ctx).Where("id IN (?)", ids).Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// GetAssistantsByPublicIDs 批量根据公开 ID 查询数字助手。
func GetAssistantsByPublicIDs(ctx context.Context, db *gorm.DB, publicIDs []string) ([]*types.DigitalAssistant, error) {
	if len(publicIDs) == 0 {
		return nil, nil
	}
	var entities []*types.DigitalAssistant
	err := db.WithContext(ctx).Where("public_id IN (?)", publicIDs).Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}
