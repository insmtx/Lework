# Leros 项目结构与文件索引

## 项目概览

Leros 是一个**企业级数字员工操作系统**，基于 Golang 构建，采用三平面架构：

- **控制 平面**（Control Plane）：Gin HTTP 服务，管理 UI API、会话、数字员工 CRUD
- **事件总线**（Event Bus）：NATS JetStream，解耦组件间通信
- **工作平面**（Worker Plane）：后台 Worker，执行 Agent 运行时

Go Module: `github.com/insmtx/Leros` | Go 1.24

### 核心依赖

| 组件 | 用途 |
|------|------|
| Gin | HTTP 框架 |
| Cobra | CLI 框架 |
| GORM + PostgreSQL | 数据库 ORM |
| NATS JetStream | 消息队列 |
| CloudWeGo Eino | Agent/LLM 框架（ADK） |
| gorilla/websocket | WebSocket |
| MCP-Go | Model Context Protocol |
| swaggo | Swagger 文档 |
| jupiter | 内部框架（ygpkg/yg-go） |

### 核心抽象接口

| 接口 | 位置 | 职责 |
|------|------|------|
| `agent.Runner` | `backend/internal/agent/runner.go:11` | 数字员工单次运行的执行边界 |
| `tools.Tool` | `backend/tools/tool.go:29` | 最小工具接口 |
| `engines.Engine` | `backend/engines/engine.go:65` | 外部 AI CLI 引擎边界 |
| `mq.EventBus` | `backend/internal/infra/mq/bus.go:32` | 事件总线（发布+订阅） |
| `Connector` | `backend/internal/api/connectors/connector.go:14` | 外部渠道连接器 |

### 数据流

```
UI Client → REST/WS → Server → NATS → Worker → Agent Runtime → Tools/MCP → NATS stream → Server → WS → UI
GitHub/GitLab Webhook → Server → NATS → Event Engine → Agent Runner
```

---

## 目录树

### 根目录

```
/ (root)
├── go.mod / go.sum           # Go 模块定义
├── Makefile                  # 构建命令（build/docker/run/swagger/dev）
├── AGENTS.md                 # AI Agent 开发指南（含构建/测试命令）
├── config.example.yaml       # 示例配置文件
├── minimal-config.yaml       # 最小启动配置
├── CONTRIBUTING.md           # 贡献指南
├── README.md / README_en.md  # 项目说明（中/英）
├── LICENSE                   # 许可证
├── .dockerignore / .gitignore
├── backend/                  # Go 后端源码（主体）
├── docs/                     # 文档
├── deployments/              # 部署配置（Docker/docker-compose）
├── frontend/                 # 前端应用（Next.js/Electron）
└── bundles/                  # 构建产物（已 gitignore）
```

### `backend/` - Go 后端源码

#### `backend/cmd/leros/` — 应用入口

| 文件 | 说明 |
|------|------|
| `main.go` | Cobra 根命令，设置日志级别 |
| `server.go` | `leros server` — 启动 HTTP 服务（加载配置、NATS、DB、路由） |
| `worker.go` | `leros worker` — 启动 Worker（MCP 服务、Agent Runtime、Task Consumer） |
| `worker_claudecode.go` | `leros worker claude-worker` — Claude Code 专用 Worker |
| `worker_simplechat.go` | `leros worker simplechat` — Leros 内置运行时 Worker |
| `chat.go` | CLI 聊天命令（本地调试交互） |
| `project.go` | Project CLI 命令（本地调试） |
| `task.go` | Task CLI 命令（本地调试） |

#### `backend/config/` — 配置类型

| 文件 | 说明 |
|------|------|
| `config.go` | 主 `Config` 结构体（Server/Github/NATS/DB/LLM/Scheduler） |
| `worker.go` | `WorkerConfig` — Worker 进程配置 |
| `scheduler.go` | `SchedulerConfig` — Worker 调度模式配置 |
| `github.go` | `GithubAppConfig` — GitHub App 集成配置 |
| `gitlab.go` | `GitlabAppConfig` — GitLab 集成配置 |

