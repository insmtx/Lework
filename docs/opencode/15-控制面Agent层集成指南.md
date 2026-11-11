# 15 控制面 Agent 层集成指南

> 来源：官方 `openapi.json`、`api`、`service`、`permissions`、`troubleshooting`，以及 lework 现有实现
> `backend/internal/agentd/`、`backend/internal/controlplane/`、`docs/architecture/as-built.md`、`docs/operations/control-plane.md`。
> 本文把前 14 篇的 OpenCode 契约落到 lework：哪些能力已经接通、怎么映射、下一步扩展什么、哪些边界不能越过。

## 1. 结论先行

lework 控制面的定位是 **单用户、单节点（可扩展多节点）的 OpenCode 控制面**。它只持有：

- 节点注册表与连接状态
- 浏览器侧的事件路由
- UI 偏好与索引

**Session、Message、Inbox、Permission、Diff、File、PTY、MCP 连接都以 OpenCode server 为准**。控制面不做第二套会话状态机，也不解析 OpenCode 的业务语义去“重建”状态。

这条边界决定了集成方式：

```text
浏览器
  │  HTTP/JSON + SSE + 终端 WebSocket（同源）
  ▼
lework serve（控制面）
  │  节点主动建立的 WSS；帧协议 request/response/event
  ▼
lework agent（节点守护，无入站端口）
  │  HTTP/JSON + SSE + PTY WebSocket（仅回环）
  ▼
opencode serve（每节点一个共享 server，按 Location 装载项目）
```

任何新能力都应优先沿这条链“透传 + 轻量投影”，而不是在控制面重新实现。

## 2. 当前实现基线（I1-B）

### 2.1 进程与连接

| 组件 | 入口 | 关键行为 |
|---|---|---|
| 控制面 | `lework serve --listen 127.0.0.1:8787` | SQLite 节点库、agent 网关、浏览器 API、SSE、内嵌 UI；默认只绑定回环 |
| 节点守护 | `lework agent --config ...` | 主动出站到 `/api/agent/connect`；管理 OpenCode 子进程；转发事件 |
| OpenCode | `opencode serve --hostname 127.0.0.1 --port <port>` | 由 supervisor 启动或附着（attach）；HTTP Basic Auth |
| 浏览器 | `http://127.0.0.1:8787/` | 只访问同源相对路径，不接触节点令牌 |

节点**没有入站端口**，控制面也**不直接连 OpenCode**。这是安全模型的基础：节点只需能出站访问控制面。

### 2.2 WSS 帧协议

`backend/internal/agentd/protocol/frame.go` 定义了唯一的帧信封：

```jsonc
{
  "type": "hello|welcome|heartbeat|request|response|event|cancel",
  "id": "请求/响应关联 ID",
  "method": "hello/request 使用",
  "params": { },
  "result": { },
  "error": { "code": "method_not_found|internal_error", "message": "..." },
  "event": {
    "node_id": "nd_...",
    "session_id": "ses_...",
    "seq": 0,
    "type": "session.text.delta",
    "data": { },
    "time": "2026-11-11T12:00:00Z"
  }
}
```

| 帧 | 方向 | 说明 |
|---|---|---|
| `hello` | agent → 控制面 | 注册：`node_name`、`agent_version`、`os`、`arch`、`capabilities[]` |
| `welcome` | 控制面 → agent | 返回分配/复用的 `node_id`；**写出成功后才允许路由请求** |
| `heartbeat` | agent → 控制面 | 默认 10 秒；控制面按心跳窗口判定失联 |
| `request` | 控制面 → agent | 浏览器动作转成的 RPC |
| `response` | agent → 控制面 | 结果或稳定错误码 |
| `event` | agent → 控制面 | OpenCode 事件与 PTY 事件 |
| `cancel` | 控制面 → agent | 预留；当前 agent 不处理服务器发起的取消 |

控制面网关会：

