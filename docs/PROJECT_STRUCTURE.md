# Leros 项目结构与文件索引

本文档用于快速定位 Leros 代码库中的主要目录、边界和常见开发入口。内容按当前仓库结构整理，处理不熟悉任务时建议先从“快速索引”定位到对应层级，再参考相邻实现。

## 项目概览

Leros 是一个企业级数字员工操作系统，后端基于 Golang，前端采用 pnpm + Turborepo monorepo。当前核心架构按职责分为：

- 控制平面：Gin HTTP 服务，负责 UI API、认证、会话、项目、任务、数字员工、模型配置和渠道入口。
- 事件与消息平面：NATS JetStream，负责外部事件、Worker 任务和运行流事件的解耦。
- 工作平面：Worker、Agent Runtime、外部 CLI 引擎和工具系统，负责实际执行任务。

Go Module: `github.com/insmtx/Leros` | Go 1.24

### 核心依赖

| 组件 | 用途 |
|------|------|
| Gin | HTTP API 和 Webhook 路由 |
| Cobra | `leros` CLI 命令入口 |
| GORM + PostgreSQL | 数据访问与模型迁移 |
| NATS JetStream | 消息队列、任务分发、运行事件流 |
| CloudWeGo Eino | 原生 Agent/LLM 运行时 |
| gorilla/websocket | WebSocket 连接与 Worker 通信 |
| MCP-Go | Worker 侧 MCP 服务 |
| swaggo | Swagger 文档生成 |
| pnpm + Turborepo | 前端 monorepo 构建与任务编排 |

### 核心抽象接口

| 接口 | 位置 | 职责 |
|------|------|------|
| `agent.Runtime` | `backend/agent/runtime.go` | 业务无关的单次执行边界 |
| `agent.RuntimeResolver` | `backend/agent/runtime.go` | Runtime 解析与默认选择 |
| `agent.Tool` | `backend/agent/tool.go` | Runtime 可调用工具的纯执行契约 |
| `tools.Tool` | `backend/tools/tool.go` | Worker 可调用工具的最小接口 |
| `mq.EventBus` | `backend/internal/infra/mq/bus.go` | 事件总线发布与订阅 |
| `Connector` | `backend/internal/api/connectors/connector.go` | 外部渠道连接器 |
| `skill/catalog.CatalogProvider` | `backend/internal/skill/catalog/provider.go` | 技能目录提供者 |

### 主数据流

```text
UI Client -> REST/WS -> Server -> NATS WorkerCommand
                                   |
                                   v
Handler -> RunCoordinator -> assistant.Service -> agent.Executor -> Runtime
   ^                                                               |
   |---------------- RunEvent / SSE / Session Message --------------|

GitHub/GitLab Webhook -> Connector -> NATS -> Worker Command
```

## 根目录

```text
/
├── AGENTS.md                 # AI Agent 开发指南和项目约束
├── CONTRIBUTING.md           # 贡献指南
├── Makefile                  # 构建、运行、Swagger 等命令
├── README.md / README_en.md  # 项目说明
├── config.example.yaml       # 完整示例配置
├── minimal-config.yaml       # 最小启动配置
├── go.mod / go.sum           # Go 模块定义
├── backend/                  # Go 后端源码
├── frontend/                 # 前端 monorepo
├── docs/                     # 架构、设计、排障与生成文档
├── deployments/              # Docker、compose、初始化脚本
├── bundles/                  # 构建产物，已忽略
└── logs/                     # 本地运行日志，已忽略
```

## `backend/` - Go 后端

### `backend/cmd/leros/` - 进程入口

该目录是进程生命周期边界，允许 Cobra 命令注册、服务启动、信号等待和 `log.Fatal`。

