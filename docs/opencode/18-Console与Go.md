# 18 Console 与 Go

> 来源：官方 `console`、`console/workspaces`、`console/members`、`console/providers`、`console/models`、`console/budgets`、`console/billing`、`console/api`、`console/api/inference`、`console/go`、`console/harnesses`、`console/websearch`、`console/sso`、`console/scim`。
> Console 是**可选**的托管服务；不连接 Console 的 OpenCode 仍然可以完全本地使用。本文说明它提供什么、策略如何下发、Go 订阅与推理网关如何接入，以及 lework 控制面应如何对待它。

## 1. Console 是什么

[OpenCode Console](https://opencode.ai/console) 是一个可选的账号/工作区服务，提供：

- **Workspace**：成员、额度、预算、Provider、策略、服务账号的归属单位；
- **Inference**：托管模型（含免费模型与付费模型）的统一网关；
- **Providers**：把用户自己的 Provider 账号接入 Console 网关，成员无需看到密钥；
- **Usage / Budgets**：按 workspace、成员、服务账号统计与限额；
- **Policies**：向所有连接的 OpenCode 客户端下发 Providers 与 Tools 规则；
- **Web Search**：托管的搜索 Provider；
- **Harnesses**：让 Claude Code、Codex、Hermes、Pi、Raycast 等也能使用 Console 模型；
- **Go / Go Plus / Go Teams**：开源模型的包月订阅。

个人账号注册后会自动创建 **Personal** workspace，只有一个 owner/member。只有需要协作时才邀请成员。

## 2. Workspace

### 2.1 基本操作

| 操作 | 路径 |
|---|---|
| 创建 | 顶栏 workspace 菜单 → **Create Workspace** → 输入名称 |
| 切换 | workspace 菜单选择；Console 保持当前页面 |
| 连接 OpenCode | 在 OpenCode 运行 `/connect`，浏览器选择 workspace |
| 断开/切换 | 再次 `/connect`；workspace policies 会被替换，断开时清除 |
| 删除 | **Settings › General › Delete workspace**；仅 owner，且必须属于至少一个其他 workspace |
| 离开 | **Account › Connected workspaces › Leave**；最后一个 owner 不能离开 |

删除是立即且不可逆的：

- 成员立即失去访问；
- 所有服务账号 API key 与待处理邀请被撤销；
- 自动充值停止；
- 进行中的 Go 订阅结束；
- 剩余 credit 作废；
- 有支付处理中时删除会被阻止。

### 2.2 角色

| 角色 | 权限 |
|---|---|
| Member | 使用 OpenCode；查看自己的 usage、logs、budget |
| Admin | Member 的全部 + 管理成员、账单、预算、Provider、策略、服务账号、设置 |
| Owner | Admin 的全部 + SSO、目录同步、授予 Owner、删除 workspace |

规则：

- 一个 workspace 可有多个 owner，但至少保留一个；
- 不能授予高于自己的角色；
- 不能修改自己的角色；
- 新成员默认 Member。

## 3. Console 策略如何影响 OpenCode

Console 的 **Providers** 与 **Tools** 规则会编译成 `policies` 语句，下发给所有连接到该 workspace 的 OpenCode 客户端。

关键点：

- 约 **1 分钟**内生效；
- 优先级高于成员的全局/项目配置；
- macOS managed preferences 与系统 `opencode.json(c)` 仍然高于 Console；
- 切换 workspace 会替换语句；断开连接会清除；
- Console 不可达时保留最后一次语句；
- Console 与 policy 插件不能被 `plugins` 配置禁用。

优先级回顾：

| 优先级 | 来源 |
|---|---|
| 1（最高） | macOS managed preferences |
| 2 | 系统 `opencode.json(c)` |
| 3 | 已连接的 Console workspace |
| 4 | 全局 `~/.config/opencode/opencode.json(c)` |
| 5 | 直接 `opencode.json(c)`（外层覆盖内层） |
| 6（最低） | `.opencode/opencode.json(c)`（外层覆盖内层） |

因此：**控制面不能用项目配置“覆盖” Console 的强制策略**。如果控制面要做组织级限制，应使用 managed/system `policies` 或 Console，而不是 UI 层拦截。

## 4. Providers 与模型

### 4.1 连接 Provider

Owner/Admin 在 **Providers** 页：

1. **Connect Provider**；
2. 选择目录中的 Provider（Anthropic、OpenAI、Google、xAI、DeepSeek、Mistral、Groq、OpenRouter 等）；
3. 输入 API key，可选 **Test connection**；
4. 保存。

Cloud Provider 需要额外信息：

| Provider | 额外字段 |
|---|---|
| Azure | Resource name、API key |
| AWS Bedrock | Region、API key |
| Google Vertex AI | Region、service account JSON |
| Cloudflare AI Gateway | Account ID、Gateway ID、API token |
| Databricks | Workspace URL、token |

自定义 Provider：

| 字段 | 说明 |
|---|---|
| Base URL | Console 转发目标，如 `https://api.deepseek.com/v1` |
| Format | Chat Completions、Responses API、Anthropic Compatible、Google Generative AI |
| Auth mode | Bearer、API key header、Custom header |
| Credential | Console 发给 Provider 的密钥 |

### 4.2 模型管理

- 每个 Provider 的模型可单独 **Enabled**；
- **New models** 自动启用未来新增模型；
- 可编辑显示名、API ID、工具支持、输入/输出类型；
- 可配置 variants、默认 variant、请求 headers/body、token 限制；
- 可覆盖 **Cost Tiers**（输入/输出/缓存读/缓存写，每 1M token），让 usage 与预算匹配实际合同价；
- 可为长上下文模型添加 **Context** 价格档；
- **Managed providers only** 可把 OpenCode 限制为 workspace 提供的 Provider。

### 4.3 在 OpenCode 中使用

成员运行 `/connect` 选择 workspace；每个启用的 Provider 显示为“workspace 名 / Provider 名”，`/models` 列出模型。

Provider 账单由 Provider 收取，Console 不收 credit；但 Console 会按模型价格记录请求成本，并计入 usage 与预算。

## 5. API key、权限与撤销

### 5.1 两类 key

| | Personal API key | Service account key |
|---|---|---|
| 创建者 | 任意成员，为自己 | Owner/Admin |
| 位置 | **Account › Personal API keys** | **Settings › Service accounts** |
| 身份 | 创建者本人 | 服务账号 |
| 计入 | 成员预算 | 服务账号预算 |
| 场景 | 个人脚本、harness | CI、共享机器、集成 |

- 每个 key 只属于一个 workspace，创建时选择；请求不需要 workspace header。
- 完整 key 只在创建时显示一次，之后只显示前缀。
- 过期时间：Never / 1 day / 7 days / 30 days / Custom（1–365 天）；过期不能续，只能新建。

### 5.2 权限

| 级别 | 允许 |
|---|---|
| All | 该成员/服务账号能做的一切，除了“本账号的 API key” |
| Restricted | 只允许选择的资源 |
| Read only | 所有资源只读；可列模型但不能调用 |

Restricted 资源与级别：

| 资源 | 级别 |
|---|---|
| Models | Read、Request |
| Usage | Read |
| Budgets | Read、Write |
| Members | Read、Write |
| Providers | Read、Write |
| Billing | Read、Write |
| Workspace settings | Read、Write |
| SSO | Read |
| Service accounts | Read |
| This account's API keys | Write（仅服务账号 key） |

限制：

- 任何 key 都不能修改 SSO 或删除 workspace；
- 服务账号 key 即使 All，也不能邀请/改角色/移除成员、管理账单、改 workspace 设置、管理模型、创建/删除服务账号；
- 个人 key 永远没有“本账号的 API key”；
- 越权请求返回 HTTP 403。

### 5.3 撤销

| key | 谁能撤销 | 位置 |
|---|---|---|
| Personal | 本人 | Account › Personal API keys › Revoke |
| Personal | Owner/Admin | Settings › Members → 成员页 → API Keys |
| Service account | Owner/Admin | Settings › Service accounts |

## 6. 预算与用量

预算通过策略限制月度支出，Owner/Admin 在 **Controls** 管理。

| 适用范围 | 作用 |
|---|---|
| Entire workspace | 所有成员与服务账号的合计 |
| All members | 每个成员一份，或全组共享一份 |
| Members tagged with | 按标签 |
| These members | 指定成员 |
| All service accounts | 每个账号一份，或共享 |
| These service accounts | 指定账号 |

- 请求必须同时满足所有适用预算；更大的额度不能覆盖更小的额度。
- 没有预算 = 不限制。
- 金额为 USD，最多两位小数；每月 1 日 00:00 UTC 重置。
- `$0` 预算阻止所有计入预算的支出。
- 修改预算会包含当月已记录支出，不重置额度。

计入预算的支出：

| 支出 | 计入 |
|---|---|
| Console 付费模型 | 是 |
| 自有 Provider（配置了价格） | 是，按配置价 |
| Web search | 是（All spend 预算） |
| 免费模型 | 否 |
| Go 计划内用量 | 否 |
| Go 超量、从 credit 支付 | 是 |

达到预算：新请求返回 **429**，直到重置或 Owner/Admin 提高预算。响应只说明“policy budget exceeded”，不点名具体预算；需要到 **Activity › Logs** 查看。

预算在请求开始时检查；进行中的请求会完成，因此实际支出可能略高于限额。

## 7. Inference API

### 7.1 基址与认证

```text
https://opencode.ai/inference
Authorization: Bearer oc_sk_...
```

免费层聊天模型**只能从 OpenCode 内部使用**；其他客户端即使有 Console key 也会收到 403。使用付费模型或 Go 订阅。

### 7.2 端点

| API | 路径 |
|---|---|
| OpenAI Chat Completions | `/inference/openai/v1/chat/completions` |
| OpenAI Responses | `/inference/openai/v1/responses` |
| Anthropic Messages | `/inference/anthropic/v1/messages` |
| Gemini | `/inference/google/v1beta/models/<model>:generateContent` |

每个模型有一个原生 API family，但 Console 会把请求翻译到其他 API。模型列表：

```bash
curl https://opencode.ai/inference/v1/models
```

### 7.3 成本字段

响应在 `usage`（OpenAI/Anthropic）或 `usageMetadata`（Gemini）旁边报告成本：

```jsonc
"usage": {
  "prompt_tokens": 1200,
  "completion_tokens": 300,
  "cost": 0.00412,
  "opencode": {
    "billing_source": "balance",
    "list_cost": 0.00412,
    "tokens": {
      "input": 1200,
      "output": 300,
      "reasoning": 120,
      "cache_read": 8000,
      "cache_write_5m": 0,
      "cache_write_1h": 0
    }
  }
}
```

| 字段 | 含义 |
|---|---|
| `cost` | 从余额扣除的美元；免费模型/Go 计划内为 0；自有 Provider key 时是估算值 |
| `opencode.billing_source` | `free`、`balance`、`go`、`go-plus`、`byok` |
| `opencode.list_cost` | 按模型标价计算的美元（含峰时/长上下文） |
| `opencode.estimated` | 自有 key 时为 `true`，表示 `cost` 是估算 |
| `opencode.allowance` | 仅 Go：本次请求计入月度额度的美元数 |
| `opencode.tokens` | 计费 token；`input` 不含缓存读写，`output` 含 reasoning |

流式响应在携带最终 usage 的事件上报告成本：Chat Completions 的 final usage chunk、Responses 的 `response.completed`、Messages 的 `message_delta`、Gemini 的最后一个 chunk。

## 8. OpenCode Go

### 8.1 计划

| 计划 | 价格 | 说明 |
|---|---|---|
| **Go** | $10/月 | 低成本访问开源模型 |
| **Go Plus** | $40/月 | 更高额度 |
| **Go Teams** | $20/成员/月 | 每个成员独立额度，比 Go 各模型高 $5/月 |

Go 完全可选，不需要它也能使用 OpenCode。Go/Go Plus 面向单成员 workspace（如 Personal）；可随时升级到 Go Teams，未使用部分按比例抵扣。Go Teams 覆盖所有成员；**服务账号不覆盖**。

### 8.2 接入

```text
/connect     → 选择 OpenCode Go，粘贴 API key
/models      → 选择模型
```

配置中的模型引用使用 `opencode-go/<model-id>`：

```jsonc
{
  "model": "opencode-go/kimi-k3"
}
```

模型列表会变化，当前包括 Grok、GLM、GPT Luna、Claude Haiku、Gemini、Kimi、LongCat、MiMo、MiniMax、Muse Spark、Qwen、DeepSeek、Hy、Step、Beam/Iceman/LongCat 免费项等。以 `/models` 或 `/inference/go/v1/models` 为准。

### 8.3 用量限制

- 限制以**月度美元额度**定义，每个模型有不同月度上限；
- 每个模型分三档：5 小时 20%、每周 50%、每月 100%；
- 每个响应通过 `opencode.allowance` 报告本次计入多少；
- 达到限制后，若开启 **Extra Usage**，可从 Console credit 继续付费；否则请求被阻止；
- Go Teams 默认开启 Extra Usage，可关闭。

### 8.4 端点

Go 使用独立基址：

```text
https://opencode.ai/inference/go
```

示例：

```text
https://opencode.ai/inference/go/openai/v1/chat/completions
https://opencode.ai/inference/go/anthropic/v1/messages
https://opencode.ai/inference/go/openai/v1/responses
https://opencode.ai/inference/go/google/v1beta/models/<model>:generateContent
```

模型列表：

```bash
curl https://opencode.ai/inference/go/v1/models
```

### 8.5 Session header

客户端应为每个会话发送稳定 ID，便于路由与 prompt cache：

```text
x-opencode-session: ses_...
```

- Go **要求**该 header（或识别客户端原生 session header）；
- Claude Code、Codex、Hermes、Pi 的原生 session header 已被识别；
- 某些代理会丢掉 header，转发时必须保留；
- 已验证客户端包括 Hermes（≥v0.21.6）、Claude Code、Codex、ZCode、Pi、jcode（≥v0.81.6）、Kilo Code CLI（含 PR #13752）；
- 已知问题客户端包括 DeepSeek Harness、GitHub Copilot Chat、Kimi Code、MiMo Code（截至文档抓取时）。

### 8.6 隐私与区域

- 工作区隐私设置适用于 Go；
- 默认阻止“会用请求数据训练”的付费端点，允许区域为欧洲与美国；没有保存隐私设置的工作区还允许 Global（中国请求）；
- DeepSeek V4.1 Flash/V4 Pro、LongCat 2.5 Free 需要 **Global** 区域；
- Muse Spark Contributor、Iceman Contributor Free 会使用 prompt/completion 训练；
- Grok、GPT、Claude Haiku、Beam Free 等有 30 天保留；多数开源模型为 0 天；
- DeepSeek 的 ZDR 协议按月续签。

## 9. Harnesses（非 OpenCode 客户端）

Console 允许把 Claude Code、Codex、Hermes、Pi、Raycast 等指向 Console 网关，用 Console API key 使用付费或 Go 模型。

| Harness | Pay-as-you-go | Go | MCP |
|---|---|---|---|
| Claude Code | 是 | 是 | 是 |
| Codex | 是 | 是 | 是 |
| Hermes | 内置 | 内置 | 是 |
| Pi | 内置 | 内置 | 是 |
| Raycast | 扩展 | 扩展 | 是 |

免费层聊天模型只允许 OpenCode 内部使用；其他 harness 收到 403。

### 9.1 基址

| 计划 | Base URL |
|---|---|
| Pay-as-you-go | `https://opencode.ai/inference` |
| Go | `https://opencode.ai/inference/go` |

按 harness 使用的 API 追加路径：

| API | 路径 |
|---|---|
| OpenAI Responses | `/openai/v1/responses` |
| OpenAI Chat Completions | `/openai/v1/chat/completions` |
| Anthropic Messages | `/anthropic/v1/messages` |
| Gemini | `/google/v1beta/models/<model>:generateContent` |

示例：

```text
https://opencode.ai/inference/go/openai/v1/chat/completions
```

### 9.2 Console MCP

支持 MCP 的 harness 可通过 Console MCP 管理 workspace：

```text
https://opencode.ai/console/mcp
```

- 使用 OAuth + 动态客户端注册，浏览器登录，不需要粘贴 key；
- OpenCode 在 workspace 启用后会自动添加 `opencode_console`，首次使用时要求浏览器登录。

## 10. lework 控制面应如何处理 Console

### 10.1 不要把 Console 当成控制面后端

Console 是 OpenCode 的托管服务，不是 lework 的 API。控制面不直接调用 Console 的 workspace/billing API，也不应保存成员的 Console API key。

正确分工：

| 事项 | 归属 |
|---|---|
| 模型凭据、Provider 账号 | 节点上的 OpenCode（`/connect` 或环境变量） |
| Console workspace 策略 | OpenCode 通过 Console 自动拉取，控制面只展示“有效策略” |
| 预算/用量 | Console；控制面可通过 OpenCode 的 `session.usage.updated` 与会话成本展示 |
| 429/预算错误 | OpenCode 返回错误，控制面原样呈现并提示去 Console 调整 |
| API key | 不进入控制面数据库、日志、事件 |

### 10.2 展示有效策略

控制面可以读取节点的配置/策略快照（未来能力），但必须区分：

- **用户/项目配置**：可编辑；
- **Console 下发策略**：只读，约 1 分钟同步；
- **系统/managed 策略**：最高优先级，只读。

UI 不应允许“在控制面里覆盖 Console deny”。如果用户尝试，应该明确提示“该限制来自 workspace 策略”。

### 10.3 Go 与模型目录

- 模型 ID 是 `opencode-go/<model-id>`；控制面展示时应保留原始 ID，不要自己拼 Provider 名。
- 模型可用性来自节点 `GET /api/model`，不要在控制面硬编码模型列表。
- 如果控制面未来直接调用推理网关（不经过 OpenCode），必须：
  - 使用节点本地凭据，不落控制面；
  - 发送 `x-opencode-session`；
  - 处理 429、预算错误、区域限制；
  - 不把免费层模型用于非 OpenCode harness。

### 10.4 组织限制的正确位置

| 需求 | 正确机制 |
|---|---|
| 禁止某 Provider | `policies: provider.use`（或 Console Providers 规则） |
| 禁止某 shell 命令 | `policies: tool.use` + `permissions: shell` |
| 只允许某些 MCP/Skill/Plugin | `policies: mcp.use/skill.use/plugin.use` |
| 限制搜索 Provider | `policies: websearch.use` |
| 限制预算 | Console Controls / 预算策略 |
| 限制模型 | Console Models 开关或 Provider 策略 |

不要用控制面的按钮禁用、前端隐藏、数据库标记来替代上述机制。

## 11. 相关文档

- [03-配置体系.md](03-配置体系.md)
- [05-模型与提供商.md](05-模型与提供商.md)
- [09-权限与策略.md](09-权限与策略.md)
- [15-控制面Agent层集成指南.md](15-控制面Agent层集成指南.md)
- [16-故障排查与运维.md](16-故障排查与运维.md)