- 用常量时间比较 `Authorization: Bearer <enrollment_token>`，**令牌只出现在握手头**；
- 拒绝带 `Origin` 的 agent 握手，避免网页借回环地址注册节点；
- 单帧上限 1 MiB，握手 10 秒超时，帧间心跳窗口默认 35 秒；
- 用连接身份覆盖事件里的 `node_id`，防止节点伪造其他节点。

### 2.3 已实现的节点 RPC

以下方法由 `agentd/handler` 分发，并同时出现在 hello 的 `capabilities` 中。控制面应以此判断功能是否可用。

| RPC | 参数要点 | 返回 | 下游 OpenCode |
|---|---|---|---|
| `node.info` | 无 | 节点名、OS、架构、agent 版本 | 无 |
| `opencode.status` | 无 | `{running,mode,baseURL}` | 健康状态快照 |
| `opencode.start` | 无 | supervisor 状态 | 启动/附着服务 |
| `opencode.restart` | 无 | 空 | 重启受管服务 |
| `opencode.stop` | 无 | 空 | 停止受管服务 |
| `session.list` | `directory`（当前仅兼容，不用于过滤） | `Session[]` | `GET /api/session` |
| `session.create` | `directory`（绝对路径） | `Session` | `POST /api/session`（`location.directory`） |
| `session.prompt` | `session_id`、`text`、`idempotency_key`（必填） | inbox item ID | `POST /api/session/{id}/prompt` |
| `session.messages` | `session_id` | `Message[]` | `GET /api/session/{id}/message?order=asc&limit=200` |
| `session.diff` | `session_id`、`directory`、可选 `from`/`to`/`context` | `FileDiff[]` | `GET /api/session/{id}/diff` |
| `session.interrupt` | `session_id` | `{interrupted}` | `POST /api/session/{id}/interrupt?resume=false` |
| `permission.reply` | `session_id`、`permission_id`、`decision`、`message` | 空 | `POST /api/session/{id}/permission/{permissionID}/reply` |
| `file.list` | `directory`、`path`（相对） | `FileInfo[]` | `GET /api/fs/list` |
| `file.read` | `directory`、`path`、`max_bytes` | UTF-8/base64 投影 | `GET /api/fs/read/{path}`（Range） |
| `pty.create` | `session_id`、`directory`、`cols`/`rows`、可选命令 | `PersistentTerminal` | `POST /api/experimental/session/{id}/terminal` |
| `pty.attach` | `pty_id` | `{attached:true}` | `connect-token` + 持久 PTY WebSocket |
| `pty.write` | `pty_id`、base64 `data` | 空 | 终端输入 |
| `pty.resize` | `pty_id`、`cols`/`rows` | 空 | 终端 resize |
| `pty.close` | `pty_id` | 空 | 删除持久终端 |

实现细节：

- `session.prompt` 的幂等键由 agent 持有，**不会**透传给 OpenCode。同一 `session_id + idempotency_key` 的成功回执会重放；失败不缓存，允许重试。
- `file.read` 单次上限 512 KiB（agent 与浏览器 API 双重限制），二进制自动转 base64。
- `workspace_roots` 非空时，`session.create`、`file.*`、`session.diff`、`pty.create` 全部做绝对路径 + 符号链接解析后的边界校验。
- PTY 绑定 Session 与目录，不提供任意路径的通用终端。

### 2.4 已实现的浏览器 API

