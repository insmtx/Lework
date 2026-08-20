package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

// GetPluginPermissions 返回插件的公开性、owner 与 admin/viewer 成员展示信息。
// 仅 Skill owner/admin 可访问；MCP 返回 400。
func (s *pluginService) GetPluginPermissions(
	ctx context.Context,
	orgID, uin uint,
	pluginID string,
) (*contract.PluginPermissionSettingsView, error) {
	plugin, err := infradb.GetPluginByPublicID(ctx, s.db, orgID, pluginID)
	if err != nil {
		return nil, err
	}
	if plugin == nil {
		return nil, contract.ErrPluginNotFound
	}
	if plugin.Kind != "skill" {
		return nil, contract.ErrPluginPermissionUnsupported
	}
	if err := s.pluginAccess().RequirePermissionRead(ctx, orgID, uin, plugin); err != nil {
		return nil, err
	}
	return s.loadPluginPermissionSettings(ctx, s.db, plugin)
}

// UpdatePluginPermissions 全量替换插件权限配置（事务内锁定资源与绑定）。
func (s *pluginService) UpdatePluginPermissions(
	ctx context.Context,
	orgID, uin uint,
	pluginID string,
	req *contract.UpdatePluginPermissionsRequest,
) (*contract.PluginPermissionSettingsView, error) {
	if req == nil {
		return nil, invalidPluginPermission("request is required")
	}
	if !types.ValidPluginVisibility(req.Visibility) {
		return nil, invalidPluginPermission("visibility must be public or private")
	}
	ownerPublicID, err := validatePermissionMembers(req.Members)
	if err != nil {
		return nil, err
	}

	plugin, err := infradb.GetPluginByPublicID(ctx, s.db, orgID, pluginID)
	if err != nil {
		return nil, err
	}
	if plugin == nil {
		return nil, contract.ErrPluginNotFound
	}
	if plugin.Kind != "skill" {
		return nil, contract.ErrPluginPermissionUnsupported
	}

	var result *contract.PluginPermissionSettingsView
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		access := newPluginAccessManager(tx)
		callerRole, accessErr := access.resolveAccessRole(ctx, orgID, uin, plugin)
		if accessErr != nil {
			return accessErr
		}
		if callerRole != types.ResourceRoleOwner && callerRole != types.ResourceRoleAdmin {
			return contract.ErrPluginForbidden
		}

		resource, resourceErr := lockPluginResource(ctx, tx, orgID, plugin.ID)
		if resourceErr != nil {
			return resourceErr
		}

		currentBindings, listErr := infradb.ListResourceBindingsByResourceID(ctx, tx, resource.ID)
		if listErr != nil {
			return listErr
		}

		ownerUin, ownerErr := currentPluginOwner(currentBindings)
		if ownerErr != nil {
			return ownerErr
		}
		ownerUser, ownerUserErr := s.resolvePermissionUser(ctx, ownerUin)
		if ownerUserErr != nil {
			return ownerUserErr
		}
		if ownerUser == nil || ownerUser.PublicID != ownerPublicID {
			return invalidPluginPermission("owner must be the current plugin owner")
		}

		// admin 不能修改 visibility。
		if callerRole == types.ResourceRoleAdmin && req.Visibility != plugin.Visibility {
			return contract.ErrPluginForbidden
		}

		targetUins, resolveErr := s.resolvePermissionMemberUins(ctx, orgID, req.Members)
		if resolveErr != nil {
			return resolveErr
		}
		if targetUins[ownerPublicID] != ownerUin {
			return invalidPluginPermission("owner must be the current plugin owner")
		}

		desired := make(map[uint]types.ResourceRole, len(targetUins))
		for _, member := range req.Members {
			desired[targetUins[member.User.PublicID]] = member.Role
		}

		current := make(map[uint]*types.ResourceBinding, len(currentBindings))
		for _, binding := range currentBindings {
			if binding.Uin != nil && *binding.Uin != 0 {
				current[*binding.Uin] = binding
			}
		}

		if err := applyPluginPermissionDiff(ctx, tx, orgID, resource.ID, current, desired); err != nil {
			return err
		}

		if req.Visibility != plugin.Visibility {
			if err := tx.Model(&types.Plugin{}).Where("id = ?", plugin.ID).
				Update("visibility", req.Visibility).Error; err != nil {
				return err
			}
			plugin.Visibility = req.Visibility
		}

		view, buildErr := s.loadPluginPermissionSettings(ctx, tx, plugin)
		if buildErr != nil {
			return buildErr
		}
		result = view
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// validatePermissionMembers 校验成员列表并返回请求中的 owner public_id。
func validatePermissionMembers(members []contract.PluginPermissionMemberInput) (string, error) {
	ownerPublicID := ""
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		publicID := strings.TrimSpace(member.User.PublicID)
		if publicID == "" {
			return "", invalidPluginPermission("member user_public_id is required")
		}
		if _, exists := seen[publicID]; exists {
			return "", invalidPluginPermission("duplicate member user_public_id")
		}
		seen[publicID] = struct{}{}
		switch member.Role {
		case types.ResourceRoleOwner:
			if ownerPublicID != "" {
				return "", invalidPluginPermission("only the current owner may hold the owner role")
			}
			ownerPublicID = publicID
		case types.ResourceRoleAdmin, types.ResourceRoleViewer:
		default:
			return "", invalidPluginPermission("invalid member role")
		}
	}
	if ownerPublicID == "" {
		return "", invalidPluginPermission("owner must be present in members")
	}
	return ownerPublicID, nil
}