| 文件 | 说明 |
|------|------|
| `main.go` | Cobra 根命令和全局日志配置 |
| `server.go` | `leros server`，启动 HTTP 服务、DB、NATS、Worker Server、路由 |
| `worker.go` | `leros worker`，启动 Worker 及 `claude`、`codex` 子命令 |
| `chat.go` | 本地 CLI 聊天调试 |
| `project.go` | Project CLI 调试命令 |
| `session.go` | Session CLI 调试命令 |
| `task.go` | Task CLI 调试命令 |

### `backend/config/` - 配置类型

| 文件 | 说明 |
|------|------|
| `config.go` | 主配置结构，包含 Server、NATS、DB、LLM、Scheduler 等 |
| `worker.go` | Worker 进程配置 |
| `scheduler.go` | Worker 调度配置 |
| `github.go` | GitHub App 配置 |
| `gitlab.go` | GitLab 配置 |

### `backend/types/` - 共享领域模型

该目录主要放跨层共享的 GORM 模型、枚举和表名常量，不放业务逻辑。

| 文件 | 核心内容 |
|------|----------|
| `digital_assistant.go` | `DigitalAssistant` |
| `session.go` | `Session`、`SessionMessage` |
| `task.go` | `Task` |
| `project.go` | `Project`、`ProjectMember` |
| `artifact.go` | `Artifact` |
| `llm_model.go` | `LLMModel` |
| `skill.go` | `Skill` |
| `skill_registry.go` | `SkillRegistry` |
| `skill_execution_log.go` | 技能执行审计日志 |
| `user.go` | `User` |
| `organization.go` | `Organization` |
| `user_org.go` | 用户与组织关系 |
| `auth.go` | 第三方账户和认证相关模型 |
| `event.go` | 持久化事件模型 |
| `constants.go` | 类型安全常量和枚举 |
| `tables.go` | 数据库表名常量 |
| `util.go` | 类型辅助函数 |

### `backend/internal/api/` - HTTP API 层

| 路径 | 说明 |
|------|------|
| `router.go` | 主路由组装，注册 API、连接器、WS、Swagger、Worker 路由 |
| `handler/` | HTTP Handler：数字员工、会话、任务、项目、模型、产物、工作流、认证、用户、组织 |
| `contract/` | API 请求/响应契约 DTO，按资源拆分，同时包含 `_type.go` 类型定义 |
| `dto/` | 通用 API 响应和部分历史 DTO |
| `middleware/` | 身份识别、请求上下文等中间件 |
| `auth/` | OAuth 编排、认证服务、账户解析、认证存储接口和内存实现 |
| `connectors/github/` | GitHub Webhook、OAuth、事件转换和 API Client |
| `connectors/gitlab/` | GitLab 连接器与事件转换 |
| `connectors/wework/` | 企业微信连接器桩代码 |

### `backend/internal/service/` - 业务服务层

| 文件 | 说明 |
|------|------|
| `digital_assistant_service.go` | 数字员工 CRUD 和调度触发 |
| `session_service.go` | 会话生命周期、消息创建、流式事件处理、任务分发 |
| `session_event_projector.go` | 运行事件投射为会话消息 |
| `message_poster.go` | 消息投递抽象 |
| `assistant_inferrer.go` | 数字员工解析 |
| `task_service.go` | 任务 CRUD |
| `project_service.go` | 项目 CRUD |
| `work_service.go` | 工作流管理 |
| `artifact_service.go` | 产物管理 |
| `llm_model_service.go` | LLM 模型配置管理 |
| `auth_service.go` | 认证业务服务 |
| `user_service.go` | 用户服务 |
| `org_service.go` | 组织服务 |
| `utils.go` | 服务层辅助函数 |

### `backend/internal/infra/` - 基础设施

| 路径 | 说明 |
|------|------|
| `mq/bus.go` | `Publisher`、`Subscriber`、`EventBus` 接口 |
| `mq/nats.go` | NATS JetStream 实现 |
| `mq/std.go` | 标准 NATS 实现 |
| `db/database.go` | DB 初始化、自动迁移和种子数据 |
| `db/*_dao.go` | DAO：会话、消息、数字员工、模型、项目、任务、产物、认证、用户、组织等 |
| `providers/github/` | GitHub OAuth Provider、Client Factory、Resolver |
| `websocket/` | WebSocket 连接器、连接管理器和消息类型 |

