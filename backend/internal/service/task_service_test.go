package service

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	dbpkg "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

func setupTestContextWithCallerUin(t *testing.T, uin uint) context.Context {
	t.Helper()
	caller := &types.Caller{
		Uin:   uin,
		OrgID: 1,
		State: types.AuthStateSucc,
	}
	trace := &types.Trace{
		RequestID: "test-request-id",
		TraceID:   "test-trace-id",
	}
	return auth.WithContext(context.Background(), caller, trace)
}

// seedProjectResourceOwner 为直连 DAO 创建的项目补种 project 资源与 owner 绑定，
// 使 PermissionService 鉴权可通过。用于绕过 service.CreateProject 的测试。
func seedProjectResourceOwner(t *testing.T, database *gorm.DB, project *types.Project, ownerUin uint) {
	t.Helper()
	ctx := context.Background()
	resource := &types.Resource{
		OrgID: project.OrgID,
		Uin:   ownerUin,
		Type:  types.ResourceTypeProject,
		BizID: project.ID,
	}
	if err := dbpkg.CreateResource(ctx, database, resource); err != nil {
		t.Fatalf("seed project resource: %v", err)
	}
	if err := dbpkg.CreateResourceBinding(ctx, database, &types.ResourceBinding{
		OrgID:      project.OrgID,
		Uin:        &ownerUin,
		ResourceID: resource.ID,
		Role:       types.ResourceRoleOwner,
	}); err != nil {
		t.Fatalf("seed project owner binding: %v", err)
	}
}

// seedProjectResourceBinding 为指定 org/项目业务 ID 补种 project 资源（若不存在）并写入用户角色绑定。
// 用于测试新权限模型下用户成员的访问校验（IsProjectUserMember / 群聊准入）。
func seedProjectResourceBinding(t *testing.T, database *gorm.DB, orgID, bizID, uin uint, role types.ResourceRole) {
	t.Helper()
	ctx := context.Background()
	resource, err := dbpkg.GetResourceByBizID(ctx, database, orgID, types.ResourceTypeProject, bizID)
	if err != nil {
		t.Fatalf("get project resource: %v", err)
	}
	if resource == nil {
		resource = &types.Resource{OrgID: orgID, Uin: uin, Type: types.ResourceTypeProject, BizID: bizID}
		if err := dbpkg.CreateResource(ctx, database, resource); err != nil {
			t.Fatalf("seed project resource: %v", err)
		}
	}
	boundUin := uin
	if err := dbpkg.CreateResourceBinding(ctx, database, &types.ResourceBinding{
		OrgID:      orgID,
		Uin:        &boundUin,
		ResourceID: resource.ID,
		Role:       role,
	}); err != nil {
		t.Fatalf("seed resource binding: %v", err)
	}
}

func seedTaskResource(t *testing.T, database *gorm.DB, task *types.Task, projectResourceID uint) {
	t.Helper()
	ctx := context.Background()
	parentID := projectResourceID
	if err := dbpkg.CreateResource(ctx, database, &types.Resource{
		OrgID:                 task.OrgID,
		Uin:                   task.OwnerID,
		Type:                  types.ResourceTypeTask,
		BizID:                 task.ID,
		ParentResourceID:      &parentID,
		ParentResourcePathIDs: types.ResourcePathIDs{projectResourceID},
	}); err != nil {
		t.Fatalf("seed task resource: %v", err)
	}
}

