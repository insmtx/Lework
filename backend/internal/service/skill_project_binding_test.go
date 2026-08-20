package service

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func TestBindInvokedSkillsToProject(t *testing.T) {
	database := setupTestDB(t)
	project := &types.Project{
		PublicID: "prj_invoked_skill",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Invoked Skill Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	orgSkill := &types.Plugin{
		PublicID:        "plugin_org_skill",
		OwnerScope:      types.OwnerScopeOrganization,
		OrgID:           1,
		Code:            "org-skill",
		Kind:            "skill",
		Name:            "Organization Skill",
		Status:          types.PluginStatusActive,
		Origin:          "org",
		CurrentRevision: 1,
		CreatedBy:       1,
		UpdatedBy:       1,
	}
	if err := database.Create(orgSkill).Error; err != nil {
		t.Fatalf("create organization Skill: %v", err)
	}
	systemSkill := &types.Plugin{
		PublicID:        "plugin_system_skill",
		OwnerScope:      types.OwnerScopeSystem,
		OrgID:           0,
		Code:            "system-skill",
		Kind:            "skill",
		Name:            "System Skill",
		Status:          types.PluginStatusActive,
		Origin:          builtinWorkerOrigin,
		CurrentRevision: 1,
	}
	if err := database.Create(systemSkill).Error; err != nil {
		t.Fatalf("create system Skill: %v", err)
	}
	inactiveSkill := &types.Plugin{
		PublicID:        "plugin_inactive_skill",
		OwnerScope:      types.OwnerScopeOrganization,
		OrgID:           1,
		Code:            "inactive-skill",
		Kind:            "skill",
		Name:            "Inactive Skill",
		Status:          types.PluginStatusArchived,
		Origin:          "org",
		CurrentRevision: 1,
		CreatedBy:       1,
		UpdatedBy:       1,
	}
	if err := database.Create(inactiveSkill).Error; err != nil {
		t.Fatalf("create inactive Skill: %v", err)
	}

	caller := &types.Caller{Uin: 1, OrgID: 1, State: types.AuthStateSucc}
	activityCount := 0
	record := func(_ context.Context, _ *gorm.DB, _ *types.Caller, _ string, _ types.ProjectActivityAction, _ types.ProjectActivityPayload) error {
		activityCount++
		return nil
	}
	result := bindInvokedSkillsToProject(
		context.Background(),
		database,
		project,
		caller,
		invokedSkillContent("org-skill", "system-skill", "missing-skill", "org-skill", "inactive-skill"),
		record,
	)

	if len(result.Added) != 1 || result.Added[0] != "org-skill" {
		t.Fatalf("added = %#v", result.Added)
	}
	if len(result.System) != 1 || result.System[0] != "system-skill" {
		t.Fatalf("system = %#v", result.System)
	}
	if len(result.Failures) != 2 {
		t.Fatalf("failures = %#v", result.Failures)
	}

	var bindingCount int64
	if err := database.Model(&types.ProjectPluginBinding{}).
		Where("project_id = ? AND deleted_at IS NULL", project.ID).
		Count(&bindingCount).Error; err != nil {
		t.Fatalf("count project bindings: %v", err)
	}
	if bindingCount != 1 {
		t.Fatalf("binding count = %d, want 1", bindingCount)
	}
	if activityCount != 1 {
		t.Fatalf("activity count = %d, want 1", activityCount)
	}

	repeated := bindInvokedSkillsToProject(
		context.Background(), database, project, caller, invokedSkillContent("org-skill"), record,
	)
	if len(repeated.AlreadyBound) != 1 || repeated.AlreadyBound[0] != "org-skill" {
		t.Fatalf("repeated = %#v", repeated)
	}
	if activityCount != 1 {
		t.Fatalf("repeated activity count = %d, want 1", activityCount)
	}

	nonLeading := bindInvokedSkillsToProject(
		context.Background(), database, project, caller, "请使用 /org-skill 但不点选芯片", nil,
	)
	if len(nonLeading.Added) != 0 || len(nonLeading.Failures) != 0 {
		t.Fatalf("slash-only content result = %#v", nonLeading)
	}
}

func invokedSkillContent(ids ...string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, `<skill-chip data-code="`+id+`">`+id+`</skill-chip>`)
	}
	return strings.Join(parts, " ")
}
