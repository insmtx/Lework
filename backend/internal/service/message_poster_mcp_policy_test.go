package service

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/types"
)

func TestMessagePosterShouldDisableProjectMCPForMultipleHumanMembers(t *testing.T) {
	database := setupTestDB(t)
	poster := &MessagePoster{db: database}
	project := &types.Project{PublicID: "prj_mcp_policy", OrgID: 1, OwnerID: 1, Name: "MCP Policy"}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	if disabled := poster.shouldDisableProjectMCP(context.Background(), 1, project.ID); !disabled {
		t.Fatal("missing project resource should disable MCP")
	}
	resource := &types.Resource{OrgID: 1, Uin: 1, Type: types.ResourceTypeProject, BizID: project.ID}
	if err := database.Create(resource).Error; err != nil {
		t.Fatalf("create resource: %v", err)
	}
	firstUin := uint(1)
	createMCPPolicyBinding(t, database, resource.ID, &firstUin, nil)

	disabled := poster.shouldDisableProjectMCP(context.Background(), 1, project.ID)
	if disabled {
		t.Fatal("one human member should not disable MCP")
	}

	assistantID := uint(10)
	createMCPPolicyBinding(t, database, resource.ID, nil, &assistantID)
	disabled = poster.shouldDisableProjectMCP(context.Background(), 1, project.ID)
	if disabled {
		t.Fatal("one human member plus an assistant should not disable MCP")
	}

	secondUin := uint(2)
	secondBinding := createMCPPolicyBinding(t, database, resource.ID, &secondUin, nil)
	disabled = poster.shouldDisableProjectMCP(context.Background(), 1, project.ID)
	if !disabled {
		t.Fatal("two human members should disable MCP")
	}
	if err := database.Delete(secondBinding).Error; err != nil {
		t.Fatalf("remove second human binding: %v", err)
	}
	if disabled = poster.shouldDisableProjectMCP(context.Background(), 1, project.ID); disabled {
		t.Fatal("removing the second human member should restore MCP")
	}
}

func TestResolveProjectPluginSnapshotsDropsConnectorAndBundledSkillWhenMCPDisabled(t *testing.T) {
	database := setupTestDB(t)
	if err := database.AutoMigrate(
		&types.Plugin{},
		&types.PluginRevision{},
		&types.ProjectPluginBinding{},
	); err != nil {
		t.Fatalf("migrate plugin tables: %v", err)
	}
	poster := &MessagePoster{db: database}
	project := &types.Project{PublicID: "prj_mcp_snapshots", OrgID: 1, OwnerID: 1, Name: "MCP Snapshots"}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}

	createMCPPolicyPlugin(t, database, project.ID, "plugin_skill", "standalone-skill", "skill",
		`{"schema":"skill/v1","artifact":{"file_upload_id":"file_skill","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`)
	createMCPPolicyPlugin(t, database, project.ID, "plugin_connector", "mail-connector", "mcp",
		`{"schema":"connector/v1","channel":"netease-mail","mode":"skill_only","auth":{"type":"none"},"skill":{"code":"connector-netease-mail","revision":1,"artifact":{"file_upload_id":"file_mail","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}}`)

	allowedSnapshots, err := poster.resolveProjectPluginSnapshots(context.Background(), 1, project.ID, false)
	if err != nil {
		t.Fatalf("resolve allowed snapshots: %v", err)
	}
	if len(allowedSnapshots) != 2 {
		t.Fatalf("allowed snapshots = %#v, want standalone Skill and connector", allowedSnapshots)
	}

	snapshots, err := poster.resolveProjectPluginSnapshots(context.Background(), 1, project.ID, true)
	if err != nil {
		t.Fatalf("resolve snapshots: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Kind != "skill" || snapshots[0].Code != "standalone-skill" {
		t.Fatalf("snapshots = %#v, want only standalone Skill", snapshots)
	}
}

func createMCPPolicyBinding(
	t *testing.T,
	database *gorm.DB,
	resourceID uint,
	uin, assistantID *uint,
) *types.ResourceBinding {
	t.Helper()
	binding := &types.ResourceBinding{
		OrgID: 1, ResourceID: resourceID, Uin: uin, AssistantID: assistantID, Role: types.ResourceRoleMember,
	}
	if err := database.Create(binding).Error; err != nil {
		t.Fatalf("create resource binding: %v", err)
	}
	return binding
}

func createMCPPolicyPlugin(
	t *testing.T,
	database *gorm.DB,
	projectID uint,
	publicID, code, kind, definition string,
) *types.Plugin {
	t.Helper()
	plugin := &types.Plugin{
		PublicID: publicID, OwnerScope: types.OwnerScopeOrganization, OrgID: 1, Code: code, Kind: kind,
		Name: code, Status: types.PluginStatusActive, Origin: "org", CurrentRevision: 1, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	if err := database.Create(&types.PluginRevision{
		PluginID: plugin.ID, Revision: 1, Status: "published", Definition: []byte(definition),
		PublishedByType: "user", PublishedByID: 1, PublishedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create plugin revision: %v", err)
	}
	if err := database.Create(&types.ProjectPluginBinding{
		ProjectID: projectID, PluginID: plugin.ID, Enabled: true, Config: []byte(`{}`), CreatedBy: 1, UpdatedBy: 1,
	}).Error; err != nil {
		t.Fatalf("create project plugin binding: %v", err)
	}
	return plugin
}
