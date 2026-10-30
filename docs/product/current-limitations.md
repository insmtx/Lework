# 当前能力边界（2026-10）

> 基线：`AIO` 分支，2026-10-31。本文以当前代码实现为准；README 中偏市场叙事的「多 Agent / 知识库 / 成本」等能力，凡未在本文「今天可用」列出的，均属于路线图。

## 今天可以稳定演示

| 能力 | 当前实现 |
|---|---|
| 项目 | 创建项目、项目成员、项目会话、项目文件 |
| 任务 | 创建任务记录、标题/描述、预期产出、验收标准，指派 `assignee_id`；任务列表与详情 |
| 会话执行 | 任务已指派队友时由 assignee 执行，否则由项目绑定的 AI 队友执行；一条消息只路由一个 assistant |
| 运行时 | `native` / `claude` / `codex` / `opencode`，需环境内安装对应 CLI |
| 产物 | Run workspace 下的 `.leros/artifacts`、`artifact_declare` 工具、文件上传/下载；任务详情聚合产物与变更文件 |
| 技能 | 内置技能、技能安装/关联、MCP 连接器（如 CoreKG） |
| 验收闭环 | 普通任务 Run 成功后进入 `pending_acceptance`；任务详情可「通过」或「驳回」，驳回会向任务会话发送返工消息 |
| 自动化 | cron / interval 自动化任务；其状态随 Run 事件投影，cron 结束直接 `completed` |
| 部署 | 单机 AIO（server + 内置 worker + PostgreSQL + NATS + Nginx 同源 Web） |

## 尚未实现，不要对外承诺

| 能力 | 事实 |
|---|---|
| 首次启动向导 | 仍依赖环境变量 seed 管理员；M1 的 `/setup` 向导未实现 |
| 示例项目 / Golden Demo | 代码闭环已具备，但 AIO 实机全链路演示尚未验证 |
| 多 Agent 并行 / 工作流 | 未实现；`firstOrDefault` 只选一个 assistant |
| 内置知识库 | 无写入/检索链路；只有外部 MCP（如 CoreKG）知识检索 |
| AI 队友长期记忆 | `DigitalAssistantMemory` / `PromptBlock` 有 DAO 无生产写入方；真实记忆是 Run 级 `.leros/memory` 文件 |
| 成本 / 配额 / 余额 / 账单 | 只记录 token usage 与 `llm_history`，无成本核算与配额 |
| 多组织切换 / 租户强隔离 | 有组织数据模型，但共享单节点资源，无租户级配额与执行环境隔离 |
| 高可用 | NATS/PostgreSQL 单实例；server 单副本；后台循环无 leader 选举 |
| Kubernetes 调度 | `k8s` 调度器已删除；只支持 `process` / `docker-cli` |
| 官方插件市场 | 代码存在，但依赖外部服务配置；核心可用路径是内置技能 |
| GitHub/GitLab/WeCom webhook 连接器 | 未在路由注册，已从主干删除 |
| 资源库 | 仅有占位页，已删除 |

## 安全默认值

- 生产环境必须设置 `LEROS_ADMIN_PASSWORD`，否则首次 seed 拒绝创建管理员；开发环境保留 `Admin123456`。
- `LEROS_DEV=true` 不再跳过鉴权；本地免鉴权必须显式设置 `LEROS_DEV_ALLOW_UNAUTH=true`，且请求来自回环地址、`LEROS_ENV` 不是 `production`。
- MCP 未配置 token 时拒绝请求；worker 会自动生成随机 token 并注入运行时。
- CORS 默认只允许 `LEROS_PUBLIC_URL` / `LEROS_CORS_ALLOWED_ORIGINS` 白名单；同源 Web 不需要跨域头。
- **模型 API Key 仍以可逆形式入库**（待修）；公开部署前请限制数据库访问。

## 部署事实

- AIO 使用 worker 镜像同时运行 server；server 通过 `scheduler.mode: process` 在容器内拉起 `/leros worker`，不使用 Docker socket。
- 数据默认在 `deployments/aio/data/app`；PostgreSQL/NATS 使用命名卷。
- AIO 的镜像地址默认指向 `registry.yygu.cn`；开源用户需要通过 `.env` 的 `LEROS_IMAGE` / `LEROS_WEB_IMAGE` 指向自己构建或发布的镜像。
- 当前开发机没有 Docker Compose，AIO 编排只做了 YAML 解析与脚本 `bash -n` 校验，未做整机启动验证。

## 验证状态

- `go build ./...`、`go build -tags enterprise ./...` 通过。
- `go test ./...` 全绿。
- 前端四个包 typecheck 通过；`pnpm -r test` 全绿（app-ui 318、web 3、desktop 40）。
- M1 核心闭环（任务验收字段、assignee 路由、Run 状态投影、验收/驳回 API、任务详情 UI）已验证；AIO 整机 Golden Demo 仍需实机启动。

## 相关文档

- [As-Built 产品架构](../architecture/2026-10-30-product-architecture-as-built.md)
- [核心产品开发计划](2026-10-30-core-product-development-plan.md)
