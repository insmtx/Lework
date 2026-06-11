// Package fetch 提供从远程源发现和下载 Skill 的能力。
package fetch

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// TrustedRepos 受信任的 GitHub 仓库列表。
var TrustedRepos = map[string]bool{
	"openai/skills":      true,
	"anthropics/skills":  true,
	"huggingface/skills": true,
	"NVIDIA/skills":      true,
}

// SkillMeta 搜索或检查返回的轻量 Skill 信息。
type SkillMeta struct {
	SkillID     string   `json:"skill_id"`
	Name        string   `json:"name,omitempty"`
	Identifier  string   `json:"identifier"`
	Source      string   `json:"source"`
	TrustLevel  string   `json:"trust_level"`
	Description string   `json:"description"`
	Version     string   `json:"version,omitempty"`
	Author      string   `json:"author,omitempty"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	Installs    int64    `json:"installs,omitempty"`
}

// SkillBundle Fetch 返回的完整 Skill 内容。
type SkillBundle struct {
	Meta    SkillMeta
	Content []byte            // SKILL.md 原始内容
	Files   map[string][]byte // 附属文件（相对路径 → 内容）
	TempDir string            // 临时解压目录（调用方负责清理）
}

// SkillSource 远程 Skill 源接口。
type SkillSource interface {
	Search(ctx context.Context, query string, limit int) ([]SkillMeta, error)
	Fetch(ctx context.Context, identifier string) (*SkillBundle, error)
	Inspect(ctx context.Context, identifier string) (*SkillMeta, error)
	SourceID() string
	CanHandle(identifier string) bool
}

// SourceRouter 管理一组远程 Skill 源，按优先级路由请求。
type SourceRouter struct {
	sources []SkillSource
}

// NewSourceRouter 创建包含所有内置源的 SourceRouter。
func NewSourceRouter() *SourceRouter {
	return &SourceRouter{
		sources: []SkillSource{
			NewUrlSource(),
			NewGitHubSource(),
			NewSkillsShSource(),
		},
	}
}

// NewSourceRouterWithSources 使用指定源列表创建 SourceRouter。
func NewSourceRouterWithSources(sources ...SkillSource) *SourceRouter {
	return &SourceRouter{sources: sources}
}

// Search 并发向所有源发起搜索，合并结果并去重。
func (r *SourceRouter) Search(ctx context.Context, query string, limit int) ([]SkillMeta, error) {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []SkillMeta
		seen    = make(map[string]bool)
	)

	for _, src := range r.sources {
		wg.Add(1)
		go func(s SkillSource) {
			defer wg.Done()
			items, err := s.Search(ctx, query, limit)
			if err != nil {
				return
			}
			mu.Lock()
			for _, item := range items {
				if !seen[item.Identifier] {
					seen[item.Identifier] = true
					results = append(results, item)
				}
			}
			mu.Unlock()
		}(src)
	}
	wg.Wait()

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// Fetch 按优先级遍历源，返回第一个成功获取的 SkillBundle。
func (r *SourceRouter) Fetch(ctx context.Context, identifier string) (*SkillBundle, error) {
	for _, src := range r.sources {
		if !src.CanHandle(identifier) {
			continue
		}
		bundle, err := src.Fetch(ctx, identifier)
		if err != nil {
			continue
		}
		return bundle, nil
	}
	return nil, fmt.Errorf("no source could handle identifier %q", identifier)
}

// ResolveShortName 对不含 "/" 的短名称，通过 skills.sh 搜索精确匹配后安装。
func (r *SourceRouter) ResolveShortName(ctx context.Context, name string) (*SkillBundle, error) {
	if strings.Contains(name, "/") {
		return nil, fmt.Errorf("ResolveShortName called with identifier containing '/': %s", name)
	}

	skillsSh := NewSkillsShSource()
	results, err := skillsSh.Search(ctx, name, 10)
	if err != nil {
		return nil, fmt.Errorf("search skills.sh for %q: %w", name, err)
	}

	for _, meta := range results {
		if strings.EqualFold(meta.Name, name) {
			return r.Fetch(ctx, meta.Identifier)
		}
	}

	return nil, fmt.Errorf("skill %q not found in any source", name)
}

// TrustLevelForRepo 根据仓库判断信任级别。
func TrustLevelForRepo(owner, repo string) string {
	full := owner + "/" + repo
	if TrustedRepos[full] {
		return "trusted"
	}
	return "community"
}
