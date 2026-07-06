package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
)

var _ contract.OrgService = (*orgService)(nil)

type orgService struct {
	db                 *gorm.DB
	workerProvisioning *WorkerProvisioningService
}

func NewOrgService(d *gorm.DB) contract.OrgService {
	return &orgService{db: d}
}

func NewOrgServiceWithProvisioning(d *gorm.DB, provisioning *WorkerProvisioningService) contract.OrgService {
	return &orgService{db: d, workerProvisioning: provisioning}
}

func (s *orgService) CreateOrg(ctx context.Context, req *contract.CreateOrgRequest) (*contract.Org, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}

	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("name is required")
	}
	if strings.TrimSpace(req.Code) == "" {
		return nil, errors.New("code is required")
	}

	existing, err := db.GetOrgByCode(ctx, s.db, strings.TrimSpace(req.Code))
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New("org code already exists")
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
		PublicID:    fmt.Sprintf("org_%s", snowflake.GenerateIDBase58()),
		Type:        orgType,
		Code:        strings.TrimSpace(req.Code),
		Name:        strings.TrimSpace(req.Name),
		Status:      status,
		Description: strings.TrimSpace(req.Description),
		Logo:        strings.TrimSpace(req.Logo),
		Address:     strings.TrimSpace(req.Address),
		Website:     strings.TrimSpace(req.Website),
		CreatedByUin: caller.Uin,
	}

	if err := db.CreateOrg(ctx, s.db, org); err != nil {
		return nil, err
	}
	if s.workerProvisioning != nil {
		if _, err := s.workerProvisioning.EnsureDefaultWorkerForOrg(ctx, org.ID, caller.Uin); err != nil {
			return nil, fmt.Errorf("ensure default worker deployment: %w", err)
		}
	}

	return convertToContractOrg(org), nil
}

func (s *orgService) GetOrg(ctx context.Context, publicID string, code string) (*contract.Org, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}

	var org *types.Organization
	var err error

	if publicID != "" {
		org, err = db.GetOrgByPublicID(ctx, s.db, publicID)
	} else if code != "" {
		org, err = db.GetOrgByCode(ctx, s.db, code)
	} else {
		return nil, errors.New("public_id or code is required")
	}

	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, errors.New("org not found")
	}

	return convertToContractOrg(org), nil
}

func (s *orgService) UpdateOrg(ctx context.Context, publicID string, req *contract.UpdateOrgRequest) (*contract.Org, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	var org *types.Organization
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		org, err = db.GetOrgByPublicID(ctx, tx, publicID)
		if err != nil {
			return err
		}
		if org == nil {
			return errors.New("org not found")
		}
		if org.ID != caller.OrgID {
			return errors.New("permission denied")
		}

		if req.Name != nil {
			org.Name = strings.TrimSpace(*req.Name)
			if org.Name == "" {
				return errors.New("name cannot be empty")
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

		return db.UpdateOrg(ctx, tx, org)
	}); err != nil {
		return nil, err
	}

	return convertToContractOrg(org), nil
}

func (s *orgService) DeleteOrg(ctx context.Context, publicID string) error {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return errors.New("user not authenticated")
	}
	if strings.TrimSpace(publicID) == "" {
		return errors.New("public_id is required")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		org, err := db.GetOrgByPublicID(ctx, tx, publicID)
		if err != nil {
			return err
		}
		if org == nil {
			return errors.New("org not found")
		}
		return db.DeleteOrg(ctx, tx, org.ID)
	})
}

func (s *orgService) ListOrgs(ctx context.Context, req *contract.ListOrgsRequest) (*contract.OrgList, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
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

	orgs, total, err := db.ListOrgs(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}

	items := make([]contract.Org, 0, len(orgs))
	for _, org := range orgs {
		items = append(items, *convertToContractOrg(org))
	}
	return &contract.OrgList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func (s *orgService) CreateOrgMember(ctx context.Context, req *contract.CreateOrgMemberRequest) (*contract.OrgMember, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, errors.New("user_id is required")
	}
	if strings.TrimSpace(req.OrgID) == "" {
		return nil, errors.New("org_id is required")
	}

	user, err := db.GetUserByPublicID(ctx, s.db, req.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("user not found")
	}

	org, err := db.GetOrgByPublicID(ctx, s.db, req.OrgID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, errors.New("org not found")
	}

	userOrg := &types.UserOrg{
		Uin:       user.ID,
		UserID:    user.ID,
		OrgID:     org.ID,
		IsDefault: req.IsDefault,
	}

	if err := db.CreateUserOrg(ctx, s.db, userOrg); err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "Duplicate") {
			return nil, errors.New("org member already exists")
		}
		return nil, err
	}

	return s.enrichOrgMember(ctx, userOrg), nil
}

