package run

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/internal/worker/agentrun"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/worker/command/run/inbox"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

// fakeDelivery implements eventbus.ManualDelivery for testing Ack/Term/Nak sequences.
type fakeDelivery struct {
	mu              sync.Mutex
	meta            *eventbus.Metadata
	ackCalled       bool
	nakCalled       bool
	nakDelay        time.Duration
	termCalled      bool
	inProgressCalls int
}

func newFakeDelivery(seq uint64) *fakeDelivery {
	return &fakeDelivery{meta: &eventbus.Metadata{Stream: seq}}
}

func (d *fakeDelivery) Metadata() (*eventbus.Metadata, error) { return d.meta, nil }
func (d *fakeDelivery) Ack() error {
	d.mu.Lock()
	d.ackCalled = true
	d.mu.Unlock()
	return nil
}
func (d *fakeDelivery) Nak() error {
	d.mu.Lock()
	d.nakCalled = true
	d.mu.Unlock()
	return nil
}
func (d *fakeDelivery) NakWithDelay(delay time.Duration) error {
	d.mu.Lock()
	d.nakDelay = delay
	d.nakCalled = true
	d.mu.Unlock()
	return nil
}
func (d *fakeDelivery) Term() error {
	d.mu.Lock()
	d.termCalled = true
	d.mu.Unlock()
	return nil
}
func (d *fakeDelivery) InProgress() error {
	d.mu.Lock()
	d.inProgressCalls++
	d.mu.Unlock()
	return nil
}

func (d *fakeDelivery) snapshot() (ack, nak, term bool, inProgress int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ackCalled, d.nakCalled, d.termCalled, d.inProgressCalls
}

type handlerPublisher struct {
	mu     sync.Mutex
	events []messaging.RunEvent
}

func (p *handlerPublisher) Publish(_ context.Context, _ string, value any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if event, ok := value.(messaging.RunEvent); ok {
		p.events = append(p.events, event)
	}
	return nil
}
func (*handlerPublisher) Request(context.Context, string, any) (*nats.Msg, error) { return nil, nil }

type handlerPreparer struct{}

func (handlerPreparer) Prepare(_ context.Context, req *agentrundomain.RunRequest) (*agentrun.PreparedRun, error) {
	return &agentrun.PreparedRun{Request: req, Execution: agent.ExecutionRequest{ExecutionID: req.RunID, TraceID: req.TraceID, Runtime: "test"}}, nil
}

type handlerFinalizer struct{}

func (handlerFinalizer) FinalizeRequired(_ context.Context, run *agentrun.PreparedRun, runtimeResult *agent.ExecutionResult, _ agentrun.JournalSnapshot) (*agentrun.Finalization, error) {
	return &agentrun.Finalization{Result: &agentrundomain.RunResult{RunID: run.Request.RunID, TraceID: run.Request.TraceID, Status: agentrundomain.RunStatusCompleted, Message: runtimeResult.Message}}, nil
}
func (handlerFinalizer) PostRunBestEffort(context.Context, *agentrun.PreparedRun, *agentrundomain.RunResult, agentrun.JournalSnapshot) {
}

type handlerRuntime struct {
	started chan struct{}
	release chan struct{}
	err     error
}

func (*handlerRuntime) Name() string { return "test" }
func (r *handlerRuntime) Execute(_ context.Context, _ agent.ExecutionRequest, _ agent.NodeObserver) (agent.ExecutionResult, error) {
	close(r.started)
	<-r.release
	if r.err != nil {
		return agent.ExecutionResult{}, r.err
	}
	return agent.ExecutionResult{Message: "done"}, nil
}

// mockInbox implements inbox.RunInbox for tests.
type mockInbox struct {
	mu      sync.Mutex
	records map[string]*inboxRecord
}
type inboxRecord struct {
	status  inbox.Status
	errMsg  string
	command messaging.WorkerCommand
}

func newMockInbox() *mockInbox { return &mockInbox{records: make(map[string]*inboxRecord)} }

