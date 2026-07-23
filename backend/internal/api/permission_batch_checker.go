package api

import (
	"context"

	"github.com/insmtx/Leros/backend/internal/api/handler"
	"github.com/insmtx/Leros/backend/internal/service"
	"github.com/insmtx/Leros/backend/types"
)

func NewPermissionBatchChecker(svc *service.PermissionService) handler.PermissionBatchChecker {
	return &permissionBatchChecker{svc: svc}
}

type permissionBatchChecker struct {
	svc *service.PermissionService
}

func (c *permissionBatchChecker) BatchCheckByPublicID(
	ctx context.Context,
	caller types.PermissionCaller,
	items []handler.PermissionBatchCheckItem,
) ([]handler.PermissionBatchCheckResult, error) {
	if len(items) == 0 {
		return nil, nil
	}

	results := make([]handler.PermissionBatchCheckResult, len(items))
	for i, item := range items {
		err := c.svc.GuardByPublicID(ctx, caller, item.ResourceType, item.PublicID, types.Action(item.Action))
		results[i] = handler.PermissionBatchCheckResult{
			Action:       item.Action,
			ResourceType: item.ResourceType,
			PublicID:     item.PublicID,
		}
		if err != nil {
			results[i].Allowed = false
			results[i].Reason = err.Error()
		} else {
			results[i].Allowed = true
			results[i].Reason = "allowed"
		}
	}

	return results, nil
}
