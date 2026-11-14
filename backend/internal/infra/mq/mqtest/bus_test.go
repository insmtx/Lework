package mqtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/infra/mq"
)

// subscriberCounts 返回当前三类订阅者的数量，供测试等待注册完成。
//
// 订阅方法注册后立即阻塞，调用方无法从返回值判断注册是否生效；测试用这个
// 只读快照替代固定 sleep。
func (b *Bus) subscriberCounts() (bytesSubs, pullSubs, coreSubs int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.byteSubs), len(b.pullSubs), len(b.coreSubs)
}

// waitForSubscribers 轮询直到条件满足或超时，避免测试依赖固定 sleep。
func waitForSubscribers(t *testing.T, bus *Bus, want int, count func() int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if count() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("subscribers never reached %d", want)
}

// TestBusFansOutStateEvents 证明同一个 subject 的事件会广播给每个字节订阅者。
func TestBusFansOutStateEvents(t *testing.T) {
	bus := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu      sync.Mutex
		payload [][]byte
	)
	handler := func(_ context.Context, data []byte) error {
		mu.Lock()
		defer mu.Unlock()
		payload = append(payload, append([]byte(nil), data...))
		return nil
	}

	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errCh <- bus.SubscribeBytes(ctx, "cp.events.node.>", 0, handler) }()
	}
	waitForSubscribers(t, bus, 2, func() int {
		bytesSubs, _, _ := bus.subscriberCounts()
		return bytesSubs
	})

	event := map[string]string{"type": "session.created"}
	if err := bus.Publish(ctx, "cp.events.node.node_1", event); err != nil {
		t.Fatalf("Publish() failed: %v", err)
	}

	want, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	mu.Lock()
	got := payload
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("handlers received %d messages, want 2", len(got))
	}
	for i, data := range got {
		if string(data) != string(want) {
			t.Fatalf("handler %d received %s, want %s", i, data, want)
		}
	}

	cancel()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errCh:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("SubscribeBytes returned %v, want context.Canceled", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("SubscribeBytes did not return after cancel")
		}
	}
}

// TestBusDeliversPullMessageToOneConsumer 证明同一 durable 的 pull 订阅者
// 组成一个消费组：每条消息只投给其中一个，合计恰好收到全部消息。
func TestBusDeliversPullMessageToOneConsumer(t *testing.T) {
	bus := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu     sync.Mutex
		seen   = make(map[string]int)
		handle = func(_ context.Context, data []byte) error {
			mu.Lock()
			defer mu.Unlock()
			seen[string(data)]++
			return nil
		}
	)

	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			errCh <- bus.SubscribePull(ctx, "CONTROLPLANE_EVENT_STREAM", "cp.events.node.>", "dup", handle)
		}()
	}
	waitForSubscribers(t, bus, 2, func() int {
		_, pullSubs, _ := bus.subscriberCounts()
		return pullSubs
	})

	const messages = 10
	for i := 0; i < messages; i++ {
		payload := map[string]int{"seq": i}
		if err := bus.Publish(ctx, "cp.events.node.node_1", payload); err != nil {
			t.Fatalf("Publish(%d) failed: %v", i, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != messages {
		t.Fatalf("distinct messages = %d, want %d", len(seen), messages)
	}
	for seq := 0; seq < messages; seq++ {
		data, err := json.Marshal(map[string]int{"seq": seq})
		if err != nil {
			t.Fatalf("marshal seq %d: %v", seq, err)
		}
		if got := seen[string(data)]; got != 1 {
			t.Fatalf("message seq=%d delivered %d times, want 1", seq, got)
		}
	}

	cancel()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errCh:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("SubscribePull returned %v, want context.Canceled", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("SubscribePull did not return after cancel")
		}
	}
}

// TestBusRequestBytesNoResponders 证明没有订阅者时返回 mq.ErrNoResponders。
func TestBusRequestBytesNoResponders(t *testing.T) {
	bus := New()

	_, err := bus.RequestBytes(context.Background(), "cp.node.node_1.request", []byte("{}"))
	if !errors.Is(err, mq.ErrNoResponders) {
		t.Fatalf("RequestBytes error = %v, want mq.ErrNoResponders", err)
	}
}

// TestBusRequestBytesCallsResponder 证明请求会同步交给匹配的应答者并取回字节。
func TestBusRequestBytesCallsResponder(t *testing.T) {
	bus := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- bus.SubscribeCore(ctx, "cp.node.node_1.request", "node_1",
			func(_ context.Context, request []byte) []byte {
				return []byte(fmt.Sprintf("reply:%s", request))
			})
	}()
	waitForSubscribers(t, bus, 1, func() int {
		_, _, coreSubs := bus.subscriberCounts()
		return coreSubs
	})

	got, err := bus.RequestBytes(context.Background(), "cp.node.node_1.request", []byte("ping"))
	if err != nil {
		t.Fatalf("RequestBytes() failed: %v", err)
	}
	if string(got) != "reply:ping" {
		t.Fatalf("reply = %q, want %q", got, "reply:ping")
	}
}

// TestMatchSubject 钉住 subject 匹配规则：精确分段与尾部 > 通配。
func TestMatchSubject(t *testing.T) {
	tests := []struct {
		pattern string
		subject string
		want    bool
	}{
		{pattern: "cp.events.node.>", subject: "cp.events.node.n1", want: true},
		{pattern: "cp.events.node.>", subject: "cp.events.node", want: false},
		{pattern: "cp.events.node.>", subject: "cp.events.node.n1.extra", want: true},
		{pattern: "cp.events.node.n1", subject: "cp.events.node.n1", want: true},
		{pattern: "cp.events.node.n1", subject: "cp.events.node.n2", want: false},
		{pattern: "cp.events.node.n1", subject: "cp.events.node.n1.extra", want: false},
		{pattern: "a.>", subject: "a", want: false},
		{pattern: ">", subject: "a", want: true},
	}

	for _, tt := range tests {
		if got := matchSubject(tt.pattern, tt.subject); got != tt.want {
			t.Fatalf("matchSubject(%q, %q) = %v, want %v", tt.pattern, tt.subject, got, tt.want)
		}
	}
}
