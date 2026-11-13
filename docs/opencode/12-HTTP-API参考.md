# 12 HTTP API 参考

> 来源：官方 `openapi.json`（`title: opencode HttpApi`，139 条 path）、`cli/web`、`build/client`、`build/plugins/server`。
> 本文是**工程索引**：完整字段以 `https://opencode.ai/docs/api` 与 `openapi.json` 为准。端点数量多，先看第 3 节的响应约定，再按资源查表。

## 1. 基址、认证与配对

### 1.1 基址

- 默认只监听回环：`http://127.0.0.1:<port>`。
- 端口：macOS/Linux/WSL 默认 `49374`，Windows 默认 `28894`；可用 `opencode service set port` 或 `opencode serve --port` 改。
- `opencode serve` 前台运行同一个 server；`opencode service` 管理共享后台服务。

### 1.2 HTTP Basic Auth

- 用户名固定为 **`opencode`**。
- 密码来自 `OPENCODE_PASSWORD`（`OPENCODE_SERVER_PASSWORD` 仍兼容）。
- 不设置时 server 生成密码；`opencode serve` 启动时会打印。
- 修改密码会让所有已登录会话失效。

```bash
curl -u opencode:a-long-secret http://127.0.0.1:4096/api/info
```

`OPENCODE_SERVER_USERNAME` 在 V2 **不再支持**。

### 1.3 配对（浏览器/移动端）

```http
POST /api/pair
→ { "code": "...", "expires_in": 300 }

GET /auth/connect/{code}
→ { "token": "..." }        // PairingSession
```

- 配对链接一次性、5 分钟过期。
- 浏览器打开链接后获得会话 cookie，加载 Web UI；会话 30 天。
- 也可用 `opencode pair` 生成链接/二维码；`--remote` 通过 OpenTunnel 暴露。

### 1.4 运行期信息

```http
GET /api/info
→ ServerInfo
```

```json
{
  "version": "2.x.y",
  "pid": 12345,
  "hostname": "host",
  "urls": ["http://127.0.0.1:49374"],
  "paths": { "tmp": "/tmp/opencode" },
  "capabilities": { "persistentPty": true }
}
```

`/api/info` 不要求 Basic Auth 之外的特殊头，是健康检查与版本探测的首选。

## 2. Location：多目录的核心

大多数读取接口接受 `location` 查询参数（OpenAPI `style: deepObject`）：

```http
GET /api/model?location[directory]=/work/app
GET /api/agent?location[directory]=/work/app
```

- 省略时使用请求的默认目录（通常为 server 工作目录）。
- 会话相关接口不需要 `location`：会话自身记录 location。
- 列表型读取返回 `{ location, data }`，其中 `location` 是 `Location.PublicRef`：

