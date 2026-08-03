//go:build !enterprise

package oss

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/insmtx/Leros/backend/pkg/accounterror"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
)

type org struct {
	db                 *gorm.DB
	userRepo           *userRepo
	orgRepo            *orgRepo
	userOrgRepo        *userOrgRepo
	departmentRepo     *departmentRepo
	memberDeptRepo     *memberDeptRepo
	workerProvisioning account.WorkerProvisioner
}

func NewOrg(d *gorm.DB, provisioning account.WorkerProvisioner) *org {
	return &org{
		db:                 d,
		userRepo:           newUserRepo(d),
		orgRepo:            newOrgRepo(d),
		userOrgRepo:        newUserOrgRepo(d),
		departmentRepo:     newDepartmentRepo(d),
		memberDeptRepo:     newMemberDeptRepo(d),
		workerProvisioning: provisioning,
	}
}

// CreateOrg 创建组织。
// Deprecated: 请使用 auth.CreateOrganization 替代。
func (s *org) CreateOrg(ctx context.Context, req *account.CreateOrgInput) (*account.Org, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, accounterror.ErrLoginRequired
	}

	if strings.TrimSpace(req.Name) == "" {
		return nil, accounterror.ErrInvalidArg
	}
	if strings.TrimSpace(req.Code) == "" {
		return nil, accounterror.ErrInvalidArg
	}

	orgType := strings.TrimSpace(req.Type)
	if orgType == "" {
		orgType = "company"
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}

	org := &types.Organization{
		PublicID:     fmt.Sprintf("org_%s", snowflake.GenerateIDBase58()),
		Type:         orgType,
		Code:         strings.TrimSpace(req.Code),
		Name:         strings.TrimSpace(req.Name),
		Status:       status,
		Description:  strings.TrimSpace(req.Description),
		Logo:         strings.TrimSpace(req.Logo),
		Address:      strings.TrimSpace(req.Address),
		Website:      strings.TrimSpace(req.Website),
		CreatedByUin: caller.Uin,
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, err := s.orgRepo.withTx(tx).GetByCode(ctx, org.Code)
		if err != nil {
			return err
		}
		if existing != nil {
			return accounterror.ErrInvalidArg
		}
		if err := s.orgRepo.withTx(tx).Create(ctx, org); err != nil {
			return err
		}

		department := &types.Department{
			Name:     org.Name,
			ParentID: 0,
			Sort:     db.DepartmentSortGap,
			OrgID:    org.ID,
		}
		if err := s.departmentRepo.withTx(tx).Create(ctx, department); err != nil {
			return err
		}

		if s.workerProvisioning != nil {
			if _, err := s.workerProvisioning.EnsureDefaultWorkerForOrg(ctx, org.ID, caller.Uin); err != nil {
				return fmt.Errorf("ensure default worker deployment: %w", err)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return convertToContractOrg(org, nil), nil
}

func (s *org) GetOrg(ctx context.Context, publicID string, code string) (*account.Org, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, accounterror.ErrLoginRequired
	}

	var org *types.Organization
	var err error

	if publicID != "" {
		org, err = s.orgRepo.GetByPublicID(ctx, publicID)
	} else if code != "" {
		org, err = s.orgRepo.GetByCode(ctx, code)
	} else {
		return nil, accounterror.ErrInvalidArg
	}

	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, accounterror.ErrOrgNotFound
	}

	logoMap, _ := resolveSingleOrgLogoMap(ctx, s.db, caller.OrgID, org.Logo)
	return convertToContractOrg(org, logoMap), nil
}

