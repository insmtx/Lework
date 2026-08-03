# AGENTS.md

Compact guidance for AI agents working in the Lework/Leros repo. Verified against the codebase on the date below; trust executable sources over any prose here if they conflict.

> Module path: `github.com/insmtx/Leros` (note: repo dir is `Lework`, Go module is `Leros`). Go 1.25.0.

## Commands

Build / check / test (run from repo root):
- `go build -o ./bundles/leros ./backend/cmd/leros/` — build server (builtin/OSS auth). Binary → `./bundles/` (gitignored).
- `go build -tags enterprise -o ./bundles/leros ./backend/cmd/leros/` — enterprise build (auth delegated to IAM). See "Editions" below.
- `make build` — same as builtin build with `-ldflags="-s -w"`.
- `make run` / `make run-detached` / `make stop` / `make logs` — docker-compose stack at `deployments/env/docker-compose.yml`.
- `make dev-setup` — one-time dev bootstrap (see `deployments/dev/dev-setup.sh`); `make dev-server` / `make dev-worker` / `dev-frontend` run each process locally.
- `make swagger` — generate swagger into `docs/swagger/` from `backend/cmd/leros/server.go` (+ handler/api/types dirs). Only output location is `docs/swagger/`.

Test (no `make test` target exists — CONTRIBUTING.md is wrong on this):
- `go test ./...` — all (excludes `//go:build integration` and `enterprise` files by default).
- `go test -tags integration ./...` — run integration tests (need NATS + DB; helpers in `backend/internal/testutil/`).
- `go test -tags enterprise ./...` — enterprise-only tests (most also need `-tags integration`).
- `go test ./backend/internal/<pkg>` — single package. `go test -run ^TestName$ ./...` — single test.

Check before committing:
- `go fmt ./...` and `gofmt -s -w .` (format; Makefile uses `-s`).
- `go vet ./...`.
- `golint ./...` / `staticcheck ./...` if installed.

Order when finishing a change: `gofmt -s` → `go vet` → `go build ./...` → `go test ./...`.

## Editions (build tags, easy to miss)

Two builds share one codebase, selected via `//go:build` tags:
- Default (`!enterprise`): builtin auth (email/phone/Worker Token) from `backend/internal/adapter/account/oss/`.
- `enterprise`: auth delegated to IAM service from `backend/internal/adapter/account/enterprise/` (+ IAM config in `config.yaml`).
Gate at `backend/internal/adapter/edition.go`. Never import `oss/` from `enterprise/` files or vice versa. README "认证体系" documents the user-facing side.

`integration` is a separate tag for DB/NATS-dependent tests (`backend/internal/testutil/`).

## Layering (strict; the most common rework cause)

| Layer | Path | May | May NOT |
|---|---|---|---|
| Process entry | `backend/cmd/leros/` | cobra commands, `lifecycle.Std().WaitExit()` / `.AddCloseFunc()` / `.Exit()`, signal handling, `log.Fatal` | business logic |
| Execution core | `backend/agent/**` | generic exec contracts, Runtime Adapter, Tool/Interaction/NodeEvent, internal process wiring | SingerOS business, NATS, business DB, Server API, Worker identity, process lifecycle |
| Library code | `backend/internal/**` | business logic; surface failures via returned `error` | `os.Exit()`, `lifecycle.Std()`, `log.Fatal`, `panic`, signals, cobra deps |
| Shared types | `backend/types/`, `backend/config/` | domain types, config structs | any business logic or external deps |

Practical points:
- `internal/**` packages must not know whether they run in server, worker, or CLI — process lifecycle belongs to `cmd/`. The `lifecycle` package is third-party (`github.com/ygpkg/yg-go/lifecycle`), not under `backend/`.
- `agent/**` is business-agnostic. A Runtime may use an API key to execute a request but must never write API keys into NodeEvent/RunEvent/Journal/NATS/logs/error text.
- Directory names mislead: `backend/internal/cli` is *library code* for CLI-related logic — it is NOT a process entry and must not own lifecycle.
- Shared constants/types across layers sink to the lowest shared package (`backend/types/`, `backend/pkg/event/`); avoid duplicate definitions. (`backend/events/` does not exist — event types/topics live in `backend/pkg/event/`.)

## Hard rules

- **No `panic` in library or business code.** Propagate via `error`. Only `cmd/leros` may use `log.Fatal` for unrecoverable startup failures. (`panic` inside test stubs to satisfy unimplemented interface methods is acceptable.)
- **No `map[string]interface{}` for business data** across function signatures, interfaces, or layer boundaries — define a struct or typed map (e.g. `map[string]string`). EXCEPTION: the `tools.Tool` Execute contract (`backend/tools/`) legitimately uses `tools.JSONInput` (= `map[string]interface{}`) as the universal tool I/O shape, and `pkg/llmprotocol` test fixtures use it for raw protocol blobs. Do not "refactor" those away blindly.
- **Never commit gitignored files.** Before committing run `git status`; `bundles/`, `.env`, `docs/superpowers/`, `deployments/env/config.yaml`, `server.config.yaml`/`worker.config.yaml` under `deployments/dev/`, `.opencode`, logs, etc. are ignored. If accidentally staged, unstage before committing.
- **DB Schema 变更必须同步迁移逻辑。** 修改 `backend/types/` 中的 GORM 模型（增/删/改名/改约束）时：
  - 新增列 → GORM AutoMigrate 自动处理，无需额外操作。
  - 删除列 → 在 `database.go` 的 `legacyColumns` 数组中注册 `{table, column}`，`dropLegacyColumns` 会在启动时自动清理。
  - 重命名列 → 在 `database.go` 的 `renamesToApply` 数组中注册 `{table, oldCol, newCol}`。
  - 删除表 → 在 `database.go` 的 `legacyTables` 数组中注册表名。
  - 数据回填 → 参考已有的 `backfillXxx` 函数模式新增迁移函数，在 `runMigrations` 中按依赖顺序调用。
  - GORM AutoMigrate **不会删除列、不会重命名列**，这些必须手动处理。