### `backend/agent/` - 独立 Agent 执行层

| 路径/文件 | 说明 |
|------|------|
| `runtime.go` | 唯一 `ExecutionRequest`、`ExecutionResult` 和 `Runtime` |
| `executor.go` | Runtime 解析与 `execution.*` 生命周期 |
| `registry.go` | 四个 Runtime 的注册和默认选择 |
| `result.go` | 唯一 `agent.Event` 信封、Usage、ToolCallRecord |
| `tool.go` | 强类型 Tool、approval、question |
| `runtime/native/` | Eino 原生 Runtime |
| `runtime/claude/` | Claude Code Runtime |
| `runtime/codex/` | Codex Runtime |
| `runtime/opencode/` | OpenCode Runtime |
| `runtime/externalcli/` | CLI 进程、provider session 和事件消费设施 |
| `runtime/provider/` | CLI provider 的内部进程协议 |
| `runtime/events/` | 活动 payload、构造器、Sink 和 Emitter |
| `runtime/todo/` | 执行期 Todo 能力 |

### `backend/internal/assistant/` - Assistant 业务包装层

| 路径/文件 | 说明 |
|------|------|
| `service.go` | 唯一业务 Run 生命周期和终态 |
| `prepared_run.go` | 业务快照、WorkspacePreparation、ExecutionRequest |
| `preparer_impl.go` | Workspace、Memory、Skill prompt、Tool 与模型准备 |
| `journal.go` | event、payload、usage、tool call 和统计归档 |
| `finalizer_impl.go` | Artifact、Git 和业务结果 |
| `context/` | Assistant 业务上下文构建 |
| `bootstrap/` | Runtime 和 Skill link 装配 |

### `backend/internal/worker/` - Worker 系统

| 路径 | 说明 |
|------|------|
| `scheduler.go` | `WorkerScheduler` 接口、`WorkerSpec`、`WorkerInstance` |
| `scheduler/process_scheduler.go` | 通过本地进程启动 Worker |
| `scheduler/dockercli_scheduler.go` | 通过 Docker CLI 调度 Worker |
| `server/` | Worker 服务端和连接管理 |
| `client/` | Worker Client 和 WebSocket Client |
| `router/` | Worker 路由 |
| `command/` | 统一 Worker 命令分发器与各 lane handler（run/interaction/skill） |
| `run/` | RunCoordinator：debounce、并发、Session 串行、取消、Close |
| `eventpub/` | `agent.Event` 到 NATS `run.stream` / `run.state` |
| `mcp/` | Worker 侧 MCP Server、Router 与实例级 token |
| `identity/` | Worker 身份配置 |
| `wsproto/` | Worker WebSocket 协议类型 |

### `backend/internal/skill/` - 技能系统

| 路径 | 说明 |
|------|------|
| `catalog/` | 文件系统技能目录扫描、Manifest 类型和 Provider 接口 |
| `manage/` | 技能安装、卸载、事件处理和生命周期管理 |
| `store/` | 技能元数据持久化 |

### `backend/internal/workspace/` - 工作空间管理

| 文件 | 说明 |
|------|------|
| `workspace.go` | 工作空间定义与路径隔离 |
| `artifacts.go` | 产物收集 |
| `artifact_storage_file.go` | 文件产物存储实现 |
| `scanner.go` | 工作空间扫描 |
| `server_paths.go` | 服务端路径解析 |

### 其他 `backend/internal/` 目录

| 路径 | 说明 |
|------|------|
| `cli/` | CLI 命令的库实现，不接管进程生命周期 |
| `memory/local/` | 本地文件记忆存储 |
| `modelrouter/` | Worker LLM 模型代理和 SSE 转发 |
| `runnable/` | 后台可运行任务，如会话完成、标题生成 |

