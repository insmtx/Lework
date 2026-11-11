# AGENTS.md

面向在本仓库工作的 AI agent 的紧凑指南。以可执行代码/配置为准；若本文与代码冲突，以代码为准。

> 模块路径 `github.com/insmtx/lework`，Go 1.25.0（见 `go.mod`）。
>
> 产品定位：**个人用户、单用户多节点的 OpenCode 控制面**。企业级只描述工程质量与私有化能力，不是当前功能承诺。旧企业版/项目制文档在 `docs/archive/`，不要据此推断当前实现。

## 常用命令

均在仓库根目录执行。

构建与运行：
- `make build` — 先构建 `frontend/apps/control-plane` 并复制到 `backend/internal/controlplane/webui/dist/`，再生成 `bundles/lework`；需要 pnpm/Node。
- `go build -o ./bundles/lework ./backend/cmd/lework/` — 只构建后端（含 `lework serve` 与 `lework agent`，默认内建认证）。不会构建前端静态资源。
- `go build -tags enterprise -o ./bundles/lework ./backend/cmd/lework/` — 企业版构建，认证交给 IAM。见下方“构建版本”。
- `make frontend-control-plane` — 只构建控制面界面（别名 `control-plane-ui`）。
- `./bundles/lework` — 不带子命令时在同一进程内启动控制面与节点 agent；裸执行不再打印帮助，帮助入口是 `lework -h`。
- `./bundles/lework serve token` — 打印当前生效的注册令牌，文件缺失时生成到 `~/.config/Lework/enrollment.token`。
- 裸执行接受的根命令 flag：`--listen`、`--db`、`--enrollment-token`、`--token-file`、`--request-timeout`、`--heartbeat-timeout`、`--agent-config`、`--node-name`、`--workspace-root`（可重复）。
- `./bundles/lework serve --listen 127.0.0.1:8787 --db ~/.config/Lework/controlplane.db` — 启动控制面，默认只绑定回环。
- `./bundles/lework agent --config /etc/lework/agent.yaml` — 启动节点 agent；缺省配置路径按平台解析（见 `backend/cmd/lework/agent.go`）。
- `make run` / `make run-detached` / `make stop` / `make logs` — 操作 `deployments/env/docker-compose.yml` 栈。
- `make dev-setup`（一次性）→ `make dev-up`，然后 `make dev-status` / `make dev-logs` / `make dev-down`。`dev-up` 要求 PostgreSQL/NATS 已就绪且 `opencode` 2.1.x 在 PATH 中（见 Makefile 注释）。
- `make swagger` — 仅生成旧 `/v1` 的 swagger 到 `docs/swagger/`；控制面不生成 swagger。

测试：
- `go test ./...` — 默认测试，会排除 `//go:build integration` 与 `enterprise` 文件。
- `go test -p 1 ./...` — CI 实际执行的命令，串行运行以避免共享资源竞争；本地出现偶发失败时优先用它复现。
- `go test ./backend/internal/<pkg>` — 单包；`go test -run ^TestName$ ./...` — 单个测试。
- `go test -tags integration ./...` — 需要 NATS + 数据库（辅助代码在 `backend/internal/testutil/`）；`backend/internal/agentd/integration_test.go` 还需要本机 `opencode`。
- `go test -tags enterprise ./...` — 企业版测试；少数集成用例（如 `adapter/account/enterprise/enterprise_integration_test.go`）还需同时加 `-tags integration`。
- 不存在 `make test` / `make test-cover`；`CONTRIBUTING.md` 里这两条是过时写法。

提交前检查（顺序与 CI 一致）：
1. `gofmt -s -w .`（CI 用 `gofmt -l .` 检查，必须为空）。
2. `go vet ./...`。
3. `go build ./...`。
4. `go test -p 1 ./...`。
5. 前端改动：`cd frontend && pnpm install --frozen-lockfile --ignore-scripts && pnpm typecheck`（CI 使用 Node 22、pnpm 10.28.2）。
6. `golint ./...` / `staticcheck ./...`（若已安装）。

`backend/cmd/lework/architecture_test.go` 会强制进程入口不得 import `backend/agent/runtime/`；改 `cmd/lework` 时注意别破坏这条约束。

## 构建版本（build tags，最容易漏）

同一套代码用 `//go:build` 标签切换：
- 默认（`!enterprise`）：内建认证（邮箱/手机/Worker Token），实现在 `backend/internal/adapter/account/oss/`。
- `enterprise`：认证委托 IAM，实现在 `backend/internal/adapter/account/enterprise/`（`config.yaml` 另有 IAM 配置）。
- 版本门面在 `backend/internal/adapter/edition.go`（`edition_oss.go` / `edition_enterprise.go`）。**禁止 `oss/` 与 `enterprise/` 互相 import。**
- `integration` 是独立标签，只用于依赖数据库/NATS 的测试（`backend/internal/testutil/`）。

控制面（`serve` / `agentd`）不使用这两套旧认证；改控制面时不要顺手“修复”它们。

## 分层与边界（违反后返工成本最高）