```json
{ "location": { "directory": "/work/app" }, "data": [ ... ] }
```

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/location` | 当前 location 与 project 信息 |
| `POST` | `/api/location/reload` | 重新加载配置/插件（204；不可用时 503） |

`Location.PublicInfo`：

```json
{
  "directory": "/work/app",
  "project": {
    "id": "prj_...",
    "directory": "/work/app",
    "canonical": "/work/app"
  }
}
```

## 3. 响应约定

### 3.1 四种形态

| 形态 | 示例 | 说明 |
|---|---|---|
| 直接对象 | `GET /api/info` → `ServerInfo` | 少数无 location 的接口 |
| `{ location, data }` | `GET /api/agent`、`/api/fs/list`、`/api/mcp`、`/api/vcs/diff` | 资源列表/读取，`data` 是数组或对象 |
| `{ data }` | 多数写接口/生成接口 | 结果包在 `data` 中 |
| `204 No Content` | `PATCH /api/session/{id}`、`POST .../interrupt` 之外的写操作 | 无 body |

列表分页统一用 `cursor`：

```json
{
  "data": [ ... ],
  "cursor": { "previous": null, "next": "..." }
}
```

会话列表的 cursor 是 `SessionsCursor`；消息列表是字符串 cursor。

### 3.2 错误

错误 body 是带 `_tag` 的编码对象：

```json
{ "_tag": "SessionNotFoundError", "sessionID": "ses_...", "message": "..." }
```

常见错误：

| `_tag` | HTTP | 含义 |
|---|---|---|
| `InvalidRequestError` | 400 | 参数/body 校验失败；可能带 `kind`、`field` |
| `UnauthorizedError` | 401 | Basic Auth 失败 |
| `ForbiddenError` | 403 | 策略/能力禁止（如 client MCP） |
| `LocationNotFoundError` | 404 | 目录未加载/不存在；带 `location` |
| `SessionNotFoundError` | 404 | 会话不存在；带 `sessionID` |
| `SessionBusyError` | 409 | 会话正在执行，操作不允许 |
| `ConflictError` | 409 | ID/资源冲突；可能带 `resource` |
| `ServiceUnavailableError` | 503 | 服务/子系统不可用；可能带 `service` |

客户端应优先按 `_tag` 分支，而不是匹配 message。

### 3.3 幂等与 ID

- `POST /api/session` 可传 `id`；同 ID 重试返回同一会话。
- `POST /api/session/{id}/prompt` 可传 `id`；同 ID 重试返回原 admission，不重复执行。
- `POST /api/session/{id}/compact` 可传 `id`；同 ID 重试幂等，不同请求复用同 ID 会 409。
- 消息/工具事件里的 `id` 是关联输入、进度、结果的键。

### 3.4 时间与金额

- 时间戳是 Unix **毫秒**（`number`）。
- 金额是 `Money.USD`：`{ amount, currency: "USD" }`。
- token 是 `TokenUsage.Info`（input/output/reasoning/cache.read/cache.write）。

## 4. 事件流（SSE）

```http
GET /api/event
Accept: text/event-stream
```

可选 `types` 过滤（字符串或数组）：

```http
GET /api/event?types=session.created&types=session.text.delta
```

行为：

- 连接后**先发** `server.connected`，用于客户端判断“已连上、需要重新同步”。
- 事件是 **live-only**：没有重放、没有自动重连。
- 断线后必须重新订阅；重连后先拉快照/消息，再消费新事件。
- 每个事件形状：

```json
{
  "id": "evt_...",
  "created": 1760000000000,
  "type": "session.text.delta",
  "location": { "directory": "/work/app" },
  "data": { "sessionID": "...", "assistantMessageID": "...", "ordinal": 0, "delta": "..." },
  "durable": { "aggregateID": "ses_...", "seq": 42, "version": "..." }
}
```

- `durable` 存在表示该事件属于 durable session log，可用 `GET /api/experimental/session/{id}/log` 读取；**不代表 SSE 会重放**。
- **增量事件没有 `durable`**：`session.text.delta`、`session.reasoning.delta`、`session.tool.input.delta`、`session.tool.progress`、`session.compaction.delta`。它们的完整结果在对应的 `*.ended` / `*.success` / `*.failed` durable 事件里。
- `permission.asked` / `permission.replied` 也没有 `durable`，断线后需查 `/api/permission/request`。
- 插件在 server 内订阅时不会收到 `server.connected`（它不是业务事件）。

完整事件类型见 [附录B-事件与权限动作速查.md](附录B-事件与权限动作速查.md)。

## 5. 端点总表

> 下表按资源分组。`GET/POST` 等缩写表示同一路径的不同方法。标 “experimental” 的路径可能在版本间变化。

### 5.1 Server / Location

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/info` | server 信息 |
| `POST` | `/api/pair` | 创建配对码 |
| `GET` | `/auth/connect/{code}` | 兑换配对码 |
| `GET` | `/api/location` | 当前 location/project |
| `POST` | `/api/location/reload` | 重载配置 |

