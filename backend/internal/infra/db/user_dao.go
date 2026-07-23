package db

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

func CreateUser(ctx context.Context, d *gorm.DB, user *types.User) error {
	return d.WithContext(ctx).Create(user).Error
}

func GetUserByID(ctx context.Context, d *gorm.DB, id uint) (*types.User, error) {
	var entity types.User
	err := d.WithContext(ctx).Where("id = ?", id).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

func GetUserByPublicID(ctx context.Context, d *gorm.DB, publicID string) (*types.User, error) {
	var entity types.User
	err := d.WithContext(ctx).Where("public_id = ?", publicID).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

func GetUserByEmail(ctx context.Context, d *gorm.DB, email string) (*types.User, error) {
	var entity types.User
	err := d.WithContext(ctx).Where("email = ?", email).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

func GetUserByPhone(ctx context.Context, d *gorm.DB, phone string) (*types.User, error) {
	var entity types.User
	err := d.WithContext(ctx).Where("phone = ?", phone).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

func UpdateUser(ctx context.Context, d *gorm.DB, user *types.User) error {
	return d.WithContext(ctx).Save(user).Error
}

func DeleteUser(ctx context.Context, d *gorm.DB, id uint) error {
	return d.WithContext(ctx).Delete(&types.User{}, id).Error
}

func GetUsersByIDs(ctx context.Context, db *gorm.DB, ids []uint) ([]*types.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var entities []*types.User
	err := db.WithContext(ctx).Where("id IN (?)", ids).Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// GetUserByUin 根据组织成员 Uin 查询用户。
func GetUserByUin(ctx context.Context, db *gorm.DB, uin uint) (*types.User, error) {
	var entity types.User
	err := db.WithContext(ctx).
		Table(types.TableNameUser+" AS u").
		Select("u.*").
		Joins("INNER JOIN "+types.TableNameUserOrg+" AS uo ON uo.user_id = u.id").
		Where("uo.uin = ? AND uo.deleted_at IS NULL AND u.deleted_at IS NULL", uin).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetUsersByUins 批量根据组织成员 Uin 查询用户。
func GetUsersByUins(ctx context.Context, db *gorm.DB, uins []uint) (map[uint]*types.User, error) {
	if len(uins) == 0 {
		return map[uint]*types.User{}, nil
	}
	type row struct {
		Uin uint
		types.User
	}
	var rows []row
	err := db.WithContext(ctx).
		Table(types.TableNameUser+" AS u").
		Select("uo.uin, u.*").
		Joins("INNER JOIN "+types.TableNameUserOrg+" AS uo ON uo.user_id = u.id").
		Where("uo.uin IN (?) AND uo.deleted_at IS NULL AND u.deleted_at IS NULL", uins).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uint]*types.User, len(rows))
	for _, row := range rows {
		user := row.User
		result[row.Uin] = &user
	}
	return result, nil
}

// GetUsersByPublicIDs 批量根据公开 ID 查询用户。
func GetUsersByPublicIDs(ctx context.Context, db *gorm.DB, publicIDs []string) ([]*types.User, error) {
	if len(publicIDs) == 0 {
		return nil, nil
	}
	var entities []*types.User
	err := db.WithContext(ctx).Where("public_id IN (?)", publicIDs).Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

func ListUser(ctx context.Context, d *gorm.DB, opt *types.PageQuery) ([]*types.User, int64, error) {
	var entities []*types.User
	var total int64

	query := d.WithContext(ctx).
		Table(types.TableNameUser + " AS u").
		Where("u.deleted_at IS NULL")
	if opt.OrgID > 0 {
		// 中文注释：用户候选列表必须限定在当前组织，避免跨组织泄露成员信息。
		query = query.Joins("INNER JOIN "+types.TableNameUserOrg+" AS uo ON uo.user_id = u.id").
			Where("uo.org_id = ? AND uo.deleted_at IS NULL", opt.OrgID)
	}

	for _, filter := range opt.Filters {
		switch filter.Field {
		case "keyword":
			kw := strings.TrimSpace(filter.Value[0])
			if kw == "" {
				break
			}
			like := "%" + kw + "%"
			conds := []string{
				"u.name LIKE ?",
				"u.email LIKE ?",
				"u.phone LIKE ?",
			}
			args := []interface{}{like, like, like}
			if digits := phoneSearchDigits(kw); len(digits) >= 3 {
				digitLike := "%" + digits + "%"
				if digitLike != like {
					conds = append(conds, "phone LIKE ?")
					args = append(args, digitLike)
				}
			}
			query = query.Where(strings.Join(conds, " OR "), args...)
		case "name":
			query = query.Where("u.name LIKE ?", "%"+filter.Value[0]+"%")
		case "email":
			query = query.Where("u.email LIKE ?", "%"+filter.Value[0]+"%")
		case "department_id":
			if len(filter.Value) == 0 {
				break
			}
			query = query.Where(`EXISTS (
				SELECT 1 FROM `+types.TableNameMemberDepartment+` AS md
				WHERE md.uin = uo.uin
				  AND md.org_id = uo.org_id
				  AND md.department_id = ?
				  AND md.deleted_at IS NULL
			)`, filter.Value[0])
		default:
			logs.WarnContextf(ctx, "[user][ListUsers] invalid filter field: %s", filter.Field)
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
		query = query.Order("u.created_at DESC")
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

// phoneSearchDigits extracts digits from a search keyword for phone fuzzy matching.
func phoneSearchDigits(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if strings.HasPrefix(digits, "86") && len(digits) > 11 {
		digits = digits[2:]
	}
	return digits
}
