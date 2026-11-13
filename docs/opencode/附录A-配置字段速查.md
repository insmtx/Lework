# 附录 A 配置字段速查

> 来源：`https://opencode.ai/config.json`（V2 schema，共 49 个顶层属性，含 `$schema`）与官方 `config`、`migrate-v1` 文档。
> 版本基准：OpenCode V2（2026-11）。schema 是可用字段与嵌套形状的权威来源；本表只做速查，字段语义与示例见 [03-配置体系.md](03-配置体系.md)。

## 1. 阅读约定

| 标记 | 含义 |
|---|---|
| ✅ | 当前 V2 推荐字段 |
| ⚠️ | schema 标记 `deprecated`；仍可加载，但应迁移 |
| `—` | schema 未声明 `default`，或默认值取决于运行环境 |

- 所有字段都可出现在全局、项目、系统或托管配置中；**`update` 只读取全局值**。
- 合并顺序：用户全局 → 项目直接配置（远到近）→ `.opencode` 配置（远到近）→ 系统配置 → macOS managed preferences。后加载者覆盖先加载者；`policies` 顺序相反，见第 5 节。
- 集合类字段（`agents`、`skills`、`plugins`、`providers`、`mcp.servers` 等）多数是**合并**，不是整体替换，具体见各专题文档。

## 2. 顶层字段总表（49 项）

### 2.1 运行与交互

| 字段 | 类型 | 默认 | 说明 | 状态 |
|---|---|---|---|---|
| `$schema` | string | — | JSON Schema 地址，仅用于编辑器校验 | ✅ |
| `shell` | string | 平台默认 shell | 终端与 `shell` 工具使用的 shell | ✅ |
| `model` | string \| `{providerID, model, variant?}` | 目录/会话上次选择 | 默认模型；根级字符串不保留 `#variant` | ✅ |
| `default_agent` | string | `build` | 会话未指定时的主 Agent；不存在时回退 `build`，再回退首个可见主 Agent | ✅ |
| `update` | `disable`\|`notify`\|`auto` | `notify` | 更新检查/安装；项目级值被忽略；自动安装不重启运行中的 server | ✅ |
| `share` | `manual`\|`auto`\|`disabled` | —（schema 无 default） | 会话是否可手动/自动分享或禁止分享 | ✅ |
| `enterprise` | `{ url?: string }` | — | 企业分享服务地址 | ✅ |
| `username` | string | — | 接受但不展示在会话中，用于遥测身份 | ✅ |
| `timezone` | string（IANA） | server 本地时区 | 定时任务使用的时区，如 `America/Los_Angeles` | ✅ |
| `logLevel` | LogLevel | — | V1 日志级别；V2 忽略并告警，改用 `OPENCODE_LOG_LEVEL` | ⚠️ |
| `server` | ServerConfig | — | V1 server 配置；V2 用 service 配置与显式 server 选项 | ⚠️ |
| `layout` | LayoutConfig | — | V1 布局；V2 固定 stretch | ⚠️ |

### 2.2 权限与 Agent

| 字段 | 类型 | 默认 | 说明 | 状态 |
|---|---|---|---|---|
| `permissions` | `Permission.Rule[]`（`{action,resource,effect}`） | 基础策略 `*/* allow`，`.env` 读需 ask | 每 Agent 的有序可提示规则；最后匹配生效 | ✅ |
| `policies` | `{action,resource,effect}[]`（effect 仅 allow/deny） | 无匹配即 allow | 不可提示的强制约束；只能收紧 | ✅ |
| `agents` | `map<string, Agent>` | 内置 Agent | 覆盖内置 Agent 或定义自定义 Agent | ✅ |
| `permission` | PermissionConfig | — | V1 按工具分组的权限；迁移为 `permissions` | ⚠️ |
| `tools` | `map<string, boolean>` | — | V1 工具开关；迁移为 `permissions` 规则 | ⚠️ |
| `agent` | `map<string, AgentConfig>` | — | V1 单数 Agent 映射；迁移为 `agents` | ⚠️ |
| `mode` | `map<string, AgentConfig>` | — | V1 模式映射；迁移为 `agents`（条目成为 primary） | ⚠️ |

