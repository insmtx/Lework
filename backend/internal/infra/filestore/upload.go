package filestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ygpkg/storage-go"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

const (
	PurposeAttachment   = "attachment"
	PurposeAvatar       = "avatar"
	PurposeArtifact     = "artifact"
	PurposeProjects     = "projects"
	PurposePlan         = "plan"
	PurposeSkillPackage = "skill_package"
)

// GenerateFilePublicID generates a unique public ID for a FileUpload record.
func GenerateFilePublicID() string {
	return fmt.Sprintf("file_%s", snowflake.GenerateIDBase58())
}

// UploadParams 文件上传参数
type UploadParams struct {
	Data         []byte
	Filename     string
	OriginalName string
	MimeType     string
	OwnerScope   types.OwnerScope
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
	params.OwnerScope = types.NormalizeOwnerScope(params.OwnerScope)
	if err := validateOwner(params.OwnerScope, params.OrgID, params.OwnerID); err != nil {
		return nil, err
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

	publicID := GenerateFilePublicID()
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
		OwnerScope:   params.OwnerScope,
		OrgID:        params.OrgID,
		OwnerID:      params.OwnerID,
		Filename:     params.Filename,
		OriginalName: originalName,
		MimeType:     params.MimeType,
		FileSize:     fileSize,
		StorageURI:   putResult.Path.URI(),
		Sha256:       sha256Hex,
		Purpose:      params.Purpose,
		Status:       "active",
		Metadata: types.ObjectMetadata{
			Extra: params.Metadata,
		},
	}

	if err := infradb.CreateFileUpload(ctx, db, fileUpload); err != nil {
		// 对象已写入存储但记录创建失败，尽力清理，避免残留孤儿对象。
		if derr := st.DeleteObject(ctx, DefaultBucket(), params.ObjectKey); derr != nil {
			return nil, fmt.Errorf("create file upload record: %w (cleanup object failed: %v)", err, derr)
		}
		return nil, fmt.Errorf("create file upload record: %w", err)
	}
	return fileUpload, nil
}

// ErrUploadTooLarge 表示上传文件超过允许的最大大小。
var ErrUploadTooLarge = errors.New("file size exceeds maximum allowed size")

// ErrEmptyFile 表示上传了空文件。
var ErrEmptyFile = errors.New("empty file is not allowed")

// UploadStreamParams 流式上传参数。
type UploadStreamParams struct {
	Filename     string
	OriginalName string
	MimeType     string
	OwnerScope   types.OwnerScope
	OrgID        uint
	OwnerID      uint
	ObjectKey    string
	Purpose      string
	Metadata     map[string]interface{}
}

// UploadStream 以流式方式写入 filestore 并同时计算 sha256，避免将整个文件读入内存。
// 数据经固定大小的缓冲从 reader 直接流向对象存储；当写入字节数超过 maxSize 或
// 文件为空时，会清理已写入对象并返回 ErrUploadTooLarge / ErrEmptyFile。
func UploadStream(ctx context.Context, db *gorm.DB, params UploadStreamParams, reader io.Reader, maxSize int64) (*types.FileUpload, error) {
	if params.Filename == "" {
		return nil, fmt.Errorf("filename is required")
	}
	if params.MimeType == "" {
		return nil, fmt.Errorf("mime type is required")
	}
	if params.ObjectKey == "" {
		return nil, fmt.Errorf("object key is required")
	}
	if maxSize <= 0 {
		return nil, fmt.Errorf("max size must be positive")
	}
	params.OwnerScope = types.NormalizeOwnerScope(params.OwnerScope)
	if err := validateOwner(params.OwnerScope, params.OrgID, params.OwnerID); err != nil {
		return nil, err
	}

	counting := &countingReader{r: reader}
	hasher := sha256.New()
	st := GetStorage()
	putResult, err := st.PutObject(ctx, DefaultBucket(), params.ObjectKey,
		io.TeeReader(io.LimitReader(counting, maxSize+1), hasher),
		storage.WithContentType(params.MimeType),
	)
	if err != nil {
		return nil, fmt.Errorf("put object: %w", err)
	}
	if counting.n > maxSize {
		_ = st.DeleteObject(ctx, DefaultBucket(), params.ObjectKey)
		return nil, ErrUploadTooLarge
	}
	if counting.n == 0 {
		_ = st.DeleteObject(ctx, DefaultBucket(), params.ObjectKey)
		return nil, ErrEmptyFile
	}

	publicID := GenerateFilePublicID()
	originalName := params.OriginalName
	if originalName == "" {
		originalName = params.Filename
	}
	fileUpload := &types.FileUpload{
		PublicID:     publicID,
		OwnerScope:   params.OwnerScope,
		OrgID:        params.OrgID,
		OwnerID:      params.OwnerID,
		Filename:     params.Filename,
		OriginalName: originalName,
		MimeType:     params.MimeType,
		FileSize:     counting.n,
		StorageURI:   putResult.Path.URI(),
		Sha256:       hex.EncodeToString(hasher.Sum(nil)),
		Purpose:      params.Purpose,
		Status:       "active",
		Metadata: types.ObjectMetadata{
			Extra: params.Metadata,
		},
	}

	if err := infradb.CreateFileUpload(ctx, db, fileUpload); err != nil {
		// 对象已写入存储但记录创建失败，尽力清理，避免残留孤儿对象。
		if derr := st.DeleteObject(ctx, DefaultBucket(), params.ObjectKey); derr != nil {
			return nil, fmt.Errorf("create file upload record: %w (cleanup object failed: %v)", err, derr)
		}
		return nil, fmt.Errorf("create file upload record: %w", err)
	}
	return fileUpload, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
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
	reader, err := OpenFileUpload(ctx, fileUpload)
	if err != nil {
		return nil, nil, err
	}
	return reader, fileUpload, nil
}

