# Lework 核心产品开发计划

> 版本：v1.0（2026-10-30）
> 基线：`1458925a`
> 配套：`docs/architecture/2026-10-30-product-architecture-as-built.md`（As-Built 事实）
> 状态：**待决策**。三项决策见 §12，确认后按 §4 执行。

---

## 0. 一页结论

### 0.1 产品定义

**Lework = 可自托管的「项目制 AI 队友工作台」。**

在项目里给 AI 队友派任务，它在真实的 Git 工作区里执行，交付可验收的产物。

不是通用聊天工具，不是工作流编排器，不是多 Agent 编排平台，也不是企业 OA。

### 0.2 唯一必须成立的闭环

```
建项目（目标 + 上下文 + 仓库）
  → 建任务（预期产出 + 验收标准）
    → 派给 AI 队友（assignee 决定执行者）
      → Run 在独立工作区执行
        → 产出 artifact / Git diff / 文档
          → 人验收（通过 / 驳回）
            → 任务完成，产物留在项目里
              → 下一次任务复用项目上下文与技能
```

这条闭环里的每一步都必须是**真实的系统行为**，不是 UI 叙事。当前代码只完成了「执行」，没有完成「委托」与「验收」。

### 0.3 90 天目标

| 时间 | 里程碑 | 结果 |
|---|---|---|
| 第 1–2 周 | M0 减法与底座 | 主干只保留核心闭环；`docker compose up` 可用；CI 绿 |
| 第 3–5 周 | M1 可信闭环 | 任务可分配、可执行、可验收；首个产物 < 10 分钟 |
| 第 6–7 周 | M2 开源发布 | v0.1.0 发布；公开镜像；Quickstart、Demo、社区模板齐备 |
| 第 8–11 周 | M3 留存与差异化 | 队友级技能、记忆沉淀、产物评审、MCP 连接器 |
| 第 12 周起 | M4 企业/商业化 | 企业能力剥离到 `enterprise/` 或独立仓库 |

### 0.4 成功标准（不是 Star 数）

1. **10 分钟首个产物**：陌生人从 `git clone` 到拿到第一个可验收产物 ≤ 10 分钟。
2. **每周验收交付物数**（North Star）：每个活跃项目每周被「验收通过」的产物数。
3. **激活率**：完成首次安装的用户中，≥ 40% 在 24 小时内完成一次「任务 → 产物 → 验收」。
4. **留存**：次周仍创建任务的用户 ≥ 25%。
5. **工程可信**：`go test ./...` 绿；CI 全绿；无 P0 安全洞；无死路由/死页面。

### 0.5 三件需要拍板的事

1. **License**：当前 LICENSE 不是 OSI 认可的开源许可证，与「现象级开源」直接冲突。见 §12.1。
2. **删除范围**：Payroll 等垂直模块是否还有在线客户。见 §12.2。
3. **目标用户**：先服务个人开发者/小团队，还是先服务企业。见 §12.3。

---

## 1. 产品定位与竞争切口

### 1.1 一句话定位

> 开源的、可自托管的「AI 队友团队」：在你的仓库里干活，交付可 review 的产物，而不是只回一段话。

### 1.2 目标用户（建议）

**第一阶段：AI 原生的小团队与独立开发者。**

- 3–20 人的产品/研发团队；
- 已经在用 Claude Code / Codex / Cursor，但想要「团队共享的项目上下文 + 任务分派 + 产物验收」；
- 愿意自托管，愿意自带模型 Key；
- 对数据主权敏感，不想把代码交给 SaaS。

**明确不服务（第一阶段）：**

- 需要 SSO/审计/多租户隔离的大型企业；
- 需要低代码工作流编排的业务人员；
- 需要「全自动无人值守多 Agent 公司」的幻想型需求。

### 1.3 竞争切口

| 对手 | 它强在哪 | Lework 的差异 |
|---|---|---|
| Claude Code / Codex CLI | 单机执行力强 | 项目制、多人协作、任务与验收、产物归档、自动化 |
| Devin / 云端 Agent | 托管体验好 | 开源自托管、自带模型、无 vendor lock-in |
| Dify / Flowise | 工作流编排 | 不做流程图；做真实工作区里的任务执行 |
| 传统项目管理 | 任务/验收完整 | AI 直接执行并产出可验收产物 |

**切口一句话：把「AI 编码助手」升级成「项目里的 AI 队友」，关键是任务、工作区、产物、验收四件事连起来。**

---

## 2. 范围裁决：保留 / 剥离 / 删除

### 2.1 裁决原则

1. **只保留能强化 §0.2 闭环的东西。**
2. **垂直行业能力、云端运营能力、企业治理能力，一律不进核心仓库。**
3. **不能开箱即用的运行时，不进默认路径。**
4. **删除优先于隐藏。** 隐藏的死代码会继续消耗维护与认知成本。
5. **有在线客户的功能不硬删，迁到 `contrib/` 或独立分支，主干不再编译。**

### 2.2 删除 / 剥离清单

