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
)

var _ contract.UserOrgService = (*userOrgService)(nil)

type userOrgService struct {
	db *gorm.DB
}

func NewUserOrgService(d *gorm.DB) contract.UserOrgService {
	return &userOrgService{db: d}
}

func (s *userOrgService) CreateUserOrg(ctx context.Context, req *contract.CreateUserOrgRequest) (*contract.UserOrg, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}
	if req.UserID == 0 {
		return nil, errors.New("user_id is required")
	}
	if req.OrgID == 0 {
		return nil, errors.New("org_id is required")
	}

	user, err := db.GetUserByID(ctx, s.db, req.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("user not found")
	}

	org, err := db.GetOrgByID(ctx, s.db, req.OrgID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, errors.New("org not found")
	}

	userOrg := &types.UserOrg{
		Uin:       req.UserID,
		UserID:    req.UserID,
		OrgID:     req.OrgID,
		IsDefault: req.IsDefault,
	}

	if err := db.CreateUserOrg(ctx, s.db, userOrg); err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "Duplicate") {
			return nil, errors.New("user org association already exists")
		}
		return nil, err
	}

	return s.enrichUserOrg(ctx, userOrg), nil
}

func (s *userOrgService) GetUserOrg(ctx context.Context, id uint, uin uint) (*contract.UserOrg, error) {
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
		return nil, errors.New("user org not found")
	}

	return s.enrichUserOrg(ctx, userOrg), nil
}

func (s *userOrgService) UpdateUserOrg(ctx context.Context, id uint, req *contract.UpdateUserOrgRequest) (*contract.UserOrg, error) {
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
			return errors.New("user org not found")
		}

		if req.OrgID != nil {
			org, err := db.GetOrgByID(ctx, tx, *req.OrgID)
			if err != nil {
				return err
			}
			if org == nil {
				return errors.New("org not found")
			}
			userOrg.OrgID = *req.OrgID
		}
		if req.IsDefault != nil {
			userOrg.IsDefault = *req.IsDefault
		}

		return db.UpdateUserOrg(ctx, tx, userOrg)
	}); err != nil {
		return nil, err
	}

	return s.enrichUserOrg(ctx, userOrg), nil
}

func (s *userOrgService) DeleteUserOrg(ctx context.Context, id uint) error {
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
			return errors.New("user org not found")
		}
		return db.DeleteUserOrg(ctx, tx, id)
	})
}

func (s *userOrgService) ListUserOrgs(ctx context.Context, req *contract.ListUserOrgsRequest) (*contract.UserOrgList, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}
	req.Fill()

	opt := types.NewPageQuery(*caller, req.Offset, req.Limit)
	opt.ListAll = req.ListAll
	if req.OrgID != nil && *req.OrgID > 0 {
		opt.AddExactFilter("org_id", formatUint(*req.OrgID))
	}
	if req.UserID != nil && *req.UserID > 0 {
		opt.AddExactFilter("user_id", formatUint(*req.UserID))
	}

	userOrgs, total, err := db.ListUserOrgs(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}

	items := make([]contract.UserOrg, 0, len(userOrgs))
	for _, uo := range userOrgs {
		items = append(items, *s.enrichUserOrg(ctx, uo))
	}
	return &contract.UserOrgList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func (s *userOrgService) enrichUserOrg(ctx context.Context, uo *types.UserOrg) *contract.UserOrg {
	result := &contract.UserOrg{
		ID:        uo.ID,
		Uin:       uo.Uin,
		UserID:    uo.UserID,
		OrgID:     uo.OrgID,
		IsDefault: uo.IsDefault,
		CreatedAt: uo.CreatedAt,
		UpdatedAt: uo.UpdatedAt,
	}

	user, _ := db.GetUserByID(ctx, s.db, uo.UserID)
	if user != nil {
		result.UserName = user.Name
		result.UserLogin = user.GithubLogin
		result.AvatarURL = user.AvatarURL
	}

	org, _ := db.GetOrgByID(ctx, s.db, uo.OrgID)
	if org != nil {
		result.OrgName = org.Name
	}

	return result
}

func formatUint(v uint) string {
	return fmt.Sprintf("%d", v)
}