// OpenFileUpload opens an already-authorized FileUpload record.
func OpenFileUpload(ctx context.Context, fileUpload *types.FileUpload) (io.ReadCloser, error) {
	if fileUpload == nil {
		return nil, fmt.Errorf("file upload record is required")
	}
	objectKey, err := storageKeyFromURI(fileUpload.StorageURI)
	if err != nil {
		return nil, fmt.Errorf("parse storage path: %w", err)
	}

	st := GetStorage()
	result, err := st.GetObject(ctx, DefaultBucket(), objectKey)
	if err != nil {
		return nil, fmt.Errorf("get object: %w", err)
	}
	return result.Body, nil
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

	url, err := PresignDownloadForFileUpload(ctx, fileUpload, ttl)
	if err != nil {
		return "", nil, err
	}
	return url, fileUpload, nil
}

// PresignDownloadForFileUpload signs a previously authorized file record without reapplying ownership rules.
func PresignDownloadForFileUpload(ctx context.Context, fileUpload *types.FileUpload, ttl time.Duration) (string, error) {
	if fileUpload == nil {
		return "", fmt.Errorf("file upload record is required")
	}
	_, bucket, objectKey, err := storage.ParseURI(fileUpload.StorageURI)
	if err != nil {
		return "", fmt.Errorf("parse storage path: %w", err)
	}
	st := GetStorage()
	url, err := st.PresignGetObject(ctx, bucket, objectKey, ttl)
	if err != nil {
		return "", fmt.Errorf("presign url: %w", err)
	}
	return url, nil
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
	OwnerScope   types.OwnerScope
	OrgID        uint
	OwnerID      uint
	FileSize     int64
	Sha256       string
	Purpose      string
	Metadata     map[string]interface{}
	PublicID     string // 可选，指定 FileUpload 的 PublicID；为空时自动生成
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
	params.OwnerScope = types.NormalizeOwnerScope(params.OwnerScope)
	if err := validateOwner(params.OwnerScope, params.OrgID, params.OwnerID); err != nil {
		return nil, err
	}

	st := GetStorage()
	pb := st.PathBuilder()
	storagePath := pb.Build(DefaultBucket(), storageKeyFromStorageURI(params.StorageURI))
	normalizedURI := storagePath.URI()

	publicID := strings.TrimSpace(params.PublicID)
	if publicID == "" {
		publicID = GenerateFilePublicID()
	}
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
		OwnerScope:   params.OwnerScope,
		OrgID:        params.OrgID,
		OwnerID:      params.OwnerID,
		Filename:     params.Filename,
		OriginalName: originalName,
		MimeType:     params.MimeType,
		FileSize:     fileSize,
		StorageURI:   normalizedURI,
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

func validateOwner(scope types.OwnerScope, orgID, ownerID uint) error {
	if !types.ValidateOwnerScope(scope, orgID) {
		return fmt.Errorf("invalid owner scope %q for org_id %d", scope, orgID)
	}
	switch scope {
	case types.OwnerScopeOrganization:
		if ownerID == 0 {
			return fmt.Errorf("organization owner is required")
		}
	case types.OwnerScopeSystem:
		if ownerID != 0 {
			return fmt.Errorf("system owner_id must be zero")
		}
	}
	return nil
}

func storageKeyFromStorageURI(uri string) string {
	_, _, key, err := storage.ParseURI(uri)
	if err != nil {
		return ""
	}
	return key
}