| # | 模块 | 现状证据 | 为什么与产品逻辑不相干 | 处置 | 影响 |
|---|---|---|---|---|---|
| 1 | Payroll 垂直应用 | `router.go:153`、`handler/payroll_proxy.go`、`layoutSlice.ts:765` | 单一客户会计场景，甚至污染了通用消息发送链路 | 删除 handler/service/config/UI；如客户仍需，迁 `contrib/payroll` 独立仓库 | 中 |
| 2 | 官方插件市场 | `official_plugin_marketplace_handler.go`、`/plugin-marketplace/*` | 需要云端运营与供给，开源版没有 | 移出核心；核心只保留本地技能包与内置技能 | 中 |
| 3 | GitHub / GitLab / WeWork 连接器 | `backend/internal/api/connectors/*`，未在 `SetupRouter` 注册 | 死代码；与核心闭环无关 | 删除；GitHub 以后以 MCP 或插件形式回归 | 低 |
| 4 | 旧客户端 WebSocket | `router.go:119`、`infra/websocket/*` | 与 SSE 双通道重复，客户端已走 POST-SSE | 删除路由与实现 | 低 |
| 5 | Worker 管理 WebSocket | `worker/server/conn.go`、`/ws/worker` | 运维面重复且鉴权薄弱 | 保留 HTTP ops（加权限），删 WS | 低 |
| 6 | 知识库占位 | `app/(shell)/knowledge/page.tsx`、nav 注释 | 无模型、无后端、无入口 | 删除页面与类型；需要时用项目文件 + MCP | 低 |
| 7 | 飞书反馈 | `feedback_service.go`、`/SubmitFeedback` | 绑定内部运营渠道 | 改为 GitHub Issues 链接；保留可选 webhook | 低 |
| 8 | 西电演示账密 | `deploy-config.ts:66`、`AuthDialog.tsx:351` | 客户私有化残留，且是明文凭据 | 删除 | 低 |
| 9 | k8s 调度 | `scheduler/kubernetes_scheduler.go` | 未验证，`deploymentsAPI()` 无限递归 | 默认移除；如保留必须标 experimental 并修复 | 中 |
| 10 | 多组织切换/创建 | `MaxOrgsPerUser=1`、`ErrNotImplementedEdition` | UI 是死路 | 单组织部署；删除切换/创建入口与 API | 低 |
| 11 | CLI 运行时默认路径 | `runtime/claude|codex|opencode` | 依赖外部二进制，开箱不可用 | native 默认；CLI 运行时作为「高级选项」 | 中 |
| 12 | modelrouter 公共代理 | `/v1/chat/completions` | 与产品闭环无关，且是历史包袱 | **冻结**，不新增功能；M3 再评估迁 `internal/llm` | 高 |
| 13 | 网易邮箱等垂直技能 | `skills/connectors/connector-netease-mail`（3.5MB） | 垂直技能，不属于核心 | 移到可选技能包 | 低 |
| 14 | 文档虚假承诺 | README、产品页 | 多 Agent、知识库、验收均未实现 | 改写为「已实现 / 路线图」两栏 | 低 |

### 2.3 保留并加倍投入

| 能力 | 理由 |
|---|---|
| 项目 + Git 工作区 | 真实产物与可追溯性的根基 |
| 原生 Runtime | 唯一开箱即用的执行内核 |
| Task / Session / Run | 委托与执行的骨架 |
| Artifact 采集 | 价值载体，不是聊天气泡 |
| Skill / Plugin（本地） | 方法复用与差异化 |
| MCP | 连接真实工具生态 |
| Automation | 从一次性执行变成长期运行 |
| SSE 实时通道 | 客户端唯一需要的实时面 |
| process / docker 调度 | 自托管可落地 |
| 权限（简化版） | 多人协作的最低保障 |

### 2.4 删除后的仓库形态

```
backend/
  cmd/leros/                 # 唯一入口
  agent/
    runtime/native/          # 默认
    runtime/{claude,codex,opencode}/  # 可选，检测不到就隐藏
  internal/
    api/                     # 只保留核心路由
    service/                 # 项目/任务/会话/运行/技能/自动化
    worker/                  # 调度、执行、事件
    runnable/                # 投影器
    infra/                   # db/mq/filestore/git
  tools/                     # 核心工具
  types/
contrib/                     # 垂直/实验能力，不参与默认构建
enterprise/                  # 商业能力（build tag）
skills/                      # 内置技能包
frontend/
  apps/web/                  # 唯一 Web 客户端
  packages/                  # store / app-ui / ui
deployments/
  quickstart/                # 公开镜像的一键部署
docs/
```

---

## 3. 目标架构与对象模型

### 3.1 运行时链路（目标态）

```
Web / CLI
  │  POST /v1/NewMessage
  ▼
Server
  ├─ 解析 session → task → assignee（优先级：任务 assignee > 显式 assistant_ids > 项目默认）
  ├─ 权限校验（项目成员 + assistant use）
  ├─ 事务写 message + outbox + command
  └─ 发布 NATS
        ▼
Worker
  ├─ 领取 command，解析 runtime（默认 native）
  ├─ 准备 workspace（clone/pull）
  ├─ 生成 context（项目目标 + 任务预期产出/验收标准 + 技能快照 + 记忆）
  ├─ 执行，实时发 run.stream / run.state
  └─ 终态：commit/push + artifact 声明 + assistant message
        ▼
Server 投影器
  ├─ session_run_state_projector：消息、artifact、会话状态
  ├─ task_run_state_projector：任务状态（新增）
  └─ automation_run_projection：自动化执行记录
```

### 3.2 核心对象（目标态）

