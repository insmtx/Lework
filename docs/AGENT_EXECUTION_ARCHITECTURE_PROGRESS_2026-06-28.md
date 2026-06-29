# Agent 执行架构实施进度（2026-06-28）

## 结论

Agent 架构重整计划已经实施完成。当前唯一业务执行链为：

```text
command/run.Handler
→ worker/run.Coordinator
→ internal/assistant.Service
→ agent.Executor
→ agent.Runtime
   ├─ native.Runtime
   ├─ claude.Runtime
   ├─ codex.Runtime
   └─ opencode.Runtime
```

`backend/agent` 是业务无关的执行层；`internal/assistant` 包裹执行层并独占
Session、Workspace、Memory、Skill、Artifact、Git 和业务 Run 终态。

| 计划阶段 | 状态 | 当前结果 |
|---|---|---|
| P0 主链正确性 | 完成 | Coordinator、消息关联、Workspace、终端归档和取消语义均已落地 |
| P1 独立执行层 | 完成 | 统一 Runtime、ExecutionRequest、ExecutionResult、Event 和 Tool 契约 |
| P2 旧层与全局状态 | 完成 | 旧目录退出代码树，关键 Store、Router、callback、session store 和 MCP token 均改为实例注入 |
| 测试与文档 | 完成 | 核心 contract、主链、双 lane、Workspace 和 projector 测试已补齐，架构索引已更新 |

## 当前层级

### `backend/agent`

只包含可复用执行概念：

- `ExecutionRequest`、`ExecutionResult`。
- `Runtime`、`Registry`、`Executor`。
- `Event`、`Observer`。
- `Tool`、`ToolDefinition`、`ToolResult`。
- `InteractionHandler` 及审批、提问的强类型请求。

递归架构测试禁止该目录导入：

- `backend/internal/*`。
- `backend/config`。
- `backend/pkg/messaging`。
- `backend/tools`。

Runtime 只产生 message、reasoning、tool、todo、artifact、approval、question 和
provider-session 活动。`execution.*` 由 Executor 负责，业务 `run.*` 由
assistant Service 负责。

### `backend/agent/runtime`

- `native` 直接实现 `agent.Runtime`，消费已经准备完成的消息、模型、Tool 和文件系统快照。
- `claude`、`codex`、`opencode` 分别实现 `agent.Runtime`。
- `externalcli.Driver` 只提供三个 CLI Runtime 共用的进程、provider session 和事件解析设施，本身不是 Runtime。
- provider、events、eventpub 和 todo 的迁移期 type alias 已删除，所有层直接使用
  `agent.Event`、`agent.Usage` 或 `json.RawMessage`。
- Tool、approval 和 question 的动态 JSON 边界使用 `json.RawMessage`；业务 Tool 公共接口不再接收 `map[string]interface{}`。

### `backend/internal/assistant`

- `Preparer` 先准备 Workspace，再处理 Session、Skill、prompt、附件、模型和 Tool。
- `PreparedRun` 同时保存业务快照、`WorkspacePreparation` 和纯 `ExecutionRequest`。
- `Service` 记录真实 `StartedAt`，调用 Executor，并保证每次 Run 恰好一个
  completed、failed 或 cancelled 终态。
- `Journal` 归档 event payload、合并序号、usage、tool calls 和统计信息。
- `Finalizer` 只使用同一 prepared snapshot 完成 Artifact、Git 和结果构造。
- 取消时用户内容固定为 `已取消`，技术错误保存在终端 payload 和
  `RunEventBody.Error`。

### Worker 调度和消息映射

`worker/run.Coordinator` 已实现：

- `MaxConcurrency` 信号量。
- 同 Session 串行、不同 Session 并行。
- debounce 批次的全部 waiter 等待同一个真实结果。
- active run 注册、取消和注销。
- Close 拒绝新请求、清理 pending timer，并等待 in-flight submission。
- 合并并去重 delivery sequence 和 `ReplyToMessageIDs`。

