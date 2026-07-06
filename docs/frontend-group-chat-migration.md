# 群聊模式 — 接口字段变动

> 基于后端 PR #27 `feat(group-chat): 任务内群聊协作服务端实现`

---

## 1. 新增接口

### `POST /v1/GlobalEvents`（全局 SSE 通知）

project 级持久 SSE 长连接，实时接收新消息通知。

**请求体**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `replay_since_seq` | `number` | 否 | 断线重连时传入，服务端从该 seq 补推期间事件 |

**SSE 事件 `message.created`**：

按 `sender_type` 区分两种 payload：

**sender_type = `human`**（真人发言）：

| 字段 | 类型 | 说明 |
|------|------|------|
| `type` | `"message.created"` | 事件类型 |
| `project_id` | `number` | 项目 ID |
| `session_id` | `string` | 会话 ID |
| `seq` | `number` | 全局事件序号 |
| `timestamp` | `number` | 事件时间戳 |
| `data.sender_type` | `"human"` | 发送者类型 |
| `data.sender_uin` | `number` | 发送者用户 ID |
| `data.sender_name` | `string` | 发送者名称 |
| `data.content` | `string` | 消息完整内容，前端可直接渲染 |
| `data.message_type` | `string` | 消息类型 |
| `data.sequence` | `number` | 会话内消息序号 |
| `data.attachments` | `array` | 附件列表 |
| `data.created_at` | `string` | 消息创建时间 |

**sender_type = `assistant`**（AI 队友开始回复）：

| 字段 | 类型 | 说明 |
|------|------|------|
| `type` | `"message.created"` | 事件类型 |
| `project_id` | `number` | 项目 ID |
| `session_id` | `string` | 会话 ID |
| `seq` | `number` | 全局事件序号 |
| `timestamp` | `number` | 事件时间戳 |
| `data.sender_type` | `"assistant"` | 发送者类型 |
| `data.assistant_id` | `number` | AI 队友 ID |
| `data.assistant_name` | `string` | AI 队友名称 |
| `data.run_id` | `string` | 关联的 agent run ID |
| `data.content` | `string` | 始终为空（T1 时刻消息尚未落库），前端凭此区分"AI 开始回复"通知，收到后通过 `session_id` + `assistant_id` 订阅 SessionEvents 获取流式输出 |

---

## 2. 已有接口字段变动

### `POST /v1/AddMessage` / `POST /v1/NewMessage` — 请求体

| 字段 | 旧 | 新 | 说明 |
|------|-----|-----|------|
| `assistant_id` | `number` | 废弃 | — |
| `assistant_ids` | 无 | `number[]` | 改为数组，当前后端取第一个 |

### `POST /v1/SessionEvents` — 请求体

| 字段 | 旧 | 新 | 说明 |
|------|-----|-----|------|
| `assistant_id` | 无 | `number?` | 可选，过滤指定 AI 队友的流式事件 |

### `POST /v1/CancelSessionRun` — 请求体

| 字段 | 旧 | 新 | 说明 |
|------|-----|-----|------|
| `run_id` | 无 | `string?` | 可选，精确取消指定的 run |

### `GetSessionMessages` — 响应

| 字段 | 旧 | 新 | 说明 |
|------|-----|-----|------|
| `sender_uin` | 无 | `number?` | 发送者用户 ID，人类有值，AI 为 null |
| `sender_name` | 无 | `string` | 发送者名称 |
| `run_id` | 无 | `string?` | 关联的 agent run ID |

### `SessionEvents` SSE — 事件

| 事件类型 | 旧 | 新 | 说明 |
|----------|-----|-----|------|
| `run.started` | 无 | 新增 | payload: `{ run_id: string }` |

---

## 3. 汇总

| 接口 | 方向 | 字段 | 变更 |
|------|------|------|------|
| `POST /v1/GlobalEvents` | 请求 | `replay_since_seq` | 新增（新接口） |
| `GlobalEvents` SSE | 事件 | `message.created` | 新增，按 sender_type 分 human/assistant 两种 payload |
| `POST /v1/AddMessage` | 请求 | `assistant_id` → `assistant_ids` | `number` 改为 `number[]` |
| `POST /v1/NewMessage` | 请求 | `assistant_id` → `assistant_ids` | `number` 改为 `number[]` |
| `POST /v1/SessionEvents` | 请求 | `assistant_id` | 新增 `number?` |
| `POST /v1/CancelSessionRun` | 请求 | `run_id` | 新增 `string?` |
| `GetSessionMessages` | 响应 | `sender_uin` | 新增 `number?` |
| `GetSessionMessages` | 响应 | `sender_name` | 新增 `string` |
| `GetSessionMessages` | 响应 | `run_id` | 新增 `string?` |
| `SessionEvents` SSE | 事件 | `run.started` | 新增 `{ run_id: string }` |
