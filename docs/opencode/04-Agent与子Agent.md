# 04 Agent 与子 Agent

> 来源：官方 `agents`、`config#agents`、`tools#subagent`、`permissions#agents`、`cli/acp`、`config#experimental`。
> 适用：OpenCode 2.x。

## 1. Agent 是什么

Agent = **系统提示 + 模型偏好 + 权限规则 + 展示信息** 的命名配置。它决定“以什么身份、用什么模型、能做什么”。

Agent 有且只有三种模式：

| `mode` | 含义 |
|---|---|
| `primary` | 作为会话主 Agent 运行；新建自定义 Agent 的默认值 |
| `subagent` | 只能通过 `subagent` 工具在子会话中运行 |
| `all` | 既可主用，也可被派遣 |

## 2. 定义位置

### 2.1 Markdown 文件

```text
~/.config/opencode/agents/<name>.md      # 全局
.opencode/agents/<name>.md               # 项目
```

规则：

- 从当前目录向上发现项目的 `.opencode` 目录，直到项目根。
- 嵌套路径成为 Agent ID 的一部分：`.opencode/agents/team/reviewer.md` → `team/reviewer`。
- 文件正文是 `system` 提示；frontmatter 字段与 JSON 配置项同名。
- V1 的 `agent/`、`mode/`、`modes/` 目录仍会被发现（见 17），新文件统一用 `agents/`。

```md title=".opencode/agents/reviewer.md"
---
description: Reviews changes for correctness and regressions
mode: subagent
model: anthropic/claude-sonnet-5-5#high
permissions:
  - action: edit
    resource: "*"
    effect: deny
  - action: shell
    resource: "*"
    effect: deny
---

Review the current changes. List findings in severity order with file and line references.
```

### 2.2 JSON / JSONC

```jsonc title="opencode.jsonc"
{
  "$schema": "https://opencode.ai/config.json",
  "agents": {
    "reviewer": {
      "description": "Reviews current changes",
      "mode": "subagent",
      "model": "anthropic/claude-sonnet-5-5#high",
      "system": "Report findings in severity order.",
      "permissions": [
        { "action": "edit", "resource": "*", "effect": "deny" }
      ]
    }
  }
}
```

## 3. 选择主 Agent

```jsonc
{
  "default_agent": "writer",
  "agents": {
    "writer": { "mode": "primary" }
  }
}
```

- `default_agent` 必须存在、可见、支持 primary；否则回退 `build`，再回退第一个可见的主 Agent。
- 修改默认 Agent **不会**替换已存在会话记录的 Agent；换 Agent 要用 `POST /api/session/{id}/agent` 或 `session/switchAgent`。
- 会话会单独记录 model；按 ID 选择主 Agent 不会改变该模型。

## 4. 内置 Agent

| Agent | 模式 | 用途 | 额外权限（相对基础策略） |
|---|---|---|---|
| `build` | primary | 默认编码 Agent | 允许 `question` |
| `plan` | primary | 探索与计划，不改普通项目文件 | 允许 `question`；拒绝编辑，除 `~/.opencode/plan` |
| `general` | subagent | 研究/多步骤任务，工具权限广 | 拒绝 `question` 与启动子 Agent |
| `explore` | subagent | 只读搜索代码/网页 | 除 shell/read/glob/grep/webfetch/websearch/external_directory 外全部拒绝；`.env` 读取需审批 |
| `compaction` | 隐藏 | 压缩摘要 | 保持基础策略 |
| `title` | 隐藏 | 生成标题 | 拒绝全部 |
| `summary` | 隐藏 | 生成摘要 | 拒绝全部 |

- 隐藏 Agent 不能被用户直接选择。
- V2 **没有** `scout`。
- 覆盖内置 Agent：用同一个 ID 定义。

```jsonc
{
  "agents": {
    "build": {
      "permissions": [
        { "action": "shell", "resource": "git push *", "effect": "ask" }
      ]
    }
  }
}
```

- `disabled: true` 可在配置加载的该点移除内置或自定义 Agent。

## 5. 合并语义

Agent 定义按配置顺序合并：

- 标量：后者覆盖前者。
- `request` map：按 key 合并。
- `permissions`：**追加**（不是替换）。
- 全局 `permissions` 先应用，Agent 规则追加在后，所以 Agent 可细化全局规则。

```jsonc
{
  "permissions": [
    { "action": "shell", "resource": "*", "effect": "ask" }
  ],
  "agents": {
    "build": {
      "permissions": [
        { "action": "shell", "resource": "git status", "effect": "allow" }
      ]
    }
  }
}
```

## 6. 全部配置项

| 字段 | 类型 | 说明 |
|---|---|---|
| `description` | string | 用途说明；对 subagent 尤其重要，模型靠它选择 Agent |
| `mode` | `primary`\|`subagent`\|`all` | 默认 `primary`（新自定义 Agent） |
| `model` | string \| object | `provider/model[#variant]`，或 `{providerID, model, variant}` |
| `system` | string | 系统提示；非空时**替换**该 Agent 的 Provider 基础提示 |
| `permissions` | 规则数组 | 有序权限规则 |
| `steps` | 正整数 | 最大模型步数 |
| `hidden` | boolean | 从列表、交互发现、子 Agent 目录中隐藏 |
| `color` | `#rrggbb` | UI 颜色 |
| `disabled` | boolean | 在配置加载该点移除 Agent |
| `request` | object | `headers` 与 `body` 覆盖；**V2 保留但尚未发送** |

### 6.1 `system` 与 Markdown 正文

