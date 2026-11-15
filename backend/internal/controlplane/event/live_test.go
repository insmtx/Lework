package event

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// fakeCoreSubscriber 捕获一次 Core NATS 订阅，阻塞到 ctx 取消，语义与生产
// 实现一致；测试可直接把负载交给 handler，不需要真实 NATS。
type fakeCoreSubscriber struct {
	subscribed chan struct{}

	mu      sync.Mutex
	subject string
	queue   string
	handler func(context.Context, []byte) []byte
}

func newFakeCoreSubscriber() *fakeCoreSubscriber {
	return &fakeCoreSubscriber{subscribed: make(chan struct{}, 1)}
}

func (f *fakeCoreSubscriber) SubscribeCore(
	ctx context.Context,
	subject, queue string,
	handler func(context.Context, []byte) []byte,
) error {
	f.mu.Lock()
	f.subject = subject
	f.queue = queue
	f.handler = handler
	f.mu.Unlock()

	select {
	case f.subscribed <- struct{}{}:
	default:
	}

	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeCoreSubscriber) waitSubscribed(t *testing.T) {
	t.Helper()

	select {
	case <-f.subscribed:
	case <-time.After(waitTimeout):
		t.Fatal("live subscriber did not subscribe")
	}
}

func (f *fakeCoreSubscriber) subscription() (subject, queue string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subject, f.queue
}

func (f *fakeCoreSubscriber) deliver(t *testing.T, payload []byte) []byte {
	t.Helper()

	f.mu.Lock()
	handler := f.handler
	f.mu.Unlock()
	if handler == nil {
		t.Fatal("live subscriber has no handler")
	}
	return handler(context.Background(), payload)
}

func TestLiveSubscriberPublishesToHub(t *testing.T) {
	hub := NewHub()
	events, cancelEvents := hub.Subscribe(1, WithLive())
	defer cancelEvents()

	sub := newFakeCoreSubscriber()
	live := NewLiveSubscriber(hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- live.Run(ctx, sub) }()
	sub.waitSubscribed(t)

	subject, queue := sub.subscription()
	if subject != "cp.live.>" {
		t.Fatalf("subscription subject = %q, want cp.live.>", subject)
	}
	if queue != "" {
		t.Fatalf("subscription queue = %q, want an empty queue group", queue)
	}

	// 负载不带 live 字段：订阅端必须按 subject 语义补上标记。
	payload, err := json.Marshal(Event{NodeID: "n1", SessionID: "ses_1", Type: "session.text.delta"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if reply := sub.deliver(t, payload); reply != nil {
		t.Fatalf("handler reply = %q, want nil", reply)
	}

	got := receive(t, events)
	if !got.Live {
		t.Fatal("live subscriber must mark delivered events as live")
	}
	if got.NodeID != "n1" || got.SessionID != "ses_1" || got.Type != "session.text.delta" {
		t.Fatalf("delivered event = %#v", got)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v, want nil after cancellation", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("Run did not exit after cancellation")
	}
}

func TestLiveSubscriberIgnoresBadPayload(t *testing.T) {
	hub := NewHub()
	events, cancelEvents := hub.Subscribe(1, WithLive())
	defer cancelEvents()

	sub := newFakeCoreSubscriber()
	live := NewLiveSubscriber(hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- live.Run(ctx, sub) }()
	sub.waitSubscribed(t)

	// 坏负载必须被吞掉：一条畸形消息不能让整条订阅退出或让消费组重投。
	if reply := sub.deliver(t, []byte("{")); reply != nil {
		t.Fatalf("handler reply = %q, want nil", reply)
	}
	select {
	case ev := <-events:
		t.Fatalf("hub received %#v from a malformed payload", ev)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v, want nil after cancellation", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("Run did not exit after cancellation")
	}
}
