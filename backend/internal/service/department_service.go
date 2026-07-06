package service

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

var _ contract.DepartmentService = (*accountDepartmentService)(nil)

type accountDepartmentService struct {
	db *gorm.DB
}

// NewDepartmentService 创建组织部门服务。
func NewDepartmentService(d *gorm.DB) contract.DepartmentService {
	return &accountDepartmentService{db: d}
}

func (s *accountDepartmentService) CreateDepartment(ctx context.Context, req *contract.CreateDepartmentRequest) (*contract.Department, error) {
	if _, err := requireAccountOrgAccess(ctx, req.OrgID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}

	existing, err := db.GetDepartmentByName(ctx, s.db, req.OrgID, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New("department name already exists")
	}

	var parent *types.Department
	if req.ParentID > 0 {
		var err error
		parent, err = db.GetDepartmentByID(ctx, s.db, req.ParentID)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, errors.New("parent department not found")
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

func (s *accountDepartmentService) GetDepartment(ctx context.Context, id uint) (*contract.Department, error) {
	caller, err := accountOrganizationCaller(ctx)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, errors.New("id is required")
	}
	department, err := db.GetDepartmentByID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if department == nil {
		return nil, errors.New("department not found")
	}
	if err := verifyAccountOrgEntity(department.OrgID, caller.OrgID); err != nil {
		return nil, err
	}
	return convertToContractDepartment(department), nil
}

func (s *accountDepartmentService) UpdateDepartment(ctx context.Context, id uint, req *contract.UpdateDepartmentRequest) (*contract.Department, error) {
	caller, err := accountOrganizationCaller(ctx)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, errors.New("id is required")
	}

	var department *types.Department
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		department, err = db.GetDepartmentByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if department == nil {
			return errors.New("department not found")
		}
		if err := verifyAccountOrgEntity(department.OrgID, caller.OrgID); err != nil {
			return err
		}
		if req.Name != nil {
			nextName := strings.TrimSpace(*req.Name)
			if nextName == "" {
				return errors.New("name is required")
			}
			if nextName != department.Name {
				existing, dbErr := db.GetDepartmentByName(ctx, tx, department.OrgID, nextName)
				if dbErr != nil {
					return dbErr
				}
				if existing != nil && existing.ID != department.ID {
					return errors.New("department name already exists")
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
					return errors.New("parent department not found")
				}
				if err := verifyAccountOrgEntity(parent.OrgID, department.OrgID); err != nil {
					return err
				}
				if parent.ID == department.ID || departmentParentIDsContain(parent.ParentIDs, department.ID) {
					return errors.New("department parent creates a cycle")
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
				return errors.New("org_id is required")
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

func (s *accountDepartmentService) DeleteDepartment(ctx context.Context, id uint) error {
	caller, err := accountOrganizationCaller(ctx)
	if err != nil {
		return err
	}
	if id == 0 {
		return errors.New("id is required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		department, err := db.GetDepartmentByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if department == nil {
			return errors.New("department not found")
		}
		if err := verifyAccountOrgEntity(department.OrgID, caller.OrgID); err != nil {
			return err
		}
		children, err := db.ListDepartmentSiblings(ctx, tx, id, 0)
		if err != nil {
			return err
		}
		if len(children) > 0 {
			return errors.New("department has child departments")
		}
		return db.DeleteDepartment(ctx, tx, id)
	})
}

func (s *accountDepartmentService) ListDepartments(ctx context.Context, req *contract.ListDepartmentsRequest) (*contract.DepartmentList, error) {
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

	departments, total, err := db.ListDepartments(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}
	items := make([]contract.Department, 0, len(departments))
	for _, department := range departments {
		items = append(items, *convertToContractDepartment(department))
	}
	return &contract.DepartmentList{Total: total, Offset: req.Offset, Limit: req.Limit, Items: items}, nil
}

func convertToContractDepartment(department *types.Department) *contract.Department {
	if department == nil {
		return nil
	}
	return &contract.Department{
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

func (s *accountDepartmentService) recomputeParentIDsForSubtree(ctx context.Context, tx *gorm.DB, department *types.Department) error {
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
