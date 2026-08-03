# AI 对话前端流程（重构用）

> 只站在前端视角：页面怎么发、store 怎么走、拉了哪些接口、消息气泡怎么变。  
> 不展开 Worker / NATS / projector 等前端无感知细节。

核心代码：

- 输入分支：`frontend/packages/app-ui/components/input/ChatInput.tsx`
- Facade：`frontend/packages/store/slices/chatSlice.ts`（装配模块 + 兼容旧 API）
- `frontend/packages/store/chat/` 分层：
  - `state.ts` — `ChatState`
  - `messageReducer.ts` — SessionEvents → Message、`mapBackendMessage`
  - `messageMerge.ts` — 乐观 merge / 占位 / GlobalEvents 用户消息
  - `sessionStream.ts` — SessionEvents 短连接
  - `globalEvents.ts` — GlobalEvents 长连接 + message.created
  - `historyLoader.ts` — 进页 hydration / resume / poll
  - `send/*` — 三条发送 + bootstrap / optimistic
  - `effects.ts` — conversations / taskDetail / fetchProjectDetail
  - `composer.ts` — 输入草稿 / 附件上传 / skill 指令
- 开流规则：
  - 问答（CreateInitialMessage / AddMessage）→ 仅 GlobalEvents assistant 后开 SessionEvents
  - 仅进任务 → GetSessionMessages + responding 时 resume SessionEvents（不靠 GE）
- API：`frontend/packages/store/api/sessionApi.ts`
- 类型：`frontend/packages/store/types/chat.ts`
- 渲染：`frontend/packages/app-ui/components/chat/*`

---

## 1. 前端眼里的系统

对话前端只干三件事：

1. **写消息**：调 HTTP，把用户内容交给后端
2. **听推送**：最多两条 SSE —— `GlobalEvents`（全局通知）+ `SessionEvents`（当前会话流）
3. **渲染 Message**：本地乐观插入 → 用事件/历史接口把气泡补全、替换、收尾

```text
ChatInput
  ├─ 纯 chat 页        → sendMessage
  ├─ 项目首页          → sendProjectMessage
  └─ 任务详情（群聊）  → sendTaskRoomMessage
         │
         ▼
   chatSlice（messagesMap / isGenerating / SSE）
         │
         ├─ sessionApi.*（HTTP）
         ├─ GlobalEvents SSE（可选）
         └─ SessionEvents SSE（流式正文）
         │
         ▼
   MessageTimeline / AIMessageBubble / UserMessageBubble
```

---

## 2. 发送入口怎么分支

`ChatInput` 提交时按「当前视图」分流（`isProjectVariant` + `currentView`）：

| 条件 | 调的 store 方法 | 产品场景 |
|---|---|---|
| 项目变体 + `taskDetail` + 已有 project/task/session | `sendTaskRoomMessage` | 任务详情里继续聊 |
| 项目变体 + `project` | `sendProjectMessage` | 项目首页发首条，创建任务并跳转 |
| 其它（含纯 `/chat`） | `sendMessage` | 独立会话聊天 |

共同约束：`isGenerating === true` 时禁止再发。

---

## 3. 三条发送路径（前端逐步）

### 路径 A：`sendMessage`（纯会话）

```text
校验 content
  → 无 activeSessionId？ CreateSession
  → AddMessage
  → 本地插入 user + 空 assistant（乐观 ID）
  → isGenerating=true, streamingMessageId=assistantId
  → 立刻开 SessionEvents（replay=false）
  → 收流直到 run.completed|failed|cancelled
  → finishStream + GetSessionMessages 对齐
```

特点：

- **不依赖 GlobalEvents**
- 发完立刻开流，路径最短
- 乐观 assistant id：`msg-assistant-{ts}`

### 路径 B：`sendProjectMessage`（项目首页新建任务）

```text
确保 GlobalEvents 已连接
  → CreateInitialMessage（带回 project/task/session）
  → bootstrapNewTaskSession：
       乐观 user（sending）+ waiting 占位 assistant
       pendingBootstrapSessionId = sessionId
       isGenerating=true
  → 跳转任务详情（layout / navigation）
  → 等 GlobalEvents message.created(assistant)
       替换 waiting 占位，写入 runId
  → GetSession：runtime_status === responding？
       是 → SessionEvents(replay=true)
       否 → 直接 GetSessionMessages，结束 generating
```

特点：

- 创建与跳转、开流是拆开的
- 开流顺序硬性依赖：**GlobalEvents → SessionEvents**
- `pendingBootstrapSessionId` 会挡住过早的 `loadConversationMessages`，避免空屏/冲掉乐观消息

### 路径 C：`sendTaskRoomMessage`（任务详情跟聊）