```
Org
 └─ Project（目标、Git 工作区、成员、技能绑定）
     ├─ Task（预期产出、验收标准、assignee、状态）
     │   └─ Session 1:1
     │       └─ Run 1:N
     │           └─ Artifact 1:N
     ├─ ResourceBinding（user / assistant 成员）
     └─ ProjectPluginBinding（技能/插件 revision 快照）

DigitalAssistant
 ├─ SystemPrompt
 ├─ WorkerDeployment 1:1
 ├─ 默认模型（组织级）
 └─ （M3）AssistantPluginBinding（队友级技能覆盖）
```

### 3.3 任务状态机（目标态）

```
created ──► in_progress ──► pending_acceptance ──► completed
   │             │                   │
   │             │                   └──► in_progress（驳回，带意见）
   │             ├──► failed
   │             └──► cancelled
   └──► cancelled
```

规则：

- `run.started` → `in_progress`（仅从 created / pending_assignment / paused 进入）。
- `run.completed` → 普通任务 `pending_acceptance`；cron 任务 `completed`（自动化默认信任）。
- `run.failed` → `failed`；`run.cancelled` → `cancelled`。
- `completed` 只能由「验收通过」进入；`pending_acceptance` 只能由人操作离开。
- 迟到的 run 事件不得覆盖人工终态。

### 3.4 验收模型（目标态）

最小字段，不引入新聚合：

| 字段 | 类型 | 说明 |
|---|---|---|
| `ExpectedOutput` | text | 预期产出，创建任务时填写 |
| `AcceptanceCriteria` | text | 验收标准，可空 |
| `ResultSummary` | text | 执行结束时的结果摘要 |
| `AcceptedBy` | bigint? | 验收人 |
| `AcceptedAt` | timestamptz? | 验收时间 |
| `AcceptanceNote` | text | 验收/驳回意见 |

产物本身不复制到任务表：通过 `session_id → messages → artifacts` 与项目文件查询。

---

## 4. 里程碑与排期

> 假设：2 名后端、1 名前端、1 名兼职设计/文档。可按人力等比缩放。

| 里程碑 | 周期 | 主题 | 退出标准 |
|---|---|---|---|
| M0 | 2 周 | 减法与开源底座 | 主干可编译、CI 绿、公开镜像 compose 可起、无死路由/死页面 |
| M1 | 3 周 | 可信闭环 | Golden Demo 全程跑通：建项目→建任务→派队友→执行→产物→验收 |
| M2 | 2 周 | 开源发布 | v0.1.0；10 分钟 Quickstart；公开 Demo；社区模板 |
| M3 | 4 周 | 留存与差异化 | 队友级技能、记忆沉淀、产物评审、MCP 连接器 |
| M4 | 持续 | 企业与商业化 | 企业能力全部在 `enterprise/` 或独立仓库，核心保持开源 |

---

## 5. M0：减法与开源底座（第 1–2 周）

### T0.1 License 与品牌（阻塞项）

**现状**：`LICENSE` 是「Apache 2.0 + 附加条件」，限制多租户 SaaS 并要求保留 LOGO。这不是 OSI 认可的开源许可证。

**动作**：

1. 由项目所有者选择 §12.1 的方案。
2. 若选 Apache-2.0：
   - 用标准 Apache-2.0 全文替换 `LICENSE`；
   - 新增 `NOTICE`、`TRADEMARK.md`（LOGO 使用规则，不写进许可证）；
   - README 顶部加许可证徽章。
3. 若选 AGPL-3.0：
   - 替换 `LICENSE` 为 AGPL-3.0；
   - 在 README 明确「网络服务使用也需开源」；
   - 企业版走商业许可。
4. 全仓搜索并移除「Open Source License」自定义表述。

**验收**：GitHub 识别为标准许可证；`LICENSE` 与 README 一致。

### T0.2 公开镜像与一键启动

**现状**：`deployments/env/docker-compose.yml` 使用 `registry.yygu.cn` 私有镜像，开源用户无法启动。

**动作**：

1. 新增 `deployments/quickstart/docker-compose.yml`：
   - `postgres:17`
   - `nats:2.12`
   - `ghcr.io/insmtx/leros:latest`（server + worker 两个服务）
   - 健康检查、命名卷、`.env` 注入。
2. 新增 `deployments/quickstart/.env.example`：
   - `LEROS_DB_DSN`、`LEROS_NATS_URL`、`LEROS_SECRET_KEY`、`LEROS_PUBLIC_URL`
   - 模型 Key 不在 env 里写死，走首次启动向导。
3. `Makefile` 增加：
   - `make quickstart`：复制 env、`docker compose up -d`、等待健康检查、打印访问地址。
   - `make quickstart-down`。
4. 镜像发布到 GHCR；打 `v*` tag 自动构建 `linux/amd64,linux/arm64`。

**验收**：干净机器执行 `make quickstart`，3 分钟内看到登录/初始化页。

### T0.3 执行 §2.2 删除清单

**动作**（按表逐项）：

