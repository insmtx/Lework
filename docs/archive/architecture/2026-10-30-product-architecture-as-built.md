# Lework 产品架构（As-Built）

> 当前代码状态：`51ee998a`（2026-10-31，分支 `AIO`）之上叠加 **Leros → Lework 命名迁移**（工作区未提交）；Go module `github.com/insmtx/Lework`。
> 初版基线：`1458925a`（2026-10-30）。本版同步了 AIO 一体机、安全默认值收紧、无关模块剥离、M1 任务验收闭环与全仓命名迁移。
> 口径：**以当前代码为准**，不采信 README / `docs/` 中与实现不一致的描述；凡引用结论均给出文件路径或符号，便于复核。
> 规模：797 个 Go 文件 / 171k 行（含测试）、265 个 `_test.go`、41 个 AutoMigrate 模型、43 个表名常量、约 152 处路由注册、4 个 Agent Runtime、64 个 service 实现文件。

---

## 0. 一句话概括

Lework 是一个**面向独立开发者与小团队的 AI 队友协作平台**：以「项目 → 任务 → 会话」为协作载体，把任务派给 AI 队友，转成 worker 上的 **Agent Run**，在 Git 工作区里执行并产出文件，再通过 **NATS 事件 + POST-SSE** 把过程、产物、用量回写到控制面，最后由人验收或驳回。

当前形态可精确描述为：

> **单体控制面（server）+ 每 AI 队友一个执行单元（worker）+ 单节点 NATS JetStream + 单实例 PostgreSQL + Git/对象存储**。AIO 用 Docker Compose 把 PostgreSQL、NATS、server（内嵌 worker）、Web、Nginx 装进一台机器。

它不是工作流编排引擎，不是通用 Agent 开发框架，也不是已完成商业化的 SaaS：计费、知识库、多组织、多 Agent 并行等能力尚不在代码中。

### 0.1 本版相对 2026-10-30 快照的变更

| 变更 | 实际影响 | 证据 |
|---|---|---|
| **M1 任务验收闭环落地** | 任务从「记录单元」变成可委托、可执行、可验收的闭环：`assignee` 参与路由，普通任务 Run 成功后进入 `pending_acceptance`，人可通过或驳回 | `service/task_service.go`、`runnable/task_run_state_projector.go`、`TaskAcceptancePanel.tsx` |
| **AIO 一体机交付** | 一条命令启动 PG + NATS + server + 内置 worker + Web + Nginx；同源代理 `/v1` 与 SSE | `deployments/aio/` |
| **安全默认值收紧** | 开发免鉴权改为显式开关 + 回环地址；CORS 白名单；MCP 空 token 拒绝；生产环境强制管理员密码 | `middleware/identify.go`、`middleware/cors.go`、`worker/mcp/auth.go`、`seed/account_oss.go` |
| **无关模块剥离** | 删除 Payroll、未接线的 webhook 连接器、旧 WebSocket 管理端点、K8s 调度器、西电演示账号与资源库占位页 | 提交 `21285489`、`e6fd2af7`、`0f76e505`、`9aeb0689` |
| **CI 基线** | 后端 build/test/vet 与前端 typecheck/test 进入 CI | `.github/workflows/` |

---

## 1. 十条关键结论（供决策先读）

| # | 结论 | 直接影响 |
|---|---|---|
| 1 | **产品主链路是「消息即任务」，现在补上了验收**：`CreateInitialMessage` 一次性创建 Project → Task → Session → Message，然后派发一个 Run；Run 结束后任务进入待验收，而不是停在「执行完成」。 | Task 仍是执行聚合根，不是可编排的 DAG 节点；工作流需要新引擎。 |
| 2 | **Task 生命周期已闭环**：普通任务 `run.completed → pending_acceptance →（通过）completed /（驳回）in_progress + 返工消息`；cron 任务直接 `completed`；投影用 `ProjectionReceipt` 幂等（`runnable/task_run_state_projector.go`）。 | 任务看板与验收流程可信；取消/失败也会落到 `cancelled` / `failed`。 |
| 3 | **「多 Agent 并行调度」仍未实现**：一条消息只路由一个 assistant，`assistant_ids` 只用第一个（`worker_resolution.go:firstOrDefault`），一次 Run 只对应一个 worker/assistant。 | 对外叙事必须收敛；要做并行需新增 fan-out + 聚合模型。 |
| 4 | **AI 队友没有技能/模型/记忆的外键**：技能走**项目级**插件绑定，模型走**组织默认模型**，记忆走 prompt block / memory 表（且无生产写入方）。同一队友在不同项目能力不同。 | 无法表达「队友自带技能包」「队友专属模型」；产品语义需明确或补绑定表。 |
| 5 | **每个 AI 队友 1:1 一个 WorkerDeployment**（`WorkerDeployment.digital_assistant_id` 唯一）。成本模型 ≈ 队友数 × 常驻进程。 | 大规模组织成本高；需要池化/按需伸缩策略。 |
| 6 | **控制面与执行面只通过 NATS 解耦**；客户端实时通道只有两个 **POST-SSE**（`SessionEvents` / `GlobalEvents`），不是 WebSocket。旧版 `/ws/*` 管理端点已删除。 | 网关/CDN 必须支持 POST + 流式响应；不能直接用 `EventSource`。 |
| 7 | **多租户是应用层 `org_id` 隔离**，无外键、无 RLS、NATS 单凭据；且 `MaxOrgsPerUser=1`、`SwitchOrganization/CreateOrganization` 未实现。 | 当前是「单组织 SaaS/私有化」，不是真正的多组织平台。 |
| 8 | **商业化闭环缺失**：有 token 用量（`llm_history` / `SessionMessage.Usage`），没有单价、余额、配额、扣减、账单。 | 「成本追踪」只能做到「用量可观测」。 |
| 9 | **安全底线已收紧，但未清零**：开发免鉴权、CORS、MCP 空 token、默认管理员、Worker WS 端点已修；**模型 API Key 仍可逆落库**，`/v1/ops/workers/status` 只校验组织不校验管理员。 | 公网/私有化交付前仍需处理密钥与运维权限。 |
| 10 | **交付形态是单节点，并新增 AIO**：NATS/PG 单实例、server 单副本、后台循环无 leader 选举；`scheduler.mode=k8s` 已删除，只支持 `process` / `docker-cli`；AIO 用 Compose 把全栈装进一台机器。 | 高可用/水平扩展不是现状；Helm Chart 仍引用已删除的 k8s 模式，属于待清理资产。 |

---

## 2. 产品能力地图

状态图例：✅ 完整可用 ｜ 🟡 可用但有明显边界 ｜ 🟠 半成品/部分实现 ｜ ⛔ 未实现/仅占位

