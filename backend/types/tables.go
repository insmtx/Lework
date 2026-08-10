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

	// TableNameSession 会话表名
	TableNameSession = tablenamePrefix + "session"
	// TableNameSessionMessage 会话消息表名
	TableNameSessionMessage = tablenamePrefix + "session_message"
	// TableNameReliableTask 通用可靠任务 Outbox 表名。
	TableNameReliableTask = tablenamePrefix + "reliable_task"
	// TableNameProjectionReceipt 通用事件投影回执表名。
	TableNameProjectionReceipt = tablenamePrefix + "projection_receipt"

	// TableNameLLMModel LLM模型配置表名
	TableNameLLMModel = tablenamePrefix + "llm_model"

	// TableNameLLMHistory LLM调用记录表名
	TableNameLLMHistory = tablenamePrefix + "llm_history"

	// TableNameLLMCallRecord 旧LLM调用记录表名（待迁移至 TableNameLLMHistory）
	TableNameLLMCallRecord = tablenamePrefix + "llm_call_record"

	// TableNameProject 项目表名
	TableNameProject = tablenamePrefix + "project"
	// TableNameProjectMember 项目成员表名
	TableNameProjectMember = tablenamePrefix + "project_member"
	// TableNameProjectActivity 项目操作动态表名
	TableNameProjectActivity = tablenamePrefix + "project_activity"
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
	// TableNamePlugin 组织插件表名
	TableNamePlugin = tablenamePrefix + "plugin"
	// TableNamePluginRevision 插件修订表名
	TableNamePluginRevision = tablenamePrefix + "plugin_revision"
	// TableNamePluginRevisionContent 插件修订内容快照表名
	TableNamePluginRevisionContent = tablenamePrefix + "plugin_revision_content"
	// TableNameProjectPluginBinding 项目插件绑定表名
	TableNameProjectPluginBinding = tablenamePrefix + "project_plugin_binding"
	// TableNamePluginMarketplaceItem 系统插件市场目录表名
	TableNamePluginMarketplaceItem = tablenamePrefix + "plugin_marketplace_item"
	// TableNameMCPChannel 系统 MCP 渠道配置表名
	TableNameMCPChannel = tablenamePrefix + "mcp_channel"

	// TableNameMessageResource 消息资源关联表名
	TableNameMessageResource = tablenamePrefix + "message_resource"

	// TableNameDepartment 组织部门表名
	TableNameDepartment = tablenamePrefix + "department"
	// TableNameMemberDepartment 组织成员部门关联表名
	TableNameMemberDepartment = tablenamePrefix + "rel_user_org_department"

	// TableNameSeedRecord SQL 种子执行记录表名
	TableNameSeedRecord = tablenamePrefix + "seed_record"

	// TableNameAutomation 自动化定时任务配置表名
	TableNameAutomation = tablenamePrefix + "automation"

	// TableNameAutomationExecution 自动化执行记录表名
	TableNameAutomationExecution = tablenamePrefix + "automation_execution"
)
