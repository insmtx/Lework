package catalog

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest 描述文件型 Skill 的元数据区域。
type Manifest struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Version     string           `yaml:"version,omitempty"`
	Metadata    ManifestMetadata `yaml:"metadata,omitempty"`
}

// Normalize 在解析 Skill 文档后补齐派生默认值。
func (m *Manifest) Normalize(defaultName string) {
	if m.Name == "" {
		m.Name = defaultName
	}

	if m.Description == "" {
		m.Description = m.Name
	}
}

// ManifestMetadata 存储 Skill 路由提示和元数据扩展。
type ManifestMetadata struct {
	Category      string   `yaml:"category,omitempty"`
	Tags          []string `yaml:"tags,omitempty"`
	Always        bool     `yaml:"always,omitempty"`
	RequiresTools []string `yaml:"requires_tools,omitempty"`
	Source        string   `yaml:"source,omitempty"`
	Trust         string   `yaml:"trust,omitempty"`
}

// Entry 表示一个已发现并解析出元数据和正文的 Skill 文档。
type Entry struct {
	Manifest      Manifest
	Body          string
	Dir           string
	Path          string
	AbsoluteDir   string
	StoredSkillID string
}

// Summary 是注入运行时提示词的紧凑视图。
type Summary struct {
	SkillID       string
	Name          string
	Description   string
	Version       string
	Category      string
	Tags          []string
	Always        bool
	RequiresTools []string
	Source        string
	Trust         string
}

// Summary 返回适合提示词使用的 Skill 条目摘要。
func (e *Entry) Summary() Summary {
	source := e.Manifest.Metadata.Source
	if source == "" {
		source = "local"
	}
	trust := e.Manifest.Metadata.Trust
	if trust == "" {
		trust = "trusted"
	}
	return Summary{
		SkillID:       e.StoredSkillID,
		Name:          e.Manifest.Name,
		Description:   e.Manifest.Description,
		Version:       e.Manifest.Version,
		Category:      e.Manifest.Metadata.Category,
		Tags:          e.Manifest.Metadata.Tags,
		Always:        e.Manifest.Metadata.Always,
		RequiresTools: e.Manifest.Metadata.RequiresTools,
		Source:        source,
		Trust:         trust,
	}
}

// ParseDocument 解析带可选 YAML frontmatter 的 SKILL.md 文档。
func ParseDocument(raw []byte) (*Manifest, string, error) {
	content, lines, endIndex, hasFrontmatter, err := splitDocument(raw)
	if err != nil {
		return nil, "", err
	}
	if content == "" {
		return &Manifest{}, "", nil
	}
	if !hasFrontmatter {
		return &Manifest{}, content, nil
	}

	manifest, _, _, err := parseManifest(lines, endIndex)
	if err != nil {
		return nil, "", fmt.Errorf("unmarshal frontmatter: %w", err)
	}

	body := strings.Join(lines[endIndex+1:], "\n")
	return manifest, strings.TrimSpace(body), nil
}

// NormalizeDocument repairs compatible plain description scalars so downstream
// runtimes receive standards-compliant YAML without losing their text.
func NormalizeDocument(raw []byte) ([]byte, error) {
	content, lines, endIndex, hasFrontmatter, err := splitDocument(raw)
	if err != nil {
		return nil, err
	}
	if content == "" || !hasFrontmatter {
		return append([]byte(nil), raw...), nil
	}

	_, normalizedLines, changed, err := parseManifest(lines, endIndex)
	if err != nil {
		return nil, fmt.Errorf("unmarshal frontmatter: %w", err)
	}
	if !changed {
		return append([]byte(nil), raw...), nil
	}

	original := string(raw)
	contentStart := strings.Index(original, content)
	if contentStart == -1 {
		return nil, fmt.Errorf("locate normalized Skill document")
	}
	normalizedContent := strings.Join(normalizedLines, "\n")
	result := make([]byte, 0, len(original)-len(content)+len(normalizedContent))
	result = append(result, original[:contentStart]...)
	result = append(result, normalizedContent...)
	result = append(result, original[contentStart+len(content):]...)
	return result, nil
}

func splitDocument(raw []byte) (string, []string, int, bool, error) {
	content := strings.TrimSpace(string(raw))
	if content == "" || !strings.HasPrefix(content, "---") {
		return content, nil, -1, false, nil
	}

	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "", nil, -1, false, fmt.Errorf("invalid frontmatter header")
	}

	endIndex := -1
	for idx := 1; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) == "---" {
			endIndex = idx
			break
		}
	}
	if endIndex == -1 {
		return "", nil, -1, false, fmt.Errorf("frontmatter closing delimiter not found")
	}
	return content, lines, endIndex, true, nil
}

func parseManifest(lines []string, endIndex int) (*Manifest, []string, bool, error) {
	manifest := &Manifest{}
	parseErr := yaml.Unmarshal(frontmatterBytes(lines, endIndex), manifest)
	if parseErr == nil {
		return manifest, lines, false, nil
	}

	normalizedLines, changed := quotePlainDescription(lines, endIndex)
	if !changed {
		return nil, nil, false, parseErr
	}
	manifest = &Manifest{}
	if err := yaml.Unmarshal(frontmatterBytes(normalizedLines, endIndex), manifest); err != nil {
		return nil, nil, false, err
	}
	return manifest, normalizedLines, true, nil
}

func frontmatterBytes(lines []string, endIndex int) []byte {
	return []byte(strings.Join(lines[1:endIndex], "\n") + "\n")
}

func quotePlainDescription(lines []string, endIndex int) ([]string, bool) {
	normalized := append([]string(nil), lines...)
	changed := false
	for idx := 1; idx < endIndex; idx++ {
		line := lines[idx]
		if strings.TrimLeft(line, " \t") != line {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found || key != "description" {
			continue
		}
		value = strings.TrimSpace(value)
		if !isPlainDescriptionWithMappingSeparator(value) {
			continue
		}
		lineEnding := ""
		if strings.HasSuffix(line, "\r") {
			lineEnding = "\r"
		}
		normalized[idx] = "description: " + strconv.Quote(value) + lineEnding
		changed = true
	}
	return normalized, changed
}

func isPlainDescriptionWithMappingSeparator(value string) bool {
	if value == "" || !strings.Contains(value, ": ") {
		return false
	}
	switch value[0] {
	case '\'', '"', '|', '>', '[', '{', '!', '&', '*':
		return false
	default:
		return true
	}
}