| 能力域 | 状态 | 实际入口 | 代码事实 |
|---|---|---|---|
| 邮箱/手机/密码登录、JWT、刷新令牌 | ✅ | `/v1/LoginByPassword` 等 | `adapter/account/oss/auth.go`；JWT 只带 `uin`，org 每请求反查 |
| 企业 IAM 认证（enterprise 构建） | 🟡 | 同一套 API | `adapter/account/enterprise/`；`-tags enterprise` 编译期切换 |
| 组织 / 成员 / 部门 CRUD | 🟡 | `/v1/CreateOrg`、`/v1/ListOrgMembers`… | 仅有 `RequireCallerOrg`，**无 handler 级权限守卫**；多组织未实现 |
| 项目（成员、插件、活动、文件、记忆） | ✅ | `/v1/CreateProject`… | `service/project_service.go`、`project_file_handler.go` |
| 任务 CRUD + 自动建任务 | ✅ | `/v1/CreateTask`、`CreateInitialMessage` | 支持标题/描述/预期产出/验收标准/assignee；状态随 Run 事件投影。注释仍称支持子任务，但 `Task` 无 `ParentTaskID` 字段，未形成编排 |
| 任务验收 | ✅ | `/v1/GetTaskDeliverables`、`/v1/AcceptTask`、`/v1/RejectTask` | 仅 `pending_acceptance` 可操作；通过写 `completed`，驳回写 `in_progress` 并发送返工消息；交付物聚合会话 artifact 与 artifact 类型项目文件 |
| 任务群聊（多真人 + AI 队友） | ✅ | `/v1/AddMessage` + `GlobalEvents` | 多真人发言、单 AI 回复；任务 assignee > 请求 `assistant_ids` > 项目默认 assistant |
| AI 队友（角色/模板/可见性/状态） | ✅ | `/v1/CreateDigitalAssistant`… | 1:1 WorkerDeployment；assignee 会绑定到项目资源并参与消息路由；无技能/模型绑定列 |
| 会话与消息 | ✅ | `/v1/CreateSession`、`GetSessionMessages` | 三种 Session：chat / task / project |
| 实时流 | ✅ | `POST /v1/SessionEvents`、`POST /v1/GlobalEvents` | POST-SSE，seq 单调、支持 replay |
| 技能 / 插件 / 插件市场 | ✅ | `/v1/plugins/*`、`/v1/plugin-marketplace/*` | `plugin` + `plugin_revision` + `plugin_revision_content` + 项目绑定 |
| MCP 连接器（平台/OAuth/绑定） | 🟡 | `/v1/plugins/mcp/*` | 平台模板 + OAuth + 运行时快照；未配置 token 时 MCP 端点直接拒绝 |
| Webhook 连接器（GitHub/GitLab/企微） | ⛔ | — | 已从主干删除；GitHub 实现曾存在但从未注册路由 |
| 自动化（cron / interval） | ✅ | `/v1/CreateAutomation`… | 自带项目「代数」重建；执行记录；Outbox 派发 |
| 模型管理（CRUD / 测试 / 默认） | ✅ | `/v1/CreateLLMModel`… | Provider 支持 OpenAI/Anthropic/Gemini/DeepSeek/custom |
| 每队友模型 / 运行时选择 | ⛔ | — | 模型取组织默认；runtime 取 worker 默认，服务端不设置 |
| 文件 / 产物 / 版本 | 🟡 | `/v1/projects/:id/files*`、artifact 事件 | Git diff 采集 + 预签名上传 + 项目文件版本；文件夹打包下载缺失 |
| 知识库 | ⛔ | 前端 `/knowledge` 已隐藏 | 仅 `types/constants.go` 有类型常量，无表、无服务 |
| 用量统计 | 🟡 | `/v1/llm/usage`、`llm_history` | 有 token/延迟/业务键；无 cost 列（已被迁移删除） |
| 计费 / 积分 / 配额 | ⛔ | — | 无表、无扣减、无账单；仅有 IAM 侧配额错误码 |
| RBAC | 🟡 | `/v1/BatchCheckPermission` + handler guard | project/task 完整；file/artifact/assistant 的细粒度执行点不足 |
| 审批 / 提问交互 | 🟡 | SessionEvents + `/v1/sessions/:id/approvals` | 四 runtime 语义不一致；native 无审批 |
| 多 Agent 并行 / 委派 | ⛔ | — | 无 fan-out、无聚合、无子 Run 编排 |
| 对外 MCP Server | 🟠 | worker `:8081/v1/mcp` | 仅 3 个工具，供 CLI runtime 反哺；不是产品级对外 MCP |
| CLI | ✅ | `lework chat/session/task/project/skill/automation/login` | `cmd/lework/*.go` |
| Web / 桌面端 | ✅ | Next.js 16.2.3 / Electron 39 | `frontend/apps/{web,desktop}` |
| AIO 一体机 | 🟡 | `deployments/aio/install.sh` | Compose 单机全栈 + Nginx 同源 Web/SSE；本机未做整机启动验证 |
| 首次启动向导 | ⛔ | — | 仍依赖 `LEWORK_ADMIN_EMAIL` / `LEWORK_ADMIN_PASSWORD` 环境变量 seed；无 `/setup` 向导 |
| 监控 / 指标 / 链路追踪 | 🟡 | 日志 + `operations/worker-status-api` | 有 HTTP 请求延迟直方图（`middleware/request_context.go` 的 `request_latency_seconds`）与结构化日志；无 Run/队列/业务级指标，无 OTel tracing 证据 |

---

## 3. 系统架构

### 3.1 全景图

```
┌────────────────────────────── 接入层 ──────────────────────────────┐
│  Web (Next.js 16, apps/web)   Desktop (Electron 39)   CLI (lework)  │
│  浏览器 / 桌面 HashRouter        cobra 子命令          worker 进程    │
└───────────────┬──────────────────────────┬────────────────────────┘
                │ HTTPS /v1 (Bearer)       │ POST + text/event-stream
                │                          │ (SessionEvents / GlobalEvents)
┌───────────────▼──────────────────────────▼────────────────────────┐
│                        控制面 server (`lework server`)              │
│  api/router.go                                                     │
│   ├─ middleware: CORS → Caller → RequestID → ClientUpdate → Logger │
│   ├─ handler/*（~152 路由：auth/session/project/task/assistant/...） │
│   └─ service/*（64 文件：项目/会话/任务/插件/自动化/权限/Worker 编排）│
│  后台循环：                                                         │
│   ├─ ReliableTaskDispatcher（Outbox，1s）                           │
│   ├─ SessionRunState/StreamProjector（run.* → DB/消息）             │
│   ├─ TaskRunStateProjector（run 终态 → 任务状态/结果摘要）          │
│   ├─ AutomationScheduler（planner 30s + dispatcher 15s）            │
│   ├─ WorkerDeploymentReconciler（10s，期望态对账）                   │
│   └─ llm.UsageConsumer / SkillPackageConsumer                      │
│  依赖：GORM → PostgreSQL；filestore；gitea；modelrouter/llm         │
└───────────────┬────────────────────────────────┬───────────────────┘
                │ NATS JetStream                  │ SQL
                │ org.<org>.worker.<w>.cmd.*      │
                │ org.<org>.session.<s>.run.*     │
┌───────────────▼────────────────────────────────┴───────────────────┐
│                       执行面 worker (`lework worker`)                │
│  command/dispatcher（4 lane: run/control/interaction/file）          │
│   → run/handler（admission → inbox(SQLite) → ack/nak）               │
│   → run/coordinator（debounce 1.5s / 并发槽 / 同会话串行 / 公平轮转） │
│   → agentrun.Service                                                │
│        prepare(workspace+skill+history+prompt+tools+MCP+model)      │
│        → Executor → Runtime(native|claude|codex|opencode)           │
│        → NodeEvent → Journal(seq) → NATS run.stream/run.state       │
│        → finalize(git diff → artifact 上传 → git commit/push)        │
│  HTTP :8081 = /health + /v1/mcp + /v1/{chat/completions,messages,...} │
└───────────────┬─────────────────────────────────────────────────────┘
                │
┌───────────────▼─────────────────────────────────────────────────────┐
│ 数据面：PostgreSQL · NATS JetStream · filestore(local|s3) · Git/Gitea│
│ 外部：LLM Provider · MCP Server(外部) · IAM(企业) · 飞书(仅反馈)      │
└─────────────────────────────────────────────────────────────────────┘
```

