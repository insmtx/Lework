package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/agentd/protocol"
)

// registerTestNode 创建并注册一个节点连接，返回连接及其写入端。
func registerTestNode(t *testing.T, r *Registry, id string) (*Connection, *fakeWriter) {
	t.Helper()

	writer := newFakeWriter()
	conn := NewConnection(id, writer)
	r.Register(Info{
		ID:           id,
		Name:         "box-" + id,
		OS:           "linux",
		Arch:         "amd64",
		AgentVersion: "dev",
		Capabilities: []string{"session.list"},
	}, conn)
	return conn, writer
}

// startRegistryRequest 在后台通过注册表发起请求。
func startRegistryRequest(t *testing.T, r *Registry, nodeID, method string, params json.RawMessage) <-chan requestResult {
	t.Helper()

	done := make(chan requestResult, 1)
	go func() {
		result, err := r.Request(context.Background(), nodeID, method, params)
		done <- requestResult{result: result, err: err}
	}()
	return done
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	registerTestNode(t, r, "node-1")

	info, ok := r.Get("node-1")
	if !ok {
		t.Fatal("Get() = false for a registered node, want true")
	}
	if !info.Connected {
		t.Error("Info.Connected = false after Register, want true")
	}
	if info.ID != "node-1" {
		t.Errorf("Info.ID = %q, want %q", info.ID, "node-1")
	}
	if info.Name != "box-node-1" {
		t.Errorf("Info.Name = %q, want %q", info.Name, "box-node-1")
	}
	if info.OS != "linux" || info.Arch != "amd64" || info.AgentVersion != "dev" {
		t.Errorf("Info metadata = %q/%q/%q, want linux/amd64/dev", info.OS, info.Arch, info.AgentVersion)
	}
	if !reflect.DeepEqual(info.Capabilities, []string{"session.list"}) {
		t.Errorf("Info.Capabilities = %v, want [session.list]", info.Capabilities)
	}
	if info.LastSeen.IsZero() {
		t.Error("Info.LastSeen is zero, want the registration time")
	}

	// 调用方修改返回值不能影响注册表内部状态。
	info.Capabilities[0] = "hacked"
	info.Name = "hacked"
	again, ok := r.Get("node-1")
	if !ok {
		t.Fatal("second Get() = false, want true")
	}
	if again.Name != "box-node-1" {
		t.Errorf("Info.Name = %q after caller mutation, want %q", again.Name, "box-node-1")
	}
	if !reflect.DeepEqual(again.Capabilities, []string{"session.list"}) {
		t.Errorf("Info.Capabilities = %v after caller mutation, want [session.list]", again.Capabilities)
	}

	if _, ok := r.Get("node-missing"); ok {
		t.Error("Get() = true for an unknown node, want false")
	}
}

func TestRegistryRegisterReplacesConnection(t *testing.T) {
	r := NewRegistry()

	first, firstWriter := registerTestNode(t, r, "node-1")
	second, secondWriter := registerTestNode(t, r, "node-1")

	if got := firstWriter.closeCount.Load(); got != 1 {
		t.Errorf("replaced writer closed %d times, want exactly 1", got)
	}
	select {
	case <-firstWriter.closed:
	case <-time.After(waitTimeout):
		t.Fatal("replaced connection writer was not closed")
	}

	if _, err := first.Request(context.Background(), "session.list", nil); !errors.Is(err, ErrConnectionClosed) {
		t.Errorf("old connection Request() error = %v, want ErrConnectionClosed", err)
	}

	done := startRegistryRequest(t, r, "node-1", "session.list", json.RawMessage(`{"directory":"/work"}`))
	frame := secondWriter.nextFrame(t)
	if frame.Method != "session.list" {
		t.Errorf("frame.Method = %q, want %q", frame.Method, "session.list")
	}
	firstWriter.expectNoFrame(t)

	want := json.RawMessage(`{"sessions":[]}`)
	if ok := second.Deliver(frameWithResult(frame.ID, want)); !ok {
		t.Fatal("Deliver() on the replacement connection = false, want true")
	}
	got := awaitResult(t, done)
	if got.err != nil {
		t.Fatalf("Request() failed: %v", got.err)
	}
	if string(got.result) != string(want) {
		t.Errorf("result = %s, want %s", got.result, want)
	}
}

func TestRegistryRegisterSameConnectionTwice(t *testing.T) {
	r := NewRegistry()
	conn, writer := registerTestNode(t, r, "node-1")

	// 重复注册同一个连接不应把它自己关掉。
	r.Register(Info{ID: "node-1", Name: "box-node-1"}, conn)

	if got := writer.closeCount.Load(); got != 0 {
		t.Fatalf("writer closed %d times after re-registering the same connection, want 0", got)
	}

	done := startRegistryRequest(t, r, "node-1", "opencode.status", nil)
	frame := writer.nextFrame(t)
	if ok := conn.Deliver(frameWithResult(frame.ID, json.RawMessage(`{"ok":true}`))); !ok {
		t.Fatal("Deliver() = false, want true")
	}
	if got := awaitResult(t, done); got.err != nil {
		t.Fatalf("Request() after re-register failed: %v", got.err)
	}
}

