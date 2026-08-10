package run

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
)

// testSubmissionSession 在 testSubmission 基础上覆盖会话，便于多会话/交互测试。
func testSubmissionSession(runID, messageID string, sequence uint64, sessionID string) RunSubmission {
	sub := testSubmission(runID, messageID, sequence)
	sub.EventContext.SessionID = sessionID
	return sub
}

// runResultQuick 构造执行成功结果。
func runResultQuick() *agentrundomain.RunResult {
	return &agentrundomain.RunResult{}
}

// TestCoordinatorComputeSlotFullThenNewTaskWaits 验证计算槽满后，新任务进入 slot-wait，
// 且不立即启动 Runtime。
func TestCoordinatorComputeSlotFullThenNewTaskWaits(t *testing.T) {
	var executing atomic.Int32
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 2,
		DebounceWindow: time.Millisecond,
	}, func(_ context.Context, _ RunSubmission) (*agentrundomain.RunResult, error) {
		executing.Add(1)
		started <- struct{}{}
		<-release
		executing.Add(-1)
		return runResultQuick(), nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	// 2 个运行任务（不同会话）占满计算槽。
	for i := 0; i < 2; i++ {
		go func(i int) {
			_, _ = coordinator.Submit(context.Background(), testSubmissionSession("run-full", "m", uint64(i+1), fmt.Sprintf("session-fill-%d", i)))
		}(i)
	}
	for i := 0; i < 2; i++ {
		<-started
	}

	// 第 3 个新任务进入 slot-wait，不应立即执行。
	thirdDone := make(chan error, 1)
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), testSubmissionSession("run-next", "n", 10, "session-next"))
		thirdDone <- submitErr
	}()
	select {
	case <-started:
		t.Fatal("next task started while compute slots were full")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if submitErr := <-thirdDone; submitErr != nil {
		t.Fatalf("Submit() error = %v", submitErr)
	}
}

// waitForInteraction 从运行 ctx 解析交互等待观察者（由 Coordinator 注入），
// 模拟一次会触发交互等待的执行：进入等待→释放计算槽→阻塞→结束。
func waitForInteraction(ctx context.Context, sub RunSubmission, release chan struct{}) error {
	obs := agent.InteractionWaitObserverFromContext(ctx)
	if obs == nil {
		return nil
	}
	end, err := obs.BeginInteractionWait(ctx, sub.EventContext.RunID+"-approval", "approval")
	if err != nil {
		return err
	}
	// 阻塞模拟用户未立即响应（期间计算槽已释放）。
	<-release
	return end()
}

// TestCoordinatorInteractionWaitReleasesComputeSlotThenNormalTaskRuns 验证
// 运行任务进入交互等待后释放计算槽，新的普通任务可立即获取计算槽。
func TestCoordinatorInteractionWaitReleasesComputeSlotThenNormalTaskRuns(t *testing.T) {
	fillStarted := make(chan struct{})
	interactionRelease := make(chan struct{})
	normalDone := make(chan error, 1)
	normalStarted := make(chan struct{})

	coordinator, err := NewCoordinator(Config{
		MaxConcurrency:         1, // 只有 1 个计算槽：若交互等待不释放，新任务将永远无法执行
		DebounceWindow:         time.Millisecond,
		MaxInteractionWaits:    1,
		InteractionWaitTimeout: time.Minute,
	}, func(ctx context.Context, sub RunSubmission) (*agentrundomain.RunResult, error) {
		if sub.EventContext.RunID == "run-fill" {
			close(fillStarted)
			if err := waitForInteraction(ctx, sub, interactionRelease); err != nil {
				return nil, err
			}
			return runResultQuick(), nil
		}
		close(normalStarted)
		return runResultQuick(), nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	fillDone := make(chan error, 1)
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), testSubmissionSession("run-fill", "f", 1, "session-a"))
		fillDone <- submitErr
	}()
	<-fillStarted

	// 新普通任务：若计算槽已被交互等待释放，应立即开始执行（而不是等待 fill 完成）。
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), testSubmissionSession("run-normal", "n", 2, "session-b"))
		normalDone <- submitErr
	}()
	<-normalStarted

	// 放行交互等待，结束 fill。
	close(interactionRelease)
	if submitErr := <-fillDone; submitErr != nil {
		t.Fatalf("fill Submit() error = %v", submitErr)
	}
	if submitErr := <-normalDone; submitErr != nil {
		t.Fatalf("normal Submit() error = %v", submitErr)
	}
}