1. Payroll：删 `handler/payroll_proxy.go`、`handler/payroll_proxy_test.go`、`RegisterPayrollProxyRoutes`、config 字段、`layoutSlice.ts` 中 payroll 分支、web/desktop 路由与页面。
2. 插件市场：删 `official_plugin_marketplace_handler.go` 及测试、路由注册；保留 `builtin_skill_marketplace_seed.go` 中内置技能种子（如仍被本地目录使用）。
3. 连接器：删 `backend/internal/api/connectors/{github,gitlab,wework}`；确认无引用。
4. 客户端 WS：删 `infra/websocket/*`、`router.go` 中 `RegisterWebSocketRoutes` 调用。
5. Worker WS：删 `worker/server/conn.go` 与 WS 路由；保留 `worker/server/server.go` 的 HTTP ops 并加权限。
6. 知识库：删 `app/(shell)/knowledge/page.tsx`、`layoutSlice.ts` 的 `knowledge` 类型与 nav 注释。
7. 飞书：删 `feedback_service.go`、`feedback_summarizer.go`、`feedback_handler.go`；前端「反馈」改为 GitHub Issues 链接。
8. 西电：删 `deploy-config.ts` 中 Xidian 常量与 `readXidianDemoLogin`；删 `AuthDialog.tsx` 的预填逻辑与测试。
9. k8s：删 `kubernetes_scheduler.go` 与 `modeK8s`；`config/scheduler.go` 注释只保留 process/docker。
10. 多组织：删前端切换/创建入口；API 返回明确「单组织部署」错误；保留 `org_id` 数据模型。
11. 连接器技能：把 `skills/connectors/connector-netease-mail` 移到 `contrib/skills/`，默认不打包。
12. 文档：见 T0.6。

**验收**：

```bash
go build ./...
go vet ./...
go test ./...
cd frontend && pnpm -r typecheck && pnpm -r build
```

全绿；`grep -ri "payroll|xidian|plugin-marketplace|connector-netease"` 在主干无命中（README 历史说明除外）。

### T0.4 移除默认管理员与开发鉴权后门

**现状**：默认 `admin@leros.local / Admin123456`；`LEROS_DEV=true` 直接放行鉴权。

**动作**：

1. 删除生产 seed 中的默认管理员；seed 仅保留 dev 环境脚本。
2. `LEROS_DEV` 不再等价于「跳过鉴权」；新增显式 `LEROS_DEV_ALLOW_UNAUTH=true`，且仅允许 `127.0.0.1`。
3. 登录接口加失败限流（按 IP + 账号）。
4. 首次启动向导见 T1.6；在向导完成前，管理 API 全部拒绝。

**验收**：生产模式无默认账号；`LEROS_DEV=true` 不再绕过鉴权；安全测试覆盖。

### T0.5 安全最小修复集（公开前必须）

| 项 | 文件 | 修复 |
|---|---|---|
| Worker 管理端点无鉴权 | `router.go` `RegisterWorkerOpsRoutes` | 要求管理员权限；写操作二次确认 |
| MCP token 为空放行 | `worker/mcp/auth.go` | 空 token 一律拒绝；常量时间比较 |
| 模型 Key 明文 | `internal/llm/manager_db.go` | AES-GCM 加密；`LEROS_SECRET_KEY` 派生；启动时迁移 |
| CORS 任意 Origin + credentials | `middleware/cors.go` | 配置白名单；默认只允许 `LEROS_PUBLIC_URL` |
| 文件下载/上传路径 | `filestore` | 路径穿越校验；内容类型白名单 |
| 默认管理员 | seed | 见 T0.4 |

**验收**：OWASP 基础检查通过；无明文密钥落库；CORS 预检只回显白名单 Origin。

### T0.6 文档真实性整改

**动作**：

1. README 拆成「今天能做什么」与「路线图」两栏。
2. 删除/标注以下未实现叙事：
   - 多 Agent 并行/工作流；
   - 知识库；
   - 结构化验收；
   - 成本/配额/计费；
   - 多组织切换。
3. 新增 `docs/product/current-limitations.md`，列出单节点、单组织、单 assistant、无计费等事实。
4. `docs/README.md` 索引本计划。

**验收**：README 的每个「能做」都能在 10 分钟内演示；无夸大。

### T0.7 CI 基线

新增 `.github/workflows/ci.yml`：

```yaml
jobs:
  backend:
    - gofmt -l .
    - go vet ./...
    - go build ./...
    - go test ./...
  frontend:
    - pnpm install --frozen-lockfile
    - pnpm -r typecheck
    - pnpm -r test
    - pnpm -r build
  docker:
    - docker build -f deployments/build/Dockerfile.leros .
```

**验收**：PR 必须全绿才能合并；主分支保护开启。

### T0.8 基线与回滚

1. 打 tag `pre-core-refactor`。
2. 所有删除在一个 PR/分支 `refactor/core-scope` 完成，便于整体回滚。
3. 记录删除前后的 `go build` 时间、二进制大小、测试数量。

---

## 6. M1：可信闭环（第 3–5 周）

> 目标：让 §0.2 闭环在代码里成立。M1 结束时的 Golden Demo 是发布素材，也是验收脚本。

### T1.1 任务模型扩展

**文件**

- `backend/types/task.go`
- `backend/types/constants.go`
- `backend/internal/api/contract/task_type.go`
- `backend/internal/service/task_service.go`

**改动**

```go
// types/task.go
ExpectedOutput     string     `gorm:"column:expected_output;type:text"`
AcceptanceCriteria string     `gorm:"column:acceptance_criteria;type:text"`
ResultSummary      string     `gorm:"column:result_summary;type:text"`
AcceptedBy         *uint      `gorm:"column:accepted_by;type:bigint"`
AcceptedAt         *time.Time `gorm:"column:accepted_at"`
AcceptanceNote     string     `gorm:"column:acceptance_note;type:text"`
```

