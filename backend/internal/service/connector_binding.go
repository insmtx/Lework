package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

// connectorActivityRecorder 记录一次项目 MCP 连接器集合变化活动。
// 由 MessagePoster / sessionService 各自提供实现（解析活动操作者 OpID、写入 tx）。
type connectorActivityRecorder func(
	ctx context.Context,
	tx *gorm.DB,
	caller *types.Caller,
	projectPublicID string,
	action types.ProjectActivityAction,
	payload types.ProjectActivityPayload,
) error

// bindConnectorsToProject 将指定组织 MCP 连接器关联到项目，镜像 AddProjectPlugin 的校验口径。
//
// 关键校验（任一失败即返回错误并中止本次 Worker 任务发布）：
//   - caller 对项目拥有更新权限（RequireProject ... ActionProjectUpdate）；
//   - 插件必须属于 caller 当前组织、类型为 mcp、状态 active、且由 caller 本人创建（即当前用户可见）。
//
// 整个关联过程在事务内完成：多个绑定 + 活动记录要么全部落地、要么全部回滚，
// 避免中途失败留下半截绑定或在写入与活动记录间出现不一致。
//
// 对已存在有效绑定的连接器幂等成功，不重复入库、不重复记录活动。
// 返回实际新增的关联数量；仅在新增时写入 MCP 变更活动。
func bindConnectorsToProject(
	ctx context.Context,
	database *gorm.DB,
	perm *PermissionService,
	caller *types.Caller,
	project *types.Project,
	connectorPublicIDs []string,
	record connectorActivityRecorder,
) (int, error) {
	if len(connectorPublicIDs) == 0 || caller == nil || project == nil {
		return 0, nil
	}

	addCount := 0
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 事务内持有的项目必须与当前 caller 匹配，并具备更新权限。
		if err := PermissionForDB(tx, perm).RequireProject(
			ctx,
			FromTypeCaller(caller),
			project,
			types.ActionProjectUpdate,
		); err != nil {
			return fmt.Errorf("project update permission: %w", err)
		}

		// 去重，保持请求顺序。
		seen := make(map[string]bool, len(connectorPublicIDs))
		validated := make([]*types.Plugin, 0, len(connectorPublicIDs))
		for _, raw := range connectorPublicIDs {
			pluginID := strings.TrimSpace(raw)
			if pluginID == "" {
				continue
			}
			if seen[pluginID] {
				continue
			}
			seen[pluginID] = true

			plugin, err := infradb.GetPluginByPublicID(ctx, tx, caller.OrgID, pluginID)
			if err != nil {
				return fmt.Errorf("get connector by public id %s: %w", pluginID, err)
			}
			if plugin == nil {
				return fmt.Errorf("connector not found or not accessible: %s", pluginID)
			}
			if !strings.EqualFold(plugin.Kind, "mcp") {
				return fmt.Errorf("plugin %s is not an mcp connector (kind=%s)", pluginID, plugin.Kind)
			}
			if plugin.Status != types.PluginStatusActive {
				return fmt.Errorf("connector %s is not active (status=%s)", pluginID, plugin.Status)
			}
			if plugin.CreatedBy != caller.Uin {
				return fmt.Errorf("connector %s is not visible to the current user", pluginID)
			}
			validated = append(validated, plugin)
		}
		if len(validated) == 0 {
			return nil
		}

		// 批量读取项目现有有效 MCP 绑定，仅对缺失项执行插入，保证幂等。
		existing, err := infradb.ListProjectPlugins(ctx, tx, caller.OrgID, project.ID, "mcp")
		if err != nil {
			return fmt.Errorf("list project connectors: %w", err)
		}
		existingIDs := make(map[uint]bool, len(existing))
		for _, plugin := range existing {
			existingIDs[plugin.ID] = true
		}

		for _, plugin := range validated {
			if existingIDs[plugin.ID] {
				continue
			}
			if err := infradb.CreateProjectPluginBinding(ctx, tx, &types.ProjectPluginBinding{
				ProjectID: project.ID,
				PluginID:  plugin.ID,
				Enabled:   true,
				Config:    []byte(`{}`),
				CreatedBy: caller.Uin,
				UpdatedBy: caller.Uin,
			}); err != nil {
				return fmt.Errorf("create project connector binding: %w", err)
			}
			addCount++
		}

		// 仅为实际新增的关联记录 MCP 变更活动。
		if addCount == 0 {
			return nil
		}
		for _, plugin := range validated {
			if !existingIDs[plugin.ID] {
				action, payload := projectPluginActivity(plugin, true)
				if err := record(ctx, tx, caller, project.PublicID, action, payload); err != nil {
					return fmt.Errorf("record connector activity: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return addCount, nil
}

// recordUserRepoActivity 通过 userRepo 将执行者 Uin 解析为项目活动操作者 PublicID 后记录一条活动。
// MessagePoster / sessionService 均可复用，二者各自持有同一 account.UserRepository 实现。
func recordUserRepoActivity(
	ctx context.Context,
	tx *gorm.DB,
	userRepo account.UserRepository,
	opUin uint,
	projectPublicID string,
	action types.ProjectActivityAction,
	payload types.ProjectActivityPayload,
) error {
	operatorID := ""
	if userRepo != nil {
		if user, err := userRepo.GetUserByUin(ctx, opUin); err == nil && user != nil {
			operatorID = user.PublicID
		}
	}
	if operatorID == "" {
		return fmt.Errorf("resolve project activity operator: user %d not found", opUin)
	}
	payload = normalizeProjectActivityPayload(payload)
	return infradb.CreateProjectActivity(ctx, tx, &types.ProjectActivity{
		ProjectID:  projectPublicID,
		OperatorID: operatorID,
		ActionType: action,
		Payload:    payload,
		Version:    1,
		CreatedAt:  time.Now(),
	})
}