单机 AIO 在接入层前再加一层 **Nginx 同源网关**：`/` → Web，`/v1/*` → server，`/v1/SessionEvents` 与 `/v1/GlobalEvents` 关闭缓冲并保持长连接。这样浏览器只访问一个 Origin，不需要 CORS 放行。

### 3.2 分层职责

| 层 | 目录 | 职责 | 约束 |
|---|---|---|---|
| 接入 | `frontend/apps/*`、`backend/cmd/lework` | UI、CLI、进程生命周期 | 不含业务逻辑 |
| 控制面 | `backend/internal/api` | 路由、鉴权、DTO、handler | handler 不直连复杂业务；经 service |
| 业务域 | `backend/internal/service` | 项目/任务/会话/插件/自动化/权限/Worker 编排 | 可依赖 db 与 adapter，不可反向依赖 api |
| 事件投影 | `backend/internal/runnable` | 消费 `run.*`，投影会话、任务、自动化状态 | 幂等靠 `ProjectionReceipt`；不产生业务命令 |
| 执行内核 | `backend/agent` | Runtime 契约、NodeEvent、工具、交互 | 业务无关；禁止访问 NATS/DB；禁止发 `run.*` |
| 执行宿主 | `backend/internal/worker` | 命令接入、调度、prepare/finalize、事件发布 | 承接业务，调用 agent |
| 能力 | `backend/tools`、`backend/internal/skill`、`backend/internal/memory` | 工具、技能、记忆 | 通过 ToolContext 注入运行上下文 |
| 基础设施 | `backend/internal/infra/*`、`backend/pkg/*` | db / mq / filestore / git / messaging | 共享契约沉到 `pkg` |
| 领域类型 | `backend/types`、`backend/config` | 实体、常量、配置 | 无业务逻辑 |

### 3.3 进程与部署单元

| 单元 | 启动命令 | 端口/职责 | 关键事实 |
|---|---|---|---|
| server | `lework server` | :8080；HTTP API + 全部后台循环 | 单副本设计；后台循环无分布式锁 |
| worker | `lework worker [claude\|codex\|opencode]` | :8081；MCP + 模型代理 + NATS 消费 | 身份 `org_id+worker_id` 静态绑定 |
| 内嵌调度 worker | `scheduler.mode=process` | server fork 子进程 | 单机/开发形态；AIO 默认模式 |
| 容器调度 worker | `scheduler.mode=docker-cli` | shell 调 docker CLI | 依赖宿主 docker；未配置 docker socket 时不可用 |
| AIO 全栈 | `deployments/aio/install.sh` | Nginx :80 → Web + server | PG/NATS/server/Web 单机 Compose |
| K8s 调度 worker | — | — | **已删除**；`scheduler.New` 只接受 `process` / `docker-cli`，Helm 模板尚未同步 |
| NATS / PG / 对象存储 | docker-compose | 单实例 | 无集群/无 HA |

---

## 4. 端到端链路

### 4.1 新任务：`POST /v1/CreateInitialMessage`

```
SessionHandler.CreateInitialMessage
  └─ sessionService.CreateInitialMessage
      └─ MessagePoster.RunNewMessage           service/message_poster.go:257
          1 resolveOrCreateProject              （无 project_id 时自动建项目）
          2 绑定 @skill / connector 到项目       （幂等）
          3 ensureProjectSession                （project 级会话，1 项目 1 个）
          4 resolveOrCreateTask                 （无 task_id 时自动建 Task）
              ├─ 写入 expected_output / acceptance_criteria
              └─ assistant_ids[0] → task.assignee_id（并绑定项目资源）
          5 createTaskSession                   （task 级会话）
          6 附件落盘 uploads/_task/<taskID>/...
          7 resolveTaskAssigneeWorker           （assignee 优先，其次项目默认）
          8 resolveWorkerTaskModel              （组织默认模型）
          9 resolveProjectPluginSnapshots       （项目绑定的插件快照）
         10 PostMessage（事务：消息 + ReliableTask Outbox）
              └─ ReliableTaskDispatcher（1s 扫描）→ NATS cmd.run
```

要点：
- 一次「新任务」实际创建 **Project + Task + Session + Message** 四件套，返回四个 public_id（`contract/work_type.go:NewMessageResponse`）。
- 任务标题默认取消息前 50 字符；`task_type=general`。
- 「@队友」在新任务链路中同时成为 `assignee_id`，后续消息按 assignee 路由；旧任务由启动迁移 `backfillTaskAssigneeBindings` 补项目绑定。
- 派发不在 HTTP 请求内直接发 NATS，而是写 **Outbox**（`lework_reliable_task`）后异步发布，避免「入库成功、消息丢失」。

### 4.2 续聊 / 任务群聊：`POST /v1/AddMessage`

```
AddMessage → getSessionForCaller（org 校验 + 权限）
  → 绑定 invoked skill
  → 解析路由：task.assignee > 请求 assistant_ids（只取第一个）> 项目默认绑定
  → 未就绪的 assignee/worker → 409（ErrAssistantWorkerNotReady）
  → PostMessage（事务：消息 + Outbox）
  → ReliableTaskDispatcher → cmd.run
```

要点：
- Session 类型：`chat` / `task` / `project`。任务群聊 = 多真人 + 一个 AI 队友；`SenderUin/SenderName` 记录真人身份。
- 历史上下文只注入「当前 assistant 上一条回复之后」的增量消息（`message_poster.go:1026`），避免重复注入。
- `assistant_ids` 是**路由覆盖**，不是多助手群发；`firstOrDefault` 决定唯一目标。任务一旦有 assignee，显式 `assistant_ids` 不能把消息转给别的队友。

### 4.3 Worker 执行流水线（核心）

