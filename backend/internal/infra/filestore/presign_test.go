package filestore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/ygpkg/storage-go/driver/local"

	"github.com/insmtx/Leros/backend/config"
)

func initTestStorage(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.StorageConfig{
		Driver:     "local",
		LocalDir:   dir,
		Bucket:     "test-bucket",
		BaseURL:    "http://127.0.0.1:8080/storage",
		SignSecret: "test-sign-secret",
	}
	if err := Init(cfg); err != nil {
		t.Fatalf("init storage: %v", err)
	}
}

func TestPresignUpload(t *testing.T) {
	initTestStorage(t)
	ctx := context.Background()

	url, expiresAt, err := PresignUpload(ctx, DefaultBucket(), "test-upload.txt")
	if err != nil {
		t.Fatalf("PresignUpload: %v", err)
	}
	if url == "" {
		t.Error("expected non-empty URL")
	}
	if expiresAt.IsZero() {
		t.Error("expected non-zero expiresAt")
	}
}

func TestPresignDownload(t *testing.T) {
	initTestStorage(t)
	ctx := context.Background()

	url, expiresAt, err := PresignDownload(ctx, DefaultBucket(), "test-download.txt")
	if err != nil {
		t.Fatalf("PresignDownload: %v", err)
	}
	if url == "" {
		t.Error("expected non-empty URL")
	}
	if expiresAt.IsZero() {
		t.Error("expected non-zero expiresAt")
	}
}

func TestIsLocal(t *testing.T) {
	initTestStorage(t)
	if !IsLocal() {
		t.Error("expected IsLocal() to return true for local driver")
	}
}

func TestPresignWithCustomBucket(t *testing.T) {
	initTestStorage(t)
	ctx := context.Background()

	url, _, err := PresignUpload(ctx, "custom-bucket", "test.txt")
	if err != nil {
		t.Fatalf("PresignUpload with custom bucket: %v", err)
	}
	if url == "" {
		t.Error("expected non-empty URL")
	}
}

func TestPresignUploadKeyWithSpecialChars(t *testing.T) {
	initTestStorage(t)
	ctx := context.Background()

	key := filepath.Join("path", "to", "file with spaces.txt")
	url, _, err := PresignUpload(ctx, DefaultBucket(), key)
	if err != nil {
		t.Fatalf("PresignUpload special chars: %v", err)
	}
	if !strings.Contains(url, "file%20with%20spaces.txt") {
		t.Errorf("URL does not contain encoded spaces: %s", url)
	}
}
