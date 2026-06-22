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

const (
	clawhubAPIBase = "https://clawhub.ai/api/v1"
)

// ClawHubSource 通过 ClawHub API 发现和下载 Skill。
type ClawHubSource struct {
	client *http.Client
}

// NewClawHubSource 创建 ClawHubSource。
func NewClawHubSource() *ClawHubSource {
	return &ClawHubSource{
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// SourceID 返回源标识。
func (c *ClawHubSource) SourceID() string {
	return "clawhub"
}

// CanHandle 处理纯 skill 名称（不含 / 和 https:// 前缀的标识符）。
func (c *ClawHubSource) CanHandle(identifier string) bool {
	return !strings.Contains(identifier, "/") && !strings.HasPrefix(identifier, "https://")
}

// Search 调用 ClawHub search 或 list API。
// 有关键词时使用 /api/v1/search?q=；无关键词时使用 /api/v1/skills?limit=。
func (c *ClawHubSource) Search(ctx context.Context, query string, limit int) ([]SkillMeta, error) {
	query = strings.TrimSpace(query)
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	var items []clawhubSkillItem
	var err error

	if query == "" || len([]rune(query)) < 2 {
		items, err = c.listSkills(ctx, limit)
	} else {
		items, err = c.searchSkills(ctx, query, limit)
	}
	if err != nil {
		return nil, err
	}

	results := make([]SkillMeta, 0, len(items))
	for _, item := range items {
		results = append(results, item.toSkillMeta())
	}
	return results, nil
}

// listSkills 调用 GET /api/v1/skills?limit= 获取默认列表。
func (c *ClawHubSource) listSkills(ctx context.Context, limit int) ([]clawhubSkillItem, error) {
	u := fmt.Sprintf("%s/skills?limit=%d&sort=trending", clawhubAPIBase, limit)
	return c.fetchSkillsList(ctx, u)
}

// searchSkills 调用 GET /api/v1/search?q= 搜索。
func (c *ClawHubSource) searchSkills(ctx context.Context, query string, limit int) ([]clawhubSkillItem, error) {
	u := fmt.Sprintf("%s/search?q=%s&limit=%d", clawhubAPIBase, url.QueryEscape(query), limit)
	return c.fetchSkillsList(ctx, u)
}

// fetchSkillsList 向给定 URL 发起请求并解析技能列表示响应。
func (c *ClawHubSource) fetchSkillsList(ctx context.Context, reqURL string) ([]clawhubSkillItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clawhub request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clawhub returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1_048_576))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return parseClawhubResponse(body)
}

// parseClawhubResponse 解析 ClawHub API 响应，支持 list(/skills) 和 search(/search) 两种结构。
func parseClawhubResponse(body []byte) ([]clawhubSkillItem, error) {
	// 先尝试解析为 search 响应结构
	var searchResp clawhubSearchResponse
	if err := json.Unmarshal(body, &searchResp); err == nil && len(searchResp.Results) > 0 {
		items := make([]clawhubSkillItem, 0, len(searchResp.Results))
		for _, sr := range searchResp.Results {
			items = append(items, sr.toSkillItem())
		}
		return items, nil
	}

	// 再尝试解析为 list 响应结构
	var listResp clawhubListResponse
	if err := json.Unmarshal(body, &listResp); err == nil && len(listResp.Items) > 0 {
		return listResp.Items, nil
	}

	return nil, nil
}

// Fetch 下载 ClawHub skill zip 包并提取内容。
// identifier 为纯 skill 名称（不含 clawhub: 前缀）。
func (c *ClawHubSource) Fetch(ctx context.Context, identifier string) (*SkillBundle, error) {
	return c.FetchVersion(ctx, identifier, "")
}

// FetchVersion 下载指定版本的 ClawHub skill zip 包并提取内容。
// slug 为纯 skill 名称（不含 clawhub: 前缀）。
// version 为空或 "latest" 时，download URL 不带 version 参数。
func (c *ClawHubSource) FetchVersion(ctx context.Context, slug, version string) (*SkillBundle, error) {
	if slug == "" {
		return nil, fmt.Errorf("empty slug")
	}

	downloadURL := fmt.Sprintf("%s/download?slug=%s", clawhubAPIBase, url.QueryEscape(slug))
	if version != "" && version != "latest" {
		downloadURL += "&version=" + url.QueryEscape(version)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clawhub download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("clawhub skill %q version %q not found (404)", slug, version)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clawhub download returned status %d for %s@%s", resp.StatusCode, slug, version)
	}

	zipBytes, err := io.ReadAll(io.LimitReader(resp.Body, 100_000_000))
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}

	return c.extractSkill(zipBytes, slug, version)
}

