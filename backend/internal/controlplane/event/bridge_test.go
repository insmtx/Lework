package event

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/pkg/messaging"
)

// sameEvent 比较经过 JSON 往返的事件。
//
// Time 用 Equal 比较：序列化会丢掉单调时钟读数，直接 DeepEqual 会得到
// 与业务无关的假差异。
func sameEvent(a, b Event) bool {
	return a.NodeID == b.NodeID &&
		a.SessionID == b.SessionID &&
		a.Seq == b.Seq &&
		a.Type == b.Type &&
		bytes.Equal(a.Data, b.Data) &&
		a.Time.Equal(b.Time)
}

// fakeSubscriber 模拟 JetStream 字节订阅：记录订阅参数、暴露 handler 供
// 测试直接投递负载，并阻塞到 ctx 取消，语义与真实实现一致。
type fakeSubscriber struct {
	once  sync.Once
	ready chan struct{}

	mu       sync.Mutex
	subject  string
	startSeq int64
	handler  func(context.Context, []byte) error
}

func newFakeSubscriber() *fakeSubscriber {
	return &fakeSubscriber{ready: make(chan struct{})}
}

func (f *fakeSubscriber) SubscribeBytes(
	ctx context.Context,
	subject string,
	startSeq int64,
	handler func(context.Context, []byte) error,
) error {
	f.mu.Lock()
	f.subject = subject
	f.startSeq = startSeq
	f.handler = handler
	f.mu.Unlock()

	f.once.Do(func() { close(f.ready) })
	<-ctx.Done()
	return ctx.Err()
}

// waitReady 等待 Bridge.Run 完成订阅注册，避免测试在 handler 就绪前投递。
func (f *fakeSubscriber) waitReady(t *testing.T) {
	t.Helper()

	select {
	case <-f.ready:
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for subscription")
	}
}

// subscription 返回订阅的 subject 与起始序列。
func (f *fakeSubscriber) subscription() (string, int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subject, f.startSeq
}

// deliver 通过已注册的 handler 投递一条原始负载。
func (f *fakeSubscriber) deliver(t *testing.T, data []byte) error {
	t.Helper()

	f.mu.Lock()
	handler := f.handler
	f.mu.Unlock()
	if handler == nil {
		t.Fatal("handler not registered")
	}
	return handler(context.Background(), data)
}

// startBridge 在后台运行 Bridge，并在测试结束时确认它在 ctx 取消后退出，
// 避免控制面关闭时遗留 goroutine。
func startBridge(t *testing.T, bridge *Bridge) (context.CancelFunc, <-chan struct{}) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		bridge.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(waitTimeout):
			t.Error("Bridge.Run did not return after context cancel")
		}
	})
	return cancel, done
}

func TestBridgeFeedsHub(t *testing.T) {
	hub := NewHub()
	ch, cancelSub := hub.Subscribe(1)
	defer cancelSub()

	sub := newFakeSubscriber()
	cancelRun, done := startBridge(t, NewBridge(sub, hub))

	sub.waitReady(t)
	if subject, seq := sub.subscription(); subject != messaging.ControlPlaneEventWildcard() || seq != 0 {
		t.Fatalf("subscription mismatch: got subject %q startSeq %d, want %q and 0",
			subject, seq, messaging.ControlPlaneEventWildcard())
	}

	want := Event{
		NodeID:    "n1",
		SessionID: "s1",
		Seq:       5,
		Type:      "message.updated",
		Data:      json.RawMessage(`{"role":"assistant"}`),
		Time:      time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC),
	}
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if err := sub.deliver(t, payload); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if got := receive(t, ch); !sameEvent(got, want) {
		t.Fatalf("hub event mismatch: got %#v, want %#v", got, want)
	}

	// Run 必须在 ctx 取消后返回，否则控制面关闭时无法回收 goroutine。
	cancelRun()
	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("Bridge.Run did not return after context cancel")
	}
}