### 5.2 Agent / Plugin

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/agent` | 列出 Agent（`{location,data}`） |
| `GET` | `/api/agent/{agentID}` | 单个 Agent |
| `GET` | `/api/plugin` | 列出插件 |
| `GET` | `/api/plugin/gui` | 读取插件 GUI bundle |
| `POST` | `/api/plugin/check` | 检查插件更新 |
| `POST` | `/api/plugin/update` | 更新插件（204） |

### 5.3 Session

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/session` | 列出会话（分页/搜索/过滤） |
| `POST` | `/api/session` | 创建会话 |
| `GET` | `/api/session/active` | 有执行在跑的会话 |
| `GET` | `/api/session/{sessionID}` | 获取会话 |
| `PATCH` | `/api/session/{sessionID}` | 更新会话（204） |
| `DELETE` | `/api/session/{sessionID}` | 删除会话（204） |
| `POST` | `/api/session/{sessionID}/fork` | 分叉 |
| `POST` | `/api/session/{sessionID}/agent` | 切换 Agent（204） |
| `POST` | `/api/session/{sessionID}/model` | 切换模型（204） |
| `POST` | `/api/session/{sessionID}/move` | 移动会话（204） |
| `POST` | `/api/session/{sessionID}/prompt` | 提交 prompt → inbox item |
| `POST` | `/api/session/{sessionID}/command` | 运行斜杠命令（204） |
| `POST` | `/api/session/{sessionID}/synthetic` | 注入合成消息 |
| `POST` | `/api/session/{sessionID}/shell` | 会话内 shell（204） |
| `POST` | `/api/session/{sessionID}/compact` | 手动压缩 |
| `POST` | `/api/session/{sessionID}/interrupt` | 中断执行 |
| `POST` | `/api/session/{sessionID}/background` | 阻塞工具转后台（204） |
| `POST` | `/api/session/{sessionID}/view` | 标记已查看（204） |
| `GET` | `/api/session/{sessionID}/context` | 上下文构成 |
| `GET` | `/api/session/{sessionID}/diff` | 按轮次 diff |
| `GET` | `/api/session/{sessionID}/message` | 消息列表（cursor） |
| `GET` | `/api/session/{sessionID}/message/{messageID}` | 单条消息 |
| `GET` | `/api/session/{sessionID}/inbox` | 待投递队列 |
| `PATCH` | `/api/session/{sessionID}/inbox/{inboxID}` | 修改队列项（204） |
| `DELETE` | `/api/session/{sessionID}/inbox/{inboxID}` | 取消队列项（204） |
| `POST` | `/api/session/{sessionID}/generate` | 基于会话上下文生成文本 |
| `PUT` | `/api/session/{sessionID}/environment` | 设置会话环境（204） |
| `POST` | `/api/session/{sessionID}/revert/stage` | 暂存回滚 |
| `DELETE` | `/api/session/{sessionID}/revert` | 清除暂存（204） |
| `POST` | `/api/session/{sessionID}/revert/commit` | 提交回滚（204） |
| `GET` | `/api/session/{sessionID}/form` | 列出表单 |
| `POST` | `/api/session/{sessionID}/form` | 创建表单 |
| `GET` | `/api/session/{sessionID}/form/{formID}` | 获取表单 |
| `DELETE` | `/api/session/{sessionID}/form/{formID}` | 取消表单（204） |
| `POST` | `/api/session/{sessionID}/form/{formID}/reply` | 回复表单（204） |

Session 相关 experimental：

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/experimental/session/stats` | 聚合统计 |
| `POST` | `/api/experimental/session/import` | 导入会话 |
| `GET` | `/api/experimental/session/{sessionID}/export` | 导出会话 |
| `POST` | `/api/experimental/session/{sessionID}/wait` | 等待空闲（204） |
| `POST` | `/api/experimental/session/{sessionID}/skill` | 激活技能（204） |
| `GET` | `/api/experimental/session/{sessionID}/log` | 会话 durable log |
| `GET/PUT/DELETE` | `/api/experimental/session/{sessionID}/instructions/entries`、`.../{key}` | 会话级指令条目 |
| `GET` | `/api/experimental/session/{sessionID}/terminal` | 最近受控终端 |
| `GET` | `/api/experimental/session/{sessionID}/terminal/read` | 读取终端输出 |
| `POST` | `/api/experimental/session/{sessionID}/terminal` | 创建/控制终端 |

### 5.4 Model / Generate / Voice

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/model` | 列出模型（`types` 过滤） |
| `GET` | `/api/model/default` | 默认模型 |
| `POST` | `/api/experimental/generate/text` | 独立文本生成 |
| `POST` | `/api/experimental/generate` | 旧路径（deprecated） |
| `POST` | `/api/experimental/generate/image` | 图片生成 |
| `POST` | `/api/experimental/generate/decisions` | 决策生成 |
| `GET` | `/api/experimental/voice/models` | 语音模型 |
| `POST` | `/api/experimental/voice/session` | 创建语音会话 |

