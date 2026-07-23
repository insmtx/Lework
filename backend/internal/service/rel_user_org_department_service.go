package service

import (
	"context"
	"errors"
	"strconv"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

var _ contract.MemberDepartmentService = (*accountOrganizationService)(nil)

// NewMemberDepartmentService 创建组织成员部门关联服务。
func NewMemberDepartmentService(d *gorm.DB, orgRepo account.OrgRepository, deptRepo account.DepartmentRepository) contract.MemberDepartmentService {
	return &accountOrganizationService{db: d, orgRepo: orgRepo, deptRepo: deptRepo}
}

func (s *accountOrganizationService) CreateMemberDepartment(ctx context.Context, req *contract.CreateMemberDepartmentRequest) (*contract.MemberDepartment, error) {
	caller, err := accountOrganizationCaller(ctx)
	if err != nil {
		return nil, err
	}
	if req.Uin == 0 {
		return nil, errors.New("成员Uin不能为空")
	}
	if req.DepartmentID == 0 {
		return nil, errors.New("部门ID不能为空")
	}
	if err := s.verifyMemberDepartmentRefs(ctx, caller.OrgID, req.Uin, req.DepartmentID); err != nil {
		return nil, err
	}

	existing, err := db.ListMemberDepartmentsByUinAndOrgID(ctx, s.db, req.Uin, caller.OrgID)
	if err != nil {
		return nil, err
	}
	for _, rel := range existing {
		if rel.DepartmentID == req.DepartmentID {
			return nil, errors.New("组织成员部门关联已存在")
		}
	}

	relation := &types.MemberDepartment{
		Uin:          req.Uin,
		OrgID:        caller.OrgID,
		DepartmentID: req.DepartmentID,
		IsPrimary:    req.IsPrimary,
	}
	if err := db.CreateMemberDepartment(ctx, s.db, relation); err != nil {
		return nil, err
	}
	return convertToContractMemberDepartment(relation), nil
}

func (s *accountOrganizationService) GetMemberDepartment(ctx context.Context, id uint) (*contract.MemberDepartment, error) {
	if err := requireAccountOrganizationCaller(ctx); err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, errors.New("id不能为空")
	}
	relation, err := db.GetMemberDepartmentByID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if relation == nil {
		return nil, errors.New("成员部门关联不存在")
	}
	return convertToContractMemberDepartment(relation), nil
}

func (s *accountOrganizationService) UpdateMemberDepartment(ctx context.Context, id uint, req *contract.UpdateMemberDepartmentRequest) (*contract.MemberDepartment, error) {
	caller, err := accountOrganizationCaller(ctx)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, errors.New("id不能为空")
	}

	var relation *types.MemberDepartment
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		relation, err = db.GetMemberDepartmentByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if relation == nil {
			return errors.New("成员部门关联不存在")
		}

		nextUin := relation.Uin
		nextDepartmentID := relation.DepartmentID
		if req.Uin != nil {
			if *req.Uin == 0 {
				return errors.New("成员Uin不能为空")
			}
			nextUin = *req.Uin
		}
		if req.DepartmentID != nil {
			if *req.DepartmentID == 0 {
				return errors.New("部门ID不能为空")
			}
			nextDepartmentID = *req.DepartmentID
		}
		if err := s.verifyMemberDepartmentRefs(ctx, caller.OrgID, nextUin, nextDepartmentID); err != nil {
			return err
		}

		relation.Uin = nextUin
		relation.OrgID = caller.OrgID
		relation.DepartmentID = nextDepartmentID

		if req.Uin != nil || req.DepartmentID != nil {
			existing, listErr := db.ListMemberDepartmentsByUinAndOrgID(ctx, tx, nextUin, relation.OrgID)
			if listErr != nil {
				return listErr
			}
			for _, rel := range existing {
				if rel.ID != relation.ID && rel.DepartmentID == nextDepartmentID {
					return errors.New("组织成员部门关联已存在")
				}
			}
		}

		if req.IsPrimary != nil {
			relation.IsPrimary = *req.IsPrimary
		}
		return db.UpdateMemberDepartment(ctx, tx, relation)
	}); err != nil {
		return nil, err
	}
	return convertToContractMemberDepartment(relation), nil
}

func (s *accountOrganizationService) DeleteMemberDepartment(ctx context.Context, id uint) error {
	if err := requireAccountOrganizationCaller(ctx); err != nil {
		return err
	}
	if id == 0 {
		return errors.New("id不能为空")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		relation, err := db.GetMemberDepartmentByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if relation == nil {
			return errors.New("成员部门关联不存在")
		}
		return db.DeleteMemberDepartment(ctx, tx, id)
	})
}

func (s *accountOrganizationService) ListMemberDepartments(ctx context.Context, req *contract.ListMemberDepartmentsRequest) (*contract.MemberDepartmentList, error) {
	caller, err := accountOrganizationCaller(ctx)
	if err != nil {
		return nil, err
	}
	req.Fill()

	opt := types.NewPageQuery(*caller, req.Offset, req.Limit)
	opt.ListAll = req.ListAll
	if req.Uin != nil && *req.Uin > 0 {
		opt.AddExactFilter("uin", uintToFilterValue(*req.Uin))
	}
	if req.DepartmentID != nil && *req.DepartmentID > 0 {
		opt.AddExactFilter("department_id", uintToFilterValue(*req.DepartmentID))
	}
	if req.OrgID != nil && *req.OrgID > 0 {
		opt.AddExactFilter("org_id", uintToFilterValue(*req.OrgID))
	}
	if req.IsPrimary != nil {
		opt.AddExactFilter("is_primary", strconv.FormatBool(*req.IsPrimary))
	}

	relations, total, err := db.ListMemberDepartments(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}
	items := make([]contract.MemberDepartment, 0, len(relations))
	for _, relation := range relations {
		items = append(items, *convertToContractMemberDepartment(relation))
	}
	return &contract.MemberDepartmentList{Total: total, Offset: req.Offset, Limit: req.Limit, Items: items}, nil
}

// verifyMemberDepartmentRefs 校验 uin 和 departmentID 都属于 callerOrgID。
func (s *accountOrganizationService) verifyMemberDepartmentRefs(ctx context.Context, callerOrgID, uin, departmentID uint) error {
	orgMember, err := s.orgRepo.GetOrgMember(ctx, 0, uin)
	if err != nil {
		return err
	}
	if orgMember == nil {
		return errors.New("用户组织不存在")
	}

	department, err := s.deptRepo.GetDepartment(ctx, departmentID)
	if err != nil {
		return err
	}
	if department == nil {
		return errors.New("部门不存在")
	}
	if department.OrgID != callerOrgID {
		return errors.New("部门不属于该用户组织")
	}
	return nil
}

func convertToContractMemberDepartment(relation *types.MemberDepartment) *contract.MemberDepartment {
	if relation == nil {
		return nil
	}
	return &contract.MemberDepartment{
		ID:           relation.ID,
		Uin:          relation.Uin,
		OrgID:        relation.OrgID,
		DepartmentID: relation.DepartmentID,
		IsPrimary:    relation.IsPrimary,
		CreatedAt:    relation.CreatedAt,
		UpdatedAt:    relation.UpdatedAt,
	}
}
