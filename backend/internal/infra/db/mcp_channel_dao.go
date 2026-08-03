package db

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
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