// resolvePermissionMemberUins 将成员 public_id 解析为同组织有效成员的 uin。
func (s *pluginService) resolvePermissionMemberUins(
	ctx context.Context,
	orgID uint,
	members []contract.PluginPermissionMemberInput,
) (map[string]uint, error) {
	if s.userRepo == nil {
		return nil, fmt.Errorf("plugin service user repository is not configured")
	}
	publicIDs := make([]string, 0, len(members))
	for _, member := range members {
		publicIDs = append(publicIDs, member.User.PublicID)
	}
	uinByPublicID, err := s.userRepo.GetUinMapByPublicIDs(ctx, orgID, publicIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]uint, len(publicIDs))
	for _, publicID := range publicIDs {
		uin, ok := uinByPublicID[publicID]
		if !ok || uin == 0 {
			return nil, invalidPluginPermission("member is not an active organization member")
		}
		result[publicID] = uin
	}
	return result, nil
}

// currentPluginOwner 返回插件资源上的 owner uin。
func currentPluginOwner(bindings []*types.ResourceBinding) (uint, error) {
	var ownerUin uint
	for _, binding := range bindings {
		if binding.Role == types.ResourceRoleOwner && binding.Uin != nil && *binding.Uin != 0 {
			if ownerUin != 0 && ownerUin != *binding.Uin {
				return 0, invalidPluginPermission("plugin has multiple owners")
			}
			ownerUin = *binding.Uin
		}
	}
	if ownerUin == 0 {
		return 0, invalidPluginPermission("plugin owner is missing")
	}
	return ownerUin, nil
}

// applyPluginPermissionDiff 将绑定同步到 desired 状态：软删除多余、更新角色、创建新增。
func applyPluginPermissionDiff(
	ctx context.Context,
	tx *gorm.DB,
	orgID, resourceID uint,
	current map[uint]*types.ResourceBinding,
	desired map[uint]types.ResourceRole,
) error {
	for uin, binding := range current {
		if _, keep := desired[uin]; keep {
			continue
		}
		if err := infradb.DeleteResourceBinding(ctx, tx, binding.ID); err != nil {
			return err
		}
	}
	for uin, role := range desired {
		if existing, ok := current[uin]; ok {
			if existing.Role == role {
				continue
			}
			if err := infradb.UpdateResourceBindingRole(ctx, tx, existing.ID, role); err != nil {
				return err
			}
			continue
		}
		uinCopy := uin
		binding := &types.ResourceBinding{
			OrgID:      orgID,
			Uin:        &uinCopy,
			ResourceID: resourceID,
			Role:       role,
		}
		if err := infradb.CreateResourceBinding(ctx, tx, binding); err != nil {
			return err
		}
	}
	return nil
}

