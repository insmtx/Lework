package account

import (
	"context"

	"github.com/insmtx/Lework/backend/types"
)

type WorkerProvisioner interface {
	EnsureDefaultWorkerForOrg(ctx context.Context, orgID, ownerID uint) (*types.WorkerDeployment, error)
}
