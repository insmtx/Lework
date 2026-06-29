# Agent 执行架构迁移进度评估与后续计划

> 历史快照：本文记录迁移中期状态，已于 2026-06-28 被
> [AGENT_EXECUTION_ARCHITECTURE_PROGRESS_2026-06-28.md](./AGENT_EXECUTION_ARCHITECTURE_PROGRESS_2026-06-28.md)
> 取代。请勿再将本文中的旧路径、完成度或“Coordinator 未接入”等结论视为当前事实。

> 评估日期：2026-06-28  
> 评估对象：当前工作区（包含 staged 与 unstaged 修改）  
> 目标方案：[AGENT_EXECUTION_ARCHITECTURE_REFACTOR_PLAN.md](./AGENT_EXECUTION_ARCHITECTURE_REFACTOR_PLAN.md)  
> 结论：迁移已启动，但仍处于新旧架构并存的中间态，不满足目标架构验收条件

## 1. 总体结论

当前代码**有按计划方向改动**，主要体现在：

- 建立了顶层 `backend/agent`。
- 建立了 `backend/internal/assistant`。
- external CLI Runner 开始直接实现 `agent.Runtime`。
- Preparer 和 Finalizer 已移动到 assistant 包。
- 增加了 `agent.Event → messaging.RunEvent` 的 NATS Sink 映射。
- `internal/eventengine` 已从工作区删除。
- Worker Coordinator 补充了 waiter 和 in-flight 等基础状态。

但这些改动大多仍是**目录迁移、接口过渡和兼容接线**，主调用链仍然是：

```text
command/run.Handler
    → internal/agent/run.Service
    → internal/assistant.Preparer
    → internal/runtime externalcli.Runner
    → backend/engines Engine
    → internal/runtime/events
    → worker/eventpub.NATSEventSink
```

目标调用链尚未形成：

```text
Command Adapter
    → RunCoordinator
    → assistant.Service
    → agent.Executor
    → agent.Runtime
    → assistant.Finalizer / EventMapper
```

从架构行为而不是文件数量判断，当前完成度约为 **25%**。现阶段不能删除兼容代码，也不适合继续叠加更多业务功能，应先修复主链正确性，再完成边界收敛。

## 2. 按目标 Phase 的完成情况

| 目标 Phase | 状态 | 当前结果 |
| --- | --- | --- |
| Phase 1：独立执行契约 | 部分完成 | 有 `backend/agent` 和 Runtime 接口，但没有 Executor、Registry、Tool、Interaction、强类型 ExecutionRequest |
| Phase 2：四个 Runtime | 初步完成 | externalcli 统一包装四个 Engine 并实现 Runtime；四个实现尚未直接进入目标 Runtime 目录 |
| Phase 3：assistant 业务层 | 部分完成 | Preparer/Finalizer 已移动；Service、ports、EventMapper 和业务 Request/Result 尚未建立 |
| Phase 4：收窄 Worker | 骨架阶段 | Coordinator 有实现但未接入；Handler 仍拥有全部旧职责 |
| Phase 5：旧包清理 | 初步开始 | eventengine、simplechat 开始删除；engines、internal/runtime、internal/agent/run 仍是主路径 |

## 3. 已完成或基本正确的部分

### 3.1 顶层 agent 包已建立

当前新增：

```text
backend/agent/
├── event_sink.go
├── prepared_run.go
├── request.go
├── result.go
└── runtime.go
```

该包当前只导入 Go 标准库，已经做到**代码 import 方向上**不依赖 `backend/internal/*`。
这是正确的第一步。

### 3.2 Runtime 接口已进入顶层包

`agent.Runtime` 和 `RuntimeResolver` 已建立，`internal/runtime/service.go` 的 registry 也已经从：

```text
map[string]agent.Runner
```

调整为：

```text
map[string]agent.Runtime
```

旧的额外 RuntimeAdapter 已不再出现在装配主路径中。

### 3.3 externalcli 已直接实现 Runtime

`internal/runtime/drivers/externalcli.Runner` 已实现：

```go
var _ agent.Runtime = (*Runner)(nil)
```

并开始读取：

