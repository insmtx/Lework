package controlplane

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// fakeNodeLister 是可变的节点列表来源，模拟节点心跳时间变化。
type fakeNodeLister struct {
	mu    sync.Mutex
	nodes []store.Node
	err   error
}

func (f *fakeNodeLister) ListNodes(context.Context) ([]store.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return append([]store.Node(nil), f.nodes...), nil
}

func (f *fakeNodeLister) setNodes(nodes ...store.Node) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nodes = nodes
}

func (f *fakeNodeLister) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// fakePublisher 记录已发布的离线事件，并可注入发布失败。
type fakePublisher struct {
	mu     sync.Mutex
	events []event.Event
	err    error
}

func (f *fakePublisher) PublishEvent(_ context.Context, ev event.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, ev)
	return nil
}

func (f *fakePublisher) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakePublisher) offline() []event.Event {
	f.mu.Lock()
	defer f.mu.Unlock()

	var offline []event.Event
	for _, ev := range f.events {
		if ev.Type == nodeOfflineEvent {
			offline = append(offline, ev)
		}
	}
	return offline
}

// TestReconcilerPublishesOfflineOncePerNode 证明节点持续超时时只发布一次离线事件。
func TestReconcilerPublishesOfflineOncePerNode(t *testing.T) {
	nodes := &fakeNodeLister{}
	publisher := &fakePublisher{}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	r := NewReconciler(nodes, publisher, time.Minute)
	r.now = func() time.Time { return now }
	nodes.setNodes(store.Node{ID: "node_1", LastSeen: now.Add(-2 * time.Minute)})

	r.sweep(context.Background())
	r.sweep(context.Background())

	offline := publisher.offline()
	if len(offline) != 1 {
		t.Fatalf("offline events = %d, want 1", len(offline))
	}
	if offline[0].NodeID != "node_1" {
		t.Fatalf("offline event node = %q, want node_1", offline[0].NodeID)
	}
	if !offline[0].Time.Equal(now) {
		t.Fatalf("offline event time = %v, want %v", offline[0].Time, now)
	}
}

// TestReconcilerResetsAfterNodeReturns 证明节点恢复后再次超时会重新发布。
func TestReconcilerResetsAfterNodeReturns(t *testing.T) {
	nodes := &fakeNodeLister{}
	publisher := &fakePublisher{}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	r := NewReconciler(nodes, publisher, time.Minute)
	r.now = func() time.Time { return now }

	nodes.setNodes(store.Node{ID: "node_1", LastSeen: now.Add(-2 * time.Minute)})
	r.sweep(context.Background())
	if got := len(publisher.offline()); got != 1 {
		t.Fatalf("offline events = %d, want 1", got)
	}

	// 心跳恢复：本轮不发布，同时清除离线标记。
	nodes.setNodes(store.Node{ID: "node_1", LastSeen: now})
	r.sweep(context.Background())
	if got := len(publisher.offline()); got != 1 {
		t.Fatalf("offline events = %d after recovery, want 1", got)
	}

	// 再次静默：标记已清除，应重新发布一条。
	nodes.setNodes(store.Node{ID: "node_1", LastSeen: now.Add(-2 * time.Minute)})
	r.sweep(context.Background())
	if got := len(publisher.offline()); got != 2 {
		t.Fatalf("offline events = %d after second timeout, want 2", got)
	}
}

// TestReconcilerForgetsRemovedNode 证明节点从存储消失后不再保留离线标记。
func TestReconcilerForgetsRemovedNode(t *testing.T) {
	nodes := &fakeNodeLister{}
	publisher := &fakePublisher{}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	r := NewReconciler(nodes, publisher, time.Minute)
	r.now = func() time.Time { return now }

	nodes.setNodes(store.Node{ID: "node_1", LastSeen: now.Add(-2 * time.Minute)})
	r.sweep(context.Background())

	nodes.setNodes()
	r.sweep(context.Background())
	if r.lastOffline["node_1"] {
		t.Fatal("offline flag survived node removal")
	}
}

// TestReconcilerRetriesAfterPublishFailure 证明发布失败不会写入离线标记，下一轮会重试。
func TestReconcilerRetriesAfterPublishFailure(t *testing.T) {
	nodes := &fakeNodeLister{}
	publisher := &fakePublisher{}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	r := NewReconciler(nodes, publisher, time.Minute)
	r.now = func() time.Time { return now }
	nodes.setNodes(store.Node{ID: "node_1", LastSeen: now.Add(-2 * time.Minute)})

	publisher.setError(errors.New("bus down"))
	r.sweep(context.Background())

	publisher.setError(nil)
	r.sweep(context.Background())
	if got := len(publisher.offline()); got != 1 {
		t.Fatalf("offline events = %d after retry, want 1", got)
	}
}

// TestReconcilerRunSweepsImmediatelyAndStopsOnCancel 证明 Run 启动即对账，
// 并且 ctx 取消后及时返回。
func TestReconcilerRunSweepsImmediatelyAndStopsOnCancel(t *testing.T) {
	nodes := &fakeNodeLister{}
	publisher := &fakePublisher{}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	r := NewReconciler(nodes, publisher, time.Minute)
	r.now = func() time.Time { return now }
	nodes.setNodes(store.Node{ID: "node_1", LastSeen: now.Add(-2 * time.Minute)})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for len(publisher.offline()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := len(publisher.offline()); got != 1 {
		t.Fatalf("offline events = %d, want 1 immediately after Run", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after cancellation")
	}
}

// TestReconcilerSurvivesListFailure 证明读取节点失败只跳过本轮，不会 panic。
func TestReconcilerSurvivesListFailure(t *testing.T) {
	nodes := &fakeNodeLister{err: errors.New("db down")}
	publisher := &fakePublisher{}
	r := NewReconciler(nodes, publisher, time.Minute)

	r.sweep(context.Background())

	if got := len(publisher.offline()); got != 0 {
		t.Fatalf("offline events = %d, want 0", got)
	}
}
