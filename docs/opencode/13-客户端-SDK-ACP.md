# 13 客户端、SDK 与 ACP

> 来源：官方 `build/client`、`build/sdk`、`build/sdk/cloudflare`、`build/client/effect`、`build/sdk/effect`、`cli/acp`。
> 三种集成方式：**网络客户端**（`@opencode/client`）、**嵌入式 SDK**（`@opencode/sdk`）、**ACP 编辑器协议**（`opencode acp`）。

## 1. 选型

| 方式 | 形态 | 适用 |
|---|---|---|
| `@opencode/client` | TypeScript 客户端，走 HTTP/SSE | 控制面后端、Web/Desktop、脚本 |
| `@opencode/sdk` | 把 OpenCode server 嵌入进程，走内存 router | 自托管服务、Serverless、需要零网络跳转 |
| ACP | JSON-RPC over stdio | Zed 等编辑器、ACP 客户端 |
| `opencode run` | CLI 一次性执行 | CI、脚本、简单自动化 |

## 2. `@opencode/client`

### 2.1 安装与创建

```sh
bun add @opencode/client
```

```ts
import { OpenCode } from "@opencode/client"

const client = OpenCode.make({
  baseUrl: "http://localhost:4096",
  headers: {
    authorization: `Bearer ${process.env.OPENCODE_TOKEN}`,
  },
})

const session = await client.session.create({
  location: { directory: "/workspace" },
})

await client.session.prompt({
  sessionID: session.id,
  text: "Review the current changes",
})
```

- 方法按 API 资源分组：`client.session`、`client.event`、`client.model` 等，类型由同一份 OpenAPI 契约生成。
- `headers` 是默认 header；每个操作最后一个参数可传 `RequestOptions`（`signal`、per-request headers）。
- 可以传自定义 `fetch`。
- **事件订阅例外**：只接受订阅者本地取消，并使用 base client 的 headers。

```ts
await client.session.list(undefined, {
  signal: AbortSignal.timeout(10_000),
})
```

### 2.2 事件流

```ts
for await (const event of client.event.subscribe()) {
  console.log(event.type)
}
```

- 原生与 RPC 订阅共享每个 client 的**一条懒连接**。
- 创建 client/handle/iterable 不会开连接；开始消费才开。
- 退出迭代或 abort 只结束该 iterator；最后一个订阅者离开才关闭连接。
- 共享源会等待活跃订阅者接收事件；消费者应先缓冲再做慢工作。
- **live-only**：无重放、无自动重连；源失败会结束当前订阅，恢复后重新订阅。
- 晚到的原生订阅者只会收到当前 `server.connected`，不会收到过去的业务事件。

过滤：

```ts
for await (const event of client.event.subscribe({ types: ["config.updated"] })) {
  // event.type is "config.updated" | "server.connected"
}
```

- `types` 在客户端侧、共享连接内过滤。
- `server.connected` 总是透传，便于过滤订阅者知道何时重新同步。

### 2.3 插件 RPC

```ts
import { OpenCode } from "@opencode/client"
import { Acme } from "opencode-acme-plugin/rpc"

const acme = client.rpc(Acme)
const result = await acme.search(
  { query: "hello" },
  { location: { directory: "/workspace" }, signal: AbortSignal.timeout(10_000) },
)

const unsubscribe = acme.events.on("updated", (event) => {
  console.log(event.type, event.location.directory, event.data.text)
})
```

- 第二个参数是 `{ location, signal, headers }`，与插件输入分开。
- 省略 location 时：base location headers → server 工作目录。
- 方法参数/结果由契约推断；Promise 客户端接受 Standard Schema 或普通 JSON Schema，**不在本地运行 parser**；服务端返回已解析/转换的结果。Effect Schema 需要 Effect 客户端。
- 声明的方法错误像普通错误一样 reject；通用 HTTP RPC 错误包装会被 typed subclient 去掉；保留 `rpc.*` 框架失败。
- `events.subscribe(name)` 与原生 async iterable 一致；`events.on` 是便捷包装，返回 unsubscribe，异步回调顺序 await。
- 订阅是 **live-only**，不重放。
- 服务端插件必须已配置并实现 RPC；只 import 定义不会注册。
- 任意 HTTP 客户端可调用 `POST /api/rpc/{rpcID}/{method}`，body `{ "input": ... }`，返回 `{ "output": ... }`。