// extractSkill 解压 ClawHub zip 包，查找 SKILL.md，收集附属文件。
func (c *ClawHubSource) extractSkill(zipBytes []byte, slug, version string) (*SkillBundle, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "clawhub-skill-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	// 获取顶层目录前缀（如 slug-main），用于去除。
	var rootPrefix string
	for _, f := range reader.File {
		parts := strings.SplitN(f.Name, "/", 2)
		if parts[0] != "" {
			rootPrefix = parts[0] + "/"
			break
		}
	}

	// 解压所有文件。
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}

		fName := f.Name
		if rootPrefix != "" {
			fName = strings.TrimPrefix(fName, rootPrefix)
		}
		if fName == "" {
			continue
		}

		destPath := filepath.Join(tmpDir, fName)
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

	// 查找 SKILL.md。
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
			return nil, fmt.Errorf("SKILL.md not found in clawhub skill %s@%s", slug, version)
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
		filepath.Walk(subPath, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(skillDir, path)
			data, readErr := os.ReadFile(path)
			if readErr == nil && len(data) <= 1_048_576 {
				files[filepath.ToSlash(rel)] = data
			}
			return nil
		})
	}

	return &SkillBundle{
		Meta: SkillMeta{
			Name:        manifest.Name,
			Identifier:  slug,
			Source:      c.SourceID(),
			TrustLevel:  "community",
			Description: manifest.Description,
			Version:     version,
			Category:    manifest.Metadata.Category,
			Tags:        manifest.Metadata.Tags,
		},
		Content: content,
		Files:   files,
		TempDir: tmpDir,
	}, nil
}

// Inspect 获取 ClawHub 上 Skill 的元数据。
func (c *ClawHubSource) Inspect(ctx context.Context, identifier string) (*SkillMeta, error) {
	slug := identifier
	if slug == "" {
		return nil, fmt.Errorf("empty slug")
	}

	items, err := c.searchSkills(ctx, slug, 5)
	if err != nil {
		return nil, fmt.Errorf("clawhub inspect: %w", err)
	}

	for _, item := range items {
		if item.Slug == slug {
			meta := item.toSkillMeta()
			return &meta, nil
		}
	}

	// 若 search 未命中，回退到 Fetch（跳过附属文件下载）。
	bundle, err := c.Fetch(ctx, identifier)
	if err != nil {
		return nil, fmt.Errorf("clawhub inspect: %w", err)
	}
	defer os.RemoveAll(bundle.TempDir)
	meta := bundle.Meta
	return &meta, nil
}

// --- API response types ---

// clawhubListResponse GET /api/v1/skills 的响应结构。
type clawhubListResponse struct {
	Items      []clawhubSkillItem `json:"items"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

// clawhubSearchResponse GET /api/v1/search 的响应结构。
type clawhubSearchResponse struct {
	Results []clawhubSearchResult `json:"results"`
}

// clawhubSearchResult search 结果条目。
type clawhubSearchResult struct {
	Score       float64 `json:"score"`
	Slug        string  `json:"slug"`
	DisplayName string  `json:"displayName"`
	Summary     string  `json:"summary"`
	Version     string  `json:"version"`
	UpdatedAt   int64   `json:"updatedAt"`
	OwnerHandle string  `json:"ownerHandle"`
	Owner       *struct {
		Handle      string `json:"handle"`
		DisplayName string `json:"displayName"`
		Image       string `json:"image"`
	} `json:"owner,omitempty"`
}

// toSkillItem 将 search 结果转为统一的 SkillItem，方便上层统一处理。
func (r *clawhubSearchResult) toSkillItem() clawhubSkillItem {
	author := r.OwnerHandle
	if r.Owner != nil && r.Owner.DisplayName != "" {
		author = r.Owner.DisplayName
	}
	version := r.Version
	if version == "" {
		version = "latest"
	}
	return clawhubSkillItem{
		Slug:        r.Slug,
		DisplayName: r.DisplayName,
		Summary:     r.Summary,
		Author:      author,
		Version:     version,
	}
}

// clawhubSkillItem 统一的 Skill 条目，由 list 的 items[] 或 search 的 results[] 转换而来。
type clawhubSkillItem struct {
	Slug          string              `json:"slug"`
	DisplayName   string              `json:"displayName"`
	Summary       string              `json:"summary"`
	Description   string              `json:"description"`
	Author        string              `json:"author"`
	Topics        []string            `json:"topics,omitempty"`
	Tags          map[string]string   `json:"tags,omitempty"`
	Stats         *clawhubStats       `json:"stats,omitempty"`
	LatestVersion *clawhubVersionInfo `json:"latestVersion,omitempty"`
	Version       string              `json:"version,omitempty"` // search 结果直接携带
}

type clawhubStats struct {
	Comments        int `json:"comments"`
	Downloads       int `json:"downloads"`
	InstallsAllTime int `json:"installsAllTime"`
	InstallsCurrent int `json:"installsCurrent"`
	Stars           int `json:"stars"`
	Versions        int `json:"versions"`
}

type clawhubVersionInfo struct {
	Version string `json:"version"`
}

// toSkillMeta 将 clawhubSkillItem 转为统一的 SkillMeta。
func (i *clawhubSkillItem) toSkillMeta() SkillMeta {
	version := i.Version
	if version == "" && i.LatestVersion != nil && i.LatestVersion.Version != "" {
		version = i.LatestVersion.Version
	}
	if version == "" {
		version = "latest"
	}

	// displayName 优先，fallback 到 slug
	name := i.DisplayName
	if name == "" {
		name = i.Slug
	}

	// 取 tags 中的 latest 作为版本标识
	if version == "latest" && i.Tags != nil {
		if v, ok := i.Tags["latest"]; ok && v != "" {
			version = v
		}
	}

	installs := int64(0)
	if i.Stats != nil {
		installs = int64(i.Stats.Downloads + i.Stats.InstallsAllTime)
	}

	// description 字段优先，为空时 fallback 到 summary
	desc := i.Description
	if desc == "" {
		desc = i.Summary
	}

	return SkillMeta{
		SkillID:     i.Slug,
		Name:        name,
		Identifier:  i.Slug,
		Source:      "clawhub",
		TrustLevel:  "community",
		Description: desc,
		Version:     version,
		Author:      i.Author,
		Tags:        i.Topics,
		Installs:    installs,
	}
}

// SkillDetail 表示从 ClawHub zip 中解析出的 skill 详细信息。
type SkillDetail struct {
	SkillID     string   `json:"skill_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Icon        string   `json:"icon,omitempty"`
	SkillMD     string   `json:"skill_md"`      // SKILL.md body（不含 frontmatter）
	Files       []string `json:"files"`
}

