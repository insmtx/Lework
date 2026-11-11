# OpenCode V2 能力文档（中文整理）

本目录是 **OpenCode V2 官方文档的中文详细整理**，面向 lework 控制面的 Agent 层能力扩展：节点 agent 如何托管 OpenCode、控制面如何通过 HTTP API / SSE 观察会话、如何呈现权限与工具调用、哪些能力可以下放给 OpenCode 而控制面只做索引与转发。

> 文档基准：官方 `https://opencode.ai/docs/`（V2 文档，抓取日期 2026-11-11）。
> API 基准：官方 `openapi.json`（`title: opencode HttpApi`，139 条 path、约 500 个 schema）。
> 结论以官方英文原文与 OpenAPI 契约 为准；本文档做中文转述与工程化注解，不改变字段名、端点、事件名。

## 为什么单独建这个目录

lework 的产品定位是 **个人用户、单用户多节点的 OpenCode 控制面**。控制面只持有节点索引、事件路由与 UI 偏好；Session、消息、diff、权限、文件、工具执行都以 OpenCode 为准，**不复制成第二个状态机**。

因此团队必须准确理解 OpenCode V2 的：

- 进程与状态归属（client / server / location / session）
- HTTP API 与 SSE 事件契约
- 权限（`permissions`）与策略（`policies`）的区别
- 工具、MCP、Code Mode、子 Agent 的可观察边界
- 插件（server 插件）能改写什么、不能改写什么

这些内容分散在官方 60+ 页面中，本目录按主题合并、补充了相互引用关系与“控制面关注点”。

## 文档地图

| 文档 | 内容 | 重点读者 |
|---|---|---|
| [01-产品与架构.md](01-产品与架构.md) | 产品形态、client/server 架构、状态归属、关键路径、事件模型 | 全体 |
| [02-安装与命令行.md](02-安装与命令行.md) | 安装方式、全部 CLI 命令、serve/pair/service、CI 用法、路径 | 运维 / 后端 |
| [03-配置体系.md](03-配置体系.md) | 配置位置与合并顺序、全部顶层字段、变量替换、实验项 | 后端 / 集成 |
| [04-Agent与子Agent.md](04-Agent与子Agent.md) | Agent 文件与 JSON 定义、模式、内置 Agent、合并、子会话 | 后端 / 产品 |
| [05-模型与提供商.md](05-模型与提供商.md) | Provider/Model/Variant、请求选项、缓存、兼容性、本地模型、WebSocket | 后端 |
| [06-会话与消息模型.md](06-会话与消息模型.md) | Session/Inbox/Message、delivery、fork/move/revert、生命周期 API | 控制面后端 |
| [07-工具系统.md](07-工具系统.md) | 内建工具清单、Code Mode、browser、worktree/session 工具 | 控制面后端 / 前端 |
| [08-MCP服务器.md](08-MCP服务器.md) | local/remote MCP、OAuth、超时、协议版本、资源、命名与权限 | 集成 |
| [09-权限与策略.md](09-权限与策略.md) | `permissions` 规则与默认值、审批流、`policies` 强制约束与优先级 | 控制面后端 / 前端 |
| [10-技能-指令-命令.md](10-技能-指令-命令.md) | Skills 发现与加载、AGENTS.md、斜杠命令与参数展开 | 产品 / 后端 |
| [11-上下文管理.md](11-上下文管理.md) | 压缩、快照与 undo、references、附件、warming、输出截断 | 控制面后端 |
| [12-HTTP-API参考.md](12-HTTP-API参考.md) | 认证、响应包裹、错误、SSE、按资源分组的完整端点表 | 控制面后端 |
| [13-客户端-SDK-ACP.md](13-客户端-SDK-ACP.md) | `@opencode/client`、`@opencode/sdk`、ACP 协议细节 | 集成 / 编辑器 |
| [14-插件开发.md](14-插件开发.md) | 插件包结构、server context、transforms、hooks、工具注册、RPC | 高级集成 |
| [15-控制面Agent层集成指南.md](15-控制面Agent层集成指南.md) | 落到 lework：节点侧、控制面侧、事件/权限/文件/PTY 方案与边界 | 架构 / 控制面 |
| [16-故障排查与运维.md](16-故障排查与运维.md) | service、日志、heap、CORS、代理、证书、升级与卸载 | 运维 |
| [17-V1到V2迁移.md](17-V1到V2迁移.md) | 破坏性变更、配置/文件/插件/API 迁移对照表 | 后端 |
| [18-Console与Go.md](18-Console与Go.md) | OpenCode Console、workspace、组织策略、Go 订阅、harness | 产品 / 运维 |
| [19-控制面能力对齐清单.md](19-控制面能力对齐清单.md) | 逐项对照：已接管 / 部分 / 未接管 / 不接管，含缺口优先级 | 架构 / 控制面 |
| [附录A-配置字段速查.md](附录A-配置字段速查.md) | 顶层配置字段一页速查 | 全体 |
| [附录B-事件与权限动作速查.md](附录B-事件与权限动作速查.md) | 事件类型、payload 要点、权限 action 清单 | 控制面后端 |