func TestCreateTask_TouchesProjectUpdatedAt(t *testing.T) {
	database := setupTestDB(t)
	service := NewTaskService(database, newTestPermissionService(database))
	ctx := setupTestContextWithCaller(t)

	project := &types.Project{
		PublicID: "prj_test_create_task_touch",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Task Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := dbpkg.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	seedProjectResourceOwner(t, database, project, 1)

	oldUpdatedAt := time.Now().Add(-time.Hour).UTC()
	if err := database.Model(&types.Project{}).
		Where("id = ?", project.ID).
		Update("updated_at", oldUpdatedAt).Error; err != nil {
		t.Fatalf("set old project updated_at: %v", err)
	}

	_, err := service.CreateTask(ctx, &contract.CreateTaskRequest{
		ProjectID: project.PublicID,
		Title:     "新建任务",
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	refreshedProject, err := dbpkg.GetProjectByID(ctx, database, project.ID)
	if err != nil {
		t.Fatalf("GetProjectByID failed: %v", err)
	}
	if refreshedProject == nil {
		t.Fatal("expected project to exist after CreateTask")
	}
	if !refreshedProject.UpdatedAt.After(oldUpdatedAt) {
		t.Fatalf("expected project updated_at after %v, got %v", oldUpdatedAt, refreshedProject.UpdatedAt)
	}
}

func TestCreateTask_AllowsMemberWithoutServiceGate(t *testing.T) {
	database := setupTestDB(t)
	service := NewTaskService(database, newTestPermissionService(database))

	project := &types.Project{
		PublicID: "prj_task_create_auth",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Task Create Auth",
		Status:   string(types.ProjectStatusActive),
	}
	if err := dbpkg.CreateProject(context.Background(), database, project); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	seedProjectResourceOwner(t, database, project, 1)
	seedProjectResourceBinding(t, database, 1, project.ID, 2, types.ResourceRoleMember)

	memberCtx := setupTestContextWithCallerUin(t, 2)
	if _, err := service.CreateTask(memberCtx, &contract.CreateTaskRequest{
		ProjectID: project.PublicID,
		Title:     "member task",
	}); err != nil {
		t.Fatalf("member CreateTask: %v", err)
	}
}

func TestDeleteProject_CascadesTasks(t *testing.T) {
	database := setupTestDB(t)
	projectService := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil, NewSkillDisplayTranslationService(database))
	taskService := NewTaskService(database, newTestPermissionService(database))
	ctx := setupTestContextWithCaller(t)

	project := &types.Project{
		PublicID: "prj_test_delete_cascade",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Delete Cascade Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := dbpkg.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	seedProjectResourceOwner(t, database, project, 1)

	if _, err := taskService.CreateTask(ctx, &contract.CreateTaskRequest{
		ProjectID: project.PublicID,
		Title:     "待删除任务",
	}); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	if err := projectService.DeleteProject(ctx, project.PublicID); err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}

	list, err := taskService.ListTasks(ctx, &contract.ListTasksRequest{})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if list.Total != 0 || len(list.Items) != 0 {
		t.Fatalf("expected no tasks after project delete, got total=%d items=%d", list.Total, len(list.Items))
	}
}

func TestListTasks_ExcludesTasksFromDeletedProject(t *testing.T) {
	database := setupTestDB(t)
	taskService := NewTaskService(database, newTestPermissionService(database))
	ctx := setupTestContextWithCaller(t)

	activeProject := &types.Project{
		PublicID: "prj_test_list_active",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Active Project",
		Status:   string(types.ProjectStatusActive),
	}
	deletedProject := &types.Project{
		PublicID: "prj_test_list_deleted",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Deleted Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := dbpkg.CreateProject(ctx, database, activeProject); err != nil {
		t.Fatalf("CreateProject active failed: %v", err)
	}
	if err := dbpkg.CreateProject(ctx, database, deletedProject); err != nil {
		t.Fatalf("CreateProject deleted failed: %v", err)
	}
	seedProjectResourceOwner(t, database, activeProject, 1)
	seedProjectResourceOwner(t, database, deletedProject, 1)

	if _, err := taskService.CreateTask(ctx, &contract.CreateTaskRequest{
		ProjectID: activeProject.PublicID,
		Title:     "保留任务",
	}); err != nil {
		t.Fatalf("CreateTask active failed: %v", err)
	}
	if _, err := taskService.CreateTask(ctx, &contract.CreateTaskRequest{
		ProjectID: deletedProject.PublicID,
		Title:     "孤儿任务",
	}); err != nil {
		t.Fatalf("CreateTask deleted failed: %v", err)
	}

	if err := dbpkg.DeleteProject(ctx, database, deletedProject.ID); err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}

	list, err := taskService.ListTasks(ctx, &contract.ListTasksRequest{})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("expected 1 active task, got total=%d items=%d", list.Total, len(list.Items))
	}
	if list.Items[0].Title != "保留任务" {
		t.Fatalf("unexpected task title: %q", list.Items[0].Title)
	}
	if list.Items[0].ProjectID != activeProject.PublicID {
		t.Fatalf("unexpected project id: %q", list.Items[0].ProjectID)
	}
}

func TestTaskService_ProjectMemberCanViewProjectTasks(t *testing.T) {
	database := setupTestDB(t)
	taskService := NewTaskService(database, newTestPermissionService(database))
	ctx := setupTestContextWithCaller(t)

	project := &types.Project{
		PublicID: "prj_test_member_tasks",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Member Task Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := dbpkg.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	seedProjectResourceOwner(t, database, project, 1)
	seedProjectResourceBinding(t, database, 1, project.ID, 2, types.ResourceRoleMember)

	created, err := taskService.CreateTask(ctx, &contract.CreateTaskRequest{
		ProjectID: project.PublicID,
		Title:     "Owner Task",
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	memberCtx := setupTestContextWithCallerUin(t, 2)
	got, err := taskService.GetTask(memberCtx, created.PublicID)
	if err != nil {
		t.Fatalf("GetTask as member failed: %v", err)
	}
	if got.Title != "Owner Task" {
		t.Fatalf("unexpected task title: %q", got.Title)
	}

	projectID := project.PublicID
	list, err := taskService.ListTasks(memberCtx, &contract.ListTasksRequest{ProjectID: &projectID})
	if err != nil {
		t.Fatalf("ListTasks as member failed: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("expected 1 task in project list, got total=%d items=%d", list.Total, len(list.Items))
	}
}

func TestListTasks_GlobalListUsesProjectBindingNotTaskOwner(t *testing.T) {
	database := setupTestDB(t)
	taskService := NewTaskService(database, newTestPermissionService(database))
	ownerCtx := setupTestContextWithCaller(t)

	project := &types.Project{
		PublicID: "prj_test_global_tasks",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Global Task Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := dbpkg.CreateProject(ownerCtx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	seedProjectResourceOwner(t, database, project, 1)
	seedProjectResourceBinding(t, database, 1, project.ID, 2, types.ResourceRoleMember)

	if _, err := taskService.CreateTask(ownerCtx, &contract.CreateTaskRequest{
		ProjectID: project.PublicID,
		Title:     "Owner Created Task",
	}); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	memberCtx := setupTestContextWithCallerUin(t, 2)
	list, err := taskService.ListTasks(memberCtx, &contract.ListTasksRequest{})
	if err != nil {
		t.Fatalf("ListTasks as member failed: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("expected member to see owner task via project binding, got total=%d items=%d", list.Total, len(list.Items))
	}
	if list.Items[0].Title != "Owner Created Task" {
		t.Fatalf("unexpected task title: %q", list.Items[0].Title)
	}

	outsiderCtx := setupTestContextWithCallerUin(t, 99)
	outsiderList, err := taskService.ListTasks(outsiderCtx, &contract.ListTasksRequest{})
	if err != nil {
		t.Fatalf("ListTasks as outsider failed: %v", err)
	}
	if outsiderList.Total != 0 || len(outsiderList.Items) != 0 {
		t.Fatalf("expected outsider to see no tasks, got total=%d items=%d", outsiderList.Total, len(outsiderList.Items))
	}
}
