package service

import (
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	dbpkg "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

func TestCreateTask_TouchesProjectUpdatedAt(t *testing.T) {
	database := setupTestDB(t)
	service := NewTaskService(database)
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

func TestDeleteProject_CascadesTasks(t *testing.T) {
	database := setupTestDB(t)
	projectService := NewProjectService(database, nil, nil, "test")
	taskService := NewTaskService(database)
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
	taskService := NewTaskService(database)
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
