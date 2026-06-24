package fetch

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	catalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
)

// GitHubSource 通过下载 GitHub 仓库 ZIP 获取 Skill。
type GitHubSource struct {
	client *http.Client
}

// NewGitHubSource 创建 GitHubSource。
func NewGitHubSource() *GitHubSource {
	return &GitHubSource{
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// SourceID 返回源标识。
func (g *GitHubSource) SourceID() string {
	return "github"
}

// CanHandle 含 "/" 的非 URL 标识符可由 GitHubSource 处理。
func (g *GitHubSource) CanHandle(identifier string) bool {
	return strings.Count(identifier, "/") >= 1 && !strings.Contains(identifier, "://")
}

// Search GitHubSource 不支持搜索
func (g *GitHubSource) Search(ctx context.Context, query string, limit int) ([]SkillMeta, error) {
	return nil, nil
}

// Fetch 下载 GitHub 仓库 ZIP 并提取 Skill。
func (g *GitHubSource) Fetch(ctx context.Context, identifier string) (*SkillBundle, error) {
	parts := strings.SplitN(identifier, "/", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid GitHub identifier %q: expected owner/repo/path", identifier)
	}
	owner, repo, skillPath := parts[0], parts[1], parts[2]

	branches := []string{"main", "master"}
	var lastErr error
	for _, branch := range branches {
		bundle, err := g.fetchBranch(ctx, owner, repo, branch, skillPath)
		if err == nil {
			return bundle, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("download GitHub skill: %w", lastErr)
}

func (g *GitHubSource) fetchBranch(ctx context.Context, owner, repo, branch, skillPath string) (*SkillBundle, error) {
	return g.fetchRef(ctx, owner, repo, "heads", branch, skillPath)
}

func (g *GitHubSource) fetchTag(ctx context.Context, owner, repo, tag, skillPath string) (*SkillBundle, error) {
	return g.fetchRef(ctx, owner, repo, "tags", tag, skillPath)
}

func (g *GitHubSource) fetchRef(ctx context.Context, owner, repo, refType, ref, skillPath string) (*SkillBundle, error) {
	zipURL := fmt.Sprintf("https://github.com/%s/%s/archive/refs/%s/%s.zip", owner, repo, refType, ref)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, zipURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned status %d for %s %s", resp.StatusCode, strings.TrimSuffix(refType, "s"), ref)
	}

	zipBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}

	return g.extractSkill(zipBytes, owner, repo, skillPath)
}

func (g *GitHubSource) extractSkill(zipBytes []byte, owner, repo, skillPath string) (*SkillBundle, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "leros-skill-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	// 获取顶层目录前缀（如 repo-main），用于去除。
	var rootPrefix string
	for _, f := range reader.File {
		parts := strings.SplitN(f.Name, "/", 2)
		if parts[0] != "" {
			rootPrefix = parts[0] + "/"
			break
		}
	}
	if rootPrefix == "" {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("empty zip or unexpected structure")
	}

	// 解压所有文件。
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}

		relPath := strings.TrimPrefix(f.Name, rootPrefix)
		if relPath == f.Name || relPath == "" {
			continue
		}

		destPath := filepath.Join(tmpDir, relPath)
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

	// 在解压根目录下查找 SKILL.md。
	skillDir := filepath.Join(tmpDir, skillPath)
	skillMDPath := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(skillMDPath); os.IsNotExist(err) {
		// 回退：在解压根目录下递归查找 SKILL.md。
		found := false
		filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || found {
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
			return nil, fmt.Errorf("SKILL.md not found in %s/%s", owner, repo)
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
	allowedSubdirs := map[string]bool{"assets": true, "references": true, "scripts": true, "templates": true}
	for subdir := range allowedSubdirs {
		subPath := filepath.Join(skillDir, subdir)
		filepath.Walk(subPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(skillDir, path)
			data, err := os.ReadFile(path)
			if err == nil && len(data) <= 1_048_576 {
				files[filepath.ToSlash(rel)] = data
			}
			return nil
		})
	}

	trustLevel := TrustLevelForRepo(owner, repo)

	return &SkillBundle{
		Meta: SkillMeta{
			Name:        manifest.Name,
			Identifier:  owner + "/" + repo + "/" + skillPath,
			Source:      "github",
			TrustLevel:  trustLevel,
			Description: manifest.Description,
		},
		Content: content,
		Files:   files,
		TempDir: tmpDir,
	}, nil
}

// Inspect 获取 GitHub 上 Skill 的元数据（不下载附属文件）。
func (g *GitHubSource) Inspect(ctx context.Context, identifier string) (*SkillMeta, error) {
	bundle, err := g.Fetch(ctx, identifier)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(bundle.TempDir)
	meta := bundle.Meta
	return &meta, nil
}

// FetchVersion 实现 fetch.VersionedSource。
// 使用 version 作为分支/tag 名下载 GitHub 仓库（如 v1.2.3 或 main）。
func (g *GitHubSource) FetchVersion(ctx context.Context, identifier, version string) (*SkillBundle, error) {
	parts := strings.SplitN(identifier, "/", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid GitHub identifier %q: expected owner/repo/path", identifier)
	}
	owner, repo, skillPath := parts[0], parts[1], parts[2]

	if version == "" {
		return g.Fetch(ctx, identifier)
	}

	bundle, branchErr := g.fetchBranch(ctx, owner, repo, version, skillPath)
	if branchErr == nil {
		return bundle, nil
	}
	bundle, tagErr := g.fetchTag(ctx, owner, repo, version, skillPath)
	if tagErr == nil {
		return bundle, nil
	}
	return nil, fmt.Errorf("download GitHub skill ref %q: branch: %v; tag: %w", version, branchErr, tagErr)
}
