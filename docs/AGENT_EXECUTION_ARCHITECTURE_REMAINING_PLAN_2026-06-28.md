# Agent 执行架构剩余实施计划（2026-06-28）

## 文档定位

本文基于当前工作区快照重新核对 Agent 架构迁移进度，只记录尚未完成的工作。

本文不表示以下事项已经实施，也不替代最终测试报告。当前工作区同时存在 staged、
unstaged 和未跟踪改动，后续应继续按同一个迁移快照处理，避免拆分后遗漏依赖关系。

## 当前结论

> 2026-06-29 更新：本文中的 P1 已实施完成。下方 P1 章节保留为实施记录，
> 后续剩余工作从 P2 和最终全仓验收继续。

业务执行主链已经建立：

```text
command/run.Handler
→ worker/run.Coordinator
→ internal/assistant.Service
→ agent.Executor
→ agent.Runtime
```

`backend/agent` 的公共执行契约、`internal/assistant` 业务封装、Coordinator 调度、
Workspace 快照和终端事件归档已经基本落地。旧的 `backend/engines`、
`backend/internal/runtime`、`backend/internal/agent` 和
`backend/internal/eventengine` 已退出代码树。

P1 实施后，内部执行路径也已完成收敛：

- Claude、Codex、OpenCode 由 composition root 直接构造并注册具体 Runtime。
- `externalcli.Driver` 只暴露 `RunInvocation`，不再形成第二套 Runtime。
- native Runtime 直接消费 `agent.ExecutionRequest`。
- `provider.Engine`、`provider.Registry`、`provider.RunRequest`、
  `provider.RunHandle` 和 `EngineEvent*` 已删除。
- 四个 Runtime 已进入同一成功、取消和 observer error contract suite。
- Finalizer、Coordinator、Handler、NATS wire golden 和 projector 缺失测试已补齐。

当前剩余重点是 P2 的全仓强类型 JSON 口径、文档最终收尾和仓库根目录最终验收。

按最终架构目标评估：

| 范围 | 当前状态 | 剩余重点 |
|---|---|---|
| P0 基线 | ✅ 已完成 | — |
| P1 主链行为 | ✅ 已完成 | 已补失败传播、取消竞态、Close 和 delivery failure 测试 |
| P1 独立执行契约 | ✅ 已完成 | 四个 Runtime 直接注册，旧 Engine 兼容框架已删除 |
| P2 旧层与全局状态 | 大部分完成 | 明确并实施全仓无类型 map 的验收口径 |
| 测试和验收 | P1 范围完成 | 仍需仓库根目录最终验收和文档一致性检查 |
| 文档 | 已更新主体 | 修正旧结论文档 |

整体仍不应按“全部完成”验收，但 P1 已不再是阻塞项。

## P1 实施结果（2026-06-29）

- 新增窄契约 `externalcli.Invoker`、`InvocationRequest` 和 `Invocation`。
- 三个 CLI Runtime 自己构造 provider invoker，并共享进程、session 和交互设施。
- bootstrap 不再创建 Engine Registry，直接注册四个 Runtime。
- native Runner 直接返回 `ExecutionResult` 并通过 Observer 发送 activity event。
- provider invocation 使用独立生命周期事件，不再复用业务 `run.*`。
- observer error 在 native 和 CLI Runtime 中均向上返回。
- Finalizer 直接使用 `PreparedRun.Workspace`，不再根据原 Request 重新计算路径。
- 新增 Executor、四 Runtime 公共契约、Coordinator 竞态、Handler 失败序号、
  NATS 五类 wire golden 和 projector 回放测试。

本轮已通过：

```bash
go test ./... -count=1 -timeout=120s
go test -race ./backend/agent/... \
  ./backend/internal/assistant \
  ./backend/internal/worker/run \
  ./backend/internal/worker/command/run \
  ./backend/internal/worker/eventpub
go vet ./backend/...
```

## 已完成且不应重复实施

后续计划不再重复以下工作：

- 不重新设计 `agent.Runtime`、`ExecutionRequest` 和 `ExecutionResult` 公共契约。
- 不恢复 `backend/engines` 或 `backend/internal/runtime`。
- 不把 Session、Workspace、Skill、Memory、Artifact 或消息队列逻辑放入
  `backend/agent`。
