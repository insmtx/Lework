// Package run provides the Worker-side RunCoordinator.
//
// Coordinator manages Worker-local scheduling: layered concurrency limits
// (compute slots, interaction-wait slots, admission ceiling), per-session
// serialization with round-robin fairness, debounce merging, cancellation,
// and active run tracking.
//
// Layered scheduling model:
//   - maxInflight     : Worker 准入上限（由 NATS 回调持续发送 InProgress），
//     由 Handler 的 admission semaphore 实施，Coordinator 仅记录容量。
//   - maxConcurrency  : 计算/运行槽，真正占用计算资源的任务数量。
//   - maxInteractionWaits : 交互等待槽，等待审批/问答的任务占用的容量（不占计算槽）。
//
// Coordinator MUST NOT depend on workspace, model, engine, artifact, event types, or NATS subjects.
package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/insmtx/Leros/backend/internal/worker/agentrun"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/pkg/utils"
	"github.com/ygpkg/yg-go/logs"
)

// RunSubmission is a submitted run request with event context and delivery sequences.
type RunSubmission struct {
	Request      *agentrundomain.RunRequest
	EventContext agentrun.EventContext
	DeliverySeqs []uint64
	// NotAfter Worker 最晚允许开始时间（零值表示不限制），在真正拿到运行槽前重新检查。
	NotAfter time.Time
	// Recovered 表示该提交来自崩溃恢复，最多占用一半运行容量。
	Recovered bool

	// memberRunIDs 记录本批次合并进来的所有成员 runID（合并前为自身 runID）。
	// 用于：取消任一成员时能定位并取消整个（可能已 active 的）合并批次。
	memberRunIDs []string
	// members 保留合并前每个 Run 的可观测摘要，避免运维快照把多个 Run
	// 错误地压缩为一个 batch。
	members []runMember

	waiterIDs []uint64
}

// ExecuteFunc is the actual execution function injected by the command adapter.
type ExecuteFunc func(
	ctx context.Context,
	submission RunSubmission,
) (*agentrundomain.RunResult, error)

// RunOutcome is the result of executing a run (possibly merged from multiple submissions).
type RunOutcome struct {
	Result       *agentrundomain.RunResult
	DeliverySeqs []uint64
}

// Config controls Coordinator behavior.
type Config struct {
	// MaxConcurrency 计算/运行槽数量，缺省 10。
	MaxConcurrency int
	// MaxInflight Worker 准入上限（与 Handler 的 admission semaphore 一致），仅用于容量日志。
	MaxInflight int
	// MaxInteractionWaits 并行交互等待槽数量，缺省 10。
	MaxInteractionWaits int
	// InteractionWaitTimeout 审批/问答等待的默认硬超时，缺省 10 分钟。
	InteractionWaitTimeout time.Duration
	// DebounceWindow trailing debounce 窗口，缺省 1.5s。
	DebounceWindow time.Duration
	// MaxRunDuration is a hard deadline starting only after a compute slot is acquired.
	MaxRunDuration time.Duration
}

// Coordinator manages Worker-local scheduling for agent runs.
type Coordinator struct {
	maxConcurrency int
	debounceWindow time.Duration
	slots          chan struct{}
	// recoverySlots 限制恢复任务最多占用一半运行容量（maxConcurrency/2），
	// 确保线上新任务始终保留可用槽位。
	recoverySlots chan struct{}
	recoveryCap   int

	interactionSlots chan struct{}
	interactionWait  time.Duration
	maxRunDuration   time.Duration

	debouncer *utils.TrailingDebouncer[RunSubmission]

	// Per-session pending queue + round-robin dispatcher.
	queueMu sync.Mutex
	queued  map[string][]*batchEntry // sessionKey -> FIFO of batches
	// sessionBusy 记录当前有正在执行的 run 的会话，保证 per-session 串行。
	sessionBusy map[string]struct{}
	// order 是「有待调度批次」的会话有序列表；cursor 记录上一次调度的位置，
	// 每次成功 dispatch 后前进并环绕，实现真正的 round-robin（避免 Go map 随机序导致偏向/饥饿）。
	order  []string
	cursor int
	notify chan struct{} // signal dispatcher (buffered 1)

	activeRuns   map[string]*activeRun
	activeRunsMu sync.RWMutex
	// memberToBatch 记录合并批次的每个成员 runID -> 所属 batch。
	// 在批次进入 slot-wait 时即建立（而非等到 active），使取消任一成员都能
	// 定位并取消整个合并批次，覆盖 slot-wait 与 active 两个阶段。
	memberToBatch   map[string]*batchEntry
	memberToBatchMu sync.Mutex

	// slotWaiting 记录已出队、正在等待计算槽（尚未注册 active run）的批次，
	// 按 activeRunKey 索引，供取消阶段在 slot-wait 中定位。
	slotWaiting   map[string]*batchEntry
	slotWaitingMu sync.Mutex

	executeFunc ExecuteFunc

	pending      map[uint64]chan runResult
	nextWaiterID uint64
	// runToWaiters 记录 runID -> waiterIDs，用于在 debounce 阶段按 runID 取消。
	runToWaiters map[string][]uint64

	// waitSlots 记录当前正在交互等待的 runID -> 结束回调，用于取消与计数。
	waitsMu sync.Mutex
	waits   map[string]*interactionWait

	stateMu     sync.Mutex
	closed      bool
	dispatchers sync.WaitGroup
	// runs 跟踪真正执行批次的 goroutine（runBatch/executeDirect 的完成时机），
	// 与 submissions（Submit 调用方返回时机）区分：确保 Close 等待所有 Runtime 退出，
	// 而不是等待 Submit 因被强制发送结果而返回。
	runs sync.WaitGroup

	submissions sync.WaitGroup

	// execCtx 是 Coordinator 的根上下文：Worker 关闭时统一取消，
	// 队列中的批次、slot-wait 与运行中的 Runtime 都从它派生，从而被级联取消。
	execCtx    context.Context
	execCancel context.CancelFunc
}