// retrySubscriber 记录每次订阅调用；前 fail 次返回错误，之后阻塞到 ctx 取消。
type retrySubscriber struct {
	mu     sync.Mutex
	calls  int
	fail   int
	called chan int
}

func newRetrySubscriber(fail int) *retrySubscriber {
	return &retrySubscriber{fail: fail, called: make(chan int, 16)}
}

func (s *retrySubscriber) SubscribeBytes(
	ctx context.Context,
	_ string,
	_ int64,
	_ func(context.Context, []byte) error,
) error {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	select {
	case s.called <- n:
	default:
	}

	if n <= s.fail {
		return errors.New("temporary subscription failure")
	}
	<-ctx.Done()
	return ctx.Err()
}

func (s *retrySubscriber) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// waitForCall 等待第 want 次订阅调用。
func waitForCall(t *testing.T, called <-chan int, want int) {
	t.Helper()

	deadline := time.After(time.Second)
	for {
		select {
		case n := <-called:
			if n >= want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for subscription call %d", want)
		}
	}
}

// TestBridgeRetriesAfterSubscriptionError 证明订阅失败不会让桥接永久停止。
func TestBridgeRetriesAfterSubscriptionError(t *testing.T) {
	sub := newRetrySubscriber(1)
	bridge := NewBridge(sub, NewHub())
	bridge.retryDelay = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		bridge.Run(ctx)
	}()

	waitForCall(t, sub.called, 1)
	waitForCall(t, sub.called, 2)
	if got := sub.callCount(); got < 2 {
		t.Fatalf("subscription calls = %d, want at least 2", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("Bridge.Run did not return after context cancel")
	}
}

// nilSubscriber 每次调用都立即返回 nil，用来模拟订阅端“提前正常返回”。
type nilSubscriber struct {
	mu     sync.Mutex
	calls  int
	called chan int
}

func newNilSubscriber() *nilSubscriber {
	return &nilSubscriber{called: make(chan int, 16)}
}

func (s *nilSubscriber) SubscribeBytes(
	_ context.Context,
	_ string,
	_ int64,
	_ func(context.Context, []byte) error,
) error {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	select {
	case s.called <- n:
	default:
	}
	return nil
}

func (s *nilSubscriber) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// TestBridgeResubscribesWhenSubscribeReturnsNil 证明 SubscribeBytes 提前返回
// nil 时也会重新订阅，而不是让 Run 退出或空转。
func TestBridgeResubscribesWhenSubscribeReturnsNil(t *testing.T) {
	sub := newNilSubscriber()
	bridge := NewBridge(sub, NewHub())
	bridge.retryDelay = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		bridge.Run(ctx)
	}()

	waitForCall(t, sub.called, 1)
	waitForCall(t, sub.called, 3)

	cancel()
	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("Bridge.Run did not return after context cancel")
	}
}

func TestBridgeDropsMalformedPayload(t *testing.T) {
	hub := NewHub()
	ch, cancelSub := hub.Subscribe(1)
	defer cancelSub()

	sub := newFakeSubscriber()
	startBridge(t, NewBridge(sub, hub))
	sub.waitReady(t)

	// 坏消息必须被吞掉：返回 nil 让订阅继续，而不是让整条订阅退出。
	if err := sub.deliver(t, []byte("not-json")); err != nil {
		t.Fatalf("handler returned error for malformed payload: %v", err)
	}

	want := Event{
		NodeID: "n1",
		Seq:    6,
		Type:   "session.updated",
		Time:   time.Date(2026, 11, 14, 10, 0, 1, 0, time.UTC),
	}
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if err := sub.deliver(t, payload); err != nil {
		t.Fatalf("handler returned error after malformed payload: %v", err)
	}

	if got := receive(t, ch); !sameEvent(got, want) {
		t.Fatalf("hub event mismatch: got %#v, want %#v", got, want)
	}
}
