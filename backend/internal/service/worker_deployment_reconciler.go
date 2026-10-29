package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/worker"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

const (
	defaultWorkerDeploymentReconcileInterval = 10 * time.Second
	defaultWorkerDeploymentProvisionTimeout  = 5 * time.Minute
)

// StartWorkerDeploymentReconciler keeps WorkerDeployment records aligned with runtime workers.
func StartWorkerDeploymentReconciler(
	ctx context.Context,
	database *gorm.DB,
	workerScheduler worker.WorkerScheduler,
	schedulerConfig *config.SchedulerConfig,
	publisher mq.Publisher,
) {
	if database == nil || workerScheduler == nil {
		return
	}

	ticker := time.NewTicker(defaultWorkerDeploymentReconcileInterval)
	defer ticker.Stop()

	reconcileWorkerDeployments(ctx, database, workerScheduler, schedulerConfig, publisher)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcileWorkerDeployments(ctx, database, workerScheduler, schedulerConfig, publisher)
		}
	}
}

func reconcileWorkerDeployments(
	ctx context.Context,
	database *gorm.DB,
	workerScheduler worker.WorkerScheduler,
	schedulerConfig *config.SchedulerConfig,
	publisher mq.Publisher,
) {
	statuses := []string{
		string(types.WorkerDeploymentStatusPending),
		string(types.WorkerDeploymentStatusProvisioning),
		string(types.WorkerDeploymentStatusFailed),
		string(types.WorkerDeploymentStatusReady),
	}
	deployments, err := db.ListWorkerDeploymentsByStatuses(ctx, database, statuses)
	if err != nil {
		logs.WarnContextf(ctx, "list worker deployments failed: %v", err)
		return
	}
	for _, deployment := range deployments {
		if err := reconcileWorkerDeployment(ctx, database, workerScheduler, schedulerConfig, deployment, publisher); err != nil {
			logs.WarnContextf(ctx, "reconcile worker deployment %s failed: %v", deployment.DeploymentName, err)
		}
	}
}

func reconcileWorkerDeployment(
	ctx context.Context,
	database *gorm.DB,
	workerScheduler worker.WorkerScheduler,
	schedulerConfig *config.SchedulerConfig,
	deployment *types.WorkerDeployment,
	publisher mq.Publisher,
) error {
	if deployment == nil {
		return nil
	}
	assistant, err := db.GetDigitalAssistantByID(ctx, database, deployment.DigitalAssistantID)
	if err != nil {
		return err
	}
	if assistant == nil || assistant.Status != string(contract.DigitalAssistantStatusActive) {
		if err := workerScheduler.Stop(ctx, deployment.DeploymentName); err != nil {
			if errors.Is(err, worker.ErrWorkerNotFound) {
				return db.MarkWorkerDeploymentStatus(ctx, database, deployment.ID, string(types.WorkerDeploymentStatusStopped), "")
			}
			return fmt.Errorf("stop inactive worker: %w", err)
		}
		return db.MarkWorkerDeploymentStatus(ctx, database, deployment.ID, string(types.WorkerDeploymentStatusStopped), "")
	}

	if deployment.Status == string(types.WorkerDeploymentStatusReady) {
		needsReconcile, driftFields, err := workerNeedsReconcile(
			ctx, workerScheduler, workerSpec(schedulerConfig, deployment, assistant))
		if err != nil {
			return err
		}
		if needsReconcile {
			logs.Infof("Worker deployment %s drifted from desired spec; reconciling fields=%v",
				deployment.DeploymentName, driftFields)
			return startWorkerDeployment(ctx, database, workerScheduler, schedulerConfig, deployment, assistant)
		}
		if err := workerScheduler.Health(ctx, deployment.DeploymentName); err != nil {
			return db.MarkWorkerDeploymentStatus(ctx, database, deployment.ID, string(types.WorkerDeploymentStatusFailed), err.Error())
		}
		return db.MarkWorkerDeploymentStatus(ctx, database, deployment.ID, string(types.WorkerDeploymentStatusReady), "")
	}

	if deployment.Status == string(types.WorkerDeploymentStatusProvisioning) {
		return reconcileProvisioningWorkerDeployment(ctx, database, workerScheduler, schedulerConfig, deployment, assistant, publisher)
	}

	return startWorkerDeployment(ctx, database, workerScheduler, schedulerConfig, deployment, assistant)
}

