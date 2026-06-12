package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/ygpkg/yg-go/logs"
)

type ArtifactStorageFile struct {
	Path     string
	Filename string
	MimeType string
	FileSize int64
	Sha256   string
	Data     []byte
}

func ResolveArtifactStorageFile(ctx context.Context, orgID uint, workerID uint, storageKey string, declaredMimeType string) (*ArtifactStorageFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	wsRoot, _ := leros.WorkspaceRoot()
	logs.InfoContextf(ctx, "ResolveArtifactStorageFile: LEROS_WORKSPACE_ROOT=%s storageKey=%s orgID=%d workerID=%d", wsRoot, storageKey, orgID, workerID)

	absolutePath, err := ArtifactStoragePath(orgID, workerID, storageKey)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact storage path: %w", err)
	}
	logs.InfoContextf(ctx, "ResolveArtifactStorageFile: resolved absolutePath=%s", absolutePath)

	info, err := os.Stat(absolutePath)
	if err != nil {
		if os.IsNotExist(err) {
			logs.WarnContextf(ctx, "ResolveArtifactStorageFile: file not found at absolutePath=%s storageKey=%s", absolutePath, storageKey)
			return nil, fmt.Errorf("artifact file not found: %s", storageKey)
		}
		return nil, fmt.Errorf("stat artifact file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("artifact path is a directory: %s", storageKey)
	}

	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("read artifact file: %w", err)
	}

	hash := sha256.Sum256(data)
	sha256Hex := hex.EncodeToString(hash[:])

	return &ArtifactStorageFile{
		Path:     absolutePath,
		Filename: filepath.Base(absolutePath),
		MimeType: detectMimeTypeFromKey(storageKey, declaredMimeType),
		FileSize: info.Size(),
		Sha256:   sha256Hex,
		Data:     data,
	}, nil
}

func OpenArtifactStorageFile(ctx context.Context, orgID uint, workerID uint, storageKey string) (io.ReadCloser, error) {
	absolutePath, err := ArtifactStoragePath(orgID, workerID, storageKey)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact storage path: %w", err)
	}
	file, err := os.Open(absolutePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("artifact file not found: %s", storageKey)
		}
		return nil, fmt.Errorf("open artifact file: %w", err)
	}
	return file, nil
}

func RepoRelativePathFromStorageKey(storageKey string) string {
	key := filepath.ToSlash(strings.TrimSpace(storageKey))
	const marker = "/repo/"
	idx := strings.Index(key, marker)
	if idx < 0 {
		return ""
	}
	return strings.TrimPrefix(key[idx+len(marker):], "/")
}

func detectMimeTypeFromKey(key, declared string) string {
	if strings.TrimSpace(declared) != "" {
		return normalizeMimeType(declared)
	}
	if ext := filepath.Ext(key); ext != "" {
		if value := mime.TypeByExtension(ext); value != "" {
			return normalizeMimeType(value)
		}
	}
	return ""
}

func normalizeMimeType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if mediaType, _, err := mime.ParseMediaType(value); err == nil {
		return mediaType
	}
	if index := strings.Index(value, ";"); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return value
}