### 2.4 本地后台服务

浏览器入口不包含本地进程管理。Node 中从 `@opencode/client/service` 导入：

| 方法 | 作用 |
|---|---|
| `Service.discover()` | 返回健康的已注册 endpoint，不启动进程 |
| `Service.ensure()` | 返回兼容服务，必要时启动 |
| `Service.stop()` | 停止已注册的实例 |
| `Service.headers(endpoint)` | 生成客户端认证 header |

```ts
import { OpenCode } from "@opencode/client"
import { Service } from "@opencode/client/service"

const endpoint = await Service.ensure()
const client = OpenCode.make({
  baseUrl: endpoint.url,
  headers: Service.headers(endpoint),
})

const info = await client.server.info()
```

`ensure` 可传注册文件、版本、服务命令、`onStart`：

```ts
const endpoint = await Service.ensure({
  file: "/var/run/opencode/service.json",
  version: (version) => version.startsWith("2."),
  command: ["opencode", "serve", "--service"],
  onStart(reason, previousVersion) {
    console.log(reason, previousVersion)
  },
})
```

省略时使用标准注册路径与 `opencode serve --service`。

## 3. `@opencode/sdk`（嵌入式）

### 3.1 创建 host

```sh
bun add @opencode/sdk
```

```ts
import { OpenCode } from "@opencode/sdk"

await using opencode = await OpenCode.create()
const session = await opencode.sessions.create({
  location: { directory: "/workspace" },
})

await opencode.sessions.prompt({
  sessionID: session.id,
  text: "Review the current changes",
})
```

- `OpenCode.create()` 返回显式拥有的 host；`await using` 会释放 router、Location 服务、fibers、scoped 插件注册。
- 没有 `await using` 时显式 `await opencode.close()`。
- 路由 API 调用经过**内存 HTTP router**，不开监听端口、无网络跳转。
- 暴露完整生成客户端，并增加 `sessions`、`events` 便捷别名。
- 与 `@opencode/client` 使用相同的 Promise 值、声明错误、请求选项、`AsyncIterable` 流。

### 3.2 Worktree

```ts
const projectID = session.projectID
const worktree = await opencode.worktree.create({ projectID, name: "task" })
await opencode.worktree.refresh({ projectID })
await opencode.worktree.remove({ projectID, directory: worktree.directory, force: false })
```

- create/refresh 从项目保存的 canonical checkout 加载配置与插件。
- list 只读保存的 inventory。
- remove 激活 canonical 插件并使用记录的 strategy；strategy 不可用时失败。
- 自定义 strategy 通过插件 worktree transform 注册。

### 3.3 事件与插件

```ts
for await (const event of opencode.events.subscribe()) {
  console.log(event.type)
}
```

- 传 `AbortSignal` 或离开迭代取消响应体。
- 启动时注册插件：

```ts
import { Plugin } from "@opencode/plugin"
import { OpenCode } from "@opencode/sdk"

const plugin = Plugin.define({
  id: "customize-agent",
  async setup(ctx) {
    await ctx.agent.transform((agents) => {
      agents.update("build", (agent) => {
        agent.description = "Builds features and fixes bugs for our team"
      })
    })
  },
})

await using opencode = await OpenCode.create({ plugins: [plugin] })
```

启动后注册：`await opencode.plugin(plugin)`。

### 3.4 Cloudflare / Effect

- Cloudflare Durable Objects 有专门指南（`build/sdk/cloudflare`）；Code Mode 继续使用 `globalThis.fetch`。
- Effect 客户端（`build/client/effect`、`build/sdk/effect`）用于需要 Effect Schema 的场景；Promise 客户端不运行本地 schema parser。

## 4. ACP（Agent Client Protocol）

### 4.1 启动

```json title="~/.config/zed/settings.json"
{
  "agent_servers": {
    "OpenCode": {
      "command": "opencode",
      "args": ["acp"]
    }
  }
}
```

- `opencode acp` 在 stdin/stdout 上收发换行分隔的 JSON-RPC，ACP 协议版本 1。
- 它启动一个**私有 OpenCode server** 供自己使用，不连共享后台服务，也不开网络端口。
- 一个进程可服务多个 ACP 会话；stdin 关闭时退出。
- 排障：`opencode --print-logs acp`，日志写 stderr，stdout 只走 ACP 消息。

