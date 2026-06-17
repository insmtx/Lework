package types

import (
	"time"

	"gorm.io/gorm"
)

// DesktopPackageDownloadStat stores aggregated desktop installer download counts.
type DesktopPackageDownloadStat struct {
	gorm.Model

	Version     string `gorm:"column:version;type:varchar(64);not null;uniqueIndex:idx_desktop_download_dim"`
	Platform    string `gorm:"column:platform;type:varchar(32);not null;uniqueIndex:idx_desktop_download_dim"`
	Arch        string `gorm:"column:arch;type:varchar(32);not null;uniqueIndex:idx_desktop_download_dim"`
	Channel     string `gorm:"column:channel;type:varchar(32);not null;default:'stable';uniqueIndex:idx_desktop_download_dim"`
	PackageType string `gorm:"column:package_type;type:varchar(32);not null;default:'';uniqueIndex:idx_desktop_download_dim"`
	Source      string `gorm:"column:source;type:varchar(64);not null;default:'';uniqueIndex:idx_desktop_download_dim"`

	DownloadCount    int64      `gorm:"column:download_count;type:bigint;not null;default:0"`
	LastDownloadedAt *time.Time `gorm:"column:last_downloaded_at"`
}

// TableName 指定 DesktopPackageDownloadStat 结构体对应的数据库表名。
func (DesktopPackageDownloadStat) TableName() string {
	return TableNameDesktopPackageDownloadStat
}

// DesktopPackageDownloadRateLimit stores recent IP report windows for anonymous download stats.
type DesktopPackageDownloadRateLimit struct {
	gorm.Model

	IPHash   string `gorm:"column:ip_hash;type:varchar(64);not null;uniqueIndex:idx_desktop_download_rate_limit"`
	Version  string `gorm:"column:version;type:varchar(64);not null;uniqueIndex:idx_desktop_download_rate_limit"`
	Platform string `gorm:"column:platform;type:varchar(32);not null;uniqueIndex:idx_desktop_download_rate_limit"`
	Arch     string `gorm:"column:arch;type:varchar(32);not null;uniqueIndex:idx_desktop_download_rate_limit"`
}

// TableName 指定 DesktopPackageDownloadRateLimit 结构体对应的数据库表名。
func (DesktopPackageDownloadRateLimit) TableName() string {
	return TableNameDesktopPackageDownloadRateLimit
}
