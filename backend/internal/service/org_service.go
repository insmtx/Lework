package service

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

var _ contract.OrgService = (*orgService)(nil)

type orgService struct {
	db *gorm.DB
}

func NewOrgService(d *gorm.DB) contract.OrgService {
	return &orgService{db: d}
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
		Type:   orgType,
		Code:   strings.TrimSpace(req.Code),
		Name:   strings.TrimSpace(req.Name),
		Status: status,
	}

	if err := db.CreateOrg(ctx, s.db, org); err != nil {
		return nil, err
	}

	return convertToContractOrg(org), nil
}

func (s *orgService) GetOrg(ctx context.Context, id uint, code string) (*contract.Org, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}

	var org *types.Organization
	var err error

	if id > 0 {
		org, err = db.GetOrgByID(ctx, s.db, id)
	} else if code != "" {
		org, err = db.GetOrgByCode(ctx, s.db, code)
	} else {
		return nil, errors.New("id or code is required")
	}

	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, errors.New("org not found")
	}

	return convertToContractOrg(org), nil
}

func (s *orgService) UpdateOrg(ctx context.Context, id uint, req *contract.UpdateOrgRequest) (*contract.Org, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}
	if id == 0 {
		return nil, errors.New("id is required")
	}

	var org *types.Organization
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		org, err = db.GetOrgByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if org == nil {
			return errors.New("org not found")
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

		return db.UpdateOrg(ctx, tx, org)
	}); err != nil {
		return nil, err
	}

	return convertToContractOrg(org), nil
}

func (s *orgService) DeleteOrg(ctx context.Context, id uint) error {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return errors.New("user not authenticated")
	}
	if id == 0 {
		return errors.New("id is required")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		org, err := db.GetOrgByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if org == nil {
			return errors.New("org not found")
		}
		return db.DeleteOrg(ctx, tx, id)
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

func convertToContractOrg(org *types.Organization) *contract.Org {
	if org == nil {
		return nil
	}
	return &contract.Org{
		ID:        org.ID,
		Type:      org.Type,
		Code:      org.Code,
		Name:      org.Name,
		Status:    org.Status,
		CreatedAt: org.CreatedAt,
		UpdatedAt: org.UpdatedAt,
	}
}