### `backend/tools/` - 工具系统

| 路径/文件 | 说明 |
|-----------|------|
| `tool.go` | `Tool`、`BaseTool`、Schema、ToolContext |
| `registry.go` | 线程安全工具注册表 |
| `memory/` | 记忆工具 |
| `skill_use/` | 技能查询和使用工具 |
| `skill_manage/` | 技能安装/卸载管理工具 |
| `todo/` | Todo/计划工具 |
| `artifact_declare/` | 产物声明工具 |
| `node/` | 文件读写、Shell、工作空间工具 |
| `node/security/` | 工作空间限制、审批、环境隔离、写入禁止规则 |
| `node/util/` | Node 工具辅助函数 |

### `backend/pkg/` - 可复用共享包

| 路径 | 说明 |
|------|------|
| `dm/` | Worker 任务、消息流 Subject 和 Consumer 名称生成 |
| `eino/` | Eino ChatModel、Flow、Message、Tool 适配 |
| `event/` | 跨模块事件结构和 NATS Topic 常量 |
| `leros/` | Leros home、技能目录等路径解析 |
| `llmprotocol/` | OpenAI Chat/Responses、Anthropic、Gemini 协议中间表示和转换 |
| `seqtracker/` | 序列跟踪器 |
| `utils/` | 去抖器和值回退辅助 |
| `workerpool/` | Worker Pool 工具 |

### `backend/prompts/` - 提示词模板系统

| 文件 | 说明 |
|------|------|
| `prompt.go` | 模板管理器和全局注册表 |
| `executor_eino.go` | 基于 Eino LLM 的提示词执行器 |
| `prompt_agent.go` | 默认 Agent 系统提示词 |
| `prompt_llm.go` | LLM 相关提示词 |
| `prompt_session.go` | 会话提示词 |
| `prompt_event.go` | 事件提示词 |
| `key.go` | 模板 Key 常量 |
| `option.go` | `RunOption` 函数式选项 |

### `backend/skills/` - 内置技能

| 路径 | 说明 |
|------|------|
| `anysearch/` | AnySearch 技能定义和 Python/Shell/Node/PowerShell CLI 封装 |
| `create-word-doc/` | Word 文档生成技能，包含 assets、data、references、scripts |
| `government-recognition-policy/` | 政府认定类政策文档技能，包含资产、清单、参考资料、脚本 |

### `backend/tests/`

后端集成或跨包测试入口。新增测试优先放在被测包旁边，只有需要跨多个包或组件协作时才放到这里。

## `frontend/` - 前端 monorepo

根包使用 pnpm 10 和 Turborepo。常用脚本在 `frontend/package.json`：

| 脚本 | 说明 |
|------|------|
| `pnpm dev:web` | 启动 Web 应用 |
| `pnpm dev:desktop` | 启动桌面应用开发模式 |
| `pnpm build` | 构建所有包 |
| `pnpm typecheck` | 类型检查 |
| `pnpm test` | 测试 |
| `pnpm lint` | 代码检查 |
| `pnpm ui:add` | 在 `packages/ui` 中添加 shadcn 组件 |

| 路径 | 说明 |
|------|------|
| `apps/web/` | Next.js Web 应用，App Router，`app/(shell)` 承载主应用壳 |
| `apps/web/components/` | Web 应用本地组件 |
| `apps/web/public/` | Web 静态资源 |
| `apps/desktop/` | Electron 桌面应用 |
| `apps/desktop/src/main/` | Electron main 进程 |
| `apps/desktop/src/preload/` | Electron preload |
| `apps/desktop/src/renderer/` | 桌面端 React 渲染层 |
| `packages/app-ui/` | 应用级 UI：auth、chat、digitalAssistant、input、layout |
| `packages/ui/` | 共享基础 UI、hooks、lib、styles |
| `packages/store/` | 状态管理、API、mocks、slices、types、utils |
| `packages/styles/` | 双端共享样式入口 |
| `packages/tsconfig/` | 共享 TypeScript 配置 |
| `packages/biome/` | 共享 Biome 配置 |