- `PreparedRun.Spec.SystemPrompt`
- `PreparedRun.Spec.Prompt`
- `PreparedRun.Spec.Messages`
- `PreparedRun.Spec.Model`
- `PreparedRun.Spec.PermissionMode`
- `PreparedRun.Spec.AllowedTools`
- `PreparedRun.Workspace`

相对于上一版只透传 SystemPrompt，这是实质进展。

### 3.4 业务 Preparer 和 Finalizer 已移动

以下实现已移动到 `internal/assistant`：

- `preparer_impl.go`
- `finalizer_impl.go`

Workspace reconciliation、Artifact 收集和 Git 操作不再直接实现在
`internal/agent/run`，方向符合目标方案。

### 3.5 Core event 到 wire event 的映射已经出现

`internal/worker/eventpub/NATSEventSink` 已经显式执行：

```text
agent.Event → messaging.RunEvent
```

并负责：

- event type 映射。
- payload 映射。
- run.stream / run.state 分类。
- 终端事件使用脱离取消信号的发布 context。

虽然该映射最终应与 NATS Publisher 解耦，但功能方向正确。

### 3.6 eventengine 已删除

当前工作区已删除 `backend/internal/eventengine` 下的实现和测试，消除了第二条
`interaction event → agent.Runner` 旁路。

### 3.7 Worker mapper 有最小测试

`command/run/mapper_test.go` 已覆盖 Workspace/Session 等字段映射，并验证部分业务标识不再
重复塞入 Metadata。

## 4. 尚未完成的架构边界

### 4.1 backend/agent 仍然是业务模型，不是独立执行模型

`backend/agent.RequestContext` 当前仍包含：

- Assistant。
- Actor。
- OrgID、ProjectID、TaskID、RequestID。
- Workspace/RepoDir。
- Attachments URL。
- Runtime kind。
- SingerOS RunStatus。
- EventSink。
- `map[string]any` Metadata。

`RunResult` 也包含业务状态和任意 Metadata。

因此当前只是把原 `internal/agent` 文件移动到顶层；import 方向虽然干净，类型语义仍与
SingerOS 业务耦合。

目标仍应是：

```text
backend/agent:
  ExecutionRequest
  ExecutionResult
  Runtime
  Executor
  Observer
  Tool
  InteractionHandler

internal/assistant:
  RunRequest
  RunResult
  Assistant / Actor / Project / Workspace business context
```

### 4.2 目标 Executor 和 Registry 尚未实现

当前 `backend/agent` 没有：

- `Executor`。
- 公开的 Runtime Registry。
- Runtime name normalization。
- 通用 execution lifecycle。
- Observer error 策略。
- Runtime contract suite。

Runtime 解析和默认 Runtime 选择仍在 `internal/runtime/service.go` 中完成。

### 4.3 PreparedRun 仍是过渡结构

当前 `PreparedRun` 仍持有完整业务 `RequestContext`：

```go
type PreparedRun struct {
    Request *RequestContext
    Spec ExecutionSpec
    Workspace PreparedWorkspace
}
```

Runtime 通过 Spec 构造完执行数据后，又回写到克隆的业务 Request 中，再进入旧
`runWithRequest`。这不是真正的独立执行边界。

此外：

- `ArtifactBaseline` 没有赋值或消费。
- `ExecutionSpec.MaxSteps` 没有被 Runtime 使用。
- `PreparedWorkspace.TaskDir` 没有被 Runtime 直接使用。
- Messages 虽写回 Request，但 Engine RunRequest 没有统一 messages 字段。
- AllowedTools 虽写回 Request，但 Engine RunRequest 没有对应能力，native 仍使用默认 Tool 集。

因此 PreparedRun 只能视为兼容层，不能继续作为最终公共 API 扩展。

### 4.4 四个 Runtime 尚未直接实现统一契约

当前仍然是：

```text
agent.Runtime
    → externalcli.Runner
    → engines.Engine
    → native / claude / codex / opencode
```

四个 provider 并未各自直接实现 `agent.Runtime`，也没有目标目录：

```text
backend/agent/runtime/native
backend/agent/runtime/claude
backend/agent/runtime/codex
backend/agent/runtime/opencode
```