### 4.2 认证

- 使用已有 provider 凭据；先 `opencode auth login`。
- 只提供一个 auth method：`opencode-login`。
- 默认是 agent auth method；`authenticate` 只接受它，不收集凭据。
- provider 需要认证时返回 ACP `auth_required`。
- 客户端 `clientCapabilities.auth.terminal = true` 时，它是 terminal auth method：

```json
{
  "id": "opencode-login",
  "name": "Login with opencode",
  "description": "Run `opencode auth login` in the terminal",
  "type": "terminal",
  "args": ["--login"]
}
```

客户端应把 `--login` 加到启动命令（`opencode acp --login`）并在终端运行，**不要**发 `authenticate`；退出码 0 表示成功，然后重连并重新 `initialize`。

- `clientCapabilities._meta["terminal-auth"] = true` 时，auth method 带：

```json
"_meta": { "terminal-auth": { "command": "opencode", "args": ["auth", "login"], "label": "OpenCode Login" } }
```

### 4.3 能力

`initialize` 响应的 agent capabilities：

```json
{
  "loadSession": true,
  "mcpCapabilities": { "http": true, "sse": false },
  "promptCapabilities": { "embeddedContext": true, "image": true },
  "sessionCapabilities": {
    "additionalDirectories": {},
    "close": {},
    "delete": {},
    "fork": {},
    "list": {},
    "resume": {}
  }
}
```

读取的 client capabilities：

| 能力 | 效果 |
|---|---|
| `auth.terminal = true` | auth method 变 terminal |
| `elicitation.form` | 用表单提问 |
| `session.compaction` | 用 ACP compaction 更新 |
| `_meta["terminal-auth"] = true` | auth method 带登录命令 |

### 4.4 会话

- 客户端创建会话时设置 `cwd`；必须是绝对路径，否则 invalid params。
- 使用该目录的配置：模型、Agent、命令、技能、指令、插件、MCP。
- `session/load` 会重放保存的消息；`session/resume` 与 `session/fork` **不重放**。
- load/resume/fork 必须传该会话的目录作为 `cwd`，不同目录会报错。
- `session/list` 每页最多 100，支持 `cwd` 过滤与 `cursor`。
- `session/close` 中断活动工作并 detach，保存的会话仍在。
- `session/delete` 像 `session/cancel` 一样结束活动 prompt，然后删除；删除不存在的会话也成功。
- 一个会话同一时间只跑一个 prompt；第二个 `session/prompt` 会报错。

### 4.5 取消

- `session/cancel` 以 `stopReason: "cancelled"` 结束活动 prompt。
- prompt 请求的 `$/cancel_request` 效果相同。
- 待处理权限请求会被拒绝；取消后未完成的工具调用报告为 `failed`。

### 4.6 额外目录

`session/new`、`session/load`、`session/resume`、`session/fork` 可传：

```json
{ "cwd": "/work/app", "additionalDirectories": ["/work/shared"], "mcpServers": [] }
```

- 每个条目必须是绝对路径，且不能含 `*`/`?`。
- 允许在这些根内访问外部目录；read/edit/shell 权限仍生效。
- 每次请求的列表**替换**之前列表（按 ACP additional directories RFD）。
- 授权存储在会话上，其他客户端使用同一会话时也生效；子会话复制这些授权。
- `session/list` 返回每个会话保存的列表。

### 4.7 配置选项

新会话使用目录默认模型与 primary Agent。`session/new/load/resume/fork` 响应包含：

| ID | Category | 值 |
|---|---|---|
| `model` | `model` | 启用的模型 `provider/model`；`/variant` 后缀也设置 effort |
| `effort` | `thought_level` | 模型 variants 与 `default`；仅有 variants 的模型才出现 |
| `mode` | `mode` | 可见的 primary Agent |

`session/set_config_option` 只接受字符串：

```json
{ "sessionId": "ses_123", "configId": "model", "value": "anthropic/claude-sonnet-5-5" }
```

`session/set_mode` 也设置 Agent。模型/Agent 变化时发 `config_option_update`；其他客户端改同一会话也发。

### 4.8 Prompt 内容

