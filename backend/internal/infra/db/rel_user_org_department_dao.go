package db

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// MemberDeptCond 成员-部门关联查询条件，内嵌 BaseCond 提供通用过滤能力。
// Use pointer embedding so that when no basic filtering is needed, it can be nil.
type MemberDeptCond struct {
	*BaseCond
	Uin          uint
	OrgID        uint
	DepartmentID uint
	IsPrimary    *bool
}

// BuildCondition 将 MemberDeptCond 转换为 GORM 查询条件。
func (c *MemberDeptCond) BuildCondition(db *gorm.DB, tableName string) *gorm.DB {
	if c.BaseCond != nil {
		db = c.BaseCond.BuildCondition(db, tableName)
	}
	if c.Uin != 0 {
		db = db.Where(tableName+".uin = ?", c.Uin)
	}
	if c.OrgID != 0 {
		db = db.Where(tableName+".org_id = ?", c.OrgID)
	}
	if c.DepartmentID != 0 {
		db = db.Where(tableName+".department_id = ?", c.DepartmentID)
	}
	if c.IsPrimary != nil {
		db = db.Where(tableName+".is_primary = ?", *c.IsPrimary)
	}
	return db
}

// MemberDepartmentEntityDao 封装了 MemberDepartment 实体的泛型 DAO。
type MemberDepartmentEntityDao struct {
	*GenericDao[types.MemberDepartment]
}

// NewMemberDepartmentEntityDao creates a MemberDepartmentEntityDao bound to the given DB connection.
func NewMemberDepartmentEntityDao(db *gorm.DB) *MemberDepartmentEntityDao {
	return &MemberDepartmentEntityDao{
		GenericDao: NewGenericDao[types.MemberDepartment](db),
	}
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

// CreateMemberDepartment 创建组织成员部门关联。
func CreateMemberDepartment(ctx context.Context, d *gorm.DB, relation *types.MemberDepartment) error {
	return d.WithContext(ctx).Create(relation).Error
}

// GetMemberDepartmentByID 按 ID 查询组织成员部门关联。
func GetMemberDepartmentByID(ctx context.Context, d *gorm.DB, id uint) (*types.MemberDepartment, error) {
	var entity types.MemberDepartment
	err := d.WithContext(ctx).Where("id = ?", id).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// UpdateMemberDepartment 更新组织成员部门关联。
func UpdateMemberDepartment(ctx context.Context, d *gorm.DB, relation *types.MemberDepartment) error {
	return d.WithContext(ctx).Save(relation).Error
}

// DeleteMemberDepartment 删除组织成员部门关联。
func DeleteMemberDepartment(ctx context.Context, d *gorm.DB, id uint) error {
	return d.WithContext(ctx).Delete(&types.MemberDepartment{}, id).Error
}

// ListMemberDepartmentsByUinAndOrgID 查询组织成员在指定组织下的部门关联列表。
func ListMemberDepartmentsByUinAndOrgID(ctx context.Context, d *gorm.DB, uin uint, orgID uint) ([]*types.MemberDepartment, error) {
	var entities []*types.MemberDepartment
	err := d.WithContext(ctx).
		Where("uin = ? AND org_id = ? AND deleted_at IS NULL", uin, orgID).
		Order("is_primary DESC, id ASC").
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
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

// ListMemberDepartmentsByUinsAndOrgID 批量查询多个组织成员的部门关联。
func ListMemberDepartmentsByUinsAndOrgID(ctx context.Context, d *gorm.DB, uins []uint, orgID uint) (map[uint][]*types.MemberDepartment, error) {
	if len(uins) == 0 {
		return map[uint][]*types.MemberDepartment{}, nil
	}
	var entities []*types.MemberDepartment
	err := d.WithContext(ctx).
		Where("uin IN (?) AND org_id = ? AND deleted_at IS NULL", uins, orgID).
		Order("is_primary DESC, id ASC").
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uint][]*types.MemberDepartment, len(uins))
	for _, entity := range entities {
		result[entity.Uin] = append(result[entity.Uin], entity)
	}
	return result, nil
}
