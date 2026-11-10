# Lework 架构概览

> 状态：目标架构 ｜ 更新：2026-11-10 ｜ 当前实现见 `as-built.md`
>
> 配套产品概览：`docs/product/overview.md` ｜ 路线图：`docs/product/roadmap.md`

## 1. 摘要

Lework 重构为**单用户、本地优先的 OpenCode 多节点控制面**：

- 控制面：Go + SQLite，提供浏览器 API、统一 SSE、Session 级 WebSocket；
- 前端：Next.js，浏览器只连控制面；
- 节点侧：每台机器部署 `lework agent`，由 systemd 常驻；
- 执行面：`lework agent` 管理本机 OpenCode 进程，并代理其 HTTP/SSE/WS；
- 连接：agent 主动出站连接控制面，节点不暴露任何入站端口；
- 上下文：Git branch/worktree/merge，由两端 agent 执行；
- 数据真相：Session、消息、diff、权限、文件均以 OpenCode 为准。

**核心约束：Lework 不实现 Agent Runtime，不复制 Session 状态，不做组织与权限；控制面不直接连 OpenCode，也不要求 SSH。**

---

## 2. 关键决策

| # | 决策 | 结论 | 理由 |
|---|---|---|---|
| D1 | 控制面技术栈 | Go + SQLite | 复用现有 AIO、单二进制、进程与网络能力 |
| D2 | 前端 | Next.js，本地 Web 优先 | 现代 GUI、开发效率；桌面壳后置 |
| D3 | 浏览器连接 | 只连 Lework 控制面 | 避免 CORS、token 暴露、多节点直连 |
| D4 | 节点连接 | `lework agent` 主动出站 WSS | 节点无需入站端口，穿越 NAT/防火墙 |
| D5 | OpenCode 生命周期 | agent 管理 | 系统服务只管 agent，进程职责集中 |
| D6 | OpenCode 暴露 | 只绑定 `127.0.0.1` | 控制面通过 agent 访问，不暴露端口 |
| D7 | 状态归属 | OpenCode 是唯一真相源 | 避免双状态机漂移 |
| D8 | 跨机上下文 | Git branch/worktree/merge | 可审查、可回滚、符合开发者习惯 |
| D9 | 多租户 | 不实现 | 目标用户是个人；单用户本地控制面 |
| D10 | Spark | agent 通过 USB-C 虚拟网卡出站 | 与普通节点同一套协议 |
| D11 | SSH | 不是产品路径 | 避免 SSH 配置、密钥、端口和权限问题 |
| D12 | OpenCode 服务形态 | I0：agent 自持普通 `serve` + 生成密码；I1 再设计 TUI 共享 | TUI 共享仍是目标，但 `--service` 忽略密码环境变量且会顶掉用户服务，需先做发现/XDG 隔离 |
| D13 | 目标节点仓库 | agent 维护 per-remote 裸仓库缓存 | 目标节点不需要预先 clone 项目 |
| D14 | 配置重启 | 有活跃 Session 时默认拒绝/排队 | 避免应用配置误杀正在执行的任务 |
| D15 | 合并策略 | 先生成结果 ref/临时分支，再显式合并 | 不默认修改 A 的当前工作区 |
| D16 | 旧产品关系 | 干净重开 | 不背旧发布兼容；旧代码只作参考，冲突模块直接删除 |
| D17 | 平台支持 | 控制面与 agent 均支持 Linux/macOS/Windows | 用户拍板；I1 Linux、I2 双平台、I5 三平台验收 |
| D18 | agent 安装 | 控制面生成手动安装命令 | 首版不做自动更新，避免签名/回滚复杂度 |
| D19 | Spark 实机 | I2 只留接口，I5 实机验收 | 避免早期被 USB-C 硬件阻塞 |

---

## 3. 总体架构

```text
┌──────────────────────────────────────────────────────────────┐
│ Browser                                                      │
│  Next.js UI: 节点 / 项目 / Session / Diff / 配置 / Action     │
└──────────────────────────────┬───────────────────────────────┘
                               │ HTTP + SSE + WebSocket
┌──────────────────────────────▼───────────────────────────────┐
│ Lework Control Plane (Go, 单进程)                            │
│                                                              │
│  Browser API / SSE Hub / PTY Hub                             │
│  Agent Gateway ── Node Registry ── Health / Version           │
│  Event Aggregator                                            │
│  Project / Session Index                                     │
│  Git Sync Orchestrator                                       │
│  Config / Action Manager                                     │
│  Local Store (SQLite)                                        │
└───────▲──────────────────▲──────────────────▲────────────────┘
        │ WSS outbound      │ WSS outbound      │ WSS outbound
┌───────┴──────┐   ┌───────┴──────┐   ┌───────┴───────────────┐
│ lework agent │   │ lework agent │   │ lework agent          │
│  └ OpenCode  │   │  └ OpenCode  │   │  └ OpenCode           │
│ Node A       │   │ Node B       │   │ Spark (USB-C vNIC)    │
└──────────────┘   └──────────────┘   └───────────────────────┘
```

