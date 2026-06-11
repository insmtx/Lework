package service

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"
)

type fileService struct {
	db *gorm.DB
}

var _ contract.FileService = (*fileService)(nil)

func NewFileService(db *gorm.DB) contract.FileService {
	return &fileService{db: db}
}

const maxUploadSize = 100 << 20 // 100MB

func (s *fileService) UploadFile(ctx context.Context, req *contract.UploadFileRequest) (*contract.UploadFileResult, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(io.LimitReader(req.File, maxUploadSize+1))
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if int64(len(data)) > maxUploadSize {
		return nil, fmt.Errorf("file size exceeds maximum allowed size of %dMB", maxUploadSize/(1<<20))
	}

	detectedMime := http.DetectContentType(data[:min(len(data), 512)])
	mimeType := req.MimeType
	if mediaType, _, err := mime.ParseMediaType(detectedMime); err == nil {
		mimeType = mediaType
	}

	ext := ""
	if idx := strings.LastIndex(req.Filename, "."); idx >= 0 {
		ext = req.Filename[idx:]
	}
	storeFilename := fmt.Sprintf("%s%s", snowflake.GenerateIDBase58(), ext)
	key := fmt.Sprintf("%s/%d/%s", req.Purpose, caller.OrgID, storeFilename)

	file, err := filestore.Upload(ctx, s.db, filestore.UploadParams{
		Data:         data,
		Filename:     storeFilename,
		OriginalName: req.Filename,
		MimeType:     mimeType,
		OrgID:        caller.OrgID,
		OwnerID:      caller.Uin,
		ObjectKey:    key,
		Purpose:      req.Purpose,
	})
	if err != nil {
		return nil, fmt.Errorf("upload file: %w", err)
	}

	return &contract.UploadFileResult{
		PublicID:     file.PublicID,
		FileUploadID: file.PublicID,
		Filename:     file.Filename,
		OriginalName: file.OriginalName,
		MimeType:     file.MimeType,
		FileSize:     file.FileSize,
		Sha256:       file.Sha256,
		StoragePath:  file.StoragePath,
		URL:          file.StoragePath,
	}, nil
}

func (s *fileService) GetFileDownloadURL(ctx context.Context, orgID uint, fileID string) (*contract.FileDownloadURL, error) {
	ttl := 30 * time.Minute
	url, fileUpload, err := filestore.PresignDownloadByPublicID(ctx, s.db, orgID, fileID, ttl)
	if err != nil {
		logs.ErrorContextf(ctx, "presign url failed: %v", err)
		return nil, fmt.Errorf("get download url failed")
	}

	return &contract.FileDownloadURL{
		URL:       url,
		Filename:  fileUpload.OriginalName,
		MimeType:  fileUpload.MimeType,
		FileSize:  fileUpload.FileSize,
		ExpiresAt: time.Now().Add(ttl).Unix(),
	}, nil
}
