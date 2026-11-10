# Lework 模块优先级（2026-11-01）

> 口径：以当前 `Lework` 工作区代码为准。本文回答一个问题：**哪些模块真正支撑「项目 → 任务 → AI 队友执行 → 产物 → 验收」闭环，哪些只是必要支撑，哪些应当冻结或移出主干。**
>
> 核心闭环：
>
> `项目上下文 → 建任务（预期产出 + 验收标准）→ 派给 AI 队友 → Run 在工作区执行 → 产物 → 人验收/驳回 → 复用`
>
> 判定规则：
>
> - **P0**：删掉它，闭环断裂、产品不可用或不可安全交付。
> - **P1**：删掉它，闭环仍能跑通，但真实使用会明显受限；属于必要支撑或差异化增强。
> - **P2**：删掉它，闭环不受影响；当前阶段投入产出低，主要是叙事、运营、分发或企业定制。

---

## 1. 一页结论

| 优先级 | 模块组 | 一句话判断 |
|---|---|---|
| **P0** | Agent Runtime（native）+ Worker Run 流水线 | 真正让 AI 队友干活的执行内核 |
| **P0** | Workspace + Git + Artifact | 产物不是聊天记录，而是可验收的真实文件 |
| **P0** | Task / Message / Assignee | 决定「谁来做、做什么、在哪个上下文做」 |
| **P0** | Run/Session/Task 投影 + SSE | 让执行过程、状态和验收触发可观测 |
| **P0** | 任务验收（通过/驳回/返工） | 产品区别于「AI 聊天框」的关键闭环 |
| **P0** | 核心工具（node / artifact_declare / todo） | Agent 能读写文件、执行命令、声明产物 |
| **P0** | 模型接入 + 项目/组织/认证/数据库/NATS/启动 | 没有这些，Agent 无法运行或无法安全隔离 |
| **P1** | 多 Runtime、技能/插件、MCP、自动化、审批、文件版本、模型管理、RBAC、CLI、用量 | 必要但不决定第一版闭环 |
| **P2** | 官方插件市场、反馈/飞书、桌面/自动更新、遥测、品牌/私有化设置、部门树、多组织、知识库、计费、公共模型代理、未闭环的 DB 记忆、垂直投标/政府技能 | 与核心业务关系弱，建议冻结或移出主干 |

**一句话**：Lework 的 P0 不是「更多 Agent 能力」，而是**把一次任务可靠地执行完，并交付可验收的产物**。P1 才是能力扩展，P2 大多是可延后的产品叙事。

---

## 2. P0：核心闭环与 Agent 执行内核

### 2.1 P0-A：Agent 真正解决问题的模块

