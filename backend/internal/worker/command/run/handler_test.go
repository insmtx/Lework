package run

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/ygpkg/yg-go/logs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/insmtx/Leros/backend/agent"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/worker/agentrun"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
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

func TestWithRunLogFieldsPreservesRequestIDForAsyncExecution(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)
	ctx := logs.WithContextLogger(context.Background(), zap.New(core).Sugar())
	ctx = withRunLogFields(ctx, runTask{
		Trace: messaging.TraceContext{ReqID: "req-async-1", TraceID: "trace-async-1", RunID: "run-1"},
		Route: messaging.RouteContext{SessionID: "session-1"},
	})

	logs.InfoContext(ctx, "async run")
	entries := observed.All()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["req_id"] != "req-async-1" {
		t.Fatalf("req_id = %#v, want req-async-1", fields["req_id"])
	}
	if _, ok := fields["trace_id"]; ok {
		t.Fatalf("trace_id should not be added to async run logs: %#v", fields["trace_id"])
	}
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

func (handlerPreparer) Prepare(_ context.Context, req *agentrundomain.RunRequest) (*agentrun.PreparedRun, func(), error) {
	return &agentrun.PreparedRun{Request: req, Execution: agent.ExecutionRequest{ExecutionID: req.RunID, TraceID: req.TraceID, Runtime: "test"}}, func() {}, nil
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
	nextID  uint64
	// nilInsertSeq 若非零，则 PutIfAbsent 对该 seq 返回 (false, nil, nil)，
	// 用于复现 command_id 冲突但 (topic, stream_seq) 无记录的边界 panic。
	nilInsertSeq uint64
}
type inboxRecord struct {
	id        uint64
	topic     string
	streamSeq uint64
	status    inbox.Status
	errMsg    string
	command   messaging.WorkerCommand
	createdAt int64
	updatedAt int64
}

func newMockInbox() *mockInbox { return &mockInbox{records: make(map[string]*inboxRecord)} }

func (m *mockInbox) PutIfAbsent(_ context.Context, topic string, streamSeq uint64, cmd messaging.WorkerCommand) (bool, *inbox.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := keyOf(topic, streamSeq)
	now := time.Now().Unix()
	if r, ok := m.records[key]; ok {
		return false, &inbox.Record{ID: r.id, Topic: topic, StreamSeq: streamSeq, CommandID: r.command.ID, Status: r.status, CreatedAt: r.createdAt, UpdatedAt: r.updatedAt}, nil
	}
	// nilInsertSeq 模拟「插入被唯一索引拒绝但 (topic, stream_seq) 无记录」的边界场景，
	// 即真实 SQLite 中按 command_id 冲突时 PutIfAbsent 会返回 (false, nil, nil)。
	if streamSeq == m.nilInsertSeq {
		return false, nil, nil
	}
	m.nextID++
	record := &inboxRecord{id: m.nextID, topic: topic, streamSeq: streamSeq, status: inbox.StatusPending, command: cmd, createdAt: now, updatedAt: now}
	m.records[key] = record
	return true, &inbox.Record{ID: record.id, Topic: topic, StreamSeq: streamSeq, CommandID: cmd.ID, Status: inbox.StatusPending, CreatedAt: now, UpdatedAt: now}, nil
}
func (m *mockInbox) MarkProcessing(_ context.Context, recordID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.byID(recordID); r != nil {
		r.status = inbox.StatusProcessing
		r.updatedAt = time.Now().Unix()
	}
	return nil
}
func (m *mockInbox) MarkCompleted(_ context.Context, recordID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.byID(recordID); r != nil {
		r.status = inbox.StatusCompleted
		r.updatedAt = time.Now().Unix()
	}
	return nil
}
func (m *mockInbox) MarkFailed(_ context.Context, recordID uint64, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.byID(recordID); r != nil {
		r.status = inbox.StatusFailed
		r.errMsg = errMsg
		r.updatedAt = time.Now().Unix()
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
				recs = append(recs, inbox.Record{ID: r.id, Topic: topic, StreamSeq: seq, CommandID: r.command.ID, Status: r.status, Command: cmdJSON(r.command), CreatedAt: r.createdAt, UpdatedAt: r.updatedAt})
			}
		}
	}
	return recs, nil
}
func (m *mockInbox) ResetProcessing(_ context.Context, topic string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, record := range m.records {
		if record.status == inbox.StatusProcessing && len(key) > len(topic)+1 && key[:len(topic)] == topic && key[len(topic)] == ':' {
			record.status = inbox.StatusPending
		}
	}
	return nil
}
func (m *mockInbox) DeleteTerminalBefore(_ context.Context, _ string, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockInbox) Close() error { return nil }