// batchEntry 表示一个已通过 debounce 合并、等待调度的批次。
type batchEntry struct {
	sessionKey string
	submission RunSubmission
	// ctx 是该批次的来源上下文（首个提交者的 ctx）：用于运行期取消传播，
	// 使 Submit(ctx) 的取消能同步取消已 active 的本批次 Runtime。
	ctx context.Context
	// cancel 用于取消 slot-wait 阶段的等待。
	cancel context.CancelFunc
	// started 表示批次是否已真正开始运行（被 acquire slot 后启动 executeFunc）。
	started bool
}

// interactionWait 表示一个正在等待交互的任务。
type interactionWait struct {
	runID   string
	started time.Time
	// ended 确保交互等待槽只被释放一次（watchdog 与 end() 都可能在超时/取消路径释放）。
	ended atomic.Bool
}

type runResult struct {
	outcome RunOutcome
	err     error
}

var errCoordinatorClosed = errors.New("run coordinator is closed")

// ErrRunRouteRequired 表示 Run 缺少必要的路由字段（OrgID/WorkerID/SessionID）。
// Handler 与 Coordinator 共用该防御性错误。
var ErrRunRouteRequired = errors.New("run requires a valid route (org_id, worker_id, session_id)")

// activeRun tracks a running agent execution that can be cancelled.
type activeRun struct {
	runID     string
	taskID    string
	sessionID string
	members   []runMember
	cancel    context.CancelFunc
	startedAt time.Time
}

type runMember struct {
	runID      string
	taskID     string
	sessionID  string
	streamSeqs []uint64
}

// NewCoordinator creates a new RunCoordinator.
func NewCoordinator(cfg Config, executeFunc ExecuteFunc) (*Coordinator, error) {
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 10
	}
	if cfg.MaxInflight <= 0 {
		cfg.MaxInflight = cfg.MaxConcurrency
	}
	if cfg.MaxInteractionWaits <= 0 {
		cfg.MaxInteractionWaits = 10
	}
	if cfg.InteractionWaitTimeout <= 0 {
		cfg.InteractionWaitTimeout = 10 * time.Minute
	}
	if cfg.DebounceWindow <= 0 {
		cfg.DebounceWindow = 1500 * time.Millisecond
	}
	if cfg.MaxRunDuration <= 0 {
		cfg.MaxRunDuration = 4 * time.Hour
	}
	if executeFunc == nil {
		return nil, fmt.Errorf("execute function is required")
	}

	// 恢复任务最多占用一半运行容量；至少保留 1 个恢复槽，避免配置为 1 时全部被占用。
	recoveryCap := max(cfg.MaxConcurrency/2, 1)

	// Coordinator 根上下文：从它派生所有批次/Runtime 上下文。
	execCtx, execCancel := context.WithCancel(context.Background())

	c := &Coordinator{
		maxConcurrency:   cfg.MaxConcurrency,
		debounceWindow:   cfg.DebounceWindow,
		slots:            make(chan struct{}, cfg.MaxConcurrency),
		recoverySlots:    make(chan struct{}, recoveryCap),
		recoveryCap:      recoveryCap,
		interactionSlots: make(chan struct{}, cfg.MaxInteractionWaits),
		interactionWait:  cfg.InteractionWaitTimeout,
		maxRunDuration:   cfg.MaxRunDuration,
		executeFunc:      executeFunc,
		activeRuns:       make(map[string]*activeRun),
		memberToBatch:    make(map[string]*batchEntry),
		slotWaiting:      make(map[string]*batchEntry),
		pending:          make(map[uint64]chan runResult),
		runToWaiters:     make(map[string][]uint64),
		queued:           make(map[string][]*batchEntry),
		sessionBusy:      make(map[string]struct{}),
		notify:           make(chan struct{}, 1),
		waits:            make(map[string]*interactionWait),
		execCtx:          execCtx,
		execCancel:       execCancel,
	}

	// The debouncer merges submissions with the same session key,
	// then enqueueSubmission appends the merged batch to the per-session queue.
	debouncer, err := utils.NewTrailingDebouncer(cfg.DebounceWindow, c.enqueueSubmission, nil, mergeSubmissions)
	if err != nil {
		return nil, err
	}
	c.debouncer = debouncer

	// 启动 round-robin 调度器。
	c.dispatchers.Add(1)
	go c.dispatchLoop()

	logs.Infof("scheduler config: worker.run.scheduler.config max_concurrency=%d max_inflight=%d max_interaction_waits=%d interaction_timeout_seconds=%d debounce_ms=%d",
		cfg.MaxConcurrency, cfg.MaxInflight, cfg.MaxInteractionWaits, int(cfg.InteractionWaitTimeout.Seconds()), int(cfg.DebounceWindow.Milliseconds()))
	return c, nil
}

