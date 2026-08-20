package types

// Action 表示对资源的操作类型，由 PermissionPolicy 定义允许的集合。
type Action string

const (
	// ActionProjectView 查看项目。
	ActionProjectView Action = "project:view"
	// ActionProjectUpdate 更新项目元数据。
	ActionProjectUpdate Action = "project:update"
	// ActionProjectDelete 删除项目。
	ActionProjectDelete Action = "project:delete"
	// ActionProjectMemberCreate 添加项目成员。
	ActionProjectMemberCreate Action = "project:member.create"
	// ActionProjectMemberUpdate 更新项目成员角色。
	ActionProjectMemberUpdate Action = "project:member.update"
	// ActionProjectMemberDelete 移除项目成员。
	ActionProjectMemberDelete Action = "project:member.delete"
	// ActionProjectMemberList 查看项目成员列表。
	ActionProjectMemberList Action = "project:member.list"
	// ActionProjectMemberLeave 退出项目（仅允许操作自己）。
	ActionProjectMemberLeave Action = "project:member.leave"

	// ActionFileView 查看文件。
	ActionFileView Action = "file:view"
	// ActionFileDownload 下载文件。
	ActionFileDownload Action = "file:download"

	// ActionArtifactView 查看产物。
	ActionArtifactView Action = "artifact:view"
	// ActionArtifactDownload 下载产物。
	ActionArtifactDownload Action = "artifact:download"

	// ActionTaskCreate 在父项目下创建任务（经项目 effective role 解释）。
	ActionTaskCreate Action = "task:create"
	// ActionTaskView 查看任务。
	ActionTaskView Action = "task:view"
	// ActionTaskUpdate 更新任务。
	ActionTaskUpdate Action = "task:update"
	// ActionTaskDelete 删除任务。
	ActionTaskDelete Action = "task:delete"

	// ActionPluginView 查看插件详情与版本。
	ActionPluginView Action = "plugin:view"
	// ActionPluginUse 在任务执行中使用插件。
	ActionPluginUse Action = "plugin:use"
	// ActionPluginUpdate 编辑插件内容。
	ActionPluginUpdate Action = "plugin:update"
	// ActionPluginDelete 删除插件。
	ActionPluginDelete Action = "plugin:delete"
	// ActionPluginPermissionRead 读取插件权限配置。
	ActionPluginPermissionRead Action = "plugin:permission.read"
	// ActionPluginPermissionUpdate 更新插件成员权限配置。
	ActionPluginPermissionUpdate Action = "plugin:permission.update"
	// ActionPluginVisibilityUpdate 修改插件公开性。
	ActionPluginVisibilityUpdate Action = "plugin:visibility.update"
)

// PermissionCaller 表示权限判断中的请求主体。
// Uin 与 AssistantID 互斥：普通用户登录时 Uin 非 0，助手身份时 AssistantID 非 0。
type PermissionCaller struct {
	OrgID       uint
	Uin         uint
	AssistantID uint
}

// ResourceRef 是前端或业务层传入的资源引用，PermissionService 据此定位 resource 记录。
type ResourceRef struct {
	Type  ResourceType
	BizID uint
}

// MemberInput 是成员管理类动作的请求上下文，由调用方传入用于派生 MemberAuthContext。
// 所有字段均来自请求参数，不得由前端直接控制业务逻辑。
type MemberInput struct {
	// TargetUin 被操作用户的 Uin（与 TargetAssistantID 互斥）。
	TargetUin uint
	// TargetAssistantID 被操作助手的 ID（与 TargetUin 互斥）。
	TargetAssistantID *uint
	// RequestedRole 本次操作希望赋予目标的角色。
	RequestedRole ResourceRole
}

// PermissionDecision 是单次权限判断结果。
type PermissionDecision struct {
	Allowed           bool
	Reason            string
	Role              ResourceRole
	ResourceID        uint
	MatchedBindingID  uint
	MatchedResourceID uint
}

// PermissionExplainDecision 在 PermissionDecision 基础上增加继承来源信息。
type PermissionExplainDecision struct {
	PermissionDecision
	Inherited bool
}