func (m *mockInbox) PutIfAbsent(_ context.Context, topic string, streamSeq uint64, cmd messaging.WorkerCommand) (bool, *inbox.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := keyOf(topic, streamSeq)
	if r, ok := m.records[key]; ok {
		return false, &inbox.Record{Topic: topic, StreamSeq: streamSeq, Status: r.status}, nil
	}
	m.records[key] = &inboxRecord{status: inbox.StatusPending, command: cmd}
	return true, &inbox.Record{Topic: topic, StreamSeq: streamSeq, Status: inbox.StatusPending}, nil
}
func (m *mockInbox) MarkProcessing(_ context.Context, topic string, seq uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.records[keyOf(topic, seq)]; ok {
		r.status = inbox.StatusProcessing
	}
	return nil
}
func (m *mockInbox) MarkCompleted(_ context.Context, topic string, seq uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.records[keyOf(topic, seq)]; ok {
		r.status = inbox.StatusCompleted
	}
	return nil
}
func (m *mockInbox) MarkFailed(_ context.Context, topic string, seq uint64, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.records[keyOf(topic, seq)]; ok {
		r.status = inbox.StatusFailed
		r.errMsg = errMsg
	}
	return nil
}
func (m *mockInbox) GetNonTerminal(_ context.Context, topic string) ([]inbox.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var recs []inbox.Record
	for k, r := range m.records {
		if r.status != inbox.StatusCompleted && r.status != inbox.StatusFailed {
			// Only return records matching this topic.
			// Key format is "topic:seq" — extract the seq.
			if len(k) > len(topic)+1 && k[:len(topic)] == topic && k[len(topic)] == ':' {
				seq := parseSeq(k[len(topic)+1:])
				recs = append(recs, inbox.Record{Topic: topic, StreamSeq: seq, Status: r.status, Command: cmdJSON(r.command)})
			}
		}
	}
	return recs, nil
}
func (m *mockInbox) DeleteTerminalBefore(_ context.Context, _ string, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockInbox) Close() error { return nil }

func (m *mockInbox) status(key string) inbox.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.records[key]; ok {
		return r.status
	}
	return ""
}

func keyOf(topic string, seq uint64) string { return topic + ":" + itoa(seq) }
func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
func parseSeq(s string) uint64 {
	var n uint64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + uint64(c-'0')
	}
	return n
}
func cmdJSON(cmd messaging.WorkerCommand) string {
	b, _ := json.Marshal(cmd)
	return string(b)
}

func standardCommand() messaging.WorkerCommand {
	return messaging.NewRunCommand("msg-1",
		messaging.RouteContext{OrgID: 1, WorkerID: 2, SessionID: "session-1"},
		messaging.TraceContext{TraceID: "trace-1", TaskID: "task-1", RunID: "run-1"},
		messaging.RunCommandPayload{
			TaskType:  messaging.TaskTypeAgentRun,
			Execution: messaging.ExecutionTarget{AssistantID: "assistant-1"},
			Input:     messaging.TaskInput{Type: messaging.InputTypeMessage, Messages: []messaging.ChatMessage{{ID: "user-1", Role: messaging.MessageRoleUser, Content: "hello"}}},
			Model:     messaging.ModelOptions{Provider: "openai", Model: "test", APIKey: "key"},
			Runtime:   messaging.RuntimeOptions{Kind: "test"},
		}, nil)
}

// TestHandlerAsyncDispatch verifies HandleRunCommand returns quickly and Ack is called.
func TestHandlerAsyncDispatch(t *testing.T) {
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{})}
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, err := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, DebounceWindow: 5 * time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer h.Close()

	ib := newMockInbox()
	h.runInbox = ib

	cmd := standardCommand()
	delivery := newFakeDelivery(42)

	done := make(chan error, 1)
	go func() { done <- h.HandleRunCommand(context.Background(), cmd, delivery) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("HandleRunCommand error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HandleRunCommand did not return quickly")
	}

	if ack, _, _, _ := delivery.snapshot(); !ack {
		t.Fatal("Ack was not called")
	}

	<-runtime.started
	close(runtime.release)

	for i := 0; i < 100; i++ {
		if ib.status(h.RunSubject()+":42") == inbox.StatusCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s := ib.status(h.RunSubject() + ":42"); s != inbox.StatusCompleted {
		t.Fatalf("inbox status = %s, want completed", s)
	}
}

// TestHandlerAsyncDedupInflight verifies duplicate delivery while inflight triggers Ack without double-submit.
func TestHandlerAsyncDedupInflight(t *testing.T) {
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{})}
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, _ := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, DebounceWindow: 100 * time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	defer h.Close()
	ib := newMockInbox()
	h.runInbox = ib

	cmd := standardCommand()

	// First delivery.
	d1 := newFakeDelivery(44)
	if err := h.HandleRunCommand(context.Background(), cmd, d1); err != nil {
		t.Fatalf("first HandleRunCommand error = %v", err)
	}
	if ack, _, _, _ := d1.snapshot(); !ack {
		t.Fatal("first Ack not called")
	}

	<-runtime.started
	key := inboxKey(h.RunSubject(), 44)
	h.stateMu.Lock()
	_, owned := h.inflight[key]
	h.stateMu.Unlock()
	if !owned {
		t.Fatal("delivery ownership was released before execution reached a terminal state")
	}

	// Second delivery (same seq) while inflight.
	d2 := newFakeDelivery(44)
	if err := h.HandleRunCommand(context.Background(), cmd, d2); err != nil {
		t.Fatalf("second HandleRunCommand error = %v", err)
	}
	ack, nak, term, _ := d2.snapshot()
	if !ack {
		t.Fatal("second Ack not called")
	}
	if term || nak {
		t.Fatal("should not Term or Nak for inflight duplicate")
	}

	close(runtime.release)

	for i := 0; i < 100; i++ {
		if ib.status(h.RunSubject()+":44") == inbox.StatusCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.stateMu.Lock()
	_, owned = h.inflight[key]
	h.stateMu.Unlock()
	if owned {
		t.Fatal("delivery ownership was not released after completion")
	}
}

