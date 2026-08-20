package service

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

func TestProjectMCPBindingUsesOrganizationPluginAndMCPActivity(t *testing.T) {
	database := setupTestDB(t)
	if err := database.AutoMigrate(
		&types.Plugin{},
		&types.PluginRevision{},
		&types.ProjectPluginBinding{},
	); err != nil {
		t.Fatalf("migrate plugin models: %v", err)
	}
	project := &types.Project{
		PublicID: "project_mcp",
		Name:     "MCP Project",
		OrgID:    1,
		OwnerID:  1,
		Status:   "active",
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	seedProjectResourceOwner(t, database, project, 1)
	seedProjectResourceBinding(t, database, 1, project.ID, 2, types.ResourceRoleAdmin)
	plugin := &types.Plugin{
		PublicID:        "plugin_mcp",
		OwnerScope:      types.OwnerScopeOrganization,
		OrgID:           1,
		Code:            "docs",
		Kind:            "mcp",
		Name:            "Docs",
		Status:          types.PluginStatusActive,
		Origin:          "org",
		CurrentRevision: 1,
		CreatedBy:       1,
		UpdatedBy:       1,
	}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	seedPluginResourceOwner(t, database, 1, plugin.ID, 1)
	if err := database.Create(&types.PluginRevision{
		PluginID:        plugin.ID,
		Revision:        1,
		Status:          "published",
		Definition:      []byte(`{"schema":"mcp/v1","transport":"http","name":"docs","url":"https://example.com/mcp"}`),
		PublishedByType: "user",
		PublishedByID:   1,
	}).Error; err != nil {
		t.Fatalf("create plugin revision: %v", err)
	}

	service := NewProjectService(
		database,
		newTestPermissionService(database),
		nil,
		nil,
		"test",
		newTestUserRepo(map[string]uint{"usr_test": 1, "usr_other": 2}),
		NewSkillDisplayTranslationService(database),
	)
	ctx := setupTestContextWithCaller(t)
	request := &contract.UpdateProjectPluginRequest{
		PublicID: project.PublicID,
		PluginID: plugin.PublicID,
	}
	otherUserCtx := setupTestContextWithCallerUin(t, 2)
	if _, err := service.AddProjectPlugin(otherUserCtx, request); err == nil {
		t.Fatal("other user must not add an MCP they did not create")
	}
	if _, err := service.AddProjectPlugin(ctx, request); err != nil {
		t.Fatalf("AddProjectPlugin() error = %v", err)
	}
	plugins, err := service.ListProjectPlugins(otherUserCtx, &contract.ListProjectPluginsRequest{
		PublicID: project.PublicID,
		Kind:     "mcp",
	})
	if err != nil {
		t.Fatalf("ListProjectPlugins() error = %v", err)
	}
	if len(plugins) != 1 || plugins[0].PublicID != plugin.PublicID {
		t.Fatalf("project MCP plugins = %#v", plugins)
	}
	var addedActivity types.ProjectActivity
	if err := database.WithContext(context.Background()).
		Where("project_id = ?", project.PublicID).
		Order("id DESC").
		First(&addedActivity).Error; err != nil {
		t.Fatalf("load add activity: %v", err)
	}
	if addedActivity.ActionType != types.ProjectActivityActionMCPsChanged ||
		len(addedActivity.Payload.AddedMCPIDs) != 1 {
		t.Fatalf("add activity = %#v", addedActivity)
	}

	if _, err := service.RemoveProjectPlugin(otherUserCtx, request); err != nil {
		t.Fatalf("other user RemoveProjectPlugin() error = %v", err)
	}
	var removedActivity types.ProjectActivity
	if err := database.WithContext(context.Background()).
		Where("project_id = ?", project.PublicID).
		Order("id DESC").
		First(&removedActivity).Error; err != nil {
		t.Fatalf("load remove activity: %v", err)
	}
	if removedActivity.ActionType != types.ProjectActivityActionMCPsChanged ||
		len(removedActivity.Payload.RemovedMCPIDs) != 1 {
		t.Fatalf("remove activity = %#v", removedActivity)
	}
}