#### `backend/types/` — 核心领域类型（GORM Models，表前缀 `leros_`）

| 文件 | 核心类型 |
|------|----------|
| `digital_assistant.go` | `DigitalAssistant`（编码/组织/名称/状态/版本/系统提示词） |
| `event.go` | `Event`（消息ID/追踪ID/来源/类型/动作/载荷） |
| `session.go` | `Session`（公共ID/类型/状态/标题）、`SessionMessage`（角色/内容/块/用量） |
| `skill.go` | `Skill`（编码/名称/描述/类别/输入输出Schema/权限） |
| `task.go` | `Task`（公共ID/组织/项目/会话/状态/截止时间） |
| `project.go` | `Project`、`ProjectMember` |
| `llm_model.go` | `LLMModel`（提供商/模型名/BaseURL/APIKey加密/最大Token/温度） |
| `user.go` | `User` |
| `organization.go` | `Organization` |
| `artifact.go` | `Artifact`（任务输出产物） |
| `skill_registry.go` | `SkillRegistry` |
| `skill_execution_log.go` | `SkillExecutionLog`（审计日志） |
| `tables.go` | 数据库表名常量 |
| `constants.go` | 所有类型安全常量：`DigitalAssistantStatus`、`LLMProviderType`（openai/anthropic/deepseek/qwen/gemini/ark/openrouter/custom）、`SessionStatus`、`TaskStatus`、`EventType/Action` 等 |
| `user_org.go` | 用户与组织关系 |
| `util.go` | 类型工具函数 |

#### `backend/pkg/` — 共享工具包

| 文件 | 说明 |
|------|------|
| `pkg/event/event.go` | 跨模块事件结构体 |
| `pkg/event/topic.go` | NATS 主题常量（`TopicGithubIssueComment`、`TopicGithubPullRequest`、`TopicGithubPush`） |
| `pkg/dm/subject.go` | Worker 任务和消息流的 NATS Subject 构建 |
| `pkg/dm/stream.go` | Stream 消息类型 |
| `pkg/dm/consumer.go` | 持久化 Consumer 名称生成 |
| `pkg/leros/home.go` | Leros 主目录和技能目录解析 |
| `pkg/utils/trailing_debouncer.go` | 尾部去抖器（任务去重） |
| `pkg/utils/value_fallback.go` | 值回退辅助 |

#### `backend/tools/` — 工具系统

| 文件 | 说明 |
|------|------|
| `tool.go` | 核心接口：`Tool`（Name/InputSchema/Execute）、`BaseTool`、`Schema`/`Property`、`ToolContext` |
| `registry.go` | 线程安全的 Tool 注册表 |
| `tools/memory/memory.go` | 记忆工具（Agent 短期/长期记忆） |
| `tools/memory/register.go` | 记忆工具注册 |
| `tools/skill_use/skill_use.go` | 技能使用工具（列出/获取技能） |
| `tools/skill_use/register.go` | 技能使用工具注册 |
| `tools/skill_manage/skill_manage.go` | 技能管理工具（安装/卸载） |
| `tools/skill_manage/register.go` | 技能管理工具注册 |
| `tools/todo/todo.go` | Todo/计划工具 |
| `tools/todo/register.go` | Todo 工具注册 |
| `tools/node/node.go` | 节点执行工具入口 |
| `tools/node/file_read.go` | 文件读取工具 |
| `tools/node/file_write.go` | 文件写入工具 |
| `tools/node/shell.go` | Shell 命令执行工具 |
| `tools/node/workspace.go` | 工作空间管理工具 |
| `tools/node/security/` | 安全策略（工作空间限制、审批门禁、环境隔离、写入禁止规则） |
| `tools/node/util/helpers.go` | 节点工具辅助函数 |
| `tools/artifact_declare/tool.go` | 产物声明工具 |
| `tools/test/echo.go` | Echo 测试工具 |

#### `backend/engines/` — 外部 AI CLI 引擎