| 层 | 路径 | 可以做 | 不可以做 |
|---|---|---|---|
| 进程入口 | `backend/cmd/lework/` | cobra 命令、`lifecycle.Std().WaitExit()` / `.AddCloseFunc()` / `.Exit()`、信号处理、启动失败的 `log.Fatal` | 业务逻辑 |
| 控制面 | `backend/internal/controlplane/**` | 节点注册表、事件中心、浏览器 API、agent 网关、SQLite 索引、UI 偏好 | 业务执行、复制 OpenCode 会话状态、组织/RBAC |
| 节点 agent | `backend/internal/agentd/**` | 注册、心跳、OpenCode 监管、PTY、WSS 传输 | 业务状态、直接持有 UI 状态 |
| 执行内核（遗留） | `backend/agent/**` | 旧企业版 Runtime/Adapter/Tool 契约 | 被控制面依赖 |
| 库代码 | `backend/internal/**` | 业务逻辑；通过返回 `error` 暴露失败 | `os.Exit()`、`lifecycle.Std()`、`log.Fatal`、`panic`、信号、cobra 依赖 |
| 共享类型 | `backend/types/`、`backend/config/` | 领域类型、配置结构 | 业务逻辑、外部依赖 |

实操要点：
- 控制面是唯一主线：`controlplane` 只持有节点索引、事件路由与 UI 偏好；Session / 消息 / diff / 权限 / 文件以 OpenCode 为准，**不得复制成第二状态机**。
- 节点无入站端口：`agentd` 主动出站 WSS；控制面不得直接连 OpenCode，也不要求 SSH。
- 旧企业版模块（`backend/agent/**`、`internal/worker/**`、`tools/**`、`modelrouter`、`seed`）保留但不再是主线；与控制面冲突时直接删除。
- `internal/**` 不知道自己在 server、worker 还是 CLI 中运行；进程生命周期属于 `cmd/`。`lifecycle` 是第三方包 `github.com/ygpkg/yg-go/lifecycle`，不在 `backend/` 下。
- `agent/**` 与业务无关。Runtime 可以用 API key 执行请求，但**绝不能**把 key 写进 NodeEvent/RunEvent/Journal/NATS/日志/错误文本。
- 目录名会骗人：`backend/internal/cli` 是 CLI 相关**库代码**，不是进程入口，也不该拥有生命周期。
- 跨层共享常量/类型下沉到最低共享包（`backend/types/`、`backend/pkg/event/`），避免重复定义。`backend/events/` 不存在，事件类型与 topic 在 `backend/pkg/event/`。

## 硬性规则

- **库代码与业务代码禁止 `panic`。** 用 `error` 向上传递。只有 `cmd/lework` 可对不可恢复的启动失败使用 `log.Fatal`。（测试桩为实现接口而 `panic` 可以接受。）
- **业务数据禁止用 `map[string]interface{}`** 跨函数签名、接口或层边界传递——定义 struct 或具体类型 map（如 `map[string]string`）。例外：`tools.Tool` 的 Execute 契约（`backend/tools/`）确实用 `tools.JSONInput`（= `map[string]interface{}`）作为通用工具 I/O；`pkg/llmprotocol` 测试夹具也用它表示原始协议数据。不要盲目“重构”掉这两处。
- **禁止提交被 gitignore 的文件。** 提交前先 `git status`；`bundles/`、`.env`、`docs/superpowers`、`deployments/env/config.yaml`、`deployments/dev/` 下的 `server.config.yaml`/`worker.config.yaml`、`.opencode`、`.agents/`、`example/`、日志、前端 `out/`/`.next/`、`webui/dist/`（除 `.gitkeep`）等均被忽略。误暂存要撤回。
- **DB Schema 变更必须同步迁移逻辑。** 修改 `backend/types/` 中的 GORM 模型（增/删/改名/改约束）时：
  - 新增列 → GORM AutoMigrate 自动处理，无需额外操作。
  - 删除列 → 在 `backend/internal/infra/db/database.go` 的 `legacyColumns` 中注册 `{table, column}`，`dropLegacyColumns` 启动时清理。
  - 重命名列 → 在 `renamesToApply` 中注册 `{table, oldCol, newCol}`。
  - 删除表 → 在 `legacyTables` 中注册表名。
  - 数据回填 → 参考已有 `backfillXxx` 函数，按依赖顺序加入 `runMigrations`。
  - GORM AutoMigrate **不会删除或重命名列**，必须手动处理。

## Modelrouter — 遗留模块，控制面不依赖

旧文档（以及本文早期版本）声称 `backend/internal/modelrouter` 已弃用、应由 `modelrouter/v2` 取代。**`modelrouter/v2` 不存在。** 真实包是 `backend/internal/modelrouter/`，被遗留的 `cmd/lework/server.go`、`cmd/lework/worker.go`、`internal/worker/router`、`internal/worker/agentrun` 以及若干 `internal/service/*`（Invoker 接口、ModelStore、proxy）使用。处理这些遗留路径时以该包为准。`backend/internal/llm/` 是并存的较新统一 LLM 层（CallRequest/CallResult、recorder、usage）。

