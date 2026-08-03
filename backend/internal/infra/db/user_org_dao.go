package db

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// UserOrgCond 用户-组织关联查询条件，内嵌 BaseCond 提供通用过滤能力。
// Use pointer embedding so that when no basic filtering is needed, it can be nil.
type UserOrgCond struct {
	*BaseCond
	Uin         uint
	UserID      uint
	OrgID       uint
	ExternalUin uint
	IsDefault   *bool
}

// BuildCondition 将 UserOrgCond 转换为 GORM 查询条件。
func (c *UserOrgCond) BuildCondition(db *gorm.DB, tableName string) *gorm.DB {
	if c.BaseCond != nil {
		db = c.BaseCond.BuildCondition(db, tableName)
	}
	if c.Uin != 0 {
		db = db.Where(tableName+".id = ?", c.Uin)
	}
	if c.UserID != 0 {
		db = db.Where(tableName+".user_id = ?", c.UserID)
	}
	if c.OrgID != 0 {
		db = db.Where(tableName+".org_id = ?", c.OrgID)
	}
	if c.ExternalUin != 0 {
		db = db.Where(tableName+".external_uin = ?", c.ExternalUin)
	}
	if c.IsDefault != nil {
		db = db.Where(tableName+".is_default = ?", *c.IsDefault)
	}
	return db
}

// UserOrgEntityDao 封装了 UserOrg 实体的泛型 DAO。
type UserOrgEntityDao struct {
	*GenericDao[types.UserOrg]
}

// NewUserOrgEntityDao creates a UserOrgEntityDao bound to the given DB connection.
func NewUserOrgEntityDao(db *gorm.DB) *UserOrgEntityDao {
	return &UserOrgEntityDao{
		GenericDao: NewGenericDao[types.UserOrg](db),
	}
}

// GetUinByPublicID 根据 org_id + user public_id 查询 uin。
func GetUinByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string) (uint, error) {
	var uo types.UserOrg
	err := db.WithContext(ctx).
		Table(types.TableNameUserOrg+" AS uo").
		Select("uo.id").
		Joins("INNER JOIN "+types.TableNameUser+" AS u ON u.id = uo.user_id").
		Where("uo.org_id = ? AND u.public_id = ? AND uo.deleted_at IS NULL AND u.deleted_at IS NULL", orgID, publicID).
		First(&uo).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return uo.ID, nil
}

// GetUinsByPublicIDs 根据 org_id + user public_id 列表批量查询对应的 uin。
func GetUinsByPublicIDs(ctx context.Context, db *gorm.DB, orgID uint, publicIDs []string) ([]uint, error) {
	if len(publicIDs) == 0 {
		return nil, nil
	}
	var uos []types.UserOrg
	err := db.WithContext(ctx).
		Table(types.TableNameUserOrg+" AS uo").
		Select("uo.id").
		Joins("INNER JOIN "+types.TableNameUser+" AS u ON u.id = uo.user_id").
		Where("uo.org_id = ? AND u.public_id IN (?) AND uo.deleted_at IS NULL AND u.deleted_at IS NULL", orgID, publicIDs).
		Find(&uos).Error
	if err != nil {
		return nil, err
	}
	result := make([]uint, 0, len(uos))
	for _, uo := range uos {
		result = append(result, uo.ID)
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
		Select("u.public_id AS public_id, uo.id AS uin").
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
					WHERE md.uin = leros_user_org.id
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
