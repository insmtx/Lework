//go:build !enterprise

package oss

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

type departmentRepo struct {
	dao *db.DepartmentEntityDao
}

func newDepartmentRepo(d *gorm.DB) *departmentRepo {
	return &departmentRepo{dao: db.NewDepartmentEntityDao(d)}
}

func (r *departmentRepo) withTx(tx *gorm.DB) *departmentRepo {
	return &departmentRepo{dao: &db.DepartmentEntityDao{GenericDao: r.dao.WithTx(tx)}}
}

func (r *departmentRepo) db() *gorm.DB {
	return r.dao.DB()
}

// GetByRef resolves a department by the first non-zero field in the reference.
func (r *departmentRepo) GetByRef(ctx context.Context, ref account.DepartmentRef) (*types.Department, error) {
	var cond *db.DepartmentCond
	switch {
	case ref.ID != 0:
		cond = &db.DepartmentCond{BaseCond: &db.BaseCond{ID: ref.ID}}
	case ref.Name != "":
		cond = &db.DepartmentCond{Name: ref.Name, OrgID: ref.OrgID}
	default:
		return nil, nil
	}
	return r.dao.GetByCond(ctx, cond)
}

func (r *departmentRepo) GetByID(ctx context.Context, id uint) (*types.Department, error) {
	return r.dao.GetByCond(ctx, &db.DepartmentCond{BaseCond: &db.BaseCond{ID: id}})
}

func (r *departmentRepo) GetByName(ctx context.Context, orgID uint, name string) (*types.Department, error) {
	return r.dao.GetByCond(ctx, &db.DepartmentCond{OrgID: orgID, Name: name})
}

func (r *departmentRepo) GetByIDs(ctx context.Context, ids []uint) ([]*types.Department, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return r.dao.ListByCond(ctx, &db.DepartmentCond{BaseCond: &db.BaseCond{IDs: ids}})
}

func (r *departmentRepo) Create(ctx context.Context, d *types.Department) error {
	return r.dao.Insert(ctx, d)
}

func (r *departmentRepo) BatchCreate(ctx context.Context, departments []*types.Department) error {
	if len(departments) == 0 {
		return nil
	}
	return r.dao.DB().WithContext(ctx).Create(&departments).Error
}

func (r *departmentRepo) Update(ctx context.Context, d *types.Department) error {
	return r.dao.Update(ctx, d)
}

func (r *departmentRepo) Delete(ctx context.Context, id uint) error {
	return r.dao.Delete(ctx, id)
}

// List paginates departments with filtering via PageQuery.
func (r *departmentRepo) List(ctx context.Context, opt *types.PageQuery) ([]*types.Department, int64, error) {
	return db.ListDepartment(ctx, r.dao.DB(), opt)
}

// ListChildren returns child departments of the given parent.
func (r *departmentRepo) ListChildren(ctx context.Context, parentID uint) ([]*types.Department, error) {
	return r.dao.ListByCond(ctx, &db.DepartmentCond{ParentID: parentID, BaseCond: &db.BaseCond{OrderBy: "sort ASC, id ASC"}})
}

// ListSiblings returns sibling departments of the given department.
func (r *departmentRepo) ListSiblings(ctx context.Context, id uint) ([]*types.Department, error) {
	dept, err := r.GetByID(ctx, id)
	if err != nil || dept == nil {
		return nil, err
	}
	return r.dao.ListByCond(ctx, &db.DepartmentCond{ParentID: dept.ParentID, BaseCond: &db.BaseCond{OrderBy: "sort ASC, id ASC"}})
}

// ListDescendantIDs returns all descendant department IDs including the given department.
func (r *departmentRepo) ListDescendantIDs(ctx context.Context, id uint, orgID uint) ([]uint, error) {
	return db.ListDepartmentAndDescendantIDs(ctx, r.dao.DB(), id, orgID)
}

// GetDefaultRoot returns the default root department for an organization.
func (r *departmentRepo) GetDefaultRoot(ctx context.Context, orgID uint) (*types.Department, error) {
	var dept types.Department
	err := r.dao.DB().WithContext(ctx).
		Where("org_id = ? AND parent_id = 0", orgID).
		Order(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: clause.Column{Name: "sort"}, Desc: false},
			{Column: clause.Column{Name: "id"}, Desc: false},
		}}).
		First(&dept).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &dept, nil
}