| 文件 | 说明 |
|------|------|
| `engine.go` | 核心接口：`Engine`（Prepare/RegisterMCP/Run）、`RunRequest`、`Process`、`RunHandle` |
| `registry.go` | 引擎注册表 |
| `env.go` | 引擎环境配置 |
| `process.go` | 进程生命周期事件 |
| `status.go` | 引擎状态类型 |
| `cli_discovery.go` | CLI 引擎自动发现（从 PATH） |
| `skills_sync.go` | 技能同步 |
| `mcp_registration.go` | MCP 服务器注册 |
| `scan.go` | 引擎扫描 |
| `workdir.go` | 引擎工作目录管理 |
| `engines/claude/adapter.go` | Claude Code 适配器 |
| `engines/claude/invoker.go` | Claude Code 进程调用器 |
| `engines/codex/adapter.go` | Codex 适配器 |
| `engines/codex/invoker.go` | Codex 进程调用器 |
| `engines/builtin/factory.go` | 引擎注册表工厂 |
| `engines/builtin/bootstrap.go` | `BootstrapService` — CLI 引擎分层引导 |

#### `backend/prompts/` — 提示词模板系统

| 文件 | 说明 |
|------|------|
| `prompt.go` | `Manager` — 模板注册表 + 全局单例 `globalManager` |
| `executor_eino.go` | `EinoExecutor` — 基于 Eino LLM 的执行器 |
| `prompt_agent.go` | **默认 Agent 系统提示词**（注册为 `KeyAgentSystemDefault`） |
| `prompt_llm.go` | LLM 相关提示词模板 |
| `prompt_session.go` | 会话提示词模板 |
| `prompt_event.go` | 事件提示词模板 |
| `key.go` | 模板 Key 常量 |
| `option.go` | `RunOption` 函数式选项 |

#### `backend/skills/` — 技能定义

| 文件 | 说明 |
|------|------|
| `anysearch/SKILL.md` | AnySearch 技能定义（Markdown Manifest 格式） |
| `anysearch/.env.example` | 环境变量示例 |
| `anysearch/runtime.conf.example` | 运行时配置示例 |
| `anysearch/scripts/anysearch_cli.py` | Python CLI 封装 |
| `anysearch/scripts/anysearch_cli.sh` | Shell CLI 封装 |
| `anysearch/scripts/anysearch_cli.js` | Node.js CLI 封装 |
| `anysearch/scripts/anysearch_cli.ps1` | PowerShell CLI 封装 |

#### `backend/internal/agent/` — Agent 系统

| 文件/目录 | 说明 |
|-----------|------|
| `runner.go` | `Runner` 接口定义（Agent 单次运行的执行边界） |
| `router.go` | `RuntimeRouter` — 按类型分发到具体 Runner（`RuntimeKindLeros`） |
| `request.go` | Agent 运行请求类型 |
| `result.go` | Agent 运行结果类型 |

#### `backend/internal/runtime/` — Agent 运行时基础设施