| 方法 | 路径 | 转发的 RPC |
|---|---|---|
| `GET` | `/api/nodes` | 读 SQLite + 实时注册表 |
| `GET` | `/api/nodes/{id}` | 同上 |
| `GET` | `/api/nodes/{id}/health` | `opencode.status` |
| `GET` | `/api/nodes/{id}/files?directory=&path=` | `file.list` |
| `GET` | `/api/nodes/{id}/files/content?directory=&path=` | `file.read` |
| `GET` | `/api/sessions?node=&directory=` | `session.list` |
| `POST` | `/api/sessions` | `session.create`（body: `node_id`、`directory`） |
| `GET` | `/api/sessions/{node}/{session}/messages` | `session.messages` |
| `POST` | `/api/sessions/{node}/{session}/prompt` | `session.prompt`（`text`、`idempotency_key`） |
| `GET` | `/api/sessions/{node}/{session}/diff?directory=&from=&to=&context=` | `session.diff` |
| `POST` | `/api/sessions/{node}/{session}/interrupt` | `session.interrupt` |
| `POST` | `/api/sessions/{node}/{session}/pty` | `pty.create` |
| `DELETE` | `/api/sessions/{node}/{session}/pty/{ptyID}` | `pty.close` |
| `GET` | `/api/pty/{node}/{session}/{ptyID}/ws` | `pty.attach` + 双向桥 |
| `POST` | `/api/sessions/{node}/{session}/permissions/{permissionID}/reply` | `permission.reply` |
| `GET` | `/api/events?node=&session=` | 订阅事件中心（SSE） |

错误映射是稳定的：

| 条件 | HTTP | `code` |
|---|---|---|
| 未登记节点 | 404 | `not_found` |
| 已登记但离线/连接已关闭 | 409 | `node_offline` |
| 节点调用超时（默认 30 秒） | 504 | `agent_timeout` |
| 节点返回业务错误 | 502 | `agent_error` |
| 浏览器参数错误 | 400 | `invalid_request` |

浏览器 API **没有独立登录态**，安全边界是“只监听回环”。不要把它直接暴露到公网；需要远程访问时使用反向代理 + 认证，或后续引入控制面认证层。

## 3. 节点侧：托管 OpenCode

### 3.1 共享 server 而不是每次起进程

`agentd/supervisor` 的策略是“能附着就附着”：

1. 先健康检查 `GET /api/info`。
2. 已有健康实例 → `attached`，不启动第二个进程。
3. 没有实例 → 启动 `opencode serve --hostname 127.0.0.1 --port <port>`，轮询到健康后标记 `managed`。
4. 受管进程启动失败或凭据被拒绝 → 停止并清空跟踪状态；`401` 不重试。
5. 已启动的进程**不随请求上下文取消**，只有 `opencode.stop`、启动失败或子进程退出才结束。

这意味着：

- 一个节点通常共享一个 OpenCode server，多个项目通过 Location 区分。
- 控制面不要假设“一个 Session 一个进程”。
- `opencode.restart` 会影响该 server 上的所有 Session，调用前应在 UI 明确提示。

### 3.2 认证与网络

- supervisor 用环境变量把密码交给子进程；agent 的 HTTP 客户端用固定用户名 `opencode` 做 Basic Auth。
- OpenCode 只监听 `127.0.0.1`；agent 也在本机访问，不经过控制面。
- 不要为了“方便调试”把 OpenCode 端口暴露到 `0.0.0.0`；所有远程访问都应经过 lework 的 WSS 通道。

### 3.3 Location 与工作目录

- 创建 Session 时必须给出绝对目录；agent 用 `location.directory` 传给 OpenCode。
- 同一目录下的多个 Session 共享该 Location 的配置、Agent、MCP 与插件。
- `workspace_roots` 是 lework 自己的边界，不是 OpenCode 的边界。OpenCode 侧仍有 `external_directory` 权限；两者应同时配置。

推荐配置组合：

```yaml
# agent.yaml
workspace_roots:
  - /home/me/projects
opencode:
  binary: opencode
  port: 4096
  env:
    OPENCODE_PASSWORD: "<random>"
```

```jsonc
// opencode.jsonc（项目或全局）
{
  "permissions": [
    { "action": "external_directory", "resource": "*", "effect": "deny" }
  ],
  "policies": [
    { "action": "tool.use", "resource": "shell:git push *", "effect": "deny" }
  ]
}
```

## 4. 会话与消息链路

### 4.1 创建会话

```http
POST /api/sessions
Content-Type: application/json

{ "node_id": "nd_x", "directory": "/home/me/projects/app" }
```

控制面校验目录是绝对路径，转发给节点，节点再校验 `workspace_roots`，最后调用 OpenCode。返回的是 OpenCode `Session.Info` 原样投影。