### 2.3 文件、快照与输出

| 字段 | 类型 | 默认 | 说明 | 状态 |
|---|---|---|---|---|
| `snapshots` | boolean | `true` | 是否抓取 undo/revert 用的文件快照；依赖 Git | ✅ |
| `watcher` | `{ ignore?: string[] }` | — | 不触发 `filesystem.changed` 的路径 | ✅ |
| `formatter` | boolean \| `map<string, Entry>` | `false` | 写文件后运行内建/自定义格式化器 | ✅ |
| `lsp` | boolean \| `map<string, Server>` | — | V2 接受并保留配置，但**不运行 LSP**、不暴露 LSP 工具/诊断 | ✅ |
| `media` | `{ image?: {...} }` | 见 2.4 | prompt 附件与图片读取的缩放/拒绝策略 | ✅ |
| `tool_output` | `{ max_lines?, max_bytes? }` | `2000` 行 / `51200` 字节 | 工具输出截断阈值；超出部分可能落盘 | ✅ |
| `attachment` | AttachmentConfig | — | V1 附件处理；迁移为 `media` | ⚠️ |
| `snapshot` | boolean | `true` | V1 单数快照开关；迁移为 `snapshots` | ⚠️ |

### 2.4 `media` / `tool_output` / `compaction` 默认值

| 字段 | 默认 | 约束 |
|---|---:|---|
| `media.image.auto_resize` | `true` | `false` 时超限直接失败 |
| `media.image.max_width` | `2000` | 正整数 |
| `media.image.max_height` | `2000` | 正整数 |
| `media.image.max_base64_bytes` | `5242880`（5 MiB） | 仅 base64 编码后的字节，不含 `data:` 前缀 |
| `tool_output.max_lines` | `2000` | 正整数 |
| `tool_output.max_bytes` | `51200`（50 KiB） | 正整数 |
| `compaction.auto` | `true` | 上下文接近上限时自动压缩；Provider 报“太长”时恢复一次 |
| `compaction.keep.tokens` | `15000` | 摘要旁保留的近期 token |
| `compaction.buffer` | `"15%"`（至少 16k） | `"1%"`–`"99%"` 或 token 数；≥ 窗口时回退默认 |

> `compaction.tail_turns`、`compaction.prune`、`compaction.preserve_recent_tokens`、`compaction.reserved` 均为 V1 遗留字段；V2 使用 `keep.tokens` 与 checkpoint 式压缩。

### 2.5 MCP、技能、命令与上下文

