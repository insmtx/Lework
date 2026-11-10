# 前端适配指南：项目成员与 PublicID 字段迁移

> 对应后端 MR：feat/project-member（Code 重命名为 PublicID，支持项目成员（用户+助手），成员权限控制）

## 目录

- [一、字段变更速查](#一字段变更速查)
- [二、DigitalAssistant：`code` → `public_id`](#二digitalassistantcode--public_id)
- [三、Project 成员模型变更](#三project-成员模型变更)
- [四、Session 接口变动](#四session-接口变动)
- [五、依赖接口文档](#五依赖接口文档)

---

## 一、字段变更速查

| 旧字段 | 新字段 | 影响范围 |
|--------|--------|---------|
| `DigitalAssistant.code` | `DigitalAssistant.public_id` | 创建/查询/列表 AI 队友、项目详情中的成员信息 |
| `CreateProject.assistant_ids` | `CreateProject.members` | 创建项目、编辑项目 |
| `UpdateProject.assistant_ids` | `UpdateProject.members` | 编辑项目 |
| `GetDigitalAssistant.code`（请求参数） | `GetDigitalAssistant.public_id`（请求参数） | 查询 AI 队友详情 |
| `Session.assistant_code` | **已移除** | 会话列表响应 |
| `ListSessionsRequest.assistant_code` | **已移除** | 会话列表筛选 |
| `assistant_id` 字段类型 | `uint` → `string`（值为 PublicID） | 所有接口的 assistant_id/applicated_assistant_id 字段 |

---

## 二、DigitalAssistant：`code` → `public_id`

### 2.1 影响的接口

| 接口 | 请求变更 | 响应变更 |
|------|---------|---------|
| `POST /CreateDigitalAssistant` | `code` → `public_id` | `code` → `public_id` |
| `POST /CreateDigitalAssistantFromTemplate` | `code` → `public_id` | `code` → `public_id` |
| `POST /GetDigitalAssistant` | 查询参数 `code` → `public_id` | `code` → `public_id` |
| `POST /ListDigitalAssistant` | 无 | 列表项 `code` → `public_id` |
| `POST /UpdateDigitalAssistant` | 无 | 无变化 |
| `POST /DetailProject` → `members` | 无 | 嵌套数字助手信息中不再有 `code` 字段 |

### 2.2 请求变更示例

```diff
// POST /GetDigitalAssistant
- { "code": "assistant_xxx" }
+ { "id": 1 }
+ // 或
+ { "public_id": "assistant_xxx" }
```

```diff
// POST /CreateDigitalAssistant
- { "code": "my-assistant", "name": "My Assistant" }
+ { "public_id": "my-assistant", "name": "My Assistant" }
```

### 2.3 响应变更示例

```diff
// 所有包含 DigitalAssistant 对象的响应中
{
  "id": 1,
- "code": "assistant_abc123",
+ "public_id": "assistant_abc123",
  "name": "代码审查助手",
  ...
}
```

### 2.4 适配要点

前端代码中搜索 `item.code`、`assistant.code`、`data.code` 等引用 DigitalAssistant 的 `code` 字段处，统一改为 `public_id`。

---

## 三、Project 成员模型变更

### 3.1 `CreateProject` 请求变更

```diff
// POST /CreateProject
{
  "name": "项目名",
  "description": "项目描述",
- "assistant_ids": [1, 2, 3],
+ "members": [
+   { "type": "assistant", "id": "1" },
+   { "type": "assistant", "id": "2" },
+   { "type": "user", "id": "user_abc123" }
+ ]
}
```

**`MemberInput` 字段说明：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `type` | `string` | 是 | `"user"` 表示人类用户，`"assistant"` 表示 AI 队友 |
| `id` | `string` | 是 | `type=assistant` 时传 `public_id`（从 ListDigitalAssistant 的 `public_id` 获取），`type=user` 时传 `public_id`（从 ListUsers 的 `public_id` 获取） |

### 3.2 `UpdateProject` 请求变更

```diff
// POST /UpdateProject
{
  "public_id": "proj_xxx",
  "name": "新名称",
- "assistant_ids": [1, 3],
+ "members": [
+   { "type": "assistant", "id": "1" },
+   { "type": "user", "id": "user_abc123" }
+ ]
}
```

**适配要点：**

- 编辑成员时，将当前要保留的成员 + 要新增的成员组合为完整数组，通过 `members` 全量提交
- 不在数组中的成员会被自动移除（`is_default=true` 的默认 AI 队友和 `member_role=owner` 的创建者不可移除，传不传都会被保留）
- `members` 传空数组 `[]` 只保留 owner + 默认 AI 队友，不会删除它们

### 3.3 新增/编辑项目时获取候选列表

**获取用户列表供选择（添加用户成员）：**

调用 `POST /ListUsers`（详见 [五.5.1](#51-post-listusers---查询用户列表)）

**获取 AI 队友列表供选择（添加 AI 队友）：**

调用 `POST /ListDigitalAssistant`（详见 [五.5.2](#52-post-listdigitalassistant---查询-ai-队友列表)）

**成员 ID 对应关系：**

| 成员类型 | 列表接口 | 取哪个字段作为 `id` |
|----------|---------|-------------------|
| `user` | `POST /ListUsers` | `item.public_id` |
| `assistant` | `POST /ListDigitalAssistant` | `item.public_id` |

### 3.4 项目成员展示

当前成员列表通过 `POST /DetailProject` 获取（详见 [五.5.3](#53-post-detailproject---项目详情含成员列表)）。

`ProjectMemberItem` 字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| `member_id` | `uint` | 成员 ID（user 表或 digital_assistant 表的自增 ID） |
| `member_type` | `string` | `"user"` 或 `"assistant"` |
| `member_role` | `string` | `"owner"`（创建者，不可移除）、`"admin"`、`"member"`、`"viewer"` |
| `is_default` | `bool` | `true` 表示系统默认 AI 队友（不可移除） |
| `joined_at` | `time` | 加入时间 |
| `name` | `string` | 用户名 / AI 队友名 |
| `avatar_url` | `string` | 头像 URL |

### 3.5 权限变更

- 项目列表 `POST /ListProjects`：现在返回用户所有有权限的项目（owner + member），而不再仅返回 owner 的项目
- 项目详情/文件/记忆等接口：非 owner 但有 member 角色的用户也能访问

---

## 四、`assistant_id` 字段类型变更：`uint` → `string` (PublicID)

### 4.1 变更说明

所有接口中 `assistant_id` / `allocated_assistant_id` / `assistant_ids` 字段的类型从 `uint`（自增 ID）改为 `string`（`DigitalAssistant.PublicID`）。字段名不变，但取值逻辑变了。

### 4.2 影响的接口和字段

#### 请求参数

| 接口 | 字段 | 旧类型 | 新类型 |
|------|------|--------|--------|
| `POST /CreateSession` | `assistant_id` | `uint` | `string` |
| `POST /AddMessage` | `assistant_ids` | `[]uint` | `[]string` |
| `POST /ListSessions` | `assistant_id` | `*uint` | `*string` |
| `POST /NewMessage` | `assistant_ids` | `[]uint` | `[]string` |
| `POST /SessionEvents` | `assistant_id` | `uint` | `string` |

#### 响应字段

| 接口 | 字段 | 旧类型 | 新类型 |
|------|------|--------|--------|
| `POST /ListSessions` 响应 | `Session.assistant_id` | `uint` | `string` |
| `POST /ListSessions` 响应 | `Session.allocated_assistant_id` | `uint` | `string` |
| `POST /GetSession` 响应 | `Session.assistant_id` | `uint` | `string` |
| `POST /GetSession` 响应 | `Session.allocated_assistant_id` | `uint` | `string` |
| `POST /CreateSession` 响应 | `Session.assistant_id` | `uint` | `string` |
| `POST /NewMessage` 响应 | `NewMessageResponse.assistant_id` | `uint` | `string` |
| `POST /SessionEvents` SSE 流 | RunEvent 中传递 assistant 信息时 | `uint` | `string` |
| `POST /GlobalEvents` SSE 流 | `MessageCreatedData.assistant_id` | `*uint` | `*string` |

### 4.3 `Session` 对象示例

```diff
{
  "session_id": "sess_xxx",
  "type": "project",
- "assistant_id": 1,
- "allocated_assistant_id": 1,
+ "assistant_id": "assistant_abc123",
+ "allocated_assistant_id": "assistant_abc123",
  "status": "active",
  ...
}
```

### 4.4 适配要点

- 前端从 `assistant_id` 取值后，可直接用于调用 `POST /GetDigitalAssistant` 的 `public_id` 参数
- 调用 `POST /SessionEvents` 时传入的 `assistant_id` 应使用 `DigitalAssistant.PublicID`（从 GlobalEvents 的 `MessageCreatedData.assistant_id` 获取或从 `Session.assistant_id` 获取）
- `assistant_ids`（复数）字段同样用 PublicID 字符串数组

---

## 五、Session 接口变动

### 5.1 `POST /ListSessions`

```diff
// 请求参数
{
  "type": "project",
- "assistant_code": "assistant_xxx",  // 已移除
- "assistant_id": 1,                   // 类型变更：uint → string
+ "assistant_id": "assistant_abc123",  // 值为 PublicID
  "keyword": "",
  "offset": 0,
  "limit": 20
}
```

```diff
// 响应 Session 对象
{
  "session_id": "sess_xxx",
- "assistant_id": 1,
- "allocated_assistant_id": 1,
- "assistant_code": "assistant_xxx",  // 已移除
+ "assistant_id": "assistant_abc123",
+ "allocated_assistant_id": "assistant_abc123",
  "status": "active",
  ...
}
```

---

## 五、依赖接口文档

### 通用约定

- Base URL: `/v1`
- 请求方式: 所有接口统一使用 `POST`
- 认证方式: 通过 Cookie/Session 传递认证信息
- 通用响应格式:

```json
{
  "code": 0,     // 0 表示成功，非 0 表示错误
  "msg": "ok",
  "data": {}
}
```

- 分页请求公共参数:

```json
{
  "offset": 0,
  "limit": 20,
  "list_all": false
}
```

---

### 5.1 `POST /ListUsers` - 查询用户列表

用于添加项目成员时搜索和选择人类用户。

```
POST /v1/ListUsers
```

**请求参数：**

```json
{
  "keyword": "张三",
  "github_login": "",
  "offset": 0,
  "limit": 20
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `keyword` | `string` | 否 | 按姓名模糊搜索 |
| `github_login` | `string` | 否 | 按 GitHub 登录名精确搜索 |
| `offset` | `int` | 否 | 偏移量，默认 0 |
| `limit` | `int` | 否 | 每页条数，默认 20 |

**响应示例：**

```json
{
  "code": 0,
  "data": {
    "total": 100,
    "offset": 0,
    "limit": 20,
    "items": [
      {
        "id": 1,
        "public_id": "user_abc123",
        "name": "张三",
        "avatar_url": "https://example.com/avatar.jpg",
        "email": "zhang@example.com",
        "github_login": "zhangsan"
      }
    ]
  }
}
```

**在 `CreateProject` / `UpdateProject` 中的用法：**

```json
// 将选中用户的 public_id 作为成员 ID 传入
{
  "members": [
    { "type": "user", "id": "user_abc123" }
  ]
}
```

---

### 5.2 `POST /ListDigitalAssistant` - 查询 AI 队友列表

用于添加项目成员时搜索和选择 AI 队友。

```
POST /v1/ListDigitalAssistant
```

**请求参数：**

```json
{
  "status": "active",
  "keyword": "",
  "source": "",
  "offset": 0,
  "limit": 20
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `status` | `string` | 否 | `"draft"` / `"active"` / `"inactive"` / `"archived"` |
| `keyword` | `string` | 否 | 按名称/public_id/描述搜索 |
| `source` | `string` | 否 | `"manual"` / `"template"` / `"system"` |
| `offset` | `int` | 否 | 偏移量，默认 0 |
| `limit` | `int` | 否 | 每页条数，默认 20 |

**响应示例：**

```json
{
  "code": 0,
  "data": {
    "total": 10,
    "offset": 0,
    "limit": 20,
    "items": [
      {
        "id": 1,
        "public_id": "assistant_abc123",
        "org_id": 1,
        "owner_id": 1,
        "name": "代码审查助手",
        "description": "帮助审查代码质量",
        "avatar": "https://example.com/avatar.png",
        "status": "active",
        "version": 1,
        "system_prompt": "你是一个专业的代码审查助手...",
        "expertise": ["code_review", "golang"],
        "source": "manual",
        "deployment": {
          "public_id": "wrk_xxx",
          "status": "ready",
          "last_error": ""
        },
        "created_at": "2025-07-01T10:00:00Z",
        "updated_at": "2025-07-01T10:00:00Z"
      }
    ]
  }
}
```

**在 `CreateProject` / `UpdateProject` 中的用法：**

```json
// 将选中 AI 队友的 public_id 作为成员 ID 传入
{
  "members": [
    { "type": "assistant", "id": "assistant_abc123" }
  ]
}
```

---

### 5.3 `POST /DetailProject` - 项目详情（含成员列表）

获取项目完整详情，包含成员列表、任务、会话。

```
POST /v1/DetailProject
```

**请求参数：**

```json
{
  "public_id": "proj_abc123"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `public_id` | `string` | 是 | 项目 public_id |

**响应示例：**

```json
{
  "code": 0,
  "data": {
    "public_id": "proj_abc123",
    "name": "我的项目",
    "description": "项目描述",
    "objective": "项目目标",
    "owner_id": 1,
    "status": "active",
    "task_count": 5,
    "metadata": {},
    "created_at": "2025-07-01T10:00:00Z",
    "updated_at": "2025-07-02T12:00:00Z",
    "session": {
      "session_id": "sess_xxx",
      "type": "project",
      ...
    },
    "tasks": [
      {
        "public_id": "task_xxx",
        "title": "任务标题",
        ...
      }
    ],
    "members": [
      {
        "member_id": 1,
        "member_type": "user",
        "member_role": "owner",
        "is_default": false,
        "joined_at": "2025-07-01T10:00:00Z",
        "name": "张三",
        "avatar_url": "https://example.com/avatar.jpg"
      },
      {
        "member_id": 2,
        "member_type": "assistant",
        "member_role": "member",
        "is_default": true,
        "joined_at": "2025-07-01T10:00:00Z",
        "name": "lework",
        "avatar_url": ""
      },
      {
        "member_id": 3,
        "member_type": "assistant",
        "member_role": "member",
        "is_default": false,
        "joined_at": "2025-07-02T12:00:00Z",
        "name": "代码审查助手",
        "avatar_url": "https://example.com/avatar.png"
      }
    ]
  }
}
```

---

### 5.4 `POST /GetProject` - 获取项目基本信息

获取项目基本信息，不含成员、任务、会话等详情。

```
POST /v1/GetProject
```

**请求参数：**

```json
{
  "public_id": "proj_abc123"
}
```

**响应示例：**

```json
{
  "code": 0,
  "data": {
    "public_id": "proj_abc123",
    "name": "我的项目",
    "description": "项目描述",
    "objective": "项目目标",
    "owner_id": 1,
    "status": "active",
    "task_count": 5,
    "metadata": {},
    "created_at": "2025-07-01T10:00:00Z",
    "updated_at": "2025-07-02T12:00:00Z"
  }
}
```

---

### 5.5 `POST /ListProjects` - 查询项目列表

```
POST /v1/ListProjects
```

**请求参数：**

```json
{
  "keyword": "",
  "status": "",
  "offset": 0,
  "limit": 20
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `keyword` | `string` | 否 | 按项目名模糊搜索 |
| `status` | `string` | 否 | 按状态筛选 |
| `offset` | `int` | 否 | 偏移量，默认 0 |
| `limit` | `int` | 否 | 每页条数，默认 20 |

> **变更说明：** 现在返回用户所有有权限的项目（owner + member），不再仅限于 owner 的项目。

**响应示例：**

```json
{
  "code": 0,
  "data": {
    "total": 50,
    "offset": 0,
    "limit": 20,
    "items": [
      {
        "public_id": "proj_abc123",
        "name": "我的项目",
        "description": "项目描述",
        "objective": "项目目标",
        "owner_id": 1,
        "status": "active",
        "task_count": 5,
        "metadata": {},
        "created_at": "2025-07-01T10:00:00Z",
        "updated_at": "2025-07-02T12:00:00Z"
      }
    ]
  }
}
```

---

### 6.6 `POST /NewMessage` - 新建消息（首页输入）

```
POST /v1/NewMessage
```

**请求参数：**

```json
{
  "content": "帮我审查一下最近的代码",
  "execution_mode": "default",
  "project_id": "proj_abc123",
  "task_id": "task_xxx",
  "assistant_ids": ["assistant_abc123"],
  "message_type": "text",
  "objective": "",
  "attachments": [],
  "metadata": {}
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `content` | `string` | 否 | 消息内容 |
| `execution_mode` | `string` | 否 | `"default"` 或 `"plan"` |
| `project_id` | `string` | 否 | 已有项目的 public_id，空则自动创建新项目 |
| `task_id` | `string` | 否 | 已有任务的 public_id，空则自动创建 |
| `assistant_ids` | `[]string` | 否 | AI 队友的 public_id 列表，空则使用项目默认 AI 队友 |
| `message_type` | `string` | 否 | 消息类型 |
| `objective` | `string` | 否 | 新任务目标（创建新任务时使用） |
| `attachments` | `[]object` | 否 | 附件列表 |
| `metadata` | `object` | 否 | 元数据 |

**响应示例：**

```json
{
  "code": 0,
  "data": {
    "project_id": "proj_abc123",
    "task_id": "task_xxx",
    "session_id": "sess_xxx",
    "message_id": "123",
    "assistant_id": "assistant_abc123"
  }
}
```

---

### 6.7 `POST /CreateSession` - 创建会话

```
POST /v1/CreateSession
```

**请求参数：**

```json
{
  "session_id": "sess_custom_id",
  "type": "chat",
  "assistant_id": "assistant_abc123",
  "title": "自定义标题",
  "expired_at": "2025-12-31T23:59:59Z"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `session_id` | `string` | 否 | 自定义会话 ID，空则自动生成 |
| `type` | `string` | 是 | 会话类型 |
| `assistant_id` | `string` | 否 | AI 队友的 PublicID |
| `title` | `string` | 否 | 会话标题 |
| `expired_at` | `time` | 否 | 过期时间 |

**响应示例：**

```json
{
  "code": 0,
  "data": {
    "session_id": "sess_custom_id",
    "type": "chat",
    "assistant_id": "assistant_abc123",
    "allocated_assistant_id": "assistant_abc123",
    "status": "active",
    "title": "自定义标题",
    "message_count": 0,
    "created_at": "2025-07-01T10:00:00Z",
    "updated_at": "2025-07-01T10:00:00Z"
  }
}
```

---

### 6.8 `POST /SessionEvents` - 会话事件流 (SSE)

```
POST /v1/SessionEvents
```

**请求参数：**

```json
{
  "session_id": "sess_xxx",
  "replay": false,
  "assistant_id": "assistant_abc123"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `session_id` | `string` | 是 | 会话 ID |
| `replay` | `bool` | 否 | 是否回放历史事件 |
| `assistant_id` | `string` | 否 | AI 队友的 PublicID，用于多 AI 场景过滤，空则不过滤 |

**响应：** `text/event-stream` SSE 流，事件格式为 JSON。当 `assistant_id` 非空时，仅下发该 AI 队友的 RunEvent。

---

### 6.9 `POST /GlobalEvents` - 全局事件流 (SSE)

```
POST /v1/GlobalEvents
```

**请求参数：**

```json
{
  "replay_since_seq": 0
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `replay_since_seq` | `uint64` | 否 | 从指定序列号开始回放 |

**响应：** `text/event-stream` SSE 流。

**`message.created` 事件 payload（`MessageCreatedData`）：**

```json
{
  "type": "message.created",
  "project_id": 1,
  "session_id": "sess_xxx",
  "seq": 100,
  "timestamp": 1720000000000,
  "data": {
    "message_id": 123,
    "sequence": 1,
    "sender_type": "assistant",
    "sender_uin": null,
    "sender_name": "代码审查助手",
    "assistant_id": "assistant_abc123",
    "assistant_name": "代码审查助手",
    "content": "",
    "run_id": "run_xxx"
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `sender_type` | `string` | `"human"` 或 `"assistant"` |
| `assistant_id` | `*string` | AI 队友的 PublicID，仅 assistant 事件填充 |
| `assistant_name` | `string` | AI 队友名称，仅 assistant 事件填充 |
| `run_id` | `string` | 运行 ID |

> **用法：** 前端收到 `sender_type=assistant` 且 `content=""` 的消息创建事件后，用 `session_id` + `assistant_id` 订阅 `POST /SessionEvents` 获取流式输出。