| 模块 | 代码位置 | 实际解决什么问题 | 判断 |
|---|---|---|---|
| **Native Runtime** | `backend/agent/runtime/native/` | 进程内 Agent 循环、模型调用、工具调用、NodeEvent 输出 | 唯一不依赖外部 CLI、工具最完整的执行内核；**默认路径必须守住** |
| **Agent 契约层** | `backend/agent/{runtime,executor,registry,node_event,tool,interaction}.go` | Runtime 抽象、事件协议、工具契约、审批/提问接口 | 执行内核与业务解耦的地基；不要让业务逻辑渗入 |
| **Worker Run 流水线** | `backend/internal/worker/agentrun/` | prepare（工作区/技能/历史/prompt/工具/MCP/模型）→ execute → finalize（diff/artifact/commit）→ journal | 真正的执行编排；一个 Run 一个终态 |
| **命令接入与调度** | `backend/internal/worker/command/`、`backend/internal/worker/run/` | admission、SQLite inbox 幂等、debounce、同会话串行、并发槽、公平轮转 | 决定「任务会不会丢、会不会并发写坏工作区」 |
| **Workspace** | `backend/internal/workspace/` | 项目 Git 仓库、运行目录、路径约束、记忆目录 | Agent 的「真实工作台」；没有它产物不可信 |
| **Git 与产物** | `backend/internal/infra/git/`、`agentrun/finalizer_impl.go`、`tools/artifact_declare/` | clone/pull、diff、commit/push、artifact 上传、`artifacts.jsonl` | 产物可追溯、可下载、可验收；这是相对聊天产品的核心差异 |
| **项目与上下文** | `service/project_service.go`、`project_file_handler.go`、project session | 项目成员、项目文件、项目会话、项目记忆 | 任务是项目内的执行单元，不是孤立聊天 |
| **任务与派发** | `service/task_service.go`、`service/message_poster.go`、`service/worker_resolution.go` | 任务字段、assignee、项目绑定、消息路由、Outbox 派发 | 决定「派给谁就由谁执行」；M1 已打通 |
| **状态投影** | `internal/runnable/{session_run_state_projector,task_run_state_projector}.go` | run.started/completed/failed/cancelled → 会话/消息/任务状态 | 没有它，任务卡在 `in_progress`，验收无法触发 |
| **SSE 实时反馈** | `worker/eventpub/`、`handler/session_handler.go`、`GlobalEvents` | run 增量、工具调用、审批/提问、终态推送到前端 | 长任务必须可观察，否则用户不知道 Agent 是否卡死 |
| **任务验收** | `service/task_service.go` 的 `GetTaskDeliverables` / `AcceptTask` / `RejectTask`、`TaskAcceptancePanel.tsx` | 聚合会话 artifact 与项目文件；通过/驳回/返工 | 产品闭环的最后一环；**当前仍有 `UpdateTask` 绕过验收的缺口，需收紧** |
| **核心工具** | `backend/tools/node/`、`artifact_declare/`、`todo/` | 读/写/终端/进程、产物声明、运行进度 | node + artifact 是 P0；todo 是 P0 可观测性的低成本补充 |
| **模型接入** | `backend/internal/modelrouter/`、`backend/internal/llm/` | 组织默认模型、调用、用量记录 | Agent 的「大脑」；两套接入路径需要收敛，但能力本身是 P0 |

### 2.2 P0-B：不做就无法交付的工程底座

| 模块 | 代码位置 | 作用 | 判断 |
|---|---|---|---|
| **认证与组织隔离** | `api/middleware/{identify,auth}.go`、`adapter/account/oss/` | 登录、JWT、`org_id` 隔离 | P0；开发免鉴权必须保持三条件 |
| **权限基础** | `service/permission_*`、project/task guard | 项目/任务的最小访问控制 | 基础隔离 P0；细粒度 RBAC 放 P1 |
| **数据库与迁移** | `internal/infra/db/` | GORM、AutoMigrate、回填/清理 | P0；启动迁移的破坏性风险需继续收敛 |
| **NATS / Outbox** | `internal/infra/mq/`、`ReliableTaskDispatcher` | 命令投递、事件流、Outbox | P0；丢消息等于任务丢失 |
| **启动与种子** | `internal/seed/`、`cmd/lework/` | 管理员、默认组织、默认模型、内置技能 | P0；首次启动向导 `/setup` 是待补的可用性缺口 |
| **AIO 部署** | `deployments/aio/` | 单机 Compose 全栈 + 同源网关 | P0 的交付形态；当前尚未整机验证 |
| **文件存储** | `internal/infra/filestore/`、presigned URL | 上传、artifact 下载、预览 | P0 的最小文件能力；版本管理放 P1 |

> P0 的边界要克制：**凡是不能直接帮助「任务执行完并验收」的能力，都不应挤进 P0。**

---

## 3. P1：必要，但不是核心能力