| 字段 | 类型 | 默认 | 说明 | 状态 |
|---|---|---|---|---|
| `mcp` | `{ timeout?, servers? }` + 旧版直挂项 | 超时 30s / 30s / 12h | V2 服务器放 `mcp.servers`；直挂项是 V1 兼容写法 | ✅（旧写法 ⚠️） |
| `compaction` | `{ auto?, keep?, buffer? }` | `auto:true`、`keep.tokens:15000`、`buffer:"15%"` | 会话压缩行为；V1 `prune`/`tail_turns` 已弃用 | ✅ |
| `skills` | string[] \| `{paths,urls}` | 自动发现 `.opencode/skill/` 与 `.opencode/skills/` | 额外技能目录或 well-known HTTP catalog；数组形式合并 | ✅（对象形式 ⚠️） |
| `commands` | `map<string, Command>` | 自动发现命令文件 | 斜杠命令；JSON 形式必须有 `template` | ✅ |
| `instructions` | string[] | `AGENTS.md` | 额外指令路径、glob 或 URL（HTTP 5 秒超时） | ✅ |
| `references` | `map<string, string \| Git \| Local>` | — | 命名的本地目录或 Git 仓库，作为外部上下文 | ✅ |
| `worktree` | `{ directory?: string }` | data 目录 `worktree/<项目 ID 前 6 位>` | 本地 worktree 的父目录 | ✅ |
| `plugins` | `(string \| {package,options?})[]` | 自动发现 `.opencode/plugin/` 与 `.opencode/plugins/` | 有序插件启用指令；`-` 前缀禁用 | ✅ |
| `providers` | `map<string, Provider>` | models.dev 目录 | Provider/模型/请求选项覆盖 | ✅ |
| `experimental` | object | 见第 3 节 | 实验开关；可能随时变更/移除 | ✅ |
| `websearch` | object \| `false` | 首次使用时询问 Provider | 搜索 Provider 选择；`false` 移除工具 | ✅ |
| `warming` | boolean \| `{prompt?,interval?,duration?}` | `false` | 缓存保温请求；`true` 等价全默认 | ✅ |
| `command` | `map<string, ...>` | — | V1 单数命令映射；迁移为 `commands` | ⚠️ |
| `reference` | `map<string, ...>` | — | V1 单数引用映射；迁移为 `references` | ⚠️ |
| `plugin` | array | — | V1 插件数组；迁移为 `plugins` | ⚠️ |

### 2.6 Provider 过滤与模型默认值（V1 遗留）

| 字段 | 类型 | 迁移目标 | 状态 |
|---|---|---|---|
| `provider` | `map<string, ProviderConfig>` | `providers` | ⚠️ |
| `enabled_providers` | string[] | 内部“默认 deny + 列出的 allow”的 `provider.use` 策略 | ⚠️ |
| `disabled_providers` | string[] | 对列出的 provider 生成 `provider.use` deny | ⚠️ |
| `small_model` | string | `agents.title.model` | ⚠️ |
| `subagent_depth` | integer（≥0） | `experimental.subagent_depth` | ⚠️ |
| `autoupdate` | boolean \| `"notify"` | `update`：`false`→`disable`、`notify`→`notify`、`true`→`auto` | ⚠️ |
| `autoshare` | boolean | `share`：`true`→`auto`，`false`→`disabled` | ⚠️ |

## 3. `experimental` 子项

所有实验项都可能在次版本变更或移除。当前 schema 中的键：

| 键 | 类型 | 默认 | 作用 |
|---|---|---|---|
| `todo` | boolean | `false` | 给 Agent `todowrite` 工具；V2 默认关闭 |
| `subagent_depth` | integer（≥0） | `1` | 子 Agent 可再开子 Agent 的深度；`1` 表示禁止嵌套 |
| `background_compaction` | boolean | `false` | 后台预写压缩摘要，避免压缩时暂停会话 |
| `image_generate` | boolean \| `{ model? }` | — | `experimental_image_generate` 的默认模型；未指定时自动挑一个 |
| `location_idle_timeout` | duration \| `false` | `"60 minutes"` | 空闲 location 的卸载时间；`false` 永不卸载 |
| `policies` | `{action,resource,effect}[]` | — | 旧位置；合并进顶层 `policies`，新配置不要再用 |
| `schedules` | boolean | — | 启用 schedules 能力 |
| `artifacts` | boolean | — | 启用 artifacts 能力 |
| `codemode_vm` | boolean | — | Code Mode VM 相关开关 |
| `mcp_events` | boolean | — | MCP 事件转发 |
| `part_rows` | boolean | — | 消息 part 行级存储/渲染 |
| `console_watches` | boolean | — | Console watch 能力 |
| `media_blob` | boolean | — | 媒体以 blob 形式存储 |
| `portable_shell_scanner` | boolean | — | 可移植 shell scanner |
| `shell_timeout_background` | boolean | — | shell 超时后转后台 |
| `all_flags` | boolean | — | 打开全部实验 flag（仅开发/验证用） |
| `disable_paste_summary` | boolean | — | 关闭粘贴内容摘要 |
| `batch_tool` | boolean | — | V1 实验项；V2 已移除且忽略 |
| `openTelemetry` | boolean | — | V1 实验项；V2 已移除且忽略 |
| `primary_tools` | boolean | — | V1 实验项；V2 已移除且忽略 |
| `continue_loop_on_deny` | boolean | — | V1 实验项；V2 已移除且忽略 |
| `mcp_timeout` | number | — | V1 实验项；V2 用 `mcp.timeout` |

