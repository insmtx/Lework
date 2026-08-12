// Package run 提供 worker agent run 命令的 cmd.run lane handler。
//
// Handler 实现了基于持久化 inbox 的异步分发，提供 at-least-once 的崩溃恢复语义。
// 消息经过校验、持久化后，在后台 goroutine 中分发给 RunCoordinator，
// 使得 NATS Ack 可以立即返回，不阻塞消息确认。
//
// 确认决策：
//   - 永久错误（payload、route、model 校验失败）→ Term
//   - inbox 写入失败 → NakWithDelay(5s)，等待重试
//   - 持久化成功并启动后台 goroutine → Ack
//   - 重启后通过 RecoverNonTerminal 恢复未完成的任务
package run

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/worker/agentrun"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/internal/worker/command/run/inbox"
	"github.com/insmtx/Leros/backend/internal/worker/eventpub"
	runcoord "github.com/insmtx/Leros/backend/internal/worker/run"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/ygpkg/yg-go/logs"
)

var errAdmissionClosed = fmt.Errorf("admission closed")

const (
	defaultMaxConcurrency     = 10
	defaultDebounceWindow     = 1500 * time.Millisecond
	inboxRetention            = 72 * time.Hour
	semInProgressInterval     = 15 * time.Second
	inboxTerminalTimeout      = 5 * time.Second
	defaultMaxInflight        = 20
	defaultInteractionWaits   = 10
	defaultInteractionTimeout = 10 * time.Minute
	defaultMaxQueuedCommands  = 1000
	defaultQueueRetry         = 15 * time.Second
	defaultQueueStartTimeout  = 30 * time.Minute
	defaultMaxRunDuration     = 4 * time.Hour
)

// Config controls a worker run handler.
type Config struct {
	OrgID          uint
	WorkerID       uint
	Env            string
	MaxConcurrency int
	MaxInflight    int
	DebounceWindow time.Duration
	// MaxInteractionWaits 最大并发交互等待数量。
	MaxInteractionWaits int
	// InteractionWaitTimeout 审批/问题等待默认硬超时。
	InteractionWaitTimeout time.Duration
	// MaxQueuedCommands limits non-terminal durable inbox records. It protects admission without blocking NATS callbacks.
	MaxQueuedCommands int
	QueueRetry        time.Duration
	QueueStartTimeout time.Duration
	MaxRunDuration    time.Duration
	InboxDBPath       string // required
}

// runTask is the internal expanded task representation.
type runTask struct {
	ID           string
	CreatedAt    time.Time
	Trace        messaging.TraceContext
	Route        messaging.RouteContext
	DeliverySeqs []uint64

	TaskType      messaging.TaskType
	ExecutionMode string
	Actor         messaging.ActorContext
	Execution     messaging.ExecutionTarget
	Workspace     messaging.WorkspaceOptions
	Project       messaging.ProjectContext
	Input         messaging.TaskInput
	Model         messaging.ModelOptions
	Runtime       messaging.RuntimeOptions
	Policy        messaging.TaskPolicy
	Plugins       []messaging.PluginSnapshot

	// 业务主键 ID，从 RunCommandPayload 直接透传，用于 llm_history 关联。
	ProjectID   uint
	SessionID   uint
	MessageID   uint
	AssistantID uint
	Uin         uint

	// NotAfter Worker 最晚允许开始时间（零值表示不限制）。
	NotAfter time.Time

	// 客户端 IP，从 RouteContext 透传，用于 llm_history 关联。
	ClientIP string
}

// Handler receives run commands and dispatches them asynchronously to the RunCoordinator.
type Handler struct {
	cfg       Config
	publisher eventbus.Publisher

	coordinator *runcoord.Coordinator
	runInbox    inbox.RunInbox

	// Admission semaphore limits concurrent inflight submissions.
	sem chan struct{}

	// inflight tracks local inbox records currently owned by this process.
	// Stream sequences can be reused after a JetStream stream is recreated.
	inflight map[string]struct{}

	// stateMu protects admissionOpen and inflight map.
	stateMu       sync.Mutex
	admissionOpen bool

	// execCtx is the root context for all async goroutines.
	execCtx    context.Context
	execCancel context.CancelFunc

	// submissions tracks inflight async dispatches for graceful drain.
	submissions sync.WaitGroup

	// recoveryWG tracks the recovery feeder goroutine.
	recoveryWG sync.WaitGroup

	// admissionWaiters 统计当前阻塞在准入 semaphore 上等待槽位的 goroutine 数量，
	// 供运维状态查询展示。
	admissionWaiters atomic.Int64

	// admissionStopped wakes admission waiters and the recovery feeder during shutdown.
	admissionStopped chan struct{}
	stopOnce         sync.Once
}

