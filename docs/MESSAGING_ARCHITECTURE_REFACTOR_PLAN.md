# Server/Worker 消息通信架构重整计划

## 最终目标

重整当前 server、worker 之间分散的消息通信实现，形成统一、清晰、可维护的消息架构：

- server/worker 消息协议统一到共享包，避免 server 侧继续依赖 `internal/worker/protocol`。
- subject 生成、consumer 名称、JetStream stream 配置集中管理，避免业务包各自拼接 topic。
- worker 侧统一消息入口，由 command dispatcher 分发到 task、control、interaction、skill 等 handler。
- server 侧统一 run event projector，收敛 run started、artifact declared、completed/failed 等状态投影逻辑。
- 保留按 QoS 分 lane 的物理 subject，避免不同延迟、吞吐、可靠性要求的消息互相阻塞。
- task 防抖只作用于 `cmd.run` lane，不影响 cancel、approval、question answer、skill request/reply。

## 当前问题

当前消息通信相关逻辑分散在多个包中：

- `backend/pkg/dm` 负责 subject、consumer、stream config。
- `backend/internal/worker/protocol` 定义 worker task、stream、control、skill message。
- `backend/internal/worker/taskconsumer` 消费 task 消息，并在内部处理 control 订阅。
- `backend/internal/worker/approval` 自建审批/问题答案订阅。
- `backend/internal/worker/skillmgmt` 自建 skill management 订阅和 request/reply。
- `backend/internal/runnable` 分散处理 session run started、artifact declared、completed 等投影。
- `backend/internal/service` 直接引用 worker internal protocol 构造消息。

这导致几个问题：

- 协议定义位置不符合分层边界，server 侧 import worker internal 包。
- 同一条会话运行在 DB model、worker task、runtime event、stream message、completed message、API DTO 之间多次包装和转换。
- worker task、control、approval、skill 订阅入口分散，启动 wiring 和错误处理不统一。
- 高频 stream event 和关键终态/状态事件耦合，容易让落库 projector 受增量消息影响。
- 如果简单合并为单一 command subject，会放大 head-of-line blocking 风险，并破坏现有 task 防抖语义。

## 目标设计

### 共享消息包

新增 `backend/pkg/messaging`，作为 server、worker 共同依赖的消息契约层。

建议包含：

- `Envelope[T]`：统一消息信封，包含 `id`、`type`、`created_at`、`trace`、`route`、`body`、`metadata`。
- `WorkerCommand`：统一 server -> worker 命令结构。
- `WorkerCommandResult`：用于 skill list/detail/install 等同步响应。
- `RunEvent`：统一 worker -> server/UI 运行事件结构。
- `Subjects`：集中生成 subject、wildcard、consumer、JetStream stream config。

`internal/infra/mq` 只保留传输能力，不再承担业务 subject 决策。

### Server -> Worker Lane

server -> worker 不合并为单一物理队列，而是统一协议后按 lane 拆分 subject：

| Lane | Subject | 用途 | 阻塞策略 |
| --- | --- | --- | --- |
| Run | `org.{org_id}.worker.{worker_id}.cmd.run` | 会话/task 执行命令 | 保留 session-keyed debounce 和 worker pool |
| Control | `org.{org_id}.worker.{worker_id}.cmd.control` | cancel run 等控制命令 | 高优先级，不经过防抖 |
| Interaction | `org.{org_id}.worker.{worker_id}.cmd.interaction` | approval resolve、question answer | 高优先级，不被长任务阻塞 |
| Skill | `org.{org_id}.worker.{worker_id}.cmd.skill` | skill install/list/detail/import/uninstall | 独立 request/reply，可慢执行 |

所有 lane 使用同一类 `WorkerCommand` 协议，但不同 subject 和 consumer 隔离消费压力。

### Worker -> Server/UI Lane

worker -> server/UI 按事件用途拆分：

| Lane | Subject | 用途 | 消费者 |
| --- | --- | --- | --- |
| Stream | `org.{org_id}.session.{session_id}.run.stream` | 高频 SSE 增量，如 message delta、reasoning delta、tool delta | API 实时流 |
| State | `org.{org_id}.session.{session_id}.run.state` | 低频关键状态，如 run.started、artifact.declared、approval.requested、question.asked、run.completed/failed/cancelled | API 实时流、server projector |

server projector 只订阅 state lane，避免关键状态落库被高频 delta 延迟。

### Worker Command Dispatcher

新增统一 worker command module，负责启动各 lane 订阅，并把消息分发到 handler：

- `RunCommandHandler`：由现有 `taskconsumer` 承担，处理 `cmd.run`，保留防抖和并发控制。
- `ControlCommandHandler`：处理 `run.cancel`，调用 task runner 的 active run registry。
- `InteractionCommandHandler`：处理 `approval.resolve`、`question.answer`，调用 `engines.DefaultInteractionRouter`。
- `SkillCommandHandler`：由现有 `skillmgmt` 改造而来，处理 skill request/reply。