- 不重新引入 eventengine、simplechat 或旧 `internal/agent/run.Service`。
- 不改变当前 WorkerCommand、RunEvent、SSE 和 API JSON 的兼容形状。
- 不绕过 `assistant.Service` 增加新的业务执行入口。

## P0：基线确认 ✅（已执行）

### 完成情况

| 任务 | 结果 |
|------|------|
| `gofmt` | ✅ 已完成 — 8 个文件自动格式化（`backend/config/worker.go` 等） |
| `go test ./... -count=1 -timeout=120s` | ✅ 已通过 — 73 个包全部通过，无失败 |
| `go vet ./backend/...` | ✅ 已通过 — 零诊断 |
| `go test -race` (coordinator/assistant/eventpub) | ✅ 已通过 — 3 个包均无竞态报告 |
| `go build -o ./bundles/leros ./backend/cmd/leros/` | ✅ 已通过 — 二进制构建成功（143MB arm64） |

## P1：删除迁移后的 Engine 兼容执行框架 ✅

这是当前最主要的架构缺口。代码虽然已经从 `backend/engines` 移到
`backend/agent/runtime/provider`，但旧 Engine 层的职责仍然存在。

### 1. 将 provider 收窄为 CLI 基础设施

`backend/agent/runtime/provider` 最终只应提供运行 CLI 所需的低层能力，例如：

- CLI 探测和环境变量准备。
- 工作目录处理。
- 进程启动、标准流读取和退出等待。
- provider session 标识读取。
- 审批和提问交互桥接。
- MCP 配置和进程级辅助能力。

这些能力应使用窄接口表达，例如 `ProcessLauncher`、`Invocation`、
`EventSource` 或等价命名，不得继续形成第二套 Runtime。

需要删除或替换：

- `provider.Engine`。
- `provider.Registry`。
- `provider.RunRequest`。
- `provider.RunHandle`。
- 以 Engine 生命周期为中心的辅助函数。

### 2. 四个 Runtime 直接构造和注册

调整 `internal/assistant/bootstrap`：

- native、Claude、Codex、OpenCode 分别由自己的构造函数创建。
- composition root 直接把四个实例注册到 `agent.Registry`。
- 删除 `builtin.NewRegistryFromConfig` 返回 provider Engine Registry 的流程。
- 删除“先构造 Engine，再包装 Driver，再包装 Runtime”的三层装配。

目标装配关系：

```text
assistant bootstrap
├─ native.NewRuntime(...)
├─ claude.NewRuntime(...)
├─ codex.NewRuntime(...)
└─ opencode.NewRuntime(...)
        ↓
agent.Registry.Register(...)
```

### 3. 将 externalcli.Driver 降为内部执行设施

`externalcli.Driver` 当前的 `Execute(context.Context, agent.ExecutionRequest, agent.Observer)`
与 `agent.Runtime` 过于接近，仍然构成隐藏的兼容 Runtime。

调整目标：

- 改为 provider invocation 级别的方法，例如 `RunInvocation`。
- 只负责进程、provider session 和事件消费。
- 不解析 Runtime 名称，不承担 Registry 选择。
- 不发布 `execution.started/completed/failed/cancelled`。
- 不拥有业务终态。

Claude、Codex 和 OpenCode Runtime 自己实现 `Name` 与 `Execute`，并显式调用该设施。

### 4. native 真正直接实现 Runtime

当前 native Runtime 仍把 `agent.ExecutionRequest` 转换为 `provider.RunRequest`，
再交给旧式 Runner。

调整目标：

- native 执行逻辑直接接收纯 `agent.ExecutionRequest`。
- 删除 native Runtime 到 `provider.RunRequest` 的转换。
- 删除只为兼容旧 Engine 契约存在的 Runner/Handle 包装。
- 保留 native 模型调用、Tool adapter 和 activity event 生成，不改变业务外部行为。

### 5. 统一事件命名

`EngineEvent*` 常量仍暴露旧架构语义，应改为统一的 activity/provider event 命名。

要求：

- Runtime 只产生 message、reasoning、tool、todo、artifact、approval、question
  和 provider-session 等活动事件。
