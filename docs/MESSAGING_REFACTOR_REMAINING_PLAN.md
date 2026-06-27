# 剩余消息通信改造计划

## Summary

本计划只覆盖当前尚未完成和需要调整的部分，不重复已经落地的 `pkg/messaging`、dispatcher 初版、state projector 初版等工作。

目标包括：

- 完成未收敛的旧订阅、旧协议、旧 projector 清理。
- 调整 `WorkerCommandBody` 设计，避免按 command 平铺不同参数。
- 补齐 `run.stream` / `run.state` 拆分后的回放和消费一致性。
- 将本轮幂等处理限定在消息投影直接相关的重复消费问题，不扩大到后续 agent 内部事件层级治理。

## Key Changes

### 1. 重做 WorkerCommand Payload 结构

将当前平铺字段式 `WorkerCommandBody` 改为：

```go
type WorkerCommandBody struct {
	CommandType CommandType     `json:"command_type"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	ReplyTo     string          `json:"reply_to,omitempty"`
}
```

为不同 command 定义独立 payload：

- `RunCommandPayload`
- `CancelRunCommandPayload`
- `ApprovalResolveCommandPayload`
- `QuestionAnswerCommandPayload`
- `SkillCommandPayload`

在 `backend/pkg/messaging` 中提供构造和解码 helper：

- `NewRunCommand`
- `NewCancelRunCommand`
- `NewApprovalResolveCommand`
- `NewQuestionAnswerCommand`
- `NewSkillCommand`
- `DecodeCommandPayload[T]`

调用方禁止直接手写 command-specific flat fields，只通过 builder 构造命令。`ReplyTo` 暂保留在 command body，后续如要进一步抽象 transport metadata 再单独调整。

### 2. 收敛 Worker 旧订阅入口

- `taskconsumer` 停止直接订阅旧 `dm.WorkerTaskSubject`，改为只暴露 `HandleRunCommand` 给 dispatcher 调用。
- 删除或停用 `taskconsumer.StartControlListener`，cancel 只走 dispatcher 的 control lane。
- `approval.Subscriber` 不再参与 worker 启动；保留必要逻辑时迁移到 interaction handler 后删除旧 subscriber。
- `skillmgmt.Consumer.Start` 不再参与 worker 启动；skill lane 只由 dispatcher 订阅。
- worker 启动 wiring 中只启动 command dispatcher，不再分别启动 task、control、approval、skill 订阅。

### 3. 消除旧协议桥接

- 移除 `messaging -> internal/worker/protocol` 的临时转换层。
- `taskconsumer` 内部直接使用 `messaging.WorkerCommand` 和 `RunCommandPayload`。
- `skill` handler 直接处理 `SkillCommandPayload`，不再转换成旧 `protocol.SkillManagementMessage`。
- server 和 worker 代码不再 import `backend/internal/worker/protocol`。
- 待引用清零后删除 `backend/internal/worker/protocol`。

### 4. 收敛 Server 旧 Runnable 和旧 Subject

- 停止启动旧 runnable：
  - `StartSessionRunStarted`
  - `StartSessionArtifactDeclared`
  - `StartSessionCompleted`
- 仅保留新的 `session_run_state_projector` 处理 state lane。
- `MessagePoster` 不再发布 `SessionMessageRequestSubject` 给标题 runnable；标题更新改成本地异步调用或新的轻量内部服务调用。
- `StreamSessionEvents` 不再订阅旧 `dm.SessionResultStreamSubject`，改为按新 run event subject 订阅。

### 5. 补齐 Stream/State 回放策略

- `run.stream` 和 `run.state` 事件都必须携带同一 run 内单调序号 `Seq`。
- processing 用户消息 metadata 中记录：
  - `run_id`
  - `reply_to_message_ids`
  - `stream_start_seq`
  - `state_start_seq`
- SSE replay 同时订阅 stream lane 和 state lane：
  - 用 NATS sequence 做各 lane 的起点。
  - 用 `run_id` / `reply_to_message_ids` 过滤目标 run。
  - 用 run 内 `Seq` 在服务端投影或发送前排序/去重。
- server projector 只消费 state lane，不依赖 stream lane 完成状态落库。
- 如果双 lane replay 暂时风险过高，则先保留单 stream replay，state lane 只服务 projector；该限制需要在代码注释和测试中明确。

### 6. 清理 dm Shim 和旧 Subject

- 将仍使用 `backend/pkg/dm` 的代码迁移到 `backend/pkg/messaging`。
- 引用清零后删除或最小化 `pkg/dm`。
- 清理旧 subject 的测试和启动逻辑：
  - `org.*.worker.*.task`
  - `org.*.worker.*.control`
  - `org.*.worker.*.approval`
  - `org.*.worker.*.skill`
  - `org.*.session.*.message.request`
  - `org.*.session.*.message.stream`
  - `org.*.session.*.message.completed`

## Test Plan

### `backend/pkg/messaging`

- command builder JSON shape。
- `DecodeCommandPayload[T]` 成功和失败路径。
- flat command-specific fields 不再出现在 `WorkerCommandBody`。

### Worker Dispatcher

- run/control/interaction/skill lane 分发正确。
- skill request/reply 能返回 `WorkerCommandResult`。
- cancel 不进入 run debounce。

### Taskconsumer

- 同 session 多条 run command 仍保持 debounce merge。
- run command payload 缺字段时返回明确错误。

### Server Projector 和 Replay

- state lane 能独立完成 started/artifact/terminal 落库。
- stream/state replay 能按 run `Seq` 去重和恢复顺序。
- 重复 state event 不产生重复 assistant message 或重复 artifact。

### 清理验证

- `rg "internal/worker/protocol" backend/internal backend/pkg` 无业务引用。
- `rg "pkg/dm|dm\\." backend/internal backend/pkg` 无新链路引用。
- worker 启动代码不再调用旧 subscriber start 方法。

建议执行：

```bash
go test ./backend/pkg/messaging ./backend/internal/worker/... ./backend/internal/service ./backend/internal/runnable
go test ./...
```

## Assumptions

- 已落地的 `pkg/messaging`、dispatcher 初版、state projector 初版作为基础继续改，不重新设计整个方案。
- 本轮目标是完成收敛和修正 command payload 建模，不扩大到 agent 内部事件层级简化。
- 幂等问题只处理本次消息投影直接相关的重复消费，不覆盖后续更广义的消息落库治理。