func (s *org) UpdateOrg(ctx context.Context, publicID string, req *account.UpdateOrgInput) (*account.Org, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, accounterror.ErrLoginRequired
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, accounterror.ErrOrgIDRequired
	}

	var org *types.Organization
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		org, err = s.orgRepo.withTx(tx).GetByPublicID(ctx, publicID)
		if err != nil {
			return err
		}
		if org == nil {
			return accounterror.ErrOrgNotFound
		}
		if org.ID != caller.OrgID {
			return accounterror.ErrPermissionDenied
		}

		if req.Name != nil {
			org.Name = strings.TrimSpace(*req.Name)
			if org.Name == "" {
				return errors.New("组织名称不可为空")
			}
		}
		if req.Type != nil {
			org.Type = strings.TrimSpace(*req.Type)
		}
		if req.Status != nil {
			org.Status = strings.TrimSpace(*req.Status)
		}
		if req.Description != nil {
			org.Description = strings.TrimSpace(*req.Description)
		}
		if req.Logo != nil {
			org.Logo = strings.TrimSpace(*req.Logo)
		}
		if req.Address != nil {
			org.Address = strings.TrimSpace(*req.Address)
		}
		if req.Website != nil {
			org.Website = strings.TrimSpace(*req.Website)
		}

		return s.orgRepo.withTx(tx).Update(ctx, org)
	}); err != nil {
		return nil, err
	}

	return convertToContractOrg(org, nil), nil
}

func (s *org) DeleteOrg(ctx context.Context, publicID string) error {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return accounterror.ErrLoginRequired
	}
	if strings.TrimSpace(publicID) == "" {
		return accounterror.ErrOrgIDRequired
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		org, err := s.orgRepo.withTx(tx).GetByPublicID(ctx, publicID)
		if err != nil {
			return err
		}
		if org == nil {
			return accounterror.ErrOrgNotFound
		}
		return s.orgRepo.withTx(tx).Delete(ctx, org.ID)
	})
}

func (s *org) ListOrgs(ctx context.Context, req *account.ListOrgsInput) (*account.OrgList, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, accounterror.ErrLoginRequired
	}
	req.Fill()

	opt := types.NewPageQuery(*caller, req.Offset, req.Limit)
	opt.ListAll = req.ListAll
	if req.Keyword != nil && *req.Keyword != "" {
		opt.AddFilter("keyword", *req.Keyword)
	}
	if req.Status != nil && *req.Status != "" {
		opt.AddFilter("status", *req.Status)
	}

	orgs, total, err := s.orgRepo.List(ctx, opt)
	if err != nil {
		return nil, err
	}

	logoMap := resolveOrgLogoURLs(ctx, s.db, caller.OrgID, orgs)
	items := make([]account.Org, 0, len(orgs))
	for _, org := range orgs {
		items = append(items, *convertToContractOrg(org, logoMap))
	}
	return &account.OrgList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func (s *org) CreateOrgMember(ctx context.Context, req *account.CreateOrgMemberInput) (*account.OrgMember, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 || caller.OrgID == 0 {
		return nil, accounterror.ErrLoginRequired
	}
	if err := s.requireDefaultOrgUser(ctx, caller.Uin, caller.OrgID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.UserID) != "" || strings.TrimSpace(req.OrgID) != "" {
		return s.createExistingOrgMember(ctx, caller.OrgID, req)
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, accounterror.ErrInvalidArg
	}
	phone, err := normalizeOrgMemberPhone(req.Phone)
	if err != nil {
		return nil, err
	}
	departmentIDs := uniqueDepartmentIDs(req.DepartmentIDs)
	if len(departmentIDs) == 0 {
		return nil, errors.New("部门ID列表不能为空")
	}

	var userOrg *types.UserOrg
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		departments, err := s.departmentRepo.withTx(tx).GetByIDs(ctx, departmentIDs)
		if err != nil {
			return err
		}
		departmentByID := make(map[uint]*types.Department, len(departments))
		for _, department := range departments {
			if department.OrgID != caller.OrgID {
				return errors.New("部门不属于该组织")
			}
			departmentByID[department.ID] = department
		}
		if len(departmentByID) != len(departmentIDs) {
			return errors.New("部门不存在")
		}

		user, err := s.userRepo.withTx(tx).GetByPhone(ctx, phone)
		if err != nil {
			return err
		}
		if user != nil {
			existing, err := s.userOrgRepo.withTx(tx).GetByUserIDAndOrgID(ctx, user.ID, caller.OrgID)
			if err != nil {
				return err
			}
			if existing != nil {
				return errors.New("手机号已存在于该组织")
			}
		} else {
			user = &types.User{
				PublicID: fmt.Sprintf("usr_%s", snowflake.GenerateIDBase58()),
				Name:     name,
				Phone:    phone,
			}
			if err := s.userRepo.withTx(tx).Create(ctx, user); err != nil {
				if db.IsUniqueConstraintError(err) {
					return errors.New("手机号已存在")
				}
				return err
			}
		}

		orgCount, err := s.userOrgRepo.withTx(tx).CountByUserID(ctx, user.ID)
		if err != nil {
			return err
		}
		userOrg = &types.UserOrg{
			UserID:    user.ID,
			OrgID:     caller.OrgID,
			IsDefault: orgCount == 0,
		}
		if err := s.userOrgRepo.withTx(tx).Create(ctx, userOrg); err != nil {
			if db.IsUniqueConstraintError(err) {
				return errors.New("组织成员已存在")
			}
			return err
		}

		seen := make(map[uint]bool, len(departmentIDs))
		uniqueDeptIDs := make([]uint, 0, len(departmentIDs))
		for _, deptID := range departmentIDs {
			if !seen[deptID] {
				seen[deptID] = true
				uniqueDeptIDs = append(uniqueDeptIDs, deptID)
			}
		}
		departmentIDs = uniqueDeptIDs

		existing, err := s.memberDeptRepo.withTx(tx).ListByUinAndOrgID(ctx, userOrg.ID, caller.OrgID)
		if err != nil {
			return err
		}
		for _, rel := range existing {
			if seen[rel.DepartmentID] {
				return errors.New("组织成员部门关联已存在")
			}
		}

		relations := make([]*types.MemberDepartment, 0, len(departmentIDs))
		for i, departmentID := range departmentIDs {
			relations = append(relations, &types.MemberDepartment{
				Uin:          userOrg.ID,
				OrgID:        caller.OrgID,
				DepartmentID: departmentID,
				IsPrimary:    i == 0,
			})
		}
		return s.memberDeptRepo.withTx(tx).BatchCreate(ctx, relations)
	}); err != nil {
		return nil, err
	}

	return s.enrichOrgMember(ctx, userOrg), nil
}