### 3.1 职责边界

| 层 | 负责 | 不负责 |
|---|---|---|
| Browser | 展示、交互、局部 UI 状态 | 直接访问节点、保存节点 token |
| Control Plane | 节点注册、索引、事件聚合、Git 编排、配置编排 | Agent 执行、工具调用、Session 状态 |
| `lework agent` | 注册/心跳、OpenCode 进程、本机代理、Git、配置、PTY | 多节点管理、跨机编排 |
| OpenCode | Session、消息、工具、权限、diff、文件、PTY | 多节点管理、跨机 Git 同步 |
| Git | 分支、worktree、diff、merge | 实时事件、Session 状态 |

---

## 4. 组件设计

### 4.1 Agent Gateway / Node Registry

Agent Gateway 是控制面唯一面向节点的入口。

职责：

- 接受 agent 的出站 WSS 连接；
- 校验 enrollment token / node token；
- 维护节点在线状态、心跳、版本、能力；
- 将控制面的请求路由到对应 agent；
- 管理节点删除、token 轮换、强制断开。

监听建议：

- 本机 AIO：`127.0.0.1:7443`；
- 多机：绑定 LAN/Tailscale 地址，必须 TLS；
- 不把 agent gateway 和浏览器 API 混在同一个监听地址上；
- 默认拒绝非 TLS 的非回环连接。

节点模型：

```go
type Node struct {
    ID           string            // ULID
    Name         string
    AgentVersion string
    OpenCodeVer  string
    Status       NodeStatus        // online | offline | degraded | unknown
    Capabilities map[string]bool
    WorkspaceRoots []string
    LastSeenAt   time.Time
}
```

### 4.2 lework agent

`lework agent` 是节点侧常驻进程，由 systemd / launchd / Windows Service 管理。

#### 职责

| 职责 | 说明 |
|---|---|
| 注册与心跳 | 主动连接控制面，上报版本、能力、状态 |
| OpenCode 进程管理 | 启动、停止、重启、日志、崩溃拉起、版本检查 |
| 本机代理 | 调用 `127.0.0.1` 上的 OpenCode HTTP/SSE/WS |
| 事件归一化 | 将 OpenCode 事件转为 Lework 事件帧 |
| Git 执行 | status/diff/branch/worktree/snapshot/fetch/push/bundle/merge |
| 配置管理 | 读取/写入/校验/备份/回滚本机配置 |
| PTY | 打开/读写/调整大小/关闭终端 |
| 文件操作 | 在允许的工作区根内读写、上传、下载 |
| 诊断 | 收集日志、版本、进程状态 |

#### 进程模型

```text
OS service manager (systemd / launchd / Windows Service)
  └── lework agent
        ├── opencode serve --hostname 127.0.0.1 --port <port>
        └── child process logs / health / restart
```

- 系统服务管理器只负责 agent 常驻；
- I0 由 agent 自持一个普通 `opencode serve`，只绑定回环，并用每进程生成（或
  运维显式配置）的密码同时配置 client 与子进程环境；agent 不写用户级
  `--service` 注册，避免顶掉用户的 TUI 服务；
- agent 优先接管端口上已健康的实例（含认证通过），不存在时才启动；
- agent 崩溃由系统服务管理器拉起；agent 恢复后重新发现/接管服务；
- **TUI 共享是目标而非 I0 交付**：`opencode serve --service` 忽略
  `OPENCODE_SERVER_PASSWORD`、使用 `$XDG_CONFIG_HOME/opencode/service.json`
  的密码，并且同一 XDG 目录下换端口启动会替换并停掉已有实例。因此 TUI attach
  需要先设计服务发现、XDG 隔离与密码交换，推迟到 I1；
- 若 I0 证明单服务无法隔离并发项目，agent 的 supervisor 接口允许切换为 per-project 进程池；默认仍为共享服务。

#### TUI 与外部 Session

- **I0 现状**：agent 使用自持的普通 `serve` 和独立密码，用户的 TUI 默认连接的是
  它自己的服务，因此 TUI 暂时看不到 agent 的 Session；这是为绕开
  `--service` 的密码/注册冲突而做的阶段性取舍；
- **目标（I1）**：完成服务发现、XDG 隔离与密码交换后，让 TUI 与 Lework 连接
  同一个服务；届时 Lework 能看到 TUI 的 Session、事件和审批；
- 如果 TUI 使用 `--standalone` 或指向另一个 server，Lework 只能看到持久化历史，不能接管实时事件；
- 该场景在 UI 标记为「外部独立服务 / 只读」，恢复前需要用户让 TUI 连回共享服务；
- 不在同一 DB 上启动第二个 server，避免锁与跨项目污染。

#### agent 配置（Linux 示例）

```yaml
# /etc/lework/agent.yaml
control_plane_url: wss://control.example.com/api/agent/connect
enrollment_token: "<one-time>"
node_name: "workstation"
tls_fingerprint: "sha256:..."   # 自签证书时必填
workspace_roots:
  - /home/me/work
  - /data/projects
opencode:
  binary: /usr/local/bin/opencode
  port: 4096
  service_mode: shared          # shared | per-project (spike)
  env:
    OPENCODE_CONFIG_CONTENT: "{}"
worktree_root: /home/me/.lework/worktrees
repo_cache_root: /home/me/.lework/cache/repos
```

