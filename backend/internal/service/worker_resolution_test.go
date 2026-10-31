package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	infradb "github.com/insmtx/Lework/backend/internal/infra/db"
	"github.com/insmtx/Lework/backend/types"
)

// setupResolveTestDB 为 resolveProjectAssistantWorker 测试构造内存 sqlite 库：
// seed project resource + assistant bindings（权限来源）。
func setupResolveTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := d.AutoMigrate(&types.Project{}, &types.Resource{}, &types.ResourceBinding{}, &types.Task{}, &types.WorkerDeployment{}, &types.DigitalAssistant{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	_ = infradb.CreateProject(ctx, d, &types.Project{PublicID: "p1", OrgID: 1, OwnerID: 1, Name: "P1", Status: string(types.ProjectStatusActive)})
	_ = d.Exec(`INSERT INTO ` + types.TableNameResource + ` (created_at, updated_at, org_id, uin, type, biz_id) VALUES (CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 1, 1, 'project', 1)`).Error
	_ = infradb.CreateResourceBinding(ctx, d, &types.ResourceBinding{OrgID: 1, AssistantID: ptrUint(100), ResourceID: 1, Role: types.ResourceRoleMember})
	_ = infradb.CreateResourceBinding(ctx, d, &types.ResourceBinding{OrgID: 1, AssistantID: ptrUint(200), ResourceID: 1, Role: types.ResourceRoleMember})
	_ = infradb.CreateWorkerDeployment(ctx, d, &types.WorkerDeployment{PublicID: "dep-100", OrgID: 1, DigitalAssistantID: 100, WorkerID: 1000, DeploymentName: "dep-100", Status: string(types.WorkerDeploymentStatusReady)})
	_ = infradb.CreateWorkerDeployment(ctx, d, &types.WorkerDeployment{PublicID: "dep-200", OrgID: 1, DigitalAssistantID: 200, WorkerID: 2000, DeploymentName: "dep-200", Status: string(types.WorkerDeploymentStatusReady)})
	// Seed DigitalAssistant rows for resolveRuntimeWorker to find via GetDigitalAssistantByID
	_ = d.Exec(`INSERT INTO ` + types.TableNameDigitalAssistant + ` (id, created_at, updated_at, public_id, org_id, owner_id, name, status, system_prompt, source) VALUES (100, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'assistant-100', 1, 1, 'Assistant 100', 'active', '', 'custom')`).Error
	_ = d.Exec(`INSERT INTO ` + types.TableNameDigitalAssistant + ` (id, created_at, updated_at, public_id, org_id, owner_id, name, status, system_prompt, source) VALUES (200, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'assistant-200', 1, 1, 'Assistant 200', 'active', '', 'custom')`).Error
	_ = infradb.CreateProject(ctx, d, &types.Project{PublicID: "p2", OrgID: 1, OwnerID: 1, Name: "P2", Status: string(types.ProjectStatusActive)})
	return d
}

func seedAssistantBinding(t *testing.T, ctx context.Context, d *gorm.DB, resourceID, assistantID uint) {
	t.Helper()
	id := assistantID
	if err := infradb.CreateResourceBinding(ctx, d, &types.ResourceBinding{
		OrgID:       1,
		AssistantID: &id,
		ResourceID:  resourceID,
		Role:        types.ResourceRoleMember,
	}); err != nil {
		t.Fatalf("seed assistant binding %d: %v", assistantID, err)
	}
}

func ptrUint(v uint) *uint {
	return &v
}

// 未传 assistantIDs + 项目有 assistant binding → 返回最新绑定的助手 (200)。
func TestResolveProjectAssistantWorkerPicksLatestByDefault(t *testing.T) {
	d := setupResolveTestDB(t)
	got, _, err := resolveProjectAssistantWorker(context.Background(), d, 1, 1, nil, nil)
	if err != nil || got != 200 {
		t.Fatalf("want 200, got %d err %v", got, err)
	}
}

// 未传 assistantIDs + 项目无 assistant binding → ErrNoDefaultAssistant。
func TestResolveProjectAssistantWorkerErrorsWhenNoAssistant(t *testing.T) {
	d := setupResolveTestDB(t)
	_, _, err := resolveProjectAssistantWorker(context.Background(), d, 1, 2, nil, nil)
	if !errors.Is(err, ErrNoDefaultAssistant) {
		t.Fatalf("want ErrNoDefaultAssistant, got %v", err)
	}
}

// 传 assistantIDs + 非项目 assistant binding → 错误。
func TestResolveProjectAssistantWorkerValidatesMembership(t *testing.T) {
	d := setupResolveTestDB(t)
	_, _, err := resolveProjectAssistantWorker(context.Background(), d, 1, 1, []uint{999}, nil)
	if err == nil {
		t.Fatal("non-member should be rejected")
	}
}

// 传 assistantIDs + 是项目 assistant binding → 返回该 worker（happy path）。
func TestResolveProjectAssistantWorkerReturnsWorkerForValidMember(t *testing.T) {
	d := setupResolveTestDB(t)
	assistantID, workerID, err := resolveProjectAssistantWorker(context.Background(), d, 1, 1, []uint{100}, nil)
	if err != nil {
		t.Fatalf("valid member should resolve: %v", err)
	}
	if assistantID != 100 {
		t.Fatalf("assistantID = %d, want 100", assistantID)
	}
	if workerID != 1000 {
		t.Fatalf("workerID = %d, want 1000", workerID)
	}
}

func TestResolveDefaultRuntimeWorkerInferrerFallbackDBDeploymentExists(t *testing.T) {
	d := setupResolveTestDB(t)
	_ = infradb.CreateWorkerDeployment(context.Background(), d, &types.WorkerDeployment{OrgID: 1, PublicID: "default-dep", DigitalAssistantID: 300, WorkerID: 1, DeploymentName: "default-dep", Status: string(types.WorkerDeploymentStatusReady)})
	assistantID, workerID, err := resolveDefaultRuntimeWorker(context.Background(), d, 1, &mockInferrer{assistantID: 9999})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if assistantID != 300 {
		t.Fatalf("assistantID = %d, want 300 (from DB deployment)", assistantID)
	}
	if workerID != 1 {
		t.Fatalf("workerID = %d, want 1 (from DB deployment)", workerID)
	}
}

// TestResolveDefaultRuntimeWorkerInferrerFallbackNoDefaultDeployment 验证当 DB 无默认 deployment (worker_id=1)
// 但 inferrer 返回有效 assistant ID 时，走 resolveRuntimeWorker 二次解析出真实 WorkerID。
func TestResolveDefaultRuntimeWorkerInferrerFallbackNoDefaultDeployment(t *testing.T) {
	d := setupResolveTestDB(t)
	d.Where("worker_id = ?", 1).Delete(&types.WorkerDeployment{})

	inferrer := &mockInferrer{assistantID: 100}
	assistantID, workerID, err := resolveDefaultRuntimeWorker(context.Background(), d, 1, inferrer)
	if err != nil {
		t.Fatalf("resolveDefaultRuntimeWorker failed: %v", err)
	}
	if assistantID != 100 {
		t.Fatalf("assistantID = %d, want 100 (DigitalAssistant.ID from inferrer resolution)", assistantID)
	}
	if workerID != 1000 {
		t.Fatalf("workerID = %d, want 1000 (WorkerDeployment.WorkerID resolved from DB)", workerID)
	}
}

// TestResolveDefaultRuntimeWorkerInferrerFallbackNoDeploymentAtAll 验证当 inferrer 返回的
// assistant ID 在 DB 中无对应 deployment 时，返回 ErrNoDefaultAssistantInOrg。
func TestResolveDefaultRuntimeWorkerInferrerFallbackNoDeploymentAtAll(t *testing.T) {
	d := setupResolveTestDB(t)
	d.Where("worker_id = ?", 1).Delete(&types.WorkerDeployment{})

	inferrer := &mockInferrer{assistantID: 99999}
	_, _, err := resolveDefaultRuntimeWorker(context.Background(), d, 1, inferrer)
	if err == nil {
		t.Fatal("expected error when inferrer returns non-existent assistant ID, got nil")
	}
}

// TestResolveDefaultRuntimeWorkerInferrerFallbackNoDBNoInferrer 验证纯静默场景
// 既无 DB 也无 inferrer → ErrNoDefaultAssistantInOrg。
func TestResolveDefaultRuntimeWorkerInferrerFallbackNoDBNoInferrer(t *testing.T) {
	_, _, err := resolveDefaultRuntimeWorker(context.Background(), nil, 1, nil)
	if !errors.Is(err, ErrNoDefaultAssistantInOrg) {
		t.Fatalf("want ErrNoDefaultAssistantInOrg, got %v", err)
	}
}

func TestResolveTaskAssigneeWorkerUsesAssignee(t *testing.T) {
	d := setupResolveTestDB(t)
	ctx := context.Background()
	project := &types.Project{PublicID: "p_assignee_route", OrgID: 1, OwnerID: 1, Name: "P", Status: string(types.ProjectStatusActive)}
	if err := infradb.CreateProject(ctx, d, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	resource := &types.Resource{OrgID: 1, Uin: 1, Type: types.ResourceTypeProject, BizID: project.ID}
	if err := infradb.CreateResource(ctx, d, resource); err != nil {
		t.Fatalf("create project resource: %v", err)
	}
	seedAssistantBinding(t, ctx, d, resource.ID, 100)
	assignee := uint(100)
	task := &types.Task{
		PublicID:   "task_assignee_route",
		OrgID:      1,
		OwnerID:    1,
		ProjectID:  project.ID,
		AssigneeID: &assignee,
		Title:      "route to assignee",
		Status:     string(types.TaskStatusCreated),
	}
	if err := infradb.CreateTask(ctx, d, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	assistantID, workerID, ok, err := resolveTaskAssigneeWorker(ctx, d, 1, task.ID, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !ok || assistantID != 100 || workerID != 1000 {
		t.Fatalf("got ok=%v assistant=%d worker=%d, want 100/1000", ok, assistantID, workerID)
	}
}

func TestResolveTaskAssigneeWorkerReturnsReadableErrorWhenWorkerNotReady(t *testing.T) {
	d := setupResolveTestDB(t)
	ctx := context.Background()
	project := &types.Project{PublicID: "p_assignee_not_ready", OrgID: 1, OwnerID: 1, Name: "P", Status: string(types.ProjectStatusActive)}
	if err := infradb.CreateProject(ctx, d, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	resource := &types.Resource{OrgID: 1, Uin: 1, Type: types.ResourceTypeProject, BizID: project.ID}
	if err := infradb.CreateResource(ctx, d, resource); err != nil {
		t.Fatalf("create project resource: %v", err)
	}
	seedAssistantBinding(t, ctx, d, resource.ID, 100)
	if err := d.Model(&types.WorkerDeployment{}).
		Where("digital_assistant_id = ?", 100).
		Update("status", string(types.WorkerDeploymentStatusPending)).Error; err != nil {
		t.Fatalf("set deployment pending: %v", err)
	}
	assignee := uint(100)
	task := &types.Task{
		PublicID:   "task_assignee_not_ready",
		OrgID:      1,
		OwnerID:    1,
		ProjectID:  project.ID,
		AssigneeID: &assignee,
		Title:      "route to not-ready assignee",
		Status:     string(types.TaskStatusCreated),
	}
	if err := infradb.CreateTask(ctx, d, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, _, _, err := resolveTaskAssigneeWorker(ctx, d, 1, task.ID, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrAssistantWorkerNotReady) || !strings.Contains(err.Error(), "Assistant 100") {
		t.Fatalf("error = %v, want not-ready with assistant name", err)
	}
}
