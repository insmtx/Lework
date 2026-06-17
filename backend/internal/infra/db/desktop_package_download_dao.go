package db

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/types"
)

// DesktopPackageDownloadDimension identifies one aggregated desktop installer package.
type DesktopPackageDownloadDimension struct {
	Version     string
	Platform    string
	Arch        string
	Channel     string
	PackageType string
	Source      string
}

// ClaimDesktopPackageDownloadWindow claims one report window for the same IP and package dimension.
func ClaimDesktopPackageDownloadWindow(
	ctx context.Context,
	db *gorm.DB,
	ipHash string,
	dim DesktopPackageDownloadDimension,
	now time.Time,
	window time.Duration,
) (bool, error) {
	var entity types.DesktopPackageDownloadRateLimit
	err := db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("ip_hash = ? AND version = ? AND platform = ? AND arch = ?", ipHash, dim.Version, dim.Platform, dim.Arch).
		First(&entity).Error
	if err == nil {
		if entity.UpdatedAt.After(now.Add(-window)) {
			return false, nil
		}
		entity.UpdatedAt = now
		if err := db.WithContext(ctx).Save(&entity).Error; err != nil {
			return false, err
		}
		return true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}

	entity = types.DesktopPackageDownloadRateLimit{
		IPHash:   ipHash,
		Version:  dim.Version,
		Platform: dim.Platform,
		Arch:     dim.Arch,
	}
	result := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&entity)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// IncrementDesktopPackageDownload increments one package dimension and returns the dimension count.
func IncrementDesktopPackageDownload(
	ctx context.Context,
	db *gorm.DB,
	dim DesktopPackageDownloadDimension,
	now time.Time,
) (int64, error) {
	stat := &types.DesktopPackageDownloadStat{
		Version:          dim.Version,
		Platform:         dim.Platform,
		Arch:             dim.Arch,
		Channel:          dim.Channel,
		PackageType:      dim.PackageType,
		Source:           dim.Source,
		DownloadCount:    1,
		LastDownloadedAt: &now,
	}

	err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "version"},
			{Name: "platform"},
			{Name: "arch"},
			{Name: "channel"},
			{Name: "package_type"},
			{Name: "source"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"download_count":     gorm.Expr(types.TableNameDesktopPackageDownloadStat+".download_count + ?", 1),
			"last_downloaded_at": now,
			"updated_at":         now,
		}),
	}).Create(stat).Error
	if err != nil {
		return 0, err
	}

	var downloadCount int64
	err = db.WithContext(ctx).Model(&types.DesktopPackageDownloadStat{}).
		Where("version = ? AND platform = ? AND arch = ? AND channel = ? AND package_type = ? AND source = ?",
			dim.Version, dim.Platform, dim.Arch, dim.Channel, dim.PackageType, dim.Source).
		Select("download_count").
		Scan(&downloadCount).Error
	if err != nil {
		return 0, err
	}
	return downloadCount, nil
}

// GetDesktopPackageDownloadTotal returns all desktop installer download counts.
func GetDesktopPackageDownloadTotal(ctx context.Context, db *gorm.DB) (int64, error) {
	var total int64
	query := db.WithContext(ctx).Model(&types.DesktopPackageDownloadStat{})
	err := query.Select("COALESCE(SUM(download_count), 0)").Scan(&total).Error
	return total, err
}
