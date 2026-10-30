# Lework 产品架构（As-Built）

> 基线：`1458925a`（2026-10-30），Go module `github.com/insmtx/Leros`。
> 口径：**以当前代码为准**，不采信 README / `docs/` 中与实现不一致的描述；凡引用结论均给出文件路径或符号，便于复核。
> 规模：816 个 Go 文件 / 173k 行（含测试）、266 个 `_test.go`、41 个 AutoMigrate 模型、43 个表名常量、约 157 处路由注册、4 个 Agent Runtime、64 个 service 实现文件。

---

## 0. 一句话概括

Lework 是一个**面向组织的 AI 队友平台**：以「项目 → 任务 → 会话」为协作载体，把一次用户消息转换成 worker 上的 **Agent Run**，在 Git 工作区里执行并产出文件，再通过 **NATS 事件 + POST-SSE** 把过程、产物、用量回写到控制面。

当前形态可精确描述为：

> **单体控制面（server）+ 每 AI 队友一个执行单元（worker）+ 单节点 NATS JetStream + 单实例 PostgreSQL + Git/对象存储**。

它不是工作流编排引擎，不是通用 Agent 开发框架，也不是已完成商业化的 SaaS：计费、知识库、多组织、多 Agent 并行等能力尚不在代码中。

---

## 1. 十条关键结论（供决策先读）

| # | 结论 | 直接影响 |
|---|---|---|
| 1 | **产品主链路是「消息即任务」**，不是流程编排：`CreateInitialMessage` 一次性创建 Project → Task → Session → Message，然后派发一个 Run。Task 是记录单元，不是可编排的 DAG 节点。 | 若要卖「工作流/多步骤编排」，需要新引擎，不是改现有 Task。 |
| 2 | **Task 生命周期没有闭环**：手动任务的 `status` 创建后不会被 Run 结果自动推进；只有自动化任务有事件投影（`runnable/automation_run_projection.go`）。 | 「任务看板」目前是半自动的；验收/审批状态需补投影。 |
| 3 | **「多 Agent 并行调度」未实现**：一条消息只路由一个 assistant，`assistant_ids` 只用第一个（`worker_resolution.go:firstOrDefault`），一次 Run 只对应一个 worker/assistant。 | 对外叙事必须收敛；要做并行需新增 fan-out + 聚合模型。 |
| 4 | **AI 队友没有技能/模型/记忆的外键**：技能走**项目级**插件绑定，模型走**组织默认模型**，记忆走 prompt block / memory 表。同一队友在不同项目能力不同。 | 无法表达「队友自带技能包」「队友专属模型」；产品语义需明确或补绑定表。 |
| 5 | **每个 AI 队友 1:1 一个 WorkerDeployment**（`WorkerDeployment.digital_assistant_id` 唯一）。成本模型 ≈ 队友数 × 常驻进程。 | 大规模组织成本高；需要池化/按需伸缩策略。 |
| 6 | **控制面与执行面只通过 NATS 解耦**；客户端实时通道只有两个 **POST-SSE**（`SessionEvents` / `GlobalEvents`），不是 WebSocket。 | 网关/CDN 必须支持 POST + 流式响应；不能直接用 `EventSource`。 |
| 7 | **多租户是应用层 `org_id` 隔离**，无外键、无 RLS、NATS 单凭据；且 `MaxOrgsPerUser=1`、`SwitchOrganization/CreateOrganization` 未实现。 | 当前是「单组织 SaaS/私有化」，不是真正的多组织平台。 |
| 8 | **商业化闭环缺失**：有 token 用量（`llm_history` / `SessionMessage.Usage`），没有单价、余额、配额、扣减、账单。 | 「成本追踪」只能做到「用量可观测」。 |
| 9 | **安全上有必须先修的 P0**：`LEROS_DEV=true` 全量后门、模型 Key 明文、Worker 管理端点无鉴权、MCP 空 token 放行、默认管理员口令。 | 私有化/企业交付前必须作为准入门槛。 |
| 10 | **交付形态是单节点**：NATS/PG 单实例、server `replicas=1 + Recreate`、后台循环无 leader 选举；`scheduler.mode=k8s` 的调度器存在无限递归缺陷，当前不可用。 | 高可用/水平扩展不是现状；k8s 交付路径需先修复。 |

