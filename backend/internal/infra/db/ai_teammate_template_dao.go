package db

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// ListAITeammateTemplates lists preset AI teammate templates with optional filters.
func ListAITeammateTemplates(ctx context.Context, db *gorm.DB, opt *types.PageQuery) ([]*types.AITeammateTemplate, int64, error) {
	var entities []*types.AITeammateTemplate
	var total int64

	query := db.WithContext(ctx).Model(&types.AITeammateTemplate{})
	for _, filter := range opt.Filters {
		switch filter.Field {
		case "status":
			if len(filter.Value) > 0 {
				query = query.Where("status = ?", filter.Value[0])
			}
		case "category":
			if len(filter.Value) > 0 {
				query = query.Where("category = ?", filter.Value[0])
			}
		case "keyword":
			if len(filter.Value) > 0 {
				kw := filter.Value[0]
				query = query.Where("name LIKE ? OR code LIKE ? OR description LIKE ? OR system_prompt LIKE ?", "%"+kw+"%", "%"+kw+"%", "%"+kw+"%", "%"+kw+"%")
			}
		}
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if len(opt.OrderBy) > 0 {
		query = query.Order(strings.Join(opt.OrderBy, ","))
	} else {
		query = query.Order("sort_order ASC, recommend_count DESC, use_count DESC, created_at DESC")
	}

	if !opt.ListAll && opt.Limit > 0 {
		query = query.Limit(opt.Limit)
	} else {
		query = query.Limit(types.PageMaxCount)
	}
	query = query.Offset(opt.Offset)

	if err := query.Find(&entities).Error; err != nil {
		return nil, 0, err
	}

	return entities, total, nil
}

// GetAITeammateTemplateByID returns a template by primary key.
func GetAITeammateTemplateByID(ctx context.Context, db *gorm.DB, id uint) (*types.AITeammateTemplate, error) {
	var entity types.AITeammateTemplate
	err := db.WithContext(ctx).First(&entity, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetAITeammateTemplateByCode returns a template by code.
func GetAITeammateTemplateByCode(ctx context.Context, db *gorm.DB, code string) (*types.AITeammateTemplate, error) {
	var entity types.AITeammateTemplate
	err := db.WithContext(ctx).Where("code = ?", code).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// UpsertAITeammateTemplate inserts or updates a preset template by code.
func UpsertAITeammateTemplate(ctx context.Context, db *gorm.DB, item *types.AITeammateTemplate) error {
	var existing types.AITeammateTemplate
	err := db.WithContext(ctx).Where("code = ?", item.Code).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return db.WithContext(ctx).Create(item).Error
		}
		return err
	}

	item.ID = existing.ID
	item.CreatedAt = existing.CreatedAt
	if item.UseCount == 0 {
		item.UseCount = existing.UseCount
	}
	if item.RecommendCount == 0 {
		item.RecommendCount = existing.RecommendCount
	}
	return db.WithContext(ctx).Save(item).Error
}

// IncrementAITeammateTemplateUseCount increments use_count for a template.
func IncrementAITeammateTemplateUseCount(ctx context.Context, db *gorm.DB, id uint) error {
	return db.WithContext(ctx).Model(&types.AITeammateTemplate{}).
		Where("id = ?", id).
		UpdateColumn("use_count", gorm.Expr("use_count + ?", 1)).Error
}

// IncrementAITeammateTemplateRecommendCount increments recommend_count for a template.
func IncrementAITeammateTemplateRecommendCount(ctx context.Context, db *gorm.DB, id uint) error {
	return db.WithContext(ctx).Model(&types.AITeammateTemplate{}).
		Where("id = ?", id).
		UpdateColumn("recommend_count", gorm.Expr("recommend_count + ?", 1)).Error
}