### 5.5 Provider / Integration / Credential

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/provider` | 列出 provider |
| `GET` | `/api/provider/{providerID}` | 单个 provider |
| `GET` | `/api/integration` | 列出集成 |
| `GET` | `/api/integration/{integrationID}` | 单个集成 |
| `POST` | `/api/experimental/integration/wellknown` | 添加 well-known 集成（204） |
| `POST` | `/api/integration/{integrationID}/connect/key` | 用 key 连接（204） |
| `POST` | `/api/integration/{integrationID}/connect/external` | 用外部凭据连接（204） |
| `POST` | `/api/integration/{integrationID}/connect/oauth` | 开始 OAuth |
| `GET` | `/api/integration/{integrationID}/connect/oauth/{attemptID}` | 查询 OAuth 状态 |
| `DELETE` | `/api/integration/{integrationID}/connect/oauth/{attemptID}` | 取消 OAuth（204） |
| `POST` | `/api/integration/{integrationID}/connect/oauth/{attemptID}/complete` | 完成 OAuth（204） |
| `POST` | `/api/integration/{integrationID}/connect/command` | 开始命令式连接 |
| `GET` | `/api/integration/{integrationID}/connect/command/{attemptID}` | 查询命令连接状态 |
| `DELETE` | `/api/integration/{integrationID}/connect/command/{attemptID}` | 取消命令连接（204） |
| `GET` | `/api/credential` | 列出凭据 |
| `POST` | `/api/credential` | 创建凭据 |
| `PATCH` | `/api/credential/{credentialID}` | 更新（204） |
| `DELETE` | `/api/credential/{credentialID}` | 删除（204） |
| `POST` | `/api/credential/{credentialID}/activate` | 激活（204） |

### 5.6 MCP

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/mcp` | 列出 server 与状态 |
| `PUT` | `/api/experimental/mcp/{server}` | 添加/替换（204） |
| `DELETE` | `/api/experimental/mcp/{server}` | 移除（204） |
| `POST` | `/api/experimental/mcp/{server}/connect` | 连接（204） |
| `POST` | `/api/experimental/mcp/{server}/disconnect` | 断开（204） |
| `GET` | `/api/mcp/resource` | 列出资源 |
| `POST` | `/api/experimental/client-mcp/token` | client MCP WebSocket token |
| `GET` | `/api/experimental/client-mcp` | 附加 client MCP（WebSocket） |

### 5.7 Project / Form / Permission

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/project` | 列出项目（数组） |
| `PATCH` | `/api/project/{projectID}` | 更新项目 |
| `GET` | `/api/form` | 全局待处理表单 |
| `GET` | `/api/permission/request` | 全局待处理权限请求 |
| `GET` | `/api/permission/saved` | 已保存审批规则 |
| `PATCH` | `/api/permission/saved/{id}` | 修改已保存规则 |
| `DELETE` | `/api/permission/saved/{id}` | 删除已保存规则（204） |
| `POST` | `/api/session/{sessionID}/permission` | 创建权限请求 |
| `GET` | `/api/session/{sessionID}/permission` | 列出会话请求 |
| `GET` | `/api/session/{sessionID}/permission/{requestID}` | 单条 |
| `POST` | `/api/session/{sessionID}/permission/{requestID}/reply` | 回复（204） |

### 5.8 Filesystem / Command / Skill

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/fs/read/*` | 读文件（原始内容） |
| `GET` | `/api/fs/list` | 列目录（`{location,data}`） |
| `GET` | `/api/fs/find` | 查找文件 |
| `POST` | `/api/experimental/fs/write` | 写文件（octet-stream） |
| `POST` | `/api/experimental/fs/watch` | 订阅文件变化（204） |
| `DELETE` | `/api/experimental/fs/watch` | 取消订阅（204） |
| `GET` | `/api/command` | 列出命令 |
| `GET` | `/api/skill` | 列出技能 |