```text
本地先插 user(sending) + waiting assistant
  → isGenerating=true
  → 确保 GlobalEvents
  → AddMessage
  → 成功后仍保持 waiting，等 GlobalEvents
  → 同时启动 runtime_status 轮询兜底（#startTaskRoomAssistantFallback）
  → GlobalEvents assistant 到达后同路径 B：替换占位 → SessionEvents(replay)
```

特点：

- 和路径 A 一样调 `AddMessage`，但**故意不开立刻 SSE**
- 必须等 GlobalEvents 才知道「哪个 assistant / 哪个 run」接了单
- AddMessage 失败：waiting 气泡标 failed，finishStream

---

## 4. 前端实际调用的接口

| 时机 | API | 谁调用 |
|---|---|---|
| 无会话时创建 | `POST /CreateSession` | `sendMessage` |
| 项目首页首条 | `POST /CreateInitialMessage` | `sendProjectMessage` |
| 已有会话发消息 | `POST /AddMessage` | `sendMessage` / `sendTaskRoomMessage` |
| 拉历史 | `POST /GetSessionMessages` | `loadConversationMessages`；流结束后也会再拉 |
| 看是否还在生成 | `POST /GetSession`（用 `runtime_status`） | 加载/回放/兜底轮询 |
| 会话流式 | `POST /SessionEvents`（SSE） | `#startSSE` |
| 全局通知 | `POST /GlobalEvents`（SSE） | `startGlobalEvents` |
| 停止生成 | `POST /CancelSessionRun` | `cancelGeneration` |
| 审批 | `POST /sessions/:id/approvals` | `submitApprovalDecision` |
| 问答 | 同上 | `submitQuestionAnswer` |

SSE 实现：`FetchSSEClient`（POST + Bearer），不是浏览器 `EventSource`。

---

## 5. 本地状态（重构时要动的面）

### 5.1 ChatState 关键字段

| 字段 | 作用 |
|---|---|
| `messagesMap` + `messageIds` | 当前时间线条目 |
| `activeSessionId` | 当前会话 |
| `isGenerating` | 锁发送、驱动输入区禁用 |
| `streamingMessageId` | 当前正在写入的 assistant 气泡 |
| `pendingBootstrapSessionId` | 新建任务跳转保护窗 |
| `cancellingSessionId` | 取消中，等终态事件 |
| `executionMode` | `default` / `plan`，随发送带给后端 |
| `inputText` / `inputAttachments` | 输入区 |

私有连接态（非 store 公开字段）：`#sseClient`、`#globalEventsClient`、`#sseAssistantMsgId`、pending GlobalEvents 缓冲。

### 5.2 Message 前端状态

`MessageStatus`：`sending` | `waiting` | `streaming` | `completed` | `failed`

常见 ID 形态：

| 前缀 | 含义 |
|---|---|
| `msg-user-{ts}` | 乐观用户消息 |
| `msg-assistant-{ts}` | 纯 chat 乐观 assistant |
| `msg-assistant-waiting-{ts}` | 等 GlobalEvents 的占位 |
| `msg-assistant-resume-{ts}` | 进页回放占位 |
| `msg-assistant-poll-{ts}` | 等 runtime_status 的临时占位 |
| 数字字符串 | `GetSessionMessages` / GlobalEvents 落库 id |

同一条逻辑回复可能经历：**waiting 占位 → GlobalEvents 换成新 id → 流结束后又被历史消息 merge**。这是重构时最大的坑之一。

### 5.3 Message 上承载的流式结构

事件往这些字段里堆：

- `content` / `processSteps`（thinking + tool）
- `toolCalls` / `todos` / `artifacts`
- `approvals` / `questions`
- `runId` / `author` / `replyTo` / `usage`

归并函数：`applySessionEventToMessage`（单事件）、`applySessionEventsToMessage`（历史 chunks）。

---

## 6. 两条 SSE，前端怎么用

### 6.1 GlobalEvents（长连接，进程级）

- Shell / 工作台 / 发项目消息时 `startGlobalEvents()`，已有连接则复用
- 前端关心的 type：
  - `message.created` + `sender_type=human` → 合并/替换乐观用户消息
  - `message.created` + `sender_type=assistant` → 替换 waiting，带上 `runId` / assistant 信息，再决定是否开 SessionEvents
  - `work.title.updated` → 转给 layout 改标题

非当前 session 的 `message.created` 会先缓冲（约 2 分钟），bootstrap 时 `#drainPendingGlobalEvents` 再放出来。

### 6.2 SessionEvents（单会话短连接）

`#startSSE(sessionId, assistantMsgId, replay?, assistantId?)`：

1. 关掉旧连接
2. POST body：`{ session_id, replay?, assistant_id? }`
3. `onMessage` → `applySessionEventToMessage` 更新 `streamingMessageId` 对应消息
4. 收到 `run.completed` / `run.failed` / `run.cancelled` → close、`finishStream`、再拉历史
5. 出错或 idle 超时 → 关流 / 拉历史兜底

