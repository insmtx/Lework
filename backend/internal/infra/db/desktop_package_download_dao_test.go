package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func setupDesktopPackageDownloadTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := database.AutoMigrate(
		&types.DesktopPackageDownloadStat{},
		&types.DesktopPackageDownloadRateLimit{},
	); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return database
}

func TestDesktopPackageDownloadClaimWindowAndTotal(t *testing.T) {
	database := setupDesktopPackageDownloadTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	dim := DesktopPackageDownloadDimension{
		Version:     "0.1.2",
		Platform:    "darwin",
		Arch:        "arm64",
		Channel:     "stable",
		PackageType: "dmg",
		Source:      "website",
	}

	claimed, err := ClaimDesktopPackageDownloadWindow(ctx, database, "ip_hash", dim, now, time.Minute)
	if err != nil {
		t.Fatalf("claim first window: %v", err)
	}
	if !claimed {
		t.Fatal("expected first report to be counted")
	}
	if _, err := IncrementDesktopPackageDownload(ctx, database, dim, now); err != nil {
		t.Fatalf("increment first report: %v", err)
	}

	claimed, err = ClaimDesktopPackageDownloadWindow(ctx, database, "ip_hash", dim, now.Add(30*time.Second), time.Minute)
	if err != nil {
		t.Fatalf("claim duplicate window: %v", err)
	}
	if claimed {
		t.Fatal("expected duplicate report in the same window to be ignored")
	}

	claimed, err = ClaimDesktopPackageDownloadWindow(ctx, database, "ip_hash", dim, now.Add(61*time.Second), time.Minute)
	if err != nil {
		t.Fatalf("claim next window: %v", err)
	}
	if !claimed {
		t.Fatal("expected report after the window to be counted")
	}
	if _, err := IncrementDesktopPackageDownload(ctx, database, dim, now.Add(61*time.Second)); err != nil {
		t.Fatalf("increment next report: %v", err)
	}

	total, err := GetDesktopPackageDownloadTotal(ctx, database)
	if err != nil {
		t.Fatalf("get total: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
}
