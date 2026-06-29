# Agent 独立执行层架构重整方案

> 状态：目标架构，尚未完成代码迁移  
> 决策日期：2026-06-28  
> 适用范围：`backend/agent`、`backend/engines`、`backend/internal/agent`、`backend/internal/eventengine`、`backend/internal/memory`、`backend/internal/runtime`、`backend/internal/skill`、Worker Run 调用链  
> 协议约束：保持现有 Server/Worker、NATS、SSE 和 HTTP API 兼容

## 1. 结论

当前 Agent Run 已经具备多 Runtime、流式事件、审批提问、Session 恢复、Workspace 和
Skill/Memory 等能力，但执行内核与 SingerOS 业务逻辑没有形成稳定边界。

目标架构采用两层模型：

1. `backend/agent` 是可独立驱动、可复用的 Agent 执行层，只理解执行请求、Runtime、
   Tool、Interaction、执行事件和执行结果。
2. `backend/internal/assistant` 是 SingerOS 业务包装层，负责 Session、Assistant、Actor、
   Memory、Skill、Workspace、Artifact、Git 和业务 Run 状态。

`native`、`claude`、`codex`、`opencode` 是四个完全同级的 Runtime。`native` 只是 Runtime
的一种实现，其内部可以继续使用 Eino；它不拥有更高层的业务特权，也不能自行读取
SingerOS Session、Skill 或 Memory。

目标调用链：

```text
WorkerCommand
    → Command Adapter
    → RunCoordinator
    → assistant.Service
    → assistant.Preparer
    → agent.Executor
    → Runtime Registry
    → native | claude | codex | opencode
    → assistant.Finalizer
    → assistant.EventMapper
    → messaging.RunEvent
    → NATS run.stream / run.state
```

## 2. 当前实现评估

### 2.1 当前实际调用链

当前 Worker 主路径为：

```text
message_poster
    → pkg/messaging.WorkerCommand
    → command.Dispatcher
    → command/run.Handler
    → agent/run.Service
    → RunPreparer
    → legacy RuntimeAdapter
    → externalcli.Runner
    → engines.Engine
    → EngineEvent / runtime event
    → RunJournal
    → NATSEventSink
    → NATS
```

即使是 `native`，也会进入与外部 CLI 相似的 Engine/Runner 包装链。

### 2.2 主要问题

#### PreparedRun 没有成为真实执行边界

当前 `PreparedRun.Spec` 包含 prompt、messages、model、tools、policy 和 max steps，但兼容
RuntimeAdapter 只把 `SystemPrompt` 回填到旧 `RequestContext`。外部 Runtime 随后再次从旧
Request 拼装 prompt、model 和 workspace。

结果是：

- 新旧 Request 模型同时存在。
- PreparedRun 看似不可变，实际 Runtime 仍依赖原始可变 Request。
- Preparer 的输出无法通过测试证明被 Runtime 完整消费。

#### Agent Run 层包含业务逻辑

当前 Preparer 直接依赖：

- modelrouter
- Session context builder
- Skill catalog
- Memory store
- Workspace

当前 Finalizer 直接负责：

- Workspace reconciliation
- Artifact 收集
- `git add`
- `git commit`
- `git push`

这些都属于 SingerOS 业务包装，不属于通用 Agent 执行内核。

#### Runtime 与 Engine 双协议并存

代码中同时存在：

- `Runner` 与 `Runtime`
- `RunHandle` 与 `Execution`
- `RunResult`、`RuntimeResult` 与 `EngineResult`
- `agent.Event`、`runtime/events.Event` 与 `EngineEvent`

部分 Engine 文件仍导入 `internal/runtime/events`，与 Engine 层声明的依赖规则不一致。

#### internal/runtime 成为职责收纳包

`internal/runtime` 当前同时包含：

- 进程级 Runtime 装配
- external CLI 适配
- Event schema
- Session/Skill/Memory context
- MCP HTTP 服务
- Todo tracker

这些模块的生命周期和上层调用者不同，不应由同一个“runtime”概念承载。

#### Worker Handler 与 Coordinator 双轨

`command/run.Handler` 仍负责：

- Wire decode 与 route validate
- Seq tracking
- Semaphore 与 WorkerPool
- Debounce 与 pending waiter
- Workspace 与附件
- Active run 与 cancel
- Agent 调用
- 失败事件补发

新增的 `worker/run.Coordinator` 尚未接入主路径，且 Session 提交路径仍返回空
`RunOutcome`。现状不是职责迁移完成，而是第二套未闭环实现。

#### eventengine 是未接入的旧入口

`internal/eventengine` 仍依赖旧 `agent.Runner`，但没有接入 Server 或 Worker 启动装配。
外部事件不应建立第二条直达 Agent 的执行链，应通过现有业务服务和 WorkerCommand 进入
统一 Run 链路。