- `enrollment_token` 只用于首次注册；
- 注册成功后换取长期 node token，存 `0600` 文件；
- `control_plane_url` 由控制面显式配置的 `public_url` 生成，不使用请求 Host 推断；
- 自签证书用 fingerprint pinning，不做 TOFU；
- agent 不接受来自局域网的入站 API。

#### 安全约束

- 以普通用户运行，不以 root 运行；
- 只允许 `workspace_roots` 下的项目；
- Git 命令参数化，不拼接 shell；
- 不提供任意 shell 给控制面/浏览器；
- OpenCode 端口只绑定回环；
- 日志不记录 prompt、文件内容、token、密钥。

### 4.3 Agent 注册与发现

```text
控制面：
  POST /api/nodes/enroll
    → 生成一次性 enrollment token（TTL 10 分钟，单次使用）
    → 返回安装命令

节点：
  执行安装命令
    → 写入 /etc/lework/agent.yaml
    → systemd 启动 lework agent
    → agent 连接 wss://.../api/agent/connect
    → 发送 hello（agent 版本、能力、OpenCode 版本）
    → 控制面校验 token，分配 node_id
    → agent 保存 node token
    → 节点 online
```

- 节点发现 = agent 注册，不做 mDNS/端口扫描；
- 安装命令中的控制面地址来自显式 `public_url`，不根据浏览器 Host 推断；
- 公网/Tailscale 用公共 CA 证书；自签证书把 SHA-256 fingerprint 写进安装命令与 `agent.yaml`，agent 只信任该指纹；
- 重新安装 agent 生成新 node_id，或通过已有 token 恢复；
- 删除节点 = 控制面撤销 node token + 断开连接；
- token 轮换由控制面发起，agent 原子替换。

### 4.4 Agent Protocol

控制面与 agent 之间使用**一条 agent 主动建立的持久 WSS 连接**，多路复用请求、事件和二进制流。

选择 WSS 而不是 gRPC 的原因：

- 穿透常见反向代理/NAT 更稳定；
- 浏览器/控制面/agent 都容易调试；
- 二进制帧天然适合 PTY、日志和 Git bundle；
- v1 不需要引入 protobuf 代码生成。

#### 帧类型

| 类型 | 方向 | 用途 |
|---|---|---|
| `hello` | agent → control | 注册、版本、能力 |
| `welcome` | control → agent | node_id、配置、心跳间隔 |
| `heartbeat` | 双向 | 存活与负载 |
| `request` | control → agent | 方法调用 |
| `response` | agent → control | 结果或错误 |
| `event` | agent → control | Session/进程/配置事件 |
| `stream.open` | 双向 | PTY、日志、bundle 流 |
| `stream.data` | 双向 | 二进制数据 |
| `stream.close` | 双向 | 结束流 |
| `cancel` | control → agent | 取消请求 |

请求示例：

```json
{"type":"request","id":"req-1","method":"session.list","params":{"directory":"/work/foo"}}
```

响应示例：

```json
{"type":"response","id":"req-1","ok":true,"result":{"sessions":[]}}
```

#### 方法集（v1）

```text
node.info
node.health
opencode.start
opencode.stop
opencode.restart
opencode.status
opencode.logs

project.list
session.list
session.create
session.get
session.prompt
session.interrupt
session.messages
session.diff
session.export
session.import

permission.list
permission.reply

file.list
file.read
file.write
file.watch

git.status
git.diff
git.branch
git.snapshot
git.worktree.create
git.worktree.remove
git.fetch
git.push
git.bundle.create
git.bundle.apply
git.merge

config.read
config.write
config.validate
config.backup
config.rollback

plugin.list
plugin.install
plugin.update
mcp.list
mcp.add
mcp.remove
skill.list
command.list
action.preview
```

#### 重连与背压

- agent 断线后指数退避重连；
- 控制面标记节点 offline，保留索引；
- 重连后先 `hello` + 全量能力/状态，再恢复增量；
- 事件流有界缓冲；溢出时通知控制面 `resync_required`；
- 请求有超时与取消；
- 大文件/大 bundle 分块传输，限制单帧大小；
- `session.prompt` 必须带客户端幂等键，控制面/agent 去重，避免重连后重复提交；
- v1 不做 Session 单写租约：多客户端并发时以 OpenCode 的 inbox 语义为准，冲突显式返回错误，不静默覆盖。

### 4.5 Event Aggregator

```text
Agent A event ─┐
Agent B event ─┼──> normalize ──> ring buffer ──> Browser SSE
Agent Spark event ─┘
```

统一事件包：

```go
type LeworkEvent struct {
    NodeID    string
    ProjectID string
    SessionID string
    Seq       int64
    Type      string
    Payload   json.RawMessage
    Time      time.Time
}
```

关键规则：