```
NATS cmd.run
 → command/run/handler.HandleRunCommand
     admission（NotAfter / 队列上限 / 已取消）
     inbox（SQLite，command_id 唯一 → 幂等）
     debounce 1.5s（同 session 合并）
     acquire compute slot（MaxConcurrency=10 / MaxInflight=20）
 → run/coordinator（同 session 串行，跨 session FIFO + 轮转）
 → agentrun.Service.Run
     1 clone/normalize
     2 Journal + publish run.started
     3 Preparer.Prepare
         workspace → skill → session messages → prompt → attachments
         → provider session resume → tools → MCP connectors → model
     4 Executor.Execute → Registry.ResolveWithKind(runtime)
     5 Finalizer.FinalizeRequired
         reconcileWorkspace → collectArtifacts → pushWorkspace
     6 归档 artifact.declared
     7 唯一终态：run.completed / run.failed / run.cancelled
     8 PostRunBestEffort
```

关键不变量：
- **一个 Run 恰好一个终态**（成功路径与 `finishError` 互斥）。
- **Runtime 不得发 `run.*`**；`run.*` 只由 `agentrun.Service` 产生。
- 事件按 `seq` 单调发布；终态事件内嵌完整快照，消费者漏收增量也能恢复。
- 失败时 `Metadata.Phase` 记录 `prepare/execute/finalize/plan_publish`，便于定位。

### 4.4 事件 → 前端

```
Runtime NodeEvent
  → NodeHandler（过滤内部事件、映射 RunEvent）
  → Journal（分配 seq、按序发布、归档）
  → NATS run.stream / run.state
      ├─ SessionRunStreamProjector（实时增量）
      ├─ SessionRunStateProjector
      │     ├─ run.started   → 建/更新 run 记录
      │     ├─ artifact.declared → 产物入库
      │     └─ 终态 → CompleteSessionMessage（消息内容/Chunks/Usage/Artifacts）
      ├─ TaskRunStateProjector（Trace.TaskID）
      │     ├─ run.started   → in_progress
      │     ├─ run.completed → pending_acceptance（cron 直接 completed）
      │     └─ failed/cancelled → failed / cancelled
      └─ AutomationRunProjection（cron 执行记录语义）
  → 前端 POST /v1/SessionEvents（SSE）
      message.delta / reasoning.delta / tool_call.* / approval / question / terminal
  → 前端 POST /v1/GlobalEvents（跨会话通知）
```

### 4.5 产物与 Git

```
prepare:
  projects/<orgID>/<projectPublicID>/repo
    有 gitea → clone；有 origin → pull；否则 git init
run:
  CapturePreRunTreeSafe（临时 index，不污染工作区）
finalize:
  git diff-tree → artifact.declared（逐文件）
  → presigned PUT → storage key projects/<org>/<project>/artifacts/<base58><ext>
  → artifacts.jsonl（sha256）
  → git add -A && commit && push origin main
```

- 推送失败 → 整个 Run 失败（产物已提交但未同步被视为硬失败）。
- 无 origin → 跳过推送（本地仓库可用）。
- `artifact_declare` 仍有 TODO：从「路径回退」改为 run-scoped ToolContext 注入（`tools/artifact_declare/tool.go:260`）。

### 4.6 自动化

```
AutomationScheduler
 ├─ Planner 30s：CAS 推进 next_run_at，生成 AutomationExecution(occurrence_key)
 └─ Dispatcher 15s：lease 2m / 最多 3 次 / 退避 {10s,30s,2m}
      → EnsureProject（项目失效则按 generation 重建）
      → 构造 RunCommand → ReliableTask → cmd.run
```

- 5 字段 cron + preset（hourly/daily/weekly/monthly）+ interval（最小 300s）。
- 时区 IANA，默认 `Asia/Shanghai`。
- 错过窗口折叠、不补跑（`LatestDue` / `MissedCount`）。
- 自动化任务仍由 `automation_run_projection.go` 处理 cron 语义；任务状态自身也由 `TaskRunStateProjector` 推进，两者按 `TaskType` 区分：普通任务进 `pending_acceptance`，cron 直接 `completed`。

### 4.7 审批 / 提问

```
Runtime 请求审批
  → runtimehost.InteractionRouter
  → coordinator.BeginInteractionWait（释放 compute slot，占用 interaction slot）
  → 阻塞等待
  ← NATS cmd.interaction（approval.resolve / question.answer）
  → 重新获取 compute slot → 回写 Runtime
```

- 等待期间不占并发额度；交互槽默认 10。
- 没有独立审批超时，依赖上层 ctx；交互槽满则 Run 快速失败。

### 4.8 任务验收闭环（M1）

```
创建任务（expected_output + acceptance_criteria + assignee）
  → Assignee 接管消息路由 → Run
  → run.completed
       ├─ task_type=cron  → completed
       └─ 普通任务        → pending_acceptance + result_summary
  → GetTaskDeliverables
       ├─ session 内 assistant 消息的 artifacts
       └─ resource_type=artifact 的项目文件（按 relative_path 去重）
  → 人验收
       ├─ AcceptTask(note)  → completed + accepted_by/accepted_at/acceptance_note
       └─ RejectTask(note)  → in_progress + 向任务会话发送【验收未通过】返工消息
                                └─ 发送失败 → 回滚到 pending_acceptance
```

状态约束（`service/task_service.go:ValidateTaskStatusTransition`）：

- `completed` / `cancelled` 是终态；验收接口只接受 `pending_acceptance`。
- **但状态机仍允许 `in_progress` / `pending_approval` → `completed`**（保留通用 `UpdateTask` 手动完成路径），因此「必须验收」目前只由验收入口保证，不是全局不变量。
- `pending_acceptance` 可由普通任务的 `run.completed` 进入；验收接口只接受该状态。
- 迟到/重复的 `run.started` 不会把 `pending_acceptance` 或终态回退；投影以 `consumer + event.ID` 的 receipt 去重。
- 交付物不复制到任务表；`git_commit` 字段目前为空，仓库没有可读取的 run commit 记录。

---

## 5. 领域模型与治理

### 5.1 核心实体

```
Organization (单层租户)
 ├─ UserOrg (UserID, OrgID)  ← MaxOrgsPerUser=1
 ├─ Department (ParentIDs 树) ── MemberDepartment
 ├─ Project (OrgID, OwnerID)
 │    ├─ ProjectMember (user|assistant, role)
 │    ├─ ProjectPluginBinding → Plugin → PluginRevision → PluginRevisionContent
 │    ├─ Task (expected_output, acceptance_criteria, assignee, pending_acceptance)
 │    │    └─ Session (task) ── SessionMessage
 │    ├─ Session (project)
 │    └─ ProjectFile / ProjectActivity
 ├─ DigitalAssistant ──1:1── WorkerDeployment
 │    ├─ PromptBlock / Memory / PromptTrace
 │    └─ Template
 ├─ Resource ── ResourceBinding (uin XOR assistant, role)
 ├─ LLMModel ── LLMHistory
 └─ Automation ── AutomationExecution
```

### 5.2 数据规模与特征