// lockPluginResource 锁定插件资源行，防止并发权限更新。
func lockPluginResource(ctx context.Context, tx *gorm.DB, orgID, pluginID uint) (*types.Resource, error) {
	var resource types.Resource
	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("org_id = ? AND type = ? AND biz_id = ? AND deleted_at IS NULL", orgID, types.ResourceTypePlugin, pluginID).
		First(&resource).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, contract.ErrPluginNotFound
		}
		return nil, err
	}
	return &resource, nil
}

// loadPluginPermissionSettings 构建插件权限设置展示视图。
func (s *pluginService) loadPluginPermissionSettings(
	ctx context.Context,
	db *gorm.DB,
	plugin *types.Plugin,
) (*contract.PluginPermissionSettingsView, error) {
	resource, err := infradb.GetResourceByBizID(ctx, db, plugin.OrgID, types.ResourceTypePlugin, plugin.ID)
	if err != nil {
		return nil, err
	}
	if resource == nil {
		return nil, contract.ErrPluginNotFound
	}
	bindings, err := infradb.ListResourceBindingsByResourceID(ctx, db, resource.ID)
	if err != nil {
		return nil, err
	}

	uins := make([]uint, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Uin != nil && *binding.Uin != 0 {
			uins = append(uins, *binding.Uin)
		}
	}
	users, err := s.resolvePermissionUsers(ctx, uins)
	if err != nil {
		return nil, err
	}

	members := make([]contract.PluginPermissionMemberView, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Uin == nil || *binding.Uin == 0 {
			continue
		}
		user := users[*binding.Uin]
		view := contract.PluginPermissionMemberView{
			Role: binding.Role,
		}
		if user != nil {
			view.User = contract.PluginPermissionUserView{
				PublicID:    user.PublicID,
				Name:        user.Name,
				Email:       user.Email,
				AvatarURL:   user.AvatarURL,
				Departments: permissionDepartments(user.Departments),
			}
		}
		members = append(members, view)
	}
	sort.SliceStable(members, func(i, j int) bool {
		return types.ResourceRoleStrength[members[i].Role] > types.ResourceRoleStrength[members[j].Role]
	})

	return &contract.PluginPermissionSettingsView{
		Visibility: plugin.Visibility,
		Members:    members,
	}, nil
}

func invalidPluginPermission(message string) error {
	return fmt.Errorf("%w: %s", contract.ErrInvalidPluginPermission, message)
}

// resolvePermissionUsers 通过账号适配层批量解析成员展示信息（企业版走 IAM、OSS 走本地库）。
func (s *pluginService) resolvePermissionUsers(ctx context.Context, uins []uint) (map[uint]*account.UserInfo, error) {
	if s.userRepo == nil {
		return nil, fmt.Errorf("plugin service user repository is not configured")
	}
	return s.userRepo.GetUsersByUins(ctx, uins)
}

// resolvePermissionUser 通过账号适配层解析单个成员展示信息。
func (s *pluginService) resolvePermissionUser(ctx context.Context, uin uint) (*account.UserInfo, error) {
	if s.userRepo == nil {
		return nil, fmt.Errorf("plugin service user repository is not configured")
	}
	return s.userRepo.GetUserByUin(ctx, uin)
}

// permissionDepartments 将账号适配层的部门信息转换为权限接口的部门展示信息。
func permissionDepartments(departments []account.OrgMemberDepartment) []contract.PluginPermissionDepartment {
	if len(departments) == 0 {
		return nil
	}
	result := make([]contract.PluginPermissionDepartment, 0, len(departments))
	for _, department := range departments {
		result = append(result, contract.PluginPermissionDepartment{
			DepartmentID: department.DepartmentID,
			Name:         department.Name,
		})
	}
	return result
}