- 当前按 OpenCode V2 client 文档的 **live-only，无历史重放** 设计；
- I0 需要实测 `/api/event` 是否支持 `seq`/重放，以及 `/api/experimental/session/{id}/log` 的可用性；若可重放，可缩小环形缓冲与 `resync_required` 逻辑；
- agent 负责把 OpenCode 事件归一化，控制面负责聚合；
- 浏览器重连时，控制面先返回 snapshot，再恢复增量；
- 每个节点事件有独立序号；控制面保留节点维度；
- 环形缓冲有上限，溢出时向浏览器发 `resync_required`；
- 不在控制面持久化完整消息流，只缓存最近事件用于重连。

重连流程：

```text
SSE 断开
  → 重新连接控制面
  → 控制面检查节点/agent 连接
  → 拉取 session list / messages / diff / permissions
  → 返回 snapshot
  → 重新订阅 agent 事件
```

### 4.5.1 PTY 与交互式终端

PTY 是双向流，不能塞进 SSE。控制面提供 **Session 级 WebSocket**：

```text
Browser <--WebSocket--> Lework <--agent stream--> lework agent <--> OpenCode PTY
```

规则：

- WebSocket 绑定到 `node_id + session_id + pty_id`，不能变成任意远程 shell；
- 只允许已注册节点、已打开 Session 的 PTY；
- 连接前校验浏览器访问 token、节点状态、Session 归属；
- 控制面与 agent 用 `stream.*` 帧转发字节；
- agent 只把 PTY 绑定到 OpenCode 的 PTY API，不提供通用 shell；
- 断开时按 OpenCode 的 PTY 生命周期处理。

### 4.6 Project / Session Index

控制面只存索引，不存消息正文：

```go
type Project struct {
    ID            string
    Name          string
    RemoteURL     string
    DefaultBranch string
}

type ProjectLocation struct {
    ID          string
    ProjectID   string
    NodeID      string
    Directory   string
    Role        string // primary | mirror | ephemeral | unmanaged
}

type SessionRef struct {
    ID                 string
    NodeID             string
    ProjectLocationID  string // 可空：外部创建的未纳管 Session
    OpenCodeSessionID  string
    Title              string
    Status             string // 缓存展示用，非真相源
    UpdatedAt          time.Time
}
```

原则：

- `Project` 是逻辑对象；`ProjectLocation` 才是目录。
- `SessionRef` 只是索引；打开 Session 时从 agent 拉最新状态。
- 不建 `Message`、`ToolCall`、`Artifact` 的本地副本。
- 节点上的 Session 列表以 OpenCode 为准；未登记目录的 Session 以 `unmanaged` 展示，允许用户一键纳管。
- 外部 TUI 创建的 Session 不隐藏，避免控制面与 OpenCode 漂移。

### 4.7 Git Sync Orchestrator

Git 由两端 agent 执行；控制面只编排。

#### 数据模型

`TaskRun` 只在跨节点 Git 执行时创建；同一节点上的普通 Session 没有 `TaskRun`。它不是新的任务状态机，只是 Git 执行上下文。

```go
type TaskRun struct {
    ID               string
    ProjectID        string
    SourceLocationID string
    TargetNodeID     string
    OpenCodeSessionID string
    Branch           string
    WorktreePath     string
    Status           TaskRunStatus
    Checkpoint       string
    Attempt          int
    Error            string
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

type TaskRunStatus string

const (
    TaskDraft      TaskRunStatus = "draft"
    TaskPreparing  TaskRunStatus = "preparing"
    TaskSyncing    TaskRunStatus = "syncing"
    TaskRunning    TaskRunStatus = "running"
    TaskCollecting TaskRunStatus = "collecting"
    TaskReview     TaskRunStatus = "review"
    TaskMerged     TaskRunStatus = "merged"
    TaskFailed     TaskRunStatus = "failed"
    TaskCancelled  TaskRunStatus = "cancelled"
)
```

#### 传输方式

1. **共享 Git remote（首选）**
   - A 的 agent push 任务分支；
   - B 的 agent fetch；
   - 结果反向 push/fetch。
2. **控制面中转 Git bundle（兜底）**
   - A 的 agent `git.bundle.create` 生成 bundle 流；
   - 控制面把流式数据转给 B 的 agent `git.bundle.apply`；
   - 节点之间不需要互相可达。

#### 目标节点仓库缓存

B 不需要预先 clone 项目：

- agent 在 `repo_cache_root` 下按 remote URL / bundle 来源维护裸仓库缓存；
- 首次任务 fetch 或 apply bundle 后创建缓存；
- 从缓存创建临时 worktree；
- 缓存按 LRU/磁盘配额清理，不删除仍被 TaskRun 引用的仓库；
- 共享 remote 可达时直接 fetch；否则用控制面中转的 bundle 初始化缓存。

#### 执行流程

