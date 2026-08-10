package run

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/internal/worker/agentrun"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

// multiRunRuntime 允许并发执行多个 run：每个 run 开启时递增 started，
// 阻塞在 gate 上（gate 关闭后放行全部）。
type multiRunRuntime struct {
	started int32
	gate    chan struct{}
	err     error
}

func (*multiRunRuntime) Name() string { return "test" }
func (r *multiRunRuntime) Execute(_ context.Context, _ agent.ExecutionRequest, _ agent.NodeObserver) (agent.ExecutionResult, error) {
	atomic.AddInt32(&r.started, 1)
	<-r.gate
	if r.err != nil {
		return agent.ExecutionResult{}, r.err
	}
	return agent.ExecutionResult{Message: "done"}, nil
}

// newStatusTestHandler 构造一个会阻塞首个 run 执行的 Handler，便于观测 Status。
func newStatusTestHandler(t *testing.T) (*Handler, *multiRunRuntime, *mockInbox) {
	t.Helper()
	runtime := &multiRunRuntime{gate: make(chan struct{})}
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, err := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, MaxInflight: 2, DebounceWindow: 10 * time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ib := newMockInbox()
	h.runInbox = ib
	t.Cleanup(func() {
		close(runtime.gate)
		h.Close()
	})
	return h, runtime, ib
}

// mustHandle 同步处理一条 run 命令（已加入标准 model 配置）。
func mustHandle(t *testing.T, h *Handler, runID string, seq uint64) {
	t.Helper()
	cmd := standardCommand()
	cmd.Trace.RunID = runID
	cmd.Trace.TaskID = "task-" + runID
	cmd.ID = "command-" + runID
	delivery := newFakeDelivery(seq)
	if err := h.HandleRunCommand(context.Background(), cmd, delivery); err != nil {
		t.Fatalf("HandleRunCommand error = %v", err)
	}
}

func TestHandlerStatusRunningAndWaitingCounts(t *testing.T) {
	h, _, _ := newStatusTestHandler(t)

	// 第一个任务占用唯一计算槽并开始执行。
	mustHandle(t, h, "run-running", 1)
	waitForStatus(t, h, func(s messaging.WorkerStatusSnapshot) bool {
		return s.RunningCount == 1
	})

	// 第二个任务因计算槽满进入 coordinator 等待。
	mustHandle(t, h, "run-waiting", 2)
	waitForStatus(t, h, func(s messaging.WorkerStatusSnapshot) bool {
		return len(s.WaitingTasks) == 1 && s.WaitingTasks[0].RunID == "run-waiting"
	})

	// 两个任务都在途（阻塞在 gate 上），校验运行/等待/准入/inbox 计数。
	st := h.Status(context.Background())
	if st.RunningCount != 1 {
		t.Fatalf("running_count = %d, want 1", st.RunningCount)
	}
	if len(st.WaitingTasks) != 1 {
		t.Fatalf("waiting_tasks = %+v, want 1", st.WaitingTasks)
	}
	if st.InboxProcessingCount < 1 {
		t.Fatalf("inbox_processing_count = %d, want >= 1 (at least the running command)", st.InboxProcessingCount)
	}
	if st.AcceptedCount < 2 {
		t.Fatalf("accepted_count = %d, want >= 2 (both commands owned)", st.AcceptedCount)
	}
	if st.MaxConcurrency != 1 {
		t.Fatalf("max_concurrency = %d, want 1", st.MaxConcurrency)
	}
}

func TestHandlerStatusIncludesDetailedFields(t *testing.T) {
	h, _, _ := newStatusTestHandler(t)

	mustHandle(t, h, "run-detailed", 77)
	waitForStatus(t, h, func(s messaging.WorkerStatusSnapshot) bool {
		return s.RunningCount == 1
	})

	st := h.Status(context.Background())
	if len(st.RunningTasks) != 1 {
		t.Fatalf("running_tasks = %+v, want 1", st.RunningTasks)
	}
	run := st.RunningTasks[0]
	if run.RunID != "run-detailed" {
		t.Fatalf("run_id = %q, want run-detailed", run.RunID)
	}
	if run.CommandID == "" {
		t.Fatalf("command_id empty, want enriched from inbox")
	}
	if run.StreamSeq != 77 {
		t.Fatalf("stream_seq = %d, want 77", run.StreamSeq)
	}
	if run.CreatedAt == 0 || run.UpdatedAt == 0 {
		t.Fatalf("timestamps zero: created=%d updated=%d", run.CreatedAt, run.UpdatedAt)
	}
	if run.StartedAt == 0 {
		t.Fatalf("started_at zero for running task")
	}
	if run.Status != "running" {
		t.Fatalf("status = %q, want running", run.Status)
	}
	if run.SessionID != "session-1" {
		t.Fatalf("session_id = %q, want session-1", run.SessionID)
	}
}

// TestStatusSnapshotJSONSerializable 验证快照可被 JSON 序列化且不含敏感字段。
func TestStatusSnapshotJSONSerializable(t *testing.T) {
	st := messaging.WorkerStatusSnapshot{
		MaxConcurrency:        1,
		RunningCount:          1,
		WaitingCount:          1,
		AdmissionWaitingCount: 0,
		AcceptedCount:         2,
		InboxPendingCount:     1,
		InboxProcessingCount:  1,
		SnapshotAt:            1700000000,
		RunningTasks: []messaging.WorkerRunSummary{
			{RunID: "r1", TaskID: "t1", SessionID: "s1", CommandID: "c1", StreamSeq: 1, Status: "running", CreatedAt: 1, UpdatedAt: 2, StartedAt: 3},
		},
		WaitingTasks: []messaging.WorkerRunSummary{
			{RunID: "r2", TaskID: "t2", SessionID: "s2", CommandID: "c2", StreamSeq: 2, Status: "waiting", CreatedAt: 4, UpdatedAt: 5},
		},
	}
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	// 摘要不应包含 prompt / api_key / 原始消息内容。
	for _, forbidden := range []string{"\"prompt\"", "api_key", "\"content\":\"hello\""} {
		if contains(string(data), forbidden) {
			t.Fatalf("snapshot unexpectedly contains sensitive field %s: %s", forbidden, data)
		}
	}
}

// waitForStatus 轮询等待 Status() 满足条件。
func waitForStatus(t *testing.T, h *Handler, cond func(messaging.WorkerStatusSnapshot) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if cond(h.Status(context.Background())) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for handler status condition")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
