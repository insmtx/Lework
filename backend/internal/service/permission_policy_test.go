package service

import (
	"testing"

	"github.com/insmtx/Leros/backend/types"
)

// TestSystemPolicyValidate 确保系统内置 policy 中所有 action 前缀正确。
func TestSystemPolicyValidate(t *testing.T) {
	errs := SystemPolicy.Validate()
	if len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("policy 校验失败: %s", e)
		}
	}
}

// TestPermissionPolicyAllows 验证 Allows 方法的基本权限判断。
func TestPermissionPolicyAllows(t *testing.T) {
	cases := []struct {
		name         string
		resourceType types.ResourceType
		role         types.ResourceRole
		action       Action
		want         bool
	}{
		{
			name:         "owner 可以查看项目",
			resourceType: types.ResourceTypeProject,
			role:         types.ResourceRoleOwner,
			action:       ActionProjectView,
			want:         true,
		},
		{
			name:         "owner 可以删除项目",
			resourceType: types.ResourceTypeProject,
			role:         types.ResourceRoleOwner,
			action:       ActionProjectDelete,
			want:         true,
		},
		{
			name:         "admin 不能删除项目",
			resourceType: types.ResourceTypeProject,
			role:         types.ResourceRoleAdmin,
			action:       ActionProjectDelete,
			want:         false,
		},
		{
			name:         "member 不能更新项目",
			resourceType: types.ResourceTypeProject,
			role:         types.ResourceRoleMember,
			action:       ActionProjectUpdate,
			want:         false,
		},
		{
			name:         "member 可以退出项目",
			resourceType: types.ResourceTypeProject,
			role:         types.ResourceRoleMember,
			action:       ActionProjectMemberLeave,
			want:         true,
		},
		{
			name:         "member 不能创建成员",
			resourceType: types.ResourceTypeProject,
			role:         types.ResourceRoleMember,
			action:       ActionProjectMemberCreate,
			want:         false,
		},
		{
			name:         "owner 可以下载文件",
			resourceType: types.ResourceTypeFile,
			role:         types.ResourceRoleOwner,
			action:       ActionFileDownload,
			want:         true,
		},
		{
			name:         "未知资源类型默认拒绝",
			resourceType: types.ResourceType("unknown"),
			role:         types.ResourceRoleOwner,
			action:       Action("unknown:view"),
			want:         false,
		},
		{
			name:         "未知角色默认拒绝",
			resourceType: types.ResourceTypeProject,
			role:         types.ResourceRole("viewer"),
			action:       ActionProjectView,
			want:         false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SystemPolicy.Allows(tc.resourceType, tc.role, tc.action)
			if got != tc.want {
				t.Errorf("Allows(%q, %q, %q) = %v, want %v",
					tc.resourceType, tc.role, tc.action, got, tc.want)
			}
		})
	}
}

// TestPermissionPolicyListAllowedActions 验证 ListAllowedActions 返回正确的动作列表。
func TestPermissionPolicyListAllowedActions(t *testing.T) {
	// owner 应拥有所有项目动作
	ownerActions := SystemPolicy.ListAllowedActions(types.ResourceTypeProject, types.ResourceRoleOwner)
	if len(ownerActions) == 0 {
		t.Error("owner 的项目动作列表不应为空")
	}

	// 将结果转为 set 方便检查
	ownerSet := make(map[Action]struct{}, len(ownerActions))
	for _, a := range ownerActions {
		ownerSet[a] = struct{}{}
	}
	for _, must := range []Action{
		ActionProjectView, ActionProjectDelete, ActionProjectArchive,
		ActionProjectMemberCreate, ActionProjectMemberDelete,
	} {
		if _, ok := ownerSet[must]; !ok {
			t.Errorf("owner 动作列表缺少 %q", must)
		}
	}

	// member 不应包含 delete/archive/member.create
	memberActions := SystemPolicy.ListAllowedActions(types.ResourceTypeProject, types.ResourceRoleMember)
	memberSet := make(map[Action]struct{}, len(memberActions))
	for _, a := range memberActions {
		memberSet[a] = struct{}{}
	}
	for _, forbidden := range []Action{
		ActionProjectDelete, ActionProjectArchive, ActionProjectMemberCreate,
	} {
		if _, ok := memberSet[forbidden]; ok {
			t.Errorf("member 动作列表不应包含 %q", forbidden)
		}
	}

	// 未知资源类型应返回 nil
	if got := SystemPolicy.ListAllowedActions(types.ResourceType("unknown"), types.ResourceRoleOwner); got != nil {
		t.Errorf("未知资源类型应返回 nil，got %v", got)
	}
}

// TestIsMemberManagementAction 验证成员管理动作的识别逻辑。
func TestIsMemberManagementAction(t *testing.T) {
	cases := []struct {
		action Action
		want   bool
	}{
		{ActionProjectMemberCreate, true},
		{ActionProjectMemberUpdate, true},
		{ActionProjectMemberDelete, true},
		{ActionProjectMemberLeave, true},
		{ActionProjectView, false},
		{ActionProjectUpdate, false},
		{ActionFileView, false},
		{Action("knowledge_base:member.create"), true}, // 未来新资源类型自动生效
		{Action("workflow:member.update"), true},
		{Action("project:members"), false}, // 不以 member. 开头
		{Action("invalidaction"), false},   // 无 : 分隔符
	}

	for _, tc := range cases {
		got := IsMemberManagementAction(tc.action)
		if got != tc.want {
			t.Errorf("IsMemberManagementAction(%q) = %v, want %v", tc.action, got, tc.want)
		}
	}
}

// TestMaxRole 验证角色强度合成逻辑。
func TestMaxRole(t *testing.T) {
	cases := []struct {
		name  string
		roles []types.ResourceRole
		want  types.ResourceRole
	}{
		{
			name:  "owner 强于 admin",
			roles: []types.ResourceRole{types.ResourceRoleAdmin, types.ResourceRoleOwner},
			want:  types.ResourceRoleOwner,
		},
		{
			name:  "admin 强于 member",
			roles: []types.ResourceRole{types.ResourceRoleMember, types.ResourceRoleAdmin},
			want:  types.ResourceRoleAdmin,
		},
		{
			name:  "单个 member",
			roles: []types.ResourceRole{types.ResourceRoleMember},
			want:  types.ResourceRoleMember,
		},
		{
			name:  "空列表返回零值",
			roles: []types.ResourceRole{},
			want:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MaxRole(tc.roles)
			if got != tc.want {
				t.Errorf("MaxRole(%v) = %q, want %q", tc.roles, got, tc.want)
			}
		})
	}
}

// TestPermissionPolicyValidateDetectsError 验证 Validate 能发现错误配置。
func TestPermissionPolicyValidateDetectsError(t *testing.T) {
	badPolicy := PermissionPolicy{
		types.ResourceTypeProject: {
			types.ResourceRoleOwner: actionSet(
				ActionProjectView,
				Action("file:view"), // 错误：file 动作放在 project 下
			),
		},
	}
	errs := badPolicy.Validate()
	if len(errs) == 0 {
		t.Error("Validate 应该检测到前缀不匹配的错误，但返回了空")
	}
}
