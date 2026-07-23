package service

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

func TestRemoveSkillFromProjectMetadata_MatchesCode(t *testing.T) {
	meta := types.ObjectMetadata{
		Tags: []string{"tag-a"},
		Type: "demo",
		Extra: map[string]interface{}{
			"skills": []interface{}{
				map[string]interface{}{"code": "demo-skill", "name": "Demo Skill"},
				map[string]interface{}{"code": "other-skill", "name": "Other"},
			},
			"note": "keep-me",
		},
	}

	newMeta, changed := removeSkillFromProjectMetadata(meta, "demo-skill")
	if !changed {
		t.Fatal("expected metadata change")
	}
	if len(newMeta.Tags) != 1 || newMeta.Tags[0] != "tag-a" {
		t.Fatalf("tags = %#v, want preserved", newMeta.Tags)
	}
	if newMeta.Type != "demo" {
		t.Fatalf("type = %q, want demo", newMeta.Type)
	}
	if newMeta.Extra["note"] != "keep-me" {
		t.Fatalf("extra.note = %#v, want keep-me", newMeta.Extra["note"])
	}

	skills, ok := newMeta.Extra["skills"].([]interface{})
	if !ok {
		t.Fatalf("skills type = %T, want []interface{}", newMeta.Extra["skills"])
	}
	if len(skills) != 1 {
		t.Fatalf("skills len = %d, want 1", len(skills))
	}
	entry, ok := skills[0].(map[string]interface{})
	if !ok || entry["code"] != "other-skill" {
		t.Fatalf("remaining skill = %#v, want other-skill", skills[0])
	}
}