// New creates a worker run handler backed by the agentrun.Service through a Coordinator.
// InboxDBPath is required — the handler must not operate without a durable inbox.
func New(cfg Config, pub eventbus.Publisher, agentRunSvc *agentrun.Service) (*Handler, error) {
	if cfg.OrgID == 0 {
		return nil, fmt.Errorf("worker org_id is required")
	}
	if cfg.WorkerID == 0 {
		return nil, fmt.Errorf("worker worker_id is required")
	}
	if pub == nil {
		return nil, fmt.Errorf("publisher is required")
	}
	if agentRunSvc == nil {
		return nil, fmt.Errorf("agent run service is required")
	}
	if strings.TrimSpace(cfg.InboxDBPath) == "" {
		return nil, fmt.Errorf("inbox DB path is required")
	}

	maxConc := cfg.MaxConcurrency
	if maxConc <= 0 {
		maxConc = defaultMaxConcurrency
	}
	window := cfg.DebounceWindow
	if window <= 0 {
		window = defaultDebounceWindow
	}
	maxInflight := cfg.MaxInflight
	if maxInflight <= 0 {
		maxInflight = defaultMaxInflight
	}
	if maxInflight < maxConc {
		maxInflight = maxConc
	}
	maxInteractionWaits := cfg.MaxInteractionWaits
	if maxInteractionWaits <= 0 {
		maxInteractionWaits = defaultInteractionWaits
	}
	interactionTimeout := cfg.InteractionWaitTimeout
	if interactionTimeout <= 0 {
		interactionTimeout = defaultInteractionTimeout
	}
	if cfg.MaxQueuedCommands <= 0 {
		cfg.MaxQueuedCommands = defaultMaxQueuedCommands
	}
	if cfg.QueueRetry <= 0 {
		cfg.QueueRetry = defaultQueueRetry
	}
	if cfg.QueueStartTimeout <= 0 {
		cfg.QueueStartTimeout = defaultQueueStartTimeout
	}
	if cfg.MaxRunDuration <= 0 {
		cfg.MaxRunDuration = defaultMaxRunDuration
	}

	ri, err := inbox.NewSQLiteRunInbox(cfg.InboxDBPath)
	if err != nil {
		return nil, fmt.Errorf("create run inbox: %w", err)
	}

	execCtx, execCancel := context.WithCancel(context.Background())

	h := &Handler{
		cfg:              cfg,
		publisher:        pub,
		runInbox:         ri,
		sem:              make(chan struct{}, maxInflight),
		inflight:         make(map[string]struct{}),
		execCtx:          execCtx,
		execCancel:       execCancel,
		admissionOpen:    true,
		admissionStopped: make(chan struct{}),
	}

	coord, err := runcoord.NewCoordinator(runcoord.Config{
		MaxConcurrency:         maxConc,
		MaxInflight:            maxInflight,
		MaxInteractionWaits:    maxInteractionWaits,
		InteractionWaitTimeout: interactionTimeout,
		DebounceWindow:         window,
		MaxRunDuration:         cfg.MaxRunDuration,
	}, h.executeSubmission(agentRunSvc))
	if err != nil {
		execCancel()
		ri.Close()
		return nil, err
	}
	h.coordinator = coord
	return h, nil
}

// executeSubmission returns an ExecuteFunc that wraps agentrun.Service.Run.
func (h *Handler) executeSubmission(svc *agentrun.Service) runcoord.ExecuteFunc {
	return func(ctx context.Context, sub runcoord.RunSubmission) (*agentrundomain.RunResult, error) {
		ec := sub.EventContext
		publisher := eventpub.NewNATSEventPublisher(h.publisher)

		if sub.Request == nil {
			return nil, fmt.Errorf("submission request is nil")
		}

		logs.InfoContextf(ctx,
			"Starting worker task run: task_id=%s run_id=%s runtime=%s assistant_id=%s",
			sub.Request.TaskID, sub.Request.RunID,
			sub.Request.Runtime.Kind, sub.Request.Assistant.PublicID,
		)

		return svc.Run(ctx, sub.Request, agentrun.EventContext{
			OrgID:             ec.OrgID,
			WorkerID:          ec.WorkerID,
			WorkerPublicID:    ec.WorkerPublicID,
			SessionID:         ec.SessionID,
			AssistantID:       sub.Request.Assistant.ID,
			AssistantPublicID: sub.Request.Assistant.PublicID,
			TraceID:           ec.TraceID,
			RequestID:         ec.RequestID,
			TaskID:            ec.TaskID,
			RunID:             ec.RunID,
			ParentID:          ec.ParentID,
			ReplyToMessageIDs: ec.ReplyToMessageIDs,
			ClientIP:          ec.ClientIP,
		}, publisher)
	}
}

// RunSubject returns the NATS subject for this handler's cmd.run lane.
func (h *Handler) RunSubject() string {
	topic, _ := messaging.WorkerCommandSubject(h.cfg.OrgID, h.cfg.WorkerID, messaging.LaneRun)
	return topic
}