| 模块 | 代码位置 | 价值 | 为什么不是 P0 | 建议 |
|---|---|---|---|---|
| **多 Runtime（claude / codex / opencode）** | `backend/agent/runtime/{claude,codex,opencode}/` | 复用用户已装的 CLI、获得更强编码/计划能力 | 依赖外部二进制，能力/审批/工具语义不一致；native 已能跑通闭环 | 保留为「高级选项」；只维护 native + 1 个 CLI，避免四套同时承诺 |
| **技能 / 插件** | `internal/skill/`、`tools/skill_use`、`tools/skill_manage`、`plugin_*` | 复用方法论、项目级能力包 | 没有技能也能用 node 工具完成任务；技能是质量/效率增强 | **P1 里最接近 P0 的一项**；先把「项目绑定 + 运行快照」打磨稳定 |
| **MCP 连接器** | `internal/worker/mcp/`、MCP platform/OAuth/binding | 接入外部工具/知识源 | 当前 native 不支持项目 MCP；CLI 才走 `/v1/mcp` 桥 | P1；不要让 MCP 成为第一版必配项 |
| **自动化（cron / interval）** | `service/automation_*` | 定时任务、周期性巡检/报表 | 核心闭环是「人派任务」，自动化的状态语义还需与验收区分 | P1；cron 直接 completed 的语义要明确 |
| **审批 / 提问** | `worker/runtimehost/`、SessionEvents、approval UI | 危险操作确认、补充信息 | native 目前没有审批路径；CLI 才有 | P1；至少保证 CLI runtime 的安全边界 |
| **文件版本 / 预览 / 打包下载** | `projectfile/`、`project_file_handler.go`、预览组件 | 产物可回滚、可消费 | 核心闭环只需要 artifact 可下载；版本与预览是增强 | P1；优先补「文件夹打包下载」 |
| **模型管理** | `service/llm_model_service.go`、`/org/models` | 配置 provider、默认模型、测试连接 | 没有 UI 也能用配置/种子模型；核心是模型可调用 | P1；保留最小 CRUD，不做复杂路由 |
| **RBAC / 细粒度权限** | `service/permission_*`、`BatchCheckPermission` | 多人协作、资源级授权 | 单机/小团队用项目成员 + org 隔离已能跑 | P1；先补 org/user guard 与 file/artifact/assistant 的 `GuardByPublicID` |
| **Worker 调度 / 部署对账** | `worker/scheduler/`、`service/worker_*` | process/docker-cli 拉起 worker、期望态对账 | AIO 只需 `process`；docker-cli 依赖宿主 docker | `process` 属于 P0 底座；docker-cli 与复杂对账放 P1 |
| **CLI** | `cmd/lework/`、`internal/cli/` | 开发者入口、脚本化 | Web 已覆盖主流程；CLI 是效率工具 | P1；保持 chat/task/session 基本命令即可 |
| **用量统计** | `llm_history`、`/llm/usage` | token/延迟/业务键可观测 | 没有它不影响执行；成本核算另属 P2 | P1；先做用量，不做计费 |
| **AI 队友模板** | `AITeammateTemplate`、模板 handler/UI | 冷启动、降低配置成本 | 手写一个队友也能跑通 | P1；模板数据可留，模板市场不做 |
| **办公技能包** | `backend/skills/worker/{docx,pdf,pptx,xlsx}` | 生成真实文档交付物 | 依赖较重，但不是 Agent 内核 | P1；按需安装，不默认全量 |

---

## 4. P2：外围、叙事或投入产出低