| 指标 | 值 |
|---|---|
| 表名常量 | 43 |
| AutoMigrate 模型 | 41 |
| 无外键约束 | 全部业务表 |
| 软删除 | 绝大多数 `gorm.Model`；`ProjectActivity`/`PluginTranslation`/`SeedRecord` 无 |
| 租户列 | 几乎所有业务表带 `org_id`，但仅靠查询条件 |
| 迁移方式 | **每次启动**执行 rename/backfill/drop（含 3 张表直接 Drop） |

### 5.3 认证与授权

**认证**
- OSS：本地 JWT；token 只携带 `uin`，组织归属每次请求从 `lework_user_org` 查。
- Enterprise：IAM 签发 claims，本地映射。
- 全局闸门只有 `RequireCallerOrg`（`OrgID>0 && State==Succ`）。
- 本地免鉴权必须同时满足三项：显式 `LEWORK_DEV_ALLOW_UNAUTH=true`、`LEWORK_ENV != production`、请求来自回环地址（`middleware/identify.go`）。仅设置 `LEWORK_DEV=true` 不再绕过鉴权。
- 生产环境首次 seed 必须提供 `LEWORK_ADMIN_PASSWORD`，否则拒绝创建管理员（`seed/account_oss.go`）；开发环境保留默认账号。

**授权**
- 三层：动作矩阵（`types/permission.go`，6 类资源 / 31 action）→ 策略（`permission_policy.go:SystemPolicy`）→ 绑定与父链继承（`Resource` + `ResourceBinding`，`MaxRole`）。
- 路由 guard 主要在 project/task；`GuardByPublicID` 对 file/artifact/assistant 直接报不支持（`permission_guard.go:75`）。
- 组织/用户/部门管理接口**没有 handler 级权限守卫**，仅依赖 service 内部使用 `caller.OrgID`。

### 5.4 租户边界

| 层 | 隔离手段 | 评价 |
|---|---|---|
| DB | `org_id` 查询条件 | 应用层隔离；无 FK/RLS |
| NATS | subject 前缀 `org.<id>.` | 逻辑隔离；单凭据，可跨租户订阅 |
| Worker | 每 org/worker 独立进程 + 独立 workspace | 进程级隔离较好 |
| 存储 | key 前缀 `projects/<orgID>/...` | 逻辑隔离 |
| 组织模型 | 单层、每人 1 org | 不支持集团/多组织 |

---

## 6. Agent 执行内核

### 6.1 四 Runtime 对比

| 维度 | `lework`（native） | `claude` | `codex` | `opencode` |
|---|---|---|---|---|
| 形态 | 进程内 Eino 循环 | CLI 子进程 stream-json | `codex app-server` JSON-RPC | ACP over stdio |
| 模型接入 | Eino（Anthropic/OpenAI 兼容） | 环境变量注入 | 生成 config.toml | `OPENCODE_CONFIG_CONTENT` |
| Lework 工具 | 全部 6 类（进程内） | 仅 3 个（MCP） | 仅 3 个（MCP） | 仅 3 个（MCP） |
| 项目 MCP 连接器 | ❌ 明确不支持 | ✅ | ✅ | ✅ |
| 审批 | ❌ 正则黑名单直接拒绝 | ✅ control_request | ✅ 单槽审批 | ✅ ACP permission |
| plan 模式 | ❌ | ❌（显式禁用） | ❌ | ✅ 唯一实现 |
| 会话续接 | 无 provider session（历史截断 20 条） | `--session-id/--resume` | thread/start|resume | ACP session |
| todo 可观测 | ✅ | ❌ | ❌ | 部分 |
| 成熟度 | 高 | 中高 | 中 | 中偏低（自带 probe 测试，ACP 包自称雏形） |

**结论：Runtime 不是可互换的「驱动」，而是能力差异很大的四种执行产品。** 同一 `PermissionMode`、`plan`、工具集在不同 runtime 上语义不同。

### 6.2 工具系统

| 层 | 契约 | 暴露路径 |
|---|---|---|
| 业务工具 | `tools.Tool{Name, Description, InputSchema, Execute(JSONInput)}` | `app.registerTools` |
| 内核工具 | `agent.Tool{Definition, Execute → ToolResult}` | `agentrun.Adapt` |
| MCP 桥 | `/v1/mcp`（Streamable HTTP） | CLI runtime 拉取 `memory`/`skill_manage`/`artifact_declare` |

已注册工具：`artifact_declare`、`skill_use`、`skill_manage`、`memory`、`todo`、`node{terminal,read,write}`。

安全边界：
- native：危险命令正则黑名单 + 工作区路径约束 + env 清洗 + 超时 5–600s + 输出截断。
- CLI：各自沙箱/审批；但 codex 生成 `sandbox_mode = "danger-full-access"`，opencode 默认 `{"*": "allow"}`，claude 空 `PermissionMode` 走 `--dangerously-skip-permissions`。

### 6.3 事件模型

| 层 | 类型 | 生产者 | 消费者 |
|---|---|---|---|
| 内核 | `NodeEvent`（message/reasoning/tool/todo/approval/question/plan） | Runtime | NodeHandler |
| 业务 | `RunEvent`（`message.delta`、`tool_call.*`、`artifact.declared`、终态） | agentrun.Service | NATS / Projector / SSE |

- 两条 lane：`run.stream`（高频增量）与 `run.state`（状态/终态）。
- seq 单调 + 按序发布；终态内嵌快照。
- 逐 delta 归档，仅在快照层合并 → 长输出会放大存储。

### 6.4 交互与审批的边界

- 审批/提问等待释放计算槽，避免长审批拖垮并发。
- 无审批超时定时器；ctx 无 deadline 时可能长期占用交互槽。
- codex 只有单槽 `pendingApproval`，并发审批会覆盖。
- native 没有「询问」路径，只能拒绝。

---

## 7. 部署与运行

### 7.1 交付形态

| 形态 | 组成 | 适用 |
|---|---|---|
| **AIO 一体机** | Nginx + Web + server（内嵌 worker）+ PG 17 + NATS 2.12 | 单机部署 / 私有化 / 演示（推荐开源默认） |
| docker-compose（旧） | PG + NATS + server + 独立 worker | 单机开发与对照 |
| Helm (k8s) | PG 18 + NATS + server + 动态 worker Deployment | **模板仍引用已删除的 `scheduler.mode=k8s`，当前不可用** |
| 本地开发 | `make dev-setup` + `dev-server/dev-worker/dev-frontend` | 开发 |
| CLI 单机 | `lework chat` / `session` / `task` | 轻量使用/调试 |

### 7.2 硬限制

- NATS 单副本、无跨节点复制；宕机 = 全平台停摆。
- PG 单实例；无 Operator/副本。
- server `replicas=1 + Recreate`；后台循环无 leader 选举 → **无法直接水平扩展**。
- worker workspace 在 k8s 用 hostPath → 绑定节点、不可漂移；当前 k8s 调度已删除。
- 队列上限 `MaxQueuedCommands=1000`、stream `DiscardOld` → 过载静默丢最旧消息。
- 无 DLQ、无 `MaxDeliver` 上限 → poison message 可无限重投。
- AIO 当前只做了 YAML 解析与脚本 `bash -n` 校验，**未做整机启动验证**。