| 文件/目录 | 说明 |
|-----------|------|
| `service.go` | `Service` — 顶层运行时服务，构建 DI 容器和 Router |
| `events/events.go` | 事件系统：`EventType`（run.*/message.*/tool_call.*/todo.*）、Payload 类型、工厂函数 |
| `events/envelope.go` | 领域消息协议：`Envelope[T]` 泛型信封（ID/Type/Trace/Route/Body） |
| `events/sink.go` | `Sink` 事件发射接口 |
| `events/stream.go` | 流转发消息类型 |
| `events/emitter.go` | 事件发射器 |
| `events/log_sink.go` | 日志事件 Sink |
| `events/message_id.go` | 消息 ID 生成 |
| `lifecycle/pipeline.go` | 运行生命周期管道编排 |
| `lifecycle/runner.go` | 生命周期 Runner（前置准备 → 委派运行 → 后置学习） |
| `lifecycle/state.go` | 运行状态管理 |
| `lifecycle/errors.go` | 生命周期错误定义 |
| `lifecycle/context/builder.go` | 请求上下文构建器 |
| `lifecycle/context/session_messages.go` | 会话消息历史加载 |
| `lifecycle/journal/run_events.go` | 运行事件定义 |
| `lifecycle/journal/run_journal.go` | 运行事件日志 |
| `lifecycle/steps/artifact.go` | 产物处理步骤 |
| `lifecycle/steps/authorize.go` | 授权步骤 |
| `lifecycle/steps/context.go` | 上下文构建步骤 |
| `lifecycle/steps/execute.go` | 执行步骤 |
| `lifecycle/steps/helpers.go` | 步骤辅助函数 |
| `lifecycle/steps/journal.go` | 日志步骤 |
| `lifecycle/steps/learning.go` | 运行后学习步骤 |
| `lifecycle/steps/model.go` | 模型配置步骤 |
| `lifecycle/steps/normalize.go` | 结果规范化步骤 |
| `lifecycle/steps/persist.go` | 持久化步骤 |
| `lifecycle/steps/pipeline.go` | 管道步骤编排 |
| `lifecycle/steps/session.go` | 会话管理步骤 |
| `lifecycle/steps/start_event.go` | 启动事件步骤 |
| `lifecycle/steps/state.go` | 状态管理步骤 |
| `drivers/native/runner.go` | **内置 Leros 运行时** — 基于 CloudWeGo Eino，8 个 LLM 提供商，绑定默认工具 |
| `drivers/native/state.go` | 原生运行时状态管理 |
| `drivers/externalcli/runner.go` | **外部 CLI Runner** — 适配 Claude Code/Codex 为 `agent.Runner` |
| `drivers/externalcli/prompt.go` | 外部 CLI 提示词构建器 |
| `drivers/externalcli/session_store.go` | Provider 会话存储接口 |
| `drivers/externalcli/session_memory_store.go` | 内存会话存储 |
| `drivers/externalcli/session_metadata_store.go` | DB 会话元数据存储 |
| `drivers/simplechat/simplechat.go` | 简单聊天 Agent |
| `drivers/simplechat/console.go` | 控制台聊天 UI |
| `eino/flow.go` | **Eino Flow** — 封装 `adk.ChatModelAgent`，支持流式/非流式 |
| `eino/chatmodel.go` | LLM 模型适配器（OpenAI/Anthropic/Qwen/DeepSeek/Gemini/Ark/OpenRouter/Custom） |
| `eino/tool_adapter.go` | Tool → Eino BaseTool 适配器 |
| `todo/tracker.go` | 运行时 Todo 跟踪器 |
| `todo/types.go` | Todo 类型定义 |
| `todo/context.go` | Todo 上下文 |
| `deps/container.go` | DI 容器（ToolRegistry/SkillCatalog 等） |
| `mcp/server.go` | MCP 服务器（Worker 运行时引导） |
| `mcp/router.go` | MCP 路由 |
| `mcp/auth.go` | MCP 认证 |

#### `backend/internal/api/` — HTTP API 层

