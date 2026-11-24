# `lework opencode`：让 TUI 与 Lework 共享 agent 管理的 OpenCode service

> 状态：已实现（本分支）。控制面协议不变；`lework agent` 仍是常驻主程序。

## 1. 背景与目标

日常入口是 OpenCode TUI，控制面是第二个客户端。现状是两套服务：

- `lework agent` 自持一个普通 `opencode serve`（默认 `127.0.0.1:4096`，密码由
  agent 生成或从环境读取）；
- 用户直接运行的 `opencode` TUI 默认连接自己的后台 service（端口动态，
  地址与密码在 `$XDG_STATE_HOME/opencode/service.json`）。

两者不是同一个 service，所以控制面看不到 TUI 里的 Session，TUI 也看不到
agent 创建的 Session。

目标：新增 `lework opencode` 子命令，让 agent 发布自己管理的服务端点，TUI 以
纯客户端身份连接同一个 service。这样：

- 用户仍然在 TUI 里直接操作；
- 控制面能列出/创建同一个 service 上的 Session，并实时接收事件；
- 不复制 OpenCode 的 Session/消息/审批状态，不新增节点入站端口。

## 2. 非目标

- 不接管用户默认的后台 service（即“TUI 起服务、agent attach”的方向）。
- 不修改控制面 API、事件模型或 PostgreSQL schema。
- 不支持跨 OS 用户共享；不做多 agent 同机多实例的端点仲裁。
- 不兼容 `opencode --standalone`：它连接的是私有服务，与本方案无关。

## 3. 已验证事实

来自本机 `opencode v2.2.7` 的真实探针（未触碰用户已有服务）：

- `opencode serve` 强制 HTTP Basic Auth；错误提示为
  `Server at <url> requires a password; set OPENCODE_PASSWORD`。
- 客户端密码解析顺序是 `OPENCODE_PASSWORD` → `OPENCODE_SERVER_PASSWORD`
  （CLI bundle 中的 `xue` 定义）。
- `opencode api --server http://127.0.0.1:<port> GET /api/info` 带
  `OPENCODE_PASSWORD` 对真实 plain `serve` 返回 200；不带密码返回 401。
- `opencode --server <url>` 是 TUI 连接指定服务的入口；`opencode <directory>`
  仍是位置参数。
- `supervisor` 已有 attach 语义，但按固定端口 + 同一密码探测；本方案不依赖它，
  agent 继续是 service 的所有者。

## 4. 设计

### 4.1 服务端点发布

新增 `backend/internal/agentd/servicefile`：

```go
type Info struct {
    URL      string    `json:"url"`
    Password string    `json:"password"`
    Port     int       `json:"port"`
    PID      int       `json:"pid"`
    Updated  time.Time `json:"updated"`
}
```

- 路径：`$XDG_STATE_HOME/lework/opencode-service.json`；未设置 XDG 时回落到
  `~/.local/state/lework/opencode-service.json`。
- 文件权限 0600，目录 0700；临时文件 + rename 原子写。
- `lework agent` 在 `Run` 启动时发布（在 `Ensure` 之前，便于并发的 TUI 轮询）；
  `Close` 时只在文件 PID 等于当前进程时删除，避免误删其他 agent 的记录。
- 发布失败只记警告，不阻断 agent 启动。

密码只用于本机同用户 TUI 连接；不写日志、不写错误文本、不进控制面/NATS。

### 4.2 `lework opencode`

```
lework opencode [directory] [--session <id>] [--continue] [--opencode <path>]
```

流程：

1. 读取 service 文件；缺失 → `no running lework agent found for this user;
   start `lework agent` first`。
2. 用 `opencode.Client` 对 `URL` 做健康检查（`GET /api/info`，Basic Auth），
   最多等待 5 秒；401 明确报告“凭据被拒”。
3. `exec.LookPath` 解析 opencode 可执行文件（`--opencode` 或
   `LEWORK_OPENCODE_BIN`，默认 `opencode`）。
4. 构造参数：`--server <url>`、可选 `--session <id>` / `--continue`、可选目录。
5. 构造环境：复制当前环境，删除继承的 `OPENCODE_PASSWORD` 与
   `OPENCODE_SERVER_PASSWORD`，注入发布文件中的 `OPENCODE_PASSWORD`。
6. 启动子进程并继承 stdin/stdout/stderr，让 TUI 直接占有当前终端。

子进程的退出错误原样返回；任何错误文本都不包含密码。

### 4.3 生命周期与并发

- agent 停止 → TUI 失去服务；agent 重启时沿用 `$XDG_STATE_HOME/lework/opencode-password`
  中持久化的生成密码（0600），TUI 自带的事件流重试会自动重连。
  手动配置的 `OPENCODE_PASSWORD` 优先于该文件。
- 同一 service 可以同时有 TUI 和控制面两个客户端：
  - `permission.asked` 广播给所有客户端，先回复者生效，另一个回复会 404；
  - PTY 是单控制器，TUI 与网页终端不应同时写同一个 PTY；
  - Session/消息仍以 OpenCode 为准，Lework 不复制状态。
- agent 作为 systemd 系统服务（root）发布 0600 文件时，普通用户读不到；
  当前要求 agent 与 `lework opencode` 使用同一 OS 用户（或都是 root）。

## 5. 失败模式与提示

| 场景 | 提示 |
| --- | --- |
| service 文件缺失 | `no running lework agent found for this user; start \`lework agent\` first` |
| 服务未就绪 | `shared OpenCode service is not available at <url>: ...` |
| 401 | `shared OpenCode service rejected credentials at <url>: ...` |
| 找不到 opencode | `find opencode executable "opencode": ...` |
| TUI 退出非 0 | `run opencode: exit status N` |

## 6. 验收

- `servicefile`：往返、0600/0700、缺文件、非法内容、幂等删除、XDG/HOME 路径。
- agent：`Run` 发布端点；`Close` 删除；PID 不匹配时不删。
- 命令：缺文件、服务未就绪、参数与环境、二进制缺失、运行错误包装、根命令接线。
- 手工：`lework agent` + `lework opencode` 打开 TUI；控制面列出同一 Session，
  并在 TUI 发 prompt 时收到实时事件。

## 7. 决策记录

- 服务所有者保持为 agent，TUI 只做客户端；不改 supervisor 的 spawn 逻辑。
- 端点通过本地状态文件发布，而不是新增入站端口或 Unix socket；与 OpenCode
  自身 `service.json` 的模式一致，改动最小。
- 不做任意 opencode flag 透传；只提供目录、session、continue 三个最常用入口。