```go
// types/constants.go
TaskStatusPendingAcceptance TaskStatus = "pending_acceptance"
```

Contract 增加同名字段；`CreateTaskRequest` / `UpdateTaskRequest` 增加 `expected_output`、`acceptance_criteria`。

**迁移**：GORM AutoMigrate 自动加列，无需 `legacyColumns`。旧任务 `expected_output` 为空，UI 显示「未填写」。

**测试**

- 创建任务带预期产出与验收标准，回读一致。
- 更新任务只改允许字段。
- 非法状态迁移被拒绝（`completed → in_progress` 仅允许驳回路径）。

**验收**：`go test ./backend/internal/service -run Task` 绿。

### T1.2 Assignee 成为真正的执行路由

**现状**：`assignee_id` 只存不用；路由取 `assistant_ids` 或项目默认。

**文件**

- `backend/internal/service/task_service.go`（CreateTask / UpdateTask）
- `backend/internal/service/message_poster.go`（NewMessage 路由）
- `backend/internal/service/worker_resolution.go`
- `backend/internal/infra/db/resource_binding_dao.go`

**改动**

1. `CreateTask` / `UpdateTask` 设置 `assignee_id` 时：
   - 校验 assistant 属于当前 org 且 `Status=active`；
   - 校验调用者有 `ActionAssistantUse`；
   - 在同一事务内 `CreateResourceBinding(project resource, assistant, member)`，已存在则忽略。
2. `NewMessage` 解析 assistant 时增加优先级：

```go
// 伪代码
func (p *MessagePoster) resolveRoutingAssistantIDs(ctx, session, requested []uint) ([]uint, error) {
    if session.TaskID != nil {
        task, _ := db.GetTaskByID(ctx, p.db, *session.TaskID)
        if task != nil && task.AssigneeID != nil && *task.AssigneeID > 0 {
            return []uint{*task.AssigneeID}, nil
        }
    }
    return requested, nil
}
```

3. `resolveProjectAssistantWorker` 保持项目成员校验；assignee 必须在项目里（T1.2.1 已保证）。
4. 若 assignee 的 worker deployment 不存在/未就绪，返回可读错误：「队友 X 的 Worker 未就绪，请先启动 Worker」。

**测试**

- 项目默认队友 A，任务 assignee=B，发消息 → `command.assistant_id=B`。
- assignee 不是项目成员 → 自动绑定后成功。
- assignee 非 active → 400。
- assignee worker 未就绪 → 409，错误信息含队友名。

**验收**：Golden Demo 中「派给队友 B」真的由 B 执行。

### T1.3 Run → Task 状态投影

**文件**

- 新增 `backend/internal/runnable/task_run_state_projector.go`
- `backend/internal/runnable/runner.go`（启动注册）
- `backend/internal/runnable/automation_run_projection.go`（保持 cron 语义）

**改动**

1. 新消费者 `task_run_state_projector`，订阅 `messaging.RunEventStateWildcard()`，独立 durable。
2. 事件到达时：
   - `runEvent.Trace.TaskID` 取任务 public_id；
   - 任务不存在 → ack 丢弃；
   - 独立 `ProjectionReceipt`（consumer=`task-run-state-projector`）保证幂等。
3. 状态映射：

| 事件 | 任务类型 | 新状态 |
|---|---|---|
| `run.started` | 全部 | `in_progress`（仅从 created/pending_assignment/paused） |
| `run.completed` | general | `pending_acceptance` |
| `run.completed` | cron | `completed`（保持现状） |
| `run.failed` | 全部 | `failed` |
| `run.cancelled` | 全部 | `cancelled` |

4. 终态保护：`completed` / `cancelled` 不被迟到事件覆盖；`pending_acceptance` 不被 `run.started` 回退。
5. 在 `runner.go` 启动两个 projector；避免与 `session_run_state_projector` 共用 consumer 名。

**测试**

- 表驱动覆盖全部事件 × 状态。
- 重复事件只生效一次（receipt）。
- 迟到 `run.started` 不覆盖 `pending_acceptance`。
- cron 任务仍直接 completed（现有自动化测试不回归）。

**验收**：手动任务跑完后，任务列表自动变「待验收」。

### T1.4 产物查询与验收 API

**文件**

- `backend/internal/api/handler/task_handler.go`
- `backend/internal/api/contract/task_type.go`
- `backend/internal/service/task_service.go`
- `backend/internal/api/router.go`
- `backend/internal/infra/db/session_message_dao.go`（如需）

**新增接口**

```http
GET /v1/GetTaskDeliverables?task_id=<public_id>
→ { artifacts: [...], files: [...], git_commit: "...", result_summary: "..." }

POST /v1/AcceptTask
{ "task_id": "...", "note": "..." }
→ { task }

POST /v1/RejectTask
{ "task_id": "...", "note": "必须补充测试" }
→ { task }
```

**行为**

- `GetTaskDeliverables`：
  - 取 task.session_id；
  - 聚合该会话全部 assistant 消息中的 artifacts；
  - 聚合项目文件中被本次 run 修改的文件（按 `updated_at >= run.started_at` 近似，M1 可接受）；
  - 返回 `git_commit`（取 run 终态记录）。