## 各目录实际职责（结构已演进，README/旧文档可能落后）

- `backend/cmd/lework/` — cobra 入口。控制面 `serve.go`（`lework serve`）；节点 `agent.go` / `agent_run.go`（`lework agent`）；旧命令 `server.go`、`worker.go`、`chat.go`、`session.go`、`task.go`、`project.go`、`skill.go`、`automation.go`、`login.go`/`logout.go`/`register.go`。
- `backend/internal/controlplane/` — `node`（注册表）、`event`（事件中心）、`api`（浏览器 API）、`agentgw`（agent 网关）、`store`（SQLite）、`webui`（内嵌界面）、`config`。
- `backend/internal/agentd/` — 节点守护：`protocol`、`opencode`（HTTP/SSE 客户端，**不是** ACP；ACP 只在遗留 `backend/agent/runtime/internal/acp`）、`supervisor`、`handler`、`runner`、`transport`（WSS）、`config`。
- `frontend/apps/control-plane/` — 当前浏览器界面（Next.js + pnpm workspace）。`frontend/apps/{web,desktop}/` 为旧产品，暂不维护。
- `backend/internal/` 其他主线/遗留子包：`adapter`（account、edition）、`api`（auth/connectors/handler/middleware/router）、`cli`、`consts`（仅 `paths.go`）、`infra`（db/git/gitea/mq/websocket 等）、`integration`（feishu）、`llm`、`modelrouter`、`seed`、`service`、`skill`、`testutil`、`worker`、`workspace`。
- `backend/agent/` — 遗留执行内核；`agent/runtime/` 下有 `native`、`claude`、`codex`、`opencode` 等具体 runtime。
- `backend/internal/api/connectors/` — GitHub/GitLab/企业微信 webhook 与渠道连接器；GitHub webhook 在这里，不在顶层 `interaction/connectors/`。
- `backend/tools/` — Tool 注册表与具体工具（artifact_declare、memory、node 的 file/shell/process、skill_manage、skill_use、todo），全部实现基于 map 的 Tool 契约。
- `backend/types/` — 领域类型（`DigitalAssistant`、`Event`、`Skill` 结构体等）与 `tables.go` 表名常量。
- `backend/config/`、`backend/pkg/`、`backend/prompts/`、`backend/skills/`（仅 `server/` + `worker/` 技能包）。
- 旧文档提到但**不存在**的目录：`backend/interaction/`、`backend/engines/`、`backend/gateway/`、`backend/events/`。

## 实现新功能（严格按顺序）

1. **先找参考实现。** 本项目大量复用既有模式，跳过这步是最主要的返工原因。
   - 新 cobra 命令 → 对照 `backend/cmd/lework/` 中同类文件（如 `task.go`）。
   - 新 HTTP API → `backend/internal/api/handler/`；路由接线在 `backend/internal/api/router.go`。
   - 新 HTTP 客户端 → grep `http.Client` / `http.NewRequest`。
   - 事件发布/订阅 → grep `eventbus.Publish` / `mq.Publisher` / NATS 用法。
   - 数据库操作 → `backend/internal/infra/db/`。
   - 跨包常量/类型 → 先查 `backend/types/` 与 `backend/internal/consts/`，避免重复定义。
2. **复制现有骨架**（import 分组、函数签名、错误处理），再替换业务逻辑。
3. **“简单改动”也不能跳过第 1 步**——文件名会误导（例如 `internal/cli` 不是入口）。

## Go 代码风格

- import 分三组、空行分隔：标准库 → 第三方 → `github.com/insmtx/lework/...`。只在避免冲突时用别名（如 `modelrouter "github.com/insmtx/lework/backend/internal/modelrouter"`）。
- 使用 Tab，不用空格；提交前跑 `gofmt -s`；单行 <120 字符。
- 导出符号写英文 GoDoc，解释**为什么**而不是**做什么**；注释与 commit message 用中文。
- 错误显式处理并用 `fmt.Errorf("...: %w", err)` 包装；依赖注入优先于全局变量；传递 `context.Context`。

## 提交与分支

- 约定式提交：`type(scope): subject`，type 用 `feat`、`fix`、`docs`、`style`、`refactor`、`test`、`chore`。commit message 用中文。
- 分支前缀：`feat/`、`fix/`、`docs/`、`refactor/`、`test/`、`enhancement/`。
- Fork-and-PR 流程（见 `CONTRIBUTING.md`）；动手前先与上游同步，不要直接推 main。

## 文档

- `docs/README.md` — 文档索引与规范，遇到不熟悉的任务先看这里。
- 当前主线：`docs/product/{overview,roadmap,current-limitations}.md`、`docs/architecture/{overview,as-built,agent-runtime}.md`、`docs/operations/control-plane.md`。
- `docs/operations/project-structure.md` 是目前最准确的结构索引；`CHANGELOG.md` 记录历史。
- `docs/archive/` — 旧企业版/项目制文档，仅历史参考，不再维护；不要从这里推断当前实现。
- `docs/design/2026-11-01-i0-findings.md` — I0 技术验证结论。
