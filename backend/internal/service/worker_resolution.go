package service

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

func resolveRuntimeWorker(ctx context.Context, database *gorm.DB, orgID, assistantID uint, inferrer AssistantInferrer) (uint, uint, error) {
	if database == nil {
		return assistantID, assistantID, nil
	}
	if assistantID > 0 {
		assistant, err := infradb.GetDigitalAssistantByID(ctx, database, assistantID)
		if err != nil {
			return 0, 0, err
		}
		if assistant == nil {
			return 0, 0, errors.New("digital assistant not found")
		}
		if assistant.OrgID != orgID {
			return 0, 0, errors.New("digital assistant organization mismatch")
		}
		if assistant.Status != string(types.DigitalAssistantStatusActive) {
			return 0, 0, fmt.Errorf("digital assistant is not active: %s", assistant.Status)
		}

		deployment, err := infradb.GetWorkerDeploymentByAssistantID(ctx, database, assistantID)
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

// resolveDefaultRuntimeWorker 解析组织级默认 AI 队友的 (assistantID, workerID)。
// 查询顺序：
//  1. DB 中 worker_id=1 的默认 deployment 存在时，直接使用
//  2. 否则用 inferrer 推断的 assistant ID，调 resolveRuntimeWorker 从 DB 二次解析
//     出真实 WorkerID（传 inferrer=nil 避免无限递归）
//  3. inferrer 也未命中或解析失败 → 返回 ErrNoDefaultAssistantInOrg
//
// 修复前 Bug：inferrer 返回值被当成 workerID 用并返回 (0, workerID, nil)，
// 导致 Session.AllocatedAssistantID=<真实 assistant ID> 但 AssistantID=0
// 链路错位。
func resolveDefaultRuntimeWorker(ctx context.Context, database *gorm.DB, orgID uint, inferrer AssistantInferrer) (uint, uint, error) {
	if database != nil {
		deployment, err := infradb.GetDefaultWorkerDeployment(ctx, database, orgID)
		if err != nil {
			return 0, 0, err
		}
		if deployment != nil {
			return deployment.DigitalAssistantID, deployment.WorkerID, nil
		}
	}
	if inferrer != nil {
		inferredAssistantID := inferrer.InferAssignedAssistantID(ctx, orgID, "")
		if inferredAssistantID > 0 {
			return resolveRuntimeWorker(ctx, database, orgID, inferredAssistantID, nil)
		}
	}
	return 0, 0, ErrNoDefaultAssistantInOrg
}

func resolveProjectWorkerID(ctx context.Context, database *gorm.DB, orgID, projectID uint, inferrer AssistantInferrer) (uint, error) {
	if database != nil && projectID > 0 {
		assistantID, err := infradb.ResolveBoundProjectAssistantID(ctx, database, orgID, projectID)
		if err != nil {
			return 0, err
		}
		if assistantID > 0 {
			_, workerID, err := resolveRuntimeWorker(ctx, database, orgID, assistantID, inferrer)
			if err != nil {
				return 0, err
			}
			return workerID, nil
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
		ok, err := infradb.IsProjectAssistantBound(ctx, database, orgID, projectID, requestedID)
		if err != nil {
			return 0, 0, fmt.Errorf("verify project assistant: %w", err)
		}
		if !ok {
			return 0, 0, fmt.Errorf("assistant %d is not a member of project %d", requestedID, projectID)
		}
		return resolveRuntimeWorker(ctx, database, orgID, requestedID, inferrer)
	}
	assistantID, err := infradb.ResolveBoundProjectAssistantID(ctx, database, orgID, projectID)
	if err != nil {
		return 0, 0, err
	}
	if assistantID == 0 {
		return 0, 0, ErrNoDefaultAssistant
	}
	return resolveRuntimeWorker(ctx, database, orgID, assistantID, inferrer)
}

// resolveDefaultBindingWorker 未指定 assistant 时，取项目 resource_bindings 中按绑定顺序第一个 assistant。
// projectID 为 0 时 fallback 到 resolveDefaultRuntimeWorker。
func resolveDefaultBindingWorker(ctx context.Context, database *gorm.DB, orgID, projectID uint, inferrer AssistantInferrer) (uint, uint, error) {
	if projectID == 0 {
		return resolveDefaultRuntimeWorker(ctx, database, orgID, inferrer)
	}
	resource, err := infradb.GetResourceByBizID(ctx, database, orgID, types.ResourceTypeProject, projectID)
	if err != nil {
		return 0, 0, err
	}
	if resource == nil {
		return 0, 0, ErrNoDefaultAssistant
	}
	binding, err := infradb.GetFirstResourceAssistantBinding(ctx, database, resource.ID)
	if err != nil {
		return 0, 0, err
	}
	if binding == nil || binding.AssistantID == nil || *binding.AssistantID == 0 {
		return 0, 0, ErrNoDefaultAssistant
	}
	return resolveRuntimeWorker(ctx, database, orgID, *binding.AssistantID, inferrer)
}

func firstOrDefault(ids []uint) uint {
	for _, id := range ids {
		if id > 0 {
			return id
		}
	}
	return 0
}

func resolveAssistantIDsByPublicID(ctx context.Context, database *gorm.DB, orgID uint, publicIDs []string) ([]uint, error) {
	if len(publicIDs) == 0 {
		return nil, nil
	}
	result := make([]uint, 0, len(publicIDs))
	for _, pid := range publicIDs {
		if pid == "" || pid == "0" {
			continue
		}
		id, err := resolveAssistantByPublicID(ctx, database, orgID, pid)
		if err != nil {
			return nil, err
		}
		if id > 0 {
			result = append(result, id)
		}
	}
	return result, nil
}