// HandleRunCommand 处理 run 命令，使用 ManualDelivery 手动控制确认时机。
//
// 确认决策：
//   - 永久错误（payload、route、model 校验失败）→ Term，不再重试
//   - inbox 写入失败 → NakWithDelay(5s)，请求 NATS 延迟重投
//   - 持久化成功 + 注册 inflight + 启动后台 goroutine → Ack，异步执行
func (h *Handler) HandleRunCommand(ctx context.Context, cmd messaging.WorkerCommand, delivery eventbus.ManualDelivery) error {
	payload, err := messaging.DecodeCommandPayload[messaging.RunCommandPayload](&cmd.Body)
	if err != nil {
		_ = delivery.Term()
		return fmt.Errorf("run command payload decode: %w", err)
	}

	task := runTask{
		ID:            cmd.ID,
		CreatedAt:     cmd.CreatedAt,
		Trace:         cmd.Trace,
		Route:         cmd.Route,
		TaskType:      payload.TaskType,
		ExecutionMode: payload.ExecutionMode,
		Actor:         payload.Actor,
		Execution:     payload.Execution,
		Workspace:     payload.Workspace,
		Project:       payload.Project,
		Input:         payload.Input,
		Model:         payload.Model,
		Runtime:       payload.Runtime,
		Policy:        payload.Policy,
		Plugins:       append([]messaging.PluginSnapshot(nil), payload.Plugins...),
		ProjectID:     payload.ProjectID,
		SessionID:     payload.SessionID,
		MessageID:     payload.MessageID,
		AssistantID:   payload.AssistantID,
		Uin:           payload.Uin,
		NotAfter:      parseRunNotAfter(payload.NotAfter),
		ClientIP:      cmd.Route.ClientIP,
	}

	nrm, routeErr := h.normalizeRunRoute(task)
	if routeErr != nil {
		_ = delivery.Term()
		h.logRouteReject(task, routeErr)
		return routeErr
	}
	task = nrm

	if err := h.validateRouteTask(task); err != nil {
		_ = delivery.Term()
		h.logRouteReject(task, err)
		return err
	}
	if task.TaskType != messaging.TaskTypeAgentRun {
		_ = delivery.Term()
		return fmt.Errorf("unsupported task type %q", task.TaskType)
	}
	if err := validateModelConfig(task.Model); err != nil {
		_ = delivery.Term()
		return err
	}

	// 自动化命令过期检查（覆盖实时投递与崩溃恢复两条路径）
	if !task.NotAfter.IsZero() && time.Now().After(task.NotAfter) {
		_ = delivery.Term()
		return fmt.Errorf("run command expired (not_after=%s)", task.NotAfter.Format(time.RFC3339))
	}

	// Get metadata for stream seq.
	var seq uint64
	if meta, err := delivery.Metadata(); err == nil && meta != nil {
		seq = meta.Stream
	}
	if seq == 0 {
		// Cannot track without a seq — request redelivery with delay.
		_ = delivery.NakWithDelay(5 * time.Second)
		return fmt.Errorf("no stream seq in message metadata")
	}

	topic := h.RunSubject()

	// Unified admission: acquire sem → persist → check admission → register inflight → WaitGroup.Add → goroutine → Ack.
	return h.admit(ctx, topic, seq, cmd, task, delivery)
}

// admit 是统一的消息准入通道，同时服务于实时投递和崩溃恢复两种场景。
//
// NATS callback only validates, persists, registers ownership and ACKs. Execution-slot waiting happens later
// in dispatchAsync, so one blocked Session cannot hold the subscription callback or prevent later messages admission.
func (h *Handler) admit(ctx context.Context, topic string, seq uint64, cmd messaging.WorkerCommand, task runTask, delivery eventbus.ManualDelivery) error {
	pending, err := h.runInbox.CountByStatus(ctx, topic, inbox.StatusPending)
	if err != nil {
		_ = delivery.NakWithDelay(h.cfg.QueueRetry)
		return fmt.Errorf("count pending inbox records: %w", err)
	}
	processing, err := h.runInbox.CountByStatus(ctx, topic, inbox.StatusProcessing)
	if err != nil {
		_ = delivery.NakWithDelay(h.cfg.QueueRetry)
		return fmt.Errorf("count processing inbox records: %w", err)
	}
	if pending+processing >= h.cfg.MaxQueuedCommands {
		_ = delivery.NakWithDelay(h.cfg.QueueRetry)
		return fmt.Errorf("run inbox full: queued=%d limit=%d", pending+processing, h.cfg.MaxQueuedCommands)
	}

	inserted, record, err := h.runInbox.PutIfAbsent(ctx, topic, seq, cmd)
	if err != nil {
		_ = delivery.NakWithDelay(h.cfg.QueueRetry)
		return fmt.Errorf("inbox PutIfAbsent: %w", err)
	}

	if !inserted {
		if record == nil {
			_ = delivery.NakWithDelay(h.cfg.QueueRetry)
			// The SQLite implementation treats this as a storage inconsistency and
			// returns an error. Keep the callback non-terminal so JetStream can
			// redeliver instead of accidentally scheduling an unknown duplicate.
			return nil
		}
		// command_id is the execution identity. A redelivery with a different
		// stream sequence must never create a second execution.
		if record.IsTerminal() {
			h.ack(ctx, delivery)
			return nil
		}

		ikey := inboxKey(record.ID)
		h.stateMu.Lock()
		_, owned := h.inflight[ikey]
		if owned {
			h.stateMu.Unlock()
			h.ack(ctx, delivery)
			return nil
		}
		// Stale record — this process will own it now.
		if !h.admissionOpen {
			h.stateMu.Unlock()
			_ = delivery.NakWithDelay(h.cfg.QueueRetry)
			return errAdmissionClosed
		}
		h.inflight[ikey] = struct{}{}
		h.submissions.Add(1)
		h.stateMu.Unlock()
		// Continue processing the original persisted record, not this delivery.
		topic, seq = record.Topic, record.StreamSeq
	} else {
		// New record — register under state lock.
		ikey := inboxKey(record.ID)
		h.stateMu.Lock()
		if !h.admissionOpen {
			h.stateMu.Unlock()
			_ = delivery.NakWithDelay(h.cfg.QueueRetry)
			return errAdmissionClosed
		}
		h.inflight[ikey] = struct{}{}
		h.submissions.Add(1)
		h.stateMu.Unlock()
	}

	if seq != 0 {
		task.DeliverySeqs = []uint64{seq}
	}
	if task.NotAfter.IsZero() {
		task.NotAfter = task.CreatedAt.Add(h.cfg.QueueStartTimeout)
	}

	logs.InfoContextf(ctx,
		"Received run command: msg_id=%s task_id=%s run_id=%s org_id=%d worker_id=%d session_id=%s task_type=%s seq=%d",
		task.ID, task.Trace.TaskID, task.Trace.RunID,
		task.Route.OrgID, task.Route.WorkerID, task.Route.SessionID,
		task.TaskType, seq,
	)

	// 5. Start goroutine then Ack.
	go h.dispatchAsync(task, record.ID, inboxKey(record.ID))
	h.ack(ctx, delivery)

	return nil
}