---

## 2. 产品能力地图

状态图例：✅ 完整可用 ｜ 🟡 可用但有明显边界 ｜ 🟠 半成品/部分实现 ｜ ⛔ 未实现/仅占位

| 能力域 | 状态 | 实际入口 | 代码事实 |
|---|---|---|---|
| 邮箱/手机/密码登录、JWT、刷新令牌 | ✅ | `/v1/LoginByPassword` 等 | `adapter/account/oss/auth.go`；JWT 只带 `uin`，org 每请求反查 |
| 企业 IAM 认证（enterprise 构建） | 🟡 | 同一套 API | `adapter/account/enterprise/`；`-tags enterprise` 编译期切换 |
| 组织 / 成员 / 部门 CRUD | 🟡 | `/v1/CreateOrg`、`/v1/ListOrgMembers`… | 仅有 `RequireCallerOrg`，**无 handler 级权限守卫**；多组织未实现 |
| 项目（成员、插件、活动、文件、记忆） | ✅ | `/v1/CreateProject`… | `service/project_service.go`、`project_file_handler.go` |
| 任务 CRUD + 自动建任务 | 🟡 | `/v1/CreateTask`、`CreateInitialMessage` | 状态不随普通 Run 流转；注释称支持子任务拆分，但 `Task` 结构体无 `ParentTaskID` 字段，未形成编排 |
| 任务群聊（多真人 + AI 队友） | ✅ | `/v1/AddMessage` + `GlobalEvents` | 多真人发言、单 AI 回复；多 assistant 仅「选一个」 |
| AI 队友（角色/模板/可见性/状态） | ✅ | `/v1/CreateDigitalAssistant`… | 1:1 WorkerDeployment；无技能/模型绑定列 |
| 会话与消息 | ✅ | `/v1/CreateSession`、`GetSessionMessages` | 三种 Session：chat / task / project |
| 实时流 | ✅ | `POST /v1/SessionEvents`、`POST /v1/GlobalEvents` | POST-SSE，seq 单调、支持 replay |
| 技能 / 插件 / 插件市场 | ✅ | `/v1/plugins/*`、`/v1/plugin-marketplace/*` | `plugin` + `plugin_revision` + `plugin_revision_content` + 项目绑定 |
| MCP 连接器（平台/OAuth/绑定） | 🟡 | `/v1/plugins/mcp/*` | 平台模板 + OAuth + 运行时快照；GitHub/GitLab 连接器未接线 |
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
| CLI | ✅ | `leros chat/session/task/project/skill/automation/login` | `cmd/leros/*.go` |
| Web / 桌面端 | ✅ | Next.js 16.2.3 / Electron 39 | `frontend/apps/{web,desktop}` |
| 业务垂直应用（薪资代理） | 🟠 | `/v1/PayrollProxy/:action` | 固定 action 映射 + 托管凭据，属于客户定制集成 |
| 监控 / 指标 / 链路追踪 | 🟡 | 日志 + `operations/worker-status-api` | 有 HTTP 请求延迟直方图（`middleware/request_context.go` 的 `request_latency_seconds`）与结构化日志；无 Run/队列/业务级指标，无 OTel tracing 证据 |

---

## 3. 系统架构

### 3.1 全景图

