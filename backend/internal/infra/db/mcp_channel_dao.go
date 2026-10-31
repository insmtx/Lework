package db

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Lework/backend/types"
)

// ListActiveMCPChannels returns active channel configurations in stable channel order.
func ListActiveMCPChannels(ctx context.Context, database *gorm.DB) ([]types.MCPChannel, error) {
	var channels []types.MCPChannel
	err := database.WithContext(ctx).
		Where("status = ?", types.MCPChannelStatusActive).
		Order("channel ASC, id ASC").
		Find(&channels).Error
	if err != nil {
		return nil, err
	}
	return channels, nil
}

// GetActiveMCPChannelByChannel returns one active channel configuration.
func GetActiveMCPChannelByChannel(
	ctx context.Context,
	database *gorm.DB,
	channel string,
) (*types.MCPChannel, error) {
	var config types.MCPChannel
	err := database.WithContext(ctx).
		Where("channel = ? AND status = ?", channel, types.MCPChannelStatusActive).
		First(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// UpsertMCPChannel reconciles all declared channel fields within the caller's transaction.
func UpsertMCPChannel(ctx context.Context, database *gorm.DB, channel *types.MCPChannel) error {
	if channel == nil {
		return errors.New("MCP channel is required")
	}
	var existing types.MCPChannel
	err := database.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("channel = ?", channel.Channel).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		insert := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(channel)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected > 0 {
			return nil
		}
		if err := database.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("channel = ?", channel.Channel).First(&existing).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := database.WithContext(ctx).Model(&types.MCPChannel{}).
		Where("id = ?", existing.ID).
		Select("name", "description", "skill_code", "transport", "url", "headers", "auth_type", "auth_config", "status").
		Updates(channel).Error; err != nil {
		return err
	}
	channel.ID = existing.ID
	return nil
}