func (s *org) createExistingOrgMember(ctx context.Context, callerOrgID uint, req *account.CreateOrgMemberInput) (*account.OrgMember, error) {
	if strings.TrimSpace(req.UserID) == "" {
		return nil, errors.New("用户ID不能为空")
	}
	if strings.TrimSpace(req.OrgID) == "" {
		return nil, errors.New("org_id is required")
	}

	user, err := s.userRepo.GetByPublicID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("用户不存在")
	}

	org, err := s.orgRepo.GetByPublicID(ctx, req.OrgID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, accounterror.ErrOrgNotFound
	}
	if org.ID != callerOrgID {
		return nil, accounterror.ErrPermissionDenied
	}

	userOrg := &types.UserOrg{
		UserID:    user.ID,
		OrgID:     org.ID,
		IsDefault: req.IsDefault,
	}

	if err := s.userOrgRepo.Create(ctx, userOrg); err != nil {
		if db.IsUniqueConstraintError(err) {
			return nil, errors.New("组织成员已存在")
		}
		return nil, err
	}

	return s.enrichOrgMember(ctx, userOrg), nil
}

func (s *org) GetOrgMember(ctx context.Context, id uint, uin uint) (*account.OrgMember, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 || caller.OrgID == 0 {
		return nil, accounterror.ErrLoginRequired
	}

	var userOrg *types.UserOrg
	var err error

	if id > 0 {
		userOrg, err = s.userOrgRepo.GetByID(ctx, id)
	} else if uin > 0 {
		userOrg, err = s.userOrgRepo.GetByUinAndOrgID(ctx, uin, caller.OrgID)
	} else {
		return nil, errors.New("id或uin不能为空")
	}

	if err != nil {
		return nil, err
	}
	if userOrg == nil {
		return nil, errors.New("组织成员不存在")
	}
	if userOrg.OrgID != caller.OrgID {
		return nil, accounterror.ErrPermissionDenied
	}

	return s.enrichOrgMember(ctx, userOrg), nil
}

