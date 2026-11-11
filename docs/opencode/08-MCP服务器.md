# 08 MCP 服务器

> 来源：官方 `mcp-servers`、`policies`、`tools`、`cli/commands`、`openapi.json`（`/api/mcp*`）。
> MCP 规范本身以 `https://modelcontextprotocol.io/` 为准；本文只描述 OpenCode 的接入方式与行为。

OpenCode 通过 MCP（Model Context Protocol）把外部工具、prompt、resource、instructions 接入 Agent。MCP 工具会占用模型上下文，因此应只添加需要的 server。

## 1. 配置结构

V2 的 server 统一放在 `mcp.servers` 下，**不再把 server 名直接放在 `mcp` 下**：

```jsonc title="opencode.jsonc"
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "timeout": {
      "startup": 45000,
      "catalog": 30000,
      "execution": 600000
    },
    "servers": {
      "my-server": {
        "type": "local",
        "command": ["npx", "-y", "example-mcp-server"]
      }
    }
  }
}
```

- 同名 server 在更高优先级配置中会**整体替换**，不会字段级合并；覆盖时必须重复所有必填字段。
- 用 `disabled: true` 保留配置但不连接；不存在 `enabled` 字段。
- 配置中的字符串支持 `{env:NAME}` 变量替换；`$NAME` 不会在 JSON 字符串里展开。

## 2. Local server（stdio）

```jsonc
{
  "mcp": {
    "servers": {
      "everything": {
        "type": "local",
        "command": ["npx", "-y", "@modelcontextprotocol/server-everything"],
        "cwd": ".",
        "environment": {
          "LOG_LEVEL": "info",
          "MCP_API_KEY": "{env:MCP_API_KEY}"
        },
        "disabled": false,
        "codemode": true,
        "timeout": { "catalog": 60000 },
        "protocol": "legacy",
        "pool": false
      }
    }
  }
}
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `type` | 是 | 必须是 `"local"` |
| `command` | 是 | 可执行文件 + 参数数组 |
| `cwd` | 否 | 工作目录；相对路径按 workspace 解析，默认 workspace |
| `environment` | 否 | 追加到继承环境之上的字符串变量 |
| `disabled` | 否 | `true` 时不连接；默认 `false` |
| `codemode` | 否 | `false` 时工具直接暴露，不经 Code Mode；默认 `true` |
| `timeout` | 否 | 覆盖全局超时 |
| `protocol` | 否 | `legacy`（默认）/`auto`/`2026-07-28` |
| `pool` | 否 | 跨目录共享连接；默认 `false` |

CLI 添加：

```sh
opencode mcp add everything -- npx -y @modelcontextprotocol/server-everything
opencode mcp add everything --env LOG_LEVEL=info -- npx -y @modelcontextprotocol/server-everything
```

`--` 之后是命令；`--env` 可重复。

## 3. Remote server（Streamable HTTP）

```jsonc
{
  "mcp": {
    "servers": {
      "context7": {
        "type": "remote",
        "url": "https://mcp.context7.com/mcp",
        "oauth": false,
        "headers": {
          "CONTEXT7_API_KEY": "{env:CONTEXT7_API_KEY}"
        }
      }
    }
  }
}
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `type` | 是 | 必须是 `"remote"` |
| `url` | 是 | 绝对 Streamable HTTP endpoint |
| `headers` | 否 | 发送到 endpoint 的字符串 header |
| `oauth` | 否 | OAuth 配置对象，或 `false` 关闭 OAuth |
| `disabled` / `codemode` / `timeout` / `protocol` / `pool` | 否 | 同 local |

`oauth: false` 只应在 server 完全使用 API key/header 凭据时使用。

CLI 添加：

```sh
opencode mcp add context7 --url https://mcp.context7.com/mcp
opencode mcp add context7 --url https://mcp.context7.com/mcp --header 'CONTEXT7_API_KEY={env:CONTEXT7_API_KEY}'
```

`--header` 可重复；引用 `{env:NAME}` 时要加引号，避免 shell 展开。