| 文件/目录 | 说明 |
|-----------|------|
| `router.go` | `SetupRouter()` — 主路由设置（GitHub/GitLab/WS/Worker/DA/LLM/Session/Project/Swagger） |
| `connectors/connector.go` | `Connector` 接口（`ChannelCode()` / `RegisterRoutes()`） |
| `connectors/github/github.go` | GitHub 连接器（Webhook + OAuth） |
| `connectors/github/webhook.go` | Webhook 签名验证与事件路由 |
| `connectors/github/converter.go` | GitHub 事件 → Leros 事件转换 |
| `connectors/github/client.go` | GitHub API 客户端 |
| `connectors/github/events.go` | GitHub 事件处理 |
| `connectors/github/types.go` | GitHub 连接器类型 |
| `connectors/gitlab/gitlab.go` | GitLab 连接器（桩代码） |
| `connectors/gitlab/converter.go` | GitLab 事件转换 |
| `connectors/gitlab/types.go` | GitLab 连接器类型 |
| `connectors/wework/app.go` | 企业微信连接器（桩代码） |
| `handler/digital_assistant_handler.go` | 数字员工 CRUD 处理器 |
| `handler/session_handler.go` | 会话和消息 CRUD + 流式端点 |
| `handler/llm_model_handler.go` | LLM 模型管理端点 |
| `handler/project_handler.go` | 项目管理端点 |
| `handler/task_handler.go` | 任务管理端点 |
| `handler/work_handler.go` | 工作流管理端点 |
| `handler/artifact_handler.go` | 产物管理端点 |
| `middleware/identify.go` | 用户身份提取中间件 |
| `middleware/request_context.go` | 请求上下文中间件 |
| `auth/auth.go` | OAuth 流程编排 |
| `auth/resolver.go` | 账户解析器 |
| `auth/service.go` | 认证服务 |
| `auth/store.go` | 认证存储接口 |
| `auth/memory_store.go` | 内存认证存储 |
| `auth/types.go` | 认证类型 |
| `auth/constants.go` | 认证常量 |
| `auth/identity.go` | 身份相关逻辑 |
| `contract/` | 服务契约 DTO（DigitalAssistant/Session/LLMModel/Project/Task/Work/Artifact/ThirdAuth） |
| `dto/response.go` | 标准 API 响应格式 |
| `dto/session.go` | 会话相关 DTO |
| `dto/digital_assistant.go` | 数字员工 DTO |
| `dto/code.go` | 代码相关 DTO |

#### `backend/internal/service/` — 业务服务

| 文件 | 说明 |
|------|------|
| `digital_assistant_service.go` | 数字员工 CRUD，触发 Worker 调度 |
| `session_service.go` | 会话生命周期管理（创建/消息/流式/完成），通过 NATS 分发任务 |
| `llm_model_service.go` | LLM 模型配置 CRUD |
| `project_service.go` | 项目 CRUD |
| `task_service.go` | 任务 CRUD |
| `work_service.go` | 工作流管理 |
| `artifact_service.go` | 产物管理 |
| `session_event_projector.go` | 会话事件投射到消息表 |
| `assistant_inferrer.go` | 员工解析 |
| `message_poster.go` | 消息投递 |
| `utils.go` | 服务层工具函数 |

#### `backend/internal/infra/` — 基础设施

| 文件/目录 | 说明 |
|-----------|------|
| `mq/bus.go` | `Publisher` / `Subscriber` / `EventBus` 接口 |
| `mq/nats.go` | NATS JetStream 实现 |
| `mq/std.go` | 非 JetStream 实现 |
| `db/database.go` | `InitDB()` — 数据库初始化 + 自动迁移 + 种子数据 |
| `db/session_dao.go` | 会话 DAO |
| `db/session_message_dao.go` | 会话消息 DAO |
| `db/digital_assistant_dao.go` | 数字员工 DAO |
| `db/llm_model_dao.go` | LLM 模型 DAO |
| `db/project_dao.go` | 项目 DAO |
| `db/task_dao.go` | 任务 DAO |
| `db/user_org_dao.go` | 用户组织 DAO |
| `db/artifact_dao.go` | 产物 DAO |
| `providers/github/` | GitHub OAuth 提供商实现 |
| `websocket/connector.go` | WebSocket 连接器（连接管理/广播/读写泵） |
| `websocket/manager.go` | 连接管理器 |
| `websocket/types.go` | WebSocket 消息类型 |

#### `backend/internal/worker/` — Worker 系统