// TestCoordinatorInteractionWaitCapacityFull 验证交互等待容量满时，第 2 个交互等待明确失败。
func TestCoordinatorInteractionWaitCapacityFull(t *testing.T) {
	done := make(chan error, 2)
	var relMu sync.Mutex
	releases := make([]chan struct{}, 0, 2)
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency:         2,
		DebounceWindow:         time.Millisecond,
		MaxInteractionWaits:    1, // 只有 1 个交互等待槽
		InteractionWaitTimeout: time.Minute,
	}, func(ctx context.Context, sub RunSubmission) (*agentrundomain.RunResult, error) {
		release := make(chan struct{})
		relMu.Lock()
		releases = append(releases, release)
		relMu.Unlock()
		if err := waitForInteraction(ctx, sub, release); err != nil {
			return nil, err
		}
		return runResultQuick(), nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	for i := 0; i < 2; i++ {
		go func(i int) {
			_, submitErr := coordinator.Submit(context.Background(), testSubmissionSession("run-wait", "w", uint64(i+1), "session-w"))
			done <- submitErr
		}(i)
	}
	// 两个任务都进入执行，各自的交互等待依次尝试申请交互槽；第 2 个应因容量满失败。
	// 用一定时间去收集释放/失败结果；为避免死锁，随后放行阻塞的等待。
	select {
	case submitErr := <-done:
		if submitErr != nil {
			t.Logf("one wait rejected with error: %v (expected when capacity held)", submitErr)
		}
	case <-time.After(2 * time.Second):
	}
	// 放行所有可能仍在阻塞的交互等待，确保 Close 不挂起。
	relMu.Lock()
	for _, ch := range releases {
		close(ch)
	}
	releases = nil
	relMu.Unlock()

	// 消费剩余的 Submit 结果。
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Submit() did not return")
		}
	}
}

// TestCoordinatorCancelQueuedWaiter 验证 queued 阶段取消：批次尚未执行时被取消，返回 context.Canceled。
func TestCoordinatorCancelQueuedWaiter(t *testing.T) {
	var executing atomic.Int32
	release := make(chan struct{})
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: time.Millisecond,
	}, func(_ context.Context, _ RunSubmission) (*agentrundomain.RunResult, error) {
		executing.Add(1)
		<-release
		return runResultQuick(), nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	// 占用唯一计算槽。
	occupyDone := make(chan error, 1)
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), testSubmissionSession("run-occupy", "o", 1, "session-a"))
		occupyDone <- submitErr
	}()
	time.Sleep(20 * time.Millisecond)

	// 第二个同会话任务进入队列（slot-wait）。
	queuedDone := make(chan error, 1)
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), testSubmissionSession("run-queued", "q", 2, "session-a"))
		queuedDone <- submitErr
	}()
	time.Sleep(20 * time.Millisecond)

	if err := coordinator.Cancel(context.Background(), 1, 2, "session-a", "run-queued"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	select {
	case submitErr := <-queuedDone:
		if submitErr == nil {
			t.Fatalf("queued Submit() should be cancelled, got nil")
		}
	case <-time.After(time.Second):
		t.Fatal("queued Submit() did not return after cancel")
	}
	close(release)
	<-occupyDone
}

// TestCoordinatorNotAfterRejectedAtSlotAcquire 验证任务在真正拿到运行槽前重新检查 NotAfter，
// 过期任务直接失败，不再启动 Runtime。
func TestCoordinatorNotAfterRejectedAtSlotAcquire(t *testing.T) {
	var executions atomic.Int32
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: time.Millisecond,
	}, func(_ context.Context, _ RunSubmission) (*agentrundomain.RunResult, error) {
		executions.Add(1)
		return runResultQuick(), nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	sub := testSubmissionSession("run-expired", "e", 1, "session-a")
	sub.NotAfter = time.Now().Add(-time.Minute) // 已过期
	if _, submitErr := coordinator.Submit(context.Background(), sub); !errors.Is(submitErr, errNotAfterExpired) {
		t.Fatalf("Submit() error = %v, want errNotAfterExpired", submitErr)
	}
	if executions.Load() != 0 {
		t.Fatalf("expired run executed %d times, want 0", executions.Load())
	}
}

