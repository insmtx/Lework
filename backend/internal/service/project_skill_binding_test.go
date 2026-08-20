package service

import (
	"context"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

func TestProjectSkillBindingUsesCodeAndIsIdempotent(t *testing.T) {
	database := setupTestDB(t)
	project := &types.Project{PublicID: "project_skill", Name: "Skill Project", OrgID: 1, OwnerID: 1, Status: "active"}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	seedProjectResourceOwner(t, database, project, 1)
	plugin := &types.Plugin{
		PublicID: "plugin_skill", OwnerScope: types.OwnerScopeOrganization, OrgID: 1,
		Code: "bid-review", Kind: "skill", Name: "Bid Review", Status: types.PluginStatusActive,
		Origin: "org", CurrentRevision: 1, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	seedPluginResourceOwner(t, database, 1, plugin.ID, 1)

	service := NewProjectService(
		database,
		newTestPermissionService(database),
		nil,
		nil,
		"test",
		newTestUserRepo(map[string]uint{"usr_test": 1}),
		NewSkillDisplayTranslationService(database),
	)
	request := &contract.UpdateProjectPluginRequest{
		PublicID: project.PublicID, PluginCode: plugin.Code, Kind: "skill",
	}

	added, err := service.AddProjectPlugin(setupTestContextWithCaller(t), request)
	if err != nil {
		t.Fatalf("AddProjectPlugin() error = %v", err)
	}
	if !added.Associated || !added.Changed || added.PluginCode != plugin.Code {
		t.Fatalf("added result = %#v", added)
	}
	repeated, err := service.AddProjectPlugin(setupTestContextWithCaller(t), request)
	if err != nil {
		t.Fatalf("repeat AddProjectPlugin() error = %v", err)
	}
	if !repeated.Associated || repeated.Changed {
		t.Fatalf("repeated add result = %#v", repeated)
	}

	listed, err := service.ListProjectPlugins(setupTestContextWithCaller(t), &contract.ListProjectPluginsRequest{
		PublicID: project.PublicID, Kind: "skill", Keyword: "bid", Limit: 20,
	})
	if err != nil {
		t.Fatalf("ListProjectPlugins() error = %v", err)
	}
	if len(listed) != 1 || listed[0].Code != plugin.Code {
		t.Fatalf("listed project skills = %#v", listed)
	}

	removed, err := service.RemoveProjectPlugin(setupTestContextWithCaller(t), request)
	if err != nil {
		t.Fatalf("RemoveProjectPlugin() error = %v", err)
	}
	if removed.Associated || !removed.Changed {
		t.Fatalf("removed result = %#v", removed)
	}
	repeatedRemove, err := service.RemoveProjectPlugin(setupTestContextWithCaller(t), request)
	if err != nil {
		t.Fatalf("repeat RemoveProjectPlugin() error = %v", err)
	}
	if repeatedRemove.Associated || repeatedRemove.Changed {
		t.Fatalf("repeated remove result = %#v", repeatedRemove)
	}

	var activityCount int64
	if err := database.Model(&types.ProjectActivity{}).
		Where("project_id = ? AND action_type = ?", project.PublicID, types.ProjectActivityActionSkillsChanged).
		Count(&activityCount).Error; err != nil {
		t.Fatalf("count project activities: %v", err)
	}
	if activityCount != 2 {
		t.Fatalf("project skill activity count = %d, want 2", activityCount)
	}
}

func TestProjectSkillBindingRejectsConflictingIdentifiers(t *testing.T) {
	err := validateUpdateProjectPluginRequest(&contract.UpdateProjectPluginRequest{
		PublicID: "project", PluginID: "plugin", PluginCode: "skill-code", Kind: "skill",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("validateUpdateProjectPluginRequest() error = %v", err)
	}
}

func TestWorkerCanManageOnlyProjectSkillsByCode(t *testing.T) {
	database := setupTestDB(t)
	project := &types.Project{PublicID: "project_worker_skill", Name: "Worker Skill", OrgID: 1, OwnerID: 1, Status: "active"}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	seedProjectResourceOwner(t, database, project, 1)
	resource, err := infradb.GetResourceByBizID(context.Background(), database, 1, types.ResourceTypeProject, project.ID)
	if err != nil || resource == nil {
		t.Fatalf("get project resource: resource=%#v err=%v", resource, err)
	}
	assistant := &types.DigitalAssistant{PublicID: "assistant-skill-manager", OrgID: 1, Name: "Skill Manager"}
	if err := database.Create(assistant).Error; err != nil {
		t.Fatalf("create assistant: %v", err)
	}
	assistantID := assistant.ID
	if err := infradb.CreateResourceBinding(context.Background(), database, &types.ResourceBinding{
		OrgID: 1, AssistantID: &assistantID, ResourceID: resource.ID, Role: types.ResourceRoleMember,
	}); err != nil {
		t.Fatalf("bind assistant to project: %v", err)
	}
	if err := infradb.CreateWorkerDeployment(context.Background(), database, &types.WorkerDeployment{
		PublicID: "deployment-skill-manager", OrgID: 1, DigitalAssistantID: assistant.ID,
		WorkerID: 77, DeploymentName: "deployment-skill-manager", Status: string(types.WorkerDeploymentStatusReady),
	}); err != nil {
		t.Fatalf("create worker deployment: %v", err)
	}
	skill := &types.Plugin{
		PublicID: "plugin_worker_skill", OwnerScope: types.OwnerScopeOrganization, OrgID: 1,
		Code: "worker-skill", Kind: "skill", Name: "Worker Skill", Status: types.PluginStatusActive,
		Origin: "org", CurrentRevision: 1, CreatedBy: 1, UpdatedBy: 1,
	}
	mcp := &types.Plugin{
		PublicID: "plugin_worker_mcp", OwnerScope: types.OwnerScopeOrganization, OrgID: 1,
		Code: "worker-mcp", Kind: "mcp", Name: "Worker MCP", Status: types.PluginStatusActive,
		Origin: "org", CurrentRevision: 1, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := database.Create(skill).Error; err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if err := database.Create(mcp).Error; err != nil {
		t.Fatalf("create mcp: %v", err)
	}

	caller := &types.Caller{OrgID: 1, WorkerID: 77, Kind: types.CallerKindWorker, State: types.AuthStateSucc}
	workerCtx := auth.WithContext(context.Background(), caller, &types.Trace{RequestID: "worker-skill-test"})
	service := NewProjectService(database, newTestPermissionService(database), nil, nil, "test", nil, NewSkillDisplayTranslationService(database))

	result, err := service.AddProjectPlugin(workerCtx, &contract.UpdateProjectPluginRequest{
		PublicID: project.PublicID, PluginCode: skill.Code, Kind: "skill",
	})
	if err != nil {
		t.Fatalf("worker AddProjectPlugin() error = %v", err)
	}
	if !result.Associated || !result.Changed {
		t.Fatalf("worker add result = %#v", result)
	}
	if _, err := service.AddProjectPlugin(workerCtx, &contract.UpdateProjectPluginRequest{
		PublicID: project.PublicID, PluginCode: mcp.Code, Kind: "mcp",
	}); err == nil || !strings.Contains(err.Error(), "only manage project skills") {
		t.Fatalf("worker MCP mutation error = %v", err)
	}

	var activity types.ProjectActivity
	if err := database.Where("project_id = ?", project.PublicID).Order("id DESC").First(&activity).Error; err != nil {
		t.Fatalf("load project activity: %v", err)
	}
	if activity.OperatorID != assistant.PublicID {
		t.Fatalf("activity operator = %q, want %q", activity.OperatorID, assistant.PublicID)
	}
}