| 模块 | 代码位置 | 现状/价值 | 为什么是 P2 | 建议 |
|---|---|---|---|---|
| **官方插件市场** | `official_plugin_marketplace_handler.go`、`/plugin-marketplace/*` | 依赖外部运营与供给 | 开源版没有持续供给，核心可用路径是内置技能 | 冻结或移出主干；保留本地技能/可选仓库 |
| **反馈 / 飞书** | `feedback_service.go`、`integration/feishu/`、FeedbackDialog | 收集反馈、转发飞书 | 与任务执行无关，绑定内部运营渠道 | P2；改为 GitHub Issues 链接即可 |
| **桌面端 / 自动更新 / ClientUpdate** | `frontend/apps/desktop/`、`client_update_handler.go` | 桌面分发、版本更新 | Web 已覆盖核心闭环；桌面是分发渠道 | P2；可保留但不要让它拖慢 Web/AIO |
| **前端遥测** | `components/telemetry/` | 页面行为分析 | 不影响产品闭环，且涉及隐私/合规 | P2；默认关闭或移除 |
| **品牌 / 私有化设置** | `private-deployment/`、`PrivateServerSettingsCard` | 私有化交付定制 | 企业交付能力，不是核心 Agent 能力 | P2；留给企业版/独立仓库 |
| **部门树 / 通讯录** | `Department`、`MemberDepartment`、`/org/departments` | 组织管理完整度 | 小团队用成员列表即可；部门不是执行必需 | P2；保留数据模型，弱化 UI |
| **多组织切换 / 创建** | `MaxOrgsPerUser=1`、`/org` | 目前单组织 | 未实现，且与单机 AIO 定位冲突 | P2；不要对外承诺 |
| **知识库** | 只有常量/隐藏页面 | 未实现 | 没有写入/检索链路；当前用项目文件 + MCP 替代 | P2；不要作为卖点 |
| **计费 / 配额 / 余额** | 只有 `llm_history` 用量 | 商业化能力 | 与开源核心闭环无关 | P2；先做用量报表，后做配额 |
| **公共模型代理** | `modelrouter` 的 `/v1/chat/completions` | 历史平台化能力 | 与项目/任务闭环无关，且是维护负担 | P2；冻结，不再新增功能 |
| **DB 型队友记忆 / PromptBlock 进化** | `DigitalAssistantMemory`、`DigitalAssistantPromptBlock` | 有读取路径，但缺少生产写入闭环 | 看起来像「长期记忆」，实际价值未验证；文件型 memory tool 是另一条路径 | P2；要么补闭环，要么清理叙事 |
| **垂直投标技能** | `backend/skills/server/bid-*` | 投标文件全流程 | 单一行业场景，不属于通用 AI 队友 | **P2，建议移到 `contrib/` 或示例技能包** |
| **政府认定政策技能** | `backend/skills/server/government-recognition-policy` | 政府公文写作 | 垂直行业能力 | **P2，建议移出主干** |
| **演示/天气技能** | `backend/skills/worker/weather` | Demo | 展示用，不解决核心问题 | P2；留在示例即可 |
| **工作台最近上下文 / 项目动态** | `WorkbenchRecentContext`、`ProjectActivity` | UI 便利/活动流 | 不影响执行与验收 | P2；可保留，低优先 |
| **静态路由/客户端配置** | `RegisterStaticRoutes`、GlobalRoutes | 运维/配置 | 基础设施细节 | P2；保持最小 |

---

## 5. 建议的投入顺序

### 先守 P0（不做完不要扩功能）

1. **收紧验收不变量**：禁止 `UpdateTask` 从 `in_progress` / `pending_approval` 直接改 `completed`，否则「必须验收」只是 UI 约定。
2. **AIO Golden Demo 实机跑通**：建项目 → 建任务（预期产出+验收标准）→ 派队友 → Run → 产物 → 验收/驳回 → 返工。
3. **首次启动向导 `/setup`**：干净环境创建管理员、组织、默认队友、默认模型、WorkerDeployment。
4. **模型 Key 加密 + WorkerOps 管理员校验**：这是公开部署前的安全底线。
5. **守住 native runtime + node/artifact + 状态投影**：不要为了多 Runtime 分散核心可靠性投入。

### P1 只选 1–2 个方向，不要全做

- 面向个人开发者：优先 **技能/项目上下文**，其次 **文件版本/预览**。
- 面向已有 Claude Code / Codex 用户：优先 **多 Runtime + 审批**，但明确能力矩阵。
- 面向团队：优先 **RBAC + 项目成员/活动**，自动化放后面。

### P2 先冻结，再决定删除或迁移

- 冻结：官方插件市场、反馈/飞书、遥测、品牌/私有化设置、公共模型代理。
- 迁移：`bid-*`、`government-recognition-policy` 等垂直技能移到 `contrib/` 或独立仓库。
- 清理叙事：多组织、知识库、计费、多 Agent 并行，不要在 README/产品页继续承诺。

---

## 6. 快速判别法

面对一个新模块，只问三个问题：

1. **删掉它，新用户还能完成「建任务 → 执行 → 产物 → 验收」吗？**
   - 不能 → P0。
   - 能，但明显更难用 → P1。
   - 能，且几乎无感 → P2。
2. **它是在提高「任务成功率/产物可信度」，还是在增加「平台功能数量」？**
   - 前者优先，后者延后。
3. **它是否依赖外部供给/运营/客户定制？**
   - 是 → 默认 P2，除非有明确客户合同。

> 结论：Lework 当前最该守住的是 **Agent 执行可靠性 + 真实产物 + 验收闭环**，而不是继续堆 Runtime、市场、组织管理或商业化模块。
