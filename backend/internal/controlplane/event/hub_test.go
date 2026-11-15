package event

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// waitTimeout 限制测试中等待异步结果的时长，避免实现出错时挂死整个测试进程。
const waitTimeout = 2 * time.Second

// receive 从订阅通道取一个事件，超时或通道意外关闭都判定为测试失败。
func receive(t *testing.T, ch <-chan Event) Event {
	t.Helper()

	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("event channel closed unexpectedly")
		}
		return ev
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

func TestPublishDeliversEventUnchanged(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe(1)
	defer cancel()

	want := Event{
		NodeID:    "node-1",
		SessionID: "ses-1",
		Seq:       7,
		Type:      "message.updated",
		Data:      json.RawMessage(`{"role":"assistant"}`),
		Time:      time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
	}
	hub.Publish(want)

	got := receive(t, ch)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event mismatch: got %#v, want %#v", got, want)
	}
}

func TestPublishFansOutToAllSubscribers(t *testing.T) {
	hub := NewHub()
	first, cancelFirst := hub.Subscribe(1)
	defer cancelFirst()
	second, cancelSecond := hub.Subscribe(1)
	defer cancelSecond()

	want := Event{NodeID: "node-1", Seq: 3, Type: "session.updated"}
	hub.Publish(want)

	for name, ch := range map[string]<-chan Event{"first": first, "second": second} {
		if got := receive(t, ch); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s subscriber event mismatch: got %#v, want %#v", name, got, want)
		}
	}
}

func TestPublishDropsSlowSubscriberWithoutBlocking(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe(1)
	defer cancel()

	first := Event{NodeID: "node-1", Seq: 1, Type: "first"}
	hub.Publish(first)

	// 第二个事件会填满 1 个缓冲；Publish 不得等待未读取的订阅者。
	done := make(chan struct{})
	go func() {
		defer close(done)
		hub.Publish(Event{NodeID: "node-1", Seq: 2, Type: "second"})
	}()

	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("Publish blocked on a full subscriber")
	}

	// 满缓冲之前的事件仍然可读，随后通道关闭。
	if got := receive(t, ch); !reflect.DeepEqual(got, first) {
		t.Fatalf("first event mismatch: got %#v, want %#v", got, first)
	}
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected dropped subscriber channel to be closed")
		}
	case <-time.After(waitTimeout):
		t.Fatal("dropped subscriber channel was not closed")
	}
}

func TestCancelClosesChannelOnceAndRemovesSubscriber(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe(1)

	cancel()
	// 重复调用必须幂等，不能重复关闭通道或 panic。
	cancel()

	if _, ok := <-ch; ok {
		t.Fatal("channel still open after cancel")
	}

	hub.mu.Lock()
	remaining := len(hub.subs)
	hub.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("subscriber not removed: %d remaining", remaining)
	}

	// 已取消的订阅者不会让后续 Publish 向已关闭通道发送。
	hub.Publish(Event{NodeID: "node-1", Seq: 9, Type: "after-cancel"})
}

// TestHubFiltersLiveEventsForPlainSubscribers 证明不关心增量事件的订阅者
// 不会收到 live 事件，避免总览订阅被 token 级流量拖慢。
func TestHubFiltersLiveEventsForPlainSubscribers(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe(4)
	defer cancel()

	hub.Publish(Event{NodeID: "node-1", Type: "session.text.delta", Live: true})
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("plain subscriber was closed after a live event")
		}
		t.Fatalf("plain subscriber received live event %#v", ev)
	case <-time.After(20 * time.Millisecond):
	}

	hub.Publish(Event{NodeID: "node-1", Type: "session.execution.succeeded"})
	got := receive(t, ch)
	if got.Type != "session.execution.succeeded" {
		t.Fatalf("event type = %q, want session.execution.succeeded", got.Type)
	}
}

// TestHubLiveSubscriberOverflowDropsFramesInsteadOfEvicting 证明 live 订阅溢出
// 时只丢帧、不淘汰订阅者：淘汰会让手机陷入「resync → 重拉 → 再溢出」循环。
func TestHubLiveSubscriberOverflowDropsFramesInsteadOfEvicting(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe(1, WithLive())

	for i := 0; i < 3; i++ {
		hub.Publish(Event{NodeID: "node-1", Seq: int64(i + 1), Type: "session.text.delta", Live: true})
	}

	// 缓冲只有 1，第一条 live 事件应保留；后续两条被丢弃且不关闭通道。
	if got := receive(t, ch); got.Type != "session.text.delta" || got.Seq != 1 {
		t.Fatalf("first event = %#v, want the retained live frame", got)
	}

	hub.Publish(Event{NodeID: "node-1", Type: "session.execution.succeeded"})
	if got := receive(t, ch); got.Type != "session.execution.succeeded" {
		t.Fatalf("event after overflow = %#v, want the state event", got)
	}

	// 取消函数在溢出后仍应幂等安全。
	cancel()
	cancel()
}

// TestHubNonLiveOverflowStillEvicts 固化现有语义：状态事件不能静默丢失，
// 缓冲满时淘汰订阅者让客户端重连并重拉快照。
func TestHubNonLiveOverflowStillEvicts(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe(1)
	defer cancel()

	hub.Publish(Event{NodeID: "node-1", Seq: 1, Type: "first"})
	hub.Publish(Event{NodeID: "node-1", Seq: 2, Type: "second"})

	if got := receive(t, ch); got.Type != "first" {
		t.Fatalf("first event = %#v, want first", got)
	}
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected the channel to be closed after a non-live overflow")
		}
	case <-time.After(waitTimeout):
		t.Fatal("channel was not closed after a non-live overflow")
	}
}

func TestSubscribeZeroBufferIsUsable(t *testing.T) {
	hub := NewHub()
	want := Event{NodeID: "node-1", Seq: 1, Type: "unbuffered"}

	// 无缓冲通道只有接收方已阻塞时才能投递，因此每次尝试创建新订阅并按
	// 调度结果重试，避免用 sleep 猜测 goroutine 何时就绪。
	deadline := time.Now().Add(waitTimeout)
	for {
		ch, cancel := hub.Subscribe(0)
		published := make(chan struct{})
		go func() {
			defer close(published)
			hub.Publish(want)
		}()

		var (
			got Event
			ok  bool
		)
		select {
		case got, ok = <-ch:
		case <-time.After(waitTimeout):
			t.Fatal("timed out waiting for event on unbuffered subscriber")
		}
		<-published
		cancel()

		if ok {
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("event mismatch: got %#v, want %#v", got, want)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("unbuffered subscriber never received a published event")
		}
	}
}
