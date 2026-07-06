package service

import (
	"context"
	"errors"
	"strconv"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

var _ contract.MemberDepartmentService = (*accountOrganizationService)(nil)

// NewMemberDepartmentService 创建组织成员部门关联服务。
func NewMemberDepartmentService(d *gorm.DB) contract.MemberDepartmentService {
	return &accountOrganizationService{db: d}
}

func (s *accountOrganizationService) CreateMemberDepartment(ctx context.Context, req *contract.CreateMemberDepartmentRequest) (*contract.MemberDepartment, error) {
	caller, err := accountOrganizationCaller(ctx)
	if err != nil {
		return nil, err
	}
	if req.Uin == 0 {
		return nil, errors.New("uin is required")
	}
	if req.DepartmentID == 0 {
		return nil, errors.New("department_id is required")
	}
	userOrg, err := s.verifyMemberDepartmentRefs(ctx, s.db, caller.OrgID, req.Uin, req.DepartmentID)
	if err != nil {
		return nil, err
	}

	relation := &types.MemberDepartment{
		Uin:          req.Uin,
		OrgID:        userOrg.OrgID,
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
		return nil, errors.New("id is required")
	}
	relation, err := db.GetMemberDepartmentByID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if relation == nil {
		return nil, errors.New("member department relation not found")
	}
	return convertToContractMemberDepartment(relation), nil
}

func (s *accountOrganizationService) UpdateMemberDepartment(ctx context.Context, id uint, req *contract.UpdateMemberDepartmentRequest) (*contract.MemberDepartment, error) {
	caller, err := accountOrganizationCaller(ctx)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, errors.New("id is required")
	}

	var relation *types.MemberDepartment
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		relation, err = db.GetMemberDepartmentByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if relation == nil {
			return errors.New("member department relation not found")
		}

		nextUin := relation.Uin
		nextDepartmentID := relation.DepartmentID
		if req.Uin != nil {
			if *req.Uin == 0 {
				return errors.New("uin is required")
			}
			nextUin = *req.Uin
		}
		if req.DepartmentID != nil {
			if *req.DepartmentID == 0 {
				return errors.New("department_id is required")
			}
			nextDepartmentID = *req.DepartmentID
		}
		userOrg, verifyErr := s.verifyMemberDepartmentRefs(ctx, tx, caller.OrgID, nextUin, nextDepartmentID)
		if verifyErr != nil {
			return verifyErr
		}

		relation.Uin = nextUin
		relation.OrgID = userOrg.OrgID
		relation.DepartmentID = nextDepartmentID
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
		return errors.New("id is required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		relation, err := db.GetMemberDepartmentByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if relation == nil {
			return errors.New("member department relation not found")
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

// verifyMemberDepartmentRefs 校验 uin 和 departmentID 都属于 callerOrgID，返回查到的 UserOrg 供调用方使用。
func (s *accountOrganizationService) verifyMemberDepartmentRefs(ctx context.Context, tx *gorm.DB, callerOrgID, uin, departmentID uint) (*types.UserOrg, error) {
	userOrg, err := db.GetUserOrgByUin(ctx, tx, uin)
	if err != nil {
		return nil, err
	}
	if userOrg == nil {
		return nil, errors.New("user org not found")
	}
	if err := verifyAccountOrgEntity(userOrg.OrgID, callerOrgID); err != nil {
		return nil, err
	}

	department, err := db.GetDepartmentByID(ctx, tx, departmentID)
	if err != nil {
		return nil, err
	}
	if department == nil {
		return nil, errors.New("department not found")
	}
	if err := verifyAccountOrgEntity(department.OrgID, userOrg.OrgID); err != nil {
		return nil, errors.New("department does not belong to user org")
	}
	return userOrg, nil
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
