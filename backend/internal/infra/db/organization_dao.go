package db

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

func CreateOrg(ctx context.Context, d *gorm.DB, org *types.Organization) error {
	return d.WithContext(ctx).Create(org).Error
}

func GetOrgByID(ctx context.Context, d *gorm.DB, id uint) (*types.Organization, error) {
	var entity types.Organization
	err := d.WithContext(ctx).Where("id = ?", id).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

func GetOrgByCode(ctx context.Context, d *gorm.DB, code string) (*types.Organization, error) {
	var entity types.Organization
	err := d.WithContext(ctx).Where("code = ?", code).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

func UpdateOrg(ctx context.Context, d *gorm.DB, org *types.Organization) error {
	return d.WithContext(ctx).Save(org).Error
}

func DeleteOrg(ctx context.Context, d *gorm.DB, id uint) error {
	return d.WithContext(ctx).Delete(&types.Organization{}, id).Error
}

func GetOrgsByIDs(ctx context.Context, d *gorm.DB, ids []uint) ([]*types.Organization, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var entities []*types.Organization
	err := d.WithContext(ctx).Where("id IN (?)", ids).Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

func ListOrgs(ctx context.Context, d *gorm.DB, opt *types.PageQuery) ([]*types.Organization, int64, error) {
	var entities []*types.Organization
	var total int64

	query := d.WithContext(ctx).Table(types.TableNameOrganization).
		Where("deleted_at IS NULL")

	for _, filter := range opt.Filters {
		switch filter.Field {
		case "keyword":
			query = query.Where("name LIKE ? OR code LIKE ?", "%"+filter.Value[0]+"%", "%"+filter.Value[0]+"%")
		case "name":
			if filter.ExactMatch {
				query = query.Where("name IN (?)", filter.Value)
			} else {
				query = query.Where("name LIKE ?", "%"+filter.Value[0]+"%")
			}
		case "code":
			if filter.ExactMatch {
				query = query.Where("code IN (?)", filter.Value)
			} else {
				query = query.Where("code LIKE ?", "%"+filter.Value[0]+"%")
			}
		case "status":
			query = query.Where("status IN (?)", filter.Value)
		case "id":
			query = query.Where("id IN (?)", filter.Value)
		default:
			logs.WarnContextf(ctx, "[org][ListOrgs] invalid filter field: %s", filter.Field)
		}
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	if len(opt.OrderBy) > 0 {
		query = query.Order(strings.Join(opt.OrderBy, ","))
	} else {
		query = query.Order("created_at DESC")
	}

	query = query.Offset(opt.Offset)
	if !opt.ListAll && opt.Limit > 0 {
		query = query.Limit(opt.Limit)
	} else {
		query = query.Limit(150)
	}

	if err := query.Find(&entities).Error; err != nil {
		return nil, 0, err
	}
	return entities, total, nil
}