// Submit submits a run request. For session-keyed submissions, it goes through
// debounce merging + per-session queue; the caller blocks until the consolidated
// batch completes.
func (c *Coordinator) Submit(ctx context.Context, submission RunSubmission) (RunOutcome, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// 防御性路由校验：缺少 OrgID/WorkerID/SessionID 立即拒绝，不创建 waiter、不入队、不启动执行。
	if err := validateSubmissionRoute(submission); err != nil {
		return RunOutcome{}, err
	}
	if err := c.beginSubmission(); err != nil {
		return RunOutcome{}, err
	}
	defer c.submissions.Done()

	key := sessionKey(submission)
	msgCount := 0
	if submission.Request != nil {
		msgCount = len(submission.Request.Input.Messages)
	}
	// 初始化成员 runID（自身）。
	if submission.EventContext.RunID != "" {
		submission.memberRunIDs = []string{submission.EventContext.RunID}
	}
	submission.members = []runMember{{
		runID:      submission.EventContext.RunID,
		taskID:     requestTaskID(submission),
		sessionID:  submission.EventContext.SessionID,
		streamSeqs: append([]uint64(nil), submission.DeliverySeqs...),
	}}
	logs.InfoContextf(ctx, "coordinator submit: session_key=%s run_id=%s worker_id=%d messages=%d",
		key, submission.EventContext.RunID, submission.EventContext.WorkerID, msgCount)
	if key == "" {
		// 所有合法 Run 都必须携带完整路由并进入 session queue；此处为二次防御。
		return RunOutcome{}, ErrRunRouteRequired
	}

	return c.scheduleAndWait(ctx, key, submission)
}

// validateSubmissionRoute 校验 RunSubmission 的必填路由字段是否齐全。
func validateSubmissionRoute(sub RunSubmission) error {
	ec := sub.EventContext
	if ec.OrgID == 0 || ec.WorkerID == 0 || strings.TrimSpace(ec.SessionID) == "" {
		return ErrRunRouteRequired
	}
	return nil
}

func (c *Coordinator) beginSubmission() error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.closed {
		return errCoordinatorClosed
	}
	c.submissions.Add(1)
	return nil
}

// scheduleAndWait registers a waiter for the session key, calls the debouncer,
// and blocks until the consolidated batch has been executed.
func (c *Coordinator) scheduleAndWait(ctx context.Context, key string, submission RunSubmission) (RunOutcome, error) {
	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		return RunOutcome{}, errCoordinatorClosed
	}
	c.nextWaiterID++
	waiterID := c.nextWaiterID
	ch := make(chan runResult, 1)
	c.pending[waiterID] = ch
	submission.waiterIDs = append(submission.waiterIDs, waiterID)
	// 记录 runID -> waiter，便于取消阶段定位（尤其 debounce/queued 尚未 flush 时）。
	if runID := submission.EventContext.RunID; runID != "" {
		c.runToWaiters[runID] = append(c.runToWaiters[runID], waiterID)
	}
	// debounce 阶段：使用带来源（recovered/live）的合并键，禁止恢复与实时任务跨来源合并，
	// 保证两者的 Recovered / NotAfter 语义互不污染。
	logs.DebugContextf(ctx, "coordinator queue enqueued: queue.debounce.defer session_key=%s waiter_id=%d run_id=%s recovered=%v",
		key, waiterID, submission.EventContext.RunID, submission.Recovered)
	c.debouncer.Call(ctx, debounceKey(key, submission.Recovered), submission)
	c.stateMu.Unlock()

	select {
	case <-ctx.Done():
		// ctx 被取消：从 pending 移除 waiter（可能仍在 debounce 或 queued 中）。
		c.cancelWaiter(submission.EventContext.RunID, waiterID)
		return RunOutcome{}, ctx.Err()
	case rr := <-ch:
		c.forgetRun(submission.EventContext.RunID, waiterID)
		return rr.outcome, rr.err
	}
}

// forgetRun 在 waiter 结果已交付后清理 runToWaiters 中该 runID 的记录。
func (c *Coordinator) forgetRun(runID string, waiterID uint64) {
	if runID == "" {
		return
	}
	c.stateMu.Lock()
	ids := c.runToWaiters[runID]
	if len(ids) == 1 && ids[0] == waiterID {
		delete(c.runToWaiters, runID)
	} else {
		kept := ids[:0]
		for _, id := range ids {
			if id != waiterID {
				kept = append(kept, id)
			}
		}
		c.runToWaiters[runID] = kept
	}
	c.stateMu.Unlock()
}

// enqueueSubmission 是 debouncer 的 flush 回调：把合并后的批次追加到 per-session 队列，
// 并通知 dispatcher 尽快出队。
func (c *Coordinator) enqueueSubmission(ctx context.Context, submission RunSubmission) error {
	key := sessionKey(submission)
	waiterIDs := append([]uint64(nil), submission.waiterIDs...)

	batch := &batchEntry{sessionKey: key, submission: submission, ctx: ctx}

	c.queueMu.Lock()
	if len(c.queued[key]) == 0 {
		// 该会话首次入队：追加到有序调度列表，参与 round-robin。
		c.order = append(c.order, key)
	}
	c.queued[key] = append(c.queued[key], batch)
	c.queueMu.Unlock()

	logs.DebugContextf(ctx, "coordinator queue enqueued: queue.enqueued session_key=%s run_id=%s waiters=%d",
		key, submission.EventContext.RunID, len(waiterIDs))

	c.signalDispatcher()
	return nil
}

