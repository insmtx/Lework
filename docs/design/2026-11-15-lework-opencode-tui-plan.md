# `lework opencode` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `lework opencode`，让用户在 TUI 中连接 agent 管理的共享 OpenCode service，从而同时被 Lework 控制面看到。

**Architecture:** agent 启动时把 `URL + Basic Auth 密码` 原子写入用户状态目录（0600）；`lework opencode` 读取该文件、健康检查后以 `opencode --server <url>` 启动 TUI，并通过 `OPENCODE_PASSWORD` 传递凭据。控制面协议与状态模型不变。

**Tech Stack:** Go 1.25、cobra、`os/exec`、`net/http/httptest`、现有 `agentd/opencode.Client`。

**Spec:** `docs/design/2026-11-15-lework-opencode-tui-design.md`

## Global Constraints

- 库代码与业务代码禁止 `panic`；错误用 `fmt.Errorf("...: %w", err)` 包装。
- 密码不得出现在日志、错误文本、帮助输出、控制面/NATS。
- 新文件权限 0600，目录 0700，原子 rename。
- 不修改控制面 API、事件模型或数据库 schema。
- 提交前：`gofmt -s -w .`、`go vet ./...`、`go build ./...`、`go test -p 1 ./...`。
- 注释与 commit message 用中文；导出符号写英文 GoDoc。
- 同一 OS 用户使用；不支持跨用户/`--standalone`。

## Review Focus

- 状态文件缺失 vs 服务未就绪 vs 凭据错误的提示必须可区分，且都不泄露密码。
- 继承环境里的 `OPENCODE_PASSWORD` / `OPENCODE_SERVER_PASSWORD` 必须被移除，不能被旧值覆盖。
- TUI 的参数顺序必须是 `--server <url>` 在 `--session` / `--continue` / 目录之前。
- agent 的 `Close` 不能删除其他 agent 写入的端点文件（PID 校验）。
- 健康检查必须容忍 agent 启动初期的短暂 503/连接失败，但对 401 立即失败。

---

### Task 1: 本地服务端点文件

**Files:**
- Create: `backend/internal/agentd/servicefile/servicefile.go`
- Test: `backend/internal/agentd/servicefile/servicefile_test.go`

**Interfaces:**
- Consumes: 标准库 `encoding/json`、`os`、`path/filepath`、`time`。
- Produces:
  - `type Info struct { URL string; Password string; Port int; PID int; Updated time.Time }`
  - `func Path() (string, error)`
  - `func Write(info Info) error`
  - `func Read() (Info, error)`
  - `func Remove() error`

- [ ] **Step 1: Write the failing tests**

`servicefile_test.go` 覆盖：

```go
func TestWriteReadRoundTrip(t *testing.T)         // XDG_STATE_HOME=t.TempDir()；写入后 Read 相等，mode 0600，目录 0700
func TestWriteRejectsMissingURLOrPassword(t *testing.T)
func TestReadMissingFileIsNotExist(t *testing.T) // errors.Is(err, fs.ErrNotExist)
func TestReadRejectsInvalidContent(t *testing.T) // 手写 "{}" 后 Read 返回错误
func TestRemoveIsIdempotent(t *testing.T)
func TestPathUsesXDGStateHome(t *testing.T)
func TestPathFallsBackToHome(t *testing.T)       // 清空 XDG_STATE_HOME，t.Setenv("HOME", temp)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./backend/internal/agentd/servicefile -run TestWriteReadRoundTrip -v`
Expected: FAIL（包/符号不存在）。

- [ ] **Step 3: Implement `servicefile.go`**

`Path`：`XDG_STATE_HOME` 非空 → `filepath.Join(dir, "lework", "opencode-service.json")`；否则 `os.UserHomeDir()` → `filepath.Join(home, ".local", "state", "lework", "opencode-service.json")`。

