//go:build !enterprise

package oss

import (
	"context"
	"strconv"

	"github.com/insmtx/Lework/backend/pkg/accounterror"
	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/internal/api/auth"
	"github.com/insmtx/Lework/backend/types"
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
		return nil, accounterror.ErrLoginRequired
	}
	if caller.OrgID == 0 {
		return nil, accounterror.ErrOrgIDRequired
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
		return nil, accounterror.ErrOrgIDRequired
	}
	if orgID != caller.OrgID {
		return nil, accounterror.ErrPermissionDenied
	}
	return caller, nil
}

func verifyAccountOrgEntity(orgID, callerOrgID uint) error {
	if orgID != callerOrgID {
		return accounterror.ErrPermissionDenied
	}
	return nil
}