func TestRegistryUnregisterIgnoresStaleConnection(t *testing.T) {
	r := NewRegistry()

	stale, _ := registerTestNode(t, r, "node-1")
	current, currentWriter := registerTestNode(t, r, "node-1")

	// 旧连接退出时不能把已经替换上来的新连接删掉。
	r.Unregister("node-1", stale)

	if _, ok := r.Get("node-1"); !ok {
		t.Fatal("Get() = false after stale Unregister, want the current connection to remain")
	}

	done := startRegistryRequest(t, r, "node-1", "session.list", nil)
	frame := currentWriter.nextFrame(t)
	_ = current.Deliver(frameWithResult(frame.ID, json.RawMessage(`{}`)))
	if got := awaitResult(t, done); got.err != nil {
		t.Fatalf("Request() after stale Unregister failed: %v", got.err)
	}

	r.Unregister("node-1", current)
	if _, ok := r.Get("node-1"); ok {
		t.Error("Get() = true after Unregister with the current connection, want false")
	}
}

// frameWithResult 构造一个成功的响应帧。
func frameWithResult(id string, result json.RawMessage) protocol.Frame {
	return protocol.Frame{Type: protocol.FrameResponse, ID: id, Result: result}
}

func TestRegistryRequestUnknownNodeReturnsErrNodeOffline(t *testing.T) {
	r := NewRegistry()

	_, err := r.Request(context.Background(), "node-missing", "opencode.status", json.RawMessage(`{"x":1}`))
	if !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("Request() error = %v, want ErrNodeOffline", err)
	}
}

func TestRegistryListReturnsSortedCopy(t *testing.T) {
	r := NewRegistry()

	if list := r.List(); list == nil || len(list) != 0 {
		t.Fatalf("List() on an empty registry = %v, want an empty non-nil slice", list)
	}

	registerTestNode(t, r, "node-b")
	registerTestNode(t, r, "node-a")
	registerTestNode(t, r, "node-c")

	list := r.List()
	ids := make([]string, 0, len(list))
	for _, info := range list {
		ids = append(ids, info.ID)
	}
	if want := []string{"node-a", "node-b", "node-c"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("List() IDs = %v, want %v", ids, want)
	}

	// 多次调用结果一致。
	if again := r.List(); !reflect.DeepEqual(again, list) {
		t.Errorf("List() is not stable across calls: %v vs %v", again, list)
	}

	list[0].Name = "hacked"
	list[0].Capabilities[0] = "hacked"
	list[0].Capabilities = append(list[0].Capabilities, "extra")

	again := r.List()
	if again[0].Name != "box-node-a" {
		t.Errorf("List()[0].Name = %q after caller mutation, want %q", again[0].Name, "box-node-a")
	}
	if !reflect.DeepEqual(again[0].Capabilities, []string{"session.list"}) {
		t.Errorf("List()[0].Capabilities = %v after caller mutation, want [session.list]", again[0].Capabilities)
	}
}

func TestRegistryCloseAll(t *testing.T) {
	r := NewRegistry()

	first, firstWriter := registerTestNode(t, r, "node-a")
	_, secondWriter := registerTestNode(t, r, "node-b")

	r.CloseAll()

	for name, writer := range map[string]*fakeWriter{"node-a": firstWriter, "node-b": secondWriter} {
		if got := writer.closeCount.Load(); got != 1 {
			t.Errorf("%s writer closed %d times, want exactly 1", name, got)
		}
	}

	if list := r.List(); len(list) != 0 {
		t.Errorf("List() after CloseAll = %v, want empty", list)
	}
	if _, ok := r.Get("node-a"); ok {
		t.Error("Get() = true after CloseAll, want false")
	}
	if _, err := first.Request(context.Background(), "session.list", nil); !errors.Is(err, ErrConnectionClosed) {
		t.Errorf("Request() after CloseAll error = %v, want ErrConnectionClosed", err)
	}

	// 重复调用必须安全，且不会再次关闭写入端。
	r.CloseAll()
	if got := firstWriter.closeCount.Load(); got != 1 {
		t.Errorf("writer closed %d times after a second CloseAll, want exactly 1", got)
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	ids := []string{"node-a", "node-b", "node-c"}

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			id := ids[i%len(ids)]
			conn := NewConnection(id, newFakeWriter())
			r.Register(Info{
				ID:           id,
				Name:         fmt.Sprintf("box-%d", i),
				Capabilities: []string{"session.list"},
			}, conn)

			_, _ = r.Get(id)
			_ = r.List()
			_, _ = r.Request(context.Background(), "node-missing", "opencode.status", nil)
			r.Unregister(id, conn)
		}(i)
	}
	wg.Wait()

	if list := r.List(); len(list) != 0 {
		t.Errorf("List() after all connections unregistered = %v, want empty", list)
	}
}