`approval`、`skillmgmt`、control listener 不再各自创建 NATS 订阅器。

### Server Projector

合并当前分散的 runnable：

- `session_run_started.go`
- `session_artifact_declared.go`
- `session_completed.go`

目标形态为单一 `session_run_state_projector`：

- 消费 `org.*.session.*.run.state`。
- `run.started`：标记源用户消息为 processing，并记录 replay start seq。
- `artifact.declared`：幂等持久化 artifact。
- `run.completed`：创建 completed assistant message，完成源用户消息。
- `run.failed` / `run.cancelled`：创建失败或取消 assistant message，更新源用户消息状态。

## 分阶段实施计划

### Phase 1：建立共享协议和 subject builder

- 新建 `backend/pkg/messaging`。
- 从 `internal/worker/protocol` 和 `pkg/dm` 中迁移可共享的 envelope、trace、route、task、stream、skill、control 类型。
- 新增 lane subject builder 和 consumer name helper。
- 新增 JSON shape、subject builder、stream config 单元测试。

### Phase 2：改造 server 发布路径

- `MessagePoster` 改为发布 `cmd.run`。
- `CancelSessionRun` 改为发布 `cmd.control`。
- `SubmitApproval`、`SubmitQuestionAnswer` 改为发布 `cmd.interaction`。
- `SkillService`、`SkillMarketplaceService` 改为通过 `cmd.skill` request/reply。
- server 侧移除对 `internal/worker/protocol` 的 import。

### Phase 3：改造 worker 订阅入口

- 新建 worker command dispatcher。
- `taskconsumer` 只订阅/处理 run lane，保留现有 debounce、seq tracker、worker pool。
- `approval` 改为 interaction handler。
- `skillmgmt` 改为 skill handler。
- control listener 改为 control handler。
- worker 启动 wiring 只启动 command dispatcher 和必要的运行时服务。

### Phase 4：改造 run event 输出和 projector

- `MQStreamSink` 改为 run event publisher。
- runtime event 根据事件类型发布到 stream lane 或 state lane。
- 合并 server 侧 run state projector。
- `StreamSessionEvents` 同时订阅 stream 和 state，并保持外部 SSE payload 兼容。

### Phase 5：清理旧协议和 subject

- 将 `backend/pkg/dm` 改为临时兼容 shim，逐步清零引用后删除。
- 删除或迁移 `backend/internal/worker/protocol`。
- 清理旧 subject：
  - `org.*.worker.*.task`
  - `org.*.worker.*.control`
  - `org.*.worker.*.approval`
  - `org.*.worker.*.skill`
  - `org.*.session.*.message.stream`
  - `org.*.session.*.message.completed`
  - `org.*.session.*.message.request`

## 测试计划

### 单元测试

- `backend/pkg/messaging`：
  - command/result/run-event JSON shape。
  - subject builder。
  - wildcard subject。
  - stream config。
- worker command dispatcher：
  - run、control、interaction、skill lane 分发正确。
  - unknown command 返回明确错误。
- task 防抖：
  - 同一 session 连续 user message 仍能 merge。
  - control、interaction、skill 不进入 task debounce。
- run event lane classifier：
  - message delta、reasoning delta、tool delta 进入 stream lane。
  - run started、artifact declared、approval/question、terminal event 进入 state lane。
- server projector：
  - started 标记 processing。
  - artifact declared 幂等落库。
  - completed/failed/cancelled 正确创建最终 assistant message。
- skill request/reply：
  - success/error/data JSON 兼容现有 API。

### 回归测试

- 用户消息 -> worker run -> SSE 增量 -> completed 落库。
- 高频 message delta 不阻塞 run.completed projector。
- 长时间 skill install 不阻塞 approval/question/cancel。
- approval/question 能恢复被阻塞的 run。
- cancel run 后用户消息和 assistant message 状态正确。
- skill list/install/detail/import/uninstall API 正常。

### 建议执行命令

```bash
go test ./backend/pkg/messaging ./backend/internal/worker/... ./backend/internal/service ./backend/internal/runnable
go test ./...
```

## 验收标准

- server 侧不再 import `backend/internal/worker/protocol`。
- worker 启动中不再分别启动 approval subscriber、skillmgmt subscriber、control listener。
- task 防抖只存在于 run lane。
- cancel、approval/question、skill 消息拥有独立 lane，不受 run debounce 和高频 stream event 阻塞。
- server projector 只消费 state lane 即可完成会话状态落库。
- 外部 HTTP API、SSE payload、数据库 schema 尽量保持兼容。

## 默认假设

- “统一”指协议、subject 生成、worker 入口和治理统一，不指所有消息共用一个 NATS subject。
- 允许调整内部 NATS subject，不要求兼容旧 subject 上未消费的历史消息。
- 文档计划优先解决可维护性，同时保留对消息阻塞和 task 防抖的工程约束。