`Write`：校验 `URL` 与 `Password` 非空；`Updated` 为零时填 `time.Now().UTC()`；`MkdirAll(dir, 0o700)`；`json.MarshalIndent`；`os.CreateTemp(dir, ".opencode-service-*.tmp")`（默认 0600）→ write → close → `os.Rename`；失败时删除临时文件。

`Read`：读取并 `json.Unmarshal`；URL 或 Password 为空时报 `invalid service file`；文件不存在时返回的错误必须可被 `errors.Is(err, fs.ErrNotExist)` 识别（直接返回 `os.ReadFile` 的错误或 `%w` 包装）。

`Remove`：`os.Remove`，`errors.Is(err, fs.ErrNotExist)` 时返回 nil。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./backend/internal/agentd/servicefile -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add backend/internal/agentd/servicefile
git commit -m "feat(agentd): 发布本地 OpenCode 服务端点文件"
```

---

### Task 2: agent 发布与清理端点

**Files:**
- Modify: `backend/cmd/lework/agent_run.go`
- Test: `backend/cmd/lework/agent_run_test.go`

**Interfaces:**
- Consumes: Task 1 的 `servicefile.Info` / `Write` / `Read` / `Remove`。
- Produces:
  - `agentRuntime` 新增字段 `password string`（`buildAgent` 赋值）。
  - `func (a *agentRuntime) publishService()`
  - `func (a *agentRuntime) unpublishService()`

- [ ] **Step 1: Write the failing tests**

在 `agent_run_test.go` 增加：

```go
func TestAgentRunPublishesServiceAndCloseRemovesIt(t *testing.T)
// XDG_STATE_HOME=t.TempDir()；构造 agentRuntime（cfg.OpenCode.Port=4097,
// password="pw"）；ctx 已取消；Run 返回 nil；servicefile.Read 得到
// URL=http://127.0.0.1:4097、Password=pw、Port=4097、PID=os.Getpid()；
// Close 后 Read 返回 fs.ErrNotExist。
func TestAgentCloseKeepsAnotherProcessServiceFile(t *testing.T)
// 先 Write(PID+1)；unpublishService 后文件仍存在；再 Write(PID)；unpublish 后消失。
```

构造 `agentRuntime` 时使用 `supervisor.New(nil, nil, supervisor.ServiceConfig{Port: 4097})` 与 `handler.New(client, service)`；`Run` 中 `Ensure` 会因 nil health checker 报错并只记日志，随后 `reconnect` 因 ctx 已取消立即返回 nil。

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./backend/cmd/lework -run 'TestAgentRunPublishes|TestAgentCloseKeeps' -v`
Expected: FAIL（`publishService` / `unpublishService` 不存在或文件未写入）。

- [ ] **Step 3: Implement**

