package db

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func CreateWorkerDeployment(ctx context.Context, database *gorm.DB, deployment *types.WorkerDeployment) error {
	return database.WithContext(ctx).Create(deployment).Error
}

func GetWorkerDeploymentByAssistantID(ctx context.Context, database *gorm.DB, assistantID uint) (*types.WorkerDeployment, error) {
	var deployment types.WorkerDeployment
	err := database.WithContext(ctx).Where("digital_assistant_id = ?", assistantID).First(&deployment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

func GetWorkerDeploymentByOrgWorkerID(ctx context.Context, database *gorm.DB, orgID, workerID uint) (*types.WorkerDeployment, error) {
	var deployment types.WorkerDeployment
	err := database.WithContext(ctx).Where("org_id = ? AND worker_id = ?", orgID, workerID).First(&deployment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

func GetDefaultWorkerDeployment(ctx context.Context, database *gorm.DB, orgID uint) (*types.WorkerDeployment, error) {
	return GetWorkerDeploymentByOrgWorkerID(ctx, database, orgID, 1)
}

func NextWorkerID(ctx context.Context, database *gorm.DB, orgID uint) (uint, error) {
	var maxID uint
	if err := database.WithContext(ctx).
		Model(&types.WorkerDeployment{}).
		Where("org_id = ?", orgID).
		Select("COALESCE(MAX(worker_id), 0)").
		Scan(&maxID).Error; err != nil {
		return 0, err
	}
	return maxID + 1, nil
}

func ListWorkerDeploymentsByStatuses(ctx context.Context, database *gorm.DB, statuses []string) ([]*types.WorkerDeployment, error) {
	var deployments []*types.WorkerDeployment
	query := database.WithContext(ctx).Order("updated_at ASC")
	if len(statuses) > 0 {
		query = query.Where("status IN ?", statuses)
	}
	if err := query.Find(&deployments).Error; err != nil {
		return nil, err
	}
	return deployments, nil
}

func ListWorkerDeploymentsByOrgAndStatuses(ctx context.Context, database *gorm.DB, orgID uint, statuses []string) ([]*types.WorkerDeployment, error) {
	var deployments []*types.WorkerDeployment
	query := database.WithContext(ctx).Where("org_id = ?", orgID).Order("worker_id ASC")
	if len(statuses) > 0 {
		query = query.Where("status IN ?", statuses)
	}
	if err := query.Find(&deployments).Error; err != nil {
		return nil, err
	}
	return deployments, nil
}

func UpdateWorkerDeployment(ctx context.Context, database *gorm.DB, deployment *types.WorkerDeployment) error {
	return database.WithContext(ctx).Save(deployment).Error
}

func MarkWorkerDeploymentStarted(ctx context.Context, database *gorm.DB, id uint, bootstrapTokenHash string) error {
	now := time.Now()
	updates := struct {
		Status             string
		BootstrapTokenHash string
		LastError          string
		LastStartedAt      *time.Time
		LastReconciledAt   *time.Time
	}{
		Status:             string(types.WorkerDeploymentStatusProvisioning),
		BootstrapTokenHash: bootstrapTokenHash,
		LastError:          "",
		LastStartedAt:      &now,
		LastReconciledAt:   &now,
	}
	return database.WithContext(ctx).Model(&types.WorkerDeployment{}).Where("id = ?", id).Updates(updates).Error
}

func MarkWorkerDeploymentStatus(ctx context.Context, database *gorm.DB, id uint, status string, lastError string) error {
	now := time.Now()
	updates := struct {
		Status           string
		LastError        string
		LastReconciledAt *time.Time
	}{
		Status:           status,
		LastError:        lastError,
		LastReconciledAt: &now,
	}
	return database.WithContext(ctx).Model(&types.WorkerDeployment{}).Where("id = ?", id).Updates(updates).Error
}