- `execution.*` 只由 `agent.Executor` 产生。
- `run.*` 只由 `assistant.Service` 产生。
- 删除 `SendEngineEvent*` 等旧命名，避免未来再次把 Runtime 当作业务 Engine。

### 删除顺序

1. 先增加新的窄 invocation 接口。
2. 逐个迁移 Claude、Codex、OpenCode。
3. 迁移 native。
4. 调整 bootstrap 直接注册 Runtime。
5. 公共 contract suite 和全仓测试通过。
6. 最后删除 provider Registry、Engine、RunRequest、RunHandle 及对应测试。

在第 5 步完成前，不提前删除仍被生产路径使用的兼容类型。

## P1：补齐执行和业务层测试 ✅

### Executor

现有测试覆盖默认 Runtime、基础生命周期、取消和 started observer error。仍需增加：

- Runtime 名称无法解析时返回明确错误并产生唯一 failed lifecycle。
- Runtime 执行失败时产生 `execution.failed`。
- terminal observer 返回错误时的返回值和唯一终态行为。
- Runtime 返回结果与 error 同时存在时的优先级。

### 四个 Runtime 公共 contract suite

当前 Claude、Codex、OpenCode 使用共同的成功/取消契约测试，native 只验证名称和
空 ExecutionID。

需要：

- native 进入同一套成功、取消和 observer 行为 contract suite。
- 四个 Runtime 共用相同断言，不允许只测试包装层。
- contract suite 必须在移除 Engine 兼容层后直接构造具体 Runtime。
- 每个 provider 额外保留协议解析、session resume 和进程退出的专项测试。

### assistant

现有 Service、Journal 和 Preparer 测试已覆盖主要语义。仍需对真实 Finalizer 增加：

- 使用同一 `PreparedRun` 和 `WorkspacePreparation`。
- Artifact manifest 合并进入终端结果。
- Git 收尾成功、失败和取消行为。
- Finalizer 失败不会产生第二个业务终态。
- completed、failed、cancelled 的 StartedAt 均来自 Service 开始时间。

### Coordinator

现有测试覆盖 debounce waiter、并发上限、Session 串行、跨 Session 并行、
active cancel 和 Close 拒绝请求。仍需增加：

- Cancel 与 pending → active 切换同时发生的竞态。
- Close 等待已经进入执行中的 submission。
- 单个 waiter context 取消不破坏同批次其他 waiter。
- 执行错误向同批次全部 waiter 一致传播。
- pending timer 触发与 Close 并发时不泄漏 goroutine、不重复完成。

### Handler

增加执行失败路径测试：

- 只有批次真正成功后才标记 delivery sequence completed。
- 执行失败时当前 delivery sequence 标记 failed。
- debounce 合并后的每个 delivery sequence 都获得一致终态。

### NATS 和服务端投影

当前 NATS 测试是语义断言，不是原计划要求的 golden wire fixture。

需要：

- 为 started、activity、completed、failed、cancelled 增加稳定 JSON golden。
- 覆盖 `ReplyToMessageIDs`、stream/state 双 lane、Artifact、Usage、
  Tool Calls、归档 Events 和 Error。
- projector 增加 chunks、artifact、usage、replay sequence 的组合测试。
- 验证重复投递和 replay 不会改变最终 Session 状态。

## P2：明确无类型 map 的验收范围

Agent 公共执行边界已经使用具名结构和 `json.RawMessage`，但全仓仍存在公开的
`map[string]interface{}` 或 `map[string]any` 契约，例如：

- `backend/pkg/llmprotocol` 的 Adapter 接口。
- `backend/internal/infra/websocket` 的消息 Payload 和发送接口。
- `backend/prompts` 的模板参数接口。
- OpenCode 外部协议 DTO 中的动态 JSON 字段。

后续必须先明确验收口径：

### Agent 架构口径

如果约束只针对 Agent 跨层业务通信：

- 保持 `backend/agent` 的架构测试。
- 允许 provider 私有协议 DTO 在 JSON 边界内部使用动态对象。
- 动态对象不得穿过 Runtime、assistant 或 Worker 公共接口。

### 全仓严格口径

如果最终标准是全仓禁止跨层无类型 map，则另立迁移批次：

