//go:build !enterprise

package oss

import (
	"context"

	"gorm.io/gorm"

	infra_db "github.com/insmtx/Lework/backend/internal/infra/db"
	"github.com/insmtx/Lework/backend/types"
)

type workerStore struct {
	db *gorm.DB
}

func newWorkerStore(db *gorm.DB) *workerStore { return &workerStore{db: db} }

func (s *workerStore) GetDeploymentByOrgWorkerID(ctx context.Context, orgID uint, workerID uint) (*types.WorkerDeployment, error) {
	return infra_db.GetWorkerDeploymentByOrgWorkerID(ctx, s.db, orgID, workerID)
}

func (s *workerStore) GetDefaultDeployment(ctx context.Context, orgID uint) (*types.WorkerDeployment, error) {
	return infra_db.GetDefaultWorkerDeployment(ctx, s.db, orgID)
}

func (s *workerStore) GetDigitalAssistantByID(ctx context.Context, id uint) (*types.DigitalAssistant, error) {
	return infra_db.GetDigitalAssistantByID(ctx, s.db, id)
}

func (s *workerStore) GetDigitalAssistantByPublicID(ctx context.Context, publicID string) (*types.DigitalAssistant, error) {
	return infra_db.GetDigitalAssistantByPublicID(ctx, s.db, publicID)
}
