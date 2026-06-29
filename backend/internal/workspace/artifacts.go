package workspace

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/insmtx/Leros/backend/types"
)

// ManifestArtifact 表示 Agent 在一次 turn 中写入 JSON Lines manifest 的一条产物声明。
type ManifestArtifact struct {
	Path         string `json:"path"`
	Title        string `json:"title,omitempty"`
	Description  string `json:"description,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
	ArtifactType string `json:"artifact_type,omitempty"`
	IsFinal      bool   `json:"is_final,omitempty"`
}

// ArtifactRecord 是 manifest 产物声明经过校验后可持久化的结构。
type ArtifactRecord struct {
	Title        string
	Filename     string
	OriginalName string
	Description  string
	ArtifactType string
	RelativePath string
	StorageKey   string
	StorageURI   string
	MimeType     string
	FileSize     int64
	Sha256       string
	Source       string
	Status       string
}

// CollectFinalArtifacts 读取并校验最终产物 manifest。
func CollectFinalArtifacts(ctx context.Context, plan *TaskWorkspace) ([]ArtifactRecord, error) {
	if plan == nil || strings.TrimSpace(plan.ArtifactManifestPath) == "" {
		return nil, nil
	}
	file, err := os.Open(plan.ArtifactManifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open artifact manifest: %w", err)
	}
	defer file.Close()

	declared := make(map[string]ManifestArtifact)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item ManifestArtifact
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, fmt.Errorf("parse artifact manifest: %w", err)
		}
		if !item.IsFinal {
			continue
		}
		key := filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(item.Path))))
		if key == "." || strings.HasPrefix(key, "../") || strings.HasPrefix(key, "/") {
			return nil, fmt.Errorf("invalid artifact path %q", item.Path)
		}
		item.Path = key
		declared[key] = item
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read artifact manifest: %w", err)
	}
	if len(declared) == 0 {
		return nil, nil
	}

	paths := make([]string, 0, len(declared))
	for path := range declared {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	artifacts := make([]ArtifactRecord, 0, len(declared))
	for _, path := range paths {
		item := declared[path]
		artifact, err := BuildArtifactRecord(plan, item)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

// BuildArtifactRecord 校验一条产物声明，并生成可存储的产物记录。
func BuildArtifactRecord(plan *TaskWorkspace, item ManifestArtifact) (ArtifactRecord, error) {
	absolute, err := SafeJoin(plan.RepoDir, item.Path)
	if err != nil {
		return ArtifactRecord{}, fmt.Errorf("validate artifact path %q: %w", item.Path, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return ArtifactRecord{}, fmt.Errorf("stat artifact %q: %w", item.Path, err)
	}
	if info.IsDir() {
		return ArtifactRecord{}, fmt.Errorf("artifact %q is a directory", item.Path)
	}
	sha, err := sha256File(absolute)
	if err != nil {
		return ArtifactRecord{}, err
	}
	storageKey, err := plan.StorageKey(item.Path)
	if err != nil {
		return ArtifactRecord{}, err
	}
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = filepath.Base(item.Path)
	}
	artifactType := strings.TrimSpace(item.ArtifactType)
	if artifactType == "" {
		artifactType = string(types.ArtifactTypeFile)
	}
	return ArtifactRecord{
		Title:        title,
		Filename:     filepath.Base(item.Path),
		OriginalName: filepath.Base(item.Path),
		Description:  strings.TrimSpace(item.Description),
		ArtifactType: artifactType,
		RelativePath: item.Path,
		StorageKey:   storageKey,
		MimeType:     detectMimeType(absolute, item.MimeType),
		FileSize:     info.Size(),
		Sha256:       sha,
		Source:       string(types.ArtifactSourceAgentDeclared),
		Status:       string(types.ArtifactStatusCompleted),
	}, nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open artifact for hash: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash artifact: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func detectMimeType(path string, declared string) string {
	if strings.TrimSpace(declared) != "" {
		return normalizeMimeType(declared)
	}
	if ext := filepath.Ext(path); ext != "" {
		if value := mime.TypeByExtension(ext); value != "" {
			return normalizeMimeType(value)
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	var buf [512]byte
	n, err := file.Read(buf[:])
	if err != nil && err != io.EOF {
		return ""
	}
	return normalizeMimeType(http.DetectContentType(buf[:n]))
}