func (c *Coordinator) signalDispatcher() {
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

// dispatchLoop 以 round-robin 方式遍历有等待批次的会话，每个会话取一个批次并启动执行。
func (c *Coordinator) dispatchLoop() {
	defer c.dispatchers.Done()
	for {
		if c.isClosed() {
			return
		}
		batch := c.dequeueOne()
		if batch == nil {
			if c.isClosed() {
				return
			}
			<-c.notify
			continue
		}
		c.runs.Add(1)
		go c.runBatch(batch)
	}
}

// dequeueOne 使用真正的 round-robin：按持久化的会话有序列表 + 游标调度。
// 每次成功 dispatch 后移动游标到下一个会话，避免 Go map 随机序导致偏向或饥饿。
// 会话忙（正在执行）或队列清空时跳过并从调度列表中移除；保证 per-session 串行。
// 返回 nil 表示当前没有可调度的批次。
func (c *Coordinator) dequeueOne() *batchEntry {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()
	if len(c.order) == 0 {
		return nil
	}
	// 从游标开始至多扫描 len(order) 次（一轮）。
	for range len(c.order) {
		if len(c.order) == 0 {
			return nil
		}
		idx := c.cursor % len(c.order)
		key := c.order[idx]
		q := c.queued[key]
		if len(q) == 0 {
			// 队列空：移除出调度列表，游标天然指向被移除位置的下一个会话。
			delete(c.queued, key)
			c.removeOrderLocked(idx)
			continue
		}
		if _, busy := c.sessionBusy[key]; busy {
			// 会话正在执行：跳过并前进游标。
			c.cursor = (idx + 1) % len(c.order)
			continue
		}
		batch := q[0]
		if len(q) == 1 {
			delete(c.queued, key)
			c.removeOrderLocked(idx)
		} else {
			c.queued[key] = q[1:]
			// 队列仍有批次：前进游标到下一会话。
			c.cursor = (idx + 1) % len(c.order)
		}
		// 出队即原子地 claim 会话，保证同一 session 的任务串行执行。
		// 只有任务完成或取消后才释放（见 runBatch 的 clearSessionBusy）。
		if key != "" {
			c.sessionBusy[key] = struct{}{}
		}
		return batch
	}
	// 一轮内所有会话均忙或无批次：返回 nil 让 dispatcher 阻塞等待 notify（会话释放才会重新触发）。
	return nil
}

// removeOrderLocked 从有序调度列表移除下标 idx 处的会话并修正游标；调用方需持有 queueMu。
func (c *Coordinator) removeOrderLocked(idx int) {
	if idx < 0 || idx >= len(c.order) {
		return
	}
	removeOrderAt(&c.order, &c.cursor, idx)
}

// removeOrderKeyLocked 按 key 从有序调度列表移除会话并修正游标；调用方需持有 queueMu。
func (c *Coordinator) removeOrderKeyLocked(key string) {
	for i, k := range c.order {
		if k == key {
			removeOrderAt(&c.order, &c.cursor, i)
			return
		}
	}
}

// removeOrderAt 移除有序列表下标 idx 的元素；被移除元素后的会话前移占用 idx。
// 保持游标有效（0 <= cursor < len(order)，列表为空时归零）。
func removeOrderAt(order *[]string, cursor *int, idx int) {
	*order = append((*order)[:idx], (*order)[idx+1:]...)
	if len(*order) == 0 {
		*cursor = 0
		return
	}
	if *cursor >= len(*order) {
		*cursor = 0
	}
}

// releaseCancelledBatch 在出队时发现批次已被取消时，直接通知其 waiters。
func (c *Coordinator) releaseCancelledBatch(batch *batchEntry) {
	c.notifyWaiters(batch.submission.waiterIDs, runResult{err: context.Canceled})
}

// hasAnyLiveWaiter 判断批次是否仍有至少一个存活（尚未被取消/交付）的 waiter。
// 用于在批次真正执行前跳过那些 waiter 已全部被取消的批（避免启动无人等待的 Runtime）。
func (c *Coordinator) hasAnyLiveWaiter(waiterIDs []uint64) bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	for _, id := range waiterIDs {
		if _, ok := c.pending[id]; ok {
			return true
		}
	}
	return false
}

// clearSessionBusy 清除会话的执行标记，并唤醒调度器消费同会话的下一个批次。
func (c *Coordinator) clearSessionBusy(sessionKey string) {
	if sessionKey == "" {
		return
	}
	c.queueMu.Lock()
	delete(c.sessionBusy, sessionKey)
	c.queueMu.Unlock()
}

