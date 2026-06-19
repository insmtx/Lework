package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/worker"
	"github.com/insmtx/Leros/backend/types"
)

func TestWorkerProvisioningEnsuresDefaultWorkerFirst(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.DigitalAssistant{}, &types.WorkerDeployment{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	ctx := context.Background()
	provisioning := NewWorkerProvisioningService(database, nil)
	defaultDeployment, err := provisioning.EnsureDefaultWorkerForOrg(ctx, 12, 34)
	if err != nil {
		t.Fatalf("ensure default worker: %v", err)
	}
	if defaultDeployment.WorkerID != 1 {
		t.Fatalf("default worker_id = %d, want 1", defaultDeployment.WorkerID)
	}
	var defaultAssistant types.DigitalAssistant
	if err := database.First(&defaultAssistant, defaultDeployment.DigitalAssistantID).Error; err != nil {
		t.Fatalf("load default assistant: %v", err)
	}
	if defaultAssistant.Code != "default_o12" {
		t.Fatalf("default assistant code = %q, want default_o12", defaultAssistant.Code)
	}

	assistant := &types.DigitalAssistant{
		Code:    "custom-agent",
		OrgID:   12,
		OwnerID: 34,
		Name:    "Custom Agent",
		Status:  string(contract.DigitalAssistantStatusDraft),
	}
	if err := database.Create(assistant).Error; err != nil {
		t.Fatalf("create assistant: %v", err)
	}
	customDeployment, err := provisioning.EnsureForAssistant(ctx, assistant)
	if err != nil {
		t.Fatalf("ensure custom worker: %v", err)
	}
	if customDeployment.WorkerID != 2 {
		t.Fatalf("custom worker_id = %d, want 2", customDeployment.WorkerID)
	}
}

func TestWorkerProvisioningRebindsLegacyDefaultWorker(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.DigitalAssistant{}, &types.WorkerDeployment{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	ctx := context.Background()
	legacyAssistant := &types.DigitalAssistant{
		Code:    "org_12_default_worker",
		OrgID:   12,
		OwnerID: 34,
		Name:    "Legacy Default Worker",
		Status:  string(contract.DigitalAssistantStatusActive),
	}
	if err := database.Create(legacyAssistant).Error; err != nil {
		t.Fatalf("create legacy assistant: %v", err)
	}
	legacyDeployment := &types.WorkerDeployment{
		OrgID:              12,
		DigitalAssistantID: legacyAssistant.ID,
		WorkerID:           1,
		DeploymentName:     "leros-worker-o12-w1",
		Status:             string(types.WorkerDeploymentStatusReady),
	}
	if err := database.Create(legacyDeployment).Error; err != nil {
		t.Fatalf("create legacy deployment: %v", err)
	}

	provisioning := NewWorkerProvisioningService(database, nil)
	defaultDeployment, err := provisioning.EnsureDefaultWorkerForOrg(ctx, 12, 34)
	if err != nil {
		t.Fatalf("ensure default worker: %v", err)
	}

	var defaultAssistant types.DigitalAssistant
	if err := database.Where("org_id = ? AND code = ?", 12, "default_o12").First(&defaultAssistant).Error; err != nil {
		t.Fatalf("load default assistant: %v", err)
	}
	if defaultDeployment.DigitalAssistantID != defaultAssistant.ID {
		t.Fatalf("deployment assistant_id = %d, want %d", defaultDeployment.DigitalAssistantID, defaultAssistant.ID)
	}
	if defaultDeployment.WorkerID != 1 {
		t.Fatalf("default worker_id = %d, want 1", defaultDeployment.WorkerID)
	}
}

func TestWorkerReconcilerDoesNotRestartProvisioningDeployment(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.DigitalAssistant{}, &types.WorkerDeployment{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	ctx := context.Background()
	assistant := &types.DigitalAssistant{
		Code:   "agent",
		OrgID:  1,
		Name:   "Agent",
		Status: string(contract.DigitalAssistantStatusActive),
	}
	if err := database.Create(assistant).Error; err != nil {
		t.Fatalf("create assistant: %v", err)
	}
	startedAt := time.Now()
	deployment := &types.WorkerDeployment{
		OrgID:              1,
		DigitalAssistantID: assistant.ID,
		WorkerID:           1,
		DeploymentName:     "leros-worker-o1-w1",
		Status:             string(types.WorkerDeploymentStatusProvisioning),
		BootstrapTokenHash: "stable-token-hash",
		LastStartedAt:      &startedAt,
	}
	if err := database.Create(deployment).Error; err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	scheduler := &fakeWorkerScheduler{healthErr: fmt.Errorf("not ready")}
	if err := reconcileWorkerDeployment(ctx, database, scheduler, nil, deployment); err != nil {
		t.Fatalf("reconcile deployment: %v", err)
	}
	if scheduler.startCalls != 0 {
		t.Fatalf("Start calls = %d, want 0", scheduler.startCalls)
	}

	var got types.WorkerDeployment
	if err := database.First(&got, deployment.ID).Error; err != nil {
		t.Fatalf("reload deployment: %v", err)
	}
	if got.BootstrapTokenHash != "stable-token-hash" {
		t.Fatalf("bootstrap hash changed to %q", got.BootstrapTokenHash)
	}
	if got.Status != string(types.WorkerDeploymentStatusProvisioning) {
		t.Fatalf("status = %q, want provisioning", got.Status)
	}
}

type fakeWorkerScheduler struct {
	startCalls int
	healthErr  error
}

func (f *fakeWorkerScheduler) Start(ctx context.Context, spec *worker.WorkerSpec) (*worker.WorkerInstance, error) {
	f.startCalls++
	return &worker.WorkerInstance{ID: spec.ID, WorkerID: spec.ID}, nil
}

func (f *fakeWorkerScheduler) Stop(ctx context.Context, workerID string) error {
	return nil
}

func (f *fakeWorkerScheduler) Health(ctx context.Context, workerID string) error {
	return f.healthErr
}

func (f *fakeWorkerScheduler) List(ctx context.Context) ([]*worker.WorkerInstance, error) {
	return nil, nil
}
