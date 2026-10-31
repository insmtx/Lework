package db

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Lework/backend/types"
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

// GetDigitalAssistantByPublicID 根据PublicID获取数字助手
func GetDigitalAssistantByPublicID(ctx context.Context, db *gorm.DB, publicID string) (*types.DigitalAssistant, error) {
	var entity types.DigitalAssistant
	err := db.WithContext(ctx).Where("public_id = ?", publicID).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetDigitalAssistantByCode is kept for older callers that used code as the assistant public identifier.
func GetDigitalAssistantByCode(ctx context.Context, db *gorm.DB, code string) (*types.DigitalAssistant, error) {
	return GetDigitalAssistantByPublicID(ctx, db, code)
}

// UpdateDigitalAssistant 更新数字助手
func UpdateDigitalAssistant(ctx context.Context, db *gorm.DB, da *types.DigitalAssistant) error {
	return db.WithContext(ctx).Save(da).Error
}

// DeleteDigitalAssistant 删除数字助手
func DeleteDigitalAssistant(ctx context.Context, db *gorm.DB, id uint) error {
	return db.WithContext(ctx).Delete(&types.DigitalAssistant{}, id).Error
}

// DigitalAssistantPublicIDExists 检查public_id是否存在（排除指定ID）
func DigitalAssistantPublicIDExists(ctx context.Context, db *gorm.DB, publicID string, excludeID uint) (bool, error) {
	var count int64
	query := db.WithContext(ctx).Model(&types.DigitalAssistant{}).Where("public_id = ?", publicID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// DigitalAssistantNameExists checks whether a normalized name already exists in an organization.
func DigitalAssistantNameExists(ctx context.Context, database *gorm.DB, orgID uint, name string, excludeID uint) (bool, error) {
	var count int64
	query := database.WithContext(ctx).
		Model(&types.DigitalAssistant{}).
		Where("org_id = ? AND LOWER(TRIM(name)) = ?", orgID, strings.ToLower(strings.TrimSpace(name)))
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListDigitalAssistant 查询数字助手列表
func ListDigitalAssistant(ctx context.Context, db *gorm.DB, opt *types.PageQuery) ([]*types.DigitalAssistant, int64, error) {
	var entities []*types.DigitalAssistant
	var total int64

	query := db.WithContext(ctx).
		Model(&types.DigitalAssistant{}).
		Where(
			"substr(public_id, 1, ?) <> ?",
			len(types.DefaultDigitalAssistantPublicIDPrefix),
			types.DefaultDigitalAssistantPublicIDPrefix,
		)

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
				query = query.Where("name LIKE ? OR role_name LIKE ? OR public_id LIKE ? OR description LIKE ? OR system_prompt LIKE ?", "%"+kw+"%", "%"+kw+"%", "%"+kw+"%", "%"+kw+"%", "%"+kw+"%")
			}
		case "source":
			if len(filter.Value) > 0 {
				query = query.Where("source = ?", filter.Value[0])
			}
		case "viewer_uin":
			if len(filter.Value) > 0 {
				query = query.Where(
					"(visibility = ? AND status = ?) OR EXISTS ("+
						"SELECT 1 FROM "+types.TableNameResource+" AS r "+
						"JOIN "+types.TableNameResourceBinding+" AS b ON b.resource_id = r.id AND b.deleted_at IS NULL "+
						"WHERE r.type = ? AND r.biz_id = "+types.TableNameDigitalAssistant+".id "+
						"AND r.org_id = "+types.TableNameDigitalAssistant+".org_id AND r.deleted_at IS NULL "+
						"AND b.uin = ?)",
					types.DigitalAssistantVisibilityPublic,
					"active",
					types.ResourceTypeAssistant,
					filter.Value[0],
				)
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
