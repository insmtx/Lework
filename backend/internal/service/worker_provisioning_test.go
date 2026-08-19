package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
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
	if defaultDeployment.PublicID == "" {
		t.Fatal("default worker public_id is empty")
	}
	var defaultAssistant types.DigitalAssistant
	if err := database.First(&defaultAssistant, defaultDeployment.DigitalAssistantID).Error; err != nil {
		t.Fatalf("load default assistant: %v", err)
	}
	if defaultAssistant.PublicID != "assistant_default_o12" {
		t.Fatalf("default assistant public_id = %q, want assistant_default_o12", defaultAssistant.PublicID)
	}
	defaultDeploymentAgain, err := provisioning.EnsureDefaultWorkerForOrg(ctx, 12, 34)
	if err != nil {
		t.Fatalf("ensure default worker again: %v", err)
	}
	if defaultDeploymentAgain.ID != defaultDeployment.ID {
		t.Fatalf("default deployment id = %d, want %d", defaultDeploymentAgain.ID, defaultDeployment.ID)
	}
	var defaultAssistantCount int64
	if err := database.Model(&types.DigitalAssistant{}).Where("org_id = ? AND public_id = ?", 12, "assistant_default_o12").Count(&defaultAssistantCount).Error; err != nil {
		t.Fatalf("count default assistants: %v", err)
	}
	if defaultAssistantCount != 1 {
		t.Fatalf("default assistant count = %d, want 1", defaultAssistantCount)
	}
	var defaultDeploymentCount int64
	if err := database.Model(&types.WorkerDeployment{}).Where("org_id = ? AND worker_id = ?", 12, 1).Count(&defaultDeploymentCount).Error; err != nil {
		t.Fatalf("count default deployments: %v", err)
	}
	if defaultDeploymentCount != 1 {
		t.Fatalf("default deployment count = %d, want 1", defaultDeploymentCount)
	}

	assistant := &types.DigitalAssistant{
		PublicID: "custom-agent",
		OrgID:    12,
		OwnerID:  34,
		Name:     "Custom Agent",
		Status:   string(contract.DigitalAssistantStatusDraft),
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
	if customDeployment.PublicID == "" {
		t.Fatal("custom worker public_id is empty")
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
		PublicID: "org_12_default_worker",
		OrgID:    12,
		OwnerID:  34,
		Name:     "Legacy Default Worker",
		Status:   string(contract.DigitalAssistantStatusActive),
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
	if err := database.Where("org_id = ? AND public_id = ?", 12, "assistant_default_o12").First(&defaultAssistant).Error; err != nil {
		t.Fatalf("load default assistant: %v", err)
	}
	if defaultDeployment.DigitalAssistantID != defaultAssistant.ID {
		t.Fatalf("deployment assistant_id = %d, want %d", defaultDeployment.DigitalAssistantID, defaultAssistant.ID)
	}
	if defaultDeployment.WorkerID != 1 {
		t.Fatalf("default worker_id = %d, want 1", defaultDeployment.WorkerID)
	}
	if defaultDeployment.PublicID == "" {
		t.Fatal("default worker public_id is empty")
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
		PublicID: "agent",
		OrgID:    1,
		Name:     "Agent",
		Status:   string(contract.DigitalAssistantStatusActive),
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
	if err := reconcileWorkerDeployment(ctx, database, scheduler, nil, deployment, nil); err != nil {
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

func TestWorkerReconcilerRestartsReadyDeploymentWhenSpecDrifts(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.DigitalAssistant{}, &types.WorkerDeployment{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	ctx := context.Background()
	assistant := &types.DigitalAssistant{
		PublicID: "agent",
		OrgID:    1,
		Name:     "Agent",
		Status:   string(contract.DigitalAssistantStatusActive),
	}
	if err := database.Create(assistant).Error; err != nil {
		t.Fatalf("create assistant: %v", err)
	}
	deployment := &types.WorkerDeployment{
		OrgID:              1,
		DigitalAssistantID: assistant.ID,
		WorkerID:           1,
		DeploymentName:     "leros-worker-o1-w1",
		Status:             string(types.WorkerDeploymentStatusReady),
		BootstrapTokenHash: "old-token-hash",
	}
	if err := database.Create(deployment).Error; err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	scheduler := &fakeWorkerScheduler{needsReconcile: true}
	cfg := &config.SchedulerConfig{
		ServerAddr:  "http://leros:8080",
		WorkerImage: "leros-worker:v2",
	}
	if err := reconcileWorkerDeployment(ctx, database, scheduler, cfg, deployment, nil); err != nil {
		t.Fatalf("reconcile deployment: %v", err)
	}
	if scheduler.startCalls != 1 {
		t.Fatalf("Start calls = %d, want 1", scheduler.startCalls)
	}
	if scheduler.lastSpec == nil {
		t.Fatal("last spec is nil")
	}
	if scheduler.lastSpec.Image != cfg.WorkerImage {
		t.Fatalf("spec image = %q, want %q", scheduler.lastSpec.Image, cfg.WorkerImage)
	}
	if scheduler.lastSpec.BootstrapToken == "" {
		t.Fatal("bootstrap token is empty")
	}

	var got types.WorkerDeployment
	if err := database.First(&got, deployment.ID).Error; err != nil {
		t.Fatalf("reload deployment: %v", err)
	}
	if got.BootstrapTokenHash == "" || got.BootstrapTokenHash == "old-token-hash" {
		t.Fatalf("bootstrap hash was not rotated: %q", got.BootstrapTokenHash)
	}
	if got.Status != string(types.WorkerDeploymentStatusProvisioning) {
		t.Fatalf("status = %q, want provisioning", got.Status)
	}
}

func TestWorkerReconcilerMarksProvisioningDeploymentReadyAfterHealthCheck(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.DigitalAssistant{}, &types.WorkerDeployment{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	ctx := context.Background()
	assistant := &types.DigitalAssistant{
		PublicID: "agent",
		OrgID:    1,
		Name:     "Agent",
		Status:   string(contract.DigitalAssistantStatusActive),
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
		LastError:          "worker is not ready",
		LastStartedAt:      &startedAt,
	}
	if err := database.Create(deployment).Error; err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	scheduler := &fakeWorkerScheduler{}
	if err := reconcileWorkerDeployment(ctx, database, scheduler, nil, deployment, nil); err != nil {
		t.Fatalf("reconcile deployment: %v", err)
	}
	if scheduler.startCalls != 0 {
		t.Fatalf("Start calls = %d, want 0", scheduler.startCalls)
	}

	var got types.WorkerDeployment
	if err := database.First(&got, deployment.ID).Error; err != nil {
		t.Fatalf("reload deployment: %v", err)
	}
	if got.Status != string(types.WorkerDeploymentStatusReady) {
		t.Fatalf("status = %q, want ready", got.Status)
	}
	if got.LastError != "" {
		t.Fatalf("last_error = %q, want empty", got.LastError)
	}
}

func TestWorkerReconcilerRestartsProvisioningDeploymentWhenRuntimeMissing(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.DigitalAssistant{}, &types.WorkerDeployment{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	ctx := context.Background()
	assistant := &types.DigitalAssistant{
		PublicID: "agent",
		OrgID:    1,
		Name:     "Agent",
		Status:   string(contract.DigitalAssistantStatusActive),
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

	scheduler := &fakeWorkerScheduler{healthErr: worker.ErrWorkerNotFound}
	if err := reconcileWorkerDeployment(ctx, database, scheduler, nil, deployment, nil); err != nil {
		t.Fatalf("reconcile deployment: %v", err)
	}
	if scheduler.startCalls != 1 {
		t.Fatalf("Start calls = %d, want 1", scheduler.startCalls)
	}

	var got types.WorkerDeployment
	if err := database.First(&got, deployment.ID).Error; err != nil {
		t.Fatalf("reload deployment: %v", err)
	}
	if got.Status != string(types.WorkerDeploymentStatusProvisioning) {
		t.Fatalf("status = %q, want provisioning", got.Status)
	}
	if got.BootstrapTokenHash == "" || got.BootstrapTokenHash == "stable-token-hash" {
		t.Fatalf("bootstrap hash was not rotated: %q", got.BootstrapTokenHash)
	}
}

type fakeWorkerScheduler struct {
	startCalls     int
	healthErr      error
	needsReconcile bool
	lastSpec       *worker.WorkerSpec
}

func (f *fakeWorkerScheduler) Start(ctx context.Context, spec *worker.WorkerSpec) (*worker.WorkerInstance, error) {
	f.startCalls++
	f.lastSpec = spec
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

func (f *fakeWorkerScheduler) NeedsReconcile(ctx context.Context, spec *worker.WorkerSpec) (bool, error) {
	return f.needsReconcile, nil
}

func (f *fakeWorkerScheduler) Shutdown(ctx context.Context) error {
	return nil
}
