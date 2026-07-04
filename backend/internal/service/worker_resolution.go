package service

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

const legacyDefaultWorkerID uint = 1

func resolveRuntimeWorker(ctx context.Context, database *gorm.DB, orgID, assistantID uint, inferrer AssistantInferrer) (uint, uint, error) {
	if database == nil {
		return assistantID, assistantID, nil
	}
	if assistantID > 0 {
		assistant, err := db.GetDigitalAssistantByID(ctx, database, assistantID)
		if err != nil {
			return 0, 0, err
		}
		if assistant == nil {
			return 0, 0, errors.New("digital assistant not found")
		}
		if assistant.OrgID != orgID {
			return 0, 0, errors.New("digital assistant organization mismatch")
		}
		if caller, _ := auth.FromContext(ctx); caller != nil && caller.Uin > 0 && assistant.OwnerID != caller.Uin {
			return 0, 0, errors.New("digital assistant owner mismatch")
		}
		if assistant.Status != string(contract.DigitalAssistantStatusActive) {
			return 0, 0, fmt.Errorf("digital assistant is not active: %s", assistant.Status)
		}

		deployment, err := db.GetWorkerDeploymentByAssistantID(ctx, database, assistantID)
		if err != nil {
			return 0, 0, err
		}
		if deployment == nil {
			return 0, 0, errors.New("worker deployment not found for assistant")
		}
		if deployment.OrgID != orgID {
			return 0, 0, errors.New("worker deployment organization mismatch")
		}
		if deployment.Status != string(types.WorkerDeploymentStatusReady) {
			return 0, 0, fmt.Errorf("worker deployment is not ready: %s", deployment.Status)
		}
		return assistantID, deployment.WorkerID, nil
	}

	assistantID, workerID, err := resolveDefaultRuntimeWorker(ctx, database, orgID, inferrer)
	if err != nil {
		return 0, 0, err
	}
	return assistantID, workerID, nil
}

func resolveDefaultRuntimeWorker(ctx context.Context, database *gorm.DB, orgID uint, inferrer AssistantInferrer) (uint, uint, error) {
	if database != nil {
		deployment, err := db.GetDefaultWorkerDeployment(ctx, database, orgID)
		if err != nil {
			return 0, 0, err
		}
		if deployment != nil {
			return deployment.DigitalAssistantID, deployment.WorkerID, nil
		}
	}
	if inferrer != nil {
		workerID := inferrer.InferAssignedAssistantID(ctx, orgID, "")
		if workerID > 0 {
			return 0, workerID, nil
		}
	}
	return 0, legacyDefaultWorkerID, nil
}

func resolveProjectWorkerID(ctx context.Context, database *gorm.DB, orgID, projectID uint, inferrer AssistantInferrer) (uint, error) {
	if database != nil && projectID > 0 {
		session, err := db.GetProjectSession(ctx, database, projectID)
		if err != nil {
			return 0, err
		}
		if session != nil && session.OrgID == orgID && session.AllocatedAssistantID > 0 {
			return session.AllocatedAssistantID, nil
		}
	}
	_, workerID, err := resolveDefaultRuntimeWorker(ctx, database, orgID, inferrer)
	return workerID, err
}