- `buildAgent` 返回值加入 `password: password`。
- `Run` 在 nil 检查之后、`Ensure` 之前调用 `a.publishService()`。
- `Close` 在 `stopService` 之后调用 `a.unpublishService()`。
- `publishService` 构造 `servicefile.Info{URL: fmt.Sprintf("http://127.0.0.1:%d", cfg.OpenCode.Port), Password, Port, PID: os.Getpid()}`；写失败用 `logs.Warnf` 并 `redact`。
- `unpublishService` 先 `servicefile.Read()`；读取失败或 `PID != os.Getpid()` 时直接返回；否则 `Remove()`，失败只警告。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./backend/cmd/lework -run 'TestAgentRunPublishes|TestAgentCloseKeeps' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/lework/agent_run.go backend/cmd/lework/agent_run_test.go
git commit -m "feat(agent): 启动时发布 OpenCode 服务端点，退出时清理"
```

---

### Task 3: `lework opencode` 命令

**Files:**
- Create: `backend/cmd/lework/opencode.go`
- Test: `backend/cmd/lework/opencode_test.go`

**Interfaces:**
- Consumes: Task 1 的 `servicefile.Info` / `Read`；`agentd/opencode.Client`；标准库 `os/exec`。
- Produces:
  - `type openCodeTUIOptions struct { Binary, Directory, SessionID string; Continue bool }`
  - `type tuiDeps struct { readInfo func() (servicefile.Info, error); health func(context.Context, servicefile.Info) error; lookPath func(string) (string, error); run func(context.Context, string, []string, []string) error }`
  - `func runOpenCodeTUI(ctx context.Context, opts openCodeTUIOptions) error`
  - `func runOpenCodeTUIWith(ctx context.Context, opts openCodeTUIOptions, deps tuiDeps) error`
  - `func waitServiceReady(ctx context.Context, info servicefile.Info) error`
  - `func tuiEnv(base []string, password string) []string`
  - `func newOpenCodeCommand() *cobra.Command`

- [ ] **Step 1: Write the failing tests**

`opencode_test.go` 覆盖：

```go
func TestRunOpenCodeTUIMissingServiceFile(t *testing.T)
// readInfo 返回 fs.ErrNotExist；错误包含 "lework agent"；不含密码。
func TestRunOpenCodeTUIUnavailableService(t *testing.T)
// health 返回错误；错误包含 "not available"；run 未被调用。
func TestRunOpenCodeTUIPassesServerSessionAndDirectory(t *testing.T)
// readInfo 返回 URL/Password；lookPath 返回 "/fake/opencode"；run 捕获 args/env；
// 断言 args == ["--server", url, "--session", "ses_1", "/work/dir"]；
// env 含 "OPENCODE_PASSWORD=pw"，且不含继承的 OPENCODE_PASSWORD/OPENCODE_SERVER_PASSWORD。
func TestRunOpenCodeTUIContinueFlag(t *testing.T) // args 含 "--continue"
func TestRunOpenCodeTUIBinaryMissing(t *testing.T) // lookPath 错误被包装
func TestRunOpenCodeTUIWrapsRunError(t *testing.T) // run 返回 errors.New("boom")，错误包含 "run opencode"
func TestWaitServiceReadyRejectsUnauthorized(t *testing.T) // httptest 401 → 错误包含 "rejected"
func TestWaitServiceReadyRetriesUntilHealthy(t *testing.T) // 前两次 503，第三次 200 → nil
func TestNewOpenCodeCommandParsesArgs(t *testing.T) // cobra 命令接收 [dir]、--session、--continue
```

测试通过替换 `deps` 完成，不启动真实 `opencode`。

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./backend/cmd/lework -run 'TestRunOpenCode|TestWaitServiceReady|TestNewOpenCodeCommand' -v`
Expected: FAIL（符号不存在）。

- [ ] **Step 3: Implement**

