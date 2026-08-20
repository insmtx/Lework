package service

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

// pluginAccessManager 集中实现插件固定角色访问策略。
//
// 判断顺序：
//  1. 校验插件存在、组织归属与 active 状态。
//  2. MCP 直接应用 owner-only 规则。
//  3. Skill 为 public 时组织成员隐式获得 view/use。
//  4. Skill 为 private 时读取插件资源的直接角色绑定。
//  5. 按固定角色策略校验具体动作。
type pluginAccessManager struct {
	db *gorm.DB
}

func newPluginAccessManager(db *gorm.DB) *pluginAccessManager {
	return &pluginAccessManager{db: db}
}

// pluginAccess 返回绑定到插件服务主库连接的访问管理器。
func (s *pluginService) pluginAccess() *pluginAccessManager {
	return newPluginAccessManager(s.db)
}

// RequireView 校验调用者可查看插件详情与版本。
func (m *pluginAccessManager) RequireView(ctx context.Context, orgID, uin uint, plugin *types.Plugin) error {
	return m.requireAction(ctx, orgID, uin, plugin, types.ActionPluginView)
}

// RequireUse 校验调用者可在任务执行中使用插件。
func (m *pluginAccessManager) RequireUse(ctx context.Context, orgID, uin uint, plugin *types.Plugin) error {
	return m.requireAction(ctx, orgID, uin, plugin, types.ActionPluginUse)
}

// RequireUpdate 校验调用者可编辑插件内容。
func (m *pluginAccessManager) RequireUpdate(ctx context.Context, orgID, uin uint, plugin *types.Plugin) error {
	return m.requireAction(ctx, orgID, uin, plugin, types.ActionPluginUpdate)
}

// RequireUpdatePermission 校验调用者拥有 owner/admin 直接角色，用于编辑/重新发布内容。
// 与 RequireUpdate 不同，它不校验 active 状态（允许 owner/admin 重新激活已归档插件）。
func (m *pluginAccessManager) RequireUpdatePermission(ctx context.Context, orgID, uin uint, plugin *types.Plugin) error {
	role, err := m.directRole(ctx, orgID, uin, plugin)
	if err != nil {
		return err
	}
	if role == types.ResourceRoleOwner || role == types.ResourceRoleAdmin {
		return nil
	}
	return contract.ErrPluginForbidden
}

// RequireDelete 校验调用者可删除插件。
func (m *pluginAccessManager) RequireDelete(ctx context.Context, orgID, uin uint, plugin *types.Plugin) error {
	return m.requireAction(ctx, orgID, uin, plugin, types.ActionPluginDelete)
}

// RequirePermissionRead 校验调用者可读取插件权限配置。
func (m *pluginAccessManager) RequirePermissionRead(ctx context.Context, orgID, uin uint, plugin *types.Plugin) error {
	return m.requireAction(ctx, orgID, uin, plugin, types.ActionPluginPermissionRead)
}

// RequirePermissionUpdate 校验调用者可更新插件成员权限配置。
func (m *pluginAccessManager) RequirePermissionUpdate(ctx context.Context, orgID, uin uint, plugin *types.Plugin) error {
	return m.requireAction(ctx, orgID, uin, plugin, types.ActionPluginPermissionUpdate)
}

// RequireVisibilityUpdate 校验调用者可修改插件公开性。
func (m *pluginAccessManager) RequireVisibilityUpdate(ctx context.Context, orgID, uin uint, plugin *types.Plugin) error {
	return m.requireAction(ctx, orgID, uin, plugin, types.ActionPluginVisibilityUpdate)
}

// ResolveRole 返回调用者在插件上的直接角色；无直接角色但公开隐式访问时返回空角色。
// 不可见时返回 contract.ErrPluginNotFound。
func (m *pluginAccessManager) ResolveRole(ctx context.Context, orgID, uin uint, plugin *types.Plugin) (types.ResourceRole, error) {
	return m.resolveAccessRole(ctx, orgID, uin, plugin)
}

// requireAction 解析访问并执行固定角色策略。
func (m *pluginAccessManager) requireAction(ctx context.Context, orgID, uin uint, plugin *types.Plugin, action types.Action) error {
	role, err := m.resolveAccessRole(ctx, orgID, uin, plugin)
	if err != nil {
		return err
	}
	if role == "" {
		// 公开隐式访问仅授予 view/use。
		if action == types.ActionPluginView || action == types.ActionPluginUse {
			return nil
		}
		return contract.ErrPluginForbidden
	}
	if SystemPolicy.Allows(types.ResourceTypePlugin, role, action) {
		return nil
	}
	return contract.ErrPluginForbidden
}

// resolveAccessRole 返回直接角色；公开 Skill 的隐式访问返回空角色。
func (m *pluginAccessManager) resolveAccessRole(ctx context.Context, orgID, uin uint, plugin *types.Plugin) (types.ResourceRole, error) {
	if plugin == nil || plugin.OwnerScope != types.OwnerScopeOrganization || plugin.OrgID != orgID {
		return "", contract.ErrPluginNotFound
	}
	if plugin.Status != types.PluginStatusActive {
		return "", contract.ErrPluginNotFound
	}
	role, err := m.directRole(ctx, orgID, uin, plugin)
	if err != nil {
		return "", err
	}
	if role != "" {
		return role, nil
	}
	// MCP 固定私有，仅 owner 可访问。
	if plugin.Kind == "mcp" {
		return "", contract.ErrPluginNotFound
	}
	// 公开 Skill 对组织内已认证调用者隐式提供 view/use。
	// 调用者 orgID 已由鉴权中间件绑定，插件也按 orgID 隔离，无需再校验成员表。
	if plugin.Visibility == types.PluginVisibilityPublic && uin > 0 {
		return "", nil
	}
	return "", contract.ErrPluginNotFound
}