## `docs/` - 文档

| 文件/目录 | 说明 |
|-----------|------|
| `ARCHITECTURE.md` | AI OS 架构设计 |
| `ARCHITECTURE_BACKEND.md` | 后端架构 |
| `ARCHITECTURE_MQ_SUBJECT.md` | MQ Subject 架构 |
| `SYSTEM_DESIGN.md` | 系统架构设计 |
| `TECH_DESIGN.md` | 技术设计 |
| `PRD.md` | 产品需求文档 |
| `DESIGN_PHILOSOPHY.md` | 设计理念 |
| `DESIGN_CODER.md` | Coder 设计 |
| `AGENT_WORKSPACE_ARTIFACT_DESIGN.md` | Agent 工作空间与产物设计 |
| `AUTH_FOUNDATION_PHASE_TASKS.md` | 认证基础阶段任务 |
| `PLANNING.md` | 路线图规划 |
| `TODO.md` | 后端 TODO |
| `ISSUE_LABELS.md` | Issue Label 约定 |
| `GITHUB_AUTH_SETUP.md` | GitHub OAuth 配置 |
| `GITHUB_WEBHOOK_TROUBLESHOOTING.md` | GitHub Webhook 排障 |
| `PR_EVENT_FLOW.md` | PR 事件流程验证 |
| `TROUBLESHOOTING.md` | 常见问题排障 |
| `frontend/` | 前端架构、通信、状态管理、布局、工程规范等文档 |
| `swagger/` | `make swagger` 生成的 Swagger Go/JSON/YAML 文件 |

## `deployments/` - 部署配置

| 路径 | 说明 |
|------|------|
| `build/Dockerfile.leros` | Leros 多阶段 Docker 构建 |
| `env/docker-compose.yml` | PostgreSQL、NATS、Leros Server、Leros Worker 完整栈 |
| `env/init.sql` | 数据库初始化 SQL |
| `env/check-services.sh` | 服务健康检查脚本 |
| `dev/` | 开发环境脚本、配置和 compose 文件 |

## 快速索引

### 新增 HTTP API

1. `backend/types/`：确认或新增共享领域模型。
2. `backend/internal/infra/db/`：新增 DAO。
3. `backend/internal/service/`：新增业务服务。
4. `backend/internal/api/contract/`：新增请求/响应契约。
5. `backend/internal/api/handler/`：新增 HTTP Handler。
6. `backend/internal/api/router.go`：注册路由。

### 新增认证、用户或组织能力

1. `backend/types/auth.go`、`user.go`、`organization.go`：确认模型。
2. `backend/internal/infra/db/*_dao.go`：补齐持久化。
3. `backend/internal/service/auth_service.go`、`user_service.go`、`org_service.go`：实现业务逻辑。
4. `backend/internal/api/auth/`：如涉及 OAuth 或第三方账户解析，先复用现有结构。
5. `backend/internal/api/handler/` 和 `contract/`：补齐 API。

### 新增渠道连接器

1. `backend/internal/api/connectors/connector.go`：实现 `Connector` 接口。
2. `backend/internal/api/connectors/<channel>/`：路由注册、签名校验、事件转换。
3. `backend/pkg/event/topic.go`：如需新 Topic，先集中定义。
4. `backend/internal/api/router.go`：注册连接器。
5. `backend/config/<channel>.go`：新增配置。

### 新增事件处理器

1. `backend/internal/api/connectors/<channel>/`：将外部事件标准化。
2. `backend/pkg/messaging/`：确认或新增强类型 wire contract。
3. `backend/internal/worker/command/<lane>/`：实现 Command adapter。
4. `backend/cmd/leros/`：在 composition root 注册依赖。

### 新增 Agent 运行时

