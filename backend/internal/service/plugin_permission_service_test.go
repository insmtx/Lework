package service

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

// TestPluginPermissionSettingsResolveOwnerAndUpdateVisibility 回归：
// owner 绑定存在时 GetPluginPermissions 返回非空 user.public_id，
// 且仅改 visibility 的 UpdatePluginPermissions 不再报「member user_public_id is required」。
func TestPluginPermissionSettingsResolveOwnerAndUpdateVisibility(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	ctx := context.Background()
	plugin := &types.Plugin{
		PublicID:   "plugin_perm",
		OrgID:      1,
		Code:       "perm",
		Kind:       "skill",
		Name:       "Perm",
		Visibility: types.PluginVisibilityPrivate,
		Status:     types.PluginStatusActive,
		Origin:     "org",
		CreatedBy:  9,
		UpdatedBy:  9,
	}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	seedPluginResourceOwner(t, database, 1, plugin.ID, 9)

	service := &pluginService{db: database, userRepo: newTestUserRepo(map[string]uint{"usr_owner": 9})}

	settings, err := service.GetPluginPermissions(ctx, 1, 9, plugin.PublicID)
	if err != nil {
		t.Fatalf("GetPluginPermissions() error = %v", err)
	}
	if len(settings.Members) != 1 || settings.Members[0].User.PublicID != "usr_owner" {
		t.Fatalf("permission members = %#v, want owner with public_id usr_owner", settings.Members)
	}

	owner := contract.PluginPermissionMemberInput{}
	owner.User.PublicID = "usr_owner"
	owner.Role = types.ResourceRoleOwner
	updated, err := service.UpdatePluginPermissions(ctx, 1, 9, plugin.PublicID, &contract.UpdatePluginPermissionsRequest{
		Visibility: types.PluginVisibilityPublic,
		Members:    []contract.PluginPermissionMemberInput{owner},
	})
	if err != nil {
		t.Fatalf("UpdatePluginPermissions() error = %v", err)
	}
	if updated.Visibility != types.PluginVisibilityPublic {
		t.Fatalf("updated visibility = %v, want public", updated.Visibility)
	}
}
