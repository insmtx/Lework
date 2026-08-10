package service

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupFileServiceTest(t *testing.T) (*gorm.DB, *fileService) {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&types.FileUpload{}); err != nil {
		t.Fatalf("migrate file upload: %v", err)
	}
	if err := filestore.Init(&config.StorageConfig{
		Driver:     "local",
		LocalDir:   t.TempDir(),
		Bucket:     "test-bucket",
		BaseURL:    "http://localhost:8080",
		SignSecret: "test-secret",
	}); err != nil {
		t.Fatalf("init filestore: %v", err)
	}
	return database, &fileService{db: database}
}

func uploadTestFile(t *testing.T, svc *fileService, scope types.OwnerScope, name string) *types.FileUpload {
	t.Helper()
	data := []byte("content of " + name)
	var orgID, ownerID uint
	if scope == types.OwnerScopeSystem {
		orgID, ownerID = 0, 0
	} else {
		orgID, ownerID = 7, 8
	}
	file, err := filestore.Upload(context.Background(), svc.db, filestore.UploadParams{
		Data:       data,
		Filename:   name + ".png",
		MimeType:   "image/png",
		OwnerScope: scope,
		OrgID:      orgID,
		OwnerID:    ownerID,
		ObjectKey:  name,
		Purpose:    filestore.PurposeAvatar,
		Size:       int64(len(data)),
	})
	if err != nil {
		t.Fatalf("upload %s file: %v", name, err)
	}
	return file
}

func TestDownloadFile_Organization(t *testing.T) {
	_, svc := setupFileServiceTest(t)
	file := uploadTestFile(t, svc, types.OwnerScopeOrganization, "org-avatar")

	reader, info, err := svc.DownloadFile(context.Background(), 7, file.PublicID)
	if err != nil {
		t.Fatalf("download organization file: %v", err)
	}
	defer reader.Close()
	if info.FileName == "" {
		t.Fatal("expected org download info")
	}
}

func TestDownloadFile_OrganizationWrongOrgDenied(t *testing.T) {
	_, svc := setupFileServiceTest(t)
	file := uploadTestFile(t, svc, types.OwnerScopeOrganization, "org-private")

	// 匿名（orgID=0）或非归属组织（orgID=9）都不能读取 organization 私有文件。
	if _, _, err := svc.DownloadFile(context.Background(), 0, file.PublicID); err == nil {
		t.Fatal("anonymous download of organization file should fail")
	}
	if _, _, err := svc.DownloadFile(context.Background(), 9, file.PublicID); err == nil {
		t.Fatal("download of organization file from wrong org should fail")
	}
}

func TestDownloadFile_SystemAnonymous(t *testing.T) {
	_, svc := setupFileServiceTest(t)
	file := uploadTestFile(t, svc, types.OwnerScopeSystem, "system-avatar")

	// 匿名（orgID=0）应能读取 system 内置资源。
	reader, info, err := svc.DownloadFile(context.Background(), 0, file.PublicID)
	if err != nil {
		t.Fatalf("anonymous download of system file: %v", err)
	}
	defer reader.Close()
	if info.FileName == "" {
		t.Fatal("expected system download info")
	}
}

func TestPresignDownloadURL_SystemAnonymous(t *testing.T) {
	_, svc := setupFileServiceTest(t)
	file := uploadTestFile(t, svc, types.OwnerScopeSystem, "system-avatar-presign")

	url, err := svc.PresignDownloadURL(context.Background(), 0, file.PublicID, "")
	if err != nil || url == "" {
		t.Fatalf("anonymous presign of system file: url=%q err=%v", url, err)
	}
}

func TestPresignDownloadURL_OrganizationWrongOrgDenied(t *testing.T) {
	_, svc := setupFileServiceTest(t)
	file := uploadTestFile(t, svc, types.OwnerScopeOrganization, "org-private-presign")

	if _, err := svc.PresignDownloadURL(context.Background(), 0, file.PublicID, ""); err == nil {
		t.Fatal("anonymous presign of organization file should fail")
	}
	if _, err := svc.PresignDownloadURL(context.Background(), 9, file.PublicID, ""); err == nil {
		t.Fatal("presign of organization file from wrong org should fail")
	}
}