`backend/engines` 仍直接依赖：

- `internal/runtime/events`
- `internal/runtime/todo`
- `internal/api/contract`
- `internal/skill/catalog`

尤其 native 仍自行读取 Session API、默认 Skill 和 Todo，未达到同级黑盒 Runtime 的边界。

### 4.5 Engine/Runtime/Event 三套协议仍同时存在

当前仍并存：

- `agent.RuntimeResult`
- `agent.RunResult`
- `engines.EngineResult`
- `engines.RunHandle`
- `engines.Execution`
- `agent.Event`
- `runtime/events.Event`
- `engines.EngineEvent`

`engines.Execution` 和 `EngineResult` 尚未成为实际 Engine 主接口，实际执行仍使用
`RunHandle.Events`。

### 4.6 assistant 不是完整业务入口

`internal/assistant` 当前只有 Preparer 和 Finalizer，并继续实现
`internal/agent/run` 定义的接口。

缺失：

- `assistant.Service`。
- 业务 `RunRequest` / `RunResult`。
- `ConversationReader`。
- `MemoryReader`。
- `SkillResolver`。
- `ModelResolver`。
- `WorkspaceManager`。
- `AttachmentIngestor`。
- `ToolProvider`。
- `EventMapper`。

实际 Service、Journal 和业务 terminal event 所有权仍在 `internal/agent/run`。

### 4.7 Workspace 与附件仍在 Worker Handler

`command/run.Handler` 仍直接执行：

- Gitea clone URL 构建。
- Task workspace 准备。
- Attachment 下载。
- Attachment git add/commit/push。
- Active run 注册。
- Cancel。
- Agent Service 调用。
- 失败事件补发。

这部分尚未进入 `assistant.Preparer` 或 Workspace 端口。

### 4.8 Coordinator 尚未接入主路径

`worker/run.Coordinator` 已补充 waiter 和 in-flight wait，但：

- `cmd/leros/worker.go` 没有构造 Coordinator。
- `command/run.Handler` 没有持有或调用 Coordinator。
- Handler 仍使用自己的 semaphore、debouncer、WorkerPool、pending 和 activeRuns。
- `maxConcurrency` 字段没有参与任何执行控制。
- Coordinator 没有测试。
- 非首条 debounce submission 立即返回空结果，若直接接入会让对应 NATS delivery 提前 ACK。
- Active run 的 Register/Unregister 没有整合到 Execute 流程。

因此 Coordinator 当前仍是未接线骨架。

### 4.9 Memory、Skill 和 Interaction 仍使用全局状态

当前仍存在：

- `modelrouter.DefaultStore()`。
- `skillcatalog.List/Get/ReadFile()`。
- `localmemory.NewStore(Options{})` 默认路径。
- `skillmanagetools.OnMutation` package-level callback。
- `engines.DefaultInteractionRouter`。

Memory/Skill 没有形成实例化 Service，也没有通过 assistant ports 注入。

### 4.10 Tool 仍违反强类型约束

当前 Tool 公共接口仍是：

```go
Execute(ctx context.Context, input map[string]interface{}) (string, error)
Validate(input map[string]interface{}) error
```

Node、Skill、Memory、Todo、Artifact 等实现均依赖该签名。目标的
`json.RawMessage + 具名 request/result struct` 尚未开始迁移。

### 4.11 runtime/events 尚未迁移

虽然 `pkg/messaging` 已拥有完整 RunEvent wire 类型，但以下模块仍导入
`internal/runtime/events`：

- Engine implementations。
- API contract/DTO/handler。
- Session services/projectors。
- CLI chat。
- Todo。
- Worker legacy stream sink。

因此 `internal/runtime/events` 当前仍不能删除。

### 4.12 旧包仍是主装配路径

Worker 入口仍调用：

```text
internal/runtime.NewService
    → internal/agent/run.NewService
    → internal/runtime/externalcli
    → backend/engines
```

`backend/agent.Executor` 和 `internal/assistant.Service` 尚未出现，说明目标主路径还未切换。

## 5. 当前 P0 正确性问题

以下问题应在继续目录迁移前优先修复。

