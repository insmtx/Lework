package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/internal/agent"
	runtimeevents "github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
)

type fakeSeqTracker struct {
	lastTerminal uint64
	terminal     map[uint64]bool
	received     []uint64
	processing   []uint64
	completed    []uint64
	failed       map[uint64]string
}

func (f *fakeSeqTracker) TrackReceived(_ context.Context, _ string, seq uint64, _, _, _, _ string) error {
	f.received = append(f.received, seq)
	return nil
}

func (f *fakeSeqTracker) MarkProcessing(_ context.Context, _ string, seq uint64) error {
	f.processing = append(f.processing, seq)
	return nil
}

func (f *fakeSeqTracker) MarkCompleted(_ context.Context, _ string, seq uint64) error {
	f.completed = append(f.completed, seq)
	return nil
}

func (f *fakeSeqTracker) MarkFailed(_ context.Context, _ string, seq uint64, errMsg string) error {
	if f.failed == nil {
		f.failed = make(map[uint64]string)
	}
	f.failed[seq] = errMsg
	return nil
}

func (f *fakeSeqTracker) GetLastCompletedSeq(context.Context, string) (uint64, error) {
	return 0, nil
}

func (f *fakeSeqTracker) GetLastTerminalSeq(context.Context, string) (uint64, error) {
	return f.lastTerminal, nil
}

func (f *fakeSeqTracker) IsDuplicate(context.Context, string, uint64) (bool, error) {
	return false, nil
}

func (f *fakeSeqTracker) IsTerminal(_ context.Context, _ string, seq uint64) (bool, error) {
	return f.terminal[seq], nil
}

func (f *fakeSeqTracker) Close() error {
	return nil
}

type fakeSubscriber struct {
	subscribeCalled     bool
	subscribeFromCalled bool
	startSeq            int64
}

func (f *fakeSubscriber) Subscribe(context.Context, string, string, func(*nats.Msg)) error {
	f.subscribeCalled = true
	return nil
}

func (f *fakeSubscriber) SubscribeFrom(_ context.Context, _ string, startSeq int64, _ func(*nats.Msg)) error {
	f.subscribeFromCalled = true
	f.startSeq = startSeq
	return nil
}

type fakePublisher struct {
	calls []publishedEvent
}

type publishedEvent struct {
	topic string
	event any
}

func (f *fakePublisher) Publish(_ context.Context, topic string, event any) error {
	f.calls = append(f.calls, publishedEvent{topic: topic, event: event})
	return nil
}

func (f *fakePublisher) Request(context.Context, string, any) (*nats.Msg, error) {
	return nil, nil
}

type fakeRunner struct {
	err    error
	result *agent.RunResult
	emit   *runtimeevents.Event
	calls  int
}