```text
1. 用户在 Project A 创建 Task，选择目标节点 B
2. A 的 agent 创建任务分支 lework/task/<id>
   - 用临时 index 对 tracked + untracked 做快照
   - 不改 A 的当前工作区
3. 传输到 B
   - 共享 remote：A push，B fetch
   - 无共享 remote：A bundle → 控制面 → B apply
4. B 的 agent 确保仓库缓存存在
   - 无缓存时从 remote fetch 或从 bundle 初始化
5. B 的 agent 创建 worktree：
   git worktree add <worktree-root>/<task-id> lework/task/<id>
6. B 的 agent 启动/复用 OpenCode，在 worktree 上创建 Session
7. 控制面订阅 B 的 Session 事件
8. 完成后 B 的 agent 提交结果到任务分支
9. 结果回到 A：
   - 共享 remote：B push，A fetch
   - 无共享 remote：B bundle → 控制面 → A apply
   - A 侧生成结果 ref/临时分支并展示 diff
   - 人确认后显式 merge / cherry-pick
10. 清理 B worktree；保留分支直到用户删除
```

#### 状态推进与恢复

- `preparing` / `syncing` / `collecting` 只由编排步骤推进；
- `running` 在目标 Session 创建成功后进入；
- `review` 在结果分支可用且 Session 进入终态后进入；
- `merged` 只由用户显式合并触发；
- 控制面重启后，按 OpenCode Session 状态、分支是否存在、worktree 是否存在做对账；
- 未完成的步骤用 `task_runs` 的 checkpoint/attempt 字段幂等重试，无法判定时标记 `failed`；
- 不把 OpenCode 的每个消息事件映射成 TaskRun 状态。

#### 脏工作区快照

v1 规则：

- 包含 tracked 改动；
- 默认包含 untracked 文件；
- 默认不包含 ignored 文件；
- 大文件超过阈值时显式失败，提示用户先提交或使用 LFS；
- 快照通过临时 index / `commit-tree` 完成，**不修改 A 的当前工作区**。

#### 项目配置

worktree 在 OpenCode 看来是一个新 location。agent 必须保证项目级配置可用：

- 优先使用已提交的 `opencode.json` / `.opencode/`；
- 对未跟踪的项目配置，agent 复制到 worktree；
- 项目级配置不使用进程级 `OPENCODE_CONFIG_CONTENT`，避免污染同节点其他项目；
- 进程级 env 只放全局密钥/路径；
- 只复制配置，不复制整个工作区。

#### 冲突与失败

- 冲突不自动解决；
- 保留任务分支和 worktree 现场；
- UI 标记 `review` / `failed`，展示冲突文件；
- 用户可选择重试、丢弃、手工处理；
- 默认不直接修改 A 的当前工作区：先更新结果 ref/临时分支，用户确认后再 merge；
- A 工作区不干净时，要求先 stash/commit 或改用临时 worktree 合并；
- push `lework/task/*` 到用户 remote 需要项目级显式开关；remote 只读或无凭据时自动改用 bundle；
- 不自动 merge 到主分支。

### 4.8 Config / Action Manager

#### 配置读取

- agent 通过本机 OpenCode `GET /api/config` 读取有效配置；
- 读取项目级配置时，按节点/项目作用域请求；
- 控制面只做展示、差异、快照，不重新实现 OpenCode 的完整合并语义。

#### 配置写入

由 agent 执行：

1. 读取当前文件与有效配置；
2. 生成候选配置；
3. schema 校验；
4. 备份原文件；
5. 写入；
6. 重启/重载 OpenCode（按需）；
7. 健康检查；
8. 失败自动回滚。

重启策略：

- 优先使用 OpenCode 的配置重载能力；只有不支持热加载的字段才重启；
- 有活跃 Session 时默认拒绝应用并提示，或排队到 idle；
- 用户可显式选择「强制重启」，UI 必须提示会中断所有运行中的 Session/PTY；
- 回滚只恢复配置，不承诺恢复被重启杀掉的 Session；
- 可靠性表增加「配置重启中断运行中 Session」。

优先级：

- OpenCode API 能改的，走 API；
- 项目级配置、无法通过 API 修改的字段，由 agent 写文件；
- 不允许「只改内存、不落盘」的假应用。

#### Action

Action = OpenCode 快捷命令 / prompt 封装。

```go
type Action struct {
    ID             string
    Scope          string // global | project
    NodeID         string // 可空：表示所有节点
    ProjectID      string // scope=project 时必填
    Name           string
    Description    string
    PromptTemplate string
    Agent          string
    Model          string
    Args           []ActionArg
}
```

- 优先映射到 OpenCode 的 command/config；
- 执行时渲染 prompt，通过 Session prompt/command 发送；
- 不新增独立执行通道；
- 全局 Action 可被项目级覆盖；
- 修改前展示最终 prompt 预览。

### 4.9 Local Store

SQLite，单文件；只存控制面元数据：

```text
nodes
projects
project_locations
session_refs
task_runs
actions
config_snapshots
audit_events
enrollment_tokens
```

不存：

- 消息正文；
- 工具调用详情；
- 完整 diff；
- Session 状态机；
- 组织/用户/权限；
- 明文 node token（只存 hash）。

