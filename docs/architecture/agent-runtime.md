# Agent Runtime 架构

> 状态：当前架构设计文档
>
> 更新时间：2026-07-05
>
> 本文件描述当前目标服务端架构与已落地代码边界。架构调整的设计决策记录见 [Agent Runtime 架构调整](../design/2026-07-04-agent-runtime-architecture-refactor.md)，实施迁移记录见 [Agent Runtime 实施开发方案](../design/2026-07-04-agent-runtime-implementation-plan.md)。

## 1. 架构概述

Agent 层是独立运行层，只负责执行契约、Runtime 注册、Runtime 调用、工具接口、交互接口和强类型 `NodeEvent`。它不拥有 SingerOS 业务 Run 生命周期，不发布 NATS，不分配业务 Seq，不读取业务 Session，也不上传 Plan 或 Artifact。

Worker 层拥有业务 Run 生命周期。Worker 接收 `WorkerCommand`，准备业务输入，调用 Agent Runtime，消费 Runtime `NodeEvent`，再映射为统一的 `messaging.RunEvent`。Server/UI 只消费 `messaging.RunEvent`。Live 和 Replay 以同一业务事件契约为基础，并尽量复用投影逻辑；当前双 lane 回放仍在演进中。

生产调用链：

```text
WorkerCommand
  → worker/command/run.Handler
  → worker/run.Coordinator
  → worker/agentrun.Service
  → agentrun.Preparer
  → agent.Executor
  → agent.Runtime
  → agentrun.Finalizer
  → agentrun.Journal
  → worker/eventpub.NATSEventPublisher
  → Server Projector
```

事件链路：

```text
Runtime Adapter
  → agent.NodeEvent
  → agent.SerialObserver
  → worker/agentrun.NodeHandler
  → messaging.RunEventBody
  → agentrun.Journal 分配 Seq 并归档
  → messaging.RunEvent
  → worker/eventpub.NATSEventPublisher
  → NATS run.stream / run.state
  → Server Projector / SSE Replay
```

## 2. 目标目录结构

```text
backend/
├── agent/
│   ├── runtime.go            # Runtime、ExecutionRequest
│   ├── executor.go           # Runtime 调用、请求校验、SerialObserver 包装
│   ├── registry.go           # Runtime 注册与默认 Runtime 选择
│   ├── node_event.go         # NodeEvent、NodeEventType、强类型 Payload、NodeObserver
│   ├── observe.go            # SerialObserver
│   ├── adapter.go            # RuntimeAdapterOptions、MCPServerConfig
│   ├── interaction.go        # Approval/Question 交互端口
│   ├── todo.go               # TodoReporter 上下文端口
│   ├── tool.go               # Tool 契约
│   ├── result.go             # ExecutionResult、Usage、ToolCallRecord
│   └── runtime/
│       ├── native/           # 内建 Runtime Adapter；Eino 只作为内部实现细节
│       ├── claude/           # Claude Code Runtime Adapter
│       ├── codex/            # Codex Runtime Adapter
│       ├── opencode/         # OpenCode Runtime Adapter
│       └── internal/
│           ├── cli/          # CLI Driver、Invocation、公开 NodeEvent 与内部控制状态分离
│           ├── process/      # 进程、环境变量、MCP 注册、工作目录公共机制
│           └── todo/         # Runtime 内部 Todo tracker
│
├── internal/
│   ├── worker/
│   │   ├── app/              # Worker composition root，装配 Runtime 与 AgentRun Service
│   │   ├── agentrun/         # 业务 Run 编排、NodeEvent 映射、Journal、Plan、Session
│   │   │   ├── domain/       # RunRequest、RunResult、业务上下文快照
│   │   │   └── context/      # ContextBuilder、历史消息、Skill 注入、SystemPrompt
│   │   ├── command/run/      # WorkerCommand → RunRequest
│   │   ├── run/              # Coordinator：并发、合并、取消、active run tracking
│   │   ├── eventpub/         # 完整 messaging.RunEvent → NATS subject/lane publish
│   │   └── runtimehost/      # Worker 对 Runtime ports 的实现，如 SQLite Provider SessionStore
│   ├── service/              # Server 业务服务与 RunEvent 投影
│   ├── runnable/             # Server replay/state projector
│   └── api/                  # HTTP/SSE contract 与 handler
│
└── pkg/
    └── messaging/            # Worker 与 Server 的唯一业务事件 wire contract
```

已删除或不再作为生产依赖的旧层级：

- `backend/agent/event.go`、`agent.Event/EventSink/EventType`。
- `backend/agent/runtime/events`。
- `backend/agent/runtime/externalcli`。
- `backend/agent/runtime/provider`。
- `backend/agent/runtime/todo` 兼容层。