## 4. 变量替换

配置字符串中支持两种替换，加载时展开：

| 语法 | 含义 | 缺失/出错时 |
|---|---|---|
| `{env:NAME}` | 环境变量 | 替换为空字符串 |
| `{file:path}` | 文件内容 | 替换为空字符串 |

- 路径相对**包含该字符串的配置文件**、绝对路径或 `~/`。
- 展开发生在配置加载时；修改环境变量或文件后需要 reload/重启。
- 密钥建议用 `{env:...}`，不要把明文写进项目配置。

```jsonc
{
  "providers": {
    "anthropic": {
      "settings": { "apiKey": "{env:ANTHROPIC_API_KEY}" }
    }
  }
}
```

## 5. 配置优先级（含 `policies` 的反向顺序）

普通设置从低到高：

```text
1. 全局 ~/.config/opencode/opencode.json(c)
2. 项目直接 opencode.json(c)：外层 → 内层
3. 项目 .opencode/opencode.json(c)：外层 → 内层
4. 系统 /etc/opencode/opencode.json(c)
5. macOS managed preferences（ai.opencode.managed.plist）
```

`policies` 的求值顺序相反：越宽泛的来源越晚求值，因此**覆盖**更窄的来源：

| 优先级 | 来源 |
|---:|---|
| 1（最高） | macOS managed preferences |
| 2 | 系统配置 |
| 3 | 已连接的 Console workspace |
| 4 | 全局配置 |
| 5 | 直接项目配置（外层覆盖内层） |
| 6（最低） | `.opencode` 项目配置（外层覆盖内层） |

- 同一文件内保持书写顺序，最后匹配者生效。
- 仓库不能重新启用被全局 deny 的 provider。
- Console 策略约每分钟刷新；Console 不可达时保留最后一次语句。

## 6. 控制面关注点

1. **节点基线配置放系统目录或临时全局配置**，不要改写用户的 `~/.config/opencode`；需要强制限制时优先生成 `policies`。
2. **`update` 是全局字段**，下发给项目配置无效；节点升级应由 agentd 或部署流程管理。
3. **`lsp` 在 V2 是“接受但不运行”**，不要把它当作能力开关；依赖诊断的功能应改用项目 lint/typecheck 命令。
4. **`media` / `tool_output` 是处理限制，不是安全边界**。真正的访问控制靠 `permissions` 与 `external_directory`。
5. **`agents`、`providers`、`plugins`、`skills` 是合并语义**：控制面下发增量时不要假设会清空用户已有条目。
6. **V1 遗留字段会自动规范化**，但 `logLevel`、`server`、`subagent_depth` 等会被忽略或改写；生成配置时直接写 V2 字段，避免依赖迁移层。
7. **`experimental` 不稳定**：控制面只对确认过版本的节点启用特定实验项，并保留降级路径。

## 7. 相关文档

- 配置合并、位置与字段详解：[03-配置体系.md](03-配置体系.md)
- Provider 与模型字段：[05-模型与提供商.md](05-模型与提供商.md)
- 权限与策略：[09-权限与策略.md](09-权限与策略.md)
- MCP 服务器字段：[08-MCP服务器.md](08-MCP服务器.md)
- 上下文、压缩、快照与引用：[11-上下文管理.md](11-上下文管理.md)
- V1 字段迁移：[17-V1到V2迁移.md](17-V1到V2迁移.md)
- 事件与权限动作速查：[附录B-事件与权限动作速查.md](附录B-事件与权限动作速查.md)
