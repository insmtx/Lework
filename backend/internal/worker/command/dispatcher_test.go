package command

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
)

// mockSubscriber records subscriptions in a concurrency-safe way.
// When returnErr is set, Subscribe returns it.
// It can also block until unblocked for testing subscription lifecycle.
type mockSubscriber struct {
	mu     sync.Mutex
	topics []string
	// returnErr causes Subscribe to return an error.
	returnErr error
	// unblock is closed when Subscribe should return (used for nop-like behavior).
	unblock chan struct{}
}

func newMockSubscriber() *mockSubscriber {
	return &mockSubscriber{}
}

func (m *mockSubscriber) Subscribe(ctx context.Context, topic string, _ string, _ func(msg *nats.Msg)) error {
	m.mu.Lock()
	m.topics = append(m.topics, topic)
	m.mu.Unlock()

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
	// Default: block until ctx is cancelled (like real subscriptions).
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockSubscriber) topicsSnapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.topics))
	copy(out, m.topics)
	return out
}

type stubRunHandler struct{ called bool }

func (s *stubRunHandler) HandleRunCommand(_ context.Context, _ messaging.WorkerCommand, _ *nats.Msg) error {
	s.called = true
	return nil
}

type stubControlHandler struct{ called bool }

func (s *stubControlHandler) HandleControlCommand(_ context.Context, _ messaging.WorkerCommand) error {
	s.called = true
	return nil
}

type stubInteractionHandler struct{ called bool }

func (s *stubInteractionHandler) HandleInteractionCommand(_ context.Context, _ messaging.WorkerCommand) error {
	s.called = true
	return nil
}

type stubSkillHandler struct{ called bool }

func (s *stubSkillHandler) HandleSkillCommand(_ context.Context, _ messaging.WorkerCommand, _ *nats.Msg) error {
	s.called = true
	return nil
}

func allHandlers() Handlers {
	return Handlers{
		Run:         &stubRunHandler{},
		Control:     &stubControlHandler{},
		Interaction: &stubInteractionHandler{},
		Skill:       &stubSkillHandler{},
	}
}

func TestNewDispatcherValidatesOrgID(t *testing.T) {
	_, err := New(Config{OrgID: 0, WorkerID: 1}, newMockSubscriber(), allHandlers())
	if err == nil {
		t.Fatal("expected error for missing org_id")
	}
}

func TestNewDispatcherValidatesWorkerID(t *testing.T) {
	_, err := New(Config{OrgID: 1, WorkerID: 0}, newMockSubscriber(), allHandlers())
	if err == nil {
		t.Fatal("expected error for missing worker_id")
	}
}

func TestNewDispatcherValidatesSubscriber(t *testing.T) {
	_, err := New(Config{OrgID: 1, WorkerID: 1}, nil, allHandlers())
	if err == nil {
		t.Fatal("expected error for missing subscriber")
	}
}

func TestNewDispatcherValidatesAllHandlers(t *testing.T) {
	sub := newMockSubscriber()
	tests := []struct {
		name     string
		handlers Handlers
	}{
		{"missing Run", Handlers{Control: &stubControlHandler{}, Interaction: &stubInteractionHandler{}, Skill: &stubSkillHandler{}}},
		{"missing Control", Handlers{Run: &stubRunHandler{}, Interaction: &stubInteractionHandler{}, Skill: &stubSkillHandler{}}},
		{"missing Interaction", Handlers{Run: &stubRunHandler{}, Control: &stubControlHandler{}, Skill: &stubSkillHandler{}}},
		{"missing Skill", Handlers{Run: &stubRunHandler{}, Control: &stubControlHandler{}, Interaction: &stubInteractionHandler{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(Config{OrgID: 1, WorkerID: 1}, sub, tt.handlers)
			if err == nil {
				t.Fatal("expected error for missing handler")
			}
		})
	}
}

func TestDispatcherRunSubscribesFourLanes(t *testing.T) {
	sub := newMockSubscriber()
	// Use unblock to make Subscribe return immediately after recording.
	sub.unblock = make(chan struct{})

	d, err := New(Config{OrgID: 1, WorkerID: 2}, sub, allHandlers())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	// Wait for subscriptions to be recorded.
	time.Sleep(100 * time.Millisecond)

	// Close unblock so all Subscribe calls return nil.
	close(sub.unblock)

	// Now cancel so Run returns nil.
	cancel()

	if err := <-errCh; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	topics := sub.topicsSnapshot()
	if len(topics) != 4 {
		t.Fatalf("expected 4 subscriptions, got %d: %v", len(topics), topics)
	}

	want := map[string]bool{
		"org.1.worker.2.cmd.run":         true,
		"org.1.worker.2.cmd.control":     true,
		"org.1.worker.2.cmd.interaction": true,
		"org.1.worker.2.cmd.skill":       true,
	}
	for _, topic := range topics {
		if !want[topic] {
			t.Errorf("unexpected topic: %q", topic)
		}
	}
}

func TestDispatcherRunPropagatesSubscribeError(t *testing.T) {
	wantErr := fmt.Errorf("fake subscribe error")
	sub := newMockSubscriber()
	sub.returnErr = wantErr

	d, err := New(Config{OrgID: 1, WorkerID: 2}, sub, allHandlers())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = d.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from Run(), got nil")
	}
}

func TestDispatcherRunReturnsNilOnContextCancel(t *testing.T) {
	// mockSubscriber blocks until ctx.Done by default.
	sub := newMockSubscriber()

	d, err := New(Config{OrgID: 1, WorkerID: 2}, sub, allHandlers())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	err = <-errCh
	if err != nil {
		t.Fatalf("Run() error on cancel = %v, want nil", err)
	}
}

func TestDispatcherParseCommand(t *testing.T) {
	d := &Dispatcher{}

	validCmd := messaging.WorkerCommand{
		Type: messaging.MessageTypeWorkerCommand,
	}
	data, _ := json.Marshal(validCmd)

	cmd, err := d.parseCommand(data)
	if err != nil {
		t.Fatalf("parseCommand error = %v", err)
	}
	if cmd.Type != messaging.MessageTypeWorkerCommand {
		t.Fatalf("expected WorkerCommand type, got %q", cmd.Type)
	}

	// Wrong type
	_, err = d.parseCommand([]byte(`{"type":"wrong"}`))
	if err == nil {
		t.Fatal("expected error for wrong message type")
	}
}
