package db

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// DepartmentSortGap 是组织部门同级排序的默认间隔。
const DepartmentSortGap = 1000

// DepartmentCond 部门查询条件，内嵌 BaseCond 提供通用过滤能力。
// Use pointer embedding so that when no basic filtering is needed, it can be nil.
type DepartmentCond struct {
	*BaseCond
	Name     string
	OrgID    uint
	ParentID uint
}

// BuildCondition 将 DepartmentCond 转换为 GORM 查询条件。
func (c *DepartmentCond) BuildCondition(db *gorm.DB, tableName string) *gorm.DB {
	if c.BaseCond != nil {
		db = c.BaseCond.BuildCondition(db, tableName)
	}
	if c.Name != "" {
		db = db.Where(tableName+".name = ?", c.Name)
	}
	if c.OrgID != 0 {
		db = db.Where(tableName+".org_id = ?", c.OrgID)
	}
	if c.ParentID != 0 {
		db = db.Where(tableName+".parent_id = ?", c.ParentID)
	}
	return db
}

// DepartmentEntityDao 封装了 Department 实体的泛型 DAO。
type DepartmentEntityDao struct {
	*GenericDao[types.Department]
}

// NewDepartmentEntityDao creates a DepartmentEntityDao bound to the given DB connection.
func NewDepartmentEntityDao(db *gorm.DB) *DepartmentEntityDao {
	return &DepartmentEntityDao{
		GenericDao: NewGenericDao[types.Department](db),
	}
}

// ListDepartment 分页查询组织部门。
func ListDepartment(ctx context.Context, d *gorm.DB, opt *types.PageQuery) ([]*types.Department, int64, error) {
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

// ListDepartmentAndDescendantIDs 查询指定部门及其所有子部门的 ID。
func ListDepartmentAndDescendantIDs(ctx context.Context, d *gorm.DB, deptID, orgID uint) ([]uint, error) {
	var ids []uint
	// CAST 需要在外部将 uint 转成字符串数组 JSON 格式
	condition := fmt.Sprintf(`[%d]`, deptID)
	err := d.WithContext(ctx).Table(types.TableNameDepartment).
		Select("id").
		Where("org_id = ? AND deleted_at IS NULL", orgID).
		Where("id = ? OR parent_ids @> CAST(? AS jsonb)", deptID, condition).
		Find(&ids).Error
	return ids, err
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
			logs.WarnContextf(ctx, "[department][ListDepartment] invalid filter field: %s", filter.Field)
		}
	}
	return query
}