## 4. OAuth

远程 server 默认启用 OAuth（除非 `oauth: false`）。OpenCode 会：

1. 发现 authorization server；
2. 用 PKCE 登录；
3. 刷新 token；
4. 把凭据存储在项目配置之外。

交互入口：

```sh
opencode mcp auth sentry      # 启动登录
opencode mcp logout sentry    # 清除凭据/切换账号
opencode mcp auth             # 从支持 OAuth 的 server 中选择
```

在 TUI/Desktop/Web 中：`/mcps` → 选择 server → 登录。

### 4.1 远程机器/SSH/容器

浏览器登录后无法回调到远端 OpenCode 时，复制地址栏中的回调 URL，粘贴回登录提示：

```text
http://127.0.0.1:52817/callback?code=...&state=...
```

该提示会出现在 `/mcps`、`opencode mcp auth` 以及 Desktop/Web 的登录对话框中。

### 4.2 客户端注册方式

OpenCode 按以下顺序选择：

| 方式 | 条件 |
|---|---|
| 预注册 client | 配置了 `oauth.client_id` |
| CIMD（Client ID Metadata Document） | authorization server 支持 CIMD 与 public client，且未设置 `oauth.redirect_uri` |
| DCR（Dynamic Client Registration） | 以上都不适用；OpenCode 注册并复用 client |

CIMD 下 OpenCode 使用的 metadata 文档：

```text
https://opencode.ai/oauth/opencode/client.json
```

允许的 loopback redirect（任意端口）：

```text
http://127.0.0.1/callback
http://localhost/callback
http://127.0.0.1/mcp/oauth/callback    # 旧客户端
```

设置自定义 `redirect_uri` 会禁用 CIMD，回退到 DCR。

### 4.3 预注册 client

V2 使用 snake_case：

```jsonc
{
  "type": "remote",
  "url": "https://mcp.example.com/mcp",
  "oauth": {
    "client_id": "{env:MCP_CLIENT_ID}",
    "client_secret": "{env:MCP_CLIENT_SECRET}",
    "scope": "tools:read tools:execute",
    "callback_port": 19876,
    "redirect_uri": "http://127.0.0.1:19876/callback",
    "auth_server_metadata_url": "https://auth.example.com/.well-known/oauth-authorization-server"
  }
}
```

| 字段 | 说明 |
|---|---|
| `client_id` | 预注册 client ID；优先于 CIMD/DCR |
| `client_secret` | confidential client 的密钥，放在 token 请求体中；public client 省略 |
| `scope` | 空格分隔的 scope；缺省时使用 server 在未认证请求中声明的 scope |
| `callback_port` | 1–65535；缺省用 `redirect_uri` 端口，再退到临时端口 |
| `redirect_uri` | 预注册 loopback URI；设置后禁用 CIMD |
| `auth_server_metadata_url` | server 未发布 protected resource metadata 时，显式指定授权服务器 metadata |

## 5. 超时

```jsonc
{
  "mcp": {
    "timeout": { "startup": 30000, "catalog": 30000, "execution": 43200000 },
    "servers": {
      "slow-tools": { "type": "remote", "url": "...", "timeout": { "catalog": 60000 } }
    }
  }
}
```

| 超时 | 默认 | 作用 |
|---|---|---|
| `startup` | 30 秒 | 传输连接 + server 初始化 |
| `catalog` | 30 秒 | 列出 tools / prompts / resources / templates |
| `execution` | 12 小时 | 工具调用、prompt 获取、resource 读取 |

时间是正整数毫秒；server 的 `timeout` 覆盖全局同名项。

## 6. 协议版本

默认使用经典 MCP `initialize` 握手，最高支持 2025-11-25。

```jsonc
{ "protocol": "auto" }
```

| 值 | 行为 |
|---|---|
| `legacy` | 默认；发 `initialize`，支持到 2025-11-25 |
| `auto` | 用 `server/discover` 探测 2026-07-28；不支持则回退 legacy |
| `2026-07-28` | 强制新版本；旧 server 会连接失败 |