// TestHandlerDrain verifies Drain waits properly.
func TestHandlerDrain(t *testing.T) {
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{})}
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, _ := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, DebounceWindow: time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	defer h.Close()
	ib := newMockInbox()
	h.runInbox = ib

	_ = h.HandleRunCommand(context.Background(), standardCommand(), newFakeDelivery(45))

	h.StopAdmission()

	done := make(chan bool, 1)
	go func() { done <- h.Drain(50 * time.Millisecond) }()
	<-runtime.started
	if ok := <-done; ok {
		t.Fatal("Drain should timeout while runtime blocked")
	}

	close(runtime.release)
	if ok := h.Drain(2 * time.Second); !ok {
		t.Fatal("Drain should succeed after runtime completes")
	}
}

// TestHandlerPayloadTerm verifies Term is called for invalid payload.
func TestHandlerPayloadTerm(t *testing.T) {
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(agent.NewRegistry()), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, _ := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, DebounceWindow: time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	defer h.Close()

	// Command with invalid body (no payload).
	cmd := messaging.WorkerCommand{Type: messaging.MessageTypeWorkerCommand, ID: "bad", Body: messaging.WorkerCommandBody{CommandType: messaging.CommandTypeRun, Payload: json.RawMessage("not-json")}}
	d := newFakeDelivery(46)
	h.HandleRunCommand(context.Background(), cmd, d)
	if _, _, term, _ := d.snapshot(); !term {
		t.Fatal("Term should be called for invalid payload")
	}
}

// TestHandlerInvalidRouteTerm verifies Term is called on route mismatch.
func TestHandlerInvalidRouteTerm(t *testing.T) {
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(agent.NewRegistry()), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, _ := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, DebounceWindow: time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	defer h.Close()

	cmd := messaging.NewRunCommand("bad-route", messaging.RouteContext{OrgID: 99, WorkerID: 88, SessionID: "s"},
		messaging.TraceContext{}, messaging.RunCommandPayload{TaskType: messaging.TaskTypeAgentRun, Model: messaging.ModelOptions{Provider: "o", Model: "m", APIKey: "k"}}, nil)
	d := newFakeDelivery(47)
	h.HandleRunCommand(context.Background(), cmd, d)
	if _, _, term, _ := d.snapshot(); !term {
		t.Fatal("Term should be called for route mismatch")
	}
}

// TestHandlerMissingModelTerm verifies Term on missing model config.
func TestHandlerMissingModelTerm(t *testing.T) {
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(agent.NewRegistry()), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, _ := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, DebounceWindow: time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	defer h.Close()

	cmd := messaging.NewRunCommand("no-model", messaging.RouteContext{OrgID: 1, WorkerID: 2, SessionID: "s"},
		messaging.TraceContext{}, messaging.RunCommandPayload{TaskType: messaging.TaskTypeAgentRun}, nil)
	d := newFakeDelivery(48)
	h.HandleRunCommand(context.Background(), cmd, d)
	if _, _, term, _ := d.snapshot(); !term {
		t.Fatal("Term should be called for missing model")
	}
}

// TestHandlerAsyncDispatchFailure verifies inbox marked failed on execution error.
func TestHandlerAsyncDispatchFailure(t *testing.T) {
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{}), err: errors.New("runtime failed")}
	close(runtime.release)
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, _ := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, DebounceWindow: time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	defer h.Close()
	ib := newMockInbox()
	h.runInbox = ib

	d := newFakeDelivery(49)
	if err := h.HandleRunCommand(context.Background(), standardCommand(), d); err != nil {
		t.Fatalf("HandleRunCommand error = %v", err)
	}
	if ack, _, _, _ := d.snapshot(); !ack {
		t.Fatal("Ack should be called even for tasks that will fail")
	}

	for i := 0; i < 100; i++ {
		if ib.status(h.RunSubject()+":49") == inbox.StatusFailed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s := ib.status(h.RunSubject() + ":49"); s != inbox.StatusFailed {
		t.Fatalf("inbox status = %s, want failed", s)
	}
}

