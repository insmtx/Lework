package push

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

type fakeDevices struct {
	devices []store.Device
	err     error
}

func (f fakeDevices) ListDevices(context.Context) ([]store.Device, error) { return f.devices, f.err }

type fakeSender struct {
	mu      sync.Mutex
	batches [][]Message
	err     error
}

func (f *fakeSender) Send(_ context.Context, msgs []Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches = append(f.batches, msgs)
	return f.err
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.batches)
}

func (f *fakeSender) firstBatch() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.batches) == 0 {
		return nil
	}
	return f.batches[0]
}

func permissionAsked(node, session, permission string) event.Event {
	data, _ := json.Marshal(map[string]string{
		"id":      permission,
		"action":  "bash",
		"message": "run tests",
	})
	return event.Event{NodeID: node, SessionID: session, Type: "permission.asked", Data: data, Time: time.Now()}
}

func TestNotifierSendsOneMessagePerDevice(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{
		{ID: "d1", PushToken: "tok-a", Platform: "android"},
		{ID: "d2", PushToken: "tok-b", Platform: "android"},
	}}, sender, func(string) string { return "dev-box" })

	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_1"))

	if sender.count() != 1 || len(sender.firstBatch()) != 2 {
		t.Fatalf("batches = %+v, want one batch of two", sender.batches)
	}
	msg := sender.firstBatch()[0]
	if msg.To != "tok-a" || msg.Tag != "ses_1" || msg.ChannelID != "permissions" || msg.Priority != "high" {
		t.Fatalf("message = %+v", msg)
	}
	if msg.Data["node"] != "node_1" || msg.Data["session"] != "ses_1" || msg.Data["permission_id"] != "perm_1" {
		t.Fatalf("data = %+v", msg.Data)
	}
	if !strings.Contains(msg.Title, "dev-box") {
		t.Fatalf("title = %q, want node label", msg.Title)
	}
	if !strings.Contains(msg.Body, "bash") {
		t.Fatalf("body = %q, want the permission action", msg.Body)
	}
}

func TestNotifierPrefersSessionTitle(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "dev-box" },
		WithSessionTitle(func(nodeID, sessionID string) string {
			if nodeID != "node_1" || sessionID != "ses_1" {
				t.Fatalf("sessionTitle(%q, %q)", nodeID, sessionID)
			}
			return "审查移动端 P1"
		}))

	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_1"))

	batch := sender.firstBatch()
	if len(batch) != 1 {
		t.Fatalf("batch = %+v, want one message", batch)
	}
	if !strings.Contains(batch[0].Title, "审查移动端 P1") {
		t.Fatalf("title = %q, want the session title", batch[0].Title)
	}
}

func TestNotifierFallsBackToNodeLabelWithoutSessionTitle(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "dev-box" },
		WithSessionTitle(func(string, string) string { return "  " }))

	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_1"))

	batch := sender.firstBatch()
	if len(batch) != 1 || !strings.Contains(batch[0].Title, "dev-box") {
		t.Fatalf("batch = %+v, want the node label in the title", batch)
	}
}

func TestNotifierIgnoresOtherEvents(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	n.Handle(context.Background(), event.Event{
		NodeID: "node_1", SessionID: "ses_1", Type: "message.part.updated",
	})

	if sender.count() != 0 {
		t.Fatalf("sent %d batches, want 0", sender.count())
	}
}

func TestNotifierDeduplicatesSamePermission(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	ev := permissionAsked("node_1", "ses_1", "perm_1")
	n.Handle(context.Background(), ev)
	n.Handle(context.Background(), ev)

	if sender.count() != 1 {
		t.Fatalf("sent %d batches, want 1", sender.count())
	}
}

func TestNotifierSurvivesSendAndLookupFailure(t *testing.T) {
	sender := &fakeSender{err: errors.New("expo down")}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_1"))
	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_2"))

	if sender.count() != 2 {
		t.Fatalf("sent %d batches, want 2 (failures must not stop later events)", sender.count())
	}

	broken := NewNotifier(fakeDevices{err: errors.New("db down")}, sender, func(string) string { return "n" })
	broken.Handle(context.Background(), permissionAsked("node_1", "ses_2", "perm_3"))
}

func TestNotifierSkipsPermissionWithoutID(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	n.Handle(context.Background(), event.Event{
		NodeID: "node_1", SessionID: "ses_1", Type: "permission.asked", Data: json.RawMessage(`{"action":"bash"}`),
	})

	if sender.count() != 0 {
		t.Fatalf("sent %d batches, want 0", sender.count())
	}
}

type blockingSender struct {
	release chan struct{}
}

func (b *blockingSender) Send(context.Context, []Message) error {
	<-b.release
	return nil
}

func TestRunExitsOnCancelWhileSending(t *testing.T) {
	sender := &blockingSender{release: make(chan struct{})}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	ctx, cancel := context.WithCancel(context.Background())
	hub := event.NewHub()
	done := make(chan struct{})
	go func() {
		n.Run(ctx, hub)
		close(done)
	}()

	for i := 0; i < 20; i++ {
		hub.Publish(permissionAsked("node_1", "ses_1", strconv.Itoa(i)))
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after cancellation while a send was blocked")
	}
	close(sender.release)
}

// scriptedSubscriber 第一次订阅立刻关闭通道，模拟 Hub 淘汰慢消费者；
// 之后每次订阅投递一条审批事件再关闭，用来证明 Run 会重新订阅。
type scriptedSubscriber struct {
	mu    sync.Mutex
	calls int
}

func (s *scriptedSubscriber) Subscribe(int) (<-chan event.Event, func()) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()

	ch := make(chan event.Event, 8)
	if n > 1 {
		ch <- permissionAsked("node_1", "ses_1", "perm_1")
	}
	close(ch)
	return ch, func() {}
}

func (s *scriptedSubscriber) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestRunResubscribesAfterEviction(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := &scriptedSubscriber{}
	done := make(chan struct{})
	go func() {
		n.Run(ctx, sub)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for sender.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sender.count() == 0 {
		t.Fatal("no push after resubscribe: Run gave up when the subscription was closed")
	}
	if sub.count() < 2 {
		t.Fatalf("Subscribe calls = %d, want at least 2", sub.count())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after cancellation")
	}
}