1. `llmprotocol` 改用 `json.RawMessage` 加 provider 具名 DTO。
2. websocket 定义具名 envelope 和 payload union。
3. prompts 定义类型化模板参数或受约束的参数值类型。
4. API、事件和存储 metadata 逐项收敛。
5. 增加静态架构测试，禁止新的公开函数签名引入无类型 map。

此项范围明显大于 Agent 架构收敛，不应混入 provider Engine 删除提交中。

## P2：文档收尾

代码与测试完成后再更新结论文档：

- 修正 `AGENT_EXECUTION_ARCHITECTURE_PROGRESS_2026-06-28.md`
  中“全部完成”和旧验证结果。
- 更新 `leros-architecture.html`，只展示实际存在的最终层级。
- 检查 `ARCHITECTURE.md`、`ARCHITECTURE_BACKEND.md` 和
  `PROJECT_STRUCTURE.md`，删除 provider Engine Registry、旧 Runner 和过时路径。
- 将过程性 progress 文档标记为历史快照，明确其对应 commit 或工作区日期。
- 使用浏览器检查 HTML 的布局、连线、文本溢出和移动端可读性。

## 建议实施批次

### 批次 A：补缺失测试（当前批次）

- P0 基线已执行并通过。
- 补 Executor、Finalizer、Coordinator、Handler 缺失测试。
- 增加 NATS golden，锁定现有 wire 兼容行为。

### 批次 B：CLI Runtime 收敛

- 提取窄 invocation 设施。
- 迁移 Claude、Codex、OpenCode。
- bootstrap 直接注册三个 CLI Runtime。
- 删除 `provider.Registry` 和 `provider.Engine`。

### 批次 C：native 收敛

- native 直接消费 `agent.ExecutionRequest`。
- 删除 `provider.RunRequest`、`RunHandle` 和旧 Runner 兼容路径。
- native 纳入完整公共 contract suite。

### 批次 D：命名、静态约束和文档

- 清理 `EngineEvent*`、`SendEngineEvent*` 等旧命名。
- 执行静态边界检查。
- 更新进度、HTML 和架构索引。

### 批次 E：可选的全仓 typed JSON 迁移

- 仅在确认全仓严格验收口径后实施。
- 独立提交、独立测试，不与 Agent Runtime 收敛混合。

## 最终验收标准

### 结构

- `backend/agent` 不依赖 SingerOS 业务包。
- 主链只有一套 Runtime、ExecutionRequest、ExecutionResult 和 activity Event。
- 四个 Runtime 在 composition root 直接注册。
- 生产路径不存在 `provider.Engine`、`provider.Registry`、
  `provider.RunRequest` 和 `provider.RunHandle`。
- externalcli 只是低层 invocation 设施，不形成第二套 Runtime。
- native 不通过旧 Engine 请求做二次转换。

### 行为

- 同 Session 串行、不同 Session 并行、debounce 全 waiter 等待真实结果。
- Cancel 和 Close 在竞态场景下仍能收敛且不泄漏 goroutine。
- 每次 execution 恰好一个 execution terminal event。
- 每次业务 Run 恰好一个 run terminal event。
- cancelled 的用户内容为 `已取消`，技术错误只进入 Error。
- ReplyToMessageIDs、Artifact、Usage、Tool Calls 和归档 Events 完整保留。

### 兼容性

- WorkerCommand、RunEvent、SSE 和 API JSON 不变。
- provider session resume 行为不变。
- NATS stream/state 双 lane 不变。
- golden wire fixture 无非预期变化。

### 质量门禁（每次批次提交前执行）

```bash
go test ./... -count=1 -timeout=120s
go test -race ./backend/internal/worker/run \
  ./backend/internal/assistant \
  ./backend/internal/worker/eventpub
go vet ./backend/...
go build -o ./bundles/leros ./backend/cmd/leros/
```

静态检查同时满足：

- 无 `.skip` 测试。
- 无生产 `panic`。
- 无迁移期 type alias。
- 旧架构目录无生产文件。
- Agent 公共边界无无类型 map。
- 若采用全仓严格口径，则所有公开跨层接口均无
  `map[string]interface{}` 和 `map[string]any`。