- `runOpenCodeTUIWith`：按设计文档 4.2 的顺序读文件 → 健康检查 → `LookPath` → 组装参数/env → `deps.run`。
- `waitServiceReady`：`opencode.New(info.URL, &http.Client{Timeout: 2s}, opencode.WithPassword(info.Password))`；每 250ms 重试，总超时 5s；`opencode.ErrUnauthorized` 立即返回 `shared OpenCode service rejected credentials`；最终返回 `shared OpenCode service is not available: %w`。
- 超时/间隔用包级变量（`serviceHealthTimeout`、`serviceHealthPoll`、`serviceHealthRequest`）便于测试。
- `tuiEnv`：删除所有 `OPENCODE_PASSWORD=` 与 `OPENCODE_SERVER_PASSWORD=` 前缀项，追加 `OPENCODE_PASSWORD=<password>`。
- `runOpenCodeExec`：`exec.Command(path, args...)`，`Stdin/Stdout/Stderr = os.Stdin/...`，`Env = env`，`Run()`；注释说明不使用 `CommandContext`，让 TUI 自己处理终端信号。
- `newOpenCodeCommand`：`Use: "opencode [directory]"`，`Args: cobra.MaximumNArgs(1)`，flags `--opencode`（默认 `LEWORK_OPENCODE_BIN` 或 `opencode`）、`--session/-s`、`--continue/-c`；`RunE` 调用 `runOpenCodeTUI`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./backend/cmd/lework -run 'TestRunOpenCode|TestWaitServiceReady|TestNewOpenCodeCommand' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/lework/opencode.go backend/cmd/lework/opencode_test.go
git commit -m "feat(cli): 新增 lework opencode 连接 agent 管理的服务"
```

---

### Task 4: 根命令接线与帮助

**Files:**
- Modify: `backend/cmd/lework/register.go`
- Modify: `backend/cmd/lework/root.go`
- Test: `backend/cmd/lework/opencode_test.go` 或 `root_test.go`

**Interfaces:**
- Consumes: Task 3 的 `newOpenCodeCommand()`。
- Produces: 根命令 `opencode` 可见，且不加载旧 CLI 配置、不追加旧 `--config` flag。

- [ ] **Step 1: Write the failing tests**

```go
func TestRootRegistersOpenCodeCommand(t *testing.T)
// newRootCommand() 的子命令里存在 "opencode"；
// opencodeCmd.Flags().Lookup("config") == nil；
// isStandaloneCommand(opencodeCmd) == true。
func TestRootHelpMentionsOpenCode(t *testing.T)
// executeRootCommand("--help") 输出包含 "opencode"。
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./backend/cmd/lework -run 'TestRootRegistersOpenCode|TestRootHelpMentionsOpenCode' -v`
Expected: FAIL。

- [ ] **Step 3: Implement**

- `register.go`：`root.AddCommand(newOpenCodeCommand())`。
- `register.go` 的 `addCLIConfigFlag` 跳过列表加入 `"opencode"`。
- `root.go` 的 `isStandaloneCommand` case 加入 `"opencode"`。
- 根帮助的 “Core commands” 增加 `opencode  Start an OpenCode TUI on the agent-managed service`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./backend/cmd/lework -run 'TestRootRegistersOpenCode|TestRootHelpMentionsOpenCode' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/lework/register.go backend/cmd/lework/root.go backend/cmd/lework/opencode_test.go
git commit -m "feat(cli): 接线 lework opencode 子命令"
```

---

### Task 5: 文档与全量验证

**Files:**
- Modify: `AGENTS.md`
- Modify: `docs/product/overview.md`
- Modify: `docs/product/current-limitations.md`
- Modify: `docs/operations/control-plane.md`

**Interfaces:**
- Consumes: Task 1–4 的行为。
- Produces: 用户可按文档完成 `lework agent` + `lework opencode` 的验收。

- [ ] **Step 1: 更新文档**

- `AGENTS.md` 常用命令加入：
  `./bundles/lework opencode [directory]` — 连接 agent 管理的 OpenCode service 启动 TUI。
- `docs/product/overview.md` 5.1.1：说明 agent 发布端点与 `lework opencode` 的共享方式；
  明确直接运行 `opencode` 仍连默认后台 service。
- `docs/product/current-limitations.md`：把“TUI 与 Lework 共享同一 service”改为
  “`lework opencode` 已支持；默认 TUI 后台 service 与 agent 分离仍不支持”。
- `docs/operations/control-plane.md`：在节点运行段加入 `lework opencode` 示例与
  同用户要求。

- [ ] **Step 2: 全量检查**

```bash
gofmt -s -w .
gofmt -l .            # 必须为空
go vet ./...
go build ./...
go test -p 1 ./...
```

Expected: 全部通过；`gofmt -l` 无输出。

- [ ] **Step 3: 手工冒烟（本机）**

在一个隔离端口上（不要动用户已有服务）：

```bash
# 1) 用测试配置启动 agent（需要可用的控制面；或用现有 agent）
# 2) 另开终端：
lework opencode /path/to/project
# 3) 控制面应能看到同一 Session，并在 TUI 发 prompt 时收到事件
```

若本机已有用户服务，换端口/临时 XDG 验证，不要 `kill` 用户进程。

- [ ] **Step 4: Commit**

```bash
git add AGENTS.md docs/product/overview.md docs/product/current-limitations.md docs/operations/control-plane.md
git commit -m "docs: 说明 lework opencode 的共享服务用法"
```