## 3. 分层职责

### agent

稳定执行契约层。定义 `Runtime`、`ExecutionRequest`、`ExecutionResult`、`NodeEvent`、`NodeObserver`、`Tool`、`InteractionHandler`、`RuntimeAdapterOptions`。

禁止依赖：

```text
backend/agent/**
  ✕ backend/internal/**
  ✕ backend/config
  ✕ backend/pkg/messaging
  ✕ backend/tools
  ✕ backend/pkg/leros
```

### agent/runtime/*

同级 Runtime Adapter。Native、Claude、Codex、OpenCode 都实现同一个 `agent.Runtime`，都使用同一个 `NodeObserver`，都返回同一个 `ExecutionResult`。

Native 是架构角色，表示内建 Runtime；Eino 只能是 `native` 内部实现细节，不作为公共架构命名。Native 不使用 `MaxSteps` 公共契约。

### agent/runtime/internal/*

Runtime 共享机制。`cli` 负责通用 CLI invocation 控制，`process` 负责子进程和环境构造，`todo` 负责 Runtime 内部 Todo tracker。共享层不堆积 Provider 特例，Provider 原始事件翻译保留在具体 Adapter。

### worker/agentrun

业务 Run 生命周期所有者。负责：

- `RunRequest` 克隆和准备。
- Workspace、Context、Tool、Model、Policy 快照构造。
- 调用 `agent.Executor`。
- 消费 `NodeEvent` 并映射为 `messaging.RunEventBody`。
- Provider Session 持久化。
- OpenCode `plan.ready` → PlanPublisher → `plan.published`。
- Journal Seq 分配、归档和终态唯一性。

### worker/eventpub

只发布完整 `messaging.RunEvent`。它只理解 messaging lane 和 NATS subject，不理解 Agent、NodeEvent、AgentRun domain。

### Server/API/Projector

Server 只消费 `messaging.RunEvent`。Live 与 Replay 以统一业务事件契约为基础，并复用公共投影语义；当前 SSE replay 主要覆盖 `run.stream`，`run.state` + `run.stream` 双 lane 回放仍在演进中。SSE contract 使用 Server 自己的 `SessionEvent`，不依赖 Agent runtime events。

## 4. 核心执行契约

```text
RunRequest
  → PreparedRun
  → ExecutionRequest
  → Runtime.Execute
  → ExecutionResult
  → RunResult
```

- `RunRequest`：Worker 业务输入快照，包含 Run/Trace/Task、Assistant、Actor、Conversation、Workspace、Runtime kind、Model 配置、Policy。Service 会先 clone，不允许 Preparer 或 Runtime 回写原始请求。
- `PreparedRun`：业务输入准备后的不可变执行快照，包含 Workspace/Context/Tool/Provider session 等派生信息。
- `ExecutionRequest`：Agent Runtime 唯一输入，包含 prompt、messages、model、tool、policy、filesystem、provider session。Runtime 执行需要 API Key，因此 API Key 可以在此执行快照中出现；但禁止进入 NodeEvent、RunEvent、Journal、NATS、日志或错误文本。
- `ExecutionResult`：Runtime 执行事实，包含最终消息、Usage、ToolCalls、Provider Session ID 等，不包含业务 RunStatus、Artifact 上传结果或 NATS 信息。
- `RunResult`：Worker Finalizer 后的业务结果，包含 completed/failed/cancelled 语义和业务产物。

## 5. Runtime NodeEvent 契约

`NodeEvent` 是 Agent Runtime 层唯一公开事件契约。Runtime Adapter 按能力发送，不伪造 Provider 不具备的阶段；Provider 原始事件只允许作为脱敏调试 metadata 保留。

当前公开 NodeEvent 类型：

```text
message.start
message.update
message.end
reasoning.update
tool_execution.start
tool_execution.update
tool_execution.end
todo.snapshot
todo.updated
approval.requested
approval.resolved
question.asked
question.answered
agent.start
agent.end
plan.ready
```

事件所有权：

| 层级 | 所有事件 | 说明 |
|---|---|---|
| Runtime Adapter | message、reasoning、tool_execution、todo、approval、question、agent、plan.ready | 执行节点事实 |
| Agent Executor | 无业务事件 | 只校验、选择 Runtime、串行化 Observer |
| Worker AgentRun | run.*、artifact、plan.published、metadata | 业务 Run 生命周期和业务事实 |
| Journal | Seq、归档、发布顺序 | Seq 分配和发布顺序必须一致 |
| Server | Session、Project、Work 投影事件 | 面向 UI 的消费视图 |

CLI Driver 的进程完成、失败、取消只是内部控制状态，不向 `NodeObserver` 发送公开 NodeEvent。

