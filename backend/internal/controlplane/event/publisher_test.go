package event

import (
	"context"
	"encoding/json"
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

	got, ok := bus.published["cp.events.node.n1"].(Event)
	if !ok {
		t.Fatalf("no state event published on cp.events.node.n1; got %#v", bus.published)
	}
	if got.NodeID != want.NodeID || got.SessionID != want.SessionID || got.Seq != want.Seq ||
		got.Type != want.Type || string(got.Data) != string(want.Data) || !got.Time.Equal(want.Time) {
		t.Fatalf("published event mismatch: got %#v, want %#v", got, want)
	}
	if got.Owner != pub.Owner() || got.Owner == "" {
		t.Fatalf("owner = %q, want the publisher owner %q", got.Owner, pub.Owner())
	}
	if got.ClusterSeq != 1 {
		t.Fatalf("cluster seq = %d, want 1 for the first state event", got.ClusterSeq)
	}
	if core.calls != 0 {
		t.Fatalf("state event must not use Core NATS, got %d calls", core.calls)
	}
}

// TestPublisherStampsMonotonicSequencePerNode 证明同一节点的状态事件序号递增，
// 不同节点各自计数，PTY 事件不参与。
func TestPublisherStampsMonotonicSequencePerNode(t *testing.T) {
	bus := &fakeBus{}
	pub := NewPublisher(bus, &fakeCore{})

	publish := func(nodeID, typ string) Event {
		t.Helper()
		if err := pub.PublishEvent(context.Background(), Event{NodeID: nodeID, Type: typ}); err != nil {
			t.Fatalf("PublishEvent(%s): %v", typ, err)
		}
		return bus.published["cp.events.node."+nodeID].(Event)
	}

	first := publish("n1", "session.created")
	second := publish("n1", "session.updated")
	other := publish("n2", "session.created")
	if first.ClusterSeq != 1 || second.ClusterSeq != 2 {
		t.Fatalf("n1 seqs = %d,%d, want 1,2", first.ClusterSeq, second.ClusterSeq)
	}
	if other.ClusterSeq != 1 {
		t.Fatalf("n2 seq = %d, want an independent counter starting at 1", other.ClusterSeq)
	}
	if first.Owner != second.Owner || first.Owner != other.Owner {
		t.Fatalf("owners = %q,%q,%q, want one owner per publisher", first.Owner, second.Owner, other.Owner)
	}

	// PTY 事件不应占用状态序号。
	pty := Event{NodeID: "n1", Type: EventTypePTYOutput, Data: json.RawMessage(`{"pty_id":"p1"}`)}
	if err := pub.PublishEvent(context.Background(), pty); err != nil {
		t.Fatalf("publish pty: %v", err)
	}
	third := publish("n1", "session.updated")
	if third.ClusterSeq != 3 {
		t.Fatalf("seq after pty = %d, want 3 (pty must not increment)", third.ClusterSeq)
	}
}

// TestPublisherPublishSnapshotDoesNotStamp 证明快照按调用方给出的 watermark
// 原样发布，不会自己递增序号。
func TestPublisherPublishSnapshotDoesNotStamp(t *testing.T) {
	bus := &fakeBus{}
	pub := NewPublisher(bus, &fakeCore{})

	snap := Event{
		NodeID:     "n1",
		Type:       EventTypeNodeSnapshot,
		Owner:      "owner-a",
		ClusterSeq: 7,
		Data:       json.RawMessage(`{"sessions":[]}`),
	}
	if err := pub.PublishSnapshot(context.Background(), snap); err != nil {
		t.Fatalf("PublishSnapshot: %v", err)
	}
	got := bus.published["cp.events.node.n1"].(Event)
	if got.Owner != "owner-a" || got.ClusterSeq != 7 {
		t.Fatalf("snapshot watermark = %q/%d, want owner-a/7", got.Owner, got.ClusterSeq)
	}
	// 后续状态事件仍从 1 开始，说明快照没有推进计数器。
	if err := pub.PublishEvent(context.Background(), Event{NodeID: "n1", Type: "session.created"}); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}
	ev := bus.published["cp.events.node.n1"].(Event)
	if ev.ClusterSeq != 1 {
		t.Fatalf("cluster seq after snapshot = %d, want 1", ev.ClusterSeq)
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

func TestPublisherRoutesLiveEventsToCore(t *testing.T) {
	bus := &fakeBus{}
	core := &fakeCore{}
	pub := NewPublisher(bus, core)

	want := Event{
		NodeID:    "node_1",
		SessionID: "ses_1",
		Seq:       5,
		Type:      "session.text.delta",
		Data:      json.RawMessage(`{"delta":"你"}`),
		Time:      time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC),
	}
	if err := pub.PublishEvent(context.Background(), want); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}

	if len(bus.published) != 0 {
		t.Fatalf("live event must not enter JetStream, got %#v", bus.published)
	}
	if core.calls != 1 || core.subject != "cp.live.node.node_1" {
		t.Fatalf("core publish = %d calls to %q, want 1 to cp.live.node.node_1", core.calls, core.subject)
	}

	var got Event
	if err := json.Unmarshal(core.data, &got); err != nil {
		t.Fatalf("unmarshal live payload: %v", err)
	}
	if !got.Live {
		t.Fatal("live payload must carry live=true")
	}
	if got.Owner != "" || got.ClusterSeq != 0 {
		t.Fatalf("live event stamped owner=%q cluster_seq=%d, want no stamp", got.Owner, got.ClusterSeq)
	}
	if got.NodeID != want.NodeID || got.SessionID != want.SessionID || got.Seq != want.Seq ||
		got.Type != want.Type || string(got.Data) != string(want.Data) || !got.Time.Equal(want.Time) {
		t.Fatalf("live payload mismatch: got %#v, want %#v", got, want)
	}
}

func TestPublisherKeepsStateEventsOnBus(t *testing.T) {
	bus := &fakeBus{}
	core := &fakeCore{}
	pub := NewPublisher(bus, core)

	ev := Event{NodeID: "node_1", Type: "session.execution.succeeded"}
	if err := pub.PublishEvent(context.Background(), ev); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}

	if _, ok := bus.published["cp.events.node.node_1"]; !ok {
		t.Fatalf("state event missing from JetStream: %#v", bus.published)
	}
	if core.calls != 0 {
		t.Fatalf("state event must not use Core NATS, got %d calls", core.calls)
	}
}

func TestIsLiveEvent(t *testing.T) {
	for _, typ := range []string{
		"session.text.delta",
		"session.reasoning.delta",
		"session.tool.input.delta",
		"session.tool.progress",
	} {
		if !IsLiveEvent(typ) {
			t.Errorf("IsLiveEvent(%q) = false, want true", typ)
		}
	}
	for _, typ := range []string{
		"session.text.ended",
		"session.execution.succeeded",
		"permission.asked",
		"pty.output",
	} {
		if IsLiveEvent(typ) {
			t.Errorf("IsLiveEvent(%q) = true, want false", typ)
		}
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
