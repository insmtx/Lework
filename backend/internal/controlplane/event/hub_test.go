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