| 文件/目录 | 说明 |
|-----------|------|
| `worker.go` | Worker 类型别名 |
| `scheduler.go` | `WorkerScheduler` 接口 + `WorkerSpec`/`WorkerInstance` |
| `client/worker_client.go` | Worker 客户端定义 |
| `client/ws_client.go` | 基于 WebSocket 的 Worker 客户端 |
| `server/server.go` | Worker 服务端（管理 Worker 进程） |
| `server/conn.go` | Worker 连接管理 |
| `router/router.go` | Worker 路由 |
| `scheduler/process_scheduler.go` | `ProcessScheduler` — 通过 `exec.Command` 启动 Worker 进程 |
| `scheduler/dockercli_scheduler.go` | `DockerCLIScheduler` — 通过 Docker CLI 调度 |
| `taskconsumer/consumer.go` | `Consumer` — 订阅 NATS Worker 任务主题，分发到 Agent Runner |
| `taskconsumer/stream_sink.go` | `MQStreamSink` — 流事件 MQ 转发 |
| `taskconsumer/mapper.go` | 任务映射器 |
| `protocol/envelope.go` | 领域消息协议 |
| `protocol/stream.go` | 协议流 |
| `protocol/task.go` | 任务协议 |
| `wsproto/types.go` | WebSocket 协议类型 |
| `identity/profile.go` | Worker 身份配置文件 |

#### `backend/internal/eventengine/` — 事件引擎

| 文件 | 说明 |
|------|------|
| `orchestrator.go` | `Orchestrator` — 订阅 NATS 交互事件，转换为 Agent 输入并分发。默认处理器：issue_comment/pull_request/push |

#### `backend/internal/skill/` — 技能系统

| 文件 | 说明 |
|------|------|
| `catalog/catalog.go` | `Catalog` — 基于文件系统的技能索引（扫描 SKILL.md） |
| `catalog/types.go` | 技能 Manifest 类型 |
| `catalog/provider.go` | `CatalogProvider` 接口 |
| `manage/manager.go` | 技能生命周期管理 |
| `manage/post_processor.go` | 技能后处理器 |
| `store/store.go` | 技能元数据持久化 |

#### `backend/internal/memory/local/` — 本地记忆存储

| 文件 | 说明 |
|------|------|
| `store.go` | 本地文件记忆存储 |

#### `backend/internal/runnable/` — 后台可运行任务

| 文件 | 说明 |
|------|------|
| `session_completed.go` | Agent 完成后标记会话为完成 |
| `session_title_handler.go` | 自动生成会话标题 |

#### `backend/internal/cli/` — CLI 内部实现

| 文件 | 说明 |
|------|------|
| `chat.go` | CLI 聊天交互命令 |
| `lister.go` | 列表功能（项目/任务/会话） |

#### `backend/internal/modelrouter/` — LLM 模型路由

| 文件 | 说明 |
|------|------|
| `handler.go` | 模型路由处理器 |
| `protocol.go` | 协议转换（统一上游请求/响应格式） |
| `resolver.go` | 上游提供商 Resolver（按模型编码路由） |
| `upstream.go` | 上游连接管理 |
| `converter.go` | 请求/响应转换器 |
| `stream_types.go` | 流式响应类型 |
| `ir.go` | 中间表示层 |
| `utils.go` | 工具函数 |
| `anthropic.go` | Anthropic 协议适配 |
| `openai_chat.go` | OpenAI Chat 协议适配 |
| `openai_responses.go` | OpenAI Responses 协议适配 |

#### `backend/internal/workspace/` — 工作空间管理

| 文件 | 说明 |
|------|------|
| `workspace.go` | 工作空间定义与隔离 |
| `artifacts.go` | 产物收集与清理 |
| `server_paths.go` | 服务端路径解析 |

### `docs/` — 文档

| 文件 | 说明 |
|------|------|
| `ARCHITECTURE.md` | AI OS 架构设计（三平面模型） |
| `DESIGN_PHILOSOPHY.md` | 核心设计理念 |
| `PRD.md` | 产品需求文档 |
| `SYSTEM_DESIGN.md` | 系统架构设计 |
| `TECH_DESIGN.md` | 技术设计 |
| `ARCHITECTURE_BACKEND.md` | 后端架构 |
| `ARCHITECTURE_MQ_SUBJECT.md` | 消息队列主题架构 |
| `PLANNING.md` | 路线图规划 |
| `TODO.md` | 后端开发 TODO |
| `PROJECT_STRUCTURE.md` | 本文件 |
| `GITHUB_AUTH_SETUP.md` | GitHub OAuth 配置 |
| `GITHUB_WEBHOOK_TROUBLESHOOTING.md` | Webhook 排障 |
| `PR_EVENT_FLOW.md` | PR 事件流程验证 |
| `TROUBLESHOOTING.md` | 常见问题排障 |
| `AGENT_WORKSPACE_ARTIFACT_DESIGN.md` | Agent 工作空间产物设计 |
| `DESIGN_CODER.md` | Coder 设计文档 |
| `AUTH_FOUNDATION_PHASE_TASKS.md` | 认证基础阶段任务 |
| `frontend/` | 前端架构文档 |
| `swagger/` | 自动生成的 Swagger 文档 |