控制面**不持久化 Session**。页面刷新后靠 `session.list` 重新拉取；Session 的标题、Agent、模型、成本、token、归档状态都以 OpenCode 返回为准。

### 4.2 发送 prompt

```http
POST /api/sessions/nd_x/ses_y/prompt
Content-Type: application/json

{
  "text": "修复登录超时",
  "idempotency_key": "ui-7f3c2a"
}
```

语义是“入队”，不是同步等待：

1. 控制面校验非空文本与幂等键；
2. 节点用幂等键去重后调用 OpenCode；
3. OpenCode 返回 inbox item ID；
4. 真正的回复通过 `/api/events` 的 SSE 获得；
5. 需要完整历史时再调用 `session.messages`。

前端应为每次发送生成稳定的幂等键（例如“会话 ID + 本地消息 ID”），重试时复用同一个键。

### 4.3 读取消息

`session.messages` 使用 `order=asc&limit=200`，把一轮对话按时间正序取回。`Message` 被投影为：

```jsonc
{
  "id": "msg_...",
  "role": "user|assistant|idle|...",
  "text": "用户文本或助手拼接文本",
  "parts": [
    { "type": "text", "text": "..." },
    { "type": "tool", "tool": "shell", "callID": "...", "state": { } }
  ],
  "finish": "...",
  "outcome": "...",
  "error": { "type": "...", "message": "...", "status": 0 }
}
```

注意：

- `role` 来自 OpenCode 的 `type` 判别字段，不是 OpenAI 风格的 role。
- 工具卡片保留原始 `state`，控制面不解释 provider 特有结构。
- `limit=200` 是单页上限；超长会话需要分页或按 turn 拉取，不能假设一次拿全。

### 4.4 中断

`session.interrupt` 固定 `resume=false`：停止当前执行，但**不恢复**排队的 steering 消息。若未来要支持“中断后继续”，需要把 `resume` 参数暴露到 RPC 与浏览器 API，并在 UI 上区分“停止”和“停止并保留队列”。

## 5. 事件与流式 UI

### 5.1 OpenCode 事件是 live-only

官方 `GET /api/event` 的 SSE：

- 连接建立后先发 `server.connected`；
- 只推送**连接之后**发生的事件；
- 没有重放、没有 Last-Event-ID、没有自动重连；
- 断线期间的事件不会补发。

需要历史时只能用快照接口（`session.messages`、`session.list`、`session.diff`）重新对账。

### 5.2 lework 的转发方式

节点侧 `runner.forwardEvents`：

- 订阅 `/api/event`；
- 把 OpenCode 事件包成 `protocol.Event`；
- `Seq` 固定为 `0`（I0 没有稳定序列）；
- `SessionID` 从 `data.sessionID` 提升到事件顶层；
- 事件流关闭时先发 `agent.resync_required`，再让连接失败并重连。

控制面侧：

- 网关把事件交给进程内 `event.Hub`；
- `/api/events` 用 SSE 扇出，支持 `node` 与 `session` 过滤；
- 慢消费者缓冲区（256）满时被 Hub 淘汰，浏览器收到 `event: resync`；
- 空闲每 15 秒发注释心跳，避免代理断开。

### 5.3 前端必须处理的两种 resync

| 信号 | 来源 | 正确处理 |
|---|---|---|
| `agent.resync_required`（普通 SSE data） | 节点到 OpenCode 的事件流断开 | 标记该节点事件可能缺失；重新拉取当前 Session 列表/消息，或提示用户刷新 |
| `event: resync`（SSE 命名事件） | 控制面 Hub 淘汰慢消费者 | 重新建立 `/api/events`，并重新拉取快照 |

不要尝试用 `Seq` 做断点续传：当前所有事件都是 `Seq=0`。如果要做可靠回放，需要先升级协议（见 5.4）。

### 5.4 建议的可靠事件升级

OpenCode 已经为部分事件提供 `durable` 字段（`aggregateID`、`seq`、`version`），并可读 `/api/experimental/session/{id}/log`。控制面可以：

