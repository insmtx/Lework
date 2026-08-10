package messaging

// ---- Worker 运维状态查询（Core NATS request/reply） ----

// WorkerStatusRequest 是 Server -> Worker 的运维状态查询请求。
//
// 该查询使用 Core NATS request/reply（临时 inbox），不写入 JetStream,
// 避免运维轮询污染任务队列。Worker 依据请求中的 org/worker 归属回答本地运行快照。
type WorkerStatusRequest struct {
	OrgID    uint `json:"org_id"`
	WorkerID uint `json:"worker_id"`
}

// WorkerRunSummary 是一次运行/等待中任务的轻量摘要。
//
// 只包含定位与生命周期字段，不携带 prompt、模型配置、环境变量或原始命令，
// 避免运维接口泄露敏感信息。
type WorkerRunSummary struct {
	RunID     string `json:"run_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	CommandID string `json:"command_id,omitempty"`
	StreamSeq uint64 `json:"stream_seq,omitempty"`
	// Status 表示该任务当前所处阶段：running 或 waiting。
	Status string `json:"status,omitempty"`
	// CreatedAt / UpdatedAt / StartedAt 均为 Unix 秒；StartedAt 零值表示尚未开始。
	CreatedAt int64 `json:"created_at,omitempty"`
	UpdatedAt int64 `json:"updated_at,omitempty"`
	StartedAt int64 `json:"started_at,omitempty"`
}

// WorkerStatusSnapshot 是 Worker 返回的本地运行状态快照。
//
// waiting_count 仅统计 Worker 已接收但尚未开始执行的本地任务
// （经 debounce 合并后进入 per-session 队列或等待计算槽的批次）；
// NATS 尚未投递到 Worker 的积压不计入该字段。
type WorkerStatusSnapshot struct {
	// OrgID / WorkerID 标识生成该快照的 Worker，用于 Server 校验回复归属。
	OrgID          uint `json:"org_id"`
	WorkerID       uint `json:"worker_id"`
	MaxConcurrency int  `json:"max_concurrency"`
	// RunningCount 已启动但尚未结束的 Runtime 数量；交互等待中的 Runtime 也计入。
	RunningCount int `json:"running_count"`
	// WaitingCount 已接收但尚未开始 Runtime 执行的本地 Run 数量。
	WaitingCount int `json:"waiting_count"`
	// DebounceWaitingCount 仍处于 debounce 窗口内的 Run 数量。
	DebounceWaitingCount int `json:"debounce_waiting_count"`
	// CoordinatorWaitingCount 已进入 Coordinator 队列或正在等待计算槽的 Run 数量。
	CoordinatorWaitingCount int `json:"coordinator_waiting_count"`
	// AdmissionWaitingCount 被 Worker 准入 semaphore 实际阻塞的消息数量。
	AdmissionWaitingCount int `json:"admission_waiting_count"`
	// AcceptedCount Worker 已持久化并由当前进程拥有的命令数量，不含尚未准入的消息。
	AcceptedCount int `json:"accepted_count"`
	// ComputeBusyCount 当前占用计算槽的 Runtime 数量。
	ComputeBusyCount int `json:"compute_busy_count"`
	// InteractionWaitingCount 当前处于审批/问答等交互等待的 Runtime 数量。
	InteractionWaitingCount int `json:"interaction_waiting_count"`
	// InboxPendingCount 持久化 inbox 中处于 pending 状态的记录数。
	InboxPendingCount int `json:"inbox_pending_count"`
	// InboxProcessingCount 持久化 inbox 中处于 processing 状态的记录数。
	InboxProcessingCount int `json:"inbox_processing_count"`
	// SnapshotAt 快照生成时间（Unix 秒）。
	SnapshotAt int64 `json:"snapshot_at"`
	// Degraded 表示快照有局部数据源不可用；Errors 只返回稳定错误码，不暴露底层细节。
	Degraded bool     `json:"degraded,omitempty"`
	Errors   []string `json:"errors,omitempty"`

	// RunningTasks 正在执行的任务摘要。
	RunningTasks []WorkerRunSummary `json:"running_tasks,omitempty"`
	// WaitingTasks 等待执行的任务摘要；最多返回 maxWaitingTasks 条。
	WaitingTasks []WorkerRunSummary `json:"waiting_tasks,omitempty"`
	// WaitingTruncated 表示等待任务摘要因数量上限被截断。
	WaitingTruncated bool `json:"waiting_truncated,omitempty"`
}

// MaxWaitingTasks 是等待任务摘要返回条数上限。
const MaxWaitingTasks = 100