### 7.3 关键配置面

`config.Config`：`server`（port/jwt/disable_event_consumers）、`nats`、`database`、`llm`、`scheduler`（仅 `process` / `docker-cli`；结构体仍残留 k8s 字段）、`storage`（local/s3）、`gitea`、`worker_auth`、`automation_scheduler`、`mcp_connectors`、`managed_credentials`、`feishu`（仅反馈）、`auth`（企业 IAM）。

关键环境变量：`LEWORK_ENV`、`LEWORK_ADMIN_EMAIL`、`LEWORK_ADMIN_PASSWORD`、`LEWORK_PUBLIC_URL`、`LEWORK_CORS_ALLOWED_ORIGINS`、`LEWORK_DEV_ALLOW_UNAUTH`、`LEWORK_WORKSPACE_ROOT`。

### 7.4 AIO 拓扑

```
浏览器 :80
  └─ gateway (Nginx)
       ├─ /            → web:3000
       ├─ /v1/*        → lework:8080
       ├─ /v1/SessionEvents, /v1/GlobalEvents → SSE（proxy_buffering off）
       └─ /lework/*     → 本地文件预签名读写
lework 容器（worker 镜像）
  └─ lework server
       └─ scheduler.mode=process → 同容器拉起 /lework worker
```

- 不需要 Docker socket，也不需要第二个 worker 容器；镜像内自带文档/浏览器/CLI 工具链。
- 数据落在 `deployments/aio/data/app/{storage,workspace,logs}`；PG/NATS 用命名卷。
- `install.sh` 生成 `.env`、渲染配置、等待 `/healthz`；备份/恢复/升级脚本已提供。
- 默认镜像指向 `registry.yygu.cn`；开源用户需通过 `LEWORK_IMAGE` / `LEWORK_WEB_IMAGE` 覆盖。

---

## 8. 代码事实 vs 产品叙事

| 叙事（README/文档） | 代码事实 | 结论 |
|---|---|---|
| 「多 Agent 调度，拆解工作流，多个子 Agent 并行」 | 单消息单 assistant；`firstOrDefault`；无 fan-out/聚合 | ⛔ 未实现 |
| 「内置 MCP Server，对外暴露 Lework 运行时能力」 | 仅 worker `:8081/v1/mcp`，3 个工具，供 CLI runtime 使用；未配置 token 时拒绝 | 🟠 内部桥，不是对外平台 |
| 「知识库沉淀上下文」 | 仅 `KnowledgeType` 常量；前端 `/knowledge` 隐藏重定向 | ⛔ 未实现 |
| 「成本追踪（按任务/项目/团队）」 | `llm_history` 有 token/延迟/业务键；无 cost/单价/聚合报表 | 🟡 仅用量 |
| 「RBAC 三级（用户/队友/技能）」 | project/task 完整；assistant/plugin 部分；org/user 无 guard | 🟡 部分 |
| 「多租户完全隔离」 | `org_id` 应用层过滤；NATS 单凭据；`MaxOrgsPerUser=1` | 🟡 逻辑隔离 |
| 「审批工作流」 | 四 runtime 语义不一致；native 无审批 | 🟡 部分 |
| 「私有化部署（k3s + Helm）」 | K8s 调度已删除；AIO Compose 可用但未实机验证；Helm 模板仍引用旧 k8s 模式 | 🟠 需重做 |
| 「渠道接入 GitHub/GitLab/企微」 | 连接器已从主干删除 | ⛔ 已移除 |
| 「技能库」 | Plugin/Revision/Content/项目绑定/marketplace 完整 | ✅ 已实现 |
| 「自动化」 | cron/interval + execution + 项目代数重建 | ✅ 已实现 |
| 「任务驱动协作」 | 任务可建、可派、可群聊，Run 结束进入待验收，可通过/驳回 | ✅ 已闭环 |
| 「文件版本管理」 | 项目文件版本 + 预览 + 恢复 | ✅ 已实现（缺文件夹打包下载） |
| 「桌面端」 | Electron 39 + 自动更新 | ✅ 已实现 |
| 「薪资代理（Payroll）」 | 后端路由/服务、前端页面、Skill、配置已删除 | ⛔ 已删除 |
| 「首次启动向导」 | 无 `/setup`；仍靠环境变量 seed 管理员与默认组织 | ⛔ 未实现 |

---

## 9. 风险登记（按决策影响排序）

### 已关闭的高风险项（2026-10-31）

| 原风险 | 处理 |
|---|---|
| `LEWORK_DEV=true` 完全绕过鉴权 | 改为 `LEWORK_DEV_ALLOW_UNAUTH` + 非生产 + 回环地址三条件 |
| CORS 任意 Origin + credentials | 改为显式白名单；同源 AIO 不需要 CORS |
| MCP 空 token 放行 | 空 token 一律 401；worker 自动生成随机 token |
| 默认管理员口令 | 生产环境必须有 `LEWORK_ADMIN_PASSWORD`，否则拒绝启动 |
| Worker 管理端点无鉴权 | 删除 `/ws/worker`、`ListWorkers`、`ShutdownWorker`、`CreateWorker` |
| K8s 调度器无限递归 | 删除 k8s 调度器；只保留 `process` / `docker-cli` |
| Payroll 垂直模块 | 后端/前端/配置/Skill 全量删除 |
| 未接线连接器 | GitHub/GitLab/企微 webhook 代码从主干删除 |

### P0 — 交付/安全准入门槛（仍未关闭）

| # | 风险 | 证据 | 影响 |
|---|---|---|---|
| 1 | 模型 API Key 可逆落库 | `llm/manager_db.go` 的 `api_key_encrypted` 实际存原文 | DB 泄露即密钥泄露 |
| 2 | 连接器/平台凭据明文 | `MCPChannel.Headers/AuthConfig`、plugin definition | 私有化交付审计风险 |

### P1 — 正确性/一致性

