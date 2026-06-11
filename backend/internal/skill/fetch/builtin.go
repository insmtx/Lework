package fetch

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

// BuiltinSource 从数据库读取内置 Skill 市场条目。
type BuiltinSource struct {
	db *gorm.DB
}

// NewBuiltinSource 创建 BuiltinSource。
func NewBuiltinSource(db *gorm.DB) *BuiltinSource {
	return &BuiltinSource{db: db}
}

// SourceID 返回源标识。
func (s *BuiltinSource) SourceID() string {
	return "leros_builtin"
}

// CanHandle 内置源不处理外部标识符。
func (s *BuiltinSource) CanHandle(identifier string) bool {
	return false
}

// Search 从数据库查询状态为 active 的内置 Skill。
func (s *BuiltinSource) Search(ctx context.Context, query string, limit int) ([]SkillMeta, error) {
	items, err := infradb.SearchBuiltinSkills(ctx, s.db, query, "", limit)
	if err != nil {
		return nil, fmt.Errorf("builtin search: %w", err)
	}

	results := make([]SkillMeta, 0, len(items))
	for _, item := range items {
		results = append(results, s.itemToMeta(item))
	}
	return results, nil
}

// Fetch 按 skill_id 查找内置 Skill，返回空 bundle（安装阶段再补完整逻辑）。
func (s *BuiltinSource) Fetch(ctx context.Context, identifier string) (*SkillBundle, error) {
	item, err := infradb.GetBuiltinSkillByID(ctx, s.db, identifier)
	if err != nil {
		return nil, fmt.Errorf("builtin fetch: %w", err)
	}
	if item == nil {
		return nil, fmt.Errorf("builtin skill %q not found", identifier)
	}
	meta := s.itemToMeta(*item)
	return &SkillBundle{Meta: meta}, nil
}

// Inspect 按 skill_id 查找内置 Skill 元数据。
func (s *BuiltinSource) Inspect(ctx context.Context, identifier string) (*SkillMeta, error) {
	item, err := infradb.GetBuiltinSkillByID(ctx, s.db, identifier)
	if err != nil {
		return nil, fmt.Errorf("builtin inspect: %w", err)
	}
	if item == nil {
		return nil, fmt.Errorf("builtin skill %q not found", identifier)
	}
	meta := s.itemToMeta(*item)
	return &meta, nil
}

func (s *BuiltinSource) itemToMeta(item types.BuiltinSkillMarketplaceItem) SkillMeta {
	return SkillMeta{
		SkillID:     item.SkillID,
		Name:        item.Name,
		Identifier:  item.SkillID,
		Source:      s.SourceID(),
		TrustLevel:  "trusted",
		Description: item.Description,
		Version:     item.Version,
		Author:      item.Author,
		Category:    item.Category,
		Tags:        []string(item.Tags),
		Icon:        item.Icon,
		Installs:    item.Installs,
	}
}
