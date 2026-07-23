//go:build !enterprise

package oss

import (
	"context"
	"errors"
	"strings"

	"github.com/insmtx/Leros/backend/pkg/accounterror"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

type department struct {
	db *gorm.DB
}

// NewDepartment 创建组织部门适配器。
func NewDepartment(d *gorm.DB) *department {
	return &department{db: d}
}

func (s *department) CreateDepartment(ctx context.Context, req *account.CreateDepartmentInput) (*account.Department, error) {
	if _, err := requireAccountOrgAccess(ctx, req.OrgID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, accounterror.ErrInvalidArg
	}

	existing, err := db.GetDepartmentByName(ctx, s.db, req.OrgID, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, accounterror.ErrInvalidArg
	}

	var parent *types.Department
	if req.ParentID > 0 {
		var err error
		parent, err = db.GetDepartmentByID(ctx, s.db, req.ParentID)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, errors.New("父部门不存在")
		}
		if err := verifyAccountOrgEntity(parent.OrgID, req.OrgID); err != nil {
			return nil, err
		}
	}

	sort := req.Sort
	if sort == 0 {
		sort = db.DepartmentSortGap
	}

	department := &types.Department{
		Name:      name,
		ParentID:  req.ParentID,
		ParentIDs: types.BuildDepartmentParentIDs(parent),
		Sort:      sort,
		OrgID:     req.OrgID,
	}
	if err := db.CreateDepartment(ctx, s.db, department); err != nil {
		return nil, err
	}
	return convertToContractDepartment(department), nil
}

func (s *department) GetDepartment(ctx context.Context, id uint) (*account.Department, error) {
	caller, err := accountOrganizationCaller(ctx)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, errors.New("id不能为空")
	}
	department, err := db.GetDepartmentByID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if department == nil {
		return nil, accounterror.ErrInvalidArg
	}
	if err := verifyAccountOrgEntity(department.OrgID, caller.OrgID); err != nil {
		return nil, err
	}
	return convertToContractDepartment(department), nil
}

func (s *department) UpdateDepartment(ctx context.Context, id uint, req *account.UpdateDepartmentInput) (*account.Department, error) {
	caller, err := accountOrganizationCaller(ctx)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, errors.New("id不能为空")
	}

	var department *types.Department
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		department, err = db.GetDepartmentByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if department == nil {
			return accounterror.ErrInvalidArg
		}
		if err := verifyAccountOrgEntity(department.OrgID, caller.OrgID); err != nil {
			return err
		}
		if req.Name != nil {
			nextName := strings.TrimSpace(*req.Name)
			if nextName == "" {
				return accounterror.ErrInvalidArg
			}
			if nextName != department.Name {
				existing, dbErr := db.GetDepartmentByName(ctx, tx, department.OrgID, nextName)
				if dbErr != nil {
					return dbErr
				}
				if existing != nil && existing.ID != department.ID {
					return accounterror.ErrInvalidArg
				}
			}
			department.Name = nextName
		}
		parentIDChanged := req.ParentID != nil && *req.ParentID != department.ParentID
		if req.ParentID != nil && *req.ParentID != department.ParentID {
			if *req.ParentID > 0 {
				parent, dbErr := db.GetDepartmentByID(ctx, tx, *req.ParentID)
				if dbErr != nil {
					return dbErr
				}
				if parent == nil {
					return errors.New("父部门不存在")
				}
				if err := verifyAccountOrgEntity(parent.OrgID, department.OrgID); err != nil {
					return err
				}
				if parent.ID == department.ID || departmentParentIDsContain(parent.ParentIDs, department.ID) {
					return errors.New("部门父级设置形成循环")
				}
				department.ParentIDs = types.BuildDepartmentParentIDs(parent)
			} else {
				department.ParentIDs = nil
			}
			department.ParentID = *req.ParentID
		}
		if req.Sort != nil {
			department.Sort = *req.Sort
		}
		if req.OrgID != nil {
			if *req.OrgID == 0 {
				return errors.New("组织ID不能为空")
			}
			if err := verifyAccountOrgEntity(*req.OrgID, caller.OrgID); err != nil {
				return err
			}
			department.OrgID = *req.OrgID
		}
		if err := db.UpdateDepartment(ctx, tx, department); err != nil {
			return err
		}
		if parentIDChanged {
			return s.recomputeParentIDsForSubtree(ctx, tx, department)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return convertToContractDepartment(department), nil
}

