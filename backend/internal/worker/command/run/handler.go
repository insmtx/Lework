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

const (
	defaultMaxConcurrency = 20
	defaultDebounceWindow = 1500 * time.Millisecond
	inboxRetention        = 72 * time.Hour
	semInProgressInterval = 15 * time.Second
)

// Config controls a worker run handler.
type Config struct {
	OrgID          uint
	WorkerID       uint
	Env            string
	MaxConcurrency int
	DebounceWindow time.Duration
	InboxDBPath    string // required
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
	Input         messaging.TaskInput
	Model         messaging.ModelOptions
	Runtime       messaging.RuntimeOptions
	Policy        messaging.TaskPolicy
}

// Handler receives run commands and dispatches them asynchronously to the RunCoordinator.
type Handler struct {
	cfg       Config
	publisher eventbus.Publisher

	coordinator *runcoord.Coordinator
	runInbox    inbox.RunInbox

	// Admission semaphore limits concurrent inflight submissions.
	sem chan struct{}

	// inflight tracks stream_seq currently owned by this process.
	// Key is "topic:stream_seq".
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

	ri, err := inbox.NewSQLiteRunInbox(cfg.InboxDBPath)
	if err != nil {
		return nil, fmt.Errorf("create run inbox: %w", err)
	}

	execCtx, execCancel := context.WithCancel(context.Background())

	h := &Handler{
		cfg:              cfg,
		publisher:        pub,
		runInbox:         ri,
		sem:              make(chan struct{}, maxConc*2),
		inflight:         make(map[string]struct{}),
		execCtx:          execCtx,
		execCancel:       execCancel,
		admissionOpen:    true,
		admissionStopped: make(chan struct{}),
	}

	coord, err := runcoord.NewCoordinator(runcoord.Config{
		MaxConcurrency: maxConc,
		DebounceWindow: window,
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
			sub.Request.Runtime.Kind, sub.Request.Assistant.ID,
		)

		return svc.Run(ctx, sub.Request, agentrun.EventContext{
			OrgID:             ec.OrgID,
			WorkerID:          ec.WorkerID,
			SessionID:         ec.SessionID,
			TraceID:           ec.TraceID,
			RequestID:         ec.RequestID,
			TaskID:            ec.TaskID,
			RunID:             ec.RunID,
			ParentID:          ec.ParentID,
			ReplyToMessageIDs: ec.ReplyToMessageIDs,
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
		Input:         payload.Input,
		Model:         payload.Model,
		Runtime:       payload.Runtime,
		Policy:        payload.Policy,
	}

	if err := h.validateRouteTask(task); err != nil {
		_ = delivery.Term()
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
// 步骤：
//  1. 获取 admission semaphore（等待时周期性发送 InProgress，防止 NATS 超时重投）
//  2. 持久化消息到本地 SQLite inbox（PutIfAbsent 幂等防重）
//  3. 在状态锁下：检查 admission 是否开放、注册 inflight 跟踪、增加 WaitGroup 计数
//  4. 启动后台 goroutine 执行 Coordinator.Submit
//  5. Ack，告知 NATS 消息已安全持久化
func (h *Handler) admit(ctx context.Context, topic string, seq uint64, cmd messaging.WorkerCommand, task runTask, delivery eventbus.ManualDelivery) error {
	// 1. Acquire semaphore with InProgress heartbeats.
	if err := h.acquireSem(ctx, delivery); err != nil {
		if nakErr := delivery.NakWithDelay(5 * time.Second); nakErr != nil {
			logs.WarnContextf(ctx, "Failed to Nak run command after admission error: %v", nakErr)
		}
		return err
	}

	// 2. Persist to durable inbox.
	inserted, existing, err := h.runInbox.PutIfAbsent(ctx, topic, seq, cmd)
	if err != nil {
		h.releaseAdmission()
		_ = delivery.NakWithDelay(5 * time.Second)
		return fmt.Errorf("inbox PutIfAbsent: %w", err)
	}

	if !inserted {
		// Record already exists.
		if existing.IsTerminal() {
			h.releaseAdmission()
			h.ack(ctx, delivery)
			return nil
		}

		// Non-terminal. Check if owned by this process.
		ikey := inboxKey(topic, seq)
		h.stateMu.Lock()
		_, owned := h.inflight[ikey]
		if owned {
			h.stateMu.Unlock()
			h.releaseAdmission()
			h.ack(ctx, delivery)
			return nil
		}
		// Stale record — this process will own it now.
		if !h.admissionOpen {
			h.stateMu.Unlock()
			h.releaseAdmission()
			_ = delivery.NakWithDelay(5 * time.Second)
			return fmt.Errorf("admission closed")
		}
		h.inflight[ikey] = struct{}{}
		h.submissions.Add(1)
		h.stateMu.Unlock()
	} else {
		// New record — register under state lock.
		ikey := inboxKey(topic, seq)
		h.stateMu.Lock()
		if !h.admissionOpen {
			h.stateMu.Unlock()
			h.releaseAdmission()
			_ = delivery.NakWithDelay(5 * time.Second)
			return fmt.Errorf("admission closed")
		}
		h.inflight[ikey] = struct{}{}
		h.submissions.Add(1)
		h.stateMu.Unlock()
	}

	// Update to processing before dispatch.
	if err := h.runInbox.MarkProcessing(ctx, topic, seq); err != nil {
		logs.ErrorContextf(ctx, "Failed to mark inbox processing: topic=%s seq=%d: %v", topic, seq, err)
	}

	if seq != 0 {
		task.DeliverySeqs = []uint64{seq}
	}

	logs.InfoContextf(ctx,
		"Received run command: msg_id=%s task_id=%s run_id=%s org_id=%d worker_id=%d session_id=%s task_type=%s seq=%d",
		task.ID, task.Trace.TaskID, task.Trace.RunID,
		task.Route.OrgID, task.Route.WorkerID, task.Route.SessionID,
		task.TaskType, seq,
	)

	// 5. Start goroutine then Ack.
	go h.dispatchAsync(task, topic, inboxKey(topic, seq))
	h.ack(ctx, delivery)

	return nil
}

// acquireSem acquires a semaphore slot, calling InProgress periodically.
// acquireSem 获取 admission semaphore 的一个槽位。
// 当 semaphore 已满时，每 15 秒发送一次 InProgress 心跳，
// 防止 NATS 因等待确认超时而重新投递消息。
// 同时监听 admissionStopped 通道以响应优雅关闭。
func (h *Handler) acquireSem(ctx context.Context, delivery eventbus.ManualDelivery) error {
	for {
		select {
		case <-h.admissionStopped:
			return fmt.Errorf("admission closed")
		case h.sem <- struct{}{}:
			return nil
		default:
		}
		if err := delivery.InProgress(); err != nil {
			return fmt.Errorf("nats in-progress: %w", err)
		}
		select {
		case <-h.admissionStopped:
			return fmt.Errorf("admission closed")
		case h.sem <- struct{}{}:
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
func (h *Handler) dispatchAsync(task runTask, topic, iKey string) {
	defer h.submissions.Done()
	defer h.releaseAdmission()
	defer h.releaseInflight(iKey)

	execCtx := h.execCtx

	req := RequestFromWorkerTask(task)
	submission := runcoord.RunSubmission{
		Request: req,
		EventContext: agentrun.EventContext{
			OrgID:             task.Route.OrgID,
			WorkerID:          task.Route.WorkerID,
			SessionID:         task.Route.SessionID,
			TraceID:           task.Trace.TraceID,
			RequestID:         task.Trace.RequestID,
			TaskID:            task.Trace.TaskID,
			RunID:             task.Trace.RunID,
			ParentID:          task.Trace.ParentID,
			ReplyToMessageIDs: replyToMessageIDs(task.Input.Messages),
		},
		DeliverySeqs: task.DeliverySeqs,
	}

	_, execErr := h.coordinator.Submit(execCtx, submission)

	for _, s := range task.DeliverySeqs {
		if execErr != nil {
			if err := h.runInbox.MarkFailed(execCtx, topic, s, execErr.Error()); err != nil {
				logs.ErrorContextf(execCtx, "Failed to mark inbox failed: topic=%s seq=%d: %v", topic, s, err)
			}
		} else {
			if err := h.runInbox.MarkCompleted(execCtx, topic, s); err != nil {
				logs.ErrorContextf(execCtx, "Failed to mark inbox completed: topic=%s seq=%d: %v", topic, s, err)
			}
		}
	}

	if execErr != nil {
		logs.WarnContextf(execCtx, "Run command execution error: msg_id=%s task_id=%s: %v",
			task.ID, task.Trace.TaskID, execErr)
	}
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
		ikey := inboxKey(rec.Topic, rec.StreamSeq)
		h.inflight[ikey] = struct{}{}
	}
	h.stateMu.Unlock()

	// Start the recovery feeder goroutine.
	h.recoveryWG.Add(1)
	go h.runRecoveryFeeder(records, topic)

	return nil
}

// runRecoveryFeeder 将恢复的 inbox 记录逐个通过 semaphore 调度执行。
// 每次从 semaphore 获取一个槽位后启动一个 goroutine 处理记录，
// 与实时投递共享同一套并发限流和生命周期管理。
func (h *Handler) runRecoveryFeeder(records []inbox.Record, topic string) {
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

		ikey := inboxKey(rec.Topic, rec.StreamSeq)

		go h.recoverRecord(rec, topic, ikey)
	}
}

// recoverRecord 处理一条恢复的 inbox 记录：反序列化命令、校验 payload、
// 构建任务并提交给 Coordinator。执行完成后更新 inbox 状态。
func (h *Handler) recoverRecord(rec inbox.Record, topic, ikey string) {
	defer h.submissions.Done()
	defer h.releaseAdmission()
	defer h.releaseInflight(ikey)

	execCtx := h.execCtx

	var cmd messaging.WorkerCommand
	if err := json.Unmarshal([]byte(rec.Command), &cmd); err != nil {
		_ = h.runInbox.MarkFailed(execCtx, topic, rec.StreamSeq, fmt.Sprintf("recovery unmarshal: %v", err))
		return
	}

	payload, err := messaging.DecodeCommandPayload[messaging.RunCommandPayload](&cmd.Body)
	if err != nil {
		_ = h.runInbox.MarkFailed(execCtx, topic, rec.StreamSeq, fmt.Sprintf("recovery payload decode: %v", err))
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
		DeliverySeqs:  []uint64{rec.StreamSeq},
	}

	// Mark processing.
	if err := h.runInbox.MarkProcessing(execCtx, topic, rec.StreamSeq); err != nil {
		logs.ErrorContextf(execCtx, "Failed to mark recovered inbox processing: topic=%s seq=%d: %v", topic, rec.StreamSeq, err)
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
			SessionID:         task.Route.SessionID,
			TraceID:           task.Trace.TraceID,
			RequestID:         task.Trace.RequestID,
			TaskID:            task.Trace.TaskID,
			RunID:             task.Trace.RunID,
			ParentID:          task.Trace.ParentID,
			ReplyToMessageIDs: replyToMessageIDs(task.Input.Messages),
		},
		DeliverySeqs: task.DeliverySeqs,
	}

	_, execErr := h.coordinator.Submit(execCtx, submission)

	if execErr != nil {
		if err := h.runInbox.MarkFailed(execCtx, topic, rec.StreamSeq, execErr.Error()); err != nil {
			logs.ErrorContextf(execCtx, "Failed to mark inbox failed (recovery): topic=%s seq=%d: %v", topic, rec.StreamSeq, err)
		}
	} else {
		if err := h.runInbox.MarkCompleted(execCtx, topic, rec.StreamSeq); err != nil {
			logs.ErrorContextf(execCtx, "Failed to mark inbox completed (recovery): topic=%s seq=%d: %v", topic, rec.StreamSeq, err)
		}
	}
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

func inboxKey(topic string, seq uint64) string {
	return fmt.Sprintf("%s:%d", topic, seq)
}
