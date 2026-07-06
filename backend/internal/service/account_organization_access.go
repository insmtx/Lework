package service

import (
	"context"
	"errors"
	"strconv"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/types"
)

func uintToFilterValue(v uint) string {
	return strconv.FormatUint(uint64(v), 10)
}

// accountOrganizationService 是多个 account 子域 service 共用的基础结构体。
type accountOrganizationService struct {
	db *gorm.DB
}

func accountOrganizationCaller(ctx context.Context) (*types.Caller, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}
	if caller.OrgID == 0 {
		return nil, errors.New("org not set")
	}
	return caller, nil
}

func requireAccountOrganizationCaller(ctx context.Context) error {
	_, err := accountOrganizationCaller(ctx)
	return err
}

func requireAccountOrgAccess(ctx context.Context, orgID uint) (*types.Caller, error) {
	caller, err := accountOrganizationCaller(ctx)
	if err != nil {
		return nil, err
	}
	if orgID == 0 {
		return nil, errors.New("org_id is required")
	}
	if orgID != caller.OrgID {
		return nil, errors.New("permission denied")
	}
	return caller, nil
}

func verifyAccountOrgEntity(orgID, callerOrgID uint) error {
	if orgID != callerOrgID {
		return errors.New("permission denied")
	}
	return nil
}
