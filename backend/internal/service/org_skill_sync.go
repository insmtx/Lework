package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

const orgSkillSyncTimeout = 45 * time.Second

func normalizeSkillInstallVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "latest"
	}
	return version
}

func orgSkillInstallPayload(item *types.OrgSkillInstallation) messaging.SkillCommandPayload {
	if item == nil {
		return messaging.SkillCommandPayload{}
	}
	action := strings.TrimSpace(item.Action)
	if action == "" {
		action = "install"
	}
	return messaging.SkillCommandPayload{
		Action:  action,
		Source:  strings.TrimSpace(item.Source),
		SkillID: strings.TrimSpace(item.SkillID),
		Version: normalizeSkillInstallVersion(item.Version),
	}
}

func requestWorkerSkillInstall(ctx context.Context, publisher mq.Publisher, orgID, workerID uint, payload messaging.SkillCommandPayload) error {
	if publisher == nil {
		return nil
	}
	if orgID == 0 || workerID == 0 {
		return nil
	}
	if strings.TrimSpace(payload.SkillID) == "" {
		return nil
	}

	topic, err := messaging.WorkerCommandSubject(orgID, workerID, messaging.LaneSkill)
	if err != nil {
		return fmt.Errorf("build skill topic: %w", err)
	}

	msg := messaging.NewSkillCommand(
		fmt.Sprintf("skill-sync-%s", uuid.New().String()),
		messaging.RouteContext{
			OrgID:    orgID,
			WorkerID: workerID,
		},
		payload,
		"",
	)

	reqCtx, cancel := context.WithTimeout(ctx, orgSkillSyncTimeout)
	defer cancel()
	reply, err := publisher.Request(reqCtx, topic, msg)
	if err != nil {
		return fmt.Errorf("request worker skill install: %w", err)
	}

	var resp messaging.WorkerCommandResult
	if err := json.Unmarshal(reply.Data, &resp); err != nil {
		return fmt.Errorf("unmarshal worker skill install response: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("worker skill install failed: %s", resp.Error)
	}
	return nil
}

func syncOrgSkillsToWorker(ctx context.Context, database *gorm.DB, publisher mq.Publisher, orgID, workerID uint) {
	if database == nil || publisher == nil || orgID == 0 || workerID == 0 {
		return
	}
	items, err := infradb.ListOrgSkillInstallations(ctx, database, orgID)
	if err != nil {
		logs.WarnContextf(ctx, "list org skill installations failed: org=%d worker=%d error=%v", orgID, workerID, err)
		return
	}
	for _, item := range items {
		payload := orgSkillInstallPayload(item)
		if err := requestWorkerSkillInstall(ctx, publisher, orgID, workerID, payload); err != nil {
			logs.WarnContextf(ctx, "sync org skill to worker failed: org=%d worker=%d source=%s skill=%s version=%s error=%v",
				orgID, workerID, item.Source, item.SkillID, item.Version, err)
		}
	}
}

func syncSkillPayloadToOrgWorkers(ctx context.Context, database *gorm.DB, publisher mq.Publisher, orgID uint, payload messaging.SkillCommandPayload, fallbackWorkerID uint) {
	if publisher == nil || orgID == 0 {
		return
	}
	if database == nil {
		if fallbackWorkerID > 0 {
			if err := requestWorkerSkillInstall(ctx, publisher, orgID, fallbackWorkerID, payload); err != nil {
				logs.WarnContextf(ctx, "sync skill to fallback worker failed: org=%d worker=%d skill=%s error=%v",
					orgID, fallbackWorkerID, payload.SkillID, err)
			}
		}
		return
	}
	statuses := []string{
		string(types.WorkerDeploymentStatusReady),
		string(types.WorkerDeploymentStatusProvisioning),
	}
	deployments, err := infradb.ListWorkerDeploymentsByOrgAndStatuses(ctx, database, orgID, statuses)
	if err != nil {
		logs.WarnContextf(ctx, "list org worker deployments for skill sync failed: org=%d error=%v", orgID, err)
		return
	}
	seen := make(map[uint]struct{}, len(deployments)+1)
	for _, deployment := range deployments {
		if deployment == nil || deployment.WorkerID == 0 {
			continue
		}
		seen[deployment.WorkerID] = struct{}{}
		if fallbackWorkerID > 0 && deployment.WorkerID == fallbackWorkerID {
			continue
		}
		if err := requestWorkerSkillInstall(ctx, publisher, orgID, deployment.WorkerID, payload); err != nil {
			logs.WarnContextf(ctx, "sync skill to worker failed: org=%d worker=%d skill=%s error=%v",
				orgID, deployment.WorkerID, payload.SkillID, err)
		}
	}
	if fallbackWorkerID > 0 {
		if _, ok := seen[fallbackWorkerID]; ok {
			return
		}
		if err := requestWorkerSkillInstall(ctx, publisher, orgID, fallbackWorkerID, payload); err != nil {
			logs.WarnContextf(ctx, "sync skill to fallback worker failed: org=%d worker=%d skill=%s error=%v",
				orgID, fallbackWorkerID, payload.SkillID, err)
		}
	}
}

func publishSkillPayloadToOrgWorkers(ctx context.Context, database *gorm.DB, publisher mq.Publisher, orgID uint, payload messaging.SkillCommandPayload, fallbackWorkerID uint) {
	if publisher == nil || orgID == 0 {
		return
	}
	if database == nil {
		if fallbackWorkerID > 0 {
			publishSkillPayloadToWorker(ctx, publisher, orgID, fallbackWorkerID, payload)
		}
		return
	}
	statuses := []string{
		string(types.WorkerDeploymentStatusReady),
		string(types.WorkerDeploymentStatusProvisioning),
	}
	deployments, err := infradb.ListWorkerDeploymentsByOrgAndStatuses(ctx, database, orgID, statuses)
	if err != nil {
		logs.WarnContextf(ctx, "list org worker deployments for skill publish failed: org=%d error=%v", orgID, err)
		return
	}
	seen := make(map[uint]struct{}, len(deployments)+1)
	for _, deployment := range deployments {
		if deployment == nil || deployment.WorkerID == 0 {
			continue
		}
		seen[deployment.WorkerID] = struct{}{}
		publishSkillPayloadToWorker(ctx, publisher, orgID, deployment.WorkerID, payload)
	}
	if fallbackWorkerID > 0 {
		if _, ok := seen[fallbackWorkerID]; ok {
			return
		}
		publishSkillPayloadToWorker(ctx, publisher, orgID, fallbackWorkerID, payload)
	}
}

func publishSkillPayloadToWorker(ctx context.Context, publisher mq.Publisher, orgID, workerID uint, payload messaging.SkillCommandPayload) {
	topic, err := messaging.WorkerCommandSubject(orgID, workerID, messaging.LaneSkill)
	if err != nil {
		logs.WarnContextf(ctx, "build skill publish topic failed: org=%d worker=%d error=%v", orgID, workerID, err)
		return
	}
	msg := messaging.NewSkillCommand(
		fmt.Sprintf("skill-publish-%s", uuid.New().String()),
		messaging.RouteContext{
			OrgID:    orgID,
			WorkerID: workerID,
		},
		payload,
		"",
	)
	if err := publisher.Publish(ctx, topic, msg); err != nil {
		logs.WarnContextf(ctx, "publish skill command failed: org=%d worker=%d action=%s skill=%s error=%v",
			orgID, workerID, payload.Action, firstNonEmpty(payload.Name, payload.SkillID), err)
	}
}