func (s *org) UpdateOrgMember(ctx context.Context, id uint, req *account.UpdateOrgMemberInput) (*account.OrgMember, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 || caller.OrgID == 0 {
		return nil, accounterror.ErrLoginRequired
	}
	if err := s.requireDefaultOrgUser(ctx, caller.Uin, caller.OrgID); err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, errors.New("id不能为空")
	}

	var userOrg *types.UserOrg
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		userOrg, err = s.userOrgRepo.withTx(tx).GetByID(ctx, id)
		if err != nil {
			return err
		}
		if userOrg == nil {
			return errors.New("组织成员不存在")
		}
		if userOrg.OrgID != caller.OrgID {
			return accounterror.ErrPermissionDenied
		}

		if req.OrgID != nil && strings.TrimSpace(*req.OrgID) != "" {
			org, err := s.orgRepo.withTx(tx).GetByPublicID(ctx, *req.OrgID)
			if err != nil {
				return err
			}
			if org == nil {
				return accounterror.ErrOrgNotFound
			}
			if org.ID != caller.OrgID {
				return accounterror.ErrPermissionDenied
			}
			userOrg.OrgID = org.ID
		}
		if req.IsDefault != nil {
			userOrg.IsDefault = *req.IsDefault
		}

		if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
			user, err := s.userRepo.withTx(tx).GetByID(ctx, userOrg.UserID)
			if err != nil {
				return err
			}
			if user == nil {
				return errors.New("用户不存在")
			}
			user.Name = strings.TrimSpace(*req.Name)
			if err := s.userRepo.withTx(tx).Update(ctx, user); err != nil {
				return err
			}
		}

		if req.DepartmentIDs != nil {
			if len(req.DepartmentIDs) == 0 {
				return errors.New("部门ID列表不可为空")
			}
			if err := s.memberDeptRepo.withTx(tx).DeleteByUinAndOrgID(ctx, userOrg.ID, userOrg.OrgID); err != nil {
				return err
			}
			seen := make(map[uint]bool, len(req.DepartmentIDs))
			relations := make([]*types.MemberDepartment, 0, len(req.DepartmentIDs))
			for i, deptID := range req.DepartmentIDs {
				if deptID == 0 {
					continue
				}
				if seen[deptID] {
					return errors.New("部门ID重复")
				}
				seen[deptID] = true
				relations = append(relations, &types.MemberDepartment{
					Uin:          userOrg.ID,
					OrgID:        userOrg.OrgID,
					DepartmentID: deptID,
					IsPrimary:    i == 0,
				})
			}
			if len(relations) == 0 {
				return errors.New("部门ID列表不可为空")
			}
			if err := s.memberDeptRepo.withTx(tx).BatchCreate(ctx, relations); err != nil {
				return err
			}
		}

		return s.userOrgRepo.withTx(tx).Update(ctx, userOrg)
	}); err != nil {
		return nil, err
	}

	return s.enrichOrgMember(ctx, userOrg), nil
}

