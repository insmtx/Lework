package account

import (
	"context"

	"github.com/insmtx/Leros/backend/types"
)

type WorkerProvisioner interface {
	EnsureDefaultWorkerForOrg(ctx context.Context, orgID, ownerID uint) (*types.WorkerDeployment, error)
}