1. 在协议中保留 `Seq`，但改为“节点侧为事件分配单调序号”，用于检测 WSS 内丢失；
2. 事件落盘到节点或控制面的短期 ring buffer，支持 `Last-Event-ID` 式补发；
3. 重连后先回放 buffer，再调用快照接口补齐；
4. 对 `session.text.delta` 这类 live-only 事件不做持久化，只保证 UI 最终通过 `session.messages` 对齐。

这件事的优先级高于“再加一个页面”：没有可靠事件，审批和流式输出都会在弱网下出现空洞。

## 6. 权限审批

### 6.1 链路

```text
OpenCode 触发 ask
  → SSE permission.asked {id, sessionID, action, resources[], save[], metadata, source, message}
  → agent 原样转发
  → 控制面 SSE
  → 浏览器展示
  → POST /api/sessions/{node}/{session}/permissions/{permissionID}/reply
  → agent permission.reply
  → OpenCode POST /api/session/{id}/permission/{requestID}/reply
```

### 6.2 决策语义

| 浏览器值 | OpenCode 值 | 结果 |
|---|---|---|
| 允许一次 | `once` | 只批准当前请求 |
| 总是允许 | `always` | 批准并保存项目级 allow pattern |
| 拒绝 | `reject` | 拒绝当前请求，并拒绝该 Session 其他待处理请求 |

UI 必须：

- 展示 `resources[]` 全量，尤其是复合 shell 命令与多文件 patch；
- 展示 `save[]`，让用户知道 `always` 会保存什么；
- 提示 `reject` 的连带影响；
- 不要把 `message` 当作可信文本直接渲染（来自模型/工具）。

### 6.3 策略与权限的分工

- `permissions`：可提示的规则，适合用户级/项目级偏好。
- `policies`：不可提示的强制约束，适合管理员/控制面下发的硬限制。
- 控制面若要做“组织策略”，应生成 `policies`，不要用 UI 层拦截伪装成安全边界。

## 7. 文件、Diff 与快照

### 7.1 文件浏览

```http
GET /api/nodes/{node}/files?directory=/work/app&path=src
GET /api/nodes/{node}/files/content?directory=/work/app&path=src/main.go
```

- `directory` 必须绝对；`path` 相对目录，禁止 `..` 与符号链接逃逸。
- `file.list` 只返回直接子项；目录以 `/` 结尾。
- `file.read` 返回 `{path,encoding,content,size,truncated}`；UTF-8 文本直接给字符串，二进制给 base64。
- 单次 512 KiB；更大的文件由前端提示下载或分段读取。

### 7.2 Diff

`session.diff` 依赖 OpenCode 的 turn 快照：

- `from`/`to` 是用户消息 ID，可只看某一轮；
- `context` 是上下文行数；
- 非 Git 工作区可能返回空数组；
- 控制面不做 diff 计算，只做展示。

### 7.3 快照与 undo

OpenCode 的 `snapshots` 依赖 Git，只覆盖 Session 的活动目录。控制面若要提供 undo：

- 先确认该 Location 是 Git 工作区且 `snapshots` 未关闭；
- 使用 OpenCode 的 revert API，而不是自己 `git checkout`；
- 明确提示“只影响该 Session 活动目录”，不会回滚其他项目或其他节点。

## 8. PTY 与终端

### 8.1 两层终端概念

| 层 | 名称 | 用途 |
|---|---|---|
| OpenCode | persistent PTY / terminal | 绑定 Session 的持久终端，实验性 API |
| lework agent | `pty.*` RPC | 把 OpenCode 终端桥接到控制面 |
| 控制面 | `/api/pty/.../ws` | 浏览器同源 WebSocket 终端 |

### 8.2 浏览器协议

- 二进制帧：终端输入/输出；
- 文本帧：
  - 客户端 → 服务端：`{"type":"resize","cols":80,"rows":24}`、`{"type":"close"}`
  - 服务端 → 客户端：`{"type":"exit"}`、`{"type":"error","message":"..."}`

