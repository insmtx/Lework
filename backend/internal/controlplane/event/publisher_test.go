package event

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// fakeBus 记录 JetStream 发布，替代真实 NATS，避免单元测试依赖外部服务。
type fakeBus struct {
	published map[string]any
}

// Publish 记录事件；published 以 subject 为键，便于断言路由是否正确。
func (f *fakeBus) Publish(_ context.Context, topic string, ev any) error {
	if f.published == nil {
		f.published = make(map[string]any)
	}
	f.published[topic] = ev
	return nil
}

// fakeCore 记录 Core NATS 发布的 subject 与原始字节。
type fakeCore struct {
	subject string
	data    []byte
	calls   int
}

func (f *fakeCore) PublishCore(_ context.Context, subject string, data []byte) error {
	f.subject = subject
	f.data = data
	f.calls++
	return nil
}

func TestPublisherRoutesStateEventToJetStream(t *testing.T) {
	bus := &fakeBus{}
	core := &fakeCore{}
	pub := NewPublisher(bus, core)

	want := Event{
		NodeID:    "n1",
		SessionID: "s1",
		Seq:       3,
		Type:      "session.updated",
		Data:      json.RawMessage(`{"title":"demo"}`),
		Time:      time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC),
	}
	if err := pub.PublishEvent(context.Background(), want); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}

	got, ok := bus.published["cp.events.node.n1"]
	if !ok {
		t.Fatalf("no state event published on cp.events.node.n1; got %#v", bus.published)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("published event mismatch: got %#v, want %#v", got, want)
	}
	if core.calls != 0 {
		t.Fatalf("state event must not use Core NATS, got %d calls", core.calls)
	}
}

func TestPublisherRoutesPTYEventToCore(t *testing.T) {
	bus := &fakeBus{}
	core := &fakeCore{}
	pub := NewPublisher(bus, core)

	want := Event{
		NodeID: "n1",
		Seq:    9,
		Type:   "pty.output",
		Data:   json.RawMessage(`{"pty_id":"p1","chunk":"bHMNCg=="}`),
	}
	if err := pub.PublishEvent(context.Background(), want); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}

	if core.subject != "cp.pty.n1.p1" {
		t.Fatalf("pty subject mismatch: got %q, want %q", core.subject, "cp.pty.n1.p1")
	}
	var got Event
	if err := json.Unmarshal(core.data, &got); err != nil {
		t.Fatalf("unmarshal pty payload: %v", err)
	}
	if !sameEvent(got, want) {
		t.Fatalf("pty payload mismatch: got %#v, want %#v", got, want)
	}
	if len(bus.published) != 0 {
		t.Fatalf("pty event must not enter JetStream, got %#v", bus.published)
	}
}

func TestPublisherRejectsPTYWithoutID(t *testing.T) {
	cases := map[string]json.RawMessage{
		"empty object": json.RawMessage(`{}`),
		"blank id":     json.RawMessage(`{"pty_id":""}`),
		"no data":      nil,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			bus := &fakeBus{}
			core := &fakeCore{}
			pub := NewPublisher(bus, core)

			ev := Event{NodeID: "n1", Type: "pty.output", Data: data}
			if err := pub.PublishEvent(context.Background(), ev); err == nil {
				t.Fatal("expected error for pty event without pty_id")
			}
			if core.calls != 0 {
				t.Fatalf("nothing should be published to Core NATS, got %d calls", core.calls)
			}
			if len(bus.published) != 0 {
				t.Fatalf("nothing should be published to JetStream, got %#v", bus.published)
			}
		})
	}
}
