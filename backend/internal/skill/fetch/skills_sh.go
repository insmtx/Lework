package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const skillsShSearchURL = "https://skills.sh/api/search"

// SkillsShSource 通过 skills.sh API 搜索 Skill。
type SkillsShSource struct {
	client *http.Client
}

// NewSkillsShSource 创建 SkillsShSource。
func NewSkillsShSource() *SkillsShSource {
	return &SkillsShSource{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// SourceID 返回源标识。
func (s *SkillsShSource) SourceID() string {
	return "SkillsSh"
}

// CanHandle 含 "/" 的非 URL 标识符可由 SkillsShSource 处理。
func (s *SkillsShSource) CanHandle(identifier string) bool {
	return strings.Count(identifier, "/") >= 1 && !strings.Contains(identifier, "://")
}

// Search 调用 skills.sh 搜索 API。
func (s *SkillsShSource) Search(ctx context.Context, query string, limit int) ([]SkillMeta, error) {
	if len([]rune(strings.TrimSpace(query))) < 2 {
		return []SkillMeta{}, nil
	}
	params := url.Values{}
	params.Set("q", query)
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}

	reqURL := skillsShSearchURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("skills.sh search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("skills.sh returned status %d", resp.StatusCode)
	}

	var apiResp skillsShSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode skills.sh response: %w", err)
	}

	var results []SkillMeta
	for _, item := range apiResp.Skills {
		parts := strings.SplitN(item.Source, "/", 2)
		if len(parts) != 2 {
			continue
		}
		owner, repo := parts[0], parts[1]
		if strings.Contains(owner, ".") || strings.Contains(repo, ".") {
			continue
		}

		identifier := item.Source + "/" + item.SkillID
		results = append(results, SkillMeta{
			SkillID:     item.SkillID,
			Name:        item.Name,
			Identifier:  identifier,
			Source:      s.SourceID(),
			TrustLevel:  TrustLevelForRepo(owner, repo),
			Description: item.Description,
			Author:      owner,
			Installs:    int64(item.Installs),
		})
	}

	return results, nil
}

// Fetch 通过 skills.sh 搜索后委托 GitHubSource 下载。
func (s *SkillsShSource) Fetch(ctx context.Context, identifier string) (*SkillBundle, error) {
	parts := strings.Split(identifier, "/")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid skills.sh identifier %q: expected owner/repo/skill", identifier)
	}

	owner, repo := parts[0], parts[1]
	skillName := parts[len(parts)-1]

	results, err := s.Search(ctx, skillName, 10)
	if err != nil {
		return nil, fmt.Errorf("search skills.sh: %w", err)
	}

	var target SkillMeta
	for _, r := range results {
		if r.Identifier == identifier {
			target = r
			break
		}
	}
	if target.Identifier == "" {
		results, err = s.Search(ctx, identifier, 10)
		if err != nil {
			return nil, fmt.Errorf("search skills.sh: %w", err)
		}
		for _, r := range results {
			if r.Identifier == identifier {
				target = r
				break
			}
		}
	}
	if target.Identifier == "" {
		return nil, fmt.Errorf("skill %q not found on skills.sh", identifier)
	}

	skillPath := strings.TrimPrefix(identifier, owner+"/"+repo+"/")
	ghSource := NewGitHubSource()
	bundle, err := ghSource.Fetch(ctx, owner+"/"+repo+"/"+skillPath)
	if err != nil {
		return nil, fmt.Errorf("fetch from GitHub: %w", err)
	}

	bundle.Meta.Source = s.SourceID()
	bundle.Meta.TrustLevel = TrustLevelForRepo(owner, repo)
	return bundle, nil
}

// Inspect 检查 skills.sh 上的 Skill 元数据。
func (s *SkillsShSource) Inspect(ctx context.Context, identifier string) (*SkillMeta, error) {
	results, err := s.Search(ctx, identifier, 5)
	if err != nil {
		return nil, err
	}
	for _, r := range results {
		if r.Identifier == identifier {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("skill %q not found on skills.sh", identifier)
}

type skillsShSearchResponse struct {
	Skills []skillsShSkillItem `json:"skills"`
	Count  int                 `json:"count"`
}

type skillsShSkillItem struct {
	ID          string `json:"id"`
	SkillID     string `json:"skillId"`
	Name        string `json:"name"`
	Source      string `json:"source"`
	Installs    int    `json:"installs"`
	Description string `json:"description"`
}