| # | 风险 | 影响 |
|---|---|---|
| 3 | `/v1/ops/workers/status` 只校验 `caller.OrgID`，不校验管理员 | 任意组织成员可查询本组织 Worker 运行快照 |
| 4 | Helm Chart 仍配置 `scheduler.mode=k8s`，而该模式已删除 | 按 Chart 部署会直接启动失败 |
| 5 | AIO 未做整机启动验证 | 脚本/YAML 正确不等于可交付 |
| 6 | 取消意图不持久化；inbox 无 `cancelled` | worker 崩溃后取消丢失，取消被记成 failed |
| 7 | control/interaction/file lane 失败仍 Ack | 指令静默丢失 |
| 8 | stream `DiscardOld` + 无 DLQ | 终态/命令可能静默丢失，run 永久 running |
| 9 | 任务状态只靠 `run.state` 事件投影，无终态对账 | 终态事件丢失时任务会停在 `in_progress` |
| 10 | 双重成员体系（ProjectMember + ResourceBinding） | 双写漂移，权限判断分歧；assignee 绑定已依赖 ResourceBinding |
| 11 | 组织/用户接口无权限 guard | 任意登录用户可尝试越权管理 |
| 12 | `GuardByPublicID` 仅支持 project/task | 前端批量鉴权对 file/artifact/assistant 报错 |
| 13 | 启动即执行破坏性迁移 | 回滚困难；大租户启动慢 |
| 14 | DAO 部分 `GetByPublicID` 不带 org | 绕过 service 即越权面 |
| 15 | native 与 CLI 安全模型相反 | 同一命令在不同 runtime 行为不同 |
| 16 | 任务注释仍声称支持子任务，但无 `ParentTaskID` | 产品/代码语义不一致 |
| 17 | `UpdateTask` 允许 `in_progress` / `pending_approval` → `completed` | 可绕过验收入口，验收不是全局强约束 |

### P2 — 架构/成本

| # | 风险 | 影响 |
|---|---|---|
| 18 | 每队友 1 worker | 成本随队友数线性增长 |
| 19 | 无容量感知路由 | 全部压到 `worker_id=1` |
| 20 | server 单副本 + 后台循环无锁 | 无法水平扩展 |
| 21 | 三套模型接入路径（Eino / CLI SDK / llmprotocol） | 统一化成本高 |
| 22 | 逐 delta 归档 | 存储放大 |
| 23 | 群聊/SSE 两段式（GlobalEvents → SessionEvents） | 前端状态机复杂、易竞态 |
| 24 | Swagger 覆盖率约 60% | 三方对接困难 |
| 25 | 前端任务状态归一化（`pending_acceptance` 在列表显示为 `in_progress`） | 列表与详情语义不一致，验收入口不够显眼 |

---

## 10. 后续决策清单

> 每个决策给出：现状 → 选项 → 建议。建议以「当前代码事实 + 产品承诺」为依据，不代表唯一解。

### D1. 产品定位：做「AI 队友平台」还是「Agent 执行平台」？

- **现状**：`AIO` 分支已删除 Payroll、未接线连接器、K8s 调度器与占位页面，主链路收敛为 Project/Task/Session + 验收。
- **选项**：
  - A. 收敛为「AI 队友协作平台」：强化项目/任务/验收/资产，弱化底层平台叙事。
  - B. 收敛为「Agent 运行时/网关」：强化 runtime、MCP、模型代理，弱化业务域。
  - C. 双轨：上层队友平台 + 下层运行时对外输出。
- **结论**：**已采用 A**。CLI、MCP、模型代理保留为内部能力，不单独作为对外产品叙事。

### D2. Runtime 策略

- **现状**：native 是唯一全工具、无审批的「内核」；claude/codex/opencode 是能力各异的 CLI 封装；服务端不设置 runtime，由 worker 默认值决定。
- **选项**：
  - A. native 优先：CLI runtime 仅作为实验能力。
  - B. CLI 优先：把 native 降级为兜底。
  - C. 能力契约 + 显式选择：为每个 runtime 声明 `tools/approval/plan/mcp` 能力，产品层按能力路由，不支持则明确报错。
- **建议**：仍选 C。先在数据模型上允许「AI 队友选择 runtime/模型」，再补 capability matrix；否则同一队友换 worker 就换行为，无法做 SLA。

### D3. Worker 拓扑与成本

- **现状**：`WorkerDeployment.digital_assistant_id` 唯一，1 队友 1 worker；无容量路由；k8s 调度器已删除，AIO 使用 `process` 在容器内拉起 worker。
- **选项**：
  - A. 保持 1:1，卖「专属队友」。
  - B. 按 org/runtime 池化 worker，多队友共享。
  - C. 混合：默认池化，付费/高隔离客户 1:1。
- **建议**：C。先实现「池化 + 按 runtime 分组 + 容量上报」，保留 1:1 作为高隔离档；Helm/k8s 要么重做，要么从主干移出。

### D4. 多 Agent / 群聊语义

- **现状**：群聊是多真人 + 单 AI；`assistant_ids` 只取第一个；无并行、无聚合、无子 Run。M1 已让 assignee 成为唯一执行者。
- **选项**：
  - A. 明确「一次一个队友」，把多 Agent 从叙事中移除。
  - B. 实现「@ 多个队友 → 并行 Run → 汇总消息」。
  - C. 实现「主 Agent 委派子 Agent」的编排层。
- **结论**：**M1 采用 A**。README/产品页不得再承诺多 Agent 并行；B 只在有明确需求时做，C 不在当前 Task 模型上硬加。

### D5. 安全与私有化门槛

- **现状**：开发免鉴权、CORS、MCP token、默认管理员、Worker WS 端点已修；模型/连接器凭据仍未加密，WorkerOps 未做管理员校验。
- **选项**：
  - A. 逐项修复后再交付。
  - B. 以「内网可信」为前提，用部署文档约束。
- **建议**：A。剩余最小集：KMS/信封加密模型与连接器凭据、`/v1/ops/workers/status` 加管理员或独立运维权限。

### D6. 商业化：计量 → 计费

- **现状**：有 `llm_history` 与 message usage；无单价/余额/配额/账单。
- **选项**：
  - A. 只做用量报表（当前能力）。
  - B. 做配额与限流（防止成本失控）。
  - C. 完整计费（账户/流水/定价/结算）。
- **建议**：先 B（配额 + 组织级预算告警），再按客户需求做 C；计费域应与 `llm_history` 解耦，异步结算。

### D7. 知识沉淀闭环

- **现状**：产物 → 项目文件/版本/记忆；但「知识库」只有常量与隐藏页面；跨项目复用依赖 skill/plugin 与 memory。
- **选项**：
  - A. 以「项目资产 + 技能库」作为知识沉淀，不建独立知识库。
  - B. 接入 CoreKG（MCP）作为外部知识源。
  - C. 自建知识库（文档/向量/检索）。
- **建议**：A + B。当前代码已支持 MCP 连接器与项目绑定，接 CoreKG 的边际成本最低；自建知识库是重投入，需明确差异化。

### D8. 数据模型与契约治理

- **现状**：41 模型、无 FK、双重成员体系、启动即破坏性迁移、Swagger 覆盖 60%。
- **选项**：
  - A. 继续增量迁移。
  - B. 引入版本化 migration + 逐步收敛成员模型 + 生成式 API 契约。
- **建议**：B。先做「迁移版本化 + 停止启动 Drop」；成员模型收敛放在权限改造窗口；API 契约以代码生成/CI 校验替代手工 Swagger。

### D9. 交付形态：AIO 优先，还是 Helm/K8s 优先？

- **现状**：AIO 已覆盖单机全栈，但未实机验证；K8s 调度器已删除，Helm Chart 仍引用旧模式。
- **选项**：
  - A. AIO 作为开源默认，Helm 从主干移出，等有企业客户再重做。
  - B. 同时维护 AIO 与 Helm，先修 Helm。
