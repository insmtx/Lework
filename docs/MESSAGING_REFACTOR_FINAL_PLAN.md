# 消息通信改造：收尾计划

## Summary

前面大部分改造已完成（统一协议 `pkg/messaging`、cmddispatcher 分发、WorkerCommandBody 改为 `CommandType+Payload`、state projector 收敛、旧 `protocol/` 包删除、旧 subscriber 停用）。本计划只覆盖两项剩余工作：

1. **修复 `StreamStartSeq` 写入错误** —— 当前 `run.started` 投影将 state lane 的 NATS seq 同时写入 `StreamStartSeq` 和 `StateStartSeq`，导致 SSE replay 用错误的序列号订阅 `run.stream`。
2. **删除 `pkg/dm`** —— 代码已无引用，目录仍在磁盘上。

## Key Changes

### 1. 新增 stream lane 投影，修正 StreamStartSeq

- 新建 `backend/internal/runnable/session_run_stream_projector.go`
- 订阅 `org.*.session.*.run.stream` 通配符，使用 ephemeral consumer。
- 收到该 session 的第一个 `run.stream` 事件时，将 NATS stream sequence 写入处理中用户消息的 `response_stream_start_seq`。
- 由于流事件的 NATS consumer 可能比 state projector 晚到达（不同 goroutine），投影需要幂等检查：如果消息已有 `response_stream_start_seq` 就不再覆盖。
- `HandleSessionRunStarted` 移除 `StreamStartSeq` 必填校验；改为 stream projector 异步设置。
- `session_run_state_projector.go` 中 `handleRunStartedEvent` 不再写入 `StreamStartSeq`（只写 `StateStartSeq`）。

**涉及文件：**
- `backend/internal/runnable/session_run_stream_projector.go`（新建）
- `backend/internal/runnable/session_run_state_projector.go`（修改）
- `backend/internal/api/router.go`（启动 stream projector）
- `backend/internal/service/session_service.go`（移除 StreamStartSeq 必填）

### 2. 删除 `pkg/dm` 包

- 确认无任何 `*.go` 文件引用 `backend/pkg/dm`（已验证）。
- 删除 `backend/pkg/dm/` 目录及其中 `consumer.go`、`stream.go`、`subject.go`。

## Test Plan

- `go test ./backend/pkg/messaging/...` — 确保原有协议测试通过
- `go test ./backend/internal/service/...` — 验证 session service 的 StreamStartSeq 变更不影响已有逻辑
- `go test ./backend/internal/worker/...` — 验证 worker 端不受影响
- 确认删除 `pkg/dm` 后 `go vet ./backend/...` 无错误

## Assumptions

- `run.started` 始终先于第一条 `message.delta` 发布，因此 stream projector 收到第一个 stream 事件时，处理中消息的 metadata 已创建。
- 若 stream 事件先于 state 事件落库（极端情况），`HandleSessionRunStarted` 改为容忍 `StreamStartSeq=0`，stream projector 仍能独立写入。
- `http.ListenAndServe` 的测试错误（`bind: operation not permitted`）和 SQLite migration 语法错误是独立预存问题，不在本计划范围内。