### `deployments/` — 部署配置

| 文件 | 说明 |
|------|------|
| `build/Dockerfile.leros` | 多阶段 Docker 构建（Go 1.24 + Ubuntu 24.04） |
| `env/docker-compose.yml` | 完整栈（PostgreSQL 17 + NATS + Leros Server + Leros Worker） |
| `env/init.sql` | 数据库初始化 SQL |
| `env/check-services.sh` | 服务健康检查脚本 |
| `dev/` | 开发环境（脚本/配置/docker-compose） |

### `frontend/` — 前端应用

| 目录 | 说明 |
|------|------|
| `apps/web/` | Next.js Web 应用（App Router, Tailwind；`app/(shell)` 承载应用壳路由） |
| `apps/desktop/` | Electron 桌面应用（React Router 维护内部页面位置） |
| `packages/ui/` | 共享 UI 组件库（shadcn/ui 40+ 组件 + hooks + lib） |
| `packages/store/` | Zustand 状态管理（chat/digital assistant/topic/layout） |
| `packages/app-ui/` | 应用级 UI 组件（chat/assistant/layout/input；通过 AppNavigation 接收平台导航） |
| `packages/styles/` | 双端共享全局样式入口（Tailwind/shadcn/token/base + app shell styles） |
| `packages/tsconfig/` | TypeScript 配置共享包 |
| `packages/biome/` | Biome 配置共享包 |

---

## 快速索引（按任务场景）

### 我要加一个新的 HTTP API
1. `types/` — 新增领域类型（如新 model struct）
2. `internal/infra/db/` — 新增 DAO
3. `internal/service/` — 新增业务服务
4. `internal/api/handler/` — 新增 HTTP Handler
5. `internal/api/contract/` — 新增请求/响应 DTO
6. `internal/api/router.go` — 注册路由

### 我要加一个新的 Agent 运行时
1. `internal/agent/runner.go` — 检查 `Runner` 接口
2. `internal/runtime/drivers/<runtime_name>/` — 实现新 Runner
3. `internal/agent/router.go` — 注册新运行时
4. `internal/runtime/service.go` — 在 Service 中初始化

### 我要加一个新的 Tool
1. `tools/tool.go` — 检查 `Tool` 接口
2. `tools/<tool_name>/` — 实现新 Tool
3. `tools/registry.go` — 注册（或通过 DI 注入）

### 我要加一个新的事件处理器
1. `pkg/event/topic.go` — 定义新 Topic 常量（如需要）
2. `internal/eventengine/orchestrator.go` — 注册 Handler + 实现处理逻辑
3. `types/event.go` — 定义新 Event 类型（如需要）

### 我要加一个新的渠道连接器
1. `internal/api/connectors/connector.go` — 实现 `Connector` 接口
2. `internal/api/connectors/<channel>/` — 路由注册 + 事件转换
3. `internal/api/router.go` — 在 `SetupRouter` 中注册
4. `config/<channel>.go` — 添加配置（如需要）

### 我要加一个新的外部 CLI 引擎
1. `engines/engine.go` — 实现 `Engine` 接口
2. `engines/<engine_name>/adapter.go` — 适配器
3. `engines/<engine_name>/invoker.go` — 进程调用器
4. `engines/builtin/factory.go` — 在工厂中注册
5. `cmd/leros/worker.go` — 添加 Worker 子命令（如需要）