// acquireSem acquires a semaphore slot, calling InProgress periodically.
// acquireSem 获取 admission semaphore 的一个槽位。
// 当 semaphore 已满时，每 15 秒发送一次 InProgress 心跳，
// 防止 NATS 因等待确认超时而重新投递消息。
// 同时监听 admissionStopped 通道以响应优雅关闭。
func (h *Handler) acquireSem(ctx context.Context, delivery eventbus.ManualDelivery) error {
	start := time.Now()
	logs.InfoContextf(ctx, "admission acquiring: in_flight=%d cap=%d",
		len(h.sem), cap(h.sem))
	// 先尝试无阻塞获取。只有容量已满、确实进入等待后才计入 admissionWaiters。
	select {
	case <-h.admissionStopped:
		return errAdmissionClosed
	case h.sem <- struct{}{}:
		logs.InfoContextf(ctx, "admission acquired: in_flight=%d cap=%d waited_ms=0", len(h.sem), cap(h.sem))
		return nil
	default:
	}

	h.admissionWaiters.Add(1)
	defer h.admissionWaiters.Add(-1)
	for {
		if err := delivery.InProgress(); err != nil {
			return fmt.Errorf("nats in-progress: %w", err)
		}
		select {
		case <-h.admissionStopped:
			return fmt.Errorf("admission closed")
		case h.sem <- struct{}{}:
			logs.InfoContextf(ctx, "admission acquired: in_flight=%d cap=%d waited_ms=%d",
				len(h.sem), cap(h.sem), time.Since(start).Milliseconds())
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(semInProgressInterval):
		}
	}
}

func (h *Handler) releaseAdmission() {
	<-h.sem
}

// dispatchAsync 构建 RunSubmission 并调用 Coordinator.Submit 异步执行。
// 在后台 goroutine 中运行，执行完成后更新 inbox 状态为 completed 或 failed。
func (h *Handler) dispatchAsync(task runTask, recordID uint64, iKey string) {
	defer h.submissions.Done()
	defer h.releaseInflight(iKey)

	execCtx := withRunLogFields(h.execCtx, task)
	if err := h.acquireBackgroundAdmission(execCtx, task.NotAfter); err != nil {
		if err == errAdmissionClosed {
			// Shutdown must leave durable work recoverable; do not turn queued work
			// into a terminal failure merely because this process is stopping.
			return
		}
		h.markTerminal(execCtx, recordID, err, false)
		return
	}
	defer h.releaseAdmission()
	if err := h.runInbox.MarkProcessing(execCtx, recordID); err != nil {
		logs.WarnContextf(execCtx, "mark inbox processing record_id=%d: %v", recordID, err)
	}

	req := RequestFromWorkerTask(task)
	submission := runcoord.RunSubmission{
		Request: req,
		EventContext: agentrun.EventContext{
			OrgID:             task.Route.OrgID,
			WorkerID:          task.Route.WorkerID,
			WorkerPublicID:    task.Route.WorkerPublicID,
			SessionID:         task.Route.SessionID,
			AssistantID:       req.Assistant.ID,
			AssistantPublicID: req.Assistant.PublicID,
			TraceID:           task.Trace.TraceID,
			RequestID:         task.Trace.RequestID,
			TaskID:            task.Trace.TaskID,
			RunID:             task.Trace.RunID,
			ParentID:          task.Trace.ParentID,
			ReplyToMessageIDs: replyToMessageIDs(task.Input.Messages),
			MemberCommandIDs:  []string{task.ID},
			ClientIP:          task.Route.ClientIP,
		},
		DeliverySeqs: task.DeliverySeqs,
		NotAfter:     task.NotAfter,
	}

	logs.InfoContextf(execCtx, "dispatching run: run_id=%s session_id=%s worker_id=%d org_id=%d",
		task.Trace.RunID, task.Route.SessionID, task.Route.WorkerID, task.Route.OrgID)
	_, execErr := h.coordinator.Submit(execCtx, submission)

	h.markTerminal(execCtx, recordID, execErr, false)

	if execErr != nil {
		logs.WarnContextf(execCtx, "Run command execution error: msg_id=%s task_id=%s run_id=%s session_id=%s: %v",
			task.ID, task.Trace.TaskID, task.Trace.RunID, task.Route.SessionID, execErr)
	}
}

