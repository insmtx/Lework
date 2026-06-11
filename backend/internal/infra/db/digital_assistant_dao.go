package db

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/types"
)

// CreateDigitalAssistant 创建数字助手
func CreateDigitalAssistant(ctx context.Context, db *gorm.DB, da *types.DigitalAssistant) error {
	return db.WithContext(ctx).Create(da).Error
}

// GetDigitalAssistantByID 根据ID获取数字助手
func GetDigitalAssistantByID(ctx context.Context, db *gorm.DB, id uint) (*types.DigitalAssistant, error) {
	var entity types.DigitalAssistant
	err := db.WithContext(ctx).First(&entity, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetDigitalAssistantByCode 根据Code获取数字助手
func GetDigitalAssistantByCode(ctx context.Context, db *gorm.DB, code string) (*types.DigitalAssistant, error) {
	var entity types.DigitalAssistant
	err := db.WithContext(ctx).Where("code = ?", code).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// UpdateDigitalAssistant 更新数字助手
func UpdateDigitalAssistant(ctx context.Context, db *gorm.DB, da *types.DigitalAssistant) error {
	return db.WithContext(ctx).Save(da).Error
}

// DeleteDigitalAssistant 删除数字助手
func DeleteDigitalAssistant(ctx context.Context, db *gorm.DB, id uint) error {
	return db.WithContext(ctx).Delete(&types.DigitalAssistant{}, id).Error
}

// DigitalAssistantCodeExists 检查code是否存在（排除指定ID）
func DigitalAssistantCodeExists(ctx context.Context, db *gorm.DB, code string, excludeID uint) (bool, error) {
	var count int64
	query := db.WithContext(ctx).Model(&types.DigitalAssistant{}).Where("code = ?", code)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListDigitalAssistant 查询数字助手列表
func ListDigitalAssistant(ctx context.Context, db *gorm.DB, opt *types.PageQuery) ([]*types.DigitalAssistant, int64, error) {
	var entities []*types.DigitalAssistant
	var total int64

	query := db.WithContext(ctx).Model(&types.DigitalAssistant{})

	if opt.OrgID > 0 {
		query = query.Where("org_id = ?", opt.OrgID)
	}
	if opt.Uin > 0 {
		query = query.Where("owner_id = ?", opt.Uin)
	}

	for _, filter := range opt.Filters {
		switch filter.Field {
		case "owner_id":
			if len(filter.Value) > 0 {
				query = query.Where("owner_id = ?", filter.Value[0])
			}
		case "status":
			if len(filter.Value) > 0 {
				query = query.Where("status = ?", filter.Value[0])
			}
		case "keyword":
			if len(filter.Value) > 0 {
				kw := filter.Value[0]
				query = query.Where("name LIKE ? OR code LIKE ? OR description LIKE ?", "%"+kw+"%", "%"+kw+"%", "%"+kw+"%")
			}
		default:
			logs.WarnContextf(ctx, "[digital_assistant][ListDigitalAssistant] invalid filter field: %s", filter.Field)
		}
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if len(opt.OrderBy) > 0 {
		query = query.Order(strings.Join(opt.OrderBy, ","))
	} else {
		query = query.Order("created_at DESC")
	}

	if !opt.ListAll && opt.Limit > 0 {
		query = query.Limit(opt.Limit)
	} else {
		query = query.Limit(150)
	}
	query = query.Offset(opt.Offset)

	if err := query.Find(&entities).Error; err != nil {
		return nil, 0, err
	}

	return entities, total, nil
}