限制：

- 输入单帧 64 KiB；
- 输出单事件 1 MiB；
- 终端尺寸 1–1000；
- 目录必须在 `workspace_roots` 内；
- 浏览器断开时控制面会尽力关闭远端 PTY；
- agent 连接结束时关闭所有 PTY，避免遗留 shell 进程。

### 8.3 安全提示

PTY 等价于宿主用户权限的 shell。不要把“能打开终端”当作普通 UI 功能：

- 默认只对可信用户开放；
- 终端创建必须绑定已存在的 Session 与合法目录；
- 不建议提供任意命令入口；命令与参数应由服务端约束或至少审计；
- 记录“谁在何时打开了哪个节点的终端”，但不要记录终端内容。

## 9. 能力扩展路线

按“先可靠、再丰富”的顺序推进。

### P0：把现有链路做稳

1. **事件可靠性**：节点侧序号 + ring buffer + 重连补发；前端统一 resync 处理。
2. **会话分页**：`session.messages` 支持 cursor/limit，避免 200 条截断。
3. **审批闭环**：展示 `save[]`、`resources[]`，支持 `always` 后的规则管理入口。
4. **节点版本协商**：hello 增加协议版本与 OpenCode 版本，控制面按版本隐藏不支持的按钮。
5. **错误可观测性**：统一记录 RPC 方法、节点、耗时、错误码；不记录 prompt 与文件内容。

### P1：补齐 OpenCode 已有、控制面未暴露的能力

| 能力 | OpenCode 端点 | 建议 RPC/浏览器接口 |
|---|---|---|
| Agent 列表 | `GET /api/agent` | `agent.list` → `GET /api/nodes/{id}/agents?directory=` |
| 模型目录 | `GET /api/model` | `model.list` → 只读展示，不缓存凭据 |
| Provider 状态 | `GET /api/provider` | `provider.list` → 展示可用性，不返回密钥 |
| 技能列表 | `GET /api/skill` | `skill.list` |
| 命令列表/执行 | `GET /api/command`、`POST /api/session/{id}/command` | `command.list` / `session.command` |
| MCP 状态 | `GET /api/mcp`、`mcp.status.changed` | `mcp.list` + 事件 |
| MCP 资源 | `GET /api/mcp/{name}/resource` | `mcp.resource.list/read` |
| 会话重命名/归档 | `PATCH /api/session/{id}` | `session.update` |
| 会话 fork/revert | `POST /api/session/{id}/fork`、`.../revert` | `session.fork` / `session.revert` |
| 用量与成本 | `session.usage.updated`、`Session.Info.cost` | 事件 + 会话详情 |
| 表单/提问 | `form.*`、`question` 工具 | 事件 + 回复接口 |
| 文件搜索 | `GET /api/fs/find` | `file.find` |
| VCS 状态/分支 | `GET /api/vcs/status`、`/api/vcs/branch` | `vcs.status` |
| 工作树 | `/api/worktree/*` | `worktree.list/create/remove` |
| 附件 | `media`、prompt `files` | 上传需先设计大小/类型/存储边界 |

原则：**只读目录类能力优先**（Agent、模型、技能、命令、MCP 状态），写能力必须带幂等键与边界校验。

### P2：产品化

- 多节点调度：按 `workspace_roots`、负载、标签选择节点；控制面只做路由，不复制 Session。
- 跨节点 Git 任务上下文：把仓库身份与 worktree 作为路由键，而不是把代码搬到控制面。
- 配置与 Action 控制面：下发 `opencode.jsonc` 片段、`permissions`/`policies`、MCP 与插件白名单；只写节点文件，不代理 OpenCode 内部状态。
- TUI 共享：OpenCode 的 TUI 是客户端，控制面不应复制其键位/渲染；最多提供“打开终端”入口。
- 观测：节点健康、OpenCode 健康、事件延迟、审批等待时长、错误率。

## 10. 端点映射总表

