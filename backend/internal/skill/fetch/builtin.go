package fetch

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	catalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
)

// BuiltinSource 通过 HTTP API 从 Leros 服务端获取内置 Skill。
type BuiltinSource struct {
	serverAddr string
	httpClient *http.Client
}

// NewBuiltinSource 创建 BuiltinSource。
func NewBuiltinSource(serverAddr string) *BuiltinSource {
	return &BuiltinSource{
		serverAddr: serverAddr,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// SourceID 返回源标识。
func (s *BuiltinSource) SourceID() string {
	return "Leros"
}

// CanHandle 处理不含 "/"、"://" 和 ":" 的短名称/skill_id。
// ":" 用于标识外部源（如 clawhub:<slug>@<version>）。
func (s *BuiltinSource) CanHandle(identifier string) bool {
	return !strings.Contains(identifier, "/") && !strings.Contains(identifier, "://") && !strings.Contains(identifier, ":")
}

// Search 调用服务端 marketplace 搜索接口。
func (s *BuiltinSource) Search(ctx context.Context, query string, limit int) ([]SkillMeta, error) {
	u := fmt.Sprintf("http://%s/v1/skill-marketplace/search?source_types=Leros&keyword=%s&limit=%d",
		s.serverAddr, url.QueryEscape(query), limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("builtin search: create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("builtin search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("builtin search: server returned status %d", resp.StatusCode)
	}

	var apiResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Items []struct {
				SourceType  string   `json:"source_type"`
				SkillID     string   `json:"skill_id"`
				Name        string   `json:"name"`
				Description string   `json:"description"`
				Version     string   `json:"version"`
				Author      string   `json:"author"`
				Category    string   `json:"category"`
				Tags        []string `json:"tags"`
				Icon        string   `json:"icon,omitempty"`
				Installs    int64    `json:"installs"`
			} `json:"items"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("builtin search: decode response: %w", err)
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("builtin search: server error %d: %s", apiResp.Code, apiResp.Message)
	}

	results := make([]SkillMeta, 0, len(apiResp.Data.Items))
	for _, item := range apiResp.Data.Items {
		results = append(results, SkillMeta{
			SkillID:     item.SkillID,
			Name:        item.Name,
			Identifier:  item.SkillID,
			Source:      s.SourceID(),
			TrustLevel:  "trusted",
			Description: item.Description,
			Version:     item.Version,
			Author:      item.Author,
			Category:    item.Category,
			Tags:        item.Tags,
			Icon:        item.Icon,
			Installs:    item.Installs,
		})
	}
	return results, nil
}

// Fetch 调用服务端下载接口获取完整 Skill 包。
func (s *BuiltinSource) Fetch(ctx context.Context, identifier string) (*SkillBundle, error) {
	u := fmt.Sprintf("http://%s/v1/skill-marketplace/skills/%s/download",
		s.serverAddr, url.PathEscape(identifier))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("builtin fetch: create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("builtin fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("builtin skill %q not found", identifier)
		}
		return nil, fmt.Errorf("builtin fetch: server returned status %d", resp.StatusCode)
	}

	zipBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("builtin fetch: read zip: %w", err)
	}

	return s.extractBuiltinZip(zipBytes, identifier)
}

// Inspect 通过搜索 API 查找指定 skill_id 的元数据。
func (s *BuiltinSource) Inspect(ctx context.Context, identifier string) (*SkillMeta, error) {
	results, err := s.Search(ctx, identifier, 5)
	if err != nil {
		return nil, fmt.Errorf("builtin inspect: %w", err)
	}
	for i := range results {
		if results[i].SkillID == identifier {
			return &results[i], nil
		}
	}
	return nil, fmt.Errorf("builtin skill %q not found", identifier)
}

// FetchVersion 实现 fetch.VersionedSource。
// 下载内置 Skill 的指定版本（通过查询参数传递），服务端目前可能忽略版本参数。
func (s *BuiltinSource) FetchVersion(ctx context.Context, identifier, version string) (*SkillBundle, error) {
	u := fmt.Sprintf("http://%s/v1/skill-marketplace/skills/%s/download",
		s.serverAddr, url.PathEscape(identifier))
	if version != "" {
		u += "?version=" + url.QueryEscape(version)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("builtin fetch: create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("builtin fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("builtin skill %q version %q not found", identifier, version)
		}
		return nil, fmt.Errorf("builtin fetch: server returned status %d", resp.StatusCode)
	}

	zipBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("builtin fetch: read zip: %w", err)
	}

	return s.extractBuiltinZip(zipBytes, identifier)
}

// extractBuiltinZip 解压服务端返回的 ZIP（无根前缀，文件直接在 ZIP 根目录）。
func (s *BuiltinSource) extractBuiltinZip(zipBytes []byte, skillID string) (*SkillBundle, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "leros-builtin-skill-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}

		destPath := filepath.Join(tmpDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(destPath), filepath.Clean(tmpDir)+string(filepath.Separator)) {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("create dir: %w", err)
		}

		rc, err := f.Open()
		if err != nil {
			os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("open zip entry: %w", err)
		}

		out, err := os.Create(destPath)
		if err != nil {
			rc.Close()
			os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("create file: %w", err)
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("extract file: %w", err)
		}
	}

	// 找到 SKILL.md。
	skillMDPath := filepath.Join(tmpDir, "SKILL.md")
	skillDir := tmpDir
	if _, err := os.Stat(skillMDPath); os.IsNotExist(err) {
		found := false
		filepath.Walk(tmpDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || found {
				return nil
			}
			if !info.IsDir() && info.Name() == "SKILL.md" {
				skillMDPath = path
				skillDir = filepath.Dir(path)
				found = true
				return filepath.SkipAll
			}
			return nil
		})
		if !found {
			os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("SKILL.md not found in builtin skill %q", skillID)
		}
	}

	content, err := os.ReadFile(skillMDPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("read SKILL.md: %w", err)
	}

	manifest, _, err := catalog.ParseDocument(content)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("parse SKILL.md: %w", err)
	}

	// 收集附属文件。
	files := make(map[string][]byte)
	filepath.Walk(skillDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info.IsDir() {
			// Skip nested skill directories (subdirectories containing their own SKILL.md).
			if path != skillDir {
				if _, statErr := os.Stat(filepath.Join(path, "SKILL.md")); statErr == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, _ := filepath.Rel(skillDir, path)
		if rel == "SKILL.md" || IsMacOSJunkPath(rel) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr == nil && len(data) <= 1_048_576 {
			files[filepath.ToSlash(rel)] = data
		}
		return nil
	})

	return &SkillBundle{
		Meta: SkillMeta{
			SkillID:     skillID,
			Name:        manifest.Name,
			Identifier:  skillID,
			Source:      s.SourceID(),
			TrustLevel:  "trusted",
			Description: manifest.Description,
			Version:     manifest.Version,
			Category:    manifest.Metadata.Category,
			Tags:        manifest.Metadata.Tags,
		},
		Content: content,
		Files:   files,
		TempDir: tmpDir,
	}, nil
}