func (h *Handler) acquireBackgroundAdmission(ctx context.Context, notAfter time.Time) error {
	var deadline <-chan time.Time
	if !notAfter.IsZero() {
		delay := time.Until(notAfter)
		if delay <= 0 {
			return fmt.Errorf("queue_start_timeout")
		}
		timer := time.NewTimer(delay)
		defer timer.Stop()
		deadline = timer.C
	}
	select {
	case h.sem <- struct{}{}:
		return nil
	default:
	}
	h.admissionWaiters.Add(1)
	defer h.admissionWaiters.Add(-1)
	select {
	case h.sem <- struct{}{}:
		return nil
	case <-h.admissionStopped:
		return errAdmissionClosed
	case <-ctx.Done():
		return ctx.Err()
	case <-deadline:
		return fmt.Errorf("queue_start_timeout")
	}
}

func withRunLogFields(ctx context.Context, task runTask) context.Context {
	fields := make([]interface{}, 0, 10)
	if task.Trace.ReqID != "" {
		fields = append(fields, "req_id", task.Trace.ReqID)
	}
	if task.Trace.RunID != "" {
		fields = append(fields, "run_id", task.Trace.RunID)
	}
	if task.Route.SessionID != "" {
		fields = append(fields, "session_id", task.Route.SessionID)
	}
	if task.Route.AssistantID != 0 {
		fields = append(fields, "assistant_id", task.Route.AssistantID)
	}
	if task.Route.WorkerID != 0 {
		fields = append(fields, "worker_id", task.Route.WorkerID)
	}
	if len(fields) == 0 {
		return ctx
	}
	return logs.WithContextFields(ctx, fields...)
}

// RecoverNonTerminal 加载当前 topic 下所有非终态的 inbox 记录，
// 将它们注册为 owned（防止重复投递），然后启动后台 feeder goroutine
// 通过 semaphore 逐个恢复执行。
//
// 必须在 NATS 订阅开始前调用，确保崩溃恢复的消息不会被实时消息插队。
func (h *Handler) RecoverNonTerminal(ctx context.Context) error {
	topic := h.RunSubject()

	// Clean up old terminal records.
	if _, err := h.runInbox.DeleteTerminalBefore(ctx, topic, time.Now().Add(-inboxRetention)); err != nil {
		logs.WarnContextf(ctx, "Failed to clean old inbox records: %v", err)
	}

	if err := h.runInbox.ResetProcessing(ctx, topic); err != nil {
		return fmt.Errorf("reset interrupted inbox records: %w", err)
	}
	records, err := h.runInbox.GetNonTerminal(ctx, topic)
	if err != nil {
		return fmt.Errorf("get non-terminal inbox records: %w", err)
	}

	if len(records) == 0 {
		return nil
	}

	logs.InfoContextf(ctx, "Recovering %d non-terminal inbox records for topic %s", len(records), topic)

	// Register all records as owned before starting the feeder.
	h.stateMu.Lock()
	for _, rec := range records {
		ikey := inboxKey(rec.ID)
		h.inflight[ikey] = struct{}{}
	}
	h.stateMu.Unlock()

	// Start the recovery feeder goroutine.
	h.recoveryWG.Add(1)
	go h.runRecoveryFeeder(records)

	return nil
}

// runRecoveryFeeder 将恢复的 inbox 记录逐个通过 semaphore 调度执行。
// 每次从 semaphore 获取一个槽位后启动一个 goroutine 处理记录，
// 与实时投递共享同一套并发限流和生命周期管理。
func (h *Handler) runRecoveryFeeder(records []inbox.Record) {
	defer h.recoveryWG.Done()

	for _, rec := range records {
		// Acquire semaphore slot.
		select {
		case h.sem <- struct{}{}:
		case <-h.admissionStopped:
			return
		case <-h.execCtx.Done():
			return
		}

		// Check admission is still open.
		h.stateMu.Lock()
		if !h.admissionOpen {
			h.stateMu.Unlock()
			<-h.sem
			return
		}
		h.submissions.Add(1)
		h.stateMu.Unlock()

		ikey := inboxKey(rec.ID)

		go h.recoverRecord(rec, ikey)
	}
}