func (m *mockInbox) byID(recordID uint64) *inboxRecord {
	for _, record := range m.records {
		if record.id == recordID {
			return record
		}
	}
	return nil
}

func (m *mockInbox) status(key string) inbox.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.records[key]; ok {
		return r.status
	}
	return ""
}

func (m *mockInbox) CountByStatus(_ context.Context, topic string, status inbox.Status) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for k, r := range m.records {
		if r.status != status {
			continue
		}
		if len(k) > len(topic)+1 && k[:len(topic)] == topic && k[len(topic)] == ':' {
			count++
		}
	}
	return count, nil
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
			Execution: messaging.ExecutionTarget{AssistantPublicID: "assistant-1"},
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
	key := inboxKey(1)
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

func TestHandlerAcceptsNewCommandWhenStreamSequenceWasReused(t *testing.T) {
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{})}
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, err := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, DebounceWindow: time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer h.Close()

	old := standardCommand()
	old.ID = "msg-old"
	_, oldRecord, err := h.RunInbox().PutIfAbsent(context.Background(), h.RunSubject(), 82, old)
	if err != nil {
		t.Fatalf("insert old inbox record: %v", err)
	}
	if err := h.RunInbox().MarkCompleted(context.Background(), oldRecord.ID); err != nil {
		t.Fatalf("complete old inbox record: %v", err)
	}

	current := standardCommand()
	current.ID = "msg-new"
	delivery := newFakeDelivery(82)
	if err := h.HandleRunCommand(context.Background(), current, delivery); err != nil {
		t.Fatalf("HandleRunCommand() error = %v", err)
	}
	if ack, nak, term, _ := delivery.snapshot(); !ack || nak || term {
		t.Fatalf("delivery disposition = ack:%v nak:%v term:%v, want Ack only", ack, nak, term)
	}

	select {
	case <-runtime.started:
	case <-time.After(time.Second):
		t.Fatal("new command was not dispatched after stream sequence reuse")
	}
	close(runtime.release)
	if ok := h.Drain(2 * time.Second); !ok {
		t.Fatal("Drain() timed out")
	}
}

// TestHandlerPutIfAbsentNilRecord verifies that an inconsistent storage result
// does not panic or execute an unknown command; the delivery remains retryable.
func TestHandlerPutIfAbsentNilRecord(t *testing.T) {
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{})}
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, _ := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, DebounceWindow: 5 * time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	defer h.Close()
	ib := newMockInbox()
	ib.nilInsertSeq = 200
	h.runInbox = ib

	delivery := newFakeDelivery(200)
	if err := h.HandleRunCommand(context.Background(), standardCommand(), delivery); err != nil {
		t.Fatalf("HandleRunCommand error = %v", err)
	}
	if ack, nak, term, _ := delivery.snapshot(); ack || !nak || term {
		t.Fatalf("disposition = ack:%v nak:%v term:%v, want Nak only", ack, nak, term)
	}
	select {
	case <-runtime.started:
		t.Fatal("inconsistent inbox result must not execute the command")
	default:
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
		messaging.RunCommandPayload{TaskType: messaging.TaskTypeAgentRun, Model: messaging.ModelOptions{Provider: "o", Model: "m", APIKey: "k"}, Runtime: messaging.RuntimeOptions{Kind: "test"}, Input: messaging.TaskInput{Type: messaging.InputTypeMessage, Messages: []messaging.ChatMessage{{ID: "u2", Role: messaging.MessageRoleUser, Content: "hi"}}}, Execution: messaging.ExecutionTarget{AssistantPublicID: "a1"}}, nil)
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

func TestHandlerAcknowledgesWhileAdmissionIsFull(t *testing.T) {
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
		// 执行槽满不应阻塞 NATS 回调；消息应先持久化并确认。
		MaxInflight:    2,
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

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("HandleRunCommand() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HandleRunCommand blocked on a full execution admission semaphore")
	}
	ack, _, _, _ := delivery.snapshot()
	if !ack {
		t.Fatal("run command was not Acked after durable inbox admission")
	}

	<-h.sem // release one background execution slot
	if err := h.execCtx.Err(); err != nil {
		t.Fatalf("HandleRunCommand() error = %v", err)
	}
	<-runtime.started
	if !h.Drain(2 * time.Second) {
		t.Fatal("Drain should succeed after the admitted task completes")
	}
	<-h.sem
}