`command/run.Handler` 只负责 wire 校验、submission 映射和 delivery 状态更新。
`worker/eventpub.NATSEventSink` 负责 core Event 到 `messaging.RunEvent` 的双 lane
映射；终端发布使用 `context.WithoutCancel` 加超时。

## P0 实施结果

### 消息关联

`TaskInput.Messages[].ID` 被映射到 `RunEventContext.ReplyToMessageIDs`。同 Session
debounce 合并时，消息 ID 和 delivery sequence 均按输入顺序去重。

### Workspace

`WorkspaceManager.PrepareWorkspace` 返回不可变的 `WorkspacePreparation`，包含：

- `WorkDir`。
- `RepoDir`。
- `TaskDir`。
- Artifact manifest 路径。

`PrepareTaskWorkspace` 先 clone/pull 仓库，再创建 turn、tmp、logs 和 manifest，
避免 clone 删除预先创建的运行目录。Preparer、Tool adapter、Runtime 和 Finalizer
使用同一个准备结果。

### 终端语义

completed、failed、cancelled 都携带：

- status、message、error。
- usage 和 tool calls。
- artifact。
- 已归档 activity events 及合并序号。
- started/completed timestamp。
- typed run metadata。

终端 JSON 编码失败会转换为唯一 `run.failed`，不会 panic，也不会产生双终态。

## P1/P2 实施结果

以下旧实现已退出代码树：

- `backend/engines`。
- `backend/internal/runtime`。
- `backend/internal/agent`。
- `backend/internal/eventengine`。
- simplechat driver。

以下 package-level 可变状态已删除或改为实例注入：

- Memory default store。
- Model Router default store。
- Skill mutation callback。
- Interaction default router。
- provider session default store。
- MCP auth token。

Skill link 同步属于 `internal/assistant/bootstrap/skilllinks`；MCP 属于
`internal/worker/mcp`；Todo reporter 属于 `agent/runtime/todo`。

## 测试覆盖

新增或恢复的关键测试：

- Executor：默认 Runtime、生命周期、取消、observer error。
- Runtime：四种具体 Runtime 的公共契约及各 provider 协议测试。
- assistant：输入不可变、单一终态、取消 Content/Error、完整 Journal、Artifact、
  时间戳、终端编码失败。
- Preparer/Workspace：单一 prepared snapshot、Skill prompt、Tool 注入、clone/pull、
  turn 目录和 manifest。
- Coordinator：全部 waiter、并发上限、Session 串行、跨 Session 并行、取消和 Close。
- Handler：真实批次等待、delivery sequence 完成时机和 reply message IDs。
- NATS sink：stream/state 双 lane、Raw tool payload、失败和取消完整终端归档。
- Server projector：completed、failed、cancelled、usage、artifact、events 和取消类型。
- 架构测试：`backend/agent` 递归依赖边界、唯一公共契约、无兼容 type alias。
- 静态审计：旧目录无文件、无生产 `panic`、无 `.skip` 测试文件。

最终验证：

```text
go test ./... -count=1 -timeout=120s
```

全仓库通过。ModelRouter 与 ClawHub 测试使用注入式 `http.Client/RoundTripper`，
不再依赖本地监听权限；Gitea 真实服务测试使用 `integration` build tag。

## 维护约束

后续改动必须保持：

1. 业务数据只在 `internal/assistant` 及外层出现。
2. Runtime 不访问 NATS、Session DB、Skill catalog、Memory store 或 Git。
3. 新 Runtime 直接实现 `agent.Runtime` 并通过公共 contract suite。
4. 新 Tool 使用 `json.RawMessage` 或具名输入，不新增跨层
   `map[string]interface{}`。
5. 外部事件转换为 WorkerCommand 后进入唯一主链，不恢复旁路执行。
6. `WorkerCommand`、`RunEvent`、SSE 和 API 的 JSON 形状保持兼容。
