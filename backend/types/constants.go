// types 包提供 Leros 的核心数据类型定义
//
// 该包定义了数字助手、事件、用户、技能等核心领域模型，
// 以及相关的常量和数据库表名定义。
package types

const (
	SystemUin   uint = 1
	SystemOrgID uint = 1
)

// DigitalAssistantStatus 表示数字助手的当前运行状态
type DigitalAssistantStatus string

const (
	// DigitalAssistantStatusActive 表示数字助手处于激活状态
	DigitalAssistantStatusActive DigitalAssistantStatus = "active"
	// DigitalAssistantStatusInactive 表示数字助手处于非激活状态
	DigitalAssistantStatusInactive DigitalAssistantStatus = "inactive"
	// DigitalAssistantStatusPaused 表示数字助手处于暂停状态
	DigitalAssistantStatusPaused DigitalAssistantStatus = "paused"
	// DigitalAssistantStatusError 表示数字助手处于错误状态
	DigitalAssistantStatusError DigitalAssistantStatus = "error"
)

// ProjectStatus 项目生命周期状态
type ProjectStatus string

const (
	ProjectStatusActive    ProjectStatus = "active"
	ProjectStatusPaused    ProjectStatus = "paused"
	ProjectStatusCompleted ProjectStatus = "completed"
	ProjectStatusArchived  ProjectStatus = "archived"
)

// MemberType 成员类型
type MemberType string

const (
	MemberTypeUser      MemberType = "user"
	MemberTypeAssistant MemberType = "assistant"
)

// MemberRole 成员角色
type MemberRole string

const (
	MemberRoleOwner  MemberRole = "owner"
	MemberRoleAdmin  MemberRole = "admin"
	MemberRoleMember MemberRole = "member"
	MemberRoleViewer MemberRole = "viewer"
)

// SkillCategory 表示技能的类别（如 integration, tool, workflow, ai 等）
type SkillCategory string

const (
	// SkillCategoryIntegration 表示集成类技能
	SkillCategoryIntegration SkillCategory = "integration"
	// SkillCategoryTool 表示工具类技能
	SkillCategoryTool SkillCategory = "tool"
	// SkillCategoryWorkflow 表示工作流技能
	SkillCategoryWorkflow SkillCategory = "workflow"
	// SkillCategoryAI 表示AI类技能
	SkillCategoryAI SkillCategory = "ai"
)

// SkillType 表示技能类型（本地技能或远程技能）
type SkillType string

const (
	// SkillTypeLocal 表示本地技能
	SkillTypeLocal SkillType = "local"
	// SkillTypeRemote 表示远程技能
	SkillTypeRemote SkillType = "remote"
)

// SkillStatus 表示技能的状态（active, inactive, deprecated）
type SkillStatus string

const (
	// SkillStatusActive 表示技能处于激活状态
	SkillStatusActive SkillStatus = "active"
	// SkillStatusInactive 表示技能处于非激活状态
	SkillStatusInactive SkillStatus = "inactive"
	// SkillStatusDeprecated 表示技能已被弃用
	SkillStatusDeprecated SkillStatus = "deprecated"
)

// SkillRegistryStatus 表示技能注册状态
type SkillRegistryStatus string

const (
	// SkillRegistryStatusRegistered 表示已注册
	SkillRegistryStatusRegistered SkillRegistryStatus = "registered"
	// SkillRegistryStatusUnregistered 表示未注册
	SkillRegistryStatusUnregistered SkillRegistryStatus = "unregistered"
	// SkillRegistryStatusUnhealthy 表示不健康
	SkillRegistryStatusUnhealthy SkillRegistryStatus = "unhealthy"
)

// ChannelType 表示渠道类型标识
type ChannelType string

const (
	// ChannelTypeGitHub 表示GitHub渠道
	ChannelTypeGitHub ChannelType = "github"
	// ChannelTypeGitLab 表示GitLab渠道
	ChannelTypeGitLab ChannelType = "gitlab"
	// ChannelTypeWeChat 表示微信渠道
	ChannelTypeWeChat ChannelType = "wechat"
	// ChannelTypeWeWork 表示企业微信渠道
	ChannelTypeWeWork ChannelType = "wework"
	// ChannelTypeFeishu 表示飞书渠道
	ChannelTypeFeishu ChannelType = "feishu"
)

// KnowledgeType 表示知识库类型标识
type KnowledgeType string

const (
	// KnowledgeTypeVectorStorage 表示向量存储知识库
	KnowledgeTypeVectorStorage KnowledgeType = "vector_storage"
	// KnowledgeTypeDocument 表示文档知识库
	KnowledgeTypeDocument KnowledgeType = "document"
	// KnowledgeTypeDatabase 表示数据库知识库
	KnowledgeTypeDatabase KnowledgeType = "database"
)