// recoverRecord 处理一条恢复的 inbox 记录：反序列化命令、校验 payload、
// 构建任务并提交给 Coordinator。执行完成后更新 inbox 状态。
func (h *Handler) recoverRecord(rec inbox.Record, ikey string) {
	defer h.submissions.Done()
	defer h.releaseAdmission()
	defer h.releaseInflight(ikey)

	var cmd messaging.WorkerCommand
	if err := json.Unmarshal([]byte(rec.Command), &cmd); err != nil {
		_ = h.runInbox.MarkFailed(h.execCtx, rec.ID, fmt.Sprintf("recovery unmarshal: %v", err))
		return
	}

	payload, err := messaging.DecodeCommandPayload[messaging.RunCommandPayload](&cmd.Body)
	if err != nil {
		_ = h.runInbox.MarkFailed(h.execCtx, rec.ID, fmt.Sprintf("recovery payload decode: %v", err))
		return
	}

	task := runTask{
		ID:            cmd.ID,
		CreatedAt:     cmd.CreatedAt,
		Trace:         cmd.Trace,
		Route:         cmd.Route,
		TaskType:      payload.TaskType,
		ExecutionMode: payload.ExecutionMode,
		Actor:         payload.Actor,
		Execution:     payload.Execution,
		Workspace:     payload.Workspace,
		Input:         payload.Input,
		Model:         payload.Model,
		Runtime:       payload.Runtime,
		Policy:        payload.Policy,
		Project:       payload.Project,
		Plugins:       append([]messaging.PluginSnapshot(nil), payload.Plugins...),
		ProjectID:     payload.ProjectID,
		SessionID:     payload.SessionID,
		MessageID:     payload.MessageID,
		AssistantID:   payload.AssistantID,
		Uin:           payload.Uin,
		NotAfter:      parseRunNotAfter(payload.NotAfter),
		DeliverySeqs:  []uint64{rec.StreamSeq},
	}
	// 崩溃恢复路径同样执行路由归一化：缺失 session 的记录标记 Failed，
	// 不进入 Coordinator、不启动 Runtime。
	nrm, routeErr := h.normalizeRunRoute(task)
	if routeErr != nil {
		h.logRouteReject(task, routeErr)
		_ = h.runInbox.MarkFailed(h.execCtx, rec.ID, fmt.Sprintf("recovery route invalid: %v", routeErr))
		return
	}
	task = nrm
	if err := h.validateRouteTask(task); err != nil {
		h.logRouteReject(task, err)
		_ = h.runInbox.MarkFailed(h.execCtx, rec.ID, fmt.Sprintf("recovery route invalid: %v", err))
		return
	}

	// 崩溃恢复路径同样执行过期检查：超过 not_after 的已恢复命令不再执行。
	if !task.NotAfter.IsZero() && time.Now().After(task.NotAfter) {
		logs.WarnContextf(h.execCtx, "Recovered run command expired, marking failed: id=%s not_after=%s",
			task.ID, task.NotAfter.Format(time.RFC3339))
		_ = h.runInbox.MarkFailed(h.execCtx, rec.ID, "run command expired (not_after)")
		return
	}

	execCtx := withRunLogFields(h.execCtx, task)

	// Mark processing.
	if err := h.runInbox.MarkProcessing(execCtx, rec.ID); err != nil {
		logs.ErrorContextf(execCtx, "Failed to mark recovered inbox processing: record_id=%d: %v", rec.ID, err)
	}

	logs.InfoContextf(execCtx,
		"Recovering run command: msg_id=%s task_id=%s run_id=%s session_id=%s seq=%d",
		task.ID, task.Trace.TaskID, task.Trace.RunID, task.Route.SessionID, rec.StreamSeq,
	)

	req := RequestFromWorkerTask(task)
	submission := runcoord.RunSubmission{
		Request: req,
		EventContext: agentrun.EventContext{
			OrgID:             task.Route.OrgID,
			WorkerID:          task.Route.WorkerID,
			WorkerPublicID:    task.Route.WorkerPublicID,
			SessionID:         task.Route.SessionID,
			AssistantID:       req.Assistant.ID,
			AssistantPublicID: req.Assistant.PublicID,
			TraceID:           task.Trace.TraceID,
			RequestID:         task.Trace.RequestID,
			TaskID:            task.Trace.TaskID,
			RunID:             task.Trace.RunID,
			ParentID:          task.Trace.ParentID,
			ReplyToMessageIDs: replyToMessageIDs(task.Input.Messages),
			MemberCommandIDs:  []string{task.ID},
			ClientIP:          cmd.Route.ClientIP,
		},
		DeliverySeqs: task.DeliverySeqs,
		NotAfter:     task.NotAfter,
		Recovered:    true,
	}

	_, execErr := h.coordinator.Submit(execCtx, submission)

	h.markTerminal(execCtx, rec.ID, execErr, true)
}

// --- Control command handling ---

// HandleControlCommand 处理 control lane 的控制命令（如 cancel）。
// 控制命令不需要手动确认，handler 同步完成后由 dispatcher 的自动 Ack 机制确认。
func (h *Handler) HandleControlCommand(ctx context.Context, cmd messaging.WorkerCommand) error {
	switch cmd.Body.CommandType {
	case messaging.CommandTypeCancel:
		payload, err := messaging.DecodeCommandPayload[messaging.CancelRunCommandPayload](&cmd.Body)
		if err != nil {
			logs.WarnContextf(ctx, "Failed to decode cancel payload: %v", err)
			return err
		}
		h.coordinator.Cancel(ctx, cmd.Route.OrgID, cmd.Route.WorkerID, cmd.Route.SessionID, payload.RunID)
	default:
		logs.WarnContextf(ctx, "unknown control command type: %s", cmd.Body.CommandType)
	}
	return nil
}

// --- 生命周期管理 ---

// StopAdmission 停止接受新的消息准入。
// 调用后不会有新的 WaitGroup.Add 发生，用于优雅关闭的第一步。
func (h *Handler) StopAdmission() {
	h.stopOnce.Do(func() {
		h.stateMu.Lock()
		h.admissionOpen = false
		close(h.admissionStopped)
		h.stateMu.Unlock()
	})
}