前端处理的主要 event type：

| type | UI 效果 |
|---|---|
| `message.delta` / `message.result` | 拼正文 |
| `reasoning.delta` | thinking 步骤 |
| `tool_call.*` | 工具块 |
| `todo.snapshot` / `todo.updated` | todo |
| `artifact.declared` | 产物 |
| `approval.requested` / `question.asked` | 审批 / 问答 UI |
| `plan.published` | plan |
| `work.title.updated` | 标题（SessionEvents 里也会出现） |
| `run.completed` / `failed` / `cancelled` | 结束生成态 |

---

## 7. 进页 / 切会话：加载与回放

### 纯 chat：`CenterCanvas`

`activeSessionId` 变化 → `loadConversationMessages(sessionId)`（默认允许 resume）。

### 任务详情：`TaskDetailPage`

- 有 `pendingBootstrapSessionId` 且本地已有消息 → **不 resume**，等 GlobalEvents
- 否则 `loadConversationMessages(sessionId, { resumeStream: ... })`
- 离开页：清消息、关 SessionEvents（bootstrap 期间会保留等待态）

### `loadConversationMessages` 前端逻辑摘要

```text
可选 GetSession 看 runtime_status
  → GetSessionMessages（分页拉全）
  → 若仍 pendingBootstrap → 直接 return（不覆盖乐观消息）
  → merge 本地 optimistic 与落库消息
  → runtime_status === responding 且当前没在流？
       → 插 resume 占位 + SessionEvents(replay)
  → 末条是 user 且还没 responding？
       → 插 poll 占位，轮询 GetSession
            responding → 换成 resume 占位开 SSE
            已出新消息 → 再 load 一次
```

---

## 8. 其它前端动作

| 动作 | 行为 |
|---|---|
| `cancelGeneration` | 标 `cancellingSessionId`，调 `CancelSessionRun`；**保持** generating + SSE，直到 `run.cancelled`；waiting 且无 `runId` 时直接 return |
| `resendMessage` | 仅对 assistant：本地再插空 assistant，立刻 `#startSSE`（**不再发 AddMessage**，偏「重连收流」） |
| `submitApprovalDecision` / `submitQuestionAnswer` | 先改本地 submitting，再打 approvals 接口 |
| `resetLocalMessages` | 关 SSE，清空 map / generating |

---

## 9. 三条路径对照（重构对照表）

| | A 纯 chat | B 新建任务 | C 任务群聊 |
|---|---|---|---|
| 写接口 | CreateSession? + AddMessage | CreateInitialMessage | AddMessage |
| 乐观 UI | user + 空 assistant | user + **waiting** | user + **waiting** |
| GlobalEvents | 不需要 | 需要 | 需要 |
| 开 SessionEvents 时机 | 发送成功立刻 | GlobalEvents assistant 之后 | 同左 |
| replay | 否（新流） | 是 | 是 |
| 跳转 | 无 | 去 taskDetail | 已在 taskDetail |
| 兜底 | SSE idle / 终态拉历史 | runtime 轮询 + bootstrap 缓冲 | 同左 + taskRoom fallback |

---

## 10. 渲染层（薄）

- `MessageTimeline`：按 `messageIds` 排气泡
- `UserMessageBubble` / `AIMessageBubble`：读 Message 字段；失败可点重发
- `ToolCallBlock` / 审批 / 问答：绑在 assistant Message 上的子结构
- `resolveAssistantMessageDisplay`：展示态整理（含 composer token 等）

渲染层基本不发请求，状态全吃 store。

---

## 11. 重构时建议优先拆的边界

当前乱点几乎都在 `chatSlice`（约 3k+ 行）。按前端职责可拆：

1. **send pipelines**  
   `sendMessage` / `sendProjectMessage` / `sendTaskRoomMessage` / `bootstrapNewTaskSession`  
   → 只负责「调哪个写接口 + 插什么乐观消息」

2. **sessionStream**  
   `#startSSE` / idle fallback / finish / cancel 与 SessionEvents 事件应用

3. **globalEvents**  
   连接生命周期、缓冲、human/assistant `message.created`、触发开流

4. **historyLoader**  
   `loadConversationMessages`、optimistic merge、resume/poll 占位

5. **messageReducer**  
   `applySessionEventToMessage`、approval/question 状态更新（纯函数，最好可单测）

目标行为（重构验收）：

- 三条发送路径对外产品行为不变
- `isGenerating` 锁发送语义不变
- waiting → streaming → completed 的 UI 过渡不变
- 进页回放 / bootstrap 不丢首条乐观消息

不必先改后端协议；先把「谁决定开哪条 SSE、谁改 Message」拆清即可。
