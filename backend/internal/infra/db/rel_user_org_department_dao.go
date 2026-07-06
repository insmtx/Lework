package db

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// CreateMemberDepartment 创建组织成员部门关联。
func CreateMemberDepartment(ctx context.Context, d *gorm.DB, relation *types.MemberDepartment) error {
	return d.WithContext(ctx).Create(relation).Error
}

// CreateMemberDepartments 批量创建组织成员部门关联。
func CreateMemberDepartments(ctx context.Context, d *gorm.DB, relations []*types.MemberDepartment) error {
	if len(relations) == 0 {
		return nil
	}
	return d.WithContext(ctx).Create(relations).Error
}

// GetMemberDepartmentByID 根据主键获取组织成员部门关联。
func GetMemberDepartmentByID(ctx context.Context, d *gorm.DB, id uint) (*types.MemberDepartment, error) {
	var entity types.MemberDepartment
	err := d.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// UpdateMemberDepartment 保存组织成员部门关联。
func UpdateMemberDepartment(ctx context.Context, d *gorm.DB, relation *types.MemberDepartment) error {
	return d.WithContext(ctx).Save(relation).Error
}

// DeleteMemberDepartment 软删除组织成员部门关联。
func DeleteMemberDepartment(ctx context.Context, d *gorm.DB, id uint) error {
	return d.WithContext(ctx).Delete(&types.MemberDepartment{}, id).Error
}

// DeleteMemberDepartmentsByUin 删除指定组织成员的部门关联。
func DeleteMemberDepartmentsByUin(ctx context.Context, d *gorm.DB, uin uint) error {
	return d.WithContext(ctx).
		Where("uin = ?", uin).
		Delete(&types.MemberDepartment{}).Error
}

// CountMemberDepartments 统计组织成员部门关联。
func CountMemberDepartments(ctx context.Context, d *gorm.DB, opt *types.PageQuery) (int64, error) {
	query := buildMemberDepartmentQuery(ctx, d, opt)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// ListMemberDepartments 分页查询组织成员部门关联。
func ListMemberDepartments(ctx context.Context, d *gorm.DB, opt *types.PageQuery) ([]*types.MemberDepartment, int64, error) {
	var entities []*types.MemberDepartment
	var total int64
	if opt == nil {
		opt = &types.PageQuery{}
	}

	query := buildMemberDepartmentQuery(ctx, d, opt)
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
		query = query.Limit(types.PageMaxCount)
	}

	if err := query.Find(&entities).Error; err != nil {
		return nil, 0, err
	}
	return entities, total, nil
}

// ListMemberDepartmentsByUin 查询指定组织成员的部门关联。
func ListMemberDepartmentsByUin(ctx context.Context, d *gorm.DB, uin uint) ([]*types.MemberDepartment, error) {
	var entities []*types.MemberDepartment
	err := d.WithContext(ctx).
		Where("uin = ? AND deleted_at IS NULL", uin).
		Order("is_primary DESC, id ASC").
		Find(&entities).Error
	return entities, err
}

func buildMemberDepartmentQuery(ctx context.Context, d *gorm.DB, opt *types.PageQuery) *gorm.DB {
	query := d.WithContext(ctx).Table(types.TableNameMemberDepartment).
		Where("deleted_at IS NULL")
	if opt == nil {
		return query
	}

	for _, filter := range opt.Filters {
		switch filter.Field {
		case "id":
			query = query.Where("id IN (?)", filter.Value)
		case "uin":
			query = query.Where("uin IN (?)", filter.Value)
		case "department_id":
			query = query.Where("department_id IN (?)", filter.Value)
		case "org_id":
			query = query.Where("org_id IN (?)", filter.Value)
		case "is_primary":
			query = query.Where("is_primary IN (?)", filter.Value)
		default:
			logs.WarnContextf(ctx, "[member_department][ListMemberDepartments] invalid filter field: %s", filter.Field)
		}
	}
	return query
}