### P0-1：run.completed 缺少 RunCompletedPayload

`internal/agent/run.Service` 创建 terminal event 时只设置：

- RunID。
- TraceID。
- Type。
- CreatedAt。
- Content。

没有把 RunResult、Usage、Events、Artifacts 和 Metadata 编码到 Event Payload。

`NATSEventSink` 只有在 terminal event Payload 非空时才构建
`messaging.RunCompletedPayload`。

Server `session_run_state_projector` 对 `run.completed` 的行为是：

```text
RunCompleted == nil
    → 记录 warning
    → return
```

因此当前成功 Run 可能无法完成并持久化 assistant message。

### P0-2：run.failed 技术错误可能丢失

失败结果正确区分了：

- `Message`：用户内容。
- `Error`：技术错误。

但 terminal event 只把 `Message` 写入 Content。普通 failed 的 Message 为空，因此
`NATSEventSink` 生成的 `RunEventError.Message` 也是空字符串，`RunResult.Error` 没有进入
wire payload。

这会破坏 `content是content，错误信息是错误信息` 的约束。

### P0-3：Prepared prompt 被重复格式化

Preparer 使用 `BuildUserInput` 生成：

```text
user: hello
```

Runtime Execute 再把该字符串放回一条 role=user 的 InputMessage，随后旧
`runWithRequest` 再调用一次 `BuildUserInput`，最终可能变成：

```text
user: user: hello
```

### P0-4：附件文本可能重复注入

Preparer 已把 Attachment block 追加到 `Spec.Prompt`。Runtime Execute 把 Spec.Prompt
写回 InputMessage，但没有清空 Request attachments。旧 runWithRequest 随后再次追加
Attachment block。

### P0-5：EventSink 吞掉发布错误

`NATSEventSink.Emit` 在以下情况仍返回 nil：

- subject 构建失败。
- NATS Publish 失败。

Journal 因此无法知道关键状态事件没有发布成功。目标方案定义的 Observer error 终止语义
尚未实现。

### P0-6：未知 Event 被静默映射为 message.delta

`mapRunEventType` 的 default 分支返回 `RunEventMessageDelta`。新增或拼写错误的 event 会被
错误地当作助手消息发布，而不是显式失败。

## 6. 当前测试状态

本次执行：

```bash
go test \
  ./backend/agent/... \
  ./backend/internal/agent/... \
  ./backend/internal/assistant/... \
  ./backend/internal/runtime/... \
  ./backend/engines/... \
  ./backend/internal/worker/run/... \
  ./backend/internal/worker/command/run/...
```

结果：

### 通过

- `backend/agent`：可编译，但没有测试。
- `backend/internal/agent/run`：可编译，但没有测试。
- `backend/internal/assistant`：可编译，但没有测试。
- `backend/internal/runtime/drivers/externalcli`。
- `backend/internal/runtime/events`。
- `backend/engines`、builtin、claude、codex、native。
- `backend/internal/worker/command/run`。
- `backend/internal/worker/run`：可编译，但没有测试。

### 源码相关失败

- `runtime/lifecycle/context`：
  `TestContextBuilderBuildSystemPromptLayers` 的 identity prompt 断言失败。
- `runtime/todo`：
  `TestTrackerSnapshotNormalizesAndEmitsFullList` 缺少预期的事件元数据。

### 环境相关失败

- `engines/opencode` 的 `httptest.NewServer` 因沙箱禁止 IPv6 bind 失败。

### 测试覆盖缺口

- `backend/agent` 无测试。
- `internal/assistant` 无测试。
- `internal/agent/run.Service` 无测试。
- Coordinator 无测试。
- 新 `worker/eventpub/NATSEventSink` 无测试。
- 原 `handler_test.go` 被改名为 `handler_test.go.skip`，核心消费、ACK、取消测试未执行。
- 没有 Worker event → Server projector 的端到端 terminal event 测试。

## 7. 后续执行计划

## Phase 0：先恢复主链正确性

该阶段优先级最高，完成前不继续大规模移动目录。

### 0.1 修复 terminal event 契约