支持 text、image、embedded resource、resource link；忽略 audio。

| block | 行为 |
|---|---|
| `text` | 发给模型 |
| 仅 `audience: assistant` 的 text | 作为独立上下文发给模型，不在用户消息里 |
| 仅 `audience: user` 的 text | 不进入模型输入 |
| `image` 带 `data` | 作为附件 |
| `image` 只有 `uri` | 按 resource link 处理 |
| `resource` 带 `text` | 在路径/URI 后内联文本 |
| `resource` 带 `blob` | 有 `mimeType` 则附件，否则丢弃 |
| `resource_link` 到 `file://` | 附件；缺失/不可读时发 Markdown 链接 |
| `resource_link` 到 `zed://` 且带 `path` query | 附加 `path` 指向文件 |
| 其他 `resource_link` | 发 Markdown 链接 |

`audience` 来自 `annotations`。既无 `data` 又无 `uri` 的 image block 会被拒绝。

### 4.9 命令

`session/new/load/resume/fork` 后发送 `available_commands_update`，包含目录命令与内置 `compact`；命令变化时再发。

```json
{
  "sessionUpdate": "available_commands_update",
  "availableCommands": [{ "name": "compact", "description": "Compact the session" }]
}
```

prompt 以 `/` + 可用命令名开头时执行该命令，其余文本作为参数；其他 `/` 开头文本按普通 prompt 发送。`/compact` 触发压缩。

### 4.10 MCP

客户端可在 `session/new/load/resume/fork` 传 stdio/HTTP MCP server：

```json
{ "type": "http", "name": "docs", "url": "https://mcp.example.com", "headers": [] }
```

- 加入到该目录，生命周期为 `opencode acp` 进程。
- 透传 HTTP headers，**不使用 OAuth**。
- 不广告 MCP over SSE，不支持 MCP over ACP。
- 与已配置 server 同名时，客户端 server 替换该目录中的配置。
- `policies` 的 `integration.use` 仍生效。

### 4.11 权限

三个选项：

```json
[
  { "optionId": "once", "kind": "allow_once", "name": "Allow once" },
  { "optionId": "always", "kind": "allow_always", "name": "Always allow" },
  { "optionId": "reject", "kind": "reject_once", "name": "Reject" }
]
```

请求会改文件时，OpenCode 尽可能附带 `diff` 预览。

### 4.12 提问

客户端声明 `elicitation.form` 时，用 `elicitation/create` 的 form 模式；若该 tool call 已作为本会话的 `session/update` 发出，请求带 `toolCallId`。

```json
{
  "mode": "form",
  "sessionId": "ses_123",
  "toolCallId": "call_1",
  "message": "Which package manager?",
  "requestedSchema": {
    "type": "object",
    "properties": { "pm": { "type": "string", "oneOf": [{ "const": "bun", "title": "Bun" }] } },
    "required": ["pm"]
  }
}
```

- 永不发送可能收集 secret 的表单。
- 字段允许“选项 + 自定义输入”时，schema 增加可选 `<key>_custom` 字符串；单选时非空自定义值替换选项，多选时追加。
- 用户拒绝/取消表单 → 取消问题。
- 无法显示问题时取消问题，并告诉模型继续。

### 4.13 压缩

- 客户端声明 `session.compaction` 时，用 ACP 的 `compaction_update` 与 `compaction_summary_chunk`（Preview RFD）。
- 加载会话时，每个完成/失败的压缩重放为一条 update。

```json
{
  "sessionUpdate": "compaction_update",
  "compactionId": "msg_123",
  "status": "completed",
  "summary": [{ "type": "text", "text": "..." }]
}
```

- 其他客户端收到 **compaction marker**（见 4.15）；子会话压缩总是用 marker。

### 4.14 Usage

prompt 响应的 `usage` 来自 end-turn usage RFD（draft），汇总该 prompt 在本会话的模型调用，不含子会话：

```json
{ "stopReason": "end_turn", "usage": { "inputTokens": 1200, "outputTokens": 340, "totalTokens": 1540 }, "_meta": {} }
```

响应前发送 `usage_update`（仅当用了 token 且知道模型上下文大小）：

```json
{ "sessionUpdate": "usage_update", "used": 1540, "size": 200000, "cost": { "amount": 0.012, "currency": "USD" } }
```