- `AcceptTask`：仅 `pending_acceptance` 可调用；写 `completed`、`accepted_by`、`accepted_at`、`acceptance_note`。
- `RejectTask`：仅 `pending_acceptance` 可调用；写 `in_progress`、`acceptance_note`；随后自动向任务会话发一条用户消息：
  ```
  【验收未通过】<note>
  请根据以上意见修改并重新交付。
  ```
  走正常 `NewMessage` 路径，触发下一次 Run。这样「驳回 → 返工」形成闭环。

**测试**

- 非待验收状态调用验收 → 409。
- 驳回后任务变 in_progress 且产生一条新消息。
- 越权验收 → 403。
- 产物聚合不重复。

**验收**：Golden Demo 可以点「通过」与「驳回」，驳回会触发返工。

### T1.5 任务详情 UI

**文件**

- `frontend/packages/app-ui/components/task/*`（新增 `DeliverablesPanel.tsx`、`AcceptanceBar.tsx`）
- `frontend/apps/web/app/(shell)/...` 任务详情页
- `frontend/packages/store/slices/taskSlice.ts`（或现有 task store）
- `frontend/packages/store/api/*`

**改动**

1. 任务详情顶部：状态徽章、assignee、预期产出、验收标准。
2. 交付物区：artifact 卡片（预览/下载）、变更文件列表、Git commit。
3. 底部验收条：`通过` / `驳回`（驳回必须填意见）。
4. 非 `pending_acceptance` 状态隐藏验收条。
5. 空状态文案：「队友还没有交付，去会话里催一下」。

**测试**：RTL 覆盖状态渲染、通过/驳回调用、驳回意见必填。

**验收**：Golden Demo 全程在 UI 完成，不需要 curl。

### T1.6 首次启动向导

**文件**

- `backend/internal/api/handler/setup_handler.go`（新增）
- `backend/internal/service/setup_service.go`（新增）
- `backend/internal/api/router.go`
- `frontend/apps/web/app/setup/page.tsx`（新增）
- `frontend/packages/store/api/setup.ts`

**接口**

```http
GET /v1/GlobalConfig
→ { needs_setup: true, ... }

POST /v1/Setup
{
  "email": "...", "password": "...", "org_name": "...",
  "model": { "provider": "openai|anthropic|deepseek|ollama", "base_url": "...", "api_key": "...", "model": "..." }
}
```

**规则**

- 仅当 `COUNT(users)==0` 时允许；否则 409。
- 创建：管理员用户、组织、默认 AI 队友、默认模型、默认 WorkerDeployment（状态 pending，等 worker 注册）。
- `api_key` 加密存储（T0.5）。
- 支持 `LEROS_SETUP_TOKEN`：设置后，Setup 请求必须带 token，防止公网被抢注。
- 前端三步：管理员账号 → 模型 → 完成（自动创建示例项目可选）。

**测试**

- 首次 Setup 成功；第二次 409。
- 无 token 且配置了 token → 401。
- 弱密码拒绝。

**验收**：干净环境打开首页自动跳 `/setup`，3 分钟完成。

### T1.7 示例项目与 Golden Demo

**动作**

1. 新增 `examples/golden-demo/`：
   - 一个最小 Git 仓库（README + 一个待修 bug + 一个失败测试）；
   - `demo.md`：演示脚本。
2. Setup 完成页提供「创建示例项目」勾选，自动导入。
3. 演示脚本（必须 5 分钟内跑完）：
   1. 创建项目「Golden Demo」，目标「修复 parse 函数的边界 bug」；
   2. 创建任务「修复空输入崩溃」，预期产出「修复 + 单测」，验收标准「`go test ./...` 通过」；
   3. 指派给默认队友；
   4. 在会话里发送「开始」；
   5. 观察实时事件；
   6. 查看 diff 与 artifact；
   7. 点「通过」，任务 completed。
4. 录制 60–90 秒 GIF，放 README 首屏。

**验收**：三个不同的人按文档独立跑通，耗时 ≤ 10 分钟。

---

## 7. M2：开源发布（第 6–7 周）

### T2.1 README 与 Quickstart

- 首屏：一句话 + GIF + 「3 条命令启动」。
- 三段式：
  1. 它是什么（项目制 AI 队友）；
  2. 5 分钟跑通（compose + setup + demo）；
  3. 与其他工具的区别（对比表）。
- 文档：
  - `docs/quickstart.md`
  - `docs/self-hosting.md`
  - `docs/concepts/{project,task,run,artifact,skill}.md`
  - `docs/architecture/`（保留 As-Built）
  - `docs/faq.md`
- 中文为主，英文 README 同步（至少 README + quickstart）。

### T2.2 发布工程

- `goreleaser`：linux/darwin/windows，amd64/arm64，checksums。
- GHCR 多架构镜像，tag：`latest`、`vX.Y.Z`、`sha-<short>`。
- `docker compose` 使用固定版本，不依赖私有 registry。
- 版本注入：`leros version` 输出 commit/date/edition。
- 数据库迁移在启动时自动执行；发布说明标注破坏性变更。

### T2.3 社区与治理

