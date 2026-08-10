package run

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
)

// newBlockingCoordinator 构造一个 execute 会阻塞到 release 的 Coordinator，
// 便于稳定地制造 running / slot-wait / queued 三种状态。
// 返回值 cleanup 必须由测试在所有 Submit 完成后调用（会在 Close 前先 release）。
func newBlockingCoordinator(t *testing.T, maxConcurrency int) (*Coordinator, func()) {
	t.Helper()
	var executing atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	co, err := NewCoordinator(Config{
		MaxConcurrency: maxConcurrency,
		DebounceWindow: 10 * time.Millisecond,
	}, func(_ context.Context, _ RunSubmission) (*agentrundomain.RunResult, error) {
		wg.Add(1)
		defer wg.Done()
		executing.Add(1)
		<-release
		executing.Add(-1)
		return runResultQuick(), nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	cleanup := func() {
		close(release)
		co.Close()
		wg.Wait()
	}
	return co, cleanup
}

func TestCoordinatorStatusRunningAndSlotWaiting(t *testing.T) {
	co, cleanup := newBlockingCoordinator(t, 1)
	defer cleanup()

	// 两个不同会话：第一个拿槽运行（running），第二个因计算槽满进入 slot-wait。
	go func() {
		_, _ = co.Submit(context.Background(), testSubmissionSession("run-running", "m1", 1, "session-run"))
	}()
	// 等待第一个任务真正开始执行，再提交第二个。
	waitRunningStarted(t, co)
	go func() {
		_, _ = co.Submit(context.Background(), testSubmissionSession("run-slotwait", "m2", 2, "session-slotwait"))
	}()

	waitFor(t, "running", func() bool { return len(co.Status().Running) == 1 })
	st := co.Status()
	if len(st.Running) != 1 {
		t.Fatalf("running = %+v, want exactly 1", st.Running)
	}
	if got := st.Running[0]; got.RunID != "run-running" || got.TaskID != "task-1" || got.SessionID != "session-run" {
		t.Fatalf("running summary = %+v", got)
	}
	if st.Running[0].StartedAt.IsZero() {
		t.Fatalf("running task started_at is zero, want set")
	}
	if got := st.MaxConcurrency; got != 1 {
		t.Fatalf("max_concurrency = %d, want 1", got)
	}

	// slot-wait 任务应出现在 waiting 中。
	waitFor(t, "waiting", func() bool { return len(co.Status().Waiting) == 1 })
	w := co.Status().Waiting
	if len(w) != 1 || w[0].RunID != "run-slotwait" {
		t.Fatalf("waiting = %+v, want run-slotwait", w)
	}
	// Submit goroutine 会在 cleanup 关闭 release 后自然返回。
}

func TestCoordinatorStatusWaitingCountsQueuedDuringDebounce(t *testing.T) {
	co, cleanup := newBlockingCoordinator(t, 1)
	defer cleanup()

	// 第一个任务占用唯一计算槽。
	go func() {
		_, _ = co.Submit(context.Background(), testSubmissionSession("run-q1", "m1", 1, "session-1"))
	}()
	waitRunningStarted(t, co)

	// 第二个任务同会话，进入 per-session 队列（session-1 busy）。
	go func() {
		_, _ = co.Submit(context.Background(), testSubmissionSession("run-q2", "m2", 2, "session-1"))
	}()

	waitFor(t, "running+waiting==2", func() bool {
		st := co.Status()
		return len(st.Running)+len(st.Waiting) == 2
	})
}

func TestCoordinatorStatusEmptySnapshot(t *testing.T) {
	co, cleanup := newBlockingCoordinator(t, 4)
	defer cleanup()

	st := co.Status()
	if len(st.Running) != 0 || len(st.Waiting) != 0 {
		t.Fatalf("empty coordinator status = running=%+v waiting=%+v, want empty", st.Running, st.Waiting)
	}
	if st.MaxConcurrency != 4 {
		t.Fatalf("max_concurrency = %d, want 4", st.MaxConcurrency)
	}
}

func TestCoordinatorStatusIncludesMergedDebouncingRuns(t *testing.T) {
	co, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: 500 * time.Millisecond,
	}, func(_ context.Context, _ RunSubmission) (*agentrundomain.RunResult, error) {
		return runResultQuick(), nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	done := make(chan error, 2)
	for _, runID := range []string{"run-debounce-a", "run-debounce-b"} {
		go func(id string) {
			_, submitErr := co.Submit(context.Background(), testSubmissionSession(id, id, 1, "session-debounce"))
			done <- submitErr
		}(runID)
	}

	waitFor(t, "merged debounce status", func() bool {
		return len(co.Status().Debouncing) == 2
	})
	status := co.Status()
	seen := make(map[string]struct{}, len(status.Debouncing))
	for _, run := range status.Debouncing {
		seen[run.RunID] = struct{}{}
	}
	if _, ok := seen["run-debounce-a"]; !ok {
		t.Fatalf("debouncing = %+v, missing run-debounce-a", status.Debouncing)
	}
	if _, ok := seen["run-debounce-b"]; !ok {
		t.Fatalf("debouncing = %+v, missing run-debounce-b", status.Debouncing)
	}

	if err := co.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err == nil {
			t.Fatal("Submit() error = nil after coordinator close, want closed error")
		}
	}
}

// waitRunningStarted 轮询等待 coordinator 至少有一个 active run。
func waitRunningStarted(t *testing.T, co *Coordinator) {
	t.Helper()
	waitFor(t, "a running task", func() bool { return len(co.Status().Running) > 0 })
}

// waitFor 轮询 cond 直到满足或超时。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}
