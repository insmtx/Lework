package service

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

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

// resolveProjectAssistantWorker 为 task/project session 解析 AI 队友 worker。
// assistantIDs 中取第一个 >0 的值；>0 时校验是该项目 assistant 成员；空时取项目最新 assistant 成员；无则 ErrNoDefaultAssistant。
func resolveProjectAssistantWorker(ctx context.Context, database *gorm.DB, orgID, projectID uint, assistantIDs []uint, inferrer AssistantInferrer) (uint, uint, error) {
	requestedID := firstOrDefault(assistantIDs)
	if database == nil || projectID == 0 {
		return resolveRuntimeWorker(ctx, database, orgID, requestedID, inferrer)
	}
	if requestedID > 0 {
		ok, err := db.IsProjectMember(ctx, database, projectID, requestedID, types.MemberTypeAssistant)
		if err != nil {
			return 0, 0, fmt.Errorf("verify project assistant: %w", err)
		}
		if !ok {
			return 0, 0, fmt.Errorf("assistant %d is not a member of project %d", requestedID, projectID)
		}
		return resolveRuntimeWorker(ctx, database, orgID, requestedID, inferrer)
	}
	member, err := db.GetLatestProjectAssistant(ctx, database, projectID)
	if err != nil {
		return 0, 0, err
	}
	if member == nil {
		return 0, 0, ErrNoDefaultAssistant
	}
	return resolveRuntimeWorker(ctx, database, orgID, member.MemberID, inferrer)
}

func firstOrDefault(ids []uint) uint {
	for _, id := range ids {
		if id > 0 {
			return id
		}
	}
	return 0
}