- 由业务 Service 构造完整 terminal payload。
- completed/failed/cancelled 均携带：
  - status。
  - result message。
  - technical error。
  - usage。
  - archived events。
  - artifacts。
  - started/completed time。
  - typed metadata。
- 保证 Server projector 能完成成功消息。
- 保证 failed 的 Content 与 ErrorMsg 分离。

### 0.2 修复 PreparedRun 兼容适配

在最终 ExecutionRequest 落地前：

- 避免对 Spec.Prompt 再次执行 BuildUserInput。
- 避免重复追加 Attachment block。
- 明确 Messages、MaxSteps、AllowedTools、TaskDir 的实际消费路径。
- 为 Runtime Execute 增加输入不变性和字段消费测试。

### 0.3 修复事件发布失败语义

- subject 构建和 Publish 失败必须返回 error。
- 未知 EventType 必须返回显式映射错误。
- terminal publish 继续使用 without-cancel + timeout。

### 0.4 恢复测试基线

- 恢复 `handler_test.go`。
- 修复 system prompt 与 todo metadata 两个失败。
- 增加 RunService → NATSEventSink → projector terminal 测试。

### Phase 0 验收

- 成功 Run 能持久化 completed assistant message。
- 失败 Run 同时保留用户 Content 和技术 ErrorMsg。
- 取消 Run 的 Content 为“已取消”。
- prompt 和附件只注入一次。
- 发布失败对调用方可见。

## Phase 1：完成真正的 backend/agent 契约

### 1.1 拆分业务与执行类型

把当前业务类型迁到 `internal/assistant`：

- RequestContext。
- AssistantContext。
- ActorContext。
- WorkspaceContext。
- RunStatus。
- 业务 RunResult。

在 `backend/agent` 新建：

- ExecutionRequest。
- ExecutionResult。
- Message。
- ModelConfig。
- FilesystemContext。
- ExecutionPolicy。
- 强类型 ExecutionEvent。

删除 PreparedRun 对业务 Request 的引用；最终由 ExecutionRequest 取代 PreparedRun。

### 1.2 建立 Executor 和 Registry

- Executor 负责 Runtime 解析、通用 execution lifecycle、取消和 Observer 错误。
- Registry 只保存 `name → Runtime`。
- Runtime 构造保留在 cmd composition root。
- 增加 import-boundary test，禁止 `backend/agent/...` 导入业务包。

### 1.3 建立 Tool 和 Interaction 端口

- Tool 输入使用 `json.RawMessage`，实现内部解码为具名 request。
- ToolResult 使用具名结构。
- Approval/Question 使用强类型 InteractionHandler。
- 不在 core 中定义 NATS 或 API DTO。

## Phase 2：四个 Runtime 直接实现统一接口

迁移顺序：

1. Claude。
2. Codex。
3. OpenCode。
4. Native。

每个 Runtime 迁移到：

```text
backend/agent/runtime/{name}
```

要求：

- 直接实现 `agent.Runtime`。
- 不通过业务 RequestContext。
- 不导入 `internal/runtime/events`。
- 不生成业务 `run.*` event。
- Provider-specific parser 保留在各自 Runtime 包。
- CLI process/session resume 公共逻辑移动到 `runtime/externalcli`。

Native 额外要求：

- Session history 由 ExecutionRequest.Messages 注入。
- Tool 由 ExecutionRequest.Tools 注入。
- MaxSteps 使用 ExecutionRequest 配置。
- 禁止导入 `internal/api/contract`、Skill catalog 和 runtime todo。

为四个 Runtime 建立同一 contract suite。

## Phase 3：完成 assistant 业务层

### 3.1 建立 assistant.Service

assistant.Service 成为唯一业务 Run 入口：

```text
validate
→ run.started
→ Preparer
→ agent.Executor
→ Finalizer
→ artifact events
→ exactly one terminal event
→ best effort post-run
```

`internal/agent/run.Service` 的职责合并到 assistant.Service 后删除。

### 3.2 建立 ports

在使用位置定义：

- ConversationReader。
- MemoryReader。
- SkillResolver。
- ModelResolver。
- WorkspaceManager。
- AttachmentIngestor。
- ToolProvider。

去除 DefaultStore 和 package-level callback。