// TestHandlerInboxRequired verifies New fails without InboxDBPath.
func TestHandlerInboxRequired(t *testing.T) {
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(agent.NewRegistry()), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	_, err := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, InboxDBPath: ""}, &handlerPublisher{}, svc)
	if err == nil {
		t.Fatal("New should fail without InboxDBPath")
	}
}

// TestHandlerStopAdmissionBlocksNewSubmissions verifies StopAdmission prevents new WaitGroup.Add.
func TestHandlerStopAdmissionBlocksNewSubmissions(t *testing.T) {
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{})}
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, _ := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, DebounceWindow: time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	defer h.Close()
	ib := newMockInbox()
	h.runInbox = ib

	// Submit one message that's running.
	_ = h.HandleRunCommand(context.Background(), standardCommand(), newFakeDelivery(50))
	<-runtime.started

	// Stop admission.
	h.StopAdmission()

	// Next submission should get NakWithDelay because admission is closed.
	cmd := messaging.NewRunCommand("msg-2", messaging.RouteContext{OrgID: 1, WorkerID: 2, SessionID: "session-2"},
		messaging.TraceContext{TraceID: "t2", TaskID: "task-2", RunID: "run-2"},
		messaging.RunCommandPayload{TaskType: messaging.TaskTypeAgentRun, Model: messaging.ModelOptions{Provider: "o", Model: "m", APIKey: "k"}, Runtime: messaging.RuntimeOptions{Kind: "test"}, Input: messaging.TaskInput{Type: messaging.InputTypeMessage, Messages: []messaging.ChatMessage{{ID: "u2", Role: messaging.MessageRoleUser, Content: "hi"}}}, Execution: messaging.ExecutionTarget{AssistantID: "a1"}}, nil)
	d2 := newFakeDelivery(51)
	h.HandleRunCommand(context.Background(), cmd, d2)
	if _, nak, _, _ := d2.snapshot(); !nak {
		t.Fatal("NakWithDelay should be called when admission closed")
	}

	close(runtime.release)

	if ok := h.Drain(2 * time.Second); !ok {
		t.Fatal("Drain should succeed")
	}
}

// TestRecoverNonTerminal verifies recovery from non-terminal inbox records.
func TestRecoverNonTerminal(t *testing.T) {
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{})}
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, _ := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 2, DebounceWindow: time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	defer h.Close()
	ib := newMockInbox()
	h.runInbox = ib

	topic := h.RunSubject()

	// Insert a non-terminal record as if from a previous crash.
	ib.PutIfAbsent(context.Background(), topic, 100, standardCommand())

	// Recover.
	if err := h.RecoverNonTerminal(context.Background()); err != nil {
		t.Fatalf("RecoverNonTerminal error = %v", err)
	}

	// Recovery feeder should have started. Wait for it.
	<-runtime.started
	duplicate := newFakeDelivery(100)
	if err := h.HandleRunCommand(context.Background(), standardCommand(), duplicate); err != nil {
		t.Fatalf("duplicate HandleRunCommand error = %v", err)
	}
	if ack, nak, term, _ := duplicate.snapshot(); !ack || nak || term {
		t.Fatalf("duplicate disposition = ack:%v nak:%v term:%v", ack, nak, term)
	}
	close(runtime.release)

	if ok := h.Drain(2 * time.Second); !ok {
		t.Fatal("Drain should succeed after recovery")
	}

	if s := ib.status(topic + ":100"); s != inbox.StatusCompleted {
		t.Fatalf("recovered record status = %s, want completed", s)
	}
}

func TestHandlerSendsInProgressWhileAdmissionIsFull(t *testing.T) {
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{})}
	close(runtime.release)
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, err := New(Config{
		OrgID:          1,
		WorkerID:       2,
		MaxConcurrency: 1,
		DebounceWindow: time.Millisecond,
		InboxDBPath:    ":memory:",
	}, &handlerPublisher{}, svc)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer h.Close()
	h.runInbox = newMockInbox()

	h.sem <- struct{}{}
	h.sem <- struct{}{}

	delivery := newFakeDelivery(101)
	done := make(chan error, 1)
	go func() {
		done <- h.HandleRunCommand(context.Background(), standardCommand(), delivery)
	}()

	deadline := time.After(time.Second)
	for {
		_, _, _, inProgress := delivery.snapshot()
		if inProgress > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("InProgress was not called while admission was full")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	<-h.sem
	if err := <-done; err != nil {
		t.Fatalf("HandleRunCommand() error = %v", err)
	}
	<-runtime.started
	if !h.Drain(2 * time.Second) {
		t.Fatal("Drain should succeed after the admitted task completes")
	}
	<-h.sem
}
