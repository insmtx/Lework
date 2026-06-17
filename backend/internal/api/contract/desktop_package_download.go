package contract

import "context"

// DesktopPackageDownloadService defines desktop installer download stat APIs.
type DesktopPackageDownloadService interface {
	ReportDesktopPackageDownload(ctx context.Context, req *ReportDesktopPackageDownloadRequest) (*ReportDesktopPackageDownloadResponse, error)
	GetDesktopPackageDownloadTotal(ctx context.Context) (*GetDesktopPackageDownloadTotalResponse, error)
}

// ReportDesktopPackageDownloadRequest is the anonymous desktop installer download report request.
type ReportDesktopPackageDownloadRequest struct {
	Version     string `json:"version" binding:"required"`
	Platform    string `json:"platform,omitempty"`
	Arch        string `json:"arch,omitempty"`
	Channel     string `json:"channel,omitempty"`
	PackageType string `json:"package_type,omitempty"`
	Source      string `json:"source,omitempty"`

	ClientIP string `json:"-"`
}

// ReportDesktopPackageDownloadResponse describes whether the report increased the count.
type ReportDesktopPackageDownloadResponse struct {
	DownloadCount int64 `json:"download_count"`
	Counted       bool  `json:"counted"`
}

// GetDesktopPackageDownloadTotalResponse is the global desktop installer download total.
type GetDesktopPackageDownloadTotalResponse struct {
	DownloadCount int64 `json:"download_count"`
}
