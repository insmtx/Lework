# 附录 B 事件与权限动作速查

> 来源：`https://opencode.ai/openapi.json`（`V2Event`、`Permission.*`、`Policy.*`）与官方 `permissions`、`policies` 文档。
> 版本基准：OpenCode V2（2026-11）。事件是 **live-only SSE**；`durable` 只表示可从 session log 读取，**不代表会自动重放**。

## 1. 事件总览

`V2Event` 联合类型共有 98 个成员：

| 分类 | 数量 | 说明 |
|---|---:|---|
| 业务事件 | 96 | 本文第 2–11 节按域列出 |
| `server.connected` | 1 | SSE 连接建立后先发送，用于触发客户端重新同步；插件在 server 内订阅时收不到 |
| `V2Event.rpc` | 1 | 内部 RPC 事件，不属于常规业务订阅 |

96 个业务事件中：

| 形态 | 数量 | 含义 |
|---|---:|---|
| durable | 46 | 事件带 `durable: { aggregateID, seq, version }`，属于 durable session log |
| live-only | 50 | 只通过 SSE 推送，断线后只能靠快照/查询补齐 |

公共信封：

```json
{
  "id": "evt_...",
  "created": 1760000000000,
  "type": "session.text.ended",
  "location": { "directory": "/work/app" },
  "data": { "sessionID": "...", "assistantMessageID": "...", "ordinal": 0, "text": "..." },
  "durable": { "aggregateID": "ses_...", "seq": 42, "version": "..." },
  "metadata": {}
}
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `id` | 是 | 事件 ID |
| `created` | 视事件 | Unix 毫秒；`server.connected` 没有该字段 |
| `type` | 是 | 事件名 |
| `location` | 否 | 事件所属 location；`server.connected` 可能为 null |
| `data` | 是 | 事件载荷，形状见下表；部分资源变更事件为任意非 null 值 |
| `durable` | 否 | 仅 durable 事件有 |
| `metadata` | 否 | 附加元数据 |

**字段约定**：下表省略所有 `session.*` / `permission.*` 事件都必填的 `sessionID`；字段后标 `?` 表示可选，未标为必填。ID 类型统一按 string 处理。

## 2. 服务、安装与内部状态

| 事件 | durable | data 关键字段 |
|---|:---:|---|
| `location.shutdown` | — | 任意非 null 值；location 正在关闭 |
| `models-dev.refreshed` | — | 任意非 null 值；models.dev 目录已刷新 |
| `installation.updated` | — | `version` |
| `installation.update-available` | — | `version` |

## 3. 凭据、集成与资源变更

这些事件多数只表示“某资源变了”，payload 为空或不被 schema 约束；客户端应重新拉取对应资源，而不是从事件推断完整状态。

| 事件 | durable | data 关键字段 |
|---|:---:|---|
| `credential.updated` | — | `integrationID`、`credentialID` |
| `credential.switched` | — | `integrationID`、`credentialID`（可为 null） |
| `integration.updated` | — | 任意非 null 值 |
| `provider.updated` | — | 任意非 null 值 |
| `model.updated` | — | 任意非 null 值 |
| `agent.updated` | — | 任意非 null 值 |
| `plugin.updated` | — | 任意非 null 值 |
| `command.updated` | — | 任意非 null 值 |
| `config.updated` | — | 任意非 null 值 |
| `skill.updated` | — | 任意非 null 值 |
| `reference.updated` | — | 任意非 null 值 |
| `websearch.updated` | — | 任意非 null 值 |
| `artifact.updated` | — | 任意非 null 值 |

## 4. 项目、Worktree、文件与 MCP

| 事件 | durable | data 关键字段 |
|---|:---:|---|
| `project.updated` | — | `id`、`canonical`、`vcs?`、`name?`、`icon?`、`commands?`、`time`、`sandboxes[]` |
| `worktree.updated` | — | `projectID` |
| `worktree.resolved` | ✅ | `projectID`、`directory`、`previous`、`adopted[]?` |
| `vcs.branch.updated` | — | `branch?` |
| `filesystem.changed` | — | `file`、`event`（`add`\|`change`\|`unlink`） |
| `mcp.status.changed` | — | `server` |
| `mcp.resources.changed` | — | `server` |

## 5. 会话生命周期

| 事件 | durable | data 关键字段 |
|---|:---:|---|
| `session.created` | ✅ | `projectID`、`location`、`subpath?`、`parentID?`、`slug`、`title?`、`agent?`、`model?`、`metadata?`、`permissions?`、`version` |
| `session.agent.selected` | ✅ | `agent`、`previous?` |
| `session.model.selected` | ✅ | `model`、`previous?` |
| `session.moved` | ✅ | `location`、`projectID`、`subpath?` |
| `session.renamed` | ✅ | `title` |
| `session.metadata.updated` | ✅ | `metadata` |
| `session.permissions` | ✅ | `permissions`（完整规则集） |
| `session.viewed` | ✅ | `idle`（number） |
| `session.archived` | ✅ | 无额外字段 |
| `session.unarchived` | ✅ | 无额外字段 |
| `session.usage.updated` | — | `cost`（`{amount,currency}`）、`tokens`（input/output/reasoning/cache） |
| `session.deleted` | ✅ | 无额外字段 |
| `session.forked` | ✅ | `parentID`、`boundary`、`instructions?`、`instructionEntries?` |
| `session.idle` | — | 无额外字段；会话进入空闲 |
| `session.status` | — | `status`（`SessionStatus`：`idle` \| `retry` \| `busy`） |

> OpenAPI 里这个 schema 的名称是 `SessionStatusUpdated`，但真正发送的 `type` 是 **`session.status`**；过滤时用 `session.status`。

## 6. Inbox 与执行生命周期

| 事件 | durable | data 关键字段 |
|---|:---:|---|
| `session.inbox.delivered` | ✅ | `inboxID` |
| `session.inbox.enqueued` | ✅ | `inboxID`、`item`（完整 inbox item） |
| `session.inbox.cancelled` | ✅ | `inboxID` |
| `session.inbox.delivery.changed` | ✅ | `inboxID`、`delivery`（`steer`\|`queue`） |
| `session.execution.started` | ✅ | 无额外字段 |
| `session.execution.succeeded` | ✅ | 无额外字段 |
| `session.execution.failed` | ✅ | `error`（`Session.StructuredError`） |
| `session.execution.interrupted` | ✅ | `reason`（`user`\|`shutdown`\|`superseded`\|`inactivity`） |

## 7. 指令、技能与会话级 Shell

| 事件 | durable | data 关键字段 |
|---|:---:|---|
| `session.instructions.updated` | ✅ | `delta`（对象：路径 → hash 或 `"removed"`）、`text?` |
| `session.synthetic` | ✅ | `text`、`description?`、`metadata?` |
| `session.skill.activated` | ✅ | `id`、`name`、`text`（注入的技能正文） |
| `session.shell.started` | ✅ | `shell`（`Shell.Info`） |
| `session.shell.ended` | ✅ | `shell`、`output`（`{output,cursor,size,truncated}`） |

## 8. Step、文本与推理

| 事件 | durable | data 关键字段 |
|---|:---:|---|
| `session.step.started` | ✅ | `assistantMessageID`、`agent`、`model`、`snapshot?`、`started` |
| `session.step.streamed` | ✅ | `assistantMessageID` |
| `session.step.ended` | ✅ | `assistantMessageID`、`finish`、`rawFinish?`、`native?`、`cost`、`tokens`、`snapshot?`、`files?` |
| `session.step.failed` | ✅ | `assistantMessageID`、`error`、`finish?`、`rawFinish?`、`native?`、`cost?`、`tokens?`、`snapshot?`、`files?` |
| `session.text.started` | ✅ | `assistantMessageID`、`ordinal` |
| `session.text.delta` | — | `assistantMessageID`、`ordinal`、`delta` |
| `session.text.ended` | ✅ | `assistantMessageID`、`ordinal`、`text`、`native?` |
| `session.reasoning.started` | ✅ | `assistantMessageID`、`ordinal`、`native?` |
| `session.reasoning.delta` | — | `assistantMessageID`、`ordinal`、`delta` |
| `session.reasoning.ended` | ✅ | `assistantMessageID`、`ordinal`、`text`、`native?` |

`finish` 取值：`stop`、`length`、`tool-calls`、`content-filter`、`error`、`unknown`。
`step.failed.finish` 目前只允许 `content-filter`。

## 9. 工具调用与重试

| 事件 | durable | data 关键字段 |
|---|:---:|---|
| `session.tool.input.started` | ✅ | `assistantMessageID`、`id`、`name` |
| `session.tool.input.delta` | — | `assistantMessageID`、`id`、`delta` |
| `session.tool.input.ended` | ✅ | `assistantMessageID`、`id`、`text` |
| `session.tool.called` | ✅ | `assistantMessageID`、`id`、`input`、`executed`、`native?` |
| `session.tool.progress` | — | `assistantMessageID`、`id`、`metadata` |
| `session.tool.success` | ✅ | `assistantMessageID`、`id`、`content[]`、`metadata?`、`executed`、`native?` |
| `session.tool.failed` | ✅ | `assistantMessageID`、`id`、`error`、`content[]?`、`metadata?`、`executed`、`native?` |
| `session.retry.scheduled` | ✅ | `assistantMessageID`、`attempt`、`at`、`error` |

- 同一个工具调用的 `id` 在 `input.*`、`called`、`progress`、`success/failed` 之间关联。
- 只有 **delta / progress** 是无 durable 的增量事件；工具输入结束与结果本身会进入 durable log。
- `executed:false` 通常表示调用被拦截或未实际执行，UI 不应展示为成功。

## 10. 压缩与回滚

| 事件 | durable | data 关键字段 |
|---|:---:|---|
| `session.compaction.started` | ✅ | `reason`（`auto`\|`manual`）、`inputID?` |
| `session.compaction.delta` | — | `text` |
| `session.compaction.ended` | ✅ | `reason`、`model?`、`native?`、`text`、`cost?`、`tokens?` |
| `session.compaction.failed` | ✅ | `reason`、`error`、`inputID?`、`cost?`、`tokens?` |
| `session.revert.staged` | ✅ | `revert`（`Session.Revert`） |
| `session.revert.cleared` | ✅ | 无额外字段 |
| `session.revert.committed` | ✅ | `to`（目标消息 ID） |

## 11. 权限、PTY、Form 与 TUI

### 11.1 权限

| 事件 | durable | data 关键字段 |
|---|:---:|---|
| `permission.asked` | — | `id`、`action`、`resources[]`、`save[]?`、`metadata?`、`source?`、`message?` |
| `permission.replied` | — | `requestID`、`reply`（`once`\|`always`\|`reject`） |

- 两个事件都**没有 durable**。断线重连后必须用 `GET /api/permission/request` 补全待处理列表。
- `resources` 必须完整展示：shell 复合命令、patch 多文件都可能有多条。
- `save` 是 `always` 时可能持久化的 pattern；`reject` 会连带拒绝同会话其他待处理请求。

### 11.2 PTY 与持久终端

| 事件 | durable | data 关键字段 |
|---|:---:|---|
| `pty.created` | — | `info`（`Pty`） |
| `pty.updated` | — | `info` |
| `pty.exited` | — | `id`、`exitCode` |
| `pty.deleted` | — | `id` |
| `persistent-pty.added` | — | `terminal`（`PersistentPty.Info`） |
| `persistent-pty.updated` | — | `terminal` |
| `persistent-pty.removed` | — | `ptyID` |

### 11.3 Shell 进程与 Form

| 事件 | durable | data 关键字段 |
|---|:---:|---|
| `shell.created` | — | `info`（`Shell.Info`） |
| `shell.exited` | — | `id`、`exit?`、`status`（`running`\|`exited`\|`timeout`\|`killed`） |
| `shell.deleted` | — | `id` |
| `form.created` | — | `form`（`Form.Info`） |
| `form.replied` | — | `id`、`answer` |
| `form.cancelled` | — | `id` |

### 11.4 TUI

| 事件 | durable | data 关键字段 |
|---|:---:|---|
| `tui.prompt.append` | — | `text` |
| `tui.command.execute` | — | `command`（预定义命令枚举或任意 string） |
| `tui.toast.show` | — | `title?`、`message`、`variant`（`info`\|`success`\|`warning`\|`error`）、`duration?` |
| `tui.session.select` | — | `sessionID` |

## 12. 权限动作（`permissions`）

### 12.1 规则与默认值

```jsonc
{ "action": "shell", "resource": "git push *", "effect": "deny" }
```

| 字段 | 取值 |
|---|---|
| `action` | 工具动作字符串；插件可扩展 |
| `resource` | 路径、命令、URL、查询、技能 ID、Agent ID 等 |
| `effect` | `allow` / `ask` / `deny` |

- 匹配使用整值通配符：`*` 匹配零或多个字符（含 `/`），`?` 匹配一个字符。
- **最后匹配者生效**；Agent 规则与会话级规则追加在全局规则之后。
- 没有规则匹配时默认 `ask`；但每个 Agent 的基础策略首先包含 `*/* → allow`，所以实际默认是允许，`.env` 读取是例外。
- 多资源操作：任一 `deny` → deny；否则任一 `ask` → ask；否则 allow。

基础策略：

```jsonc
[
  { "action": "*", "resource": "*", "effect": "allow" },
  { "action": "read", "resource": "*.env", "effect": "ask" },
  { "action": "read", "resource": "*.env.*", "effect": "ask" },
  { "action": "read", "resource": "*.env.example", "effect": "allow" }
]
```

内置 Agent 追加：

| Agent | 追加策略 |
|---|---|
| `build` | 允许提问 |
| `plan` | 允许提问；除 `~/.opencode/plan` 下文件外拒绝编辑 |
| `general` | 拒绝提问与启动子 Agent |
| `explore` | 仅允许 shell/read/glob/grep/webfetch/websearch/external_directory；`.env` 读取 ask |
| `title` | 拒绝所有动作 |
| `summary` | 拒绝所有动作 |
| `compaction` | 保持基础策略 |

### 12.2 内置 action 与 resource

| action | resource | 说明 |
|---|---|---|
| `read` | Location 相对路径或规范化绝对外部路径 | 读文件 |
| `edit` | 目标路径 | `edit`/`write`/`patch` 共用 |
| `glob` | glob pattern | 文件枚举 |
| `grep` | 正则表达式 | **不是**搜索路径 |
| `shell` | scanner 产出的命令字符串 | 复合命令可能拆成多条 |
| `subagent` | 目标 Agent ID | 子 Agent 启动 |
| `skill` | 技能 ID | 技能激活 |
| `question` | `*` | 向用户提问 |
| `webfetch` | URL | 网页抓取 |
| `websearch` | 查询字符串 | 网页搜索 |
| `experimental_image_generate` | `providerID/modelID[#variant]` | 图片生成 |
| `external_directory` | 规范化的外部目录边界，通常 `/*` 结尾 | 项目/Worktree 之外的访问 |
| `<server>_<tool>` | `*` | MCP 工具；非法字符转 `_` |
| `opencode_list_mcp_resources` | MCP server 名 | 列出资源 |
| `opencode_read_mcp_resource` | `<server>:<uri>` | 读取资源 |
| `execute` | `*` | Code Mode；deny 会移除 Code Mode 及其专属工具 |

- `doom_loop`、`lsp` 不是 V2 Core 的权限 action。
- `external_directory` 默认 allow；要恢复 V1 的“项目外先询问”，显式加 `{ "action": "external_directory", "resource": "*", "effect": "ask" }`。
- 审批响应：`once`（仅本次）、`always`（保存项目级 allow pattern）、`reject`（拒绝并连带拒绝同会话其他待处理请求）。

## 13. 强制策略动作（`policies`）

| action | resource | 作用 |
|---|---|---|
| `provider.use` | Provider ID | Provider 是否可用；deny 后从目录与模型选择中消失 |
| `tool.use` | `<permission action>:<value>` | 权限检查之后硬拒绝；返回 `Blocked by configuration policy` |
| `integration.connect` | integration ID | `/connect` 可列出/连接哪些集成 |
| `integration.use` | `<integration>#<account>` 或裸 ID | 该集成提供的 provider/模型/搜索/MCP/配置 |
| `mcp.use` | MCP server 名 | MCP server 是否可用 |
| `skill.use` | 技能 ID | 技能是否可用 |
| `plugin.use` | 无版本包名或绝对路径 | 插件是否加载 |
| `websearch.use` | 搜索 Provider ID | `websearch` 可用哪些搜索源 |

规则语义：

- `effect` 只有 `allow` / `deny`，**从不提示**。
- 通配符与 `permissions` 相同；最后匹配者生效，**宽泛规则在前，例外在后**。
- 没有匹配时默认 `allow`。
- 只能收紧；`allow` 只用于解除更早的宽泛 deny。
- 校验失败的语句被丢弃并在 server 日志告警，其余语句继续生效。

```jsonc
{
  "policies": [
    { "action": "provider.use", "resource": "*", "effect": "deny" },
    { "action": "provider.use", "resource": "anthropic", "effect": "allow" },
    { "action": "tool.use", "resource": "shell:git push *", "effect": "deny" }
  ]
}
```

策略来源优先级（高 → 低）：macOS managed preferences → 系统配置 → Console workspace → 全局配置 → 直接项目配置 → `.opencode` 项目配置。与普通配置“就近覆盖”相反，越宽泛的 `policies` 越晚求值。

## 14. 控制面关注点

1. **先订阅再提交**：SSE 是 live-only。先建立 `GET /api/event`，再 `POST /api/session/{id}/prompt`，否则可能丢首事件。
2. **重连必须重新同步**：收到 `server.connected` 后，用 `GET /api/session`、`GET /api/session/{id}/message`、`GET /api/experimental/session/{id}/log` 对齐；不要依赖事件补发。
3. **`durable` ≠ 可重放**：它只说明事件在 session log 中；SSE 断线不会补发。需要历史时读 log，并处理 `log.synced` 水位。
4. **`permission.asked` / `permission.replied` 没有 durable**：断线后必须查 `GET /api/permission/request` 与 `GET /api/permission/saved`，否则审批列表会漏项。
5. **增量事件要合并**：`session.text.delta`、`reasoning.delta`、`tool.input.delta`、`tool.progress`、`compaction.delta` 都是片段；最终以 `*.ended` / `*.success` 的完整内容为准。
6. **ID 关联**：工具调用用 `sessionID + assistantMessageID + id` 关联输入、进度与结果；不要只用 `id`。
7. **过滤在服务端做**：用 `?types=` 减少事件量，但注意一个类型可能同时有 delta 与 ended，不能只订阅 delta。
8. **策略不是审批**：控制面的“管理员限制”应生成 `policies`（最好走托管配置），不要做成可被用户同意的私有开关。
9. **被 deny 的对象会消失**：MCP server/skill/plugin 被策略拒绝时可能从列表里直接移除，UI 需要给出原因而不是空白。
10. **V1 迁移**：`permission` → `permissions`，`bash` → `shell`，`task` → `subagent`，`write`/`patch` → `edit`；V2 默认允许外部目录访问。

## 15. 相关文档

- 事件流与端点：[12-HTTP-API参考.md](12-HTTP-API参考.md)
- 权限与策略详解：[09-权限与策略.md](09-权限与策略.md)
- 控制面集成方案：[15-控制面Agent层集成指南.md](15-控制面Agent层集成指南.md)
- V1 事件/字段迁移：[17-V1到V2迁移.md](17-V1到V2迁移.md)
- 配置字段速查：[附录A-配置字段速查.md](附录A-配置字段速查.md)