#### Memory 与 Skill 依赖全局环境

System prompt 和 Tool 会直接调用默认 Memory/Skill 路径或 package-level 函数，Skill
mutation 还通过全局 callback 同步外部 CLI。这使测试、并发隔离和多 Worker 配置变得困难。

### 2.3 当前验证基线

定向测试显示：

- 新增的 `internal/agent/run` 和 `worker/run.Coordinator` 没有独立测试。
- 原 `command/run/handler_test.go` 被改为 `.skip`。
- System prompt 层级断言失败。
- Todo event metadata 断言失败。
- OpenCode/Skill fetch 的部分测试因沙箱禁止 IPv6 `httptest` 监听失败，不属于本次源码架构问题。

## 3. 目标分层

```text
backend/
├── agent/                              # 可复用 Agent 执行层
│   ├── executor.go                     # Runtime 解析与统一执行生命周期
│   ├── runtime.go                      # Runtime、Resolver、Registry
│   ├── request.go                      # ExecutionRequest
│   ├── result.go                       # ExecutionResult
│   ├── event.go                        # 强类型执行事件
│   ├── tool.go                         # Tool 契约
│   ├── interaction.go                  # Approval / Question 端口
│   └── runtime/
│       ├── native/                     # Eino 实现
│       ├── claude/                     # Claude CLI 实现
│       ├── codex/                      # Codex CLI 实现
│       ├── opencode/                   # OpenCode 实现
│       └── externalcli/                # CLI 进程与恢复会话公共设施
│
├── internal/
│   ├── assistant/                      # SingerOS Agent 业务包装
│   │   ├── service.go                  # 业务 Run 编排
│   │   ├── request.go                  # 业务 RunRequest / RunResult
│   │   ├── preparer.go                 # 执行前业务准备
│   │   ├── finalizer.go                # 执行后业务收尾
│   │   ├── event_mapper.go             # Core event → wire event
│   │   └── ports.go                    # 业务依赖端口
│   ├── memory/                         # Memory 存储实现
│   ├── skill/                          # Skill 领域服务
│   ├── workspace/                      # Workspace 与 Artifact 实现
│   └── worker/
│       ├── command/                    # NATS 入站适配
│       ├── run/                        # 调度、合并、取消
│       └── eventpub/                   # Wire event 发布
│
└── pkg/messaging/                      # Server/Worker wire contract
```

目录数量不是独立目标。只有在职责、生命周期或依赖方向确实不同的情况下才保留子包。
Runtime 的 provider-specific 协议需要独立子包；Preparer 的内部步骤不拆成多个包。

## 4. 独立 Agent 执行层

### 4.1 ExecutionRequest

`ExecutionRequest` 是已经准备完成、可以直接交给 Runtime 的输入。

```go
type ExecutionRequest struct {
    ExecutionID string
    SessionKey  string

    SystemPrompt string
    Prompt       string
    Messages     []Message
    Model        ModelConfig
    Tools        []Tool
    Policy       ExecutionPolicy
    Filesystem   FilesystemContext
}
```

允许包含：

- 通用 execution/session 标识。
- 已生成的 prompt 和消息上下文。
- 已解析的模型设置。
- 已绑定的 Tool。
- 通用执行策略。
- WorkDir 和 Runtime state directory。

禁止包含：

- OrgID、ProjectID、AssistantID、ActorID。
- NATS subject、delivery sequence、reply message ID。
- SingerOS Session DTO。
- Skill、Memory、Artifact 或 Git 业务对象。
- 未解析的任意业务 metadata map。

### 4.2 Runtime

```go
type Runtime interface {
    Name() string
    Execute(
        ctx context.Context,
        request ExecutionRequest,
        observer Observer,
    ) (ExecutionResult, error)
}
```

Runtime 约束：

- 只消费 `ExecutionRequest`。
- 不修改传入 Request。
- 不访问 NATS、数据库或 SingerOS Service。
- 不发布 `run.started/completed/failed/cancelled` 业务事件。
- 通过 Observer 发布 message、reasoning、tool、approval、question 和 provider-session 活动。
- 取消通过 `context.Context` 传播。
- 执行失败返回 error，不能使用 `panic`。

### 4.3 Executor 与 Registry

`agent.Executor` 持有 RuntimeResolver，并完成：

1. 校验执行请求。
2. 根据 Runtime name 解析实现。
3. 生成通用 execution lifecycle。
4. 调用 Runtime。
5. 统一处理取消和 Observer 错误。
6. 返回 ExecutionResult。

Runtime Registry 只保存 `name → Runtime`，不创建业务 Tool，不读取配置文件。Runtime 的
具体构造由 `cmd/leros/worker.go` 完成。

### 4.4 Event

Core Event 使用具名 payload，不复用 wire event：