## 推荐阅读路径

**第一次接触 OpenCode V2**：01 → 02 → 03 → 06 → 12 → 15。

**要做控制面会话/流式 UI**：06 → 12（SSE 与端点）→ 07（工具事件）→ 09（权限审批）→ 15 → 19（当前对齐进度）。

**要做模型/Provider 管理**：05 → 03 → 12（provider/credential/integration）→ 18。

**要做插件或深度定制**：14 → 13 → 07 → 09。

**从 V1 升级**：17 → 03 → 16。

## 核心结论（先看这 10 条）

1. **OpenCode V2 是一个 client-server 系统**：TUI、Desktop、Web、`opencode run` 都连接到同一个“共享后台 server”；server 拥有 sessions、配置、插件、权限和工具执行。`opencode serve` 只是把同一个 server 放在前台运行。
2. **状态不在客户端**：会话、消息、inbox、权限请求、快照、MCP 连接都在 server 侧。客户端重连后靠快照 API + 事件流重建界面。
3. **一个 server 可服务多个 location（目录/项目）**：`/api/session`、`/api/model` 等读取接口返回 `{ location, data }`，location 由请求的目录头/参数决定；session 自身记录 `location`。
4. **提交 prompt 是“入队”而不是“同步拿回复”**：`POST /api/session/{id}/prompt` 返回 inbox item，真正的输出通过 `GET /api/event` 的 SSE 事件流获得。
5. **`delivery` 只有两个值**：`steer`（默认，插到当前执行的安全边界）与 `queue`（按队列顺序等待）。
6. **权限分两层**：`permissions` 是每 Agent 的有序规则（`allow`/`ask`/`deny`，最后匹配生效），`policies` 是二元强制约束（只有 `allow`/`deny`，从不提示，只能收紧）。企业/托管限制必须用 `policies`。
7. **默认基础权限几乎全允许**：`*/* → allow`，仅 `*.env`、`*.env.*` 读需要审批；`external_directory` 默认 **allow**（与 V1 的“默认询问”不同）。控制面如果需要审批外部路径，必须显式加规则。
8. **工具事件足够重建 UI**：`session.tool.input.started/delta/ended`、`session.tool.called/progress/success/failed`，配合 `session.text.delta` 与 `session.reasoning.delta` 可以流式渲染。
9. **压缩是“摘要 + 保留尾部”**：默认 `keep.tokens=15000`、`buffer="15%"`；压缩会丢失细节，长任务需要控制面提示用户或调大保留量。快照（undo）依赖 Git，且只覆盖会话活动目录。
10. **V2 与 V1 的破坏性变更集中在三处**：插件 API、server API/client、终端客户端配置（`tui.json` → 全局 `cli.json`）。配置字段大多兼容，但 `permission→permissions`、`bash→shell`、`task→subagent` 等必须按 17 号文档迁移。

## 与 lework 的边界（务必遵守）

| 事项 | 归属 | 说明 |
|---|---|---|
| Session / Message / Tool 状态 | OpenCode server | 控制面只缓存索引与 UI 偏好，不建第二状态机 |
| 权限审批决策 | OpenCode server + 客户端回复 | 控制面转发 `permission.asked` 并调用 reply 接口 |
| 文件读写 / diff / 快照 | OpenCode server | 控制面通过 `/api/fs/*`、`/api/vcs/*`、`/api/session/{id}/diff` 读取 |
| 节点注册、心跳、WSS 隧道 | lework 控制面 + agentd | OpenCode 不感知控制面 |
| OpenCode 进程生命周期 | 节点 agent (`agentd`) | 单节点上按 location/项目托管，参考 `backend/internal/agentd/` |
| 组织级限制 | OpenCode `policies`（可托管下发） | 不要用控制面私有的“伪权限”替代 |

> 本目录中的“控制面关注点”只是工程建议，不改变 OpenCode 的契约；实现时以 `backend/internal/agentd/opencode` 与官方 OpenAPI 为准。

## 维护约定

- 官方文档更新后，优先核对：`config`、`permissions`、`policies`、`api`（OpenAPI）、`cli/commands`、`migrate-v1`。
- 端点/字段以 `openapi.json` 为准；文档页面的叙述可能滞后于 schema。
- 文件名保持中文描述性命名（本目录是第三方资料整理，例外于 `docs/README.md` 的 kebab-case 规则）。
- 新增文件后同步更新上表，并在 [docs/README.md](../README.md) 的索引中登记。
