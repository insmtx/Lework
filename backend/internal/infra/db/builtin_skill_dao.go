package db

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ygpkg/yg-go/logs"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// skillFrontMatterMetadata SKILL.md front matter 中 metadata 字段的结构。
type skillFrontMatterMetadata struct {
	Tags []string `yaml:"tags"`
}

// skillFrontMatter SKILL.md 的 YAML front matter 结构。
type skillFrontMatter struct {
	Name        string                   `yaml:"name"`
	Description string                   `yaml:"description"`
	Version     string                   `yaml:"version"`
	Authors     []string                 `yaml:"authors"`
	Metadata    skillFrontMatterMetadata `yaml:"metadata"`
}

// ================================================================================
// 查询 DAO
// ================================================================================

// SearchBuiltinSkills 查询状态为 active 的内置 Skill，支持关键词和分类过滤。
func SearchBuiltinSkills(ctx context.Context, db *gorm.DB, keyword, category string, limit int) ([]types.BuiltinSkillMarketplaceItem, error) {
	query := db.WithContext(ctx).Model(&types.BuiltinSkillMarketplaceItem{}).Where("status = ?", "active")

	if strings.TrimSpace(keyword) != "" {
		like := "%" + strings.TrimSpace(keyword) + "%"
		query = query.Where("skill_id LIKE ? OR name LIKE ? OR description LIKE ?", like, like, like)
	}
	if strings.TrimSpace(category) != "" {
		query = query.Where("category = ?", strings.TrimSpace(category))
	}
	if limit > 0 {
		query = query.Limit(limit)
	}

	var items []types.BuiltinSkillMarketplaceItem
	if err := query.Order("published_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// GetBuiltinSkillByID 按 skill_id 查询单条内置 Skill。
// 未找到时返回 nil, nil（遵循项目 DAO 惯例）。
func GetBuiltinSkillByID(ctx context.Context, db *gorm.DB, skillID string) (*types.BuiltinSkillMarketplaceItem, error) {
	var item types.BuiltinSkillMarketplaceItem
	err := db.WithContext(ctx).
		Where("skill_id = ? AND status = ?", skillID, "active").
		First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

// ================================================================================
// 种子数据
// ================================================================================

// SeedBuiltinSkillMarketplace 从 backend/skills/server/ 下的 SKILL.md 解析元数据并写入数据库。
func SeedBuiltinSkillMarketplace(db *gorm.DB) error {
	var count int64
	db.Model(&types.BuiltinSkillMarketplaceItem{}).Count(&count)
	if count > 0 {
		return nil
	}

	serverDir, err := resolveSkillsServerDir()
	if err != nil {
		return fmt.Errorf("resolve skills server dir: %w", err)
	}

	entries, err := os.ReadDir(serverDir)
	if err != nil {
		return fmt.Errorf("read skills server dir %s: %w", serverDir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(serverDir, entry.Name())
		skillMDPath := filepath.Join(skillDir, "SKILL.md")
		content, err := os.ReadFile(skillMDPath)
		if err != nil {
			logs.Warnf("Skip skill dir %s: cannot read SKILL.md: %v", entry.Name(), err)
			continue
		}

		fm, err := parseSkillFrontMatter(content)
		if err != nil {
			logs.Warnf("Skip skill dir %s: parse front matter: %v", entry.Name(), err)
			continue
		}

		skillID := fm.Name
		if skillID == "" {
			skillID = entry.Name()
		}

		version := fm.Version
		if version == "" {
			version = "1.0.0"
		}

		author := "Leros"
		if len(fm.Authors) > 0 {
			author = fm.Authors[0]
		}

		tags := types.SkillStringList(fm.Metadata.Tags)
		now := time.Now()

		item := &types.BuiltinSkillMarketplaceItem{
			SkillID:     skillID,
			Name:        skillID,
			Description: fm.Description,
			Version:     version,
			Author:      author,
			Category:    "",
			Tags:        tags,
			Verified:    true,
			Status:      "active",
			PublishedAt: &now,
		}

		if err := db.Create(item).Error; err != nil {
			return fmt.Errorf("create builtin skill %s: %w", skillID, err)
		}
		logs.Infof("Seeded builtin skill marketplace item: %s", skillID)
	}

	return nil
}

// ================================================================================
// 辅助函数
// ================================================================================

// resolveSkillsServerDir 解析 backend/skills/server/ 目录路径。
// 从工作目录开始向上查找，兜底 /app/backend/skills/server/（Docker 环境）。
func resolveSkillsServerDir() (string, error) {
	relPath := filepath.Join("backend", "skills", "server")

	if wd, err := os.Getwd(); err == nil {
		for dir := filepath.Clean(wd); ; dir = filepath.Dir(dir) {
			candidate := filepath.Join(dir, relPath)
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				return candidate, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	dockerPath := filepath.Join(string(os.PathSeparator), "app", relPath)
	if info, err := os.Stat(dockerPath); err == nil && info.IsDir() {
		return dockerPath, nil
	}

	return "", fmt.Errorf("skills server directory not found")
}

// parseSkillFrontMatter 从 SKILL.md 内容中提取 YAML front matter。
func parseSkillFrontMatter(content []byte) (*skillFrontMatter, error) {
	parts := bytes.SplitN(content, []byte("---\n"), 3)
	if len(parts) < 3 {
		// 也尝试不带尾部换行的分隔
		parts = bytes.SplitN(content, []byte("---"), 3)
		if len(parts) < 3 {
			return nil, fmt.Errorf("no YAML front matter found")
		}
	}

	var fm skillFrontMatter
	if err := yaml.Unmarshal(parts[1], &fm); err != nil {
		return nil, fmt.Errorf("unmarshal front matter: %w", err)
	}
	return &fm, nil
}
