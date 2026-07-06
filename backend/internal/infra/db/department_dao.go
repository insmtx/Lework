package db

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// DepartmentSortGap 是组织部门同级排序的默认间隔。
const DepartmentSortGap = 1000

// CreateDepartment 创建组织部门。
func CreateDepartment(ctx context.Context, d *gorm.DB, department *types.Department) error {
	return d.WithContext(ctx).Create(department).Error
}

// CreateDepartments 批量创建组织部门。
func CreateDepartments(ctx context.Context, d *gorm.DB, departments []*types.Department) error {
	if len(departments) == 0 {
		return nil
	}
	return d.WithContext(ctx).Create(departments).Error
}

// GetDepartmentByID 根据主键获取组织部门。
func GetDepartmentByID(ctx context.Context, d *gorm.DB, id uint) (*types.Department, error) {
	var entity types.Department
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

// GetDepartmentByName 根据组织和名称获取组织部门。
func GetDepartmentByName(ctx context.Context, d *gorm.DB, orgID uint, name string) (*types.Department, error) {
	var entity types.Department
	err := d.WithContext(ctx).
		Where("org_id = ? AND name = ? AND deleted_at IS NULL", orgID, name).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetDepartmentsByIDs 根据主键列表批量获取组织部门。
func GetDepartmentsByIDs(ctx context.Context, d *gorm.DB, ids []uint) ([]*types.Department, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var entities []*types.Department
	err := d.WithContext(ctx).
		Where("id IN (?) AND deleted_at IS NULL", ids).
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// UpdateDepartment 保存组织部门。
func UpdateDepartment(ctx context.Context, d *gorm.DB, department *types.Department) error {
	return d.WithContext(ctx).Save(department).Error
}

// UpdateDepartmentSort 更新组织部门排序值。
func UpdateDepartmentSort(ctx context.Context, d *gorm.DB, id uint, sort uint) error {
	return d.WithContext(ctx).
		Model(&types.Department{}).
		Where("id = ?", id).
		Update("sort", sort).Error
}

// DeleteDepartment 软删除组织部门。
func DeleteDepartment(ctx context.Context, d *gorm.DB, id uint) error {
	return d.WithContext(ctx).Delete(&types.Department{}, id).Error
}

// CountDepartments 统计组织部门。
func CountDepartments(ctx context.Context, d *gorm.DB, opt *types.PageQuery) (int64, error) {
	query := buildDepartmentQuery(ctx, d, opt)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// ListDepartments 分页查询组织部门。
func ListDepartments(ctx context.Context, d *gorm.DB, opt *types.PageQuery) ([]*types.Department, int64, error) {
	var entities []*types.Department
	var total int64
	if opt == nil {
		opt = &types.PageQuery{}
	}

	query := buildDepartmentQuery(ctx, d, opt)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	if len(opt.OrderBy) > 0 {
		query = query.Order(strings.Join(opt.OrderBy, ","))
	} else {
		query = query.Order("sort ASC, id ASC")
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

// ListDepartmentSiblings 查询同一父部门下的兄弟部门。
func ListDepartmentSiblings(ctx context.Context, d *gorm.DB, parentID uint, excludeID uint) ([]*types.Department, error) {
	var entities []*types.Department
	query := d.WithContext(ctx).
		Where("parent_id = ? AND deleted_at IS NULL", parentID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	if err := query.Order("sort ASC, id ASC").Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, nil
}

func buildDepartmentQuery(ctx context.Context, d *gorm.DB, opt *types.PageQuery) *gorm.DB {
	query := d.WithContext(ctx).Table(types.TableNameDepartment).
		Where("deleted_at IS NULL")
	if opt == nil {
		return query
	}

	for _, filter := range opt.Filters {
		switch filter.Field {
		case "id":
			query = query.Where("id IN (?)", filter.Value)
		case "org_id":
			query = query.Where("org_id IN (?)", filter.Value)
		case "parent_id":
			query = query.Where("parent_id IN (?)", filter.Value)
		case "name":
			if filter.ExactMatch {
				query = query.Where("name IN (?)", filter.Value)
			} else if len(filter.Value) > 0 {
				query = query.Where("name LIKE ?", "%"+filter.Value[0]+"%")
			}
		case "keyword":
			if len(filter.Value) > 0 {
				query = query.Where("name LIKE ?", "%"+filter.Value[0]+"%")
			}
		default:
			logs.WarnContextf(ctx, "[department][ListDepartments] invalid filter field: %s", filter.Field)
		}
	}
	return query
}
