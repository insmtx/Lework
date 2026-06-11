package filestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/ygpkg/storage-go"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

const (
	PurposeArtifact = "artifact"
)

// UploadParams 文件上传参数
type UploadParams struct {
	Data         []byte
	Filename     string
	OriginalName string
	MimeType     string
	OrgID        uint
	OwnerID      uint
	ObjectKey    string
	Purpose      string
}

// Upload 写入 filestore 并创建 FileUpload 记录
func Upload(ctx context.Context, db *gorm.DB, params UploadParams) (*types.FileUpload, error) {
	if len(params.Data) == 0 {
		return nil, fmt.Errorf("file data is required")
	}
	if params.Filename == "" {
		return nil, fmt.Errorf("filename is required")
	}
	if params.MimeType == "" {
		return nil, fmt.Errorf("mime type is required")
	}
	if params.OrgID == 0 || params.OwnerID == 0 {
		return nil, fmt.Errorf("org and owner are required")
	}
	if params.ObjectKey == "" {
		return nil, fmt.Errorf("object key is required")
	}

	hash := sha256.Sum256(params.Data)
	sha256Hex := hex.EncodeToString(hash[:])

	st := GetStorage()
	putResult, err := st.PutObject(ctx, DefaultBucket(), params.ObjectKey, bytes.NewReader(params.Data),
		storage.WithContentType(params.MimeType),
	)
	if err != nil {
		return nil, fmt.Errorf("put object: %w", err)
	}

	publicID := fmt.Sprintf("%s://%s", putResult.Path.Scheme(), snowflake.GenerateIDBase58())
	originalName := params.OriginalName
	if originalName == "" {
		originalName = params.Filename
	}
	fileUpload := &types.FileUpload{
		PublicID:     publicID,
		OrgID:        params.OrgID,
		OwnerID:      params.OwnerID,
		Filename:     params.Filename,
		OriginalName: originalName,
		MimeType:     params.MimeType,
		FileSize:     int64(len(params.Data)),
		StoragePath:  putResult.Path.Path(),
		Sha256:       sha256Hex,
		Purpose:      params.Purpose,
		Status:       "active",
	}

	if err := infradb.CreateFileUpload(ctx, db, fileUpload); err != nil {
		return nil, fmt.Errorf("create file upload record: %w", err)
	}
	return fileUpload, nil
}

// OpenFileByPublicID 通过 FileUpload.PublicID 从 filestore 打开文件流
func OpenFileByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string) (io.ReadCloser, *types.FileUpload, error) {
	fileUpload, err := infradb.GetFileUploadByPublicID(ctx, db, orgID, publicID)
	if err != nil {
		return nil, nil, err
	}
	if fileUpload == nil {
		return nil, nil, fmt.Errorf("file upload record not found")
	}

	st := GetStorage()
	result, err := st.GetObject(ctx, DefaultBucket(), fileUpload.StoragePath)
	if err != nil {
		return nil, nil, fmt.Errorf("get object: %w", err)
	}
	return result.Body, fileUpload, nil
}

// PresignDownloadByPublicID 通过 FileUpload.PublicID 生成预签名下载 URL
func PresignDownloadByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string, ttl time.Duration) (string, *types.FileUpload, error) {
	fileUpload, err := infradb.GetFileUploadByPublicID(ctx, db, orgID, publicID)
	if err != nil {
		return "", nil, err
	}
	if fileUpload == nil {
		return "", nil, fmt.Errorf("file upload record not found")
	}

	st := GetStorage()
	url, err := st.PresignGetObject(ctx, DefaultBucket(), fileUpload.StoragePath, ttl)
	if err != nil {
		return "", nil, fmt.Errorf("presign url: %w", err)
	}
	return url, fileUpload, nil
}