- `system` 非空会替换 Provider 基础提示；项目指令、技能、references 等仍会加入。
- Markdown Agent 的正文就是 `system`，不要在 frontmatter 再写 `system`。

### 6.2 `model`

- 子 Agent 未配置模型时**继承父会话模型**。
- JSON 支持展开形式，便于程序生成配置。

```jsonc
{
  "agents": {
    "reviewer": {
      "model": {
        "providerID": "anthropic",
        "model": "claude-sonnet-5-5",
        "variant": "high"
      }
    }
  }
}
```

### 6.3 `steps`

```yaml
steps: 8
```

到达最后一步时，OpenCode 移除工具并要求模型用文本总结；新的用户输入会重置额度。

### 6.4 `hidden` 与 `color`

- `hidden` 只控制可见性，**不是安全边界**；限制行为用 `permissions`。
- `color` 为六位十六进制。

### 6.5 `request`（重要限制）

```jsonc
{
  "agents": {
    "reviewer": {
      "request": {
        "headers": { "x-agent": "reviewer" },
        "body": { "temperature": 0.1 }
      }
    }
  }
}
```

V2 的 session runner **保留这些值但尚未随模型请求发送**。要生效，把 `temperature`/`top_p` 等放到 Provider、模型或模型变体上：

```jsonc
{
  "providers": {
    "openai": {
      "models": {
        "gpt-6.1-sol": {
          "variants": [{ "id": "review", "body": { "temperature": 0.2 } }]
        }
      }
    }
  },
  "agents": { "reviewer": { "model": "openai/gpt-6.1-sol#review" } }
}
```

### 6.6 不要使用的旧字段

新 V2 Agent 配置**不要**使用：`temperature`、`top_p`、`prompt`、`permission`、`tools`、`disable`、`maxSteps`。对应新字段：`system`、`permissions`、`disabled`、`steps`。

传 `tools` 这种旧字段会触发配置警告；需要禁用工具请用 `permissions`（例如 `{action:"edit",resource:"*",effect:"deny"}`）。

## 7. 权限与 Agent

- Agent 的 `permissions` 是**追加**到全局规则之后的有序列表。
- **子 Agent 使用自己的权限，不是父权限的子集**。父 Agent 的 `subagent` 权限只决定它能启动哪些子 Agent。
- 例：只允许启动 `reviewer`：

```jsonc
{
  "agents": {
    "orchestrator": {
      "permissions": [
        { "action": "subagent", "resource": "*", "effect": "deny" },
        { "action": "subagent", "resource": "reviewer", "effect": "allow" }
      ]
    }
  }
}
```

- 会话级权限规则（创建/更新 session 时传入）会在 Agent 规则之后求值；子会话继承创建时生效的规则。

## 8. 子 Agent 机制

### 8.1 通过工具启动

`subagent` 工具参数：Agent ID、简短 `description`、完整 `prompt`。

- 前台调用：等待结果。
- `background: true`：立即返回，子会话完成后通知父会话。
- 返回的 `sessionID` 可用于继续同一个子会话。
- 只有 `subagent` 模式的 Agent 可用。
- 默认嵌套深度为 1（子 Agent 不能再开子 Agent）；`experimental.subagent_depth` 可调。

### 8.2 上下文与状态

- 子 Agent 在**全新的子会话**中运行，拥有独立上下文。
- 子会话有自己的 Agent、模型、权限、标题、token/cost。
- 父会话的 `subagent` 权限控制“能否启动、启动谁”；子会话行为由子 Agent 自己的 `permissions` 决定。
- 子会话可以前台或后台运行；后台完成时把结果/失败回报给父会话。

### 8.3 与命令的关系

斜杠命令的 `subagent: true` 会强制用子会话执行（即使 Agent 是 primary）；详情见 10。

## 9. Agent 与模型/命令的优先级

模型选择顺序（命令场景）：

1. 命令的 `model`
2. 所选命令 Agent 的 `model`
3. 会话当前模型

Agent 场景：

1. Agent 的 `model`
2. 子 Agent 继承父会话模型（未配置时）
3. 会话模型
4. 配置默认模型

## 10. API 与 ACP 映射

| 场景 | HTTP API | ACP |
|---|---|---|
| 列出 Agent | `GET /api/agent` | `available_commands_update` / `config_option_update` 中的 `mode` |
| 读取单个 | `GET /api/agent/{agentID}` | — |
| 会话切 Agent | `POST /api/session/{id}/agent` | `session/set_mode` 或 `session/set_config_option`（`mode`） |
| 子会话 | 内部由 `subagent` 工具创建 | `_meta["opencode/child-session"]` 标记 |

`Agent.Info` 关键字段：`id`、`name`、`model`、`request`、`system`、`description`、`mode`、`hidden`、`color`、`steps`、`permissions`。

## 11. 控制面关注点

1. **Agent 列表是 location 相关的**：`GET /api/agent` 返回 `{ location, data }`，控制面切换节点/项目后要重新拉取。
2. **不要在前端自行拼 Agent 权限**；展示 `permissions` 规则即可，实际裁决在 server。
3. **子会话要显示为树**：`Session.Info.parentID` + `fork` 字段 + 事件中的子会话标记。控制面应支持折叠，但不要复制子会话状态。
4. **默认 Agent 不要在控制面写死**；读 `GET /api/config` 或 `/api/agent`，否则会与用户配置冲突。
5. **`request` 尚未生效**，不要把 per-agent 采样参数当作可配置能力暴露给用户。
6. **`steps` 会用尽**：长任务中 Agent 可能在最后一步只输出总结，控制面应允许用户继续发消息重置额度。