## 6. 选择性外发策略

进入 NATS/UI 的事件由 Worker `NodeHandler` 和业务服务选择性映射：

- message、reasoning、tool、todo。
- approval、question。
- artifact、plan.published。
- run 生命周期。

当前 `NodeEvent` 到 `messaging.RunEvent` 的主要映射：

| Runtime `NodeEvent` | 对外 `messaging.RunEvent` | 说明 |
|---|---|---|
| `message.update` | `message.delta` | 助手文本增量。 |
| `message.end` | `message.completed` | 最终助手消息和 usage。 |
| `reasoning.update` | `reasoning.delta` | Runtime 推理内容更新；对外仍以增量事件推送。 |
| `tool_execution.start` | `tool_call.started` | 工具调用开始。 |
| `tool_execution.end` | `tool_call.finished` | 工具调用结束；`IsError=true` 表示失败。 |
| `todo.snapshot` | `todo.snapshot` | Runtime todo 全量快照。 |
| `todo.updated` | `todo.updated` | Runtime todo 更新。 |
| `approval.requested` | `approval.requested` | 请求用户审批。 |
| `approval.resolved` | `approval.resolved` | 审批结果。 |
| `question.asked` | `question.asked` | Runtime 提问。 |
| `question.answered` | `question.answered` | 用户回答。 |
| `plan.ready` | `plan.published` | Worker PlanPublisher 读取、校验、上传后发布业务事件。 |

默认内部事件不扩大 wire contract：

- Runtime/Driver 控制状态。
- `message.start`、`tool_execution.update`、`agent.end` 等内部观测事件。
- `agent.start` 仅用于 Worker 持久化 Provider Session，不映射为 `messaging.RunEvent`。
- `plan.ready` 原始 Runtime 事件不直接外发；只外发 Worker 处理后的 `plan.published`。
- 当前没有独立的 `tool_call.failed` 或 `tool_call.completed` wire event；工具失败通过 `tool_call.finished` 的 `IsError=true` 表达。
- 未来 execution/turn 类内部观测事件。

## 7. 凭据、Session 与 Plan

- API Key：可以进入 `ExecutionRequest.Model` 供 Runtime 执行使用；禁止进入 NodeEvent、RunEvent、Journal、NATS、日志和错误文本。
- Provider Session：Runtime 通过 `agent.start` 的 `AgentStartedPayload.ProviderSessionID` 暴露 Provider Session ID；Worker `ProviderSessionStore` 负责持久化；SQLite 实现在 `worker/runtimehost`。该信息不直接映射为 `messaging.RunEvent`。
- OpenCode Plan：OpenCode Adapter 只发 `plan.ready` 路径事实；Worker PlanPublisher 读取、校验、上传并发布业务事件 `plan.published`。
- 外部 CLI 数据目录：由 Worker app 按 runtime kind 派生为 `workspace_root/.{runtime}` 并注入 Runtime；当前 OpenCode 使用该路径持久化会话数据库，不使用全局 setter 或运行时回调解析。

## 8. 架构守卫与验证

当前守卫覆盖：

- `backend/agent/**` 禁止 SingerOS 业务依赖和旧兼容类型别名。
- `worker/eventpub` 禁止依赖 Agent、NodeEvent、AgentRun domain。
- Server projection 层禁止依赖 `agent/runtime/*` 和 `worker/agentrun/domain`。

建议提交前验证：

```bash
go test ./backend/agent/...
go test ./backend/internal/worker/... ./backend/internal/service/... ./backend/internal/runnable/... ./backend/internal/api/... ./backend/tools/todo/...
go vet ./backend/agent/... ./backend/internal/worker/... ./backend/internal/service/... ./backend/internal/runnable/... ./backend/internal/api/...
git diff --check
```

## 9. 验收标准

- 四个 Runtime 目录和接口同级：Native、Claude、Codex、OpenCode。
- `backend/agent` 无 SingerOS 业务依赖。
- 四个 Runtime 不生成 `agent.Event`，只输出强类型 `NodeEvent`。
- Worker/AgentRun 是 NodeEvent 到业务 RunEvent 的唯一映射点。
- EventPub 只发布完整 `messaging.RunEvent`。
- Server Live/Replay 只消费 `messaging.RunEvent`；公共投影语义保持一致，双 lane 回放能力按当前实现逐步完善。
- Seq 分配与发布顺序一致。
- Native 不暴露 Eino，不使用 MaxSteps 公共契约。
- OpenCode 不承担业务上传，只发 `plan.ready`。
- API Key 不进入持久化 inbox、事件、Journal、NATS 或日志。
- 目标测试、race、vet 和架构守卫全部通过。
