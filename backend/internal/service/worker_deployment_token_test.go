package service

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/config"
	"github.com/insmtx/Lework/backend/internal/api/auth"
	"github.com/insmtx/Lework/backend/internal/api/contract"
	"github.com/insmtx/Lework/backend/internal/worker"
	"github.com/insmtx/Lework/backend/types"
)

// tokenAwareScheduler 在 fakeWorkerScheduler 之上模拟 k8s 调度器读取线上 token 的能力。
type tokenAwareScheduler struct {
	fakeWorkerScheduler
	liveToken string
	liveErr   error
	startErr  error
}

func (f *tokenAwareScheduler) CurrentBootstrapToken(context.Context, string) (string, error) {
	return f.liveToken, f.liveErr
}

func (f *tokenAwareScheduler) Start(
	ctx context.Context,
	spec *worker.WorkerSpec,
) (*worker.WorkerInstance, error) {
	f.startCalls++
	f.lastSpec = spec
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &worker.WorkerInstance{ID: spec.ID, WorkerID: spec.ID}, nil
}

func newTokenTestDeployment(
	t *testing.T,
	status string,
	hash string,
) (*gorm.DB, *types.WorkerDeployment) {
	t.Helper()

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.DigitalAssistant{}, &types.WorkerDeployment{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
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
		DeploymentName:     "lework-worker-o1-w1",
		Status:             status,
		BootstrapTokenHash: hash,
	}
	if err := database.Create(deployment).Error; err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	return database, deployment
}

func reloadDeployment(t *testing.T, database *gorm.DB, id uint) types.WorkerDeployment {
	t.Helper()
	var got types.WorkerDeployment
	if err := database.First(&got, id).Error; err != nil {
		t.Fatalf("reload deployment: %v", err)
	}
	return got
}

// TestReconcileReusesLiveBootstrapToken 是“一次 reconcile 不因 token 重建 Pod”的核心断言。
func TestReconcileReusesLiveBootstrapToken(t *testing.T) {
	const liveToken = "live-bootstrap-token"
	database, deployment := newTokenTestDeployment(
		t,
		string(types.WorkerDeploymentStatusReady),
		auth.HashBootstrapToken(liveToken),
	)
	scheduler := &tokenAwareScheduler{liveToken: liveToken}
	scheduler.needsReconcile = true

	if err := reconcileWorkerDeployment(
		context.Background(), database, scheduler, &config.SchedulerConfig{WorkerImage: "img:v1"}, deployment, nil,
	); err != nil {
		t.Fatalf("reconcile deployment: %v", err)
	}
	if scheduler.startCalls != 1 {
		t.Fatalf("Start calls = %d, want 1", scheduler.startCalls)
	}
	if scheduler.lastSpec == nil || scheduler.lastSpec.BootstrapToken != liveToken {
		t.Fatalf("spec bootstrap token = %q, want reused live token", scheduler.lastSpec.BootstrapToken)
	}

	got := reloadDeployment(t, database, deployment.ID)
	if got.BootstrapTokenHash != auth.HashBootstrapToken(liveToken) {
		t.Fatalf("bootstrap hash = %q, want unchanged live hash", got.BootstrapTokenHash)
	}
	if got.Status != string(types.WorkerDeploymentStatusProvisioning) {
		t.Fatalf("status = %q, want provisioning", got.Status)
	}
}

// TestReconcileAdoptsLiveTokenWhenHashDiverged 覆盖“DB 与线上分叉”的自愈方向：
// 以正在运行的 Pod 使用的 token 为准，回写 DB，而不是轮转出新 token 制造 401。
func TestReconcileAdoptsLiveTokenWhenHashDiverged(t *testing.T) {
	const liveToken = "live-bootstrap-token"
	database, deployment := newTokenTestDeployment(
		t,
		string(types.WorkerDeploymentStatusReady),
		"stale-token-hash",
	)
	scheduler := &tokenAwareScheduler{liveToken: liveToken}
	scheduler.needsReconcile = true

	if err := reconcileWorkerDeployment(
		context.Background(), database, scheduler, &config.SchedulerConfig{WorkerImage: "img:v1"}, deployment, nil,
	); err != nil {
		t.Fatalf("reconcile deployment: %v", err)
	}
	if scheduler.lastSpec == nil || scheduler.lastSpec.BootstrapToken != liveToken {
		t.Fatalf("spec bootstrap token = %q, want live token", scheduler.lastSpec.BootstrapToken)
	}

	got := reloadDeployment(t, database, deployment.ID)
	if got.BootstrapTokenHash != auth.HashBootstrapToken(liveToken) {
		t.Fatalf("bootstrap hash = %q, want live hash written back", got.BootstrapTokenHash)
	}
}

// TestReconcileKeepsHashWhenApplyFails 保证 Apply 失败不会留下“DB 已轮转”的分叉。
func TestReconcileKeepsHashWhenApplyFails(t *testing.T) {
	const liveToken = "live-bootstrap-token"
	originalHash := auth.HashBootstrapToken(liveToken)
	database, deployment := newTokenTestDeployment(t, string(types.WorkerDeploymentStatusReady), originalHash)
	scheduler := &tokenAwareScheduler{
		liveToken: liveToken,
		startErr:  errors.New("update worker deployment: Operation cannot be fulfilled on deployments.apps"),
	}
	scheduler.needsReconcile = true

	err := reconcileWorkerDeployment(
		context.Background(), database, scheduler, &config.SchedulerConfig{WorkerImage: "img:v1"}, deployment, nil,
	)
	if err == nil {
		t.Fatal("reconcile deployment error = nil, want apply failure")
	}

	got := reloadDeployment(t, database, deployment.ID)
	if got.BootstrapTokenHash != originalHash {
		t.Fatalf("bootstrap hash = %q, want unchanged after failed apply", got.BootstrapTokenHash)
	}
	if got.Status != string(types.WorkerDeploymentStatusFailed) {
		t.Fatalf("status = %q, want failed", got.Status)
	}
}

// TestReconcileGeneratesTokenWhenDeploymentHasNone 覆盖首次创建：线上没有 token 才生成。
func TestReconcileGeneratesTokenWhenDeploymentHasNone(t *testing.T) {
	database, deployment := newTokenTestDeployment(t, string(types.WorkerDeploymentStatusReady), "")
	scheduler := &tokenAwareScheduler{liveToken: ""}
	scheduler.needsReconcile = true

	if err := reconcileWorkerDeployment(
		context.Background(), database, scheduler, &config.SchedulerConfig{WorkerImage: "img:v1"}, deployment, nil,
	); err != nil {
		t.Fatalf("reconcile deployment: %v", err)
	}
	if scheduler.lastSpec == nil || scheduler.lastSpec.BootstrapToken == "" {
		t.Fatal("spec bootstrap token is empty, want generated token")
	}

	got := reloadDeployment(t, database, deployment.ID)
	if got.BootstrapTokenHash != auth.HashBootstrapToken(scheduler.lastSpec.BootstrapToken) {
		t.Fatalf("bootstrap hash = %q, want hash of generated token", got.BootstrapTokenHash)
	}
}
