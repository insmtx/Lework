package service

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// TestBindConnectorsToProject 覆盖连接器批量关联的权限校验、可见性校验与幂等行为。
// 项目资源与 owner 绑定由 seedProjectResourceOwner 补种，使 Uin=1 具备项目更新权限。
func TestBindConnectorsToProject(t *testing.T) {
	database := setupTestDB(t)
	if err := database.AutoMigrate(
		&types.Plugin{},
		&types.ProjectPluginBinding{},
		&types.ProjectActivity{},
		&types.Resource{},
		&types.ResourceBinding{},
	); err != nil {
		t.Fatalf("migrate models: %v", err)
	}

	project := &types.Project{
		PublicID: "prj_connector",
		Name:     "Connector Project",
		OrgID:    1,
		OwnerID:  1,
		Status:   "active",
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	seedProjectResourceOwner(t, database, project, 1)
	perm := newTestPermissionService(database)

	newPlugin := func(publicID, kind, status string, creator uint) *types.Plugin {
		plugin := &types.Plugin{
			PublicID:        publicID,
			OwnerScope:      types.OwnerScopeOrganization,
			OrgID:           1,
			Code:            publicID,
			Kind:            kind,
			Name:            publicID,
			Status:          status,
			Origin:          "org",
			CurrentRevision: 1,
			CreatedBy:       creator,
			UpdatedBy:       creator,
		}
		if err := database.Create(plugin).Error; err != nil {
			t.Fatalf("create plugin %s: %v", publicID, err)
		}
		return plugin
	}

	mcp := newPlugin("connector_a", "mcp", types.PluginStatusActive, 1)
	newPlugin("connector_skill", "skill", types.PluginStatusActive, 1)
	newPlugin("connector_inactive", "mcp", "archived", 1)
	otherUserMCP := newPlugin("connector_other", "mcp", types.PluginStatusActive, 2)

	caller := &types.Caller{Uin: 1, OrgID: 1, State: types.AuthStateSucc}

	countBound := func() int {
		var count int64
		database.Model(&types.ProjectPluginBinding{}).
			Where("project_id = ? AND deleted_at IS NULL", project.ID).
			Count(&count)
		return int(count)
	}

	// userRepo 将 uin 1/2 解析为 PublicID，供活动记录使用。
	userRepo := newTestUserRepo(map[string]uint{"usr_test": 1, "usr_other": 2})
	record := func(ctx context.Context, tx *gorm.DB, act *types.Caller, pid string, action types.ProjectActivityAction, payload types.ProjectActivityPayload) error {
		return recordUserRepoActivity(ctx, tx, userRepo, act.Uin, pid, action, payload)
	}

	t.Run("空列表直接返回且不写入", func(t *testing.T) {
		_, err := bindConnectorsToProject(
			context.Background(), database, perm, caller, project, []string{},
			func(ctx context.Context, tx *gorm.DB, act *types.Caller, pid string, action types.ProjectActivityAction, payload types.ProjectActivityPayload) error {
				return nil
			},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if countBound() != 0 {
			t.Fatalf("expected no binding, got count=%d", countBound())
		}
	})

	t.Run("合法 MCP 连接器关联成功并记录活动", func(t *testing.T) {
		_, err := bindConnectorsToProject(
			context.Background(), database, perm, caller, project, []string{mcp.PublicID}, record,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var activity types.ProjectActivity
		if err := database.Order("id DESC").First(&activity).Error; err != nil {
			t.Fatalf("load activity: %v", err)
		}
		if activity.ActionType != types.ProjectActivityActionMCPsChanged ||
			len(activity.Payload.AddedMCPIDs) != 1 {
			t.Fatalf("activity = %#v", activity)
		}
	})

	t.Run("重复关联幂等成功且不重复记录", func(t *testing.T) {
		before := countBound()
		_, err := bindConnectorsToProject(
			context.Background(), database, perm, caller, project, []string{mcp.PublicID, mcp.PublicID}, record,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if countBound() != before {
			t.Fatalf("expected idempotent count, before=%d after=%d", before, countBound())
		}
	})

	t.Run("非 MCP 插件被拒绝", func(t *testing.T) {
		_, err := bindConnectorsToProject(
			context.Background(), database, perm, caller, project, []string{"connector_skill"},
			func(ctx context.Context, tx *gorm.DB, act *types.Caller, pid string, action types.ProjectActivityAction, payload types.ProjectActivityPayload) error {
				return nil
			},
		)
		if err == nil {
			t.Fatal("expected error for non-mcp plugin")
		}
	})

	t.Run("inactive 连接器被拒绝", func(t *testing.T) {
		_, err := bindConnectorsToProject(
			context.Background(), database, perm, caller, project, []string{"connector_inactive"},
			func(ctx context.Context, tx *gorm.DB, act *types.Caller, pid string, action types.ProjectActivityAction, payload types.ProjectActivityPayload) error {
				return nil
			},
		)
		if err == nil {
			t.Fatal("expected error for inactive connector")
		}
	})

	t.Run("他人创建连接器对当前用户不可见", func(t *testing.T) {
		_, err := bindConnectorsToProject(
			context.Background(), database, perm, caller, project, []string{otherUserMCP.PublicID},
			func(ctx context.Context, tx *gorm.DB, act *types.Caller, pid string, action types.ProjectActivityAction, payload types.ProjectActivityPayload) error {
				return nil
			},
		)
		if err == nil {
			t.Fatal("expected error for connector created by another user")
		}
	})

	t.Run("无项目更新权限的用户被拒绝", func(t *testing.T) {
		// caller Uin=2 未绑定到该项目资源，RequireProject(ActionProjectUpdate) 应拒绝。
		noPermCaller := &types.Caller{Uin: 2, OrgID: 1, State: types.AuthStateSucc}
		_, err := bindConnectorsToProject(
			context.Background(), database, perm, noPermCaller, project, []string{mcp.PublicID},
			func(ctx context.Context, tx *gorm.DB, act *types.Caller, pid string, action types.ProjectActivityAction, payload types.ProjectActivityPayload) error {
				return nil
			},
		)
		if err == nil {
			t.Fatal("expected permission denied for user without project update right")
		}
	})
}
