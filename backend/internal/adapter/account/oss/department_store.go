//go:build !enterprise

package oss

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	infra_db "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

type departmentStore struct {
	db *gorm.DB
}

func newDepartmentStore(db *gorm.DB) *departmentStore { return &departmentStore{db: db} }

func (s *departmentStore) Get(ctx context.Context, ref DepartmentRef) (*types.Department, error) {
	switch {
	case ref.ID != 0:
		return infra_db.GetDepartmentByID(ctx, s.db, ref.ID)
	case ref.Name != "":
		return infra_db.GetDepartmentByName(ctx, s.db, ref.OrgID, ref.Name)
	}
	return nil, nil
}

func (s *departmentStore) GetByIDs(ctx context.Context, ids []uint) ([]*types.Department, error) {
	return infra_db.GetDepartmentsByIDs(ctx, s.db, ids)
}

func (s *departmentStore) Create(ctx context.Context, d *types.Department) (*types.Department, error) {
	if err := infra_db.CreateDepartment(ctx, s.db, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *departmentStore) Update(ctx context.Context, d *types.Department) (*types.Department, error) {
	if err := infra_db.UpdateDepartment(ctx, s.db, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *departmentStore) Delete(ctx context.Context, id uint) error {
	return infra_db.DeleteDepartment(ctx, s.db, id)
}

func (s *departmentStore) ListDescendantIDs(ctx context.Context, id uint, orgID uint) ([]uint, error) {
	return infra_db.ListDepartmentAndDescendantIDs(ctx, s.db, id, orgID)
}

func (s *departmentStore) BatchCreate(ctx context.Context, departments []*types.Department) error {
	return infra_db.CreateDepartments(ctx, s.db, departments)
}

func (s *departmentStore) GetDefaultRootByOrgID(ctx context.Context, orgID uint) (*types.Department, error) {
	return infra_db.GetDefaultRootDepartmentByOrgID(ctx, s.db, orgID)
}

func (s *departmentStore) UpdateSort(ctx context.Context, id uint, sort uint) error {
	return infra_db.UpdateDepartmentSort(ctx, s.db, id, sort)
}

func (s *departmentStore) List(ctx context.Context, filter account.DepartmentFilter, page account.PageRequest) (*account.PageResult[*types.Department], error) {
	opt := convertDepartmentFilterToPageQuery(filter, page)
	departments, total, err := infra_db.ListDepartment(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}
	return &account.PageResult[*types.Department]{
		Items:    departments,
		Total:    total,
		Page:     page.Page,
		PageSize: page.PageSize,
	}, nil
}

func (s *departmentStore) ListChildren(ctx context.Context, parentID uint) ([]*types.Department, error) {
	return infra_db.ListChildDepartments(ctx, s.db, parentID)
}

func (s *departmentStore) ListSiblings(ctx context.Context, id uint) ([]*types.Department, error) {
	return infra_db.ListDepartmentSiblingsByID(ctx, s.db, id)
}

func convertDepartmentFilterToPageQuery(filter account.DepartmentFilter, page account.PageRequest) *types.PageQuery {
	opt := &types.PageQuery{
		Pagination: types.Pagination{
			Offset: int(page.Page * page.PageSize),
			Limit:  int(page.PageSize),
		},
	}
	if filter.Keyword != "" {
		opt.AddFilter("keyword", filter.Keyword)
	}
	if filter.ParentID != nil {
		opt.AddExactFilter("parent_id", fmt.Sprintf("%d", *filter.ParentID))
	}
	return opt
}