## Modelrouter — ignore stale v1/v2 claims

Older docs (and prior AGENTS revisions) claim `backend/internal/modelrouter` (v1) is deprecated in favor of `modelrouter/v2`. **`modelrouter/v2` does not exist.** The real package is `backend/internal/modelrouter/` and is actively used by `cmd/leros/server.go`, `cmd/leros/worker.go`, `internal/worker/router`, `internal/worker/agentrun`, and several `internal/service/*` files (Invoker interface, ModelStore, proxy). A future refactor may move it to `backend/internal/llm/proxy/` (see `docs/design/unified-llm-package.md`), but until then treat `modelrouter` as the source of truth. `backend/internal/llm/` is a newer unified LLM layer (CallRequest/CallResult, recorder, usage) coexisting with it.

## Where things actually live (structure evolved; README/old docs lag)

- `backend/cmd/leros/` — cobra entrypoint. Files: `server.go`, `worker.go`, `chat.go`, `session.go`, `task.go`, `project.go`, `skill.go`, `login.go`/`logout.go`/`register.go`.
- `backend/internal/` — adapter (account/oss|enterprise, edition), api (auth, connectors, contract, dto, handler, middleware, router), cli, consts (only `paths.go`), infra (db, filestore, git, gitea, mq, providers, sms, websocket), integration (feishu), llm, memory, modelrouter, projectfile, runnable, service, skill (catalog, store, links, builtin, cache, fetch, token.go), testutil, worker (agentrun, app, command, eventpub, identity, mcp, router, run, runtimehost, scheduler, server, wsproto), workspace.
- `backend/agent/` — execution core (runtime, adapter, executor, registry, interaction, node_event, observe, tool). `agent/runtime/` holds concrete runtimes: `native`, `claude`, `codex`, `opencode`.
- `backend/internal/api/connectors/` — webhook/channel connectors (`github`, `gitlab`, `wework`). GitHub webhook handling lives here, not in a top-level `interaction/connectors/` tree as old docs claim.
- `backend/tools/` — Tool registry + concrete tools (artifact_declare, memory, node (file/shell/process, OS-split), skill_manage, skill_use, todo). All implement the map-based Tool contract.
- `backend/types/` — domain types: `DigitalAssistant`, `Event`, `Skill` (struct, not interface), `tables.go` (DB table name constants).
- `backend/config/`, `backend/pkg/`, `backend/prompts/`, `backend/skills/` (only `server/` + `worker/` subdirs with worker skill bundles like docx/pdf/pptx/xlsx).
- NOT present despite old docs: `backend/interaction/`, `backend/engines/`, `backend/gateway/`, `backend/events/`. GitHub webhook handling now lives under `backend/internal/api/connectors/`, not a top-level `interaction/connectors/` tree.

## Implementing new functionality (follow strictly)

1. **Search for a reference first.** This project reuses patterns; skipping this is the top rework cause.
   - New cobra command → mirror a file in `backend/cmd/leros/` (e.g. `task.go`).
   - New HTTP API → `backend/internal/api/handler/`; router wiring in `backend/internal/api/router.go`.
   - New HTTP client call → grep `http.Client` / `http.NewRequest`.
   - Event publish/subscribe → grep `eventbus.Publish` / `mq.Publisher` / NATS usage.
   - DB ops → `backend/internal/infra/db/`.
   - Cross-package constants/types → check `backend/types/` and `backend/internal/consts/` before defining new ones.
2. **Copy an existing skeleton** (imports grouping, function signatures, error handling), then swap in business logic.
3. **Do not skip step 1 on "simple" changes** — filenames mislead (e.g. `internal/cli` is not an entrypoint).

## Code style (Go)

- Imports in three groups separated by blank lines: stdlib → third-party → `github.com/insmtx/Leros/...`. Aliases only to avoid collisions (e.g. `modelrouter "github.com/insmtx/Leros/backend/internal/modelrouter"`).
- Tabs, not spaces. Run `gofmt -s` before commit. Lines <120 chars.
- Exported symbols get English GoDoc explaining *why*, not *what*. Comments and commit messages use Chinese.
- Errors handled explicitly, wrapped with `fmt.Errorf("...: %w", err)`. DI over globals. Pass `context.Context`.

## Commit / branch conventions

- Conventional commits: `type(scope): subject`. Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`. Commit messages in Chinese.
- Branch prefixes: `feat/`, `fix/`, `docs/`, `refactor/`, `test/`, `enhancement/`.
- Fork-and-PR workflow (see CONTRIBUTING.md). Sync with upstream before starting; do not push directly to main.

## Docs

- `docs/README.md` — doc index/spec. Consult first for unfamiliar tasks.
- Architecture: `docs/architecture/{overview,backend,agent-runtime,workspace-artifact,mq-subject,system-design,design-philosophy}.md` (plus `Lework 权限与资源占用设计文档.md` and `leros-architecture.html`).
- Design notes under `docs/design/` include multi-phase refactor plans (e.g. `unified-llm-package*.md`) — treat as proposals, not implemented state.
- `docs/operations/project-structure.md` is the most accurate structural reference; `CHANGELOG.md` records history.
- `docs/product/` holds `prd.md`/`planning.md`/`todo.md`; `docs/design/tech-design.md` (not `docs/TECH_DESIGN.md`). README links `docs/PRD.md`, `docs/ARCHITECTURE.md` etc. by basename, but those root-level files do not exist — the real files are under `docs/<category>/`. Prefer the subpathed versions.
