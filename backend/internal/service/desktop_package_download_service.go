package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/pkg/utils"
)

const desktopPackageDownloadWindow = time.Minute

var _ contract.DesktopPackageDownloadService = (*desktopPackageDownloadService)(nil)

type desktopPackageDownloadService struct {
	db *gorm.DB
}

// NewDesktopPackageDownloadService creates a desktop installer download stat service.
func NewDesktopPackageDownloadService(db *gorm.DB) contract.DesktopPackageDownloadService {
	return &desktopPackageDownloadService{db: db}
}

func (s *desktopPackageDownloadService) ReportDesktopPackageDownload(
	ctx context.Context,
	req *contract.ReportDesktopPackageDownloadRequest,
) (*contract.ReportDesktopPackageDownloadResponse, error) {
	if req == nil {
		return nil, errors.New("request is required")
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		return nil, errors.New("version is required")
	}
	clientIP := strings.TrimSpace(req.ClientIP)
	if clientIP == "" {
		return nil, errors.New("client ip is required")
	}

	now := time.Now()
	dim := db.DesktopPackageDownloadDimension{
		Version:     version,
		Platform:    normalizeDesktopDownloadField(req.Platform, "unknown"),
		Arch:        normalizeDesktopDownloadField(req.Arch, "unknown"),
		Channel:     normalizeDesktopDownloadField(req.Channel, "stable"),
		PackageType: normalizeDesktopDownloadField(req.PackageType, ""),
		Source:      normalizeDesktopDownloadField(req.Source, ""),
	}

	var counted bool
	var total int64
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claimed, err := db.ClaimDesktopPackageDownloadWindow(
			ctx,
			tx,
			hashDesktopDownloadIP(clientIP),
			dim,
			now,
			desktopPackageDownloadWindow,
		)
		if err != nil {
			return err
		}
		counted = claimed
		if counted {
			if _, err := db.IncrementDesktopPackageDownload(ctx, tx, dim, now); err != nil {
				return err
			}
		}

		total, err = db.GetDesktopPackageDownloadTotal(ctx, tx)
		return err
	}); err != nil {
		return nil, err
	}

	return &contract.ReportDesktopPackageDownloadResponse{
		DownloadCount: total,
		Counted:       counted,
	}, nil
}

func (s *desktopPackageDownloadService) GetDesktopPackageDownloadTotal(
	ctx context.Context,
) (*contract.GetDesktopPackageDownloadTotalResponse, error) {
	total, err := db.GetDesktopPackageDownloadTotal(ctx, s.db)
	if err != nil {
		return nil, err
	}
	return &contract.GetDesktopPackageDownloadTotalResponse{DownloadCount: total}, nil
}

func normalizeDesktopDownloadField(value string, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return utils.DefaultString(value, fallback)
}

func hashDesktopDownloadIP(ip string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(ip)))
	return hex.EncodeToString(sum[:])
}
