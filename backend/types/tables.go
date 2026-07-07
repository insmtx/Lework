// types 包提供 Leros 的核心数据类型定义
//
// 该包定义了数字助手、事件、用户、技能等核心领域模型，
// 以及相关的常量和数据库表名定义。
package types

// 数据库表名前缀常量
const (
	tablenamePrefix = "leros_" // 数据库表名统一前缀
)

// 数据库表名常量定义
const (

	// TableNameUser 用户表名
	TableNameUser = tablenamePrefix + "user"
	// TableNameOrganization 组织表名
	TableNameOrganization = tablenamePrefix + "organization"
	// TableNameUserOrg 用户组织关联表名
	TableNameUserOrg = tablenamePrefix + "user_org"
	// TableNameAuthRefreshToken 登录刷新令牌表名
	TableNameAuthRefreshToken = tablenamePrefix + "auth_refresh_token"
	// TableNameAuthLoginAttempt 登录失败尝试表名
	TableNameAuthLoginAttempt = tablenamePrefix + "auth_login_attempt"
	// TableNameAuthPhoneVerificationCode 手机验证码表名
	TableNameAuthPhoneVerificationCode = tablenamePrefix + "auth_phone_verification_code"

	// TableNameDigitalAssistant 数字助手表名
	TableNameDigitalAssistant = tablenamePrefix + "digital_assistant"
	// TableNameDigitalAssistantPromptBlock AI 队友提示词分层块表名
	TableNameDigitalAssistantPromptBlock = tablenamePrefix + "digital_assistant_prompt_block"
	// TableNameDigitalAssistantMemory AI 队友长期记忆表名
	TableNameDigitalAssistantMemory = tablenamePrefix + "digital_assistant_memory"
	// TableNameAssistantPromptTrace AI 队友提示词注入追踪表名
	TableNameAssistantPromptTrace = tablenamePrefix + "assistant_prompt_trace"
	// TableNameAITeammateTemplate AI 队友模板表名
	TableNameAITeammateTemplate = tablenamePrefix + "ai_teammate_template"
	// TableNameDigitalAssistantInstance 数字助手实例表名
	TableNameDigitalAssistantInstance = tablenamePrefix + "digital_assistant_instance"
	// TableNameWorkerDeployment Worker 部署表名
	TableNameWorkerDeployment = tablenamePrefix + "worker_deployment"

	// TableNameEvent 事件表名
	TableNameEvent = tablenamePrefix + "event"

	// TableNameSkill 技能表名
	TableNameSkill = tablenamePrefix + "skill"
	// TableNameSkillLog 技能执行日志表名
	TableNameSkillLog = tablenamePrefix + "skill_execution_log"
	// TableNameSkillRegistry 技能注册表名
	TableNameSkillRegistry = tablenamePrefix + "skill_registry"

	// TableNameSession 会话表名
	TableNameSession = tablenamePrefix + "session"
	// TableNameSessionMessage 会话消息表名
	TableNameSessionMessage = tablenamePrefix + "session_message"

	// TableNameLLMModel LLM模型配置表名
	TableNameLLMModel = tablenamePrefix + "llm_model"

	// TableNameProject 项目表名
	TableNameProject = tablenamePrefix + "project"
	// TableNameProjectMember 项目成员表名
	TableNameProjectMember = tablenamePrefix + "project_member"
	// TableNameResource 统一资源表名
	TableNameResource = tablenamePrefix + "resource"
	// TableNameResourceBinding 统一资源身份绑定表名
	TableNameResourceBinding = tablenamePrefix + "resource_binding"

	// TableNameTask 任务表名
	TableNameTask = tablenamePrefix + "task"
	// TableNameWorkbenchRecentContext 工作台最近使用上下文表名
	TableNameWorkbenchRecentContext = tablenamePrefix + "workbench_recent_context"

	// TableNameFileUpload 文件上传表名
	TableNameFileUpload = tablenamePrefix + "file_upload"
	// TableNameProjectFile 项目文件关联表名
	TableNameProjectFile = tablenamePrefix + "project_file"
	// TableNameBuiltinSkillMarketplaceItem 内置 Skill 市场条目表名
	TableNameBuiltinSkillMarketplaceItem = tablenamePrefix + "builtin_skill_marketplace_item"
	// TableNameSkillMarketplaceItem Skill 市场记录缓存表名
	TableNameSkillMarketplaceItem = tablenamePrefix + "skill_marketplace_item"
	// TableNameOrgSkillInstallation 组织级 Skill 安装记录表名
	TableNameOrgSkillInstallation = tablenamePrefix + "org_skill_installation"

	// TableNameMessageResource 消息资源关联表名
	TableNameMessageResource = tablenamePrefix + "message_resource"

	// TableNameDepartment 组织部门表名
	TableNameDepartment = tablenamePrefix + "department"
	// TableNameMemberDepartment 组织成员部门关联表名
	TableNameMemberDepartment = tablenamePrefix + "rel_user_org_department"
)
