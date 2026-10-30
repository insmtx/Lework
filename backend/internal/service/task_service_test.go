package service

import (
	"context"
	"errors"
	"strings"
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

func mustCreateTaskProjectForM1(t *testing.T, database *gorm.DB, publicID string) *types.Project {
	t.Helper()
	ctx := context.Background()
	project := &types.Project{
		PublicID: publicID,
		OrgID:    1,
		OwnerID:  1,
		Name:     "M1 Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := dbpkg.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	seedProjectResourceOwner(t, database, project, 1)
	return project
}

func TestCreateTask_PersistsExpectedOutputAndAcceptanceCriteria(t *testing.T) {
	database := setupTestDB(t)
	service := NewTaskService(database, newTestPermissionService(database))
	ctx := setupTestContextWithCaller(t)
	project := mustCreateTaskProjectForM1(t, database, "prj_m1_task_fields")

	created, err := service.CreateTask(ctx, &contract.CreateTaskRequest{
		ProjectID:          project.PublicID,
		Title:              "修复空输入崩溃",
		ExpectedOutput:     "修复补丁 + 单元测试",
		AcceptanceCriteria: "go test ./... 通过",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if created.ExpectedOutput != "修复补丁 + 单元测试" {
		t.Fatalf("created expected_output = %q", created.ExpectedOutput)
	}
	if created.AcceptanceCriteria != "go test ./... 通过" {
		t.Fatalf("created acceptance_criteria = %q", created.AcceptanceCriteria)
	}

	got, err := service.GetTask(ctx, created.PublicID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ExpectedOutput != created.ExpectedOutput || got.AcceptanceCriteria != created.AcceptanceCriteria {
		t.Fatalf("round trip mismatch: got %#v, want %#v", got, created)
	}
}

func TestUpdateTask_OnlyChangesProvidedFields(t *testing.T) {
	database := setupTestDB(t)
	service := NewTaskService(database, newTestPermissionService(database))
	ctx := setupTestContextWithCaller(t)
	project := mustCreateTaskProjectForM1(t, database, "prj_m1_task_update")

	created, err := service.CreateTask(ctx, &contract.CreateTaskRequest{
		ProjectID:      project.PublicID,
		Title:          "原始标题",
		Description:    "原始描述",
		ExpectedOutput: "旧产出",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	expected := "新产出"
	criteria := "验收标准 A"
	updated, err := service.UpdateTask(ctx, created.PublicID, &contract.UpdateTaskRequest{
		ExpectedOutput:     &expected,
		AcceptanceCriteria: &criteria,
	})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if updated.Title != "原始标题" || updated.Description != "原始描述" {
		t.Fatalf("unrelated fields changed: %#v", updated)
	}
	if updated.ExpectedOutput != expected || updated.AcceptanceCriteria != criteria {
		t.Fatalf("expected/criteria not updated: %#v", updated)
	}
}

func TestUpdateTask_RejectsInvalidStatusTransition(t *testing.T) {
	database := setupTestDB(t)
	service := NewTaskService(database, newTestPermissionService(database))
	ctx := setupTestContextWithCaller(t)
	project := mustCreateTaskProjectForM1(t, database, "prj_m1_task_transition")

	created, err := service.CreateTask(ctx, &contract.CreateTaskRequest{
		ProjectID: project.PublicID,
		Title:     "状态迁移",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := database.Model(&types.Task{}).
		Where("public_id = ?", created.PublicID).
		Update("status", string(types.TaskStatusCompleted)).Error; err != nil {
		t.Fatalf("set completed: %v", err)
	}

	next := string(types.TaskStatusInProgress)
	if _, err := service.UpdateTask(ctx, created.PublicID, &contract.UpdateTaskRequest{Status: &next}); err == nil {
		t.Fatal("expected completed -> in_progress to be rejected")
	}
}

func TestUpdateTask_AllowsAcceptanceLifecycle(t *testing.T) {
	database := setupTestDB(t)
	service := NewTaskService(database, newTestPermissionService(database))
	ctx := setupTestContextWithCaller(t)
	project := mustCreateTaskProjectForM1(t, database, "prj_m1_task_lifecycle")

	created, err := service.CreateTask(ctx, &contract.CreateTaskRequest{
		ProjectID: project.PublicID,
		Title:     "闭环任务",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	for _, status := range []types.TaskStatus{
		types.TaskStatusInProgress,
		types.TaskStatusPendingAcceptance,
		types.TaskStatusCompleted,
	} {
		value := string(status)
		if _, err := service.UpdateTask(ctx, created.PublicID, &contract.UpdateTaskRequest{Status: &value}); err != nil {
			t.Fatalf("transition to %s: %v", status, err)
		}
	}
}

func seedAssistantForTaskM1(t *testing.T, database *gorm.DB, name, status string) *types.DigitalAssistant {
	t.Helper()
	assistant := &types.DigitalAssistant{
		PublicID:   "asst_" + strings.ReplaceAll(strings.ToLower(name), " ", "_"),
		OrgID:      1,
		OwnerID:    1,
		Visibility: types.DigitalAssistantVisibilityPublic,
		Name:       name,
		Status:     status,
		Source:     "custom",
	}
	if err := dbpkg.CreateDigitalAssistant(context.Background(), database, assistant); err != nil {
		t.Fatalf("CreateDigitalAssistant: %v", err)
	}
	return assistant
}

func TestCreateTask_BindsAssigneeToProject(t *testing.T) {
	database := setupTestDB(t)
	service := NewTaskService(database, newTestPermissionService(database))
	ctx := setupTestContextWithCaller(t)
	project := mustCreateTaskProjectForM1(t, database, "prj_m1_assignee_bind")
	assistant := seedAssistantForTaskM1(t, database, "Builder", string(types.DigitalAssistantStatusActive))

	created, err := service.CreateTask(ctx, &contract.CreateTaskRequest{
		ProjectID:  project.PublicID,
		Title:      "派给 Builder",
		AssigneeID: &assistant.ID,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if created.AssigneeID == nil || *created.AssigneeID != assistant.ID {
		t.Fatalf("assignee = %#v, want %d", created.AssigneeID, assistant.ID)
	}

	resource, err := dbpkg.GetResourceByBizID(ctx, database, 1, types.ResourceTypeProject, project.ID)
	if err != nil || resource == nil {
		t.Fatalf("project resource: %v %#v", err, resource)
	}
	binding, err := dbpkg.GetResourceBindingByAssistantID(ctx, database, resource.ID, assistant.ID)
	if err != nil {
		t.Fatalf("GetResourceBindingByAssistantID: %v", err)
	}
	if binding == nil {
		t.Fatal("assignee was not bound to project")
	}
}

func TestCreateTask_RejectsInactiveAssignee(t *testing.T) {
	database := setupTestDB(t)
	service := NewTaskService(database, newTestPermissionService(database))
	ctx := setupTestContextWithCaller(t)
	project := mustCreateTaskProjectForM1(t, database, "prj_m1_assignee_inactive")
	assistant := seedAssistantForTaskM1(t, database, "Paused", string(types.DigitalAssistantStatusPaused))

	if _, err := service.CreateTask(ctx, &contract.CreateTaskRequest{
		ProjectID:  project.PublicID,
		Title:      "不能派给暂停队友",
		AssigneeID: &assistant.ID,
	}); err == nil {
		t.Fatal("expected inactive assignee to be rejected")
	}
}

type recordingTaskSessionService struct {
	contract.SessionService
	addMessageCalls []*contract.AddMessageRequest
	addMessageIDs   []string
	err             error
}

func (r *recordingTaskSessionService) AddMessage(_ context.Context, sessionID string, req *contract.AddMessageRequest) (*contract.SessionMessage, error) {
	r.addMessageIDs = append(r.addMessageIDs, sessionID)
	r.addMessageCalls = append(r.addMessageCalls, req)
	if r.err != nil {
		return nil, r.err
	}
	return &contract.SessionMessage{}, nil
}

func seedTaskWithSessionForM1(t *testing.T, database *gorm.DB, publicID string, status types.TaskStatus) (*types.Task, *types.Session) {
	t.Helper()
	ctx := context.Background()
	project := mustCreateTaskProjectForM1(t, database, "prj_"+publicID)
	task := &types.Task{
		PublicID:  publicID,
		OrgID:     1,
		OwnerID:   1,
		ProjectID: project.ID,
		TaskType:  types.TaskTypeGeneral,
		Title:     "交付任务",
		Status:    string(status),
	}
	if err := dbpkg.CreateTask(ctx, database, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	session := &types.Session{
		PublicID:  "sess_" + publicID,
		Type:      types.SessionTypeTask,
		Uin:       1,
		OrgID:     1,
		ProjectID: &project.ID,
		TaskID:    &task.ID,
		Status:    string(types.SessionStatusActive),
	}
	if err := dbpkg.CreateSession(ctx, database, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	task.SessionID = &session.ID
	if err := dbpkg.UpdateTask(ctx, database, task); err != nil {
		t.Fatalf("UpdateTask session: %v", err)
	}
	return task, session
}

func TestGetTaskDeliverablesAggregatesArtifactsWithoutDuplicates(t *testing.T) {
	database := setupTestDB(t)
	service := NewTaskService(database, newTestPermissionService(database))
	ctx := setupTestContextWithCaller(t)
	task, session := seedTaskWithSessionForM1(t, database, "task_deliverables", types.TaskStatusPendingAcceptance)

	artifactA := types.MessageArtifact{ArtifactID: "art_a", Title: "报告", Filename: "report.md"}
	artifactB := types.MessageArtifact{ArtifactID: "art_b", Title: "截图", Filename: "shot.png"}
	for i, artifacts := range []types.MessageArtifactSlice{{artifactA}, {artifactA, artifactB}} {
		msg := &types.SessionMessage{
			SessionID:   session.ID,
			Role:        string(types.MessageRoleAssistant),
			Content:     "交付",
			MessageType: string(types.MessageTypeText),
			Status:      string(types.MessageStatusCompleted),
			Sequence:    int64(i + 1),
			Timestamp:   time.Now().UnixMilli(),
			Artifacts:   artifacts,
		}
		if err := dbpkg.CreateMessage(ctx, database, msg); err != nil {
			t.Fatalf("CreateMessage: %v", err)
		}
	}

	for _, file := range []*types.ProjectFile{
		{FilePublicID: "file_report", OrgID: 1, ProjectID: task.ProjectID, TaskID: task.ID, ResourceType: types.ProjectFileResourceTypeArtifact, RelativePath: "report.md", InitialFilePublicID: "file_report", VersionNo: 1},
		{FilePublicID: "file_upload", OrgID: 1, ProjectID: task.ProjectID, TaskID: task.ID, ResourceType: types.ProjectFileResourceTypeUserUpload, RelativePath: "input.txt", InitialFilePublicID: "file_upload", VersionNo: 1},
	} {
		if err := dbpkg.CreateProjectFile(ctx, database, file); err != nil {
			t.Fatalf("CreateProjectFile: %v", err)
		}
	}

	got, err := service.GetTaskDeliverables(ctx, task.PublicID)
	if err != nil {
		t.Fatalf("GetTaskDeliverables: %v", err)
	}
	if len(got.Artifacts) != 2 {
		t.Fatalf("artifacts = %d, want 2: %#v", len(got.Artifacts), got.Artifacts)
	}
	if len(got.Files) != 1 {
		t.Fatalf("files = %d, want only artifact file: %#v", len(got.Files), got.Files)
	}
	if got.Files[0].RelativePath != "report.md" {
		t.Fatalf("unexpected deliverable file: %#v", got.Files[0])
	}
}

func TestAcceptTaskOnlyFromPendingAcceptance(t *testing.T) {
	database := setupTestDB(t)
	service := NewTaskService(database, newTestPermissionService(database))
	ctx := setupTestContextWithCaller(t)
	task, _ := seedTaskWithSessionForM1(t, database, "task_accept", types.TaskStatusPendingAcceptance)

	accepted, err := service.AcceptTask(ctx, task.PublicID, "验收通过")
	if err != nil {
		t.Fatalf("AcceptTask: %v", err)
	}
	if accepted.Status != string(types.TaskStatusCompleted) {
		t.Fatalf("status = %s", accepted.Status)
	}
	if accepted.AcceptedBy == nil || *accepted.AcceptedBy != 1 || accepted.AcceptedAt == nil {
		t.Fatalf("acceptance metadata = %#v", accepted)
	}
	if accepted.AcceptanceNote != "验收通过" {
		t.Fatalf("note = %q", accepted.AcceptanceNote)
	}

	if _, err := service.AcceptTask(ctx, task.PublicID, "再次验收"); !errors.Is(err, ErrTaskNotPendingAcceptance) {
		t.Fatalf("second accept error = %v, want ErrTaskNotPendingAcceptance", err)
	}
}

func TestRejectTaskQueuesReworkMessage(t *testing.T) {
	database := setupTestDB(t)
	notifier := &recordingTaskSessionService{}
	service := NewTaskServiceWithSession(database, newTestPermissionService(database), notifier)
	ctx := setupTestContextWithCaller(t)
	task, session := seedTaskWithSessionForM1(t, database, "task_reject", types.TaskStatusPendingAcceptance)

	rejected, err := service.RejectTask(ctx, task.PublicID, "必须补充测试")
	if err != nil {
		t.Fatalf("RejectTask: %v", err)
	}
	if rejected.Status != string(types.TaskStatusInProgress) {
		t.Fatalf("status = %s", rejected.Status)
	}
	if rejected.AcceptanceNote != "必须补充测试" {
		t.Fatalf("note = %q", rejected.AcceptanceNote)
	}
	if len(notifier.addMessageCalls) != 1 || notifier.addMessageIDs[0] != session.PublicID {
		t.Fatalf("notifier calls = %#v ids=%#v", notifier.addMessageCalls, notifier.addMessageIDs)
	}
	if !strings.Contains(notifier.addMessageCalls[0].Content, "必须补充测试") {
		t.Fatalf("rework content = %q", notifier.addMessageCalls[0].Content)
	}

	if _, err := service.RejectTask(ctx, task.PublicID, "重复驳回"); !errors.Is(err, ErrTaskNotPendingAcceptance) {
		t.Fatalf("second reject error = %v, want ErrTaskNotPendingAcceptance", err)
	}
}

func TestRejectTaskRestoresPendingAcceptanceWhenReworkFails(t *testing.T) {
	database := setupTestDB(t)
	notifier := &recordingTaskSessionService{err: errors.New("worker not ready")}
	service := NewTaskServiceWithSession(database, newTestPermissionService(database), notifier)
	ctx := setupTestContextWithCaller(t)
	task, _ := seedTaskWithSessionForM1(t, database, "task_reject_failure", types.TaskStatusPendingAcceptance)
	if err := database.Model(&types.Task{}).Where("id = ?", task.ID).Update("acceptance_note", "旧意见").Error; err != nil {
		t.Fatalf("seed note: %v", err)
	}

	if _, err := service.RejectTask(ctx, task.PublicID, "新意见"); err == nil {
		t.Fatal("expected reject to fail when rework message fails")
	}
	var got types.Task
	if err := database.First(&got, task.ID).Error; err != nil {
		t.Fatalf("load task: %v", err)
	}
	if got.Status != string(types.TaskStatusPendingAcceptance) {
		t.Fatalf("status = %s, want pending_acceptance", got.Status)
	}
	if got.AcceptanceNote != "旧意见" {
		t.Fatalf("acceptance_note = %q, want old note", got.AcceptanceNote)
	}
}