对 local legacy server 探测会短暂多起一个进程，server 忽略未知请求时可能等到 startup 超时，因此 `auto` 只用于确实需要的 server。

## 7. 共享连接（pool）

一个 OpenCode server 可以打开多个目录，每个目录默认各连一份 MCP。`pool: true` 让相同配置的目录共享一条连接与一份工具列表。

```jsonc
{
  "mcp": {
    "servers": {
      "cloudflare": {
        "type": "remote",
        "url": "https://mcp.cloudflare.com/mcp",
        "pool": true
      }
    }
  }
}
```

规则：

- 只有 2026-07-28 revision 的连接会共享；设置 `pool` 后未指定 `protocol` 视为 `auto`。旧 revision 或 `legacy` 仍按目录各连一份。
- 共享条件：URL、headers、OAuth 设置与登录账号（remote）；command、cwd、environment（local）；timeout 与 protocol 也必须一致。
- server 名不必相同，但 OAuth 登录按名字存储，因此 OAuth server 只有同名才共享。
- local server 只在同一台机器、非 workspace 的目录间共享；跨项目共享需要固定 `cwd`（如 `"~"`），否则每个项目各起一份并记录警告。
- 共享连接没有 roots；工具要求输入时，请求出现在调用它的目录。
- 最后一个使用目录关闭或断开时，连接关闭。

## 8. 命名规则

工具名：

```text
<server>_<tool>
```

非字母、数字、`_`、`-` 的字符替换为 `_`：

```text
server: context 7
tool:   resolve.library/id
name:   context_7_resolve_library_id
```

MCP prompt 变成命令 `<server>:<prompt>`，同样规范化：

```text
/context_7:find_docs react "server components"
```

- server 名应短且规范化后唯一。
- Code Mode 下工具按规范化 server 名分组：`tools.context_7.resolve_library_id(...)`。
- 权限 action 使用规范化 `<server>_<tool>`。

## 9. 权限与策略

### 9.1 工具级

```jsonc
{
  "permissions": [
    { "action": "context7_*", "resource": "*", "effect": "deny" }
  ]
}
```

- 可以隐藏/拒绝工具而不断开 server。
- `codemode: false` 只影响暴露方式，不改变权限 action。
- 拒绝 `execute` 会移除 Code Mode 及其专属工具，包括默认走 Code Mode 的 MCP 工具。

### 9.2 MCP server 级

`policies` 的 `mcp.use` 按 server 名控制是否可用：

```jsonc
{
  "policies": [
    { "action": "mcp.use", "resource": "*", "effect": "deny" },
    { "action": "mcp.use", "resource": "github", "effect": "allow" }
  ]
}
```

被拒绝的 MCP server 会被移除，无论它来自配置、插件还是 Console。

### 9.3 集成级

通过 Console workspace 或 well-known origin 下发的 MCP server，受 `integration.use` 约束；签入 MCP server 本身不受 `integration.connect` 影响，用 `mcp.use` 控制。

## 10. 资源（resources）

Agent 通过两个内置工具读取：

| 工具 | 权限 action | resource |
|---|---|---|
| `list_mcp_resources` | `opencode_list_mcp_resources` | server 名 |
| `read_mcp_resource` | `opencode_read_mcp_resource` | `<server>:<uri>` |

```jsonc
{
  "permissions": [
    { "action": "opencode_read_mcp_resource", "resource": "docs:*", "effect": "allow" }
  ]
}
```

API：

```http
GET /api/mcp/resource
```

`GET /api/mcp` 返回 server 列表与连接状态。

## 11. Server instructions

server 在初始化时返回 instructions，OpenCode 会加入系统提示：

```text
<mcp_instructions>
  <server name="sentry">
    Search for an existing issue before creating a new one.
  </server>
</mcp_instructions>
```

- 使用 Code Mode 的 server 会额外加入一行，说明工具在哪里。
- 如果 Agent 权限拒绝了该 server 的所有工具，或拒绝了其 Code Mode 的 `execute`，instructions 会被省略。

