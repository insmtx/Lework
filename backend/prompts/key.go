package prompts

const KeyAgentSystemDefault = "agent.system.default" // 角色定义：Leros 助手身份声明

// Native Runner 专用 — 在通用提示词之前追加，仅 native agent 使用。
const (
	KeyAgentNativeTaskCompletion  = "agent.native.task_completion"  // 任务完成：反编造、持续执行直到产出真实结果
	KeyAgentNativeToolEnforcement = "agent.native.tool_enforcement" // 工具强制：执行纪律、必须用工具而非描述意图
	KeyAgentNativeSkillLoading    = "agent.native.skill_loading"    // Skill 加载指令 + <available_skills> 占位（有 skills 时）
	KeyAgentNativeSkillUsageHint  = "agent.native.skill_usage_hint" // Skill 调用提示：强调使用 skill_use(name) 加载 skill
)

// Native Runner 专用 — 产物声明。
const (
	KeyAgentNativeArtifactDeclaration = "agent.native.artifact_declaration" // 产物声明：必须声明交付物
)

// ContextBuilder 通用层 — 所有引擎共享。
const (
	KeyAgentSystemMemoryGuidance = "agent.system.memory_guidance" // Memory 工具指导：何时保存/不保存记忆
)

// 平台格式指导 — 按消息通道注入对应的格式约束。
const (
	KeyAgentSystemPlatformWechat = "agent.system.platform.wechat" // 微信：长度限制、Markdown 兼容
	KeyAgentSystemPlatformFeishu = "agent.system.platform.feishu" // 飞书：富文本格式
	KeyAgentSystemPlatformSlack  = "agent.system.platform.slack"  // Slack：mrkdwn 格式
	KeyAgentSystemPlatformAPI    = "agent.system.platform.api"    // API：纯 JSON 响应
)

const (
	KeyEventOrchestratorHeader           = "event.orchestrator.header"
	KeyEventOrchestratorTaskDefault      = "event.orchestrator.task.default"
	KeyEventOrchestratorTaskPullRequest  = "event.orchestrator.task.pull_request"
	KeyEventOrchestratorTaskPush         = "event.orchestrator.task.push"
	KeyEventOrchestratorTaskIssueComment = "event.orchestrator.task.issue_comment"
)

const KeyLLMTestConnectivity = "llm.test.connectivity"

const KeySessionTitle = "session.title.generate"
const KeyWorkTitle = "work.title.generate"