`cost` 是会话总成本（USD）。

### 4.15 子会话

prompt 运行期间，子会话更新作为 prompt 会话的 `session/update` 发送，带 `_meta["opencode/child-session"]`：

```json
{
  "sessionUpdate": "tool_call",
  "toolCallId": "ses_child:call_1",
  "title": "Explore the codebase: src/app.ts",
  "_meta": {
    "opencode/child-session": { "id": "ses_child", "parentID": "ses_123", "depth": 1, "title": "Explore the codebase" }
  }
}
```

- tool call ID 前缀 `<childSessionId>:`；title 前缀子会话标题（若有）。
- `depth`：prompt 会话的子会话为 1，孙会话为 2，依此类推。
- 子会话的权限请求在 `toolCall._meta` 带同样 key；prompt 结束后不再发送子会话更新，但权限请求/问题仍会到达。
- 字段：`id`、`parentID`、`depth`、可选 `title`。

### 4.16 重试状态

模型调用失败并安排重试时，发 `session_info_update` + `_meta["opencode/retry"]`；重试开始时发 `"opencode/retry": null`。prompt 响应到达时清除。

```json
{
  "sessionUpdate": "session_info_update",
  "_meta": {
    "opencode/retry": {
      "attempt": 1,
      "nextRetryAt": "2026-10-02T12:00:05.000Z",
      "error": { "type": "provider.rate-limit", "message": "Rate limit exceeded", "status": 429 }
    }
  }
}
```

| 字段 | 说明 |
|---|---|
| `attempt` | 重试次数 |
| `nextRetryAt` | 下次尝试时间（ISO 8601） |
| `error` | `type`、`message`；provider 提供时还有 `status`、`response.body` |

### 4.17 压缩标记

客户端未声明 `session.compaction` 时，压缩进度用 `session_info_update` + `_meta["opencode/compaction"]`；子会话总用 marker。

```json
{
  "sessionUpdate": "session_info_update",
  "_meta": { "opencode/compaction": { "status": "started", "messageId": "msg_123", "reason": "auto" } }
}
```

| 字段 | 说明 |
|---|---|
| `status` | `started` / `completed` / `failed`；取消算 `failed` |
| `messageId` | 配对开始/结束，视为不透明键 |
| `reason` | `auto` / `manual` |
| `error` | `failed` 时存在，形状同 retry |

marker **不包含摘要**。自动压缩可能没有 `started` 就失败。加载会话时，每个完成/失败的压缩重放为一条 marker。

## 5. 控制面关注点

- **控制面应优先用 `@opencode/client` 的契约类型**（如果采用 TS），或按 OpenAPI 生成 Go 类型；不要手写字段。
- **共享事件连接**：一个 client 一条连接，所有订阅者共享；控制面多面板应复用同一 client，而不是每个面板各开 SSE。
- **慢消费者会阻塞共享源**：前端消费要缓冲/批处理，避免长任务阻塞事件。
- **`Service.ensure()` 适合节点 agent**：它能在节点上发现/启动共享服务并生成认证 header；控制面不应直接调用它。
- **ACP 是编辑器通道，不是控制面 API**：控制面用 HTTP/SSE；ACP 用于 Zed 等客户端。若控制面想复用 ACP，需要自己实现 JSON-RPC 客户端并处理权限/表单/子会话 `_meta`。
- **ACP 的 `additionalDirectories` 是会话级授权**：与 `external_directory` 权限叠加；不要把它当作绕过权限的手段。
- **子会话 `_meta` 是展示关键**：HTTP 事件流通过 `parentID` 关联，ACP 通过 `_meta["opencode/child-session"]`；两者都要能构建会话树。
- **压缩/重试标记**：控制面 UI 应区分 `compaction_update`（有摘要）与 marker（无摘要），以及 `retry` 状态。

## 6. 相关文档

- HTTP 端点与 SSE：[12-HTTP-API参考.md](12-HTTP-API参考.md)
- 会话/消息字段：[06-会话与消息模型.md](06-会话与消息模型.md)
- 插件开发（RPC/transforms）：[14-插件开发.md](14-插件开发.md)
- 节点集成：[15-控制面Agent层集成指南.md](15-控制面Agent层集成指南.md)