迁移：启动时 schema migrate；备份/恢复只针对该 SQLite 文件与密钥文件。

---

## 5. 控制面 API（草案）

### 5.1 节点与 agent

```text
GET    /api/nodes
POST   /api/nodes/enroll          # 生成一次性 enrollment token + 安装命令
GET    /api/nodes/{id}
PATCH  /api/nodes/{id}
DELETE /api/nodes/{id}
POST   /api/nodes/{id}/token/rotate
GET    /api/nodes/{id}/health
GET    /api/nodes/{id}/capabilities

GET    /api/agent/connect         # WSS，agent 主动连接（独立监听）
```

### 5.2 项目与 Session

```text
GET    /api/projects
POST   /api/projects
GET    /api/projects/{id}/locations
POST   /api/projects/{id}/locations

GET    /api/sessions?node=&project=&status=
POST   /api/sessions
GET    /api/sessions/{nodeID}/{sessionID}
POST   /api/sessions/{nodeID}/{sessionID}/prompt
POST   /api/sessions/{nodeID}/{sessionID}/interrupt
GET    /api/sessions/{nodeID}/{sessionID}/messages
GET    /api/sessions/{nodeID}/{sessionID}/diff
```

### 5.3 跨节点任务与 Git

`/api/tasks` 只表示跨节点 Git 执行；同一节点上的普通任务直接用 `/api/sessions`。

```text
POST   /api/tasks
GET    /api/tasks/{id}
POST   /api/tasks/{id}/start
POST   /api/tasks/{id}/cancel
POST   /api/tasks/{id}/merge
DELETE /api/tasks/{id}
GET    /api/tasks/{id}/diff
```

### 5.4 配置与 Action

```text
GET    /api/config?node=&project=
PATCH  /api/config
GET    /api/config/diff?nodes=
POST   /api/config/apply
POST   /api/config/rollback

GET    /api/actions
POST   /api/actions
PATCH  /api/actions/{id}
DELETE /api/actions/{id}
POST   /api/actions/{id}/preview
```

### 5.5 事件与 PTY

```text
GET    /api/events            # 统一 SSE
GET    /api/events/snapshot   # 重连对账
GET    /api/pty/{nodeID}/{sessionID}/{ptyID}/ws  # PTY 双向流
```

v1 不做通用 OpenCode 代理端点；浏览器只能访问上述显式 API。

---

## 6. 前端架构

### 6.1 路由

```text
/                    节点总览
/nodes/:nodeId       节点详情
/projects            项目列表
/projects/:id        项目详情
/sessions/:node/:id  Session 详情
/tasks/:id           跨节点任务详情
/actions             Action 管理
/settings            设置
```

### 6.2 状态管理

- 服务端状态：TanStack Query / SWR；
- 实时事件：一个全局 SSE 连接，按 node/session 分发；
- PTY：独立 WebSocket；
- 本地 UI 状态：轻量 store；
- 不把 Session 消息存进全局 store 的长期缓存，按需拉取。

### 6.3 关键界面

- 节点切换器与在线状态；
- agent 状态 / OpenCode 进程控制；
- 统一 Session 列表；
- 对话时间线 + 工具卡片；
- 权限审批条；
- 并排 diff；
- 文件树；
- PTY 终端；
- 配置 diff / 应用 / 回滚；
- Action 预览。

### 6.4 性能

- 长会话虚拟列表；
- 事件批量刷新；
- diff 懒加载；
- 大文件不进入首屏；
- 多节点事件限流与背压；
- PTY 只传字节，不做内容解析。

---

## 7. 安全设计

### 7.1 访问边界

- 浏览器 API 默认绑定 `127.0.0.1`；
- 需要局域网访问时，显式开启并设置单用户访问 token；
- agent gateway 使用独立监听，远程必须 TLS；
- 节点不开放入站端口；
- 不实现组织/RBAC，但保留单用户 token 和 agent token 两层。

### 7.2 密钥与 token

- enrollment token：一次性、短 TTL、只存 hash；
- node token：注册后签发，agent 存 `0600`，控制面只存 hash；
- node token 可轮换、可撤销；
- agent 连接必须 TLS（回环可降级为明文 WS）；
- 节点 token 不进入日志；
- 配置文件中的 secret 使用 env 引用或 OpenCode credential API；
- 配置快照必须脱敏；
- 日志统一 redaction：`authorization`、`api_key`、`token`、`password`。

### 7.3 命令执行

- Git、配置、插件安装都由 agent 执行；
- 命令参数化，不做字符串拼接；
- 只允许 `workspace_roots` 内的路径；
- 高风险操作（merge、删除 worktree、应用配置、重启 OpenCode）需要显式确认；
- 不提供任意远程 shell 给浏览器；
- agent 以普通用户运行，systemd 做最小权限加固。

### 7.4 版本

- agent 启动时校验 OpenCode 版本；
- 控制面维护支持范围；
- 对 experimental API 做能力探测；
- 版本不兼容时拒绝操作并给出升级提示。

---

## 8. 部署

### 8.1 控制面 / 本地 Web