func (s *department) DeleteDepartment(ctx context.Context, id uint) error {
	caller, err := accountOrganizationCaller(ctx)
	if err != nil {
		return err
	}
	if id == 0 {
		return errors.New("id不能为空")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		department, err := db.GetDepartmentByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if department == nil {
			return accounterror.ErrInvalidArg
		}
		if err := verifyAccountOrgEntity(department.OrgID, caller.OrgID); err != nil {
			return err
		}
		if department.ParentID == 0 {
			return errors.New("禁止删除根部门")
		}
		children, err := db.ListDepartmentSiblings(ctx, tx, id, 0)
		if err != nil {
			return err
		}
		if len(children) > 0 {
			return errors.New("部门下存在子部门")
		}
		return db.DeleteDepartment(ctx, tx, id)
	})
}

func (s *department) ListDepartment(ctx context.Context, req *account.ListDepartmentInput) (*account.DepartmentList, error) {
	caller, err := accountOrganizationCaller(ctx)
	if err != nil {
		return nil, err
	}
	req.Fill()

	opt := types.NewPageQuery(*caller, req.Offset, req.Limit)
	opt.ListAll = req.ListAll
	if req.Keyword != nil && strings.TrimSpace(*req.Keyword) != "" {
		opt.AddFilter("keyword", strings.TrimSpace(*req.Keyword))
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		opt.AddExactFilter("name", strings.TrimSpace(*req.Name))
	}
	if req.ParentID != nil {
		opt.AddExactFilter("parent_id", uintToFilterValue(*req.ParentID))
	}
	if req.OrgID != nil && *req.OrgID > 0 {
		if *req.OrgID != caller.OrgID {
			return nil, errors.New("permission denied")
		}
		opt.AddExactFilter("org_id", uintToFilterValue(*req.OrgID))
	} else {
		opt.AddExactFilter("org_id", uintToFilterValue(caller.OrgID))
	}

	departments, total, err := db.ListDepartment(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}
	items := make([]account.Department, 0, len(departments))
	for _, department := range departments {
		items = append(items, *convertToContractDepartment(department))
	}
	return &account.DepartmentList{Total: total, Offset: req.Offset, Limit: req.Limit, Items: items}, nil
}

func convertToContractDepartment(department *types.Department) *account.Department {
	if department == nil {
		return nil
	}
	return &account.Department{
		ID:        department.ID,
		Name:      department.Name,
		ParentID:  department.ParentID,
		ParentIDs: department.ParentIDs,
		Sort:      department.Sort,
		OrgID:     department.OrgID,
		CreatedAt: department.CreatedAt,
		UpdatedAt: department.UpdatedAt,
	}
}

func departmentParentIDsContain(parentIDs []uint, id uint) bool {
	for _, parentID := range parentIDs {
		if parentID == id {
			return true
		}
	}
	return false
}

func (s *department) recomputeParentIDsForSubtree(ctx context.Context, tx *gorm.DB, department *types.Department) error {
	children, err := db.ListDepartmentSiblings(ctx, tx, department.ID, 0)
	if err != nil {
		return err
	}
	for _, child := range children {
		child.ParentIDs = types.BuildDepartmentParentIDs(department)
		if err := db.UpdateDepartment(ctx, tx, child); err != nil {
			return err
		}
		if err := s.recomputeParentIDsForSubtree(ctx, tx, child); err != nil {
			return err
		}
	}
	return nil
}
