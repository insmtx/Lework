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
	err := db.WithContext(ctx).Where("uin = ?", uin).Order("is_default DESC, id ASC").First(&userOrg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &userOrg, nil
}

// GetUserOrgByUinAndOrgID 获取用户在指定组织下的关联。
func GetUserOrgByUinAndOrgID(ctx context.Context, db *gorm.DB, uin, orgID uint) (*types.UserOrg, error) {
	var userOrg types.UserOrg
	err := db.WithContext(ctx).Where("uin = ? AND org_id = ?", uin, orgID).First(&userOrg).Error
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

// GetUserOrgByUserIDAndOrgID 获取用户在指定组织下的关联。
func GetUserOrgByUserIDAndOrgID(ctx context.Context, db *gorm.DB, userID, orgID uint) (*types.UserOrg, error) {
	var userOrg types.UserOrg
	err := db.WithContext(ctx).Where("user_id = ? AND org_id = ?", userID, orgID).First(&userOrg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &userOrg, nil
}

// GetUinByPublicID 根据 org_id + user public_id 查询 uin。
func GetUinByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string) (uint, error) {
	var uo types.UserOrg
	err := db.WithContext(ctx).
		Table(types.TableNameUserOrg+" AS uo").
		Select("uo.uin").
		Joins("INNER JOIN "+types.TableNameUser+" AS u ON u.id = uo.user_id").
		Where("uo.org_id = ? AND u.public_id = ? AND uo.deleted_at IS NULL AND u.deleted_at IS NULL", orgID, publicID).
		First(&uo).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return uo.Uin, nil
}

// GetUinsByPublicIDs 根据 org_id + user public_id 列表批量查询对应的 uin。
func GetUinsByPublicIDs(ctx context.Context, db *gorm.DB, orgID uint, publicIDs []string) ([]uint, error) {
	if len(publicIDs) == 0 {
		return nil, nil
	}
	var uos []types.UserOrg
	err := db.WithContext(ctx).
		Table(types.TableNameUserOrg+" AS uo").
		Select("uo.uin").
		Joins("INNER JOIN "+types.TableNameUser+" AS u ON u.id = uo.user_id").
		Where("uo.org_id = ? AND u.public_id IN (?) AND uo.deleted_at IS NULL AND u.deleted_at IS NULL", orgID, publicIDs).
		Find(&uos).Error
	if err != nil {
		return nil, err
	}
	result := make([]uint, 0, len(uos))
	for _, uo := range uos {
		result = append(result, uo.Uin)
	}
	return result, nil
}

// GetPublicIDUinMapByPublicIDs 根据 org_id + user public_id 列表返回 public_id -> uin 映射。
// 用于需要保留 public_id 与 uin 对应关系的场景（如按成员角色批量绑定）。
func GetPublicIDUinMapByPublicIDs(ctx context.Context, db *gorm.DB, orgID uint, publicIDs []string) (map[string]uint, error) {
	if len(publicIDs) == 0 {
		return map[string]uint{}, nil
	}
	type publicIDUinRow struct {
		PublicID string
		Uin      uint
	}
	var rows []publicIDUinRow
	err := db.WithContext(ctx).
		Table(types.TableNameUserOrg+" AS uo").
		Select("u.public_id AS public_id, uo.uin AS uin").
		Joins("INNER JOIN "+types.TableNameUser+" AS u ON u.id = uo.user_id").
		Where("uo.org_id = ? AND u.public_id IN (?) AND uo.deleted_at IS NULL AND u.deleted_at IS NULL", orgID, publicIDs).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]uint, len(rows))
	for _, r := range rows {
		result[r.PublicID] = r.Uin
	}
	return result, nil
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

// CountUserOrgsByUserID 统计用户所属组织数量。
func CountUserOrgsByUserID(ctx context.Context, db *gorm.DB, userID uint) (int64, error) {
	var count int64
	err := db.WithContext(ctx).
		Model(&types.UserOrg{}).
		Where("user_id = ?", userID).
		Count(&count).Error
	return count, err
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
		Where("deleted_at IS NULL").
		Where("user_id NOT IN (SELECT id FROM leros_user WHERE email = ?)", "admin@leros.local")

	for _, filter := range opt.Filters {
		switch filter.Field {
		case "org_id":
			query = query.Where("org_id IN (?)", filter.Value)
		case "user_id":
			query = query.Where("user_id IN (?)", filter.Value)
		case "is_default":
			query = query.Where("is_default IN (?)", filter.Value)
		case "department_id":
			query = query.Where(`
				EXISTS (
					SELECT 1 FROM leros_rel_user_org_department md
					WHERE md.uin = leros_user_org.uin
					  AND md.org_id = leros_user_org.org_id
					  AND md.department_id IN (?)
					  AND md.deleted_at IS NULL
				)
			`, filter.Value)
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

// GetUserOrgByExternalUin looks up a user-org mapping by the identity-platform
// UIN ID. Used by the enterprise adapter to resolve the local Uin from an
// identity-issued JWT.
func GetUserOrgByExternalUin(ctx context.Context, db *gorm.DB, externalUin uint) (*types.UserOrg, error) {
	var userOrg types.UserOrg
	err := db.WithContext(ctx).Where("external_uin = ?", externalUin).First(&userOrg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &userOrg, nil
}

// GetUserOrgsByUserIDsAndOrgID 批量根据 userIDs + orgID 查询 user_org 记录。
func GetUserOrgsByUserIDsAndOrgID(ctx context.Context, db *gorm.DB, userIDs []uint, orgID uint) ([]*types.UserOrg, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	var entities []*types.UserOrg
	err := db.WithContext(ctx).
		Where("user_id IN (?) AND org_id = ? AND deleted_at IS NULL", userIDs, orgID).
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}
