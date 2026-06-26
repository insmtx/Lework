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
	Size         int64
	Metadata     map[string]interface{}
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

	publicID := fmt.Sprintf("file_%s", snowflake.GenerateIDBase58())
	originalName := params.OriginalName
	if originalName == "" {
		originalName = params.Filename
	}
	fileSize := params.Size
	if fileSize <= 0 {
		fileSize = int64(len(params.Data))
	}
	fileUpload := &types.FileUpload{
		PublicID:     publicID,
		OrgID:        params.OrgID,
		OwnerID:      params.OwnerID,
		Filename:     params.Filename,
		OriginalName: originalName,
		MimeType:     params.MimeType,
		FileSize:     fileSize,
		StorageURI:  putResult.Path.URI(),
		Sha256:       sha256Hex,
		Purpose:      params.Purpose,
		Status:       "active",
		Metadata: types.ObjectMetadata{
			Extra: params.Metadata,
		},
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

	objectKey, err := storageKeyFromURI(fileUpload.StorageURI)
	if err != nil {
		return nil, nil, fmt.Errorf("parse storage path: %w", err)
	}

	st := GetStorage()
	result, err := st.GetObject(ctx, DefaultBucket(), objectKey)
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

	objectKey, err := storageKeyFromURI(fileUpload.StorageURI)
	if err != nil {
		return "", nil, fmt.Errorf("parse storage path: %w", err)
	}

	st := GetStorage()
	url, err := st.PresignGetObject(ctx, DefaultBucket(), objectKey, ttl)
	if err != nil {
		return "", nil, fmt.Errorf("presign url: %w", err)
	}
	return url, fileUpload, nil
}

func ResolvePublicURL(ctx context.Context, storagePath string) (string, error) {
	_, bucket, key, err := storage.ParseURI(storagePath)
	if err != nil {
		return "", fmt.Errorf("parse storage uri: %w", err)
	}

	st := GetStorage()
	info, err := st.HeadObject(ctx, bucket, key)
	if err != nil {
		return "", fmt.Errorf("head object %s/%s: %w", bucket, key, err)
	}
	return info.Path.PublicURL(), nil
}

func storageKeyFromURI(uri string) (string, error) {
	_, _, key, err := storage.ParseURI(uri)
	if err != nil {
		return "", fmt.Errorf("invalid storage uri %q: %w", uri, err)
	}
	return key, nil
}

// ParseStorageURI 解析 filestore URI，返回 bucket 和 key。
func ParseStorageURI(uri string) (string, string, error) {
	_, bucket, key, err := storage.ParseURI(uri)
	if err != nil {
		return "", "", fmt.Errorf("parse storage uri: %w", err)
	}
	return bucket, key, nil
}

// RecordUploadParams 记录已上传文件的元数据参数（不上传文件本身）
type RecordUploadParams struct {
	StorageURI   string
	Filename     string
	OriginalName string
	MimeType     string
	OrgID        uint
	OwnerID      uint
	FileSize     int64
	Sha256       string
	Purpose      string
	Metadata     map[string]interface{}
}

// RecordUpload 仅创建 FileUpload 记录，不上传文件。
// 用于 Worker 已通过预签名 URL 完成上传后的元数据记录。
func RecordUpload(ctx context.Context, db *gorm.DB, params RecordUploadParams) (*types.FileUpload, error) {
	if params.StorageURI == "" {
		return nil, fmt.Errorf("storage uri is required")
	}
	if params.Filename == "" {
		return nil, fmt.Errorf("filename is required")
	}
	if params.OrgID == 0 || params.OwnerID == 0 {
		return nil, fmt.Errorf("org and owner are required")
	}

	st := GetStorage()
	pb := st.PathBuilder()
	storagePath := pb.Build(DefaultBucket(), storageKeyFromStorageURI(params.StorageURI))
	normalizedURI := storagePath.URI()

	publicID := fmt.Sprintf("file_%s", snowflake.GenerateIDBase58())
	originalName := params.OriginalName
	if originalName == "" {
		originalName = params.Filename
	}
	fileSize := params.FileSize
	if fileSize <= 0 {
		fileSize = 0
	}

	fileUpload := &types.FileUpload{
		PublicID:     publicID,
		OrgID:        params.OrgID,
		OwnerID:      params.OwnerID,
		Filename:     params.Filename,
		OriginalName: originalName,
		MimeType:     params.MimeType,
		FileSize:     fileSize,
		StorageURI:  normalizedURI,
		Sha256:       params.Sha256,
		Purpose:      params.Purpose,
		Status:       "active",
		Metadata: types.ObjectMetadata{
			Extra: params.Metadata,
		},
	}

	if err := infradb.CreateFileUpload(ctx, db, fileUpload); err != nil {
		return nil, fmt.Errorf("create file upload record: %w", err)
	}
	return fileUpload, nil
}

func storageKeyFromStorageURI(uri string) string {
	_, _, key, err := storage.ParseURI(uri)
	if err != nil {
		return ""
	}
	return key
}