// GetDetail 下载指定版本 ClawHub skill 的 zip 包并解析出完整详情（元数据 + SKILL.md + 文件列表）。
// 与 Fetch 不同，GetDetail 不会将文件写入磁盘，仅返回解析后的信息。
func (c *ClawHubSource) GetDetail(ctx context.Context, slug, version string) (*SkillDetail, error) {
	var downloadURL string
	if version == "" || version == "latest" {
		downloadURL = fmt.Sprintf("%s/download?slug=%s", clawhubAPIBase, url.QueryEscape(slug))
	} else {
		downloadURL = fmt.Sprintf("%s/download?slug=%s&version=%s", clawhubAPIBase, url.QueryEscape(slug), url.QueryEscape(version))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clawhub download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("clawhub skill %q version %q not found (404)", slug, version)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clawhub download returned status %d for %s@%s", resp.StatusCode, slug, version)
	}

	zipBytes, err := io.ReadAll(io.LimitReader(resp.Body, 100_000_000))
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	// 获取顶层目录前缀（如 slug-main），用于去除。
	var rootPrefix string
	for _, f := range reader.File {
		parts := strings.SplitN(f.Name, "/", 2)
		if parts[0] != "" {
			rootPrefix = parts[0] + "/"
			break
		}
	}

	// 遍历 zip 文件，查找 SKILL.md 并收集文件列表。
	var skillMDRaw []byte
	var files []string
	allowedSubdirs := map[string]bool{"assets": true, "references": true, "scripts": true, "templates": true}

	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}

		fName := f.Name
		if rootPrefix != "" {
			fName = strings.TrimPrefix(fName, rootPrefix)
		}
		if fName == "" {
			continue
		}

		// 收集文件列表
		dirPart := filepath.Dir(fName)
		if dirPart == "." || allowedSubdirs[dirPart] {
			files = append(files, filepath.ToSlash(fName))
		}

		// 读取 SKILL.md
		if strings.EqualFold(filepath.Base(fName), "SKILL.md") {
			rc, openErr := f.Open()
			if openErr != nil {
				return nil, fmt.Errorf("open SKILL.md in zip: %w", openErr)
			}
			skillMDRaw, err = io.ReadAll(io.LimitReader(rc, 1_048_576))
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("read SKILL.md: %w", err)
			}
		}
	}

	if skillMDRaw == nil {
		return nil, fmt.Errorf("SKILL.md not found in clawhub skill %s@%s", slug, version)
	}

	// 解析 frontmatter
	manifest, skillMDBody, err := catalog.ParseDocument(skillMDRaw)
	if err != nil {
		return nil, fmt.Errorf("parse SKILL.md: %w", err)
	}

	// 确保版本有值
	resolvedVersion := version
	if resolvedVersion == "latest" && manifest.Version != "" {
		resolvedVersion = manifest.Version
	}

	return &SkillDetail{
		SkillID:     slug,
		Name:        manifest.Name,
		Description: manifest.Description,
		Version:     resolvedVersion,
		Author:      "",
		Category:    manifest.Metadata.Category,
		Tags:        manifest.Metadata.Tags,
		Icon:        "",
		SkillMD:     skillMDBody,
		Files:       files,
	}, nil
}