func TestCreateProjectRecordsInitialActivitiesInOrder(t *testing.T) {
	database := setupTestDB(t)
	ctx := auth.WithContext(context.Background(), &types.Caller{
		Uin:   1,
		OrgID: 1,
		State: types.AuthStateSucc,
	}, &types.Trace{})

	seedReadyAssistant(t, database, "default", "默认队友", "默认队友")
	assistant := seedReadyAssistant(t, database, "analyst", "分析专家", "分析专家")
	user := &types.User{
		PublicID: "usr_member",
		Name:     "成员",
		Email:    "member@example.com",
		Phone:    "13800000002",
	}
	if err := database.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := database.Create(&types.UserOrg{
		Uin:    2,
		UserID: user.ID,
		OrgID:  1,
	}).Error; err != nil {
		t.Fatalf("create user org: %v", err)
	}

	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil)
	project, err := service.CreateProject(ctx, &contract.CreateProjectRequest{
		Name: "手动创建项目",
		Members: []contract.MemberInput{
			{Type: "assistant", ID: assistant.PublicID},
			{Type: "user", ID: user.PublicID},
		},
		Metadata: map[string]interface{}{
			"extra": map[string]interface{}{
				"skills": []interface{}{
					map[string]interface{}{"code": "skill-alpha", "name": "Skill Alpha"},
					map[string]interface{}{"code": "skill-beta", "name": "Skill Beta"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	activities, err := infradb.ListProjectActivities(ctx, database, infradb.ProjectActivityListOptions{
		ProjectID: project.PublicID,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListProjectActivities failed: %v", err)
	}
	if len(activities) != 3 {
		t.Fatalf("activity count = %d, want 3", len(activities))
	}

	// 列表默认倒序；按时间正序验证创建项目 -> 队友 -> 技能。
	ordered := []*types.ProjectActivity{activities[2], activities[1], activities[0]}
	gotActions := []types.ProjectActivityAction{
		ordered[0].ActionType,
		ordered[1].ActionType,
		ordered[2].ActionType,
	}
	wantActions := []types.ProjectActivityAction{
		types.ProjectActivityActionProjectCreated,
		types.ProjectActivityActionParticipantsChanged,
		types.ProjectActivityActionSkillsChanged,
	}
	if !reflect.DeepEqual(gotActions, wantActions) {
		t.Fatalf("actions = %#v, want %#v", gotActions, wantActions)
	}
	if !ordered[0].CreatedAt.Before(ordered[1].CreatedAt) || !ordered[1].CreatedAt.Before(ordered[2].CreatedAt) {
		t.Fatalf("created_at order is not project -> participants -> skills: %v, %v, %v",
			ordered[0].CreatedAt, ordered[1].CreatedAt, ordered[2].CreatedAt)
	}
	if !reflect.DeepEqual(ordered[1].Payload.AddedAITeammateIDs, []string{assistant.PublicID}) {
		t.Fatalf("added ai teammate ids = %#v, want %s", ordered[1].Payload.AddedAITeammateIDs, assistant.PublicID)
	}
	if !reflect.DeepEqual(ordered[1].Payload.AddedMemberIDs, []string{user.PublicID}) {
		t.Fatalf("added member ids = %#v, want %s", ordered[1].Payload.AddedMemberIDs, user.PublicID)
	}
	if !reflect.DeepEqual(ordered[2].Payload.AddedSkillIDs, []string{"skill-alpha", "skill-beta"}) {
		t.Fatalf("added skill ids = %#v", ordered[2].Payload.AddedSkillIDs)
	}
}

func TestRemoveSkillFromProjectMetadata_MatchesNameCaseInsensitive(t *testing.T) {
	meta := types.ObjectMetadata{
		Extra: map[string]interface{}{
			"skills": []interface{}{
				map[string]interface{}{"code": "alpha", "name": "Demo Skill"},
			},
		},
	}

	_, changed := removeSkillFromProjectMetadata(meta, "demo skill")
	if !changed {
		t.Fatal("expected metadata change when matching display name")
	}
}

func TestRemoveSkillFromProjectMetadata_NoSkills(t *testing.T) {
	meta := types.ObjectMetadata{
		Extra: map[string]interface{}{"note": "only"},
	}

	_, changed := removeSkillFromProjectMetadata(meta, "demo-skill")
	if changed {
		t.Fatal("expected no change without skills array")
	}
}

func TestRemoveSkillFromProjectMetadata_NoMatch(t *testing.T) {
	meta := types.ObjectMetadata{
		Extra: map[string]interface{}{
			"skills": []interface{}{
				map[string]interface{}{"code": "other-skill", "name": "Other"},
			},
		},
	}

	_, changed := removeSkillFromProjectMetadata(meta, "demo-skill")
	if changed {
		t.Fatal("expected no change when skill not referenced")
	}
}

func setupProjectSkillReferenceDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, func()) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	database, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open gorm db: %v", err)
	}
	cleanup := func() {
		sqlDB.Close()
	}
	return database, mock, cleanup
}

func TestCleanupOrgProjectSkillReferences_UpdatesMatchingProjects(t *testing.T) {
	database, mock, cleanup := setupProjectSkillReferenceDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	columns := []string{
		"id", "created_at", "updated_at", "deleted_at", "public_id",
		"org_id", "owner_id", "name", "description", "objective", "status",
		"gitea_repo_full_name", "gitea_repo_id", "gitea_default_branch", "metadata",
	}
	metadata := []byte(`{"extra":{"skills":[{"code":"demo-skill","name":"Demo Skill"},{"code":"keep","name":"Keep"}]}}`)

	mock.ExpectQuery(`SELECT .* FROM "leros_project" WHERE \(org_id = \$1 AND deleted_at IS NULL\) AND \(EXISTS`).
		WithArgs(uint(100), "demo-skill", "demo-skill").
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			1, now, now, nil, "prj_demo",
			100, 1, "Demo Project", "", "", "active",
			"", 0, "main", metadata,
		))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "leros_project" SET`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	updated, err := cleanupOrgProjectSkillReferences(ctx, database, 100, "demo-skill")
	if err != nil {
		t.Fatalf("cleanupOrgProjectSkillReferences failed: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

// ---- file / artifact auth helpers ----

func seedProjectFileWithResource(
	t *testing.T,
	database *gorm.DB,
	project *types.Project,
	projectResourceID uint,
	resourceType types.ProjectFileResourceType,
	filePublicID string,
) *types.ProjectFile {
	t.Helper()
	ctx := context.Background()

	fileUpload := &types.FileUpload{
		PublicID:     filePublicID,
		OrgID:        project.OrgID,
		OwnerID:      1,
		Filename:     "report.pdf",
		OriginalName: "report.pdf",
		MimeType:     "application/pdf",
		FileSize:     1024,
		StorageURI:   "filestore://default/uploads/report.pdf",
		Status:       "active",
	}
	if err := database.Create(fileUpload).Error; err != nil {
		t.Fatalf("create file upload: %v", err)
	}

	relativePath := "report.pdf"
	if resourceType == types.ProjectFileResourceTypeUserUpload {
		relativePath = "uploads/report.pdf"
	}

	projectFile := &types.ProjectFile{
		FilePublicID: filePublicID,
		OrgID:        project.OrgID,
		ProjectID:    project.ID,
		ResourceID:   fileUpload.ID,
		ResourceType: resourceType,
		RelativePath: relativePath,
		Uin:          1,
	}
	if err := infradb.CreateProjectFileVersion(ctx, database, projectFile); err != nil {
		t.Fatalf("create project file version: %v", err)
	}

	parentID := projectResourceID
	resource := &types.Resource{
		OrgID:                 project.OrgID,
		Uin:                   1,
		Type:                  types.ResourceTypeFile,
		BizID:                 projectFile.ID,
		ParentResourceID:      &parentID,
		ParentResourcePathIDs: types.ResourcePathIDs{projectResourceID},
	}
	if resourceType == types.ProjectFileResourceTypeArtifact {
		resource.Type = types.ResourceTypeArtifact
	}
	if err := infradb.CreateResource(ctx, database, resource); err != nil {
		t.Fatalf("create file/artifact resource: %v", err)
	}

	return projectFile
}

func seedProjectWithFileTree(t *testing.T) (*gorm.DB, *types.Project, *types.Resource) {
	t.Helper()
	database := setupTestDB(t)
	project := &types.Project{
		PublicID: "prj_file_auth",
		Name:     "File Auth Project",
		OrgID:    1,
		OwnerID:  1,
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}

	ctx := context.Background()
	projectResource := &types.Resource{
		OrgID: project.OrgID,
		Uin:   1,
		Type:  types.ResourceTypeProject,
		BizID: project.ID,
	}
	if err := infradb.CreateResource(ctx, database, projectResource); err != nil {
		t.Fatalf("create project resource: %v", err)
	}
	ownerUin := uint(1)
	if err := infradb.CreateResourceBinding(ctx, database, &types.ResourceBinding{
		OrgID:      project.OrgID,
		Uin:        &ownerUin,
		ResourceID: projectResource.ID,
		Role:       types.ResourceRoleOwner,
	}); err != nil {
		t.Fatalf("create owner binding: %v", err)
	}

	seedProjectFileWithResource(t, database, project, projectResource.ID, types.ProjectFileResourceTypeUserUpload, "file_upload_1")
	seedProjectFileWithResource(t, database, project, projectResource.ID, types.ProjectFileResourceTypeArtifact, "artifact_1")

	return database, project, projectResource
}

func TestGetProjectFileTree_FiltersByFileView(t *testing.T) {
	database, project, _ := seedProjectWithFileTree(t)
	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil)

	ownerCtx := setupTestContextWithCaller(t)
	tree, err := service.GetProjectFileTree(ownerCtx, project.PublicID, contract.ProjectFileTreeQuery{})
	if err != nil {
		t.Fatalf("GetProjectFileTree owner: %v", err)
	}
	if len(tree) != 2 {
		t.Fatalf("owner tree len = %d, want 2", len(tree))
	}

	outsiderCtx := setupTestContextWithCallerUin(t, 99)
	tree, err = service.GetProjectFileTree(outsiderCtx, project.PublicID, contract.ProjectFileTreeQuery{})
	if err != nil {
		t.Fatalf("GetProjectFileTree outsider: %v", err)
	}
	if len(tree) != 0 {
		t.Fatalf("outsider tree len = %d, want 0 (filtered by file view)", len(tree))
	}
}

func TestDownloadProjectFile_RequiresDownload(t *testing.T) {
	database, project, _ := seedProjectWithFileTree(t)
	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil)

	outsiderCtx := setupTestContextWithCallerUin(t, 99)
	_, _, _, err := service.DownloadProjectFile(outsiderCtx, project.PublicID, "uploads/report.pdf")
	if err == nil {
		t.Fatal("expected permission denied for outsider download")
	}
	if !strings.HasPrefix(err.Error(), "permission denied") {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

// ---- member management auth helpers ----

func seedTestUser(t *testing.T, database *gorm.DB, publicID string, uin uint) *types.User {
	t.Helper()
	user := &types.User{
		PublicID: publicID,
		Name:     publicID,
		Email:    publicID + "@example.com",
		Phone:    publicID,
	}
	if err := database.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := database.Create(&types.UserOrg{
		Uin:    uin,
		UserID: user.ID,
		OrgID:  1,
	}).Error; err != nil {
		t.Fatalf("create user org: %v", err)
	}
	return user
}

func seedProjectWithResource(t *testing.T, database *gorm.DB, publicID string) (*types.Project, *types.Resource) {
	t.Helper()
	project := &types.Project{
		PublicID: publicID,
		Name:     "Member Auth Project",
		OrgID:    1,
		OwnerID:  1,
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	ctx := context.Background()
	resource := &types.Resource{
		OrgID: project.OrgID,
		Uin:   1,
		Type:  types.ResourceTypeProject,
		BizID: project.ID,
	}
	if err := infradb.CreateResource(ctx, database, resource); err != nil {
		t.Fatalf("create project resource: %v", err)
	}
	return project, resource
}

func TestBindProjectUserMembers_AdminCannotCreateOwner(t *testing.T) {
	database := setupTestDB(t)
	seedReadyAssistant(t, database, "default", "默认队友", "默认队友")
	project, resource := seedProjectWithResource(t, database, "prj_bind_admin")
	seedTestUser(t, database, "usr_admin", 2)
	seedTestUser(t, database, "usr_target", 3)
	seedProjectResourceBinding(t, database, 1, project.ID, 2, types.ResourceRoleAdmin)

	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil).(*projectService)
	adminCtx := setupTestContextWithCallerUin(t, 2)
	err := service.bindProjectUserMembers(adminCtx, database, 1, resource.ID, project.ID, &types.Caller{
		Uin:   2,
		OrgID: 1,
		State: types.AuthStateSucc,
	}, []userMemberInput{{PublicID: "usr_target", Role: types.ResourceRoleOwner}})
	if err == nil {
		t.Fatal("expected permission denied when admin creates owner")
	}
	if !strings.HasPrefix(err.Error(), "permission denied") {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

func TestSyncProjectUserMembers_AdminCannotPromoteToOwner(t *testing.T) {
	database := setupTestDB(t)
	seedReadyAssistant(t, database, "default", "默认队友", "默认队友")
	project, _ := seedProjectWithResource(t, database, "prj_sync_promote")
	seedTestUser(t, database, "usr_admin", 2)
	seedTestUser(t, database, "usr_member", 3)
	seedProjectResourceBinding(t, database, 1, project.ID, 1, types.ResourceRoleOwner)
	seedProjectResourceBinding(t, database, 1, project.ID, 2, types.ResourceRoleAdmin)
	seedProjectResourceBinding(t, database, 1, project.ID, 3, types.ResourceRoleMember)

	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil)
	adminCtx := auth.WithContext(context.Background(), &types.Caller{
		Uin:   2,
		OrgID: 1,
		State: types.AuthStateSucc,
	}, &types.Trace{RequestID: "test", TraceID: "test"})

	_, err := service.UpdateProject(adminCtx, project.PublicID, &contract.UpdateProjectRequest{
		Members: []contract.MemberInput{
			{Type: "user", ID: "usr_member", Role: string(types.ResourceRoleOwner)},
		},
	})
	if err == nil {
		t.Fatal("expected permission denied when admin promotes member to owner")
	}
	if !strings.HasPrefix(err.Error(), "permission denied") {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

func TestSyncProjectUserMembers_AdminCannotChangeOwnerRole(t *testing.T) {
	database := setupTestDB(t)
	seedReadyAssistant(t, database, "default", "默认队友", "默认队友")
	project, _ := seedProjectWithResource(t, database, "prj_sync_admin_owner")
	seedTestUser(t, database, "usr_admin", 2)
	seedProjectResourceBinding(t, database, 1, project.ID, 1, types.ResourceRoleOwner)
	seedProjectResourceBinding(t, database, 1, project.ID, 2, types.ResourceRoleAdmin)

	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil)
	adminCtx := setupTestContextWithCallerUin(t, 2)

	_, err := service.UpdateProject(adminCtx, project.PublicID, &contract.UpdateProjectRequest{
		Members: []contract.MemberInput{
			{Type: "user", ID: "usr_test", Role: string(types.ResourceRoleMember)},
		},
	})
	if err == nil {
		t.Fatal("expected permission denied when admin changes owner role")
	}
	if !strings.HasPrefix(err.Error(), "permission denied") {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

func TestSyncProjectUserMembers_OwnerCanAddMember(t *testing.T) {
	database := setupTestDB(t)
	seedReadyAssistant(t, database, "default", "默认队友", "默认队友")
	project, _ := seedProjectWithResource(t, database, "prj_sync_add_member")
	seedTestUser(t, database, "usr_new_member", 4)
	seedProjectResourceBinding(t, database, 1, project.ID, 1, types.ResourceRoleOwner)

	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil)
	ownerCtx := setupTestContextWithCaller(t)

	updated, err := service.UpdateProject(ownerCtx, project.PublicID, &contract.UpdateProjectRequest{
		Members: []contract.MemberInput{
			{Type: "user", ID: "usr_new_member", Role: string(types.ResourceRoleMember)},
		},
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if updated == nil {
		t.Fatal("expected updated project")
	}

	resource, err := infradb.GetResourceByBizID(context.Background(), database, 1, types.ResourceTypeProject, project.ID)
	if err != nil {
		t.Fatalf("get resource: %v", err)
	}
	binding, err := infradb.GetResourceBindingByUin(context.Background(), database, resource.ID, 4)
	if err != nil {
		t.Fatalf("get member binding: %v", err)
	}
	if binding == nil {
		t.Fatal("expected member binding to be created")
	}
	if binding.Role != types.ResourceRoleMember {
		t.Fatalf("expected member role, got %q", binding.Role)
	}
}

func TestCreateProject_BindsAssistantMembers(t *testing.T) {
	database := setupTestDB(t)
	defaultAsst := seedReadyAssistant(t, database, "default", "默认队友", "默认队友")
	extraAsst := seedReadyAssistant(t, database, "analyst", "分析专家", "分析专家")

	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil)
	ownerCtx := setupTestContextWithCaller(t)

	project, err := service.CreateProject(ownerCtx, &contract.CreateProjectRequest{
		Name: "Assistant Binding Project",
		Members: []contract.MemberInput{
			{Type: "assistant", ID: extraAsst.PublicID},
		},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	proj, err := infradb.GetProjectByPublicID(context.Background(), database, 1, project.PublicID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	resource, err := infradb.GetResourceByBizID(context.Background(), database, 1, types.ResourceTypeProject, proj.ID)
	if err != nil {
		t.Fatalf("get resource: %v", err)
	}

	defaultBinding, err := infradb.GetResourceBindingByAssistantID(context.Background(), database, resource.ID, defaultAsst.ID)
	if err != nil {
		t.Fatalf("get default assistant binding: %v", err)
	}
	if defaultBinding == nil {
		t.Fatal("expected default assistant binding to be created")
	}
	if defaultBinding.Role != types.ResourceRoleMember {
		t.Fatalf("expected default assistant member role, got %q", defaultBinding.Role)
	}

	extraBinding, err := infradb.GetResourceBindingByAssistantID(context.Background(), database, resource.ID, extraAsst.ID)
	if err != nil {
		t.Fatalf("get extra assistant binding: %v", err)
	}
	if extraBinding == nil {
		t.Fatal("expected extra assistant binding to be created")
	}
	if extraBinding.Role != types.ResourceRoleMember {
		t.Fatalf("expected extra assistant member role, got %q", extraBinding.Role)
	}
}

func TestSyncProjectAssistantMembers_OwnerCanAddAndRemoveAssistantBinding(t *testing.T) {
	database := setupTestDB(t)
	defaultAsst := seedReadyAssistant(t, database, "default", "默认队友", "默认队友")
	extraAsst := seedReadyAssistant(t, database, "analyst", "分析专家", "分析专家")
	project, resource := seedProjectWithResource(t, database, "prj_sync_asst_binding")
	seedProjectResourceBinding(t, database, 1, project.ID, 1, types.ResourceRoleOwner)

	defaultID := defaultAsst.ID
	if err := infradb.CreateResourceBinding(context.Background(), database, &types.ResourceBinding{
		OrgID:       1,
		AssistantID: &defaultID,
		ResourceID:  resource.ID,
		Role:        types.ResourceRoleMember,
	}); err != nil {
		t.Fatalf("seed default assistant binding: %v", err)
	}

	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil)
	ownerCtx := setupTestContextWithCaller(t)

	if _, err := service.UpdateProject(ownerCtx, project.PublicID, &contract.UpdateProjectRequest{
		Members: []contract.MemberInput{
			{Type: "assistant", ID: extraAsst.PublicID},
		},
	}); err != nil {
		t.Fatalf("UpdateProject add assistant: %v", err)
	}

	addedBinding, err := infradb.GetResourceBindingByAssistantID(context.Background(), database, resource.ID, extraAsst.ID)
	if err != nil {
		t.Fatalf("get added assistant binding: %v", err)
	}
	if addedBinding == nil {
		t.Fatal("expected assistant binding to be created on add")
	}

	if _, err := service.UpdateProject(ownerCtx, project.PublicID, &contract.UpdateProjectRequest{
		Members: []contract.MemberInput{},
	}); err != nil {
		t.Fatalf("UpdateProject remove assistant: %v", err)
	}

	removedBinding, err := infradb.GetResourceBindingByAssistantID(context.Background(), database, resource.ID, extraAsst.ID)
	if err != nil {
		t.Fatalf("get removed assistant binding: %v", err)
	}
	if removedBinding != nil {
		t.Fatal("expected assistant binding to be removed")
	}

	defaultBinding, err := infradb.GetResourceBindingByAssistantID(context.Background(), database, resource.ID, defaultAsst.ID)
	if err != nil {
		t.Fatalf("get default assistant binding after remove: %v", err)
	}
	if defaultBinding == nil {
		t.Fatal("expected default assistant binding to remain")
	}
}

func TestDetailProject_IncludesAllTasksWithoutPerTaskCan(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)

	project, resource := seedProjectWithResource(t, database, "prj_detail_tasks")
	seedProjectResourceBinding(t, database, 1, project.ID, 1, types.ResourceRoleOwner)

	taskTitles := []string{"Task Alpha", "Task Beta", "Task Gamma"}
	for i, title := range taskTitles {
		task := &types.Task{
			PublicID:  fmt.Sprintf("task_detail_%d", i),
			OrgID:     1,
			OwnerID:   1,
			ProjectID: project.ID,
			Title:     title,
			Status:    string(types.TaskStatusCreated),
		}
		if err := database.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", title, err)
		}
		seedTaskResource(t, database, task, resource.ID)
	}

	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil)
	detail, err := service.DetailProject(ctx, project.PublicID)
	if err != nil {
		t.Fatalf("DetailProject: %v", err)
	}
	if len(detail.Tasks) != len(taskTitles) {
		t.Fatalf("task count = %d, want %d", len(detail.Tasks), len(taskTitles))
	}

	gotTitles := make([]string, 0, len(detail.Tasks))
	for _, item := range detail.Tasks {
		gotTitles = append(gotTitles, item.Task.Title)
	}
	for _, want := range taskTitles {
		found := false
		for _, got := range gotTitles {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing task title %q in %#v", want, gotTitles)
		}
	}
}

func TestDetailProject_ReturnsUserProfileByMemberUin(t *testing.T) {
	database := setupTestDB(t)
	// 先占用用户表主键，确保用户表 ID 与组织成员 UIN 不相等，从而覆盖真实映射关系。
	seedTestUser(t, database, "usr_padding", 2)
	owner := seedTestUser(t, database, "usr_owner", 17)
	owner.Name = "项目创建者"
	owner.AvatarURL = "file_owner_avatar"
	if err := database.Save(owner).Error; err != nil {
		t.Fatalf("update owner profile: %v", err)
	}

	project, _ := seedProjectWithResource(t, database, "prj_detail_member_profile")
	seedProjectResourceBinding(t, database, 1, project.ID, 17, types.ResourceRoleOwner)

	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil)
	detail, err := service.DetailProject(setupTestContextWithCallerUin(t, 17), project.PublicID)
	if err != nil {
		t.Fatalf("DetailProject: %v", err)
	}
	if len(detail.Members) != 1 {
		t.Fatalf("member count = %d, want 1", len(detail.Members))
	}

	member := detail.Members[0]
	if member.MemberID != 17 || member.PublicID != owner.PublicID {
		t.Fatalf("member identity = (%d, %q), want (17, %q)", member.MemberID, member.PublicID, owner.PublicID)
	}
	if member.Name != owner.Name || member.AvatarURL != owner.AvatarURL {
		t.Fatalf("member profile = (%q, %q), want (%q, %q)", member.Name, member.AvatarURL, owner.Name, owner.AvatarURL)
	}
}

func TestLeaveProject_RemovesCallerBinding(t *testing.T) {
	database := setupTestDB(t)
	project, _ := seedProjectWithResource(t, database, "prj_leave_member")
	seedTestUser(t, database, "usr_member", 3)
	seedProjectResourceBinding(t, database, 1, project.ID, 3, types.ResourceRoleMember)

	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil)
	memberCtx := setupTestContextWithCallerUin(t, 3)

	if err := service.LeaveProject(memberCtx, project.PublicID); err != nil {
		t.Fatalf("LeaveProject: %v", err)
	}

	ctx := context.Background()
	resource, err := infradb.GetResourceByBizID(ctx, database, 1, types.ResourceTypeProject, project.ID)
	if err != nil {
		t.Fatalf("get resource: %v", err)
	}
	binding, err := infradb.GetResourceBindingByUin(ctx, database, resource.ID, 3)
	if err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if binding != nil {
		t.Fatal("expected member binding removed after leave")
	}
}

func seedProjectFileRecord(
	t *testing.T,
	database *gorm.DB,
	project *types.Project,
	projectResourceID uint,
	filePublicID string,
	resourceType types.ProjectFileResourceType,
	relativePath string,
	originalName string,
) *types.ProjectFile {
	t.Helper()
	ctx := context.Background()

	if originalName == "" {
		originalName = filepath.Base(strings.TrimSuffix(relativePath, "/"))
	}

	var resourceID uint
	fileUpload := &types.FileUpload{
		PublicID:     filePublicID,
		OrgID:        project.OrgID,
		OwnerID:      1,
		Filename:     originalName,
		OriginalName: originalName,
		MimeType:     "application/octet-stream",
		FileSize:     1024,
		StorageURI:   "filestore://default/" + relativePath,
		Status:       "active",
	}
	if err := database.Create(fileUpload).Error; err != nil {
		t.Fatalf("create file upload: %v", err)
	}
	resourceID = fileUpload.ID

	projectFile := &types.ProjectFile{
		FilePublicID: filePublicID,
		OrgID:        project.OrgID,
		ProjectID:    project.ID,
		ResourceID:   resourceID,
		ResourceType: resourceType,
		RelativePath: relativePath,
		Uin:          1,
	}
	if err := database.Create(projectFile).Error; err != nil {
		t.Fatalf("create project file: %v", err)
	}

	resourceKind := types.ResourceTypeFile
	if resourceType == types.ProjectFileResourceTypeArtifact {
		resourceKind = types.ResourceTypeArtifact
	}
	resource := &types.Resource{
		OrgID:                 project.OrgID,
		Uin:                   1,
		Type:                  resourceKind,
		BizID:                 projectFile.ID,
		ParentResourceID:      &projectResourceID,
		ParentResourcePathIDs: types.ResourcePathIDs{projectResourceID},
	}
	if err := infradb.CreateResource(ctx, database, resource); err != nil {
		t.Fatalf("create file resource: %v", err)
	}

	return projectFile
}

func seedProjectFileFilterTree(t *testing.T) (*gorm.DB, *types.Project) {
	t.Helper()
	database := setupTestDB(t)
	project := &types.Project{
		PublicID: "prj_file_filter",
		Name:     "File Filter Project",
		OrgID:    1,
		OwnerID:  1,
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}

	ctx := context.Background()
	projectResource := &types.Resource{
		OrgID: project.OrgID,
		Uin:   1,
		Type:  types.ResourceTypeProject,
		BizID: project.ID,
	}
	if err := infradb.CreateResource(ctx, database, projectResource); err != nil {
		t.Fatalf("create project resource: %v", err)
	}
	ownerUin := uint(1)
	if err := infradb.CreateResourceBinding(ctx, database, &types.ResourceBinding{
		OrgID:      project.OrgID,
		Uin:        &ownerUin,
		ResourceID: projectResource.ID,
		Role:       types.ResourceRoleOwner,
	}); err != nil {
		t.Fatalf("create owner binding: %v", err)
	}

	seedProjectFileRecord(t, database, project, projectResource.ID, "file_upload_pdf", types.ProjectFileResourceTypeUserUpload, "uploads/demo/report.pdf", "report.pdf")
	seedProjectFileRecord(t, database, project, projectResource.ID, "file_upload_doc", types.ProjectFileResourceTypeUserUpload, "uploads/notes.docx", "notes.docx")
	seedProjectFileRecord(t, database, project, projectResource.ID, "file_artifact_md", types.ProjectFileResourceTypeArtifact, "summary.md", "summary.md")

	return database, project
}

func TestGetProjectFileTree_FiltersByResourceTypeAndFileExt(t *testing.T) {
	database, project := seedProjectFileFilterTree(t)
	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil)
	ownerCtx := setupTestContextWithCaller(t)

	uploadTree, err := service.GetProjectFileTree(ownerCtx, project.PublicID, contract.ProjectFileTreeQuery{
		ResourceType: string(types.ProjectFileResourceTypeUserUpload),
	})
	if err != nil {
		t.Fatalf("GetProjectFileTree upload: %v", err)
	}
	if len(uploadTree) != 2 {
		t.Fatalf("upload tree len = %d, want 2", len(uploadTree))
	}

	artifactTree, err := service.GetProjectFileTree(ownerCtx, project.PublicID, contract.ProjectFileTreeQuery{
		ResourceType: string(types.ProjectFileResourceTypeArtifact),
	})
	if err != nil {
		t.Fatalf("GetProjectFileTree artifact: %v", err)
	}
	if len(artifactTree) != 1 || artifactTree[0].Path != "summary.md" {
		t.Fatalf("artifact tree = %#v", artifactTree)
	}

	pdfTree, err := service.GetProjectFileTree(ownerCtx, project.PublicID, contract.ProjectFileTreeQuery{
		FileExt: "pdf",
	})
	if err != nil {
		t.Fatalf("GetProjectFileTree pdf: %v", err)
	}
	if len(pdfTree) != 1 || pdfTree[0].Path != "uploads/demo/report.pdf" {
		t.Fatalf("pdf tree = %#v", pdfTree)
	}
}
