// Package messaging 是 server、worker 共同依赖的消息契约层。
//
// 本包定义统一的消息信封、命令结构、事件结构、subject 生成和 JetStream stream 配置，
// 使 server 和 worker 不再各自维护独立的协议定义和 subject 拼接逻辑。
//
// 按 QoS 分 lane 的物理 subject 设计：
//
//	Server -> Worker:
//	  cmd.run         - 会话/task 执行命令，保留 session-keyed debounce
//	  cmd.control     - cancel run 等控制命令，不经过防抖
//	  cmd.interaction - approval resolve、question answer
//	  cmd.skill       - skill install/list/detail/import/uninstall，request/reply
//
//	Worker -> Server/UI:
//	  run.stream      - 高频 SSE 增量（message delta、reasoning delta、tool delta）
//	  run.state       - 低频关键状态（run.started、artifact.declared、approval/question、terminal）
package messaging

import (
	"encoding/json"
	"time"
)

// MessageType 表示消息的顶层类型标识。
type MessageType string

const (
	// MessageTypeWorkerCommand 统一 server -> worker 命令。
	MessageTypeWorkerCommand MessageType = "worker.command"
	// MessageTypeRunEvent 统一 worker -> server/UI 运行事件。
	MessageTypeRunEvent MessageType = "run.event"
)

// TraceContext 携带跨越 UI、Server、Worker、Runtime 的分布式追踪标识。
type TraceContext struct {
	TraceID   string `json:"trace_id"`
	RequestID string `json:"request_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	ParentID  string `json:"parent_id,omitempty"`
}

// RouteContext 携带消息路由信息，用于消息投递和租户隔离。
type RouteContext struct {
	OrgID     uint   `json:"org_id"`
	SessionID string `json:"session_id,omitempty"`
	WorkerID  uint   `json:"worker_id,omitempty"`
}

// Envelope 是通用消息信封，用于所有 MQ topic 上的消息传输。
type Envelope[T any] struct {
	ID        string      `json:"id"`
	Type      MessageType `json:"type"`
	CreatedAt time.Time   `json:"created_at"`

	Trace TraceContext `json:"trace"`
	Route RouteContext `json:"route"`

	Body     T               `json:"body"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}