1. `backend/agent/runtime.go`：确认纯 `Runtime` 契约。
2. `backend/agent/runtime/<runtime_name>/`：直接实现 `agent.Runtime`。
3. `backend/agent/runtime_contract_test.go`：加入共用 contract suite。
4. `backend/cmd/leros/worker.go`：构造并注册 Runtime。

### 新增外部 CLI 引擎

1. `backend/agent/runtime/provider/`：只在确有共性时扩展 provider 进程协议。
2. `backend/agent/runtime/externalcli/`：复用通用进程、会话与事件消费设施。
3. `backend/agent/runtime/<runtime_name>/`：实现具体 Runtime、解析器和 responder。
4. `backend/cmd/leros/worker.go`：实例化并注册，禁止增加全局默认实例。

### 新增 Tool

1. `backend/tools/tool.go`：确认 `Tool` 接口和 Schema 结构。
2. `backend/tools/<tool_name>/`：实现工具。
3. `backend/tools/<tool_name>/register.go`：提供注册入口。
4. `backend/internal/assistant/bootstrap/builtin/`：接入 Tool 注册。
5. `backend/internal/assistant/tool_adapter.go`：保持业务 Tool 到 `agent.Tool` 的边界转换。

### 新增 Skill

1. `backend/skills/<skill_name>/SKILL.md`：新增技能 Manifest。
2. `backend/internal/skill/catalog/`：确认 Manifest 解析是否支持新增字段。
3. `backend/internal/skill/manage/`：如涉及安装/卸载流程，复用 Manager 和事件处理。
4. `backend/tools/skill_use/`、`skill_manage/`：如需 Agent 调用能力，补齐工具行为。

### 新增 Worker 调度能力

1. `backend/internal/worker/scheduler.go`：确认调度接口。
2. `backend/internal/worker/scheduler/`：实现具体调度器。
3. `backend/internal/worker/server/`：如涉及连接生命周期，补齐服务端管理。
4. `backend/internal/worker/command/run/`：如涉及任务消费，补齐映射和流事件转发。
5. `backend/config/scheduler.go`、`worker.go`：补齐配置。

### 新增工作空间或产物能力

1. `backend/internal/workspace/`：实现路径、扫描、存储或产物收集逻辑。
2. `backend/internal/worker/command/run/`：确认运行时工作空间注入。
3. `backend/internal/service/artifact_service.go`：如涉及 API 产物管理，接入服务层。
4. `backend/internal/api/handler/artifact_handler.go`：暴露端点。

### 新增前端页面或共享组件

1. `frontend/apps/web/app/(shell)/`：新增 Web 应用壳内页面。
2. `frontend/packages/app-ui/`：优先沉淀应用级复用组件。
3. `frontend/packages/ui/`：仅放跨业务的基础 UI 组件。
4. `frontend/packages/store/`：新增 API、状态 slice、类型和 mocks。
5. `docs/frontend/`：涉及架构或状态约定时同步文档。

## 分层边界提醒

| 层级 | 路径 | 允许 | 禁止 |
|------|------|------|------|
| 进程入口 | `backend/cmd/leros/` | Cobra、进程生命周期、信号处理、`log.Fatal` | 业务逻辑 |
| 业务与基础设施库 | `backend/internal/*` | 业务逻辑、运行时、DAO、连接器，通过 `error` 向上传递失败 | `os.Exit()`、`lifecycle.Std()`、`log.Fatal`、`panic`、Cobra 依赖 |
| 共享类型 | `backend/types/`、`backend/config/` | 领域类型、配置结构、常量 | 业务逻辑、外部系统调用 |
| 可复用包 | `backend/pkg/` | 无业务状态的共享工具和协议转换 | 依赖上层 `internal` 包 |

新增实现前先搜索已有参照，例如新增 Handler 先看 `backend/internal/api/handler/`，新增 DAO 先看 `backend/internal/infra/db/`，新增 Worker 能力先看 `backend/internal/worker/`。优先复用现有骨架和错误处理方式。