- **建议**：A。开源项目的「一条命令跑起来」比多编排形态更重要；Helm 保留在独立仓库或 `contrib/`，避免 CI 与文档继续背负不可用路径。

---

## 11. 建议优先级

### 已完成（截至 2026-10-31）

1. 任务验收闭环：字段、`pending_acceptance`、Run 投影、交付物聚合、通过/驳回 API 与详情页。
2. assignee 路由：创建任务自动绑定、旧任务启动回填、未就绪返回 409。
3. 安全最小集：开发免鉴权三条件、CORS 白名单、MCP 强制 token、生产管理员密码、Worker WS 删除。
4. 模块剥离：Payroll、未接线连接器、K8s 调度器、演示账号、资源库占位页。
5. AIO 一体机：Compose 全栈、同源网关、备份/恢复/升级脚本、CI 基线。

### 下一步（M1 收尾）

6. 首次启动向导 `/setup`：干净环境创建管理员、组织、默认队友、默认模型与 WorkerDeployment。
7. 示例项目与 Golden Demo runbook：覆盖「建任务（预期产出 + 验收标准）→ 派队友 → Run → 产物 → 验收/驳回 → 返工」。
8. 在可用 Docker 环境启动 AIO，跑通 Golden Demo 并补回归测试。
9. 模型 API Key 加密；`/v1/ops/workers/status` 管理员级校验。
10. Helm/k8s：重做或从主干移出，避免不可用交付路径。

### 1–2 月（可用性）

11. 持久化取消意图（inbox `cancelled` 或 DB 取消表）。
12. control/interaction/file lane 失败不 Ack；为 stream 增加 `MaxDeliver` + 告警。
13. Runtime capability matrix + 每队友 runtime/模型选择。
14. Worker 容量上报 + least-loaded 路由（或池化 worker PoC）。
15. 权限补齐：org/user guard、file/artifact/assistant 的 `GuardByPublicID`。

### 1 季度（架构）

16. 凭据加密（KMS/信封加密）与独立 credential store。
17. 计费/配额域（先配额，后账单）。
18. 迁移版本化 + 成员模型收敛。
19. server 水平扩展方案（后台循环 leader 选举或拆分）。
20. 知识沉淀：CoreKG/MCP 接入或自建知识库的取舍落地。

---

## 附录 A：关键文件索引

| 主题 | 文件 |
|---|---|
| 路由装配 | `backend/internal/api/router.go` |
| 鉴权中间件 | `backend/internal/api/middleware/{identify,auth,cors}.go` |
| 会话/SSE | `backend/internal/api/handler/session_handler.go`、`service/session_service.go` |
| 消息编排 | `backend/internal/service/message_poster.go` |
| 任务验收 | `backend/internal/service/task_service.go`、`backend/internal/api/handler/task_handler.go`、`frontend/packages/app-ui/components/layout/TaskAcceptancePanel.tsx` |
| 任务状态投影 | `backend/internal/runnable/task_run_state_projector.go` |
| 会话/自动化投影 | `backend/internal/runnable/{session_run_state_projector,session_run_stream_projector,automation_run_projection}.go` |
| Worker 接入 | `backend/internal/worker/command/run/handler.go`、`run/coordinator.go` |
| Run 生命周期 | `backend/internal/worker/agentrun/{service,preparer_impl,finalizer_impl,journal}.go` |
| Runtime 契约 | `backend/agent/{runtime,executor,registry,node_event,interaction}.go` |
| Runtime 实现 | `backend/agent/runtime/{native,claude,codex,opencode}/` |
| 工具 | `backend/tools/{tool,registry}.go`、`backend/tools/*/` |
| MCP 桥 | `backend/internal/worker/mcp/{server,router,auth}.go` |
| 消息契约 | `backend/pkg/messaging/{subject,envelope,command,event}.go` |
| 数据模型 | `backend/types/*.go`、`backend/internal/infra/db/database.go` |
| 权限 | `backend/internal/service/{permission_policy,permission_core,permission_guard}.go` |
| 自动化 | `backend/internal/service/automation_*.go` |
| Worker 调度 | `backend/internal/worker/scheduler/*.go`、`service/worker_*.go` |
| LLM | `backend/internal/llm/`、`backend/internal/modelrouter/` |
| AIO 部署 | `deployments/aio/{docker-compose.yml,install.sh,nginx/nginx.conf,config/}` |
| 能力边界 | `docs/product/current-limitations.md` |
| 核心产品计划 | `docs/product/2026-10-30-core-product-development-plan.md` |
| 前端状态/API | `frontend/packages/store/{appStore.ts,api/client.ts,chat/*}` |
| 前端页面 | `frontend/apps/web/app/(shell)/*`、`frontend/packages/app-ui/` |

## 附录 B：统计口径与复核方法

- 代码规模：`find backend -name '*.go' | wc -l` / `xargs wc -l` = 797 / 171478（2026-10-31 工作区）。
- 表：`backend/types/tables.go` 中 `TableName*` 常量 43 个；`runMigrations` 的 models 41 个。
- 路由：非测试文件中 `.GET/.POST/.PUT/PATCH/DELETE(` 注册点约 152 处（含 worker 与 modelrouter）。
- 测试：`find backend -name '*_test.go' | wc -l` = 265。
- Runtime：`backend/agent/runtime/{native,claude,codex,opencode}`。
- 本文所有「未实现/不一致」结论均已在对应文件核对。M1 合并时已执行 `go vet ./...`、`go build ./...`、`go build -tags enterprise ./...`、`go test ./...` 与前端 `pnpm -r typecheck` / `pnpm -r test`；**本节统计与命名迁移后的工作区尚未重跑全量测试**，AIO 整机也未启动验证。

## 附录 C：术语

| 术语 | 含义 |
|---|---|
| AI 队友 / DigitalAssistant | 组织内的 AI 执行者，绑定 1 个 WorkerDeployment |
| Worker | 执行 Agent Run 的独立进程，按 `org_id+worker_id` 标识 |
| Run | 一次 Agent 执行，产出唯一终态事件 |
| Session | 会话；chat / task / project 三类 |
| Plugin | 技能/MCP/Connector 的统一载体，按 revision 不可变发布 |
| Skill | Plugin 的一种（`kind=skill`），项目绑定后进入运行快照 |
| Outbox / ReliableTask | 事务性消息派发表，避免入库与投递不一致 |
| Projector | 消费 run 事件并投影到 DB/消息的组件；任务投影用 `ProjectionReceipt` 幂等 |
| Assignee | 任务指定的 AI 队友；任务消息路由的最高优先级 |
| Pending Acceptance | Run 成功后的待验收状态；只能由人通过/驳回离开 |
| AIO | All-in-One 一体机部署，单机 Compose 跑齐控制面、执行面、数据面与 Web |
| ACP | Agent Client Protocol，opencode runtime 使用的 stdio JSON-RPC 协议 |
