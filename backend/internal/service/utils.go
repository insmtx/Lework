package service

import (
	"context"
	"errors"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/types"
)

func getOrgIDFromContext(ctx context.Context) (uint, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		return 0, errors.New("user not authenticated or org not set")
	}
	return caller.OrgID, nil
}

func verifyOrgPermission(daOrgID, orgID uint) error {
	if daOrgID != orgID {
		return errors.New("permission denied")
	}
	return nil
}

func verifyUserPermission(ownerID, uin uint) error {
	if ownerID != uin {
		return errors.New("permission denied")
	}
	return nil
}

func getCallerFromContext(ctx context.Context) (*types.Caller, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}
	return caller, nil
}