// TestHandlerMissingSessionTerm verifies live run missing RouteSessionID is Term'd,
// not written to inbox, not executing.
func TestHandlerMissingSessionTerm(t *testing.T) {
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{})}
	close(runtime.release)
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, _ := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, DebounceWindow: time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	defer h.Close()
	ib := newMockInbox()
	h.runInbox = ib

	cmd := messaging.NewRunCommand("no-session", messaging.RouteContext{OrgID: 1, WorkerID: 2}, // 无 SessionID
		messaging.TraceContext{}, messaging.RunCommandPayload{TaskType: messaging.TaskTypeAgentRun, Model: messaging.ModelOptions{Provider: "o", Model: "m", APIKey: "k"}}, nil)
	d := newFakeDelivery(61)
	if err := h.HandleRunCommand(context.Background(), cmd, d); err == nil {
		t.Fatal("HandleRunCommand() should return error for missing session")
	}
	if _, _, term, _ := d.snapshot(); !term {
		t.Fatal("Term should be called for missing session")
	}
	ib.mu.Lock()
	recordCount := len(ib.records)
	ib.mu.Unlock()
	if recordCount != 0 {
		t.Fatalf("inbox should have no records for missing-session run, got %d", recordCount)
	}
}

// TestHandlerRouteOrgWorkerZeroFilled verifies OrgID/WorkerID==0 are filled from config.
func TestHandlerRouteOrgWorkerZeroFilled(t *testing.T) {
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{})}
	close(runtime.release)
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, _ := New(Config{OrgID: 7, WorkerID: 9, MaxConcurrency: 1, DebounceWindow: time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	defer h.Close()
	ib := newMockInbox()
	h.runInbox = ib

	cmd := messaging.NewRunCommand("zero-route", messaging.RouteContext{OrgID: 0, WorkerID: 0, SessionID: "s"},
		messaging.TraceContext{}, messaging.RunCommandPayload{TaskType: messaging.TaskTypeAgentRun, Model: messaging.ModelOptions{Provider: "o", Model: "m", APIKey: "k"}}, nil)
	d := newFakeDelivery(62)
	if err := h.HandleRunCommand(context.Background(), cmd, d); err != nil {
		t.Fatalf("HandleRunCommand() error = %v", err)
	}
	if ack, _, _, _ := d.snapshot(); !ack {
		t.Fatal("valid run with default-filled route should Ack")
	}
	ib.mu.Lock()
	recordCount := len(ib.records)
	ib.mu.Unlock()
	if recordCount == 0 {
		t.Fatal("valid run should be written to inbox")
	}
}

// TestHandlerRecoveryMissingSessionMarkFailed verifies recovery record missing session is MarkFailed,
// not resubmitted to Coordinator / not executed.
func TestHandlerRecoveryMissingSessionMarkFailed(t *testing.T) {
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{})}
	close(runtime.release)
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, _ := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 2, DebounceWindow: time.Millisecond, InboxDBPath: ":memory:"}, &handlerPublisher{}, svc)
	defer h.Close()
	ib := newMockInbox()
	h.runInbox = ib

	topic := h.RunSubject()
	// 插入一条缺失 session 的记录（崩溃恢复）。
	cmd := messaging.NewRunCommand("recover-no-session", messaging.RouteContext{OrgID: 1, WorkerID: 2}, // 无 SessionID
		messaging.TraceContext{}, messaging.RunCommandPayload{TaskType: messaging.TaskTypeAgentRun, Model: messaging.ModelOptions{Provider: "o", Model: "m", APIKey: "k"}}, nil)
	ib.PutIfAbsent(context.Background(), topic, 200, cmd)

	if err := h.RecoverNonTerminal(context.Background()); err != nil {
		t.Fatalf("RecoverNonTerminal error = %v", err)
	}

	// 等待 record 被标记为 Failed（缺失 session 的恢复不进入 Runtime）。
	deadline := time.After(3 * time.Second)
	for {
		if s := ib.status(topic + ":200"); s == inbox.StatusFailed {
			break
		}
		select {
		case <-deadline:
			t.Fatal("recovery missing-session record was not marked Failed")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	// Runtime 不应被启动。
	select {
	case <-runtime.started:
		t.Fatal("Runtime should not start for missing-session recovered record")
	default:
	}
	if !h.Drain(2 * time.Second) {
		t.Fatal("Drain should succeed")
	}
}
