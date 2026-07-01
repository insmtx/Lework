package command

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
)

// mockSubscriber records subscription calls, distinguishing auto vs manual.
type mockSubscriber struct {
	mu           sync.Mutex
	autoTopics   []string
	manualTopics []string
	returnErr    error
	unblock      chan struct{}
	started      chan struct{}
}

func newMockSubscriber() *mockSubscriber {
	return &mockSubscriber{started: make(chan struct{}, 4)}
}

func (m *mockSubscriber) Subscribe(ctx context.Context, topic string, _ string, _ func(msg *nats.Msg)) error {
	m.mu.Lock()
	m.autoTopics = append(m.autoTopics, topic)
	m.mu.Unlock()
	m.started <- struct{}{}
	if m.returnErr != nil {
		return m.returnErr
	}
	if m.unblock != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.unblock:
			return nil
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockSubscriber) SubscribeManualDurable(ctx context.Context, topic string, _ string, _ func(msg *nats.Msg)) error {
	m.mu.Lock()
	m.manualTopics = append(m.manualTopics, topic)
	m.mu.Unlock()
	m.started <- struct{}{}
	if m.returnErr != nil {
		return m.returnErr
	}
	if m.unblock != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.unblock:
			return nil
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockSubscriber) autoCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.autoTopics)
}

func (m *mockSubscriber) manualCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.manualTopics)
}

// stubRunHandler implements RunHandler with ManualDelivery.
type stubRunHandler struct{ called bool }

func (s *stubRunHandler) HandleRunCommand(_ context.Context, _ messaging.WorkerCommand, _ eventbus.ManualDelivery) error {
	s.called = true
	return nil
}

type dispatcherDelivery struct {
	termCalled bool
}

func (*dispatcherDelivery) Metadata() (*eventbus.Metadata, error) { return nil, nil }
func (*dispatcherDelivery) Ack() error                            { return nil }
func (*dispatcherDelivery) Nak() error                            { return nil }
func (*dispatcherDelivery) NakWithDelay(time.Duration) error      { return nil }
func (d *dispatcherDelivery) Term() error {
	d.termCalled = true
	return nil
}
func (*dispatcherDelivery) InProgress() error { return nil }

type stubControlHandler struct {
	calledWith *messaging.WorkerCommand
}

func (s *stubControlHandler) HandleControlCommand(_ context.Context, cmd messaging.WorkerCommand) error {
	s.calledWith = &cmd
	return nil
}

type stubInteractionHandler struct {
	called bool
}

func (s *stubInteractionHandler) HandleInteractionCommand(_ context.Context, _ messaging.WorkerCommand) error {
	s.called = true
	return nil
}

type stubSkillHandler struct {
	called bool
}

func (s *stubSkillHandler) HandleSkillCommand(_ context.Context, _ messaging.WorkerCommand, _ *nats.Msg) error {
	s.called = true
	return nil
}

func TestNewRequiresAllHandlers(t *testing.T) {
	sub := newMockSubscriber()
	cfg := Config{OrgID: 1, WorkerID: 2}
	handlers := Handlers{Run: &stubRunHandler{}, Control: &stubControlHandler{}, Interaction: &stubInteractionHandler{}, Skill: &stubSkillHandler{}}
	d, err := New(cfg, sub, handlers)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil dispatcher")
	}
}

func TestDispatcherRunSubscribesFourLanes(t *testing.T) {
	sub := newMockSubscriber()
	sub.unblock = make(chan struct{})
	handlers := Handlers{Run: &stubRunHandler{}, Control: &stubControlHandler{}, Interaction: &stubInteractionHandler{}, Skill: &stubSkillHandler{}}
	d, err := New(Config{OrgID: 1, WorkerID: 2}, sub, handlers)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	for range 4 {
		select {
		case <-sub.started:
		case <-time.After(2 * time.Second):
			t.Fatal("not all lane subscriptions started")
		}
	}
	close(sub.unblock)
	select {
	case e := <-errCh:
		if e != nil {
			t.Fatalf("Run() error = %v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return")
	}
	if sub.manualCount() != 1 {
		t.Fatalf("expected 1 manual subscription (run lane), got %d", sub.manualCount())
	}
	if sub.autoCount() != 3 {
		t.Fatalf("expected 3 auto subscriptions (control, interaction, skill), got %d", sub.autoCount())
	}
}

func TestDispatcherRunPropagatesSubscribeError(t *testing.T) {
	sub := newMockSubscriber()
	sub.returnErr = fmt.Errorf("subscribe failed")
	handlers := Handlers{Run: &stubRunHandler{}, Control: &stubControlHandler{}, Interaction: &stubInteractionHandler{}, Skill: &stubSkillHandler{}}
	d, _ := New(Config{OrgID: 1, WorkerID: 2}, sub, handlers)
	err := d.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDispatcherRunReturnsNilOnContextCancel(t *testing.T) {
	sub := newMockSubscriber()
	handlers := Handlers{Run: &stubRunHandler{}, Control: &stubControlHandler{}, Interaction: &stubInteractionHandler{}, Skill: &stubSkillHandler{}}
	d, _ := New(Config{OrgID: 1, WorkerID: 2}, sub, handlers)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	for range 4 {
		select {
		case <-sub.started:
		case <-time.After(2 * time.Second):
			t.Fatal("not all lane subscriptions started")
		}
	}
	cancel()
	select {
	case e := <-errCh:
		if e != nil {
			t.Fatalf("Run() error = %v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after cancel")
	}
}

func TestDispatcherParseCommand(t *testing.T) {
	d, _ := New(Config{OrgID: 1, WorkerID: 2}, newMockSubscriber(), Handlers{Run: &stubRunHandler{}, Control: &stubControlHandler{}, Interaction: &stubInteractionHandler{}, Skill: &stubSkillHandler{}})

	valid := &messaging.WorkerCommand{Type: messaging.MessageTypeWorkerCommand, ID: "test"}
	data, _ := json.Marshal(valid)
	cmd, err := d.parseCommand(data)
	if err != nil {
		t.Fatalf("parseCommand() error = %v", err)
	}
	if cmd.ID != "test" {
		t.Fatalf("expected ID 'test', got %q", cmd.ID)
	}

	_, err = d.parseCommand([]byte("not-json"))
	if err == nil {
		t.Fatal("expected error for non-JSON")
	}
}

func TestDispatcherHandleRunCallsTermOnParseFailure(t *testing.T) {
	sub := newMockSubscriber()
	handlers := Handlers{Run: &stubRunHandler{}, Control: &stubControlHandler{}, Interaction: &stubInteractionHandler{}, Skill: &stubSkillHandler{}}
	d, _ := New(Config{OrgID: 1, WorkerID: 2}, sub, handlers)

	delivery := &dispatcherDelivery{}
	d.handleRunDelivery(context.Background(), []byte("not-json"), delivery)
	if !delivery.termCalled {
		t.Fatal("Term should be called when the run command envelope is invalid")
	}
}