// Drain 等待所有正在执行的后台分发任务（包括恢复 feeder）完成。
// 返回 true 表示在超时前全部完成，false 表示超时。
// 必须在 StopAdmission 之后调用，确保没有新的任务再加入。
func (h *Handler) Drain(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		h.recoveryWG.Wait()
		h.submissions.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Close 关闭 handler，释放资源。必须在 Drain 成功之后调用。
func (h *Handler) Close() error {
	h.execCancel()
	if h.coordinator != nil {
		_ = h.coordinator.Close()
	}
	return h.runInbox.Close()
}

// RunInbox 返回持久化 inbox 实例，用于外部在关闭时访问 inbox 状态。
func (h *Handler) RunInbox() inbox.RunInbox {
	return h.runInbox
}

// Status 返回 Worker 本地运行状态快照，供运维状态查询使用。
//
// 数据来源：
//   - running/waiting 任务清单来自 Coordinator 的调度状态；
//   - command_id、stream_seq、created_at、updated_at 来自持久化 inbox 记录，
//     通过 run_id 关联补齐。
//   - admission_waiting 为当前阻塞在准入 semaphore 上的 goroutine 数；
//   - accepted 为当前拥有的 stream_seq 数（inflight 映射大小）。
//
// 摘要只包含定位与生命周期字段，不携带 prompt、模型配置、环境变量或原始命令。
func (h *Handler) Status(ctx context.Context) messaging.WorkerStatusSnapshot {
	if ctx == nil {
		ctx = context.Background()
	}
	snapshot := messaging.WorkerStatusSnapshot{
		OrgID:      h.cfg.OrgID,
		WorkerID:   h.cfg.WorkerID,
		SnapshotAt: time.Now().UTC().Unix(),
	}

	coord := h.coordinator.Status()
	snapshot.MaxConcurrency = coord.MaxConcurrency
	snapshot.ComputeBusyCount = coord.ComputeBusyCount
	snapshot.InteractionWaitingCount = coord.InteractionWaitingCount
	for _, r := range coord.Running {
		snapshot.RunningTasks = append(snapshot.RunningTasks, messaging.WorkerRunSummary{
			RunID:     r.RunID,
			TaskID:    r.TaskID,
			SessionID: r.SessionID,
			Status:    "running",
			StartedAt: r.StartedAt.Unix(),
		})
	}
	for _, w := range coord.Debouncing {
		snapshot.WaitingTasks = append(snapshot.WaitingTasks, messaging.WorkerRunSummary{
			RunID:     w.RunID,
			TaskID:    w.TaskID,
			SessionID: w.SessionID,
			StreamSeq: firstStreamSeq(w.StreamSeqs),
			Status:    "debouncing",
		})
	}
	snapshot.DebounceWaitingCount = len(coord.Debouncing)
	for _, w := range coord.Waiting {
		snapshot.WaitingTasks = append(snapshot.WaitingTasks, messaging.WorkerRunSummary{
			RunID:     w.RunID,
			TaskID:    w.TaskID,
			SessionID: w.SessionID,
			StreamSeq: firstStreamSeq(w.StreamSeqs),
			Status:    w.Status,
		})
	}
	snapshot.CoordinatorWaitingCount = len(coord.Waiting)
	snapshot.RunningCount = len(snapshot.RunningTasks)
	// WaitingCount 是真实的等待任务总数（不随摘要截断而缩减）；
	// WaitingTasks 只展示前 MaxWaitingTasks 条，超限时置 WaitingTruncated。
	snapshot.WaitingCount = len(snapshot.WaitingTasks)
	if len(snapshot.WaitingTasks) > messaging.MaxWaitingTasks {
		snapshot.WaitingTasks = snapshot.WaitingTasks[:messaging.MaxWaitingTasks]
		snapshot.WaitingTruncated = true
	}

	// 从 inbox 记录补齐 command_id / stream_seq / created_at / updated_at。
	if err := h.enrichTaskSummaries(ctx, &snapshot); err != nil {
		addSnapshotError(&snapshot, "inbox_details_unavailable")
	}

	snapshot.AdmissionWaitingCount = int(h.admissionWaiters.Load())

	h.stateMu.Lock()
	snapshot.AcceptedCount = len(h.inflight)
	h.stateMu.Unlock()

	topic := h.RunSubject()
	if p, err := h.runInbox.CountByStatus(ctx, topic, inbox.StatusPending); err == nil {
		snapshot.InboxPendingCount = p
	} else {
		addSnapshotError(&snapshot, "inbox_pending_count_unavailable")
	}
	if pr, err := h.runInbox.CountByStatus(ctx, topic, inbox.StatusProcessing); err == nil {
		snapshot.InboxProcessingCount = pr
	} else {
		addSnapshotError(&snapshot, "inbox_processing_count_unavailable")
	}

	return snapshot
}

// enrichTaskSummaries 用 inbox 记录补齐运行/等待任务摘要中的 command_id、
// stream_seq、created_at、updated_at 字段。
func (h *Handler) enrichTaskSummaries(ctx context.Context, snapshot *messaging.WorkerStatusSnapshot) error {
	if len(snapshot.RunningTasks) == 0 && len(snapshot.WaitingTasks) == 0 {
		return nil
	}
	records, err := h.runInbox.GetNonTerminal(ctx, h.RunSubject())
	if err != nil {
		logs.WarnContextf(ctx, "status enrich inbox: %v", err)
		return err
	}
	byRun := make(map[string]inbox.Record, len(records))
	for _, rec := range records {
		var cmd messaging.WorkerCommand
		if err := json.Unmarshal([]byte(rec.Command), &cmd); err != nil {
			continue
		}
		if cmd.Trace.RunID == "" {
			continue
		}
		byRun[cmd.Trace.RunID] = rec
	}
	enrich := func(s *messaging.WorkerRunSummary) {
		rec, ok := byRun[s.RunID]
		if !ok {
			return
		}
		var cmd messaging.WorkerCommand
		if err := json.Unmarshal([]byte(rec.Command), &cmd); err == nil {
			s.CommandID = cmd.ID
		}
		s.StreamSeq = rec.StreamSeq
		s.CreatedAt = rec.CreatedAt
		s.UpdatedAt = rec.UpdatedAt
	}
	for i := range snapshot.RunningTasks {
		enrich(&snapshot.RunningTasks[i])
	}
	for i := range snapshot.WaitingTasks {
		enrich(&snapshot.WaitingTasks[i])
	}
	return nil
}

func firstStreamSeq(seqs []uint64) uint64 {
	if len(seqs) == 0 {
		return 0
	}
	return seqs[0]
}

func addSnapshotError(snapshot *messaging.WorkerStatusSnapshot, code string) {
	for _, existing := range snapshot.Errors {
		if existing == code {
			return
		}
	}
	snapshot.Degraded = true
	snapshot.Errors = append(snapshot.Errors, code)
}

// --- 辅助方法 ---

func (h *Handler) releaseInflight(key string) {
	h.stateMu.Lock()
	delete(h.inflight, key)
	h.stateMu.Unlock()
}

func (h *Handler) ack(ctx context.Context, delivery eventbus.ManualDelivery) {
	if err := delivery.Ack(); err != nil {
		logs.WarnContextf(ctx, "Failed to Ack run command: %v", err)
	}
}

func (h *Handler) markTerminal(logCtx context.Context, recordID uint64, execErr error, recovered bool) {
	markCtx, cancel := context.WithTimeout(context.Background(), inboxTerminalTimeout)
	defer cancel()

	source := "live"
	if recovered {
		source = "recovery"
	}
	if execErr != nil {
		if err := h.runInbox.MarkFailed(markCtx, recordID, execErr.Error()); err != nil {
			logs.ErrorContextf(logCtx, "Failed to mark inbox failed: source=%s record_id=%d: %v", source, recordID, err)
			return
		}
		logs.InfoContextf(logCtx, "Marked inbox failed: source=%s record_id=%d", source, recordID)
		return
	}

	if err := h.runInbox.MarkCompleted(markCtx, recordID); err != nil {
		logs.ErrorContextf(logCtx, "Failed to mark inbox completed: source=%s record_id=%d: %v", source, recordID, err)
		return
	}
	logs.InfoContextf(logCtx, "Marked inbox completed: source=%s record_id=%d", source, recordID)
}

func replyToMessageIDs(messages []messaging.ChatMessage) []string {
	ids := make([]string, 0, len(messages))
	seen := make(map[string]struct{}, len(messages))
	for _, m := range messages {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func (h *Handler) validateRouteTask(task runTask) error {
	if task.Route.OrgID != 0 && task.Route.OrgID != h.cfg.OrgID {
		return fmt.Errorf("task org_id %d does not match worker org_id %d", task.Route.OrgID, h.cfg.OrgID)
	}
	if task.Route.WorkerID != 0 && task.Route.WorkerID != h.cfg.WorkerID {
		return fmt.Errorf("task worker_id %d does not match worker_id %d", task.Route.WorkerID, h.cfg.WorkerID)
	}
	return nil
}

// normalizeRunRoute 统一路由归一化：
//   - Route.SessionID 去除首尾空格后必须非空（缺失即拒绝，强制 session-bound）；
//   - Route.OrgID == 0 时补为当前 Handler 配置的 OrgID；
//   - Route.WorkerID == 0 时补为当前 Handler 配置的 WorkerID；
//   - 非零但与当前 Worker 不匹配仍由 validateRouteTask 拒绝。
func (h *Handler) normalizeRunRoute(task runTask) (runTask, error) {
	task.Route.SessionID = strings.TrimSpace(task.Route.SessionID)
	if task.Route.SessionID == "" {
		return task, runcoord.ErrRunRouteRequired
	}
	if task.Route.OrgID == 0 {
		task.Route.OrgID = h.cfg.OrgID
	}
	if task.Route.WorkerID == 0 {
		task.Route.WorkerID = h.cfg.WorkerID
	}
	return task, nil
}

// logRouteReject 结构化记录路由拒绝，包含关联字段但不记录 prompt/token/用户输入。
func (h *Handler) logRouteReject(task runTask, err error) {
	logs.WarnContextf(context.Background(),
		"run route rejected: run_id=%s task_id=%s org_id=%d worker_id=%d session_present=%v reason=%v",
		task.Trace.RunID, task.Trace.TaskID, task.Route.OrgID, task.Route.WorkerID,
		strings.TrimSpace(task.Route.SessionID) != "", err,
	)
}

func validateModelConfig(model messaging.ModelOptions) error {
	if strings.TrimSpace(model.Provider) == "" {
		return fmt.Errorf("llm provider is required")
	}
	if strings.TrimSpace(model.Model) == "" {
		return fmt.Errorf("llm model is required")
	}
	if strings.TrimSpace(model.APIKey) == "" {
		return fmt.Errorf("llm api_key is required")
	}
	return nil
}

func inboxKey(recordID uint64) string {
	return fmt.Sprintf("inbox:%d", recordID)
}

// parseRunNotAfter 解析命令 payload 中的 not_after（RFC3339）；解析失败返回零值（不限制）。
func parseRunNotAfter(s string) time.Time {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}
	}
	return t
}