- `CONTRIBUTING.md`：开发环境、分层规则、提交规范、PR 流程。
- `CODE_OF_CONDUCT.md`。
- `SECURITY.md`：漏洞上报、支持版本。
- Issue 模板：bug / feature / question；PR 模板含测试清单。
- `ROADMAP.md`：M3/M4 公开。
- 10 个 `good first issue`：
  - 文档错别字、i18n、Docker 健康检查、日志、错误文案、前端空状态等。
- Discussions 开启；Discord/微信群二选一（建议 Discord + GitHub Discussions）。

### T2.4 发布素材

- 60–90 秒 Demo 视频（无配音，字幕）。
- 3 张架构图：产品闭环、运行时链路、部署形态。
- 对比表：vs Claude Code / Devin / Dify。
- 「为什么开源」博客：数据主权、BYO model、可审计产物。
- 发布渠道：HN Show HN、Reddit r/selfhosted & r/LocalLLaMA、X、V2EX、即刻、少数派。

### T2.5 发布门槛（Go/No-Go）

- [ ] `go test ./...` 绿，CI 绿。
- [ ] 干净机器 10 分钟跑通 Golden Demo。
- [ ] 无 P0 安全洞（T0.5 全部关闭）。
- [ ] License 为 OSI 认可。
- [ ] 公开镜像可拉取。
- [ ] README 无未实现承诺。
- [ ] 至少 3 个外部用户提前试跑并反馈。

---

## 8. M3：留存与差异化（第 8–11 周）

### T3.1 队友级技能绑定

**问题**：技能绑在项目上，「同一个队友」跨项目能力不同。

**方案**：

- 新表 `leros_digital_assistant_plugin_binding`（assistant_id, plugin_id, revision_id, enabled）。
- Run 准备技能时合并：项目绑定 ∪ 队友绑定；同 code 以队友绑定为准。
- UI：队友详情 → 技能页，可添加/禁用。
- API：`GET/POST/DELETE /v1/DigitalAssistantPlugins`。

**验收**：同一队友在两个项目里拥有相同基础技能；项目可追加。

### T3.2 记忆沉淀闭环

**问题**：`DigitalAssistantMemory` / `PromptBlock` 无生产写入方；「越用越强」不成立。

**方案（先项目、后队友）**：

1. Run 终态后，异步生成「交付摘要」：
   - 输入：任务描述、验收标准、最终回复、变更文件列表；
   - 输出：3–5 条事实/决策；
   - 写入项目工作区 `.leros/memory/project.md`（走现有 local memory store）。
2. 下次 Run 的 system prompt 注入项目记忆（已有 `BuildPromptBlock` 路径）。
3. 队友记忆先做「手动提升」：任务详情提供「记住这条」，写入 `DigitalAssistantMemory` + `DigitalAssistantPromptBlock`。
4. 自动提取队友记忆放 M4，避免早期噪声。

**验收**：连续两个任务，第二个任务能引用第一个任务的结论。

### T3.3 产物评审体验

- Diff viewer（按文件、按行）。
- Run 时间线：开始 → 工具调用 → 文件变更 → 完成。
- Artifact 预览：Markdown、图片、PDF、docx（能预览的预览，不能的下载）。
- 「重新运行」按钮：带上次上下文重跑。

### T3.4 MCP 连接器

- OAuth / Token 两种接入。
- 连接器目录：GitHub、Notion、Postgres、Filesystem 四个即可。
- 连接测试 + 权限说明。
- 移除「平台自动连接」这类垂直逻辑。

### T3.5 用量与成本可见

- 按项目/队友/模型聚合 token 用量。
- 估算成本（按模型单价），只展示，不做计费。
- 明确「不自带余额，BYO key」。

### T3.6 自动化模板

- 3 个模板：每日站会摘要、每周竞品简报、PR 巡检。
- 一键创建 + 预览 cron。
- 执行记录与产物关联。

---

## 9. M4：企业与商业化边界

**原则：核心仓库保持开源；企业能力不进默认构建。**

| 能力 | 去向 |
|---|---|
| SSO / IAM / SCIM | `enterprise/`（build tag）或独立仓库 |
| 多组织 / 租户隔离 / RLS | 企业版 |
| 细粒度 RBAC / 审计日志 | 企业版 |
| 配额 / 计费 / 发票 | 企业版或云服务 |
| k8s / HA / 多区域 | 企业版 |
| 官方技能市场 | 云服务 |
| 托管版（SaaS） | 独立产品 |

企业版通过商业许可分发；核心 API 保持稳定，企业版以适配器方式接入。

---

## 10. 风险与回滚

| 风险 | 概率 | 影响 | 缓解 |
|---|---|---|---|
| 删除 Payroll 影响在线客户 | 中 | 高 | 先确认 §12.2；迁 `contrib/` 或独立分支；主分支打 tag 可回滚 |
| License 变更引发贡献者异议 | 中 | 中 | 在 CONTRIBUTING 说明；保留历史 tag；必要时发公告 |
| 任务状态投影与自动化冲突 | 中 | 中 | 独立 consumer + 独立 receipt；cron 保持原语义；表驱动测试 |
| assignee 路由破坏现有会话 | 中 | 高 | 仅对 `session.task_id != nil` 生效；保留显式 `assistant_ids` 覆盖；灰度开关 |
| 首次向导被公网抢注 | 低 | 高 | `LEROS_SETUP_TOKEN`；完成后关闭 Setup |
| 模型 Key 加密迁移失败 | 低 | 高 | 双读兼容；迁移失败不阻塞启动；日志告警 |
| 开源发布后无人维护 | 中 | 高 | good first issue、CI、明确 roadmap；至少 1 名固定维护者 |
| 范围再次膨胀 | 高 | 高 | 本计划的「不做清单」写入 CONTRIBUTING；新功能必须映射到闭环 |