func reconcileProvisioningWorkerDeployment(
	ctx context.Context,
	database *gorm.DB,
	workerScheduler worker.WorkerScheduler,
	schedulerConfig *config.SchedulerConfig,
	deployment *types.WorkerDeployment,
	assistant *types.DigitalAssistant,
	publisher mq.Publisher,
) error {
	if err := workerScheduler.Health(ctx, deployment.DeploymentName); err == nil {
		if err := db.MarkWorkerDeploymentStatus(ctx, database, deployment.ID, string(types.WorkerDeploymentStatusReady), ""); err != nil {
			return err
		}
		return nil
	} else {
		if errors.Is(err, worker.ErrWorkerNotFound) {
			logs.Infof("Worker deployment %s runtime instance is missing; restarting", deployment.DeploymentName)
			return startWorkerDeployment(ctx, database, workerScheduler, schedulerConfig, deployment, assistant)
		}
		startedAt := deployment.LastStartedAt
		if startedAt == nil {
			return db.MarkWorkerDeploymentStatus(ctx, database, deployment.ID, string(types.WorkerDeploymentStatusFailed), err.Error())
		}
		if time.Since(*startedAt) > defaultWorkerDeploymentProvisionTimeout {
			return db.MarkWorkerDeploymentStatus(ctx, database, deployment.ID, string(types.WorkerDeploymentStatusFailed), err.Error())
		}
		return db.MarkWorkerDeploymentStatus(ctx, database, deployment.ID, string(types.WorkerDeploymentStatusProvisioning), err.Error())
	}
}

func startWorkerDeployment(
	ctx context.Context,
	database *gorm.DB,
	workerScheduler worker.WorkerScheduler,
	schedulerConfig *config.SchedulerConfig,
	deployment *types.WorkerDeployment,
	assistant *types.DigitalAssistant,
) error {
	spec := workerSpec(schedulerConfig, deployment, assistant)
	bootstrapToken, err := resolveBootstrapToken(ctx, workerScheduler, deployment)
	if err != nil {
		return err
	}
	spec.BootstrapToken = bootstrapToken

	if _, err := workerScheduler.Start(ctx, spec); err != nil {
		_ = db.MarkWorkerDeploymentStatus(ctx, database, deployment.ID, string(types.WorkerDeploymentStatusFailed), err.Error())
		return err
	}
	// Apply 成功后才提交 token hash。反过来会在 Update 失败时留下“DB 已轮转、
	// 线上仍是旧 token”的分叉，老 Pod 重启换票就会 401。
	return db.MarkWorkerDeploymentStarted(ctx, database, deployment.ID, auth.HashBootstrapToken(bootstrapToken))
}

// resolveBootstrapToken 优先复用线上 Deployment 正在使用的 token。
//
// token 会写进 Pod template，轮转一次就等于触发一次 Recreate；只有线上没有 token
// （首次创建）时才生成新的。DB hash 与线上不一致时以线上为准——正在运行的 Pod 用的是
// 线上 token，把它回写进 DB 可以在下一轮自愈，而不是制造一次 401。
func resolveBootstrapToken(
	ctx context.Context,
	workerScheduler worker.WorkerScheduler,
	deployment *types.WorkerDeployment,
) (string, error) {
	if reader, ok := workerScheduler.(worker.WorkerBootstrapTokenReader); ok {
		liveToken, err := reader.CurrentBootstrapToken(ctx, deployment.DeploymentName)
		if err != nil {
			return "", fmt.Errorf("read current bootstrap token: %w", err)
		}
		if liveToken = strings.TrimSpace(liveToken); liveToken != "" {
			return liveToken, nil
		}
	}
	return auth.GenerateBootstrapToken()
}

func workerNeedsReconcile(
	ctx context.Context,
	workerScheduler worker.WorkerScheduler,
	spec *worker.WorkerSpec,
) (bool, []string, error) {
	if reporter, ok := workerScheduler.(worker.WorkerSpecDriftReporter); ok {
		fields, err := reporter.DriftFields(ctx, spec)
		if err != nil {
			return false, nil, err
		}
		return len(fields) > 0, fields, nil
	}
	reconciler, ok := workerScheduler.(worker.WorkerSpecReconciler)
	if !ok {
		return false, nil, nil
	}
	needs, err := reconciler.NeedsReconcile(ctx, spec)
	return needs, nil, err
}

func workerSpec(
	schedulerConfig *config.SchedulerConfig,
	deployment *types.WorkerDeployment,
	assistant *types.DigitalAssistant,
) *worker.WorkerSpec {
	spec := &worker.WorkerSpec{
		ID:         deployment.DeploymentName,
		OrgID:      deployment.OrgID,
		WorkerID:   deployment.WorkerID,
		ServerAddr: schedulerServerAddr(schedulerConfig),
		Image:      schedulerWorkerImage(schedulerConfig),
		EnvType:    worker.WorkerEnvProcess,
	}
	if assistant != nil {
		spec.Name = assistant.Name
	}
	return spec
}

func schedulerServerAddr(schedulerConfig *config.SchedulerConfig) string {
	if schedulerConfig == nil {
		return ""
	}
	return strings.TrimSpace(schedulerConfig.ServerAddr)
}

func schedulerWorkerImage(schedulerConfig *config.SchedulerConfig) string {
	if schedulerConfig == nil {
		return ""
	}
	return strings.TrimSpace(schedulerConfig.WorkerImage)
}