func (s *org) ListOrgMembers(ctx context.Context, req *account.ListOrgMembersInput) (*account.OrgMemberList, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 || caller.OrgID == 0 {
		return nil, accounterror.ErrLoginRequired
	}
	req.Fill()

	opt := types.NewPageQuery(*caller, req.Offset, req.Limit)
	opt.ListAll = req.ListAll
	if req.OrgID != nil && *req.OrgID > 0 {
		if *req.OrgID != caller.OrgID {
			return nil, accounterror.ErrPermissionDenied
		}
		opt.AddExactFilter("org_id", fmt.Sprintf("%d", *req.OrgID))
	} else {
		opt.AddExactFilter("org_id", fmt.Sprintf("%d", caller.OrgID))
	}
	if req.UserID != nil && strings.TrimSpace(*req.UserID) != "" {
		user, err := s.userRepo.GetByPublicID(ctx, *req.UserID)
		if err != nil {
			return nil, err
		}
		if user != nil {
			opt.AddExactFilter("user_id", fmt.Sprintf("%d", user.ID))
		}
	}
	if req.DepartmentID != nil && *req.DepartmentID > 0 {
		subDeptIDs, err := s.departmentRepo.ListDescendantIDs(ctx, *req.DepartmentID, caller.OrgID)
		if err != nil {
			return nil, err
		}
		deptFilterValues := make([]string, len(subDeptIDs))
		for i, id := range subDeptIDs {
			deptFilterValues[i] = fmt.Sprintf("%d", id)
		}
		opt.AddExactFilter("department_id", deptFilterValues...)
	}

	userOrgs, total, err := s.userOrgRepo.List(ctx, opt)
	if err != nil {
		return nil, err
	}

	items := make([]account.OrgMember, 0, len(userOrgs))
	for _, uo := range userOrgs {
		items = append(items, *s.enrichOrgMember(ctx, uo))
	}
	return &account.OrgMemberList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func (s *org) enrichOrgMember(ctx context.Context, uo *types.UserOrg) *account.OrgMember {
	result := &account.OrgMember{
		ID:        uo.ID,
		Uin:       uo.ID,
		IsDefault: uo.IsDefault,
		CreatedAt: uo.CreatedAt,
		UpdatedAt: uo.UpdatedAt,
	}

	user, _ := s.userRepo.GetByID(ctx, uo.UserID)
	if user != nil {
		result.UserID = user.PublicID
		result.UserName = user.Name
		result.UserLogin = user.Name
		result.UserPhone = user.Phone
		result.AvatarURL = user.AvatarURL
	}

	org, _ := s.orgRepo.GetByID(ctx, uo.OrgID)
	if org != nil {
		result.OrgID = org.PublicID
		result.OrgName = org.Name
	}

	relations, _ := s.memberDeptRepo.ListByUinAndOrgID(ctx, uo.ID, uo.OrgID)
	departmentIDs := make([]uint, 0, len(relations))
	for _, relation := range relations {
		departmentIDs = append(departmentIDs, relation.DepartmentID)
	}
	departments, _ := s.departmentRepo.GetByIDs(ctx, departmentIDs)
	departmentByID := make(map[uint]*types.Department, len(departments))
	for _, department := range departments {
		departmentByID[department.ID] = department
	}
	result.Departments = make([]account.OrgMemberDepartment, 0, len(relations))
	for _, relation := range relations {
		departmentName := ""
		if department := departmentByID[relation.DepartmentID]; department != nil {
			departmentName = department.Name
		}
		result.Departments = append(result.Departments, account.OrgMemberDepartment{
			DepartmentID: relation.DepartmentID,
			Name:         departmentName,
			IsPrimary:    relation.IsPrimary,
		})
	}

	return result
}

func normalizeOrgMemberPhone(phone string) (string, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", errors.New("手机号不能为空")
	}
	phone = strings.TrimPrefix(phone, "+86")
	phone = strings.TrimPrefix(phone, "86")
	if !regexp.MustCompile(`^1[3-9]\d{9}$`).MatchString(phone) {
		return "", errors.New("手机号格式不正确")
	}
	return phone, nil
}

func uniqueDepartmentIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	result := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func (s *org) requireDefaultOrgUser(ctx context.Context, uin, orgID uint) error {
	org, err := s.orgRepo.GetByID(ctx, orgID)
	if err != nil {
		return err
	}
	if org == nil || org.CreatedByUin != uin {
		return accounterror.ErrPermissionDenied
	}
	return nil
}

func convertToContractOrg(org *types.Organization, logoURLMap map[string]string) *account.Org {
	if org == nil {
		return nil
	}
	logoURL := org.Logo
	if account.IsFilePublicID(logoURL) {
		if resolved, ok := logoURLMap[logoURL]; ok {
			logoURL = resolved
		}
	}
	return &account.Org{
		PublicID:    org.PublicID,
		Type:        org.Type,
		Code:        org.Code,
		Name:        org.Name,
		Status:      org.Status,
		Description: org.Description,
		Logo:        logoURL,
		Address:     org.Address,
		Website:     org.Website,
		CreatedAt:   org.CreatedAt,
		UpdatedAt:   org.UpdatedAt,
	}
}
