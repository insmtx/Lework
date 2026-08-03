package db

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// OrgCond 组织查询条件，内嵌 BaseCond 提供通用过滤能力。
// Use pointer embedding so that when no basic filtering is needed, it can be nil.
type OrgCond struct {
	*BaseCond
	Code   string
	Status string
	Name   string
}

// BuildCondition 将 OrgCond 转换为 GORM 查询条件。
func (c *OrgCond) BuildCondition(db *gorm.DB, tableName string) *gorm.DB {
	if c.BaseCond != nil {
		db = c.BaseCond.BuildCondition(db, tableName)
	}
	if c.Code != "" {
		db = db.Where(tableName+".code = ?", c.Code)
	}
	if c.Status != "" {
		db = db.Where(tableName+".status = ?", c.Status)
	}
	if c.Name != "" {
		db = db.Where(tableName+".name = ?", c.Name)
	}
	return db
}

// OrgEntityDao 封装了 Organization 实体的泛型 DAO。
type OrgEntityDao struct {
	*GenericDao[types.Organization]
}

// NewOrgEntityDao creates an OrgEntityDao bound to the given DB connection.
func NewOrgEntityDao(db *gorm.DB) *OrgEntityDao {
	return &OrgEntityDao{
		GenericDao: NewGenericDao[types.Organization](db),
	}
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
