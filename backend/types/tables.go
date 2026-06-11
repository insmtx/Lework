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

	// TableNameDigitalAssistant 数字助手表名
	TableNameDigitalAssistant = tablenamePrefix + "digital_assistant"
	// TableNameDigitalAssistantInstance 数字助手实例表名
	TableNameDigitalAssistantInstance = tablenamePrefix + "digital_assistant_instance"

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

	// TableNameTask 任务表名
	TableNameTask = tablenamePrefix + "task"

	// TableNameArtifact 任务产出物表名
	TableNameArtifact = tablenamePrefix + "artifact"

	// TableNameBuiltinSkillMarketplaceItem 内置 Skill 市场条目表名
	TableNameBuiltinSkillMarketplaceItem = tablenamePrefix + "builtin_skill_marketplace_item"
)