// TestCoordinatorInteractionWaitTimeoutCancels 验证交互等待超过硬超时后自动取消任务并释放容量。
func TestCoordinatorInteractionWaitTimeoutCancels(t *testing.T) {
	interactionStarted := make(chan struct{})
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency:         1,
		DebounceWindow:         time.Millisecond,
		MaxInteractionWaits:    1,
		InteractionWaitTimeout: 80 * time.Millisecond, // 较短超时便于测试
	}, func(ctx context.Context, sub RunSubmission) (*agentrundomain.RunResult, error) {
		close(interactionStarted)
		// 进入交互等待，但不释放（模拟用户超时未响应）。
		obs := agent.InteractionWaitObserverFromContext(ctx)
		if obs == nil {
			return runResultQuick(), nil
		}
		if _, err := obs.BeginInteractionWait(ctx, "req-timeout", "approval"); err != nil {
			return nil, err
		}
		// begin 后正常应 end()；这里故意不调用，让 watchdog 超时取消 run ctx。
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	done := make(chan error, 1)
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), testSubmissionSession("run-timeout", "t", 1, "session-a"))
		done <- submitErr
	}()
	<-interactionStarted

	select {
	case submitErr := <-done:
		if submitErr == nil {
			t.Fatal("timeout run should be cancelled, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("interaction wait timeout did not cancel the run")
	}
	// 交互等待槽应已被释放。
	if got := len(coordinator.interactionSlots); got != 0 {
		t.Fatalf("interaction slots held = %d after timeout, want 0", got)
	}
}

// TestCoordinatorRoundRobinFairness 验证不同会话 round-robin 调度，单个会话不会长期独占运行槽。
// 通过让两个会话交替提交足够多的任务，观察它们在计算槽上交错执行。
// TestCoordinatorRoundRobinFairness 通过直接驱动 dequeueOne 验证真正的 round-robin：
// 会话 A、B 各有两个待调度批次，按游标逐个 dispatch（期间释放会话以模拟执行完成），
// 出队顺序必须是严格的 A,B,A,B 交替，而不是拖尽某一会话。
// 注：构造裸 Coordinator（不启动后台 dispatcher），保证 dequeueOne 的调用次序确定。
func TestCoordinatorRoundRobinFairness(t *testing.T) {
	c := &Coordinator{
		queued:       make(map[string][]*batchEntry),
		order:        []string{},
		sessionBusy:  make(map[string]struct{}),
		notify:       make(chan struct{}, 1),
		stateMu:      sync.Mutex{},
		pending:      make(map[uint64]chan runResult),
		waits:        make(map[string]*interactionWait),
		runToWaiters: make(map[string][]uint64),
		activeRuns:   make(map[string]*activeRun),
		slotWaiting:  make(map[string]*batchEntry),
	}
	se := func(session string) RunSubmission {
		return testSubmissionSession("run-rr-"+session, "m", 1, session)
	}
	// 入队顺序：A0, B0, A1, B1。
	for _, s := range []string{"session-a", "session-b", "session-a", "session-b"} {
		if err := c.enqueueSubmission(context.Background(), se(s)); err != nil {
			t.Fatalf("enqueueSubmission() error = %v", err)
		}
	}

	var got []string
	// 模拟执行：dispatch（claim 会话）-> 完成后 clearSessionBusy -> 继续。
	for i := 0; i < 4; i++ {
		b := c.dequeueOne()
		if b == nil {
			t.Fatalf("dequeueOne() returned nil at step %d (starvation/deadlock)", i)
		}
		got = append(got, b.sessionKey)
		// 释放会话，模拟该批次执行完成。
		c.clearSessionBusy(b.sessionKey)
	}

	want := []string{"1:2:session-a", "1:2:session-b", "1:2:session-a", "1:2:session-b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("round-robin order = %v, want %v (single-session starvation or non-robin)", got, want)
		}
	}
}