// runBatch 真正运行一个已出队的批次：先获取计算槽（支持取消），再执行。
// 批次上下文从 Coordinator 根上下文（关闭级联）与批次来源上下文（Submit 取消传播）
// 合并派生，二者任一取消都会取消本批次。
func (c *Coordinator) runBatch(batch *batchEntry) {
	batchCtx, cancel := mergeContext(c.execCtx, batch.ctx)
	batch.cancel = cancel
	defer cancel()
	// 标记批次 goroutine 结束（与 dispatcher 的 runs.Add(1) 配对），让 Close 等待真正的 Runtime 退出。
	defer c.runs.Done()

	sub := batch.submission
	sKey := batch.sessionKey
	waitStart := time.Now()

	// 会话已在出队时 claim（见 dequeueOne）。这里注册释放，确保本批次的所有
	// 退出路径（跳过/取消/过期/完成）都释放会话并唤醒 dispatcher。
	defer func() {
		c.clearSessionBusy(sKey)
		c.signalDispatcher()
	}()

	// 若批次的所有 waiter 均已被取消（例如 debounce 阶段 Cancel 移除了它们），
	// 则不再启动 Runtime，直接释放批次资源。
	if !c.hasAnyLiveWaiter(sub.waiterIDs) {
		logs.InfoContextf(batchCtx, "coordinator batch all-waiters-cancelled, skipping: run_id=%s session_key=%s waiters=%d",
			sub.EventContext.RunID, sKey, len(sub.waiterIDs))
		return
	}

	activeKey := ""
	if sub.EventContext.RunID != "" {
		activeKey = activeRunKey(sub.EventContext.OrgID, sub.EventContext.WorkerID, sub.EventContext.SessionID, sub.EventContext.RunID)
	}
	// slot-wait 阶段注册：按主 run 定位 slotWaiting，并为每个成员 runID 建立
	// memberToBatch，使得 slot-wait 阶段取消任一成员也能取消整个合并批次。
	if activeKey != "" {
		c.slotWaitingMu.Lock()
		c.slotWaiting[activeKey] = batch
		c.slotWaitingMu.Unlock()
	}
	defer func() {
		c.slotWaitingMu.Lock()
		delete(c.slotWaiting, activeKey)
		c.slotWaitingMu.Unlock()
	}()
	// 成员映射：覆盖 slot-wait 与 active；批次结束清理。
	if len(sub.memberRunIDs) > 0 {
		c.memberToBatchMu.Lock()
		for _, m := range sub.memberRunIDs {
			if m != "" {
				c.memberToBatch[m] = batch
			}
		}
		c.memberToBatchMu.Unlock()
	}
	defer func() {
		c.memberToBatchMu.Lock()
		for _, m := range sub.memberRunIDs {
			if m != "" {
				delete(c.memberToBatch, m)
			}
		}
		c.memberToBatchMu.Unlock()
	}()

	// slot-wait：等待计算槽。
	logs.DebugContextf(batchCtx, "coordinator slot wait: slot.wait session_key=%s run_id=%s active=%d cap=%d",
		sKey, sub.EventContext.RunID, len(c.slots), cap(c.slots))

	slotHandle, err := c.acquireComputeSlot(batchCtx, sub.Recovered)
	if err != nil {
		// 等待被取消（cancel）或 coordinator 关闭。
		logs.InfoContextf(batchCtx, "coordinator slot wait cancelled: run_id=%s session_key=%s err=%v",
			sub.EventContext.RunID, sKey, err)
		c.releaseCancelledBatch(batch)
		return
	}
	// 计算槽由 handle 管理（含恢复许可所有权），交互等待时可释放/重取，结束幂等释放。
	defer slotHandle.release()

	waitedMS := time.Since(waitStart).Milliseconds()
	if waitedMS > 1000 {
		logs.WarnContextf(batchCtx, "coordinator slot acquired slow: slot.acquired run_id=%s session_key=%s waited_ms=%d active=%d cap=%d",
			sub.EventContext.RunID, sKey, waitedMS, len(c.slots), cap(c.slots))
	} else {
		logs.InfoContextf(batchCtx, "coordinator slot acquired: slot.acquired run_id=%s session_key=%s waited_ms=%d active=%d cap=%d",
			sub.EventContext.RunID, sKey, waitedMS, len(c.slots), cap(c.slots))
	}
	batch.started = true

	// 真正拿到槽前重新检查 NotAfter：过期任务直接标记失败，不再启动 Runtime。
	if !sub.NotAfter.IsZero() && time.Now().After(sub.NotAfter) {
		logs.WarnContextf(batchCtx, "run expired before slot start: run_id=%s session_key=%s not_after=%s",
			sub.EventContext.RunID, sKey, sub.NotAfter.Format(time.RFC3339))
		c.notifyWaiters(sub.waiterIDs, runResult{err: errNotAfterExpired})
		return
	}

	runCtx, runCancel := context.WithTimeout(batchCtx, c.maxRunDuration)
	defer runCancel()
	if activeKey == "" && sKey != "" && sub.EventContext.RunID != "" {
		activeKey = activeRunKey(sub.EventContext.OrgID, sub.EventContext.WorkerID, sub.EventContext.SessionID, sub.EventContext.RunID)
	}
	if activeKey != "" {
		c.RegisterRun(activeKey, sub.EventContext.RunID, requestTaskID(sub), sub.EventContext.SessionID, sub.members, runCancel)
		defer c.UnregisterRun(activeKey)
	}
	logs.InfoContextf(runCtx, "coordinator executing: run_id=%s session_key=%s",
		sub.EventContext.RunID, sKey)

	// 构造带交互等待控制器的上下文。
	logCtx := c.newInteractionWaiter(runCtx, sub.EventContext.RunID, slotHandle, runCancel)

	result, execErr := c.executeFunc(logCtx, sub)
	outcome := RunOutcome{Result: result, DeliverySeqs: sub.DeliverySeqs}
	c.notifyWaiters(sub.waiterIDs, runResult{outcome: outcome, err: execErr})
}