## 12. 输入请求（elicitation）

server 可以在调用中要求输入。OpenCode 显示为表单，或对 URL 请求显示链接：

```text
sentry is requesting input
```

关闭表单即取消请求。HTTP API 侧对应 `Session.Form`；ACP 侧对应 `elicitation/create`（客户端需声明 `elicitation.form`）。

## 13. 会话关联 `_meta`

为会话发起的调用，OpenCode 会在 `CallToolRequest.params._meta` 中带上：

```json
{
  "method": "tools/call",
  "params": {
    "name": "lookup",
    "arguments": { "query": "example" },
    "_meta": { "ai.opencode/sessionID": "ses_..." }
  }
}
```

| 规则 | 说明 |
|---|---|
| 身份 | 标识发起调用的 OpenCode 会话，不是 MCP 传输会话 |
| 存在性 | 无会话上下文的调用可能没有 |
| 安全 | 不可单独用作认证/授权依据 |
| 隐私 | 远程 server 会收到原始 ID，可能记录 |

该字段不会出现在模型可见的 schema 中。

## 14. 管理

### 14.1 CLI / TUI

```sh
opencode mcp list
opencode mcp add <name> ...
opencode mcp auth [name]
opencode mcp logout <name>
```

TUI 中用 `/mcps` 查看、连接、断开、认证。**断开不会改配置**；要持久禁用，编辑 `disabled: true`。

### 14.2 HTTP API

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/mcp` | 列出 server 与状态 |
| `PUT` | `/api/experimental/mcp/{server}` | 添加/替换 server 配置 |
| `DELETE` | `/api/experimental/mcp/{server}` | 移除 |
| `POST` | `/api/experimental/mcp/{server}/connect` | 连接 |
| `POST` | `/api/experimental/mcp/{server}/disconnect` | 断开 |
| `GET` | `/api/mcp/resource` | 列出资源 |
| `POST` | `/api/experimental/client-mcp/token` | 创建 client MCP WebSocket token |
| `GET` | `/api/experimental/client-mcp` | 附加 client MCP server（WebSocket） |

`client-mcp` 让客户端把自己实现的 MCP server 挂到 OpenCode server 上，适合 Desktop/插件场景；token 有权限限制（403 可能表示不允许）。

### 14.3 状态事件

`mcp.status.changed`（payload `{ server }`）在连接状态变化时发出；控制面可据此显示“连接中/已连接/需要认证/失败”。

## 15. 控制面关注点

- **节点侧托管 MCP**：MCP server 的进程/连接属于 OpenCode 节点，不属于控制面。控制面只读取 `/api/mcp` 状态并转发操作。
- **OAuth 是交互式的**：远程机器需要把回调 URL 传回。控制面若要支持“在浏览器完成授权”，需要把回调 URL 通过节点隧道送回并提交给 `opencode mcp auth` 的提示（或使用 client-mcp 方案）。
- **不要把 MCP token/header 写进日志或事件**。`headers` 里常含 API key；控制面展示配置时应脱敏。
- **`pool` 会改变隔离性**：共享连接意味着一个目录的工具列表/连接状态会影响其他目录；控制面展示时要标注“共享连接”。
- **超时分级**：`startup`/`catalog` 短、`execution` 长。控制面的“连接超时”与“工具执行超时”应分别提示。
- **`codemode: false` 会放大工具列表**：context 占用更高，但工具调用更直接。控制面可把它作为“调试/兼容”开关暴露。
- **策略优先级**：`mcp.use` deny 会直接移除 server，且受托管/Console 策略约束；控制面不要提供绕过入口。

## 16. 相关文档

- 工具与 Code Mode：[07-工具系统.md](07-工具系统.md)
- 权限/策略优先级：[09-权限与策略.md](09-权限与策略.md)
- 插件管理 MCP transform：[14-插件开发.md](14-插件开发.md)
- 集成与凭据：[12-HTTP-API参考.md](12-HTTP-API参考.md)