### 5.9 RPC / Event

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/api/rpc/{rpcID}/{method}` | 调用插件 RPC（`{input}` → `{output}`） |
| `GET` | `/api/event` | SSE 事件流 |

### 5.10 PTY / Persistent PTY / Terminal

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/pty` | 列出 PTY |
| `POST` | `/api/pty` | 创建 PTY |
| `GET` | `/api/pty/{ptyID}` | 获取 |
| `PUT` | `/api/pty/{ptyID}` | 更新（如 resize） |
| `DELETE` | `/api/pty/{ptyID}` | 删除（204） |
| `POST` | `/api/pty/{ptyID}/connect-token` | WebSocket token |
| `GET` | `/api/pty/{ptyID}/connect` | WebSocket 连接 |
| `GET` | `/api/experimental/persistent-pty/{ptyID}` | 持久 PTY |
| `PUT` | `/api/experimental/persistent-pty/{ptyID}` | 更新 |
| `DELETE` | `/api/experimental/persistent-pty/{ptyID}` | 删除（204） |
| `GET` | `/api/experimental/persistent-pty/{ptyID}/snapshot` | 快照 |
| `POST` | `/api/experimental/persistent-pty/{ptyID}/connect-token` | token |
| `GET` | `/api/experimental/persistent-pty/{ptyID}/connect` | WebSocket |
| `POST` | `/api/experimental/persistent-pty/shutdown` | 关闭（204） |
| `POST` | `/api/experimental/persistent-pty/handoff` | 交接 |

