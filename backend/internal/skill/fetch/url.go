package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	catalog "github.com/insmtx/Lework/backend/internal/skill/catalog"
)

// UrlSource 通过 HTTP 直链下载 SKILL.md。
type UrlSource struct {
	client *http.Client
}

// NewUrlSource 创建 UrlSource。
func NewUrlSource() *UrlSource {
	return &UrlSource{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// SourceID 返回源标识。
func (u *UrlSource) SourceID() string {
	return "url"
}

// CanHandle 以 http:// 或 https:// 开头且路径以 .md 结尾的标识符由 UrlSource 处理。
func (u *UrlSource) CanHandle(identifier string) bool {
	ident := strings.TrimSpace(identifier)
	if !strings.HasPrefix(ident, "http://") && !strings.HasPrefix(ident, "https://") {
		return false
	}

	// 排除 well-known 发现端点和 index.json。
	if strings.Contains(ident, "/.well-known/skills/") || strings.HasSuffix(strings.TrimRight(ident, "/"), "/index.json") {
		return false
	}

	parsed, err := url.Parse(ident)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(parsed.Path), ".md")
}

// Search UrlSource 不支持搜索。
func (u *UrlSource) Search(ctx context.Context, query string, limit int) ([]SkillMeta, error) {
	return nil, nil
}

// Fetch 从 URL 下载 SKILL.md 内容。
func (u *UrlSource) Fetch(ctx context.Context, identifier string) (*SkillBundle, error) {
	if !u.CanHandle(identifier) {
		return nil, fmt.Errorf("UrlSource cannot handle %q", identifier)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, identifier, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("URL returned status %d", resp.StatusCode)
	}

	content, err := io.ReadAll(io.LimitReader(resp.Body, 100_000))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	manifest, _, err := catalog.ParseDocument(content)
	if err != nil {
		return nil, fmt.Errorf("parse SKILL.md from URL: %w", err)
	}

	if manifest.Name == "" {
		return nil, fmt.Errorf("SKILL.md at %s has no name in frontmatter", identifier)
	}

	return &SkillBundle{
		Meta: SkillMeta{
			Name:        manifest.Name,
			Identifier:  identifier,
			Source:      "url",
			TrustLevel:  "community",
			Description: manifest.Description,
		},
		Content: content,
		Files:   nil,
		TempDir: "",
	}, nil
}

// Inspect 获取 URL 对应 SKILL.md 的元数据。
func (u *UrlSource) Inspect(ctx context.Context, identifier string) (*SkillMeta, error) {
	bundle, err := u.Fetch(ctx, identifier)
	if err != nil {
		return nil, err
	}
	meta := bundle.Meta
	return &meta, nil
}