// RuntimeType 表示运行时环境类型标识
type RuntimeType string

const (
	// RuntimeTypeDocker 表示Docker容器运行时
	RuntimeTypeDocker RuntimeType = "docker"
	// RuntimeTypeProcess 表示进程运行时
	RuntimeTypeProcess RuntimeType = "process"
)

// LLMProviderType 表示LLM提供商类型标识
type LLMProviderType string

const (
	// LLMProviderOpenAI 表示OpenAI提供商
	LLMProviderOpenAI LLMProviderType = "openai"
	// LLMProviderAnthropic 表示Anthropic提供商
	LLMProviderAnthropic LLMProviderType = "anthropic"
	// LLMProviderDeepSeek 表示DeepSeek提供商
	LLMProviderDeepSeek LLMProviderType = "deepseek"
	// LLMProviderQwen 表示通义千问提供商
	LLMProviderQwen LLMProviderType = "qwen"
	// LLMProviderGemini 表示Google Gemini提供商
	LLMProviderGemini LLMProviderType = "gemini"
	// LLMProviderArk 表示火山方舟提供商
	LLMProviderArk LLMProviderType = "ark"
	// LLMProviderOpenRouter 表示OpenRouter提供商
	LLMProviderOpenRouter LLMProviderType = "openrouter"
	// LLMProviderCustom 表示自定义提供商
	LLMProviderCustom LLMProviderType = "custom"
)

// MemoryType 表示记忆存储类型标识
type MemoryType string

const (
	// MemoryTypePostgres 表示基于PostgreSQL的记忆存储
	MemoryTypePostgres MemoryType = "postgres"
	// MemoryTypeInMemory 表示内存记忆存储
	MemoryTypeInMemory MemoryType = "in_memory"
)

// EventType 表示事件的类型
type EventType string

const (
	// EventTypeGitHub 表示GitHub类型的事件
	EventTypeGitHub EventType = "github"
	// EventTypeGitLab 表示GitLab类型的事件
	EventTypeGitLab EventType = "gitlab"
	// EventTypeWebhook 表示Webhook类型的事件
	EventTypeWebhook EventType = "webhook"
)

// EventAction 表示事件的动作行为
type EventAction string

const (
	// EventActionOpened 表示打开或创建事件
	EventActionOpened EventAction = "opened"
	// EventActionClosed 表示关闭或删除事件
	EventActionClosed EventAction = "closed"
	// EventActionUpdated 表示更新事件
	EventActionUpdated EventAction = "updated"
	// EventActionCommented 表示评论事件
	EventActionCommented EventAction = "commented"
)

// TaskStatus 表示任务的当前状态
type TaskStatus string

const (
	TaskStatusCreated           TaskStatus = "created"
	TaskStatusPendingAssignment TaskStatus = "pending_assignment"
	TaskStatusInProgress        TaskStatus = "in_progress"
	TaskStatusPendingApproval   TaskStatus = "pending_approval"
	TaskStatusCompleted         TaskStatus = "completed"
	TaskStatusFailed            TaskStatus = "failed"
	TaskStatusCancelled         TaskStatus = "cancelled"
	TaskStatusPaused            TaskStatus = "paused"
)

type TaskType string

const (
	TaskTypeGeneral TaskType = "general"
	TaskTypeCron    TaskType = "cron"
)

// ArtifactType 表示产出物的类型
type ArtifactType string

const (
	// ArtifactTypeText 文本内容（文档、代码、报告等）
	ArtifactTypeText ArtifactType = "text"
	// ArtifactTypeStructuredData 结构化数据（JSON、表格、CSV）
	ArtifactTypeStructuredData ArtifactType = "structured_data"
	// ArtifactTypeFile 文件附件（图片、压缩包、二进制）
	ArtifactTypeFile ArtifactType = "file"
	// ArtifactTypeComposite 复合产物（多种类型混合）
	ArtifactTypeComposite ArtifactType = "composite"
)

// ArtifactStatus 表示产出物的状态
type ArtifactStatus string

const (
	// ArtifactStatusGenerating 产出物生成中
	ArtifactStatusGenerating ArtifactStatus = "generating"
	// ArtifactStatusCompleted 产出物已完成
	ArtifactStatusCompleted ArtifactStatus = "completed"
	// ArtifactStatusFailed 产出物生成失败
	ArtifactStatusFailed ArtifactStatus = "failed"
)

// ArtifactSource 表示产出物记录来源。
type ArtifactSource string

const (
	// ArtifactSourceAgentDeclared 表示 Agent 通过 manifest 显式声明的产物。
	ArtifactSourceAgentDeclared ArtifactSource = "agent_declared"
	// ArtifactSourceDiff 表示通过执行前后文件变更自动检测到的产物。
	ArtifactSourceDiff ArtifactSource = "diff"
)
