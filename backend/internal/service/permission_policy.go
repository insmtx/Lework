package service

import (
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/types"
)

// Action 表示一个权限动作，格式为 "resource_type:action_name"。
type Action string

const (
	// 项目相关动作
	ActionProjectView         Action = "project:view"
	ActionProjectUpdate       Action = "project:update"
	ActionProjectDelete       Action = "project:delete"
	ActionProjectArchive      Action = "project:archive"
	ActionProjectMemberCreate Action = "project:member.create"
	ActionProjectMemberUpdate Action = "project:member.update"
	ActionProjectMemberDelete Action = "project:member.delete"
	ActionProjectMemberList   Action = "project:member.list"
	ActionProjectMemberLeave  Action = "project:member.leave"

	// 文件相关动作
	ActionFileView     Action = "file:view"
	ActionFileDownload Action = "file:download"

	// 产物相关动作
	ActionArtifactView     Action = "artifact:view"
	ActionArtifactDownload Action = "artifact:download"
)

// ActionSet 是动作集合，用于 O(1) 查找。
type ActionSet map[Action]struct{}

// actionSet 从动作列表构建 ActionSet。
func actionSet(actions ...Action) ActionSet {
	m := make(ActionSet, len(actions))
	for _, a := range actions {
		m[a] = struct{}{}
	}
	return m
}

// IdentityActions 存储一个角色对应的允许动作集合（O(1) 查找）。
type IdentityActions map[types.ResourceRole]ActionSet

// PermissionPolicy 存储所有资源类型的权限策略，不入库，由代码维护。
// 结构：资源类型 -> 身份 -> 允许的动作集合。
// 新增 action 只需修改此处，不需要变更数据库。
type PermissionPolicy map[types.ResourceType]IdentityActions

// SystemPolicy 是系统内置权限策略，由后端代码维护，不受组织配置影响。
// 规则：owner 的动作集合必须是 admin 的超集，admin 的必须是 member 的超集。
// 扩展：如需支持组织级自定义规则，在 PermissionService 中叠加 OrgPolicy，不修改此变量。
var SystemPolicy = PermissionPolicy{
	types.ResourceTypeProject: {
		types.ResourceRoleOwner: actionSet(
			ActionProjectView,
			ActionProjectUpdate,
			ActionProjectDelete,
			ActionProjectArchive,
			ActionProjectMemberCreate,
			ActionProjectMemberUpdate,
			ActionProjectMemberDelete,
			ActionProjectMemberList,
		),
		types.ResourceRoleAdmin: actionSet(
			ActionProjectView,
			ActionProjectUpdate,
			ActionProjectMemberCreate,
			ActionProjectMemberUpdate,
			ActionProjectMemberDelete,
			ActionProjectMemberList,
		),
		types.ResourceRoleMember: actionSet(
			ActionProjectView,
			ActionProjectMemberList,
			ActionProjectMemberLeave,
		),
	},
	types.ResourceTypeFile: {
		types.ResourceRoleOwner:  actionSet(ActionFileView, ActionFileDownload),
		types.ResourceRoleAdmin:  actionSet(ActionFileView, ActionFileDownload),
		types.ResourceRoleMember: actionSet(ActionFileView, ActionFileDownload),
	},
	types.ResourceTypeArtifact: {
		types.ResourceRoleOwner:  actionSet(ActionArtifactView, ActionArtifactDownload),
		types.ResourceRoleAdmin:  actionSet(ActionArtifactView, ActionArtifactDownload),
		types.ResourceRoleMember: actionSet(ActionArtifactView, ActionArtifactDownload),
	},
}

// Allows 判断指定资源类型下的指定角色是否允许执行该动作（O(1) 查找）。
// 未命中 policy 时默认返回 false（拒绝）。
func (p PermissionPolicy) Allows(resourceType types.ResourceType, identity types.ResourceRole, action Action) bool {
	identityMap, ok := p[resourceType]
	if !ok {
		return false
	}
	set, ok := identityMap[identity]
	if !ok {
		return false
	}
	_, ok = set[action]
	return ok
}

// ListAllowedActions 返回指定资源类型下指定角色允许执行的所有动作。
// 供 ExplainPermission 接口展示当前角色可执行的动作列表。
func (p PermissionPolicy) ListAllowedActions(resourceType types.ResourceType, identity types.ResourceRole) []Action {
	identityMap, ok := p[resourceType]
	if !ok {
		return nil
	}
	set, ok := identityMap[identity]
	if !ok {
		return nil
	}
	result := make([]Action, 0, len(set))
	for a := range set {
		result = append(result, a)
	}
	return result
}

// Validate 校验 policy 中所有 action 的前缀是否与其所属资源类型一致。
// 返回所有校验错误，无错误时返回 nil。
// 建议在单元测试中调用，防止拼写错误导致权限静默失败。
func (p PermissionPolicy) Validate() []string {
	var errs []string
	for rt, roleMap := range p {
		prefix := string(rt) + ":"
		for identity, set := range roleMap {
			for a := range set {
				if !strings.HasPrefix(string(a), prefix) {
					errs = append(errs, fmt.Sprintf(
						"action %q 与资源类型 %q（身份 %q）前缀不匹配，期望前缀 %q",
						a, rt, identity, prefix,
					))
				}
			}
		}
	}
	return errs
}

// IsMemberManagementAction 判断动作是否属于成员管理类动作。
// 通过 action 字符串中 ":" 后的部分是否以 "member." 开头来推断，
// 无需随资源类型扩展而手动维护。
func IsMemberManagementAction(action Action) bool {
	s := string(action)
	idx := strings.Index(s, ":")
	if idx < 0 {
		return false
	}
	return strings.HasPrefix(s[idx+1:], "member.")
}

// MaxRole 从多个角色中取强度最高的，用于文件/产物的角色合成。
// owner > admin > member，未识别角色强度视为 0。
// 强度定义见 types.ResourceRoleStrength。
func MaxRole(roles []types.ResourceRole) types.ResourceRole {
	var best types.ResourceRole
	bestScore := -1
	for _, id := range roles {
		if s := types.ResourceRoleStrength[id]; s > bestScore {
			bestScore = s
			best = id
		}
	}
	return best
}