```
┌────────────────────────────── 接入层 ──────────────────────────────┐
│  Web (Next.js 16, apps/web)   Desktop (Electron 39)   CLI (leros)  │
│  浏览器 / 桌面 HashRouter        cobra 子命令          worker 进程    │
└───────────────┬──────────────────────────┬────────────────────────┘
                │ HTTPS /v1 (Bearer)       │ POST + text/event-stream
                │                          │ (SessionEvents / GlobalEvents)
┌───────────────▼──────────────────────────▼────────────────────────┐
│                        控制面 server (`leros server`)              │
│  api/router.go                                                     │
│   ├─ middleware: CORS → Caller → RequestID → ClientUpdate → Logger │
│   ├─ handler/*（~157 路由：auth/session/project/task/assistant/...） │
│   └─ service/*（64 文件：项目/会话/任务/插件/自动化/权限/Worker 编排）│
│  后台循环：                                                         │
│   ├─ ReliableTaskDispatcher（Outbox，1s）                           │
│   ├─ SessionRunState/StreamProjector（run.* → DB/消息）             │
│   ├─ AutomationScheduler（planner 30s + dispatcher 15s）            │
│   ├─ WorkerDeploymentReconciler（10s，期望态对账）                   │
│   └─ llm.UsageConsumer / SkillPackageConsumer                      │
│  依赖：GORM → PostgreSQL；filestore；gitea；modelrouter/llm         │
└───────────────┬────────────────────────────────┬───────────────────┘
                │ NATS JetStream                  │ SQL
                │ org.<org>.worker.<w>.cmd.*      │
                │ org.<org>.session.<s>.run.*     │
┌───────────────▼────────────────────────────────┴───────────────────┐
│                       执行面 worker (`leros worker`)                │
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

### 3.2 分层职责

| 层 | 目录 | 职责 | 约束 |
|---|---|---|---|
| 接入 | `frontend/apps/*`、`backend/cmd/leros` | UI、CLI、进程生命周期 | 不含业务逻辑 |
| 控制面 | `backend/internal/api` | 路由、鉴权、DTO、handler | handler 不直连复杂业务；经 service |
| 业务域 | `backend/internal/service` | 项目/任务/会话/插件/自动化/权限/Worker 编排 | 可依赖 db 与 adapter，不可反向依赖 api |
| 执行内核 | `backend/agent` | Runtime 契约、NodeEvent、工具、交互 | 业务无关；禁止访问 NATS/DB；禁止发 `run.*` |
| 执行宿主 | `backend/internal/worker` | 命令接入、调度、prepare/finalize、事件发布 | 承接业务，调用 agent |
| 能力 | `backend/tools`、`backend/internal/skill`、`backend/internal/memory` | 工具、技能、记忆 | 通过 ToolContext 注入运行上下文 |
| 基础设施 | `backend/internal/infra/*`、`backend/pkg/*` | db / mq / filestore / git / websocket / messaging | 共享契约沉到 `pkg` |
| 领域类型 | `backend/types`、`backend/config` | 实体、常量、配置 | 无业务逻辑 |

### 3.3 进程与部署单元

| 单元 | 启动命令 | 端口/职责 | 关键事实 |
|---|---|---|---|
| server | `leros server` | :8080；HTTP API + 全部后台循环 | 单副本设计；后台循环无分布式锁 |
| worker | `leros worker [claude\|codex\|opencode]` | :8081；MCP + 模型代理 + NATS 消费 | 身份 `org_id+worker_id` 静态绑定 |
| 内嵌调度 worker | `scheduler.mode=process` | server fork 子进程 | 单机/开发形态 |
| 容器调度 worker | `scheduler.mode=docker-cli` | shell 调 docker CLI | 依赖宿主 docker |
| K8s 调度 worker | `scheduler.mode=k8s` | 创建 Deployment | **当前有栈溢出缺陷，不可用**（见 §8） |
| NATS / PG / 对象存储 | docker-compose / Helm | 单实例 | 无集群/无 HA |

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
          5 createTaskSession                   （task 级会话）
          6 附件落盘 uploads/_task/<taskID>/...
          7 resolveProjectAssistantWorker       （assistant + worker 路由）
          8 resolveWorkerTaskModel              （组织默认模型）
          9 resolveProjectPluginSnapshots       （项目绑定的插件快照）
         10 PostMessage（事务：消息 + ReliableTask Outbox）
              └─ ReliableTaskDispatcher（1s 扫描）→ NATS cmd.run
```

要点：
- 一次「新任务」实际创建 **Project + Task + Session + Message** 四件套，返回四个 public_id（`contract/work_type.go:NewMessageResponse`）。
- 任务标题默认取消息前 50 字符；`task_type=general`。
- 派发不在 HTTP 请求内直接发 NATS，而是写 **Outbox**（`leros_reliable_task`）后异步发布，避免「入库成功、消息丢失」。

### 4.2 续聊 / 任务群聊：`POST /v1/AddMessage`

```
AddMessage → getSessionForCaller（org 校验 + 权限）
  → 绑定 connector / invoked skill
  → 解析 assistant_ids（只取第一个）或项目默认绑定
  → PostMessage（事务：消息 + Outbox）
  → ReliableTaskDispatcher → cmd.run
```

要点：
- Session 类型：`chat` / `task` / `project`。任务群聊 = 多真人 + 一个 AI 队友；`SenderUin/SenderName` 记录真人身份。
- 历史上下文只注入「当前 assistant 上一条回复之后」的增量消息（`message_poster.go:1026`），避免重复注入。
- `assistant_ids` 是**路由覆盖**，不是多助手群发；`firstOrDefault` 决定唯一目标。

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
      └─ SessionRunStateProjector
            ├─ run.started   → 建/更新 run 记录
            ├─ artifact.declared → 产物入库
            └─ 终态 → CompleteSessionMessage（消息内容/Chunks/Usage/Artifacts）
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
- **自动化任务**的 Task 状态会随 run 事件流转；手动任务不会。

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
 │    ├─ Task ── Session (task) ── SessionMessage
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
- OSS：本地 JWT；token 只携带 `uin`，组织归属每次请求从 `leros_user_org` 查。
- Enterprise：IAM 签发 claims，本地映射。
- 全局闸门只有 `RequireCallerOrg`（`OrgID>0 && State==Succ`）。
- `LEROS_DEV=true` 直接返回 `Uin=1, OrgID=1`（`middleware/identify.go:80`）——生产误配即关闭鉴权。

**授权**
- 三层：动作矩阵（`types/permission.go`，6 类资源 / 38 action）→ 策略（`permission_policy.go:SystemPolicy`）→ 绑定与父链继承（`Resource` + `ResourceBinding`，`MaxRole`）。
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

| 维度 | `leros`（native） | `claude` | `codex` | `opencode` |
|---|---|---|---|---|
| 形态 | 进程内 Eino 循环 | CLI 子进程 stream-json | `codex app-server` JSON-RPC | ACP over stdio |
| 模型接入 | Eino（Anthropic/OpenAI 兼容） | 环境变量注入 | 生成 config.toml | `OPENCODE_CONFIG_CONTENT` |
| Leros 工具 | 全部 6 类（进程内） | 仅 3 个（MCP） | 仅 3 个（MCP） | 仅 3 个（MCP） |
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
| docker-compose | PG 17 + NATS 2.12 + server + worker | 单机部署 / 演示 |
| Helm (k8s) | PG 18 + NATS 1 副本 + server 1 副本 + 动态 worker Deployment | 私有化（需先修 k8s 调度器） |
| 本地开发 | `make dev-setup` + `dev-server/dev-worker/dev-frontend` | 开发 |
| CLI 单机 | `leros chat` / `session` / `task` | 轻量使用/调试 |

### 7.2 硬限制

- NATS 单副本、无跨节点复制；宕机 = 全平台停摆。
- PG 单实例；无 Operator/副本。
- server `replicas=1 + Recreate`；后台循环无 leader 选举 → **无法直接水平扩展**。
- worker workspace 用 hostPath（k8s）→ 绑定节点、不可漂移。
- 队列上限 `MaxQueuedCommands=1000`、stream `DiscardOld` → 过载静默丢最旧消息。
- 无 DLQ、无 `MaxDeliver` 上限 → poison message 可无限重投。

### 7.3 关键配置面

`config.Config`：`server`（port/jwt/disable_event_consumers）、`nats`、`database`、`llm`、`scheduler`（process/docker-cli/k8s）、`storage`（local/s3）、`gitea`、`worker_auth`、`automation_scheduler`、`mcp_connectors`、`managed_credentials`、`payroll_proxy`、`feishu`、`auth`（企业 IAM）。

---

## 8. 代码事实 vs 产品叙事

| 叙事（README/文档） | 代码事实 | 结论 |
|---|---|---|
| 「多 Agent 调度，拆解工作流，多个子 Agent 并行」 | 单消息单 assistant；`firstOrDefault`；无 fan-out/聚合 | ⛔ 未实现 |
| 「内置 MCP Server，对外暴露 Lework 运行时能力」 | 仅 worker `:8081/v1/mcp`，3 个工具，供 CLI runtime 使用；token 为空时放行 | 🟠 内部桥，不是对外平台 |
| 「知识库沉淀上下文」 | 仅 `KnowledgeType` 常量；前端 `/knowledge` 隐藏重定向 | ⛔ 未实现 |
| 「成本追踪（按任务/项目/团队）」 | `llm_history` 有 token/延迟/业务键；无 cost/单价/聚合报表 | 🟡 仅用量 |
| 「RBAC 三级（用户/队友/技能）」 | project/task 完整；assistant/plugin 部分；org/user 无 guard | 🟡 部分 |
| 「多租户完全隔离」 | `org_id` 应用层过滤；NATS 单凭据；`MaxOrgsPerUser=1` | 🟡 逻辑隔离 |
| 「审批工作流」 | 四 runtime 语义不一致；native 无审批 | 🟡 部分 |
| 「私有化部署（k3s + Helm）」 | `scheduler.mode=k8s` 无限递归；单副本 | 🟠 需修复 |
| 「渠道接入 GitHub/GitLab/企微」 | GitHub 有完整实现但未注册；GitLab 空壳；企微空目录 | ⛔ 未接线 |
| 「技能库」 | Plugin/Revision/Content/项目绑定/marketplace 完整 | ✅ 已实现 |
| 「自动化」 | cron/interval + execution + 项目代数重建 | ✅ 已实现 |
| 「任务驱动协作」 | 任务可建、可群聊；普通任务状态不随 Run 流转 | 🟡 半闭环 |
| 「文件版本管理」 | 项目文件版本 + 预览 + 恢复 | ✅ 已实现（缺文件夹打包下载） |
| 「桌面端」 | Electron 39 + 自动更新 | ✅ 已实现 |

---

## 9. 风险登记（按决策影响排序）

### P0 — 交付/安全准入门槛

| # | 风险 | 证据 | 影响 |
|---|---|---|---|
| 1 | `LEROS_DEV=true` 完全绕过鉴权 | `middleware/identify.go:80` | 误配即全量越权 |
| 2 | K8s 调度器 `deploymentsAPI()` 无限递归 | `scheduler/kubernetes_scheduler.go:52-57`；构造器不设 `deployments` | k8s 模式首次调用栈溢出 |
| 3 | 模型 API Key 明文落库 | `llm/manager_db.go:22,38,317,484`（列名 `api_key_encrypted` 误导） | DB 泄露即密钥泄露 |
| 4 | Worker 管理端点无鉴权 | `worker/server/server.go:39-43` 挂在根路径 | 可匿名关停/创建 worker |
| 5 | MCP 端点在 token 为空时放行 | `worker/mcp/auth.go:33-38` | 任意人可写技能/记忆/产物声明 |
| 6 | 默认管理员 `admin@leros.local / Admin123456` | `seed/account_oss.go` | 未改密即已知口令 |
| 7 | CORS 任意 Origin + credentials | `middleware/cors.go` | 任意站点可访问 API（Bearer 降低 CSRF，但仍过宽） |
| 8 | 连接器/模型凭证明文 | `plugin_definition.go`、`MCPChannel.Headers/AuthConfig` | 私有化交付审计风险 |

### P1 — 正确性/一致性

| # | 风险 | 影响 |
|---|---|---|
| 9 | 取消意图不持久化；inbox 无 `cancelled` | worker 崩溃后取消丢失，取消被记成 failed |
| 10 | control/interaction/file lane 失败仍 Ack | 指令静默丢失 |
| 11 | stream `DiscardOld` + 无 DLQ | 终态/命令可能静默丢失，run 永久 running |
| 12 | 普通 Task 状态不随 Run 更新 | 看板/验收流程不可信 |
| 13 | 双重成员体系（ProjectMember + ResourceBinding） | 双写漂移，权限判断分歧 |
| 14 | 组织/用户接口无权限 guard | 任意登录用户可尝试越权管理 |
| 15 | `GuardByPublicID` 仅支持 project/task | 前端批量鉴权对 file/artifact/assistant 报错 |
| 16 | 启动即执行破坏性迁移 | 回滚困难；大租户启动慢 |
| 17 | DAO 部分 `GetByPublicID` 不带 org | 绕过 service 即越权面 |
| 18 | native 与 CLI 安全模型相反 | 同一命令在不同 runtime 行为不同 |

### P2 — 架构/成本

| # | 风险 | 影响 |
|---|---|---|
| 19 | 每队友 1 worker | 成本随队友数线性增长 |
| 20 | 无容量感知路由 | 全部压到 `worker_id=1` |
| 21 | server 单副本 + 后台循环无锁 | 无法水平扩展 |
| 22 | 三套模型接入路径（Eino / CLI SDK / llmprotocol） | 统一化成本高 |
| 23 | 逐 delta 归档 | 存储放大 |
| 24 | 群聊/SSE 两段式（GlobalEvents → SessionEvents） | 前端状态机复杂、易竞态 |
| 25 | Swagger 覆盖率约 60% | 三方对接困难 |
| 26 | 遗留 WS（`/v1/ws/client`、`/ws/worker`）与 NATS 双通道 | 维护负担与安全面 |

---

## 10. 后续决策清单

> 每个决策给出：现状 → 选项 → 建议。建议以「当前代码事实 + 产品承诺」为依据，不代表唯一解。

### D1. 产品定位：做「AI 队友平台」还是「Agent 执行平台」？

- **现状**：主链路围绕 Project/Task/Session；但也暴露 CLI、MCP、模型代理、Payroll 垂直代理等平台化能力。
- **选项**：
  - A. 收敛为「AI 队友协作平台」：强化项目/任务/验收/资产，弱化底层平台叙事。
  - B. 收敛为「Agent 运行时/网关」：强化 runtime、MCP、模型代理，弱化业务域。
  - C. 双轨：上层队友平台 + 下层运行时对外输出。
- **建议**：短期选 A（现有 UI/数据模型都围绕它），把 B 作为内部能力而非对外产品；C 需要先解决 runtime 能力契约与多租户隔离，成本最高。

### D2. Runtime 策略

- **现状**：native 是唯一全工具、无审批的「内核」；claude/codex/opencode 是能力各异的 CLI 封装；服务端不设置 runtime，由 worker 默认值决定。
- **选项**：
  - A. native 优先：CLI runtime 仅作为实验能力。
  - B. CLI 优先：把 native 降级为兜底。
  - C. 能力契约 + 显式选择：为每个 runtime 声明 `tools/approval/plan/mcp` 能力，产品层按能力路由，不支持则明确报错。
- **建议**：C。先在数据模型上允许「AI 队友选择 runtime/模型」，再补 capability matrix；否则同一队友换 worker 就换行为，无法做 SLA。

### D3. Worker 拓扑与成本

- **现状**：`WorkerDeployment.digital_assistant_id` 唯一，1 队友 1 worker；无容量路由；k8s 调度器不可用。
- **选项**：
  - A. 保持 1:1，卖「专属队友」。
  - B. 按 org/runtime 池化 worker，多队友共享。
  - C. 混合：默认池化，付费/高隔离客户 1:1。
- **建议**：C。先实现「池化 + 按 runtime 分组 + 容量上报」，保留 1:1 作为高隔离档；同时修复 k8s 调度器。

### D4. 多 Agent / 群聊语义

- **现状**：群聊是多真人 + 单 AI；`assistant_ids` 只取第一个；无并行、无聚合、无子 Run。
- **选项**：
  - A. 明确「一次一个队友」，把多 Agent 从叙事中移除。
  - B. 实现「@ 多个队友 → 并行 Run → 汇总消息」。
  - C. 实现「主 Agent 委派子 Agent」的编排层。
- **建议**：先 A（诚实叙事），若市场强需求再做 B（成本可控：fan-out + 聚合消息 + 并发槽隔离）；C 需要新的编排模型，不建议在当前 Task 模型上硬加。

### D5. 安全与私有化门槛

- **现状**：§9 的 P0 全部可复现。
- **选项**：
  - A. 逐项修复后再交付。
  - B. 以「内网可信」为前提，用部署文档约束。
- **建议**：A。最小修复集：移除/隔离 `LEROS_DEV`、KMS 加密模型与连接器凭据、worker 管理面加 token、MCP 强制 token、默认管理员首登改密、CORS 白名单。

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

---

## 11. 建议优先级

### 0–2 周（止血）

1. 修复 `kubernetes_scheduler.deploymentsAPI()` 递归 + 构造器注入 client。
2. 安全最小集：`LEROS_DEV`、MCP token、worker 管理面 token、默认管理员改密。
3. 为 `SESSION_RUN_STREAM` / `WORKER_CMD_STREAM` 增加 `MaxDeliver` + 告警，至少避免静默丢失。
4. control/interaction/file lane 失败不 Ack。
5. 修正对外叙事（多 Agent、知识库、MCP Server、多租户）。

### 1–2 月（可用性）

6. Task 状态投影：普通 Run 的 started/completed/failed/cancelled → Task 状态。
7. 持久化取消意图（inbox `cancelled` 或 DB 取消表）。
8. Runtime capability matrix + 每队友 runtime/模型选择。
9. Worker 容量上报 + least-loaded 路由（或池化 worker PoC）。
10. 权限补齐：org/user guard、file/artifact/assistant 的 `GuardByPublicID`。

### 1 季度（架构）

11. 凭据加密（KMS/信封加密）与独立 credential store。
12. 计费/配额域（先配额，后账单）。
13. 迁移版本化 + 成员模型收敛。
14. server 水平扩展方案（后台循环 leader 选举或拆分）。
15. 知识沉淀：CoreKG/MCP 接入或自建知识库的取舍落地。

---

## 附录 A：关键文件索引

| 主题 | 文件 |
|---|---|
| 路由装配 | `backend/internal/api/router.go` |
| 鉴权中间件 | `backend/internal/api/middleware/{identify,auth,cors}.go` |
| 会话/SSE | `backend/internal/api/handler/session_handler.go`、`service/session_service.go` |
| 消息编排 | `backend/internal/service/message_poster.go` |
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
| 前端状态/API | `frontend/packages/store/{appStore.ts,api/client.ts,chat/*}` |
| 前端页面 | `frontend/apps/web/app/(shell)/*`、`frontend/packages/app-ui/` |

## 附录 B：统计口径与复核方法

- 代码规模：`find backend -name '*.go' | wc -l` / `xargs wc -l`。
- 表：`backend/types/tables.go` 中 `TableName*` 常量 43 个；`runMigrations` 的 models 41 个。
- 路由：非测试文件中 `.GET/.POST/.PUT/PATCH/DELETE(` 注册点约 157 处（含 worker 与 modelrouter）。
- 测试：`find backend -name '*_test.go' | wc -l` = 266。
- Runtime：`backend/agent/runtime/{native,claude,codex,opencode}`。
- 本文所有「未实现/不一致」结论均已在对应文件核对；未运行完整 `go test ./...`，成熟度判断基于代码结构、测试文件形态与显式 TODO/probe 标记。

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
| Projector | 消费 run 事件并投影到 DB/消息的组件 |
| ACP | Agent Client Protocol，opencode runtime 使用的 stdio JSON-RPC 协议 |