// directRole 读取插件资源的直接角色绑定。
func (m *pluginAccessManager) directRole(ctx context.Context, orgID, uin uint, plugin *types.Plugin) (types.ResourceRole, error) {
	if uin == 0 {
		return "", nil
	}
	resource, err := infradb.GetResourceByBizID(ctx, m.db, orgID, types.ResourceTypePlugin, plugin.ID)
	if err != nil {
		return "", err
	}
	if resource == nil {
		return "", nil
	}
	binding, err := infradb.GetResourceBindingByUin(ctx, m.db, resource.ID, uin)
	if err != nil {
		return "", err
	}
	if binding == nil {
		return "", nil
	}
	return binding.Role, nil
}

// softDeletePluginPermissionResource 软删除插件权限资源及其全部绑定。
func softDeletePluginPermissionResource(ctx context.Context, tx *gorm.DB, orgID, pluginID uint) error {
	resource, err := infradb.GetResourceByBizID(ctx, tx, orgID, types.ResourceTypePlugin, pluginID)
	if err != nil {
		return err
	}
	if resource == nil {
		return nil
	}
	bindings, err := infradb.ListResourceBindingsByResourceID(ctx, tx, resource.ID)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if err := infradb.DeleteResourceBinding(ctx, tx, binding.ID); err != nil {
			return err
		}
	}
	return infradb.DeleteResource(ctx, tx, resource.ID)
}

// downloadableSkillCodes 返回可下载的 Skill code 集合，按项目运行授权或个人权限校验。
func (s *pluginService) downloadableSkillCodes(
	ctx context.Context,
	orgID, actorUin uint,
	project *types.Project,
	codes []string,
) (map[string]bool, error) {
	allowed := make(map[string]bool, len(codes))
	for _, code := range codes {
		plugin, err := infradb.GetOrganizationPluginByIdentity(ctx, s.db, orgID, "skill", code)
		if err != nil {
			return nil, err
		}
		if plugin == nil {
			continue
		}
		ok, err := s.authorizeSkillDownload(ctx, orgID, actorUin, project, plugin)
		if err != nil {
			return nil, err
		}
		if ok {
			allowed[code] = true
		}
	}
	return allowed, nil
}

// authorizeSkillDownload 按项目运行授权或个人权限判断 Skill 是否可下载。
func (s *pluginService) authorizeSkillDownload(
	ctx context.Context,
	orgID, actorUin uint,
	project *types.Project,
	plugin *types.Plugin,
) (bool, error) {
	// 有项目绑定时按项目运行授权允许下载，不再要求个人直接角色。
	if project != nil {
		bound, err := infradb.IsPluginBoundToProject(ctx, s.db, project.ID, plugin.ID)
		if err != nil {
			return false, err
		}
		if bound {
			return true, nil
		}
	}
	// 公开 Skill 对组织内调用者开放。
	if plugin.Visibility == types.PluginVisibilityPublic {
		return true, nil
	}
	// 私有 Skill 需要 actor 的直接角色。
	if actorUin == 0 {
		return false, nil
	}
	role, err := s.pluginAccess().ResolveRole(ctx, orgID, actorUin, plugin)
	if err != nil {
		if errors.Is(err, contract.ErrPluginNotFound) {
			return false, nil
		}
		return false, err
	}
	return role != "", nil
}

// ensurePluginResourceOwner 幂等创建插件的活动权限资源与 owner 绑定。
// 必须在插件创建事务内调用，避免无权限窗口。
func ensurePluginResourceOwner(ctx context.Context, tx *gorm.DB, orgID, pluginID, ownerUin uint) error {
	resource, err := infradb.GetResourceByBizID(ctx, tx, orgID, types.ResourceTypePlugin, pluginID)
	if err != nil {
		return err
	}
	if resource == nil {
		resource = &types.Resource{
			OrgID:                 orgID,
			Uin:                   ownerUin,
			Type:                  types.ResourceTypePlugin,
			BizID:                 pluginID,
			ParentResourcePathIDs: types.ResourcePathIDs{},
		}
		if err := infradb.CreateResource(ctx, tx, resource); err != nil {
			return err
		}
	}
	if ownerUin == 0 {
		return nil
	}
	existing, err := infradb.GetResourceBindingByUin(ctx, tx, resource.ID, ownerUin)
	if err != nil {
		return err
	}
	if existing == nil {
		uin := ownerUin
		binding := &types.ResourceBinding{
			OrgID:      orgID,
			Uin:        &uin,
			ResourceID: resource.ID,
			Role:       types.ResourceRoleOwner,
		}
		if err := infradb.CreateResourceBinding(ctx, tx, binding); err != nil {
			return err
		}
	}
	return nil
}