func (s *orgService) GetOrgMember(ctx context.Context, id uint, uin uint) (*contract.OrgMember, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}

	var userOrg *types.UserOrg
	var err error

	if id > 0 {
		userOrg, err = db.GetUserOrgByID(ctx, s.db, id)
	} else if uin > 0 {
		userOrg, err = db.GetUserOrgByUin(ctx, s.db, uin)
	} else {
		return nil, errors.New("id or uin is required")
	}

	if err != nil {
		return nil, err
	}
	if userOrg == nil {
		return nil, errors.New("org member not found")
	}

	return s.enrichOrgMember(ctx, userOrg), nil
}

func (s *orgService) UpdateOrgMember(ctx context.Context, id uint, req *contract.UpdateOrgMemberRequest) (*contract.OrgMember, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}
	if id == 0 {
		return nil, errors.New("id is required")
	}

	var userOrg *types.UserOrg
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		userOrg, err = db.GetUserOrgByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if userOrg == nil {
			return errors.New("org member not found")
		}

		if req.OrgID != nil && strings.TrimSpace(*req.OrgID) != "" {
			org, err := db.GetOrgByPublicID(ctx, tx, *req.OrgID)
			if err != nil {
				return err
			}
			if org == nil {
				return errors.New("org not found")
			}
			userOrg.OrgID = org.ID
		}
		if req.IsDefault != nil {
			userOrg.IsDefault = *req.IsDefault
		}

		return db.UpdateUserOrg(ctx, tx, userOrg)
	}); err != nil {
		return nil, err
	}

	return s.enrichOrgMember(ctx, userOrg), nil
}

func (s *orgService) DeleteOrgMember(ctx context.Context, id uint) error {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return errors.New("user not authenticated")
	}
	if id == 0 {
		return errors.New("id is required")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		userOrg, err := db.GetUserOrgByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if userOrg == nil {
			return errors.New("org member not found")
		}
		return db.DeleteUserOrg(ctx, tx, id)
	})
}

func (s *orgService) ListOrgMembers(ctx context.Context, req *contract.ListOrgMembersRequest) (*contract.OrgMemberList, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}
	req.Fill()

	opt := types.NewPageQuery(*caller, req.Offset, req.Limit)
	opt.ListAll = req.ListAll
	if req.OrgID != nil && strings.TrimSpace(*req.OrgID) != "" {
		org, err := db.GetOrgByPublicID(ctx, s.db, *req.OrgID)
		if err != nil {
			return nil, err
		}
		if org != nil {
			opt.AddExactFilter("org_id", fmt.Sprintf("%d", org.ID))
		}
	}
	if req.UserID != nil && strings.TrimSpace(*req.UserID) != "" {
		user, err := db.GetUserByPublicID(ctx, s.db, *req.UserID)
		if err != nil {
			return nil, err
		}
		if user != nil {
			opt.AddExactFilter("user_id", fmt.Sprintf("%d", user.ID))
		}
	}

	userOrgs, total, err := db.ListUserOrgs(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}

	items := make([]contract.OrgMember, 0, len(userOrgs))
	for _, uo := range userOrgs {
		items = append(items, *s.enrichOrgMember(ctx, uo))
	}
	return &contract.OrgMemberList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func (s *orgService) enrichOrgMember(ctx context.Context, uo *types.UserOrg) *contract.OrgMember {
	result := &contract.OrgMember{
		ID:        uo.ID,
		Uin:       uo.Uin,
		IsDefault: uo.IsDefault,
		CreatedAt: uo.CreatedAt,
		UpdatedAt: uo.UpdatedAt,
	}

	user, _ := db.GetUserByID(ctx, s.db, uo.UserID)
	if user != nil {
		result.UserID = user.PublicID
		result.UserName = user.Name
		result.UserLogin = user.GithubLogin
		result.AvatarURL = user.AvatarURL
	}

	org, _ := db.GetOrgByID(ctx, s.db, uo.OrgID)
	if org != nil {
		result.OrgID = org.PublicID
		result.OrgName = org.Name
	}

	return result
}

func convertToContractOrg(org *types.Organization) *contract.Org {
	if org == nil {
		return nil
	}
	return &contract.Org{
		PublicID:    org.PublicID,
		Type:        org.Type,
		Code:        org.Code,
		Name:        org.Name,
		Status:      org.Status,
		Description: org.Description,
		Logo:        org.Logo,
		Address:     org.Address,
		Website:     org.Website,
		CreatedAt:   org.CreatedAt,
		UpdatedAt:   org.UpdatedAt,
	}
}