```text
execution.started
message.delta
reasoning.delta
message.result
tool.started
tool.completed
tool.failed
approval.requested
approval.resolved
question.asked
question.answered
provider_session.started
execution.completed
execution.failed
execution.cancelled
```

Runtime 只能产生中间活动事件；execution lifecycle 由 Executor 产生。业务层根据执行结果
和 Finalizer 结果决定最终 `run.*` 状态。

Observer 返回 error 时终止执行。确实允许失败的 listener 必须由调用方显式包装为
best-effort observer。

### 4.5 Tool 与 Interaction

新 Tool 公共签名禁止使用 `map[string]interface{}`：

```go
type Tool interface {
    Definition() ToolDefinition
    Execute(ctx context.Context, input json.RawMessage) (ToolResult, error)
}
```

复杂 Tool 可以在实现内部把 `json.RawMessage` 解码为具名 request struct。

Approval 和 Question 通过 Runtime 构造参数注入的强类型 `InteractionHandler` 处理。禁止
依赖 package-level `DefaultInteractionRouter`。

## 5. SingerOS 业务包装层

### 5.1 assistant.Service

`assistant.Service.Run` 是一次业务 Agent Run 的唯一编排入口：

```text
Clone and validate domain request
→ emit run.started
→ Preparer.Prepare
→ agent.Executor.Execute
→ Finalizer.FinalizeRequired
→ emit artifact events
→ emit exactly one terminal run event
→ PostRunBestEffort
```

Service 决定：

- completed / failed / cancelled。
- 用户可见 Message。
- 技术 Error。
- Artifact 和业务 metadata。
- `run.started` 与唯一 terminal event。

### 5.2 Preparer

Preparer 通过就近定义的接口注入：

- `ConversationReader`
- `MemoryReader`
- `SkillResolver`
- `ModelResolver`
- `WorkspaceManager`
- `AttachmentIngestor`
- `ToolProvider`

Preparer 负责把 SingerOS 业务 Request 转换为纯 `agent.ExecutionRequest`。它可以读取业务
资源，但 Runtime 不可以。

### 5.3 Finalizer

Finalizer 负责：

- Workspace reconciliation。
- Artifact manifest 收集。
- Git stage/commit/push。
- 业务 RunResult。
- Required 与 best-effort post-run 的语义区分。

Finalizer 失败可以把业务 Run 标记为 failed；metrics、learning、diagnostics 等
best-effort 任务不能修改已经发布的终态。

### 5.4 Content 与 Error

业务结果必须继续区分：

- `Message`：用户可见内容。
- `Error`：诊断信息。

取消时 Message 使用稳定的用户文案“已取消”，底层 `context canceled` 或 provider
错误只进入 Error。

## 6. Memory、Skill 与 Tool

### 6.1 Memory

`internal/memory` 提供实例化 Store，不暴露默认全局路径。业务层负责选择 workspace root
并注入 Store。

Memory 的使用方式分为：

- Preparer 读取持久化 Memory 并生成 prompt context。
- Memory Tool 通过构造函数接收 Store。

两者不能在 Runtime 内部通过全局函数自行查找 Store。

### 6.2 Skill

`internal/skill` 以一个 Service 统一 catalog、store 和 cache 的对外入口；远端 GitHub、
ClawHub、Skills.sh、URL 获取保留为 source adapter。

Preparer 负责：

- 解析用户显式调用的 Skill。
- 读取 Skill 文档。
- 生成 prompt context。

Runtime 只接收最终 prompt、Tool 或外部 CLI 所需的已解析 Skill directory。

### 6.3 Tool

业务 Tool 位于 Agent core 之外，但实现 `agent.Tool`。ToolContext 使用具名结构体，不透传
任意业务 map。Skill mutation、Todo reporter、Artifact declaration 等依赖全部通过构造
函数注入。

## 7. Worker 与事件协议

### 7.1 Command Adapter

`command/run.Handler` 最终只负责：

- Decode WorkerCommand。
- 校验 route 和 wire payload。
- 获取 NATS delivery sequence。
- 映射为 `RunSubmission`。
- 调用 Coordinator。
- 根据 Coordinator 结果更新 delivery 状态。

### 7.2 RunCoordinator

Coordinator 统一拥有：

- Session debounce。
- 并发上限。
- Pending waiter。
- Active run。
- Cancel。
- Graceful close。
- Delivery sequence 聚合。

Coordinator 不理解 Workspace、Model、Runtime、Artifact 或 NATS subject。

### 7.3 Wire Event

现有 `WorkerCommand`、`messaging.RunEvent`、`run.stream`、`run.state`、SSE DTO 和 HTTP API
保持兼容。

Core Event 在 `assistant.EventMapper` 中转换为 `messaging.RunEvent`。Server 和 Worker 共用
的 wire 类型收敛到 `pkg/messaging`，不再放在 `internal/runtime/events`。

