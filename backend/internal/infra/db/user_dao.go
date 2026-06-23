package db

import (
	"context"
	"errors"
	"strings"

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

func GetUserByGithubLogin(ctx context.Context, d *gorm.DB, githubLogin string) (*types.User, error) {
	var entity types.User
	err := d.WithContext(ctx).Where("github_login = ?", githubLogin).First(&entity).Error
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

func ListUsers(ctx context.Context, d *gorm.DB, opt *types.PageQuery) ([]*types.User, int64, error) {
	var entities []*types.User
	var total int64

	query := d.WithContext(ctx).Table(types.TableNameUser).
		Where("deleted_at IS NULL")

	for _, filter := range opt.Filters {
		switch filter.Field {
		case "keyword":
			query = query.Where("name LIKE ? OR github_login LIKE ? OR email LIKE ?",
				"%"+filter.Value[0]+"%", "%"+filter.Value[0]+"%", "%"+filter.Value[0]+"%")
		case "name":
			query = query.Where("name LIKE ?", "%"+filter.Value[0]+"%")
		case "github_login":
			if filter.ExactMatch {
				query = query.Where("github_login IN (?)", filter.Value)
			} else {
				query = query.Where("github_login LIKE ?", "%"+filter.Value[0]+"%")
			}
		case "email":
			query = query.Where("email LIKE ?", "%"+filter.Value[0]+"%")
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