// acquireComputeSlot 获取计算槽（恢复任务额外获取一半容量的恢复许可），
// 返回已持有 token 的 handle；所有权完整转交 handle，后续 release/重取都由 handle 管理，
// 避免与调用方的 defer 双重释放。ctx 取消时中断等待。
func (c *Coordinator) acquireComputeSlot(ctx context.Context, recovered bool) (*computeSlotHandle, error) {
	handle := &computeSlotHandle{coordinator: c, recovered: recovered}
	// 恢复任务需额外获取恢复许可（最多 maxConcurrency/2）。
	if recovered {
		select {
		case c.recoverySlots <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	select {
	case c.slots <- struct{}{}:
		handle.held = true
		return handle, nil
	case <-ctx.Done():
		if recovered {
			<-c.recoverySlots
		}
		return nil, ctx.Err()
	}
}

var errNotAfterExpired = errors.New("run expired before slot start")

func requestTaskID(submission RunSubmission) string {
	if submission.Request != nil && strings.TrimSpace(submission.Request.TaskID) != "" {
		return submission.Request.TaskID
	}
	return submission.EventContext.TaskID
}

// cancelWaiter 尝试取消一个正在 debounce / queued / slot-wait 中的 waiter。
// 如果 waiter 已经进入 active 执行，则无法在此取消（由 activeRuns 处理）。
func (c *Coordinator) cancelWaiter(runID string, waiterID uint64) {
	c.stateMu.Lock()
	_, exists := c.pending[waiterID]
	if exists {
		delete(c.pending, waiterID)
	}
	// 清理 runID 映射。
	if runID != "" {
		ids := c.runToWaiters[runID]
		kept := ids[:0]
		for _, id := range ids {
			if id != waiterID {
				kept = append(kept, id)
			}
		}
		if len(kept) == 0 {
			delete(c.runToWaiters, runID)
		} else {
			c.runToWaiters[runID] = kept
		}
	}
	c.stateMu.Unlock()
}

func (c *Coordinator) notifyWaiters(waiterIDs []uint64, result runResult) {
	waiters := make([]chan runResult, 0, len(waiterIDs))
	c.stateMu.Lock()
	for _, waiterID := range waiterIDs {
		if ch, ok := c.pending[waiterID]; ok {
			delete(c.pending, waiterID)
			waiters = append(waiters, ch)
		}
	}
	c.stateMu.Unlock()
	for _, ch := range waiters {
		ch <- result
	}
}

func (c *Coordinator) isClosed() bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.closed
}

// mergeContext 返回 base 的一个子上下文，当 parent 被取消时同样取消该子上下文。
// 用于让批次上下文同时响应 Coordinator 关闭（base）与提交者取消（parent）。
// goroutine 在任一路径结束时退出，生命周期受运行时长限制，不会泄漏。
func mergeContext(base, parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// Cancel cancels the run identified by org/worker/session/runID across all
// scheduling phases: debounce, queued, slot-wait, or active.
func (c *Coordinator) Cancel(ctx context.Context, orgID, workerID uint, sessionID, runID string) error {
	if runID == "" {
		return nil
	}
	key := activeRunKey(orgID, workerID, sessionID, runID)
	logs.InfoContextf(ctx, "coordinator cancel request: cancel.request run_id=%s session_id=%s",
		runID, sessionID)

	// 1. active run（或合并批次的成员 -> 所属批次的 active 运行）。
	c.activeRunsMu.RLock()
	ar, ok := c.activeRuns[key]
	c.activeRunsMu.RUnlock()
	if ok {
		ar.cancel()
		logs.InfoContextf(ctx, "coordinator cancel resolved active: cancel.resolved run_id=%s phase=active", runID)
		return nil
	}

	// 1a. 合并批次的成员（slot-wait 或 active 阶段）：经 memberToBatch 定位批次并取消。
	//      batch.cancel 取消批次上下文，slot-wait 与 active 的 Runtime 均级联取消。
	c.memberToBatchMu.Lock()
	memberBatch, mOK := c.memberToBatch[runID]
	c.memberToBatchMu.Unlock()
	if mOK && memberBatch != nil && memberBatch.cancel != nil {
		memberBatch.cancel()
		logs.InfoContextf(ctx, "coordinator cancel resolved merged member: cancel.resolved run_id=%s phase=member_batch", runID)
		return nil
	}

	// 1b. slot-wait 中的批次（已出队、等待计算槽、尚未注册 active run）。
	c.slotWaitingMu.Lock()
	batch, waitNo := c.slotWaiting[key]
	if waitNo {
		delete(c.slotWaiting, key)
	}
	c.slotWaitingMu.Unlock()
	if waitNo {
		if batch != nil && batch.cancel != nil {
			batch.cancel()
			logs.InfoContextf(ctx, "coordinator cancel resolved slotwait: cancel.resolved run_id=%s phase=slot_wait", runID)
		}
		return nil
	}

	// 2. debounce / queued 中的 pending waiter（按 runID 定位）。
	targetWaiters := c.cancelWaitersForRun(ctx, runID)

	// 3. queued / slot-wait 中的批次（含 merged batch 成员认定）。
	c.cancelQueuedForKey(ctx, orgID, workerID, sessionID, runID, targetWaiters)
	return nil
}

// cancelWaitersForRun 取消该 runID 当前仍 pending 的 waiter（可能仍在 debounce 或 queued 中）。
// 返回被取消的 waiterID 列表，供批次级取消判定合并成员。
func (c *Coordinator) cancelWaitersForRun(ctx context.Context, runID string) []uint64 {
	c.stateMu.Lock()
	waiterIDs := append([]uint64(nil), c.runToWaiters[runID]...)
	delete(c.runToWaiters, runID)
	waiters := make([]chan runResult, 0, len(waiterIDs))
	for _, waiterID := range waiterIDs {
		if ch, ok := c.pending[waiterID]; ok {
			delete(c.pending, waiterID)
			waiters = append(waiters, ch)
		}
	}
	c.stateMu.Unlock()
	for _, ch := range waiters {
		ch <- runResult{err: context.Canceled}
	}
	if len(waiters) > 0 {
		logs.InfoContextf(ctx, "coordinator cancel resolved pending: cancel.resolved run_id=%s phase=debounce_or_queued waiters=%d",
			runID, len(waiters))
	}
	return waiterIDs
}

// cancelQueuedForKey 在 per-session 队列与 slot-wait 中查找并取消批次。
// targetWaiters 是目标 runID 当前已取消的 waiterID，用来认定 merged batch 成员：
// 若批次的任一 waiter 属于 targetWaiters，则取消整个批次（含所有关联 waiters）。
func (c *Coordinator) cancelQueuedForKey(ctx context.Context, orgID, workerID uint, sessionID, runID string, targetWaiters []uint64) {
	sKeyMatch := fmt.Sprintf("%d:%d:%s", orgID, workerID, sessionID)
	targetSet := make(map[uint64]struct{}, len(targetWaiters))
	for _, id := range targetWaiters {
		targetSet[id] = struct{}{}
	}
	c.queueMu.Lock()
	var found bool
	var toRelease [][]uint64
	for sKey, q := range c.queued {
		// 只匹配该会话的队列。
		if sKey != sKeyMatch {
			continue
		}
		kept := q[:0]
		for _, b := range q {
			member := b.submission.EventContext.RunID == runID || hasAnyWaiter(b.submission.waiterIDs, targetSet)
			if !member {
				kept = append(kept, b)
				continue
			}
			found = true
			if b.cancel != nil {
				// 批次已启动（slot-wait/运行中）：取消整个 merged batch。
				b.cancel()
				logs.InfoContextf(ctx, "coordinator cancel resolved batch: cancel.resolved run_id=%s phase=slot_wait_or_active merged_waiters=%d",
					runID, len(b.submission.waiterIDs))
			} else {
				// 尚未启动（仍在队列）：标记批次，出队时释放其所有 waiters。
				logs.InfoContextf(ctx, "coordinator cancel resolved queued batch: cancel.resolved run_id=%s phase=queued merged_waiters=%d",
					runID, len(b.submission.waiterIDs))
				// 通知其所有 waiters（含尚未被单独取消的成员）。
				toRelease = append(toRelease, append([]uint64(nil), b.submission.waiterIDs...))
			}
		}
		if len(kept) == 0 {
			delete(c.queued, sKey)
			c.removeOrderKeyLocked(sKey)
		} else {
			c.queued[sKey] = kept
		}
	}
	c.queueMu.Unlock()
	for _, ids := range toRelease {
		c.notifyWaiters(ids, runResult{err: context.Canceled})
	}
	if !found {
		logs.InfoContextf(ctx, "coordinator cancel not found: run_id=%s (may have completed or be in debounce)", runID)
	}
}

// hasAnyWaiter 判断 waiterIDs 是否与 targetSet 有交集（用于 merged batch 成员认定）。
func hasAnyWaiter(waiterIDs []uint64, targetSet map[uint64]struct{}) bool {
	for _, id := range waiterIDs {
		if _, ok := targetSet[id]; ok {
			return true
		}
	}
	return false
}

// RegisterRun records an active run for cancellation tracking.
func (c *Coordinator) RegisterRun(sessionKey, runID, taskID, sessionID string, members []runMember, cancel context.CancelFunc) {
	if sessionKey == "" {
		return
	}
	c.activeRunsMu.Lock()
	if c.activeRuns == nil {
		c.activeRuns = make(map[string]*activeRun)
	}
	c.activeRuns[sessionKey] = &activeRun{
		runID:     runID,
		taskID:    taskID,
		sessionID: sessionID,
		members:   cloneRunMembers(members),
		cancel:    cancel,
		startedAt: time.Now(),
	}
	c.activeRunsMu.Unlock()
}

// UnregisterRun removes a previously registered active run.
func (c *Coordinator) UnregisterRun(sessionKey string) {
	if sessionKey == "" {
		return
	}
	c.activeRunsMu.Lock()
	delete(c.activeRuns, sessionKey)
	c.activeRunsMu.Unlock()
}

// RunningRun 是正在执行的任务的轻量摘要，由 Coordinator.Status 返回。
type RunningRun struct {
	RunID     string
	TaskID    string
	SessionID string
	StartedAt time.Time
}

// WaitingRun 是已接收但尚未开始执行的任务的轻量摘要，由 Coordinator.Status 返回。
type WaitingRun struct {
	RunID      string
	TaskID     string
	SessionID  string
	StreamSeqs []uint64
	Status     string
}

// RunStatus 是 Coordinator 调度状态的只读快照，用于运维状态查询。
type RunStatus struct {
	MaxConcurrency          int
	ComputeBusyCount        int
	InteractionWaitingCount int
	// Running 正在执行（已持有计算槽）的任务。
	Running []RunningRun
	// Waiting 已接收但尚未开始执行的任务。
	// 包括 per-session 队列中等待调度的批次与已出队但仍在等待计算槽的批次。
	Waiting []WaitingRun
	// Debouncing 是仍处于 debounce 窗口、尚未进入调度队列的 Run。
	Debouncing []WaitingRun
}

// Status 返回 Coordinator 当前调度状态的只读快照。
// 该方法是只读的，不加锁批量读取时可能拿到近似一致的视图，
// 但对诊断 Worker 运行水位足够精确。
func (c *Coordinator) Status() RunStatus {
	status := RunStatus{MaxConcurrency: c.maxConcurrency, ComputeBusyCount: len(c.slots)}
	c.waitsMu.Lock()
	status.InteractionWaitingCount = len(c.waits)
	c.waitsMu.Unlock()

	runningKeys := make(map[string]struct{})
	c.activeRunsMu.RLock()
	for _, ar := range c.activeRuns {
		members := ar.members
		if len(members) == 0 {
			members = []runMember{{runID: ar.runID, taskID: ar.taskID, sessionID: ar.sessionID}}
		}
		for _, member := range members {
			if member.runID == "" {
				continue
			}
			status.Running = append(status.Running, RunningRun{
				RunID:     member.runID,
				TaskID:    member.taskID,
				SessionID: member.sessionID,
				StartedAt: ar.startedAt,
			})
			runningKeys[member.runID] = struct{}{}
		}
	}
	c.activeRunsMu.RUnlock()

	// 等待任务 = per-session 队列中的批次 + 已出队等待计算槽的批次。
	// 已持有计算槽并开始执行的批次会在 runBatch 结束前同时存在于 slotWaiting，
	// 因此用 runningKeys 排除那些已转入 running 的批次，避免 running/waiting 重复计数。
	seen := make(map[string]struct{})
	var waiting []WaitingRun

	for _, submission := range c.debouncer.PendingValues() {
		addWaitingSubmission(submission, "debouncing", &status.Debouncing, seen, runningKeys)
	}

	c.slotWaitingMu.Lock()
	for _, batch := range c.slotWaiting {
		addWaiting(batch, "slot_waiting", &waiting, seen, runningKeys)
	}
	c.slotWaitingMu.Unlock()

	c.queueMu.Lock()
	for _, q := range c.queued {
		for _, batch := range q {
			addWaiting(batch, "queued", &waiting, seen, runningKeys)
		}
	}
	c.queueMu.Unlock()

	status.Waiting = waiting
	return status
}

// addWaiting 将一批等待调度的批次去重后追加到 waiting 列表。
// runningKeys 中已开始的 runID 会被跳过（已计入 Running，不计入 Waiting）。
func addWaiting(batch *batchEntry, phase string, dst *[]WaitingRun, seen map[string]struct{}, runningKeys map[string]struct{}) {
	if batch == nil {
		return
	}
	addWaitingSubmission(batch.submission, phase, dst, seen, runningKeys)
}

func addWaitingSubmission(submission RunSubmission, phase string, dst *[]WaitingRun, seen map[string]struct{}, runningKeys map[string]struct{}) {
	members := submission.members
	if len(members) == 0 {
		members = []runMember{{
			runID:      submission.EventContext.RunID,
			taskID:     requestTaskID(submission),
			sessionID:  submission.EventContext.SessionID,
			streamSeqs: submission.DeliverySeqs,
		}}
	}
	for _, member := range members {
		if member.runID == "" {
			continue
		}
		if _, ok := seen[member.runID]; ok {
			continue
		}
		if _, ok := runningKeys[member.runID]; ok {
			continue
		}
		seen[member.runID] = struct{}{}
		*dst = append(*dst, WaitingRun{
			RunID:      member.runID,
			TaskID:     member.taskID,
			SessionID:  member.sessionID,
			StreamSeqs: append([]uint64(nil), member.streamSeqs...),
			Status:     phase,
		})
	}
}

// Close shuts down the coordinator gracefully, waiting for all in-flight runs.
func (c *Coordinator) Close() error {
	if c == nil {
		return nil
	}
	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		c.submissions.Wait()
		c.dispatchers.Wait()
		c.runs.Wait()
		return nil
	}
	c.closed = true
	c.debouncer.Close()
	// 取消 Coordinator 根上下文，级联取消所有排队/slot-wait/运行中批次及其 Runtime。
	c.execCancel()
	waiters := make([]chan runResult, 0, len(c.pending))
	for waiterID, ch := range c.pending {
		delete(c.pending, waiterID)
		waiters = append(waiters, ch)
	}
	c.stateMu.Unlock()

	// 唤醒 dispatcher 使其退出。
	c.signalDispatcher()

	for _, ch := range waiters {
		ch <- runResult{err: errCoordinatorClosed}
	}
	c.submissions.Wait()
	c.dispatchers.Wait()
	// 等待所有批次 goroutine（runBatch）真正退出，确保 Close 返回时没有 Runtime 仍在运行。
	c.runs.Wait()
	return nil
}

// sessionKey builds a unique key for per-session serialization.
func sessionKey(submission RunSubmission) string {
	ec := submission.EventContext
	if ec.OrgID == 0 || ec.WorkerID == 0 || strings.TrimSpace(ec.SessionID) == "" {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s", ec.OrgID, ec.WorkerID, strings.TrimSpace(ec.SessionID))
}

// debounceKey 生成带来源的 debounce 合并键：恢复(1)与实时(0)任务使用不同的键，
// 禁止跨来源合并，避免恢复批次丢失/污染实时批次的 Recovered·NotAfter 语义。
func debounceKey(sessionKey string, recovered bool) string {
	if recovered {
		return sessionKey + ":recovered"
	}
	return sessionKey + ":live"
}

// activeRunKey builds a unique key for tracking an active run, scoped to a
// specific run ID so that multiple runs on the same session+worker can
// coexist and be cancelled independently.
func activeRunKey(orgID, workerID uint, sessionID, runID string) string {
	return fmt.Sprintf("%d:%d:%s:%s", orgID, workerID, sessionID, runID)
}

// mergeSubmissions merges two submissions for the same session.
func mergeSubmissions(existing RunSubmission, incoming RunSubmission) RunSubmission {
	merged := existing
	merged.DeliverySeqs = appendUniqueUint64(nil, existing.DeliverySeqs...)
	merged.DeliverySeqs = appendUniqueUint64(merged.DeliverySeqs, incoming.DeliverySeqs...)
	merged.waiterIDs = append(append([]uint64(nil), existing.waiterIDs...), incoming.waiterIDs...)
	// 收集所有成员 runID，保证取消任一成员都能定位整个（可能已 active 的）合并批次。
	merged.memberRunIDs = append(append([]string(nil), existing.memberRunIDs...), incoming.memberRunIDs...)
	merged.members = append(cloneRunMembers(existing.members), cloneRunMembers(incoming.members)...)
	merged.EventContext.ReplyToMessageIDs = appendUniqueString(
		append([]string(nil), existing.EventContext.ReplyToMessageIDs...),
		incoming.EventContext.ReplyToMessageIDs...,
	)
	merged.EventContext.MemberCommandIDs = appendUniqueString(
		append([]string(nil), existing.EventContext.MemberCommandIDs...),
		incoming.EventContext.MemberCommandIDs...,
	)

	// Merge input messages.
	if incoming.Request != nil {
		existingReq := agentrundomain.CloneRequest(merged.Request)
		if existingReq == nil {
			existingReq = &agentrundomain.RunRequest{}
		}
		incomingReq := incoming.Request
		existingReq.Input.Messages = append(existingReq.Input.Messages, incomingReq.Input.Messages...)
		existingReq.Input.Attachments = append(existingReq.Input.Attachments, incomingReq.Input.Attachments...)
		merged.Request = existingReq
	}

	return merged
}

func cloneRunMembers(in []runMember) []runMember {
	if len(in) == 0 {
		return nil
	}
	out := make([]runMember, 0, len(in))
	for _, member := range in {
		member.streamSeqs = append([]uint64(nil), member.streamSeqs...)
		out = append(out, member)
	}
	return out
}

func appendUniqueUint64(dst []uint64, values ...uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(dst)+len(values))
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

func appendUniqueString(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(values))
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}