## 8. eventengine 处理

删除未接入的 `internal/eventengine`。

GitHub 等外部事件继续先进入 Server 业务服务，由业务服务决定是否构建 WorkerCommand。
禁止保留一条 `interaction event → agent.Runner` 的旁路，否则 Session、权限、Workspace、
调度和事件持久化都会被绕过。

## 9. 迁移计划

### Phase 1：建立新执行契约

- 新建 `backend/agent`。
- 定义 ExecutionRequest、ExecutionResult、Runtime、Observer、Tool、Interaction。
- 建立 Runtime contract suite 和依赖边界测试。
- 暂不改变 NATS 或 API。

### Phase 2：迁移四个 Runtime

- 依次迁移 native、Claude、Codex、OpenCode。
- 每个 Runtime 直接实现同一个 `agent.Runtime`。
- 迁移 provider session、approval、question 和 CLI process 公共设施。
- 合并旧 Runner/Runtime、RunHandle/Execution 和三套 Event。
- 全部 Runtime 通过 contract suite 后删除 compatibility adapter。

### Phase 3：建立 assistant 业务层

- 将 ContextBuilder、model routing、Session history、Memory、Skill、Workspace 和附件迁入
  Preparer。
- 将 Artifact、Git 和业务终态迁入 Finalizer。
- 建立 core event 到 wire event 的唯一 mapper。
- Worker 主路径切换到 assistant.Service。

### Phase 4：收窄 Worker

- 完成 Coordinator 的等待、结果返回、取消和关闭语义。
- 恢复并迁移 Handler 测试。
- Handler 降为 NATS adapter。

### Phase 5：清理旧包

- 删除旧 `backend/engines` 位置。
- 删除 `internal/agent/run` 和 legacy Runner。
- 删除 `internal/runtime` 中已迁移的 service、drivers、events、lifecycle。
- 将 MCP HTTP 服务移动到 Worker infrastructure。
- 将 Todo reporter 移入 Tool/assistant 能力。
- 删除 `internal/eventengine`。
- 删除 `.skip` 测试和所有临时 type alias。

每个 Phase 结束必须保持可构建。兼容层只能单向存在，并在同一迁移序列中明确删除，不能
成为永久第二套架构。

## 10. 测试与验收

### Runtime contract

四个 Runtime 必须通过同一套行为测试：

- 成功、失败、取消。
- Message/reasoning 流。
- Tool started/completed/failed。
- Approval 与 question。
- Provider session resume。
- Observer failure。
- Context cancellation 后不继续产生事件。

### assistant.Service

- 输入 Request 在执行前后不变。
- Preparer 输出被 Runtime 完整消费。
- 每次 Run 恰好一个 started 和一个 terminal event。
- Required finalize 失败能够改变业务状态。
- Best-effort 失败不改变业务状态。
- Message 与 Error 分离。

### Worker

- 同 Session debounce 合并。
- 不同 Session 并行。
- 第一条 delivery 等待合并批次完成。
- Cancel 与完成竞争。
- Seq received/processing/completed/failed 恢复。
- Close 等待 in-flight Run。

### Wire compatibility

- WorkerCommand JSON golden test。
- run.stream/run.state payload golden test。
- SSE replay 和首序号测试。
- Session projector 的 completed/failed/cancelled 持久化测试。

### 架构验收

必须满足：

```text
backend/agent/... 不导入：
  backend/internal/...
  backend/config
  backend/pkg/messaging
  NATS
  Session/API DTO
  Memory/Skill/Workspace 业务包
```

主路径中只能存在：

- 一个 Runtime 接口。
- 一个执行 Event 模型。
- 一个 ExecutionResult。
- 一个业务 terminal event 生产者。
- 一个 core event → wire event mapper。

## 11. 明确不做

- 不拆独立 Go module。
- 不增加服务进程、NATS stream 或 command lane。
- 不改变前端 API/SSE 契约。
- 不引入工作流引擎或通用插件框架。
- 不把 Preparer 每一步拆成独立 package。
- 不把 native 提升为高于 Claude/Codex/OpenCode 的特殊 Runtime。

## 12. 最终决策摘要

| 设计点 | 决策 |
| --- | --- |
| Agent core | 新建顶层可复用 `backend/agent` |
| 业务包装 | 新建 `backend/internal/assistant` |
| Runtime | native / Claude / Codex / OpenCode 完全同级 |
| Native | Eino 仅是 native Runtime 内部实现 |
| Message bus | 保持 WorkerCommand 与双 lane |
| 事件 | Core event 与 wire event 分离并显式映射 |
| Memory/Skill | 由业务 Preparer 注入，不允许 Runtime 直接读取 |
| Workspace/Git | 归属业务 Finalizer |
| eventengine | 删除旁路 |
| 迁移 | 分阶段替换，最终删除全部兼容层 |