- 单个 Go 进程：浏览器 API + SSE + PTY Hub + Agent Gateway；
- 静态前端可嵌入二进制；
- SQLite 单文件；
- 浏览器 API 默认 `127.0.0.1`；
- Agent Gateway 独立监听，默认回环，多机时绑定 LAN/Tailscale + TLS；
- 可选桌面壳（Electron/Tauri）后置。

### 8.2 节点 / agent

三平台都用系统服务托管 agent，agent 再托管 OpenCode：

| 平台 | agent 服务 | 配置文件 |
|---|---|---|
| Linux | systemd unit | `/etc/lework/agent.yaml` |
| macOS | launchd plist | `~/Library/Application Support/Lework/agent.yaml` |
| Windows | Windows Service | `%ProgramData%\\Lework\\agent.yaml` |

Linux 示例：

```ini
[Unit]
Description=Lework Agent

[Service]
ExecStart=/usr/local/bin/lework agent --config /etc/lework/agent.yaml
Restart=always
User=lework

[Install]
WantedBy=default.target
```

agent 管理 OpenCode：

```text
lework agent
  └── opencode serve --hostname 127.0.0.1 --port 4096
```

- OpenCode 不监听外网；
- 本机 client 与 OpenCode 共用每进程密码（`OPENCODE_PASSWORD` →
  `OPENCODE_SERVER_PASSWORD` → 随机生成），密码只经子进程环境传递，不写日志；
- agent 主动连接控制面；
- 节点不需要 SSH、端口映射、反向代理；
- 控制面不可达时，agent 重试并保持本机 OpenCode 可用；
- PTY：Unix 用 PTY，Windows 用 ConPTY；
- 路径、shell、进程信号统一走 `agentd` 的平台抽象，不在业务代码里散落 `runtime.GOOS` 判断；
- 手动安装命令根据平台生成，首版不做自动更新。

### 8.3 Spark

```text
Host (Control Plane) <-- USB-C vNIC --> Spark (lework agent)
```

- Spark 上安装 agent；
- agent 通过 USB-C 虚拟网卡主动连接控制面；
- agent 管理 Spark 上的 OpenCode；
- 不需要在 Spark 上开放入站端口；
- 地址发现方式（DHCP / link-local / 固定）在 I2 只留接口，I5 实机验证。

### 8.4 网络建议

- 本机：回环；
- 局域网：控制面绑定内网地址 + TLS；
- 跨公网：Tailscale / WireGuard / 受控反代；
- 不把 agent gateway 暴露到公网裸端口。

### 8.5 备份与恢复

- 备份：SQLite + 控制面密钥 + 节点配置快照；
- 不备份 OpenCode 的 session 数据（由节点自身负责）；
- 恢复后 agent 重连、重新同步节点状态。

---

## 9. 可靠性与错误处理

| 场景 | 处理 |
|---|---|
| agent 离线 | 标记 offline；保留索引；不自动删 session |
| agent 崩溃 | systemd 拉起；重连后全量对账 |
| OpenCode 崩溃 | agent 按策略重启；保留日志 |
| OpenCode 版本不兼容 | 拒绝连接；提示升级 |
| 事件断流 | 重连 + snapshot 对账 |
| Session 不存在 | 标记 stale；允许从节点重新同步 |
| Git push/fetch 失败 | 保留现场；任务进入 failed/review |
| 工作区脏 | 快照失败则阻止启动，不静默丢弃 |
| 配置应用失败 | 自动回滚到备份；展示错误 |
| 配置需重启且有活跃 Session | 默认拒绝/排队；强制重启需显式确认 |
| 事件洪水 | 限流/丢弃并标记 resync_required |
| 控制面不可达 | agent 保持本机 OpenCode；指数退避重连 |
| TUI 使用独立 server | 标记外部独立服务；只读历史，不接管实时事件 |
| 目标节点无仓库缓存 | 从 remote fetch 或 bundle 初始化；失败则任务 failed |

---

## 10. 可观测性

- 结构化日志：`node_id`、`agent_id`、`session_id`、`task_id`、`request_id`；
- 节点指标：agent 版本、OpenCode 版本、延迟、错误率、进程状态；
- Git 同步记录：分支、commit、耗时、结果；
- 配置变更审计：哪个节点、哪个文件、diff 摘要、应用/回滚结果；
- 不记录 prompt 正文、消息内容、密钥。

---

## 11. 测试策略

### 11.1 Agent 协议

- 注册、心跳、重连、token 轮换/撤销；
- 请求超时/取消；
- 二进制流分块；
- 背压与 `resync_required`。

### 11.2 OpenCode 契约

- 用 OpenAPI 生成 mock server；
- 覆盖 session、event、permission、diff、worktree、config；
- 固定 OpenCode 版本矩阵。

### 11.3 Git

- 干净工作区；
- tracked 改动；
- untracked；
- ignored；
- 冲突；
- 共享 remote；
- 控制面中转 bundle；
- 大文件；
- worktree 清理；
- 项目配置注入。

### 11.4 E2E