**回滚策略**：

- M0 删除：整分支回滚 + `pre-core-refactor` tag。
- M1 数据：只加列，不删列；回滚代码不影响数据。
- M1 投影：消费者可通过配置关闭（`LEROS_DISABLE_TASK_PROJECTION=true`）。
- License：保留历史 `LICENSE` 于 `docs/legal/`，发布说明记录变更。

---

## 11. 排期与人力

### 11.1 关键路径

```
T0.1 License ─┐
T0.2 镜像   ─┼─► T0.3 删除 ─► T0.5 安全 ─► T0.7 CI ─┐
T0.4 默认账号 ┘                                      │
                                                     ▼
T1.1 任务模型 ─► T1.2 路由 ─► T1.3 投影 ─► T1.4 验收 API ─► T1.5 UI
                                                     │
T1.6 向导 ─► T1.7 Demo ──────────────────────────────┘
                                                     ▼
T2.1 文档 ─► T2.2 发布 ─► T2.3 社区 ─► T2.5 Go/No-Go ─► v0.1.0
```

### 11.2 人力分配（建议）

- 后端 A：T1.1 / T1.3 / T1.4 / T0.5 后端部分。
- 后端 B：T1.2 / T1.6 / T0.3 后端删除 / T0.2。
- 前端：T1.5 / T1.6 前端 / T0.3 前端删除。
- 设计/文档（兼职）：T2.1 / T2.4 / Demo GIF。
- 所有者：T0.1 License、§12 决策、发布对外。

### 11.3 每周节奏

- 周一：计划与风险。
- 周三：Golden Demo 手动跑一遍（从 M1 开始）。
- 周五：CI 绿 + 演示 + 文档更新。
- 每两周：砍需求，不加需求。

---

## 12. 待决策（阻塞执行）

### 12.1 License

| 方案 | 优点 | 缺点 | 建议 |
|---|---|---|---|
| A. Apache-2.0 核心 + 商标政策 + 商业企业版 | 最大采用率；GitHub 识别标准；企业友好 | 云厂商可白嫖 | **推荐**，若目标是「现象级」 |
| B. AGPL-3.0 核心 + 商业企业版 | 阻止闭源 SaaS 白嫖 | 部分企业直接排除 | 若 SaaS 保护优先 |
| C. 保留当前自定义许可证 | 保护现有商业 | 不是开源；社区不认；GitHub 标签难看 | 不推荐 |

**需要回答**：核心是 Apache-2.0 还是 AGPL-3.0？

### 12.2 删除范围

**需要回答**：Payroll 是否还有在线客户/合同？

- 有 → 迁 `contrib/payroll` 或独立仓库，主干删路由与 UI。
- 没有 → 直接删除。

同样确认：官方插件市场、GitLab/WeWork 连接器、飞书反馈、k8s 调度是否有外部使用者。

### 12.3 目标用户

**需要回答**：第一阶段主打谁？

- A. 独立开发者/小团队（推荐）：强调 5 分钟上手、BYO key、项目产物。
- B. 中大型企业：强调权限、审计、私有化、多组织。
- C. 两者都要：本计划会失焦，不建议。

### 12.4 发布节奏

**需要回答**：是否接受「M1 完成即发 v0.1.0，M2 只做发布工程」？

- 是 → 更快拿到反馈，M3 按反馈调整。
- 否 → 等 M2 完整再发，风险是闭门造车。

---

## 13. 附录：Golden Demo 验收脚本

> 这份脚本是产品逻辑的最终验收集。任何一项失败，M1 不算完成。

1. 干净机器：`git clone` → `make quickstart` → 打开 `http://localhost:3000`。
2. 首次向导：创建管理员 → 配置模型（OpenAI/Anthropic/DeepSeek/Ollama 任一）→ 完成。
3. 创建项目「Golden Demo」，目标：「修复 parse 函数的边界 bug」。
4. 创建任务：
   - 标题：修复空输入崩溃；
   - 预期产出：修复代码 + 单元测试；
   - 验收标准：`go test ./...` 通过；
   - 指派：默认队友。
5. 进入任务会话，发送「开始」。
6. 观察：
   - 任务状态 `created → in_progress`；
   - 实时事件可见工具调用；
   - 工作区出现变更；
   - 终态后任务变 `pending_acceptance`；
   - 交付物区出现 diff + artifact。
7. 点「驳回」，填写「补充边界测试」：
   - 任务回到 `in_progress`；
   - 会话出现验收意见；
   - 队友再次执行。
8. 点「通过」：
   - 任务 `completed`；
   - 验收人/时间/意见落库；
   - 产物仍在项目里可查。
9. 新建第二个任务，确认它能读到项目记忆（M3 后）。
10. 重启 server/worker，任务状态与产物不丢。

---

## 14. 变更记录

| 日期 | 版本 | 变更 |
|---|---|---|
| 2026-10-30 | v1.0 | 初稿：基于 As-Built 架构与核心产品逻辑，给出减法清单、M0–M4 计划与验收脚本 |