### 5.11 Media / Schedule / Artifact / Job

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/experimental/media/{hash}` | 读取媒体 blob |
| `POST` | `/api/experimental/schedule` | 创建 schedule |
| `GET` | `/api/experimental/schedule` | 列出 |
| `GET` | `/api/experimental/schedule/{scheduleID}` | 获取 |
| `DELETE` | `/api/experimental/schedule/{scheduleID}` | 删除（204） |
| `POST` | `/api/experimental/schedule/{scheduleID}/pause` | 暂停 |
| `POST` | `/api/experimental/schedule/{scheduleID}/resume` | 恢复 |
| `GET` | `/api/experimental/artifacts/providers` | artifact provider |
| `GET` | `/api/experimental/artifacts` | 列出 artifact |
| `POST` | `/api/experimental/artifacts` | 创建 |
| `GET` | `/api/experimental/artifacts/{artifactID}` | 读取 |
| `PATCH` | `/api/experimental/artifacts/{artifactID}` | 更新 |
| `DELETE` | `/api/experimental/artifacts/{artifactID}` | 删除（204） |
| `GET` | `/api/experimental/artifacts/{artifactID}/view/*` | 查看本地 artifact |
| `GET` | `/api/experimental/job` | 后台任务 |

### 5.12 Shell / Reference / Worktree / VCS

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/shell` | 列出运行中的 shell |
| `POST` | `/api/shell` | 运行 shell |
| `GET` | `/api/shell/{id}` | 获取 |
| `DELETE` | `/api/shell/{id}` | 删除（204） |
| `GET` | `/api/shell/{id}/output` | 读取输出 |
| `GET` | `/api/reference` | 列出引用 |
| `GET` | `/api/worktree` | 列出 worktree |
| `POST` | `/api/worktree` | 创建 |
| `DELETE` | `/api/worktree` | 删除（204） |
| `POST` | `/api/worktree/refresh` | 刷新（204） |
| `POST` | `/api/vcs/init` | 初始化仓库（204） |
| `GET` | `/api/vcs` | VCS 信息 |
| `GET` | `/api/vcs/base` | review base |
| `GET` | `/api/vcs/status` | 状态 |
| `GET` | `/api/vcs/branch` | 分支 |
| `GET` | `/api/vcs/diff` | diff（`{location,data}`） |

### 5.13 Debug / Migration / Websearch / Usage / Config

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/api/debug/heap-dump` | 写 heap snapshot |
| `GET` | `/api/debug/location` | 列出已加载 location |
| `DELETE` | `/api/debug/location` | 驱逐 location（204） |
| `GET` | `/api/experimental/migration/v1` | V1 迁移状态 |
| `GET` | `/api/websearch/provider` | 列出搜索 provider |
| `POST` | `/api/websearch` | 执行搜索 |
| `GET` | `/api/experimental/usage/limits` | 用量限制 |
| `GET` | `/api/config` | 当前配置（数组） |
| `GET` | `/api/config/shell` | 可用 shell |
| `PATCH` | `/api/experimental/config` | 更新全局配置（204） |

## 6. 关键请求/响应 schema

### 6.1 会话创建

见 [06-会话与消息模型.md](06-会话与消息模型.md#21-创建)。字段：`id`、`parentID`、`title`、`agent`、`model`、`location`、`metadata`、`permissions`。

### 6.2 Prompt

```json
{
  "id": "msg_optional",
  "text": "required",
  "files": [{ "uri": "file:///abs/path", "name": "...", "description": "...", "mention": { "start": 0, "end": 5 } }],
  "agents": [{ "name": "explore", "mention": { "start": 0, "end": 8 } }],
  "skills": [{ "id": "effect", "mention": { "start": 0, "end": 7 } }],
  "metadata": {},
  "delivery": "steer",
  "resume": true
}
```

### 6.3 权限回复

```json
{ "decision": "once", "message": "optional feedback" }
```

`decision`: `once` / `always` / `reject`。

### 6.4 Revert

```json
{ "messageID": "msg_...", "files": true }
```

### 6.5 模型引用

```json
{ "providerID": "anthropic", "id": "claude-sonnet-5-5", "variant": "high" }
```

字符串形式 `provider/model#variant` 用于配置与命令；API body 用对象。

### 6.6 结构化错误（消息/工具）

```json
{
  "type": "provider.rate-limit",
  "message": "Rate limit exceeded",
  "status": 429,
  "response": { "body": "..." },
  "retryAfterMs": 5000
}
```

## 7. WebSocket 端点

以下路径不是普通 HTTP，而是升级为 WebSocket：

| 路径 | 用途 |
|---|---|
| `GET /api/pty/{ptyID}/connect` | 交互式终端 |
| `GET /api/experimental/persistent-pty/{ptyID}/connect` | 持久终端 |
| `GET /api/experimental/client-mcp` | 客户端提供的 MCP server |

连接前通常先取 `connect-token`（或 client-mcp token），把 token 放在查询参数/子协议中。具体握手格式以 `@opencode/client` 实现为准。

## 8. CLI 的等价入口

`opencode api` 使用本地服务发现与认证，可避免手写 URL/密码：

```sh
opencode api get /api/info
opencode api post /api/session/ses_example/compact --data '{}'
opencode api get /api/agent
```

- 接受 HTTP method + path，或 OpenAPI operation ID。
- `--data` / `-d` 传 JSON body；`--header` / `-H` 加 header。
- **可能启动共享后台服务**（若没有兼容的健康服务）。

## 9. 控制面关注点

- **认证**：节点 agent 持有 `OPENCODE_PASSWORD`，控制面不应直接持有明文密码；通过 agent 隧道代理请求，或在节点侧注入。
- **Location 语义**：所有读取都要带 `location[directory]`，否则会读到 server 默认目录。控制面缓存键必须包含目录。
- **先订阅后提交**：SSE 是 live-only；先 `GET /api/event`，再 `POST .../prompt`，避免丢首事件。
- **重连策略**：断线后重新订阅，并用 `GET /api/session`、`GET /api/session/{id}/message`、`GET /api/experimental/session/{id}/log` 对齐；不要假设事件会补发。
- **错误按 `_tag` 处理**：尤其 `SessionBusyError`（不要重试 prompt）、`LocationNotFoundError`（目录未加载）、`ServiceUnavailableError`（子系统不可用）。
- **幂等 ID**：控制面为每条用户输入生成稳定 `id`，网络重试不会重复执行。
- **experimental 端点**：`/api/experimental/*` 可能在版本间变化；上线前做能力探测（`/api/info` 的 `capabilities`，或按版本判断），不要硬依赖。
- **响应包裹**：`{location,data}` 与 `{data}` 不要混用；写接口多为 204，不要解析 body。
- **SSE 背压**：客户端必须尽快消费/缓冲；慢消费者会拖住共享连接（`@opencode/client` 会等待活跃订阅者接收事件）。
- **不要把 `/api/experimental/config` 当作租户配置 API**：它改的是全局配置，影响该节点所有项目。

## 10. 相关文档

- 会话与消息字段：[06-会话与消息模型.md](06-会话与消息模型.md)
- 事件类型与 payload：[附录B-事件与权限动作速查.md](附录B-事件与权限动作速查.md)
- SDK/客户端：[13-客户端-SDK-ACP.md](13-客户端-SDK-ACP.md)
- 节点集成方案：[15-控制面Agent层集成指南.md](15-控制面Agent层集成指南.md)
- 运维与排障：[16-故障排查与运维.md](16-故障排查与运维.md)