- 真实 OpenCode 单节点；
- 两节点 A/B；
- agent 断线/重连；
- 配置应用/回滚；
- 断网恢复；
- Spark 实机（手工验收）。

---

## 12. 迁移与清理

### 12.0 迁移策略

**已拍板：干净重开。**

- 旧产品不再保证可构建/可发布；
- 不保留旧 API、旧数据模型、旧任务状态机的兼容层；
- 新控制面与 agent 独立建模块，只复用通用工具和前端设计系统；
- 与新产品冲突的旧模块直接删除，不做「边删边迁」的双路径；
- 旧代码只作行为参考，尤其是 OpenCode 调用、配置和部署经验。

### 12.1 保留

- 现有前端设计系统与组件；
- AIO/部署脚本中可复用的部分；
- 日志、配置加载、基础 HTTP/进程工具；
- 单用户本地运行形态。

### 12.2 删除或移出主干

- 自建 Agent Runtime 与 worker 执行链；
- 任务状态机与验收流程；
- 组织、成员、部门、权限、邀请；
- 模型代理、插件市场、反馈/飞书、遥测、计费；
- 多 Runtime 适配器；
- 垂直行业技能包。

### 12.3 新增模块

```text
backend/cmd/lework              # 控制面与节点 agent 入口
backend/internal/controlplane/
  api/          # 浏览器 API/SSE/PTY
  agentgw/      # WSS Agent Gateway
  node/         # 节点注册与健康
  event/        # normalize, buffer, resync
  project/      # project/location/session index
  git/          # task snapshot, sync, merge
  config/       # read/write/diff/rollback
  action/       # prompt/command wrappers
  store/        # SQLite
backend/internal/agentd/
  protocol/     # hello/request/response/stream
  opencode/     # local process + HTTP/SSE/WS
  supervisor/   # process lifecycle
  git/          # local git operations
  config/       # local config files
  pty/          # local PTY
```

---

## 13. 实施顺序

| 阶段 | 内容 | 技术退出标准 |
|---|---|---|
| I0 | agent + OpenCode spike | 单节点注册、进程管理、Session 全链路；三平台接口定型 |
| I1 | 控制面骨架 + 单节点 UI（Linux） | 浏览器完成 Session 操作，无 TUI |
| I2 | 多节点 + macOS/Windows agent 基线 | 两平台注册、切换、断线重连、PTY |
| I3 | Git 同步 | A→B 任务 + review/merge |
| I4 | 配置/Action | 配置读、diff、应用、回滚 |
| I5 | AIO/三平台安装包/Spark | 可交付安装包与三平台 + Spark 实机验收 |

---

## 14. 待验证问题

1. agent 与控制面的 WSS 在反向代理/Tailscale 下的稳定性与帧大小限制。
2. ~~OpenCode V2 的认证/pairing 是否需要，以及 agent 本机访问的最小权限。~~
   I0 已确认：`serve` 强制 HTTP Basic Auth，用户名固定 `opencode`，密码来自
   `OPENCODE_PASSWORD`/`OPENCODE_SERVER_PASSWORD` 或随机；agent 已用生成密码
   打通本机访问。I1 仍需设计 `--service`/TUI 场景下的密码交换与最小权限。
3. 项目级配置是否有稳定写 API；没有则 agent 文件写入的边界。
4. Spark USB-C 虚拟网卡的地址分配方式（DHCP / link-local / 固定）与接口发现。
5. 控制面中转 Git bundle 的大仓库性能与断点续传。
6. OpenCode `session export/import` 是否可用于跨节点会话迁移，还是只做 Git 上下文迁移。
7. Action 与 OpenCode command/config 的最终映射。
8. 控制面是否需要持久化事件用于断线重放，还是只做 snapshot 对账。
9. OpenCode shared service 在单节点多项目/多 worktree 并发下的隔离与锁表现；不达标时切换 per-project 进程池。I0 实测一个普通 `serve` 可并发服务两个项目；TUI 共享时 `--service` 的隔离仍需 I1 复核。

### 14.1 已拍板

1. **旧产品关系**：干净重开；旧版不再保证可构建/可发布，不保留兼容层。
2. **控制面 OS**：Linux + macOS + Windows。
3. **agent OS**：Linux + macOS + Windows；I1 Linux、I2 双平台、I5 三平台验收。
4. **Spark 实机时点**：I2 只留接口，I5 实机验收。
5. **agent 安装/升级**：首版只做控制面生成的手动安装命令，不做自动更新。

### 14.2 拍板带来的约束

- agent 必须抽象 systemd / launchd / Windows Service；
- PTY 必须同时支持 Unix PTY 与 Windows ConPTY；
- 路径、shell、信号、文件权限需要平台适配；
- I5 需要三平台安装包、签名与实机验收；
- 干净重开意味着 I0/I1 不写兼容层，旧模块按依赖顺序删除。

---

## 15. 一句话验收

> 一个 Go 控制面，一个本地 Web GUI，每台机器一个 `lework agent`；agent 主动出站、管理 OpenCode、执行 Git 与配置，浏览器不直连节点，Session 以 OpenCode 为准。
