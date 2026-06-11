package contract

import (
	"context"
	"io"
)

type FileService interface {
	UploadFile(ctx context.Context, req *UploadFileRequest) (*UploadFileResult, error)
	DownloadFile(ctx context.Context, orgID uint, fileID string) (io.ReadCloser, *FileDownloadInfo, error)
}

type UploadFileRequest struct {
	OrgID    uint
	OwnerID  uint
	File     io.Reader
	Filename string
	FileSize int64
	MimeType string
	Purpose  string
}

type UploadFileResult struct {
	PublicID     string `json:"public_id"`
	FileUploadID string `json:"file_upload_id"`
	Filename     string `json:"filename"`
	OriginalName string `json:"original_name"`
	MimeType     string `json:"mime_type"`
	FileSize     int64  `json:"file_size"`
	Sha256       string `json:"sha256"`
	StoragePath  string `json:"storage_path"`
	URL          string `json:"url"`
}

type FileDownloadInfo struct {
	FileName  string
	MimeType  string
	Size      int64
	PublicURL string
}

type FileObjectInfo struct {
	Key          string `json:"key"`
	Filename     string `json:"filename"`
	OriginalName string `json:"original_name"`
	MimeType     string `json:"mime_type"`
	FileSize     int64  `json:"file_size"`
	PublicID     string `json:"public_id"`
	ModTime      int64  `json:"mod_time,omitempty"`
	URL          string `json:"url,omitempty"`
}