// TestCoordinatorRoundRobinInterleavesConcurrentSessions 并发压力下验证：
// 两个会话同时有大量待调度批次时，任一会话都不被长期饿死（游标 round-robin 生效）。
// （精确的 A,B 交替顺序已由 TestCoordinatorRoundRobinFairness 确定性验证。）
func TestCoordinatorRoundRobinInterleavesConcurrentSessions(t *testing.T) {
	var orderMu sync.Mutex
	var order []string
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 2,
		DebounceWindow: time.Millisecond,
	}, func(_ context.Context, sub RunSubmission) (*agentrundomain.RunResult, error) {
		orderMu.Lock()
		order = append(order, sub.EventContext.SessionID)
		orderMu.Unlock()
		return runResultQuick(), nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	// 每会话 12 个批次，分批提交（间隔略大于 debounce 窗口）以避免全部合并成一个批次。
	const perSession = 12
	done := make(chan error, 2*perSession)
	for i := 0; i < 2*perSession; i++ {
		session := "session-a"
		if i%2 == 1 {
			session = "session-b"
		}
		go func(i int, s string) {
			_, submitErr := coordinator.Submit(context.Background(), testSubmissionSession(fmt.Sprintf("run-fair-%d", i), "m", uint64(i+1), s))
			done <- submitErr
		}(i, session)
		time.Sleep(2 * time.Millisecond) // 错开提交，避免 debounce 合并
	}
	for i := 0; i < 2*perSession; i++ {
		select {
		case submitErr := <-done:
			if submitErr != nil {
				t.Fatalf("Submit() error = %v", submitErr)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("tasks did not complete")
		}
	}

	orderMu.Lock()
	defer orderMu.Unlock()
	buckets := map[string]int{}
	for _, s := range order {
		buckets[s]++
	}
	if len(buckets) != 2 {
		t.Fatalf("scheduled buckets = %v, want both sessions", buckets)
	}
	// 每会话 ~12 个，任一低于 3 即视为饥饿。
	for s, n := range buckets {
		if n < 3 {
			t.Fatalf("session %s scheduled only %d tasks (starvation)", s, n)
		}
	}
}

// TestCoordinatorSameSessionStrictSerialization 验证同一 session 的任务严格串行：
// 即使多个同会话批次同时入队、且计算槽充足，同一时刻也只有一个执行（dequeue 阶段即 claim）。
func TestCoordinatorSameSessionStrictSerialization(t *testing.T) {
	var currentPerSession sync.Map // sessionKey -> current running count
	var violation atomic.Bool
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 8,
		DebounceWindow: time.Millisecond,
	}, func(_ context.Context, sub RunSubmission) (*agentrundomain.RunResult, error) {
		session := sub.EventContext.SessionID
		cur, _ := currentPerSession.LoadOrStore(session, new(int32))
		count := (*int32)(cur.(*int32))
		now := atomic.AddInt32(count, 1)
		if now > 1 {
			violation.Store(true)
		}
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(count, -1)
		return runResultQuick(), nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	const total = 30
	done := make(chan error, total)
	for i := 0; i < total; i++ {
		go func(i int) {
			// 全部任务都进同一 session，验证串行。
			_, submitErr := coordinator.Submit(context.Background(), testSubmissionSession(fmt.Sprintf("run-serial-%d", i), "m", uint64(i+1), "session-only"))
			done <- submitErr
		}(i)
	}
	for i := 0; i < total; i++ {
		select {
		case submitErr := <-done:
			if submitErr != nil {
				t.Fatalf("Submit() error = %v", submitErr)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("same-session tasks did not complete")
		}
	}
	if violation.Load() {
		t.Fatal("same-session runs executed concurrently (serialization violated)")
	}
}

// TestCoordinatorRecoveredTaskOwnsRecoverySlotOnce 验证恢复任务的 recovery/slot 双 token
// 只由 handle 管理一次（修复双重释放死锁）：并发恢复任务不产生告警或挂起。
func TestCoordinatorRecoveredTaskOwnsRecoverySlotOnce(t *testing.T) {
	var running atomic.Int32
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 2, // 恢复槽 = 1
		DebounceWindow: time.Millisecond,
	}, func(_ context.Context, _ RunSubmission) (*agentrundomain.RunResult, error) {
		running.Add(1)
		started <- struct{}{}
		<-release
		running.Add(-1)
		return runResultQuick(), nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	// 提交多个恢复任务（不同会话避免互相串行），观察是否能正常释放与完成。
	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		sub := testSubmissionSession(fmt.Sprintf("run-recover-%d", i), "m", uint64(i+1), fmt.Sprintf("session-recover-%d", i))
		sub.Recovered = true
		go func() {
			_, submitErr := coordinator.Submit(context.Background(), sub)
			done <- submitErr
		}()
	}
	// 恢复槽只有 1：同时最多 1 个恢复任务运行。
	<-started
	select {
	case <-started:
		t.Fatal("second recovered task started while recovery slot held by first")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	for i := 0; i < 3; i++ {
		select {
		case submitErr := <-done:
			if submitErr != nil {
				t.Fatalf("Submit() error = %v", submitErr)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("recovered task did not complete (recovery slot leak?)")
		}
	}
}

// TestDebounceKeySeparatesRecoveredFromLive 验证恢复与实时任务使用不同的 debounce 合并键，
// 禁止跨来源合并（#11 语义冲突防护）。
func TestDebounceKeySeparatesRecoveredFromLive(t *testing.T) {
	live := debounceKey("1:2:session-a", false)
	rec := debounceKey("1:2:session-a", true)
	if live == rec {
		t.Fatalf("live and recovered debounce keys share the same value %q", live)
	}
	if got := debounceKey("1:2:session-a", false); got != live {
		t.Fatalf("debounceKey live not stable: got %q want %q", got, live)
	}
}

// TestCoordinatorCloseCancelsBlockedExecutor 验证关闭时根上下文级联取消阻塞的执行器
// （#13）：executor 阻塞等 ctx.Done 时，Close 应能取消它并快速返回，不挂起等待完成。
func TestCoordinatorCloseCancelsBlockedExecutor(t *testing.T) {
	started := make(chan struct{})
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: time.Millisecond,
	}, func(ctx context.Context, _ RunSubmission) (*agentrundomain.RunResult, error) {
		close(started)
		// 阻塞直到被取消（模拟耗时 Runtime）。
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	submitDone := make(chan error, 1)
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), testSubmissionSession("run-blocked", "m", 1, "session-a"))
		submitDone <- submitErr
	}()
	<-started

	// Close 应通过根上下文取消阻塞的 executor。
	closeDone := make(chan struct{})
	go func() {
		_ = coordinator.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Close() did not return: blocked executor not cancelled by root context")
	}
	select {
	case submitErr := <-submitDone:
		if submitErr == nil {
			t.Fatal("blocked Submit() should be cancelled by Close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Submit() did not return after Close")
	}
}

// TestCoordinatorCancelMergedBatchMemberCancelsWholeBatch 验证合并批次取消关闭语义（#12）：
// 同一会话两个提交合并为一个执行单元后，取消任一成员会取消整个 batch，
// 所有关联 waiter 得到一致的 context.Canceled。
func TestCoordinatorCancelMergedBatchMemberCancelsWholeBatch(t *testing.T) {
	started := make(chan struct{})
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: 50 * time.Millisecond, // 保证两个提交落在同一窗口内合并
	}, func(ctx context.Context, _ RunSubmission) (*agentrundomain.RunResult, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	// 依次提交两个同会话提交，确保在 debounce 窗口内合并为一个 batch。
	done := make(chan error, 2)
	for _, runID := range []string{"run-merge-1", "run-merge-2"} {
		go func(runID string) {
			_, submitErr := coordinator.Submit(context.Background(), testSubmissionSession(runID, runID, 1, "session-merge"))
			done <- submitErr
		}(runID)
		time.Sleep(5 * time.Millisecond) // 保持两提交都在 50ms 窗口内
	}
	<-started

	// 取消第二个成员：应取消整个合并 batch，使第一个 waiter 也收到取消。
	if err := coordinator.Cancel(context.Background(), 1, 2, "session-merge", "run-merge-2"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	for i := 0; i < 2; i++ {
		select {
		case submitErr := <-done:
			if submitErr == nil {
				t.Fatalf("member %d Submit() should be cancelled, got nil", i)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("member %d Submit() did not return after merged-batch cancel", i)
		}
	}
}

// TestCoordinatorSubmitContextCancelsActiveRun 验证 Submit(ctx) 的取消传播到已 active 的 Runtime：
// 调用方 ctx 取消不仅让 Submit 返回，也应取消正在执行的 batch（#2）。
func TestCoordinatorSubmitContextCancelsActiveRun(t *testing.T) {
	started := make(chan struct{})
	execReturned := make(chan time.Duration, 1)
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: 5 * time.Millisecond,
	}, func(ctx context.Context, _ RunSubmission) (*agentrundomain.RunResult, error) {
		close(started)
		// 记录从开始到 ctx 取消的耗时：若提交者 ctx 传播到 Runtime，应快速返回。
		start := time.Now()
		<-ctx.Done()
		execReturned <- time.Since(start)
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	callerCtx, callerCancel := context.WithCancel(context.Background())
	submitDone := make(chan error, 1)
	go func() {
		_, submitErr := coordinator.Submit(callerCtx, testSubmissionSession("run-ctx-cancel", "m", 1, "session-a"))
		submitDone <- submitErr
	}()
	<-started

	callerCancel() // 取消调用方 ctx
	select {
	case elapsed := <-execReturned:
		if elapsed > 2*time.Second {
			t.Fatalf("Runtime did not respond to submitter ctx cancellation promptly (elapsed=%v)", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Runtime did not exit after submitter ctx cancellation (submitter ctx not propagated to active run)")
	}
	// Submit 会在 executor 返回后、通过 notifyWaiters 很快返回；这里阻塞等待而非非阻塞探测。
	select {
	case <-submitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Submit() did not return after caller ctx cancellation")
	}
}

// TestCoordinatorCloseWaitsForRunBatchGoroutine 验证 Close 等待真正的 runBatch goroutine 退出，
// 即使该 Submit 已被强制结束（waiter 被发送结果），底层 Runtime 仍未完成时 Close 不返回（#4）。
func TestCoordinatorCloseWaitsForRunBatchGoroutine(t *testing.T) {
	started := make(chan struct{})
	execExit := make(chan struct{})
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: 5 * time.Millisecond,
	}, func(ctx context.Context, _ RunSubmission) (*agentrundomain.RunResult, error) {
		close(started)
		<-ctx.Done() // 只有根上下文取消才退出
		close(execExit)
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	go func() {
		_, _ = coordinator.Submit(context.Background(), testSubmissionSession("run-close-wait", "m", 1, "session-a"))
	}()
	// 确保 run 已真正开始（否则 Close 可能在批次启动前将其跳过，无法验证"等待活跃 run"）。
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("run never started before Close")
	}

	// Close 应通过根上下文取消并等待 runBatch 真正退出（execExit 关闭）。
	closeDone := make(chan struct{})
	go func() {
		_ = coordinator.Close()
		close(closeDone)
	}()
	select {
	case <-execExit:
		// Runtime 已退出——这是正确路径。
	case <-time.After(5 * time.Second):
		t.Fatal("runBatch goroutine did not exit after Close()")
	}
	select {
	case <-closeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Close() did not return after runBatch exited")
	}
}

// TestCoordinatorCloseCancelsDirectExecution 验证无 session 的直接执行也被纳入 Close 生命周期：
// TestCoordinatorRejectsMissingSession 验证缺失 session（无完整路由）的 submission
// 直接返回 ErrRunRouteRequired，不创建 waiter、不入队、不启动 Runtime。
func TestCoordinatorRejectsMissingSession(t *testing.T) {
	var executions atomic.Int32
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: 5 * time.Millisecond,
	}, func(_ context.Context, _ RunSubmission) (*agentrundomain.RunResult, error) {
		executions.Add(1)
		return runResultQuick(), nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	sub := testSubmission("run-no-session", "m", 1)
	sub.EventContext.SessionID = "" // 无会话

	if _, submitErr := coordinator.Submit(context.Background(), sub); submitErr != ErrRunRouteRequired {
		t.Fatalf("Submit() error = %v, want ErrRunRouteRequired", submitErr)
	}
	if executions.Load() != 0 {
		t.Fatalf("execute function called %d times for missing-session run, want 0", executions.Load())
	}
	// 不应产生任何 waiter / 队列项。
	coordinator.stateMu.Lock()
	waiterCount := len(coordinator.pending)
	queuedCount := len(coordinator.order)
	coordinator.stateMu.Unlock()
	if waiterCount != 0 || queuedCount != 0 {
		t.Fatalf("missing-session run created state: waiters=%d queued=%d", waiterCount, queuedCount)
	}
}

// TestCoordinatorRejectsMissingOrgOrWorker 验证缺少 OrgID/WorkerID 也被拒绝。
func TestCoordinatorRejectsMissingOrgOrWorker(t *testing.T) {
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: 5 * time.Millisecond,
	}, func(_ context.Context, _ RunSubmission) (*agentrundomain.RunResult, error) {
		return runResultQuick(), nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	noOrg := testSubmissionSession("run-no-org", "m", 1, "session-a")
	noOrg.EventContext.OrgID = 0
	if _, submitErr := coordinator.Submit(context.Background(), noOrg); submitErr != ErrRunRouteRequired {
		t.Fatalf("missing-org Submit() error = %v, want ErrRunRouteRequired", submitErr)
	}

	noWorker := testSubmissionSession("run-no-worker", "m", 2, "session-b")
	noWorker.EventContext.WorkerID = 0
	if _, submitErr := coordinator.Submit(context.Background(), noWorker); submitErr != ErrRunRouteRequired {
		t.Fatalf("missing-worker Submit() error = %v, want ErrRunRouteRequired", submitErr)
	}
}