func (f *fakeRunner) Run(ctx context.Context, req *agent.RequestContext) (*agent.RunResult, error) {
	f.calls++
	if f.emit != nil && req.EventSink != nil {
		_ = req.EventSink.Emit(ctx, f.emit)
	}
	if f.err != nil {
		return f.result, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &agent.RunResult{RunID: req.RunID, Status: agent.RunStatusCompleted}, nil
}

func TestConsumerExecuteWithTrackerMarksAllSeqsFailed(t *testing.T) {
	t.Setenv(leros.EnvWorkspaceRoot, t.TempDir())

	runErr := errors.New("skill not found")
	tracker := &fakeSeqTracker{}
	publisher := &fakePublisher{}
	h := &Handler{
		cfg:        Config{OrgID: 1, WorkerID: 2},
		publisher:  publisher,
		runner:     &fakeRunner{err: runErr},
		seqTracker: tracker,
	}
	task := testRunTask()
	task.Route.SessionID = "session_1"
	setSeqs(&task, []uint64{7, 8})

	err := h.executeWithTracker(context.Background(), task)
	if !errors.Is(err, runErr) {
		t.Fatalf("executeWithTracker error = %v, want %v", err, runErr)
	}

	if !sameSeqs(tracker.processing, []uint64{7, 8}) {
		t.Fatalf("processing seqs = %v, want [7 8]", tracker.processing)
	}
	for _, seq := range []uint64{7, 8} {
		if tracker.failed[seq] != runErr.Error() {
			t.Fatalf("failed[%d] = %q, want %q", seq, tracker.failed[seq], runErr.Error())
		}
	}
	if len(tracker.completed) != 0 {
		t.Fatalf("completed seqs = %v, want none", tracker.completed)
	}
	if len(publisher.calls) != 1 {
		t.Fatalf("published events = %d, want one state lane publish", len(publisher.calls))
	}
	if evt, ok := publisher.calls[0].event.(messaging.RunEvent); !ok ||
		evt.Body.Event != messaging.RunEventRunFailed {
		t.Fatalf("first published event = %#v, want run.failed event", publisher.calls[0].event)
	}
}

func TestConsumerExecuteWithTrackerDoesNotEmitRunFailedForCancelledRun(t *testing.T) {
	t.Setenv(leros.EnvWorkspaceRoot, t.TempDir())

	tracker := &fakeSeqTracker{}
	publisher := &fakePublisher{}
	cancelledEvent := runtimeevents.NewRunCompleted(runtimeevents.RunCompletedPayload{
		Status: string(agent.RunStatusCancelled),
		Result: runtimeevents.RunResultPayload{
			Message: "已取消",
		},
	}, "已取消")
	cancelledEvent.Type = runtimeevents.EventCancelled
	h := &Handler{
		cfg:       Config{OrgID: 1, WorkerID: 2},
		publisher: publisher,
		runner: &fakeRunner{
			err: context.Canceled,
			result: &agent.RunResult{
				RunID:  "run_1",
				Status: agent.RunStatusCancelled,
				Error:  context.Canceled.Error(),
			},
			emit: cancelledEvent,
		},
		seqTracker: tracker,
	}
	task := testRunTask()
	task.Route.SessionID = "session_1"
	setSeqs(&task, []uint64{7})

	if err := h.executeWithTracker(context.Background(), task); err != nil {
		t.Fatalf("executeWithTracker error = %v, want nil for cancellation", err)
	}

	if !sameSeqs(tracker.completed, []uint64{7}) {
		t.Fatalf("completed seqs = %v, want [7]", tracker.completed)
	}
	if len(tracker.failed) != 0 {
		t.Fatalf("failed seqs = %#v, want none", tracker.failed)
	}
	if len(publisher.calls) != 1 {
		t.Fatalf("published events = %d, want one state lane publish", len(publisher.calls))
	}
	for _, call := range publisher.calls {
		evt, ok := call.event.(messaging.RunEvent)
		if !ok {
			t.Fatalf("published event = %#v, want RunEvent", call.event)
		}
		if evt.Body.Event == messaging.RunEventRunFailed {
			t.Fatalf("published run.failed for cancellation: %#v", evt)
		}
	}
}

func TestConsumerExecuteWithTrackerEmitsRunFailedWhenPrepareWorkspaceFails(t *testing.T) {
	t.Setenv(leros.EnvWorkspaceRoot, t.TempDir())

	tracker := &fakeSeqTracker{}
	publisher := &fakePublisher{}
	h := &Handler{
		cfg:        Config{OrgID: 1, WorkerID: 2},
		publisher:  publisher,
		runner:     &fakeRunner{},
		seqTracker: tracker,
	}
	task := testRunTask()
	task.Route.SessionID = "session_1"
	task.Workspace.ProjectID = "project_1"
	// Use invalid work_dir to trigger workspace prepare failure, verifying run.failed is emitted.
	task.Runtime.WorkDir = "../escape"
	setSeqs(&task, []uint64{9})

	err := h.executeWithTracker(context.Background(), task)
	if err == nil {
		t.Fatal("executeWithTracker error = nil, want workspace prepare error")
	}

	if tracker.failed[9] == "" {
		t.Fatalf("failed seq 9 should be recorded, got %q", tracker.failed[9])
	}
	if len(publisher.calls) != 1 {
		t.Fatalf("published events = %d, want one state lane publish", len(publisher.calls))
	}
	if evt, ok := publisher.calls[0].event.(messaging.RunEvent); !ok ||
		evt.Body.Event != messaging.RunEventRunFailed {
		t.Fatalf("first published event = %#v, want run.failed event", publisher.calls[0].event)
	}
}

func testRunTask() runTask {
	return runTask{
		ID:        "msg_1",
		CreatedAt: time.Now().UTC(),
		Trace: messaging.TraceContext{
			TraceID: "trace_1",
			TaskID:  "task_1",
			RunID:   "run_1",
		},
		Route: messaging.RouteContext{
			OrgID:    1,
			WorkerID: 2,
		},
		TaskType: messaging.TaskTypeAgentRun,
		Input: messaging.TaskInput{
			Type: messaging.InputTypeMessage,
			Messages: []messaging.ChatMessage{
				{Role: messaging.MessageRoleUser, Content: "hello"},
			},
		},
		Model: messaging.ModelOptions{
			Provider: "openai",
			Model:    "gpt-4.1",
			APIKey:   "test-key",
		},
	}
}

func sameSeqs(got []uint64, want []uint64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
