package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/types"
)

// CacheKey 是市场记录缓存的复合查询键。
type CacheKey struct {
	Source  string
	SkillID string
	Version string
}

// cacheKey 生成 map key。
func cacheKey(source, skillID, version string) string {
	return fmt.Sprintf("%s|%s|%s", source, skillID, version)
}

// batchAndSize 限制单条 SQL 中 OR 条件的数量，避免优化器退化。
const batchAndSize = 30

// BatchGetSkillMarketplaceItems 按 (source, skill_id, version) 批量查询缓存记录。
//
// 将 key 按 source 分组后分批查询，每组用 (source, skill_id, version)
// 复合索引做 eq_ref 级别的 (skill_id = ? AND version = ?) OR 条件，
// 每组最多 batchAndSize 个 OR，数据库能稳定走索引。
func BatchGetSkillMarketplaceItems(ctx context.Context, db *gorm.DB, keys []CacheKey) (map[string]*types.SkillMarketplaceItem, error) {
	if len(keys) == 0 {
		return map[string]*types.SkillMarketplaceItem{}, nil
	}

	result := make(map[string]*types.SkillMarketplaceItem, len(keys))

	// 按 source 分组，每组内用 AND OR 的 IN 查询
	groups := groupKeysBySource(keys)
	for source, groupKeys := range groups {
		for i := 0; i < len(groupKeys); i += batchAndSize {
			end := min(i+batchAndSize, len(groupKeys))
			batch := groupKeys[i:end]

			conditions := make([]string, len(batch))
			params := make([]any, 0, len(batch)*2)
			for j, k := range batch {
				conditions[j] = "(skill_id = ? AND version = ?)"
				params = append(params, k.SkillID, k.Version)
			}

			var items []types.SkillMarketplaceItem
			if err := db.WithContext(ctx).
				Where("source = ? AND ("+strings.Join(conditions, " OR ")+")", append([]any{source}, params...)...).
				Find(&items).Error; err != nil {
				return nil, err
			}
			for i := range items {
				key := cacheKey(items[i].Source, items[i].SkillID, items[i].Version)
				result[key] = &items[i]
			}
		}
	}

	return result, nil
}

// groupKeysBySource 将 keys 按 Source 分组。
func groupKeysBySource(keys []CacheKey) map[string][]CacheKey {
	groups := make(map[string][]CacheKey, 2)
	for _, k := range keys {
		groups[k.Source] = append(groups[k.Source], k)
	}
	return groups
}

// BatchUpsertSkillMarketplaceItems 按 (source, skill_id, version) 冲突时更新。
func BatchUpsertSkillMarketplaceItems(ctx context.Context, db *gorm.DB, items []types.SkillMarketplaceItem) error {
	if len(items) == 0 {
		return nil
	}

	err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "source"},
			{Name: "skill_id"},
			{Name: "version"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "description", "translated_description", "author",
			"installs", "category", "tags", "updated_at",
		}),
	}).Create(&items).Error
	if err != nil {
		logs.Errorf("batch upsert skill marketplace items: %v", err)
		return err
	}
	return nil
}