| 用户动作 | 浏览器 API | 节点 RPC | OpenCode |
|---|---|---|---|
| 看节点 | `GET /api/nodes` | 无 | 无 |
| 看 OpenCode 健康 | `GET /api/nodes/{id}/health` | `opencode.status` | `GET /api/info`（supervisor 缓存） |
| 启动/重启/停止 | 尚未暴露 | `opencode.start/restart/stop` | 进程管理 |
| 列会话 | `GET /api/sessions?node=` | `session.list` | `GET /api/session` |
| 建会话 | `POST /api/sessions` | `session.create` | `POST /api/session` |
| 发消息 | `POST .../prompt` | `session.prompt` | `POST .../prompt` |
| 看消息 | `GET .../messages` | `session.messages` | `GET .../message` |
| 中断 | `POST .../interrupt` | `session.interrupt` | `POST .../interrupt` |
| 看 diff | `GET .../diff` | `session.diff` | `GET .../diff` |
| 审批 | `POST .../permissions/{id}/reply` | `permission.reply` | `POST .../permission/{id}/reply` |
| 列文件 | `GET /api/nodes/{id}/files` | `file.list` | `GET /api/fs/list` |
| 读文件 | `GET /api/nodes/{id}/files/content` | `file.read` | `GET /api/fs/read/{path}` |
| 开终端 | `POST .../pty` | `pty.create` | `POST /api/experimental/session/{id}/terminal` |
| 终端 I/O | `WS /api/pty/.../ws` | `pty.attach/write/resize` | 持久 PTY WebSocket |
| 事件 | `GET /api/events` | `event` 帧 | `GET /api/event` |

## 11. 风险与约束

| 风险 | 影响 | 缓解 |
|---|---|---|
| OpenCode 事件 live-only | 断线丢事件，UI 状态漂移 | resync + 快照对账；升级为带序号的可补发通道 |
| `Seq=0` | 无法检测丢帧 | 节点侧分配序号；不要依赖当前字段 |
| 单节点共享 server | 重启影响所有 Session | UI 明确提示；按节点维护维护窗口 |
| 浏览器 API 无登录 | 回环外暴露即风险 | 只绑回环；反向代理加认证；不要转发端口 |
| `workspace_roots` 为空 | 文件/PTY 可访问任意路径 | 生产配置必须显式设置 |
| shell/PTY 等价宿主权限 | 命令执行风险 | 窄 allowlist、`policies` 硬拒绝、审计 |
| OpenCode 版本漂移 | 实验性端点（PTY、wait）可能变化 | hello 上报版本；能力探测；失败降级 |
| 大文件/大输出 | 帧超限或内存膨胀 | 512 KiB 读上限、64 KiB 输入、1 MiB 输出上限 |
| 敏感数据 | prompt、文件、凭据进入日志 | 不记录 params/prompt；日志脱敏；heap 快照不外传 |

## 12. 验收清单

接入一个新能力前，至少确认：

- [ ] 该能力的状态归属明确（OpenCode / 节点 / 控制面）。
- [ ] 节点 RPC 有幂等键或明确的“可重复调用”语义。
- [ ] 路径参数经过绝对路径与 `workspace_roots` 校验。
- [ ] 浏览器接口不回传令牌、密钥、完整 prompt。
- [ ] 事件断线时有 resync 路径，并能用快照接口对齐。
- [ ] 节点离线、超时、OpenCode 401/503 都有稳定错误码。
- [ ] UI 对慢节点有超时与重试，不无限转圈。
- [ ] 新增端点已登记到本文与 `12-HTTP-API参考.md`。

## 13. 相关文档

- [01-产品与架构.md](01-产品与架构.md)
- [06-会话与消息模型.md](06-会话与消息模型.md)
- [09-权限与策略.md](09-权限与策略.md)
- [12-HTTP-API参考.md](12-HTTP-API参考.md)
- [13-客户端-SDK-ACP.md](13-客户端-SDK-ACP.md)
- [16-故障排查与运维.md](16-故障排查与运维.md)
