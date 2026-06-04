package db

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// GetUserOrgByUin 根据UIN获取用户组织
func GetUserOrgByUin(ctx context.Context, db *gorm.DB, uin uint) (*types.UserOrg, error) {
	var userOrg types.UserOrg
	err := db.WithContext(ctx).Where("uin = ?", uin).First(&userOrg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &userOrg, nil
}

// GetUserOrgByUserID 获取用户默认组织（若无默认则取首个）
func GetUserOrgByUserID(ctx context.Context, db *gorm.DB, userID uint) (*types.UserOrg, error) {
	var userOrg types.UserOrg
	// 优先获取默认组织
	err := db.WithContext(ctx).Where("user_id = ? AND is_default = ?", userID, true).First(&userOrg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 若无默认组织，获取首个组织
			err = db.WithContext(ctx).Where("user_id = ?", userID).First(&userOrg).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, nil
				}
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return &userOrg, nil
}

// GetUserOrgsByUserID 获取用户全部组织关联。
func GetUserOrgsByUserID(ctx context.Context, db *gorm.DB, userID uint) ([]*types.UserOrg, error) {
	var userOrgs []*types.UserOrg
	err := db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("is_default DESC, id ASC").
		Find(&userOrgs).Error
	if err != nil {
		return nil, err
	}
	return userOrgs, nil
}

// CreateUserOrg 创建用户组织
func CreateUserOrg(ctx context.Context, db *gorm.DB, userOrg *types.UserOrg) error {
	return db.WithContext(ctx).Create(userOrg).Error
}

// UpdateUserOrg 更新用户组织关联
func UpdateUserOrg(ctx context.Context, db *gorm.DB, userOrg *types.UserOrg) error {
	return db.WithContext(ctx).Save(userOrg).Error
}

// GetUserOrgByID 根据ID获取用户组织关联
func GetUserOrgByID(ctx context.Context, db *gorm.DB, id uint) (*types.UserOrg, error) {
	var userOrg types.UserOrg
	err := db.WithContext(ctx).Where("id = ?", id).First(&userOrg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &userOrg, nil
}

// DeleteUserOrg 删除用户组织
func DeleteUserOrg(ctx context.Context, db *gorm.DB, id uint) error {
	return db.WithContext(ctx).Delete(&types.UserOrg{}, id).Error
}

// ListUserOrgs 分页查询用户组织关联列表
func ListUserOrgs(ctx context.Context, d *gorm.DB, opt *types.PageQuery) ([]*types.UserOrg, int64, error) {
	var entities []*types.UserOrg
	var total int64

	query := d.WithContext(ctx).Table(types.TableNameUserOrg).
		Where("deleted_at IS NULL")

	for _, filter := range opt.Filters {
		switch filter.Field {
		case "org_id":
			query = query.Where("org_id IN (?)", filter.Value)
		case "user_id":
			query = query.Where("user_id IN (?)", filter.Value)
		case "is_default":
			query = query.Where("is_default IN (?)", filter.Value)
		default:
			logs.WarnContextf(ctx, "[user_org][ListUserOrgs] invalid filter field: %s", filter.Field)
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
