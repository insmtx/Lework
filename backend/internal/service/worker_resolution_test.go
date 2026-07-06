package service

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

// setupResolveTestDB 为 resolveProjectAssistantWorker 测试构造内存 sqlite 库：
// AutoMigrate Project/ProjectMember/WorkerDeployment，并 seed 两类项目数据——
//   - project 1: 两个 assistant 成员 (100, 200) + 对应 worker deployment
//   - project 2: 无 assistant 成员（用于 "无 AI 队友" 场景）
func setupResolveTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := d.AutoMigrate(&types.Project{}, &types.ProjectMember{}, &types.WorkerDeployment{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	_ = infradb.CreateProject(ctx, d, &types.Project{PublicID: "p1", OrgID: 1, OwnerID: 1, Name: "P1", Status: string(types.ProjectStatusActive)})
	_ = infradb.CreateProjectMember(ctx, d, &types.ProjectMember{ProjectID: 1, MemberID: 100, MemberType: types.MemberTypeAssistant, MemberRole: types.MemberRoleMember})
	_ = infradb.CreateProjectMember(ctx, d, &types.ProjectMember{ProjectID: 1, MemberID: 200, MemberType: types.MemberTypeAssistant, MemberRole: types.MemberRoleMember})
	_ = infradb.CreateWorkerDeployment(ctx, d, &types.WorkerDeployment{OrgID: 1, DigitalAssistantID: 100, WorkerID: 1000, DeploymentName: "dep-100", Status: string(types.WorkerDeploymentStatusReady)})
	_ = infradb.CreateWorkerDeployment(ctx, d, &types.WorkerDeployment{OrgID: 1, DigitalAssistantID: 200, WorkerID: 2000, DeploymentName: "dep-200", Status: string(types.WorkerDeploymentStatusReady)})
	_ = infradb.CreateProject(ctx, d, &types.Project{PublicID: "p2", OrgID: 1, OwnerID: 1, Name: "P2", Status: string(types.ProjectStatusActive)})
	return d
}

// 未传 assistantIDs + 项目有 assistant 成员 → 返回最新成员 (200) 的 worker。
func TestResolveProjectAssistantWorkerPicksLatestByDefault(t *testing.T) {
	d := setupResolveTestDB(t)
	got, _, err := resolveProjectAssistantWorker(context.Background(), d, 1, 1, nil, nil)
	if err != nil || got != 200 {
		t.Fatalf("want 200, got %d err %v", got, err)
	}
}

// 未传 assistantIDs + 项目无 assistant 成员 → ErrNoDefaultAssistant。
func TestResolveProjectAssistantWorkerErrorsWhenNoAssistant(t *testing.T) {
	d := setupResolveTestDB(t)
	_, _, err := resolveProjectAssistantWorker(context.Background(), d, 1, 2, nil, nil)
	if !errors.Is(err, ErrNoDefaultAssistant) {
		t.Fatalf("want ErrNoDefaultAssistant, got %v", err)
	}
}

// 传 assistantIDs + 非项目 assistant 成员 → 错误。
func TestResolveProjectAssistantWorkerValidatesMembership(t *testing.T) {
	d := setupResolveTestDB(t)
	_, _, err := resolveProjectAssistantWorker(context.Background(), d, 1, 1, []uint{999}, nil)
	if err == nil {
		t.Fatal("non-member should be rejected")
	}
}

// 传 assistantIDs + 是项目 assistant 成员 → 返回该 worker（happy path）。
// setup seeds member 100 → WorkerID 1000，显式指定应解析出该 deployment。
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