### 3.3 分离 EventMapper 与 Publisher

- assistant.EventMapper：ExecutionEvent → messaging.RunEvent。
- worker/eventpub：只负责 subject 和 Publish。
- Publisher 不复制 core payload struct。
- wire golden tests 保证 JSON 兼容。

## Phase 4：接入 RunCoordinator 并压薄 Handler

### 4.1 完成 Coordinator 语义

- 使用 `maxConcurrency` 做真实并发限制。
- 所有合并 delivery 在 batch 完成前保持正确 ACK/状态语义。
- Coordinator 内统一 RegisterRun/UnregisterRun。
- Cancel、Close 和 Submit 竞态测试。
- SinkFactory 或 ExecuteFunc 只能传入必要端口。

### 4.2 切换 Worker 主路径

Worker 装配改为：

```text
Command Handler
    → RunCoordinator
    → assistant.Service
```

从 Handler 删除：

- Workspace 准备。
- Attachment 下载。
- Git。
- WorkerPool/debouncer/pending/activeRuns。
- 业务失败事件补发。

## Phase 5：Memory、Skill 和 Tool 强类型化

- Memory 使用实例化 Store，由 composition root 注入。
- Skill 对外收敛为一个 Service，source fetch 保留独立 adapter。
- Skill mutation 改为注入 callback/event port。
- Tool 逐个迁移到 RawMessage + typed request/result。
- 删除 `map[string]interface{}` 公共签名。

建议顺序：

1. Todo。
2. Memory。
3. Artifact。
4. Skill use/manage。
5. Node tools。

## Phase 6：删除旧包和兼容层

完成主路径切换后删除：

- `backend/engines` 旧位置。
- `internal/runtime/service.go`。
- `internal/runtime/drivers`。
- `internal/runtime/events`。
- `internal/runtime/lifecycle`。
- `internal/runtime/todo`。
- `internal/agent/run`。
- legacy stream sink。
- Deprecated Runner/RunHandle/Execution 别名。
- `.skip` 测试文件。

MCP HTTP server 移到 Worker infrastructure，不能因为 MCP 保留整个 runtime 收纳包。

## 8. 建议提交拆分

为降低当前 staged/unstaged 混合状态的风险，后续按以下提交拆分：

1. `fix(agent): 修复终端事件与执行输入重复拼接`
2. `test(agent): 恢复主链与终端投影测试`
3. `refactor(agent): 建立独立执行契约与Executor`
4. `refactor(runtime): 迁移四种Runtime实现`
5. `refactor(assistant): 收敛业务准备与收尾`
6. `refactor(worker): 接入RunCoordinator并压薄Handler`
7. `refactor(skill): 实例化Memory和Skill依赖`
8. `refactor(agent): 删除旧Runtime与兼容层`

每个提交都必须通过对应的定向测试，不允许把“移动文件”和“改变运行语义”混在同一提交中。

## 9. 最终验收清单

- [ ] `backend/agent` 不包含 Assistant、Actor、Org、Project、业务 Workspace。
- [ ] `backend/agent` 不包含业务 RunStatus。
- [ ] 只有一个 Runtime 接口。
- [ ] 四个 Runtime 直接实现该接口。
- [ ] 只有一个 ExecutionEvent 模型。
- [ ] 只有一个 ExecutionResult。
- [ ] assistant.Service 是业务 Run 唯一入口。
- [ ] Runtime 不读取 Session、Memory、Skill 或数据库。
- [ ] Handler 不准备 Workspace、不执行 Git。
- [ ] Coordinator 是唯一调度与取消所有者。
- [ ] Core event 与 wire event 显式映射。
- [ ] terminal event 恰好一个且 payload 完整。
- [ ] NATS publish error 不被吞掉。
- [ ] content 与 error_msg 分离。
- [ ] Tool 公共接口不使用 `map[string]interface{}`。
- [ ] `internal/eventengine` 已删除。
- [ ] `internal/runtime` 和旧 `backend/engines` 已删除。
- [ ] Handler、Coordinator、assistant.Service、四个 Runtime 均有测试。
- [ ] Server/Worker wire schema、SSE 和 HTTP API 保持兼容。
