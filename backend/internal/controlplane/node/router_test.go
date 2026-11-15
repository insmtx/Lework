package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/insmtx/lework/backend/internal/infra/mq"
)

// forwardCall 记录一次跨实例转发请求，便于断言 subject 与负载。
type forwardCall struct {
	subject string
	request []byte
}

// fakeForwarder 是测试用的 Core NATS 请求端。
//
// 未设置 handler 时返回 ErrNoResponders，模拟 subject 上没有应答者；
// 设置 handler 后由测试决定回复或错误。
type fakeForwarder struct {
	mu      sync.Mutex
	calls   []forwardCall
	handler func(subject string, request []byte) ([]byte, error)
}

func (f *fakeForwarder) RequestBytes(_ context.Context, subject string, request []byte) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, forwardCall{subject: subject, request: append([]byte(nil), request...)})
	handler := f.handler
	f.mu.Unlock()

	if handler == nil {
		return nil, fmt.Errorf("fake forwarder: no responders: %w", mq.ErrNoResponders)
	}
	return handler(subject, request)
}

func (f *fakeForwarder) setHandler(handler func(subject string, request []byte) ([]byte, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler = handler
}

func (f *fakeForwarder) callsSnapshot() []forwardCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]forwardCall(nil), f.calls...)
}

// TestRouterUsesLocalConnection 固化控制器裁决：本实例持有连接时直接调用，
// 不产生任何跨实例流量。
func TestRouterUsesLocalConnection(t *testing.T) {
	registry := NewRegistry()
	conn, writer := registerTestNode(t, registry, "node-1")
	forwarder := &fakeForwarder{}

	router := NewRouter(registry, forwarder)
	done := make(chan requestResult, 1)
	go func() {
		result, err := router.Request(context.Background(), "node-1", "session.list",
			json.RawMessage(`{"directory":"/work"}`))
		done <- requestResult{result: result, err: err}
	}()

	frame := writer.nextFrame(t)
	if frame.Method != "session.list" {
		t.Fatalf("method = %q, want session.list", frame.Method)
	}
	want := json.RawMessage(`{"sessions":[]}`)
	if ok := conn.Deliver(frameWithResult(frame.ID, want)); !ok {
		t.Fatal("Deliver() on the local connection = false, want true")
	}

	got := awaitResult(t, done)
	if got.err != nil {
		t.Fatalf("Request() failed: %v", got.err)
	}
	if string(got.result) != string(want) {
		t.Fatalf("result = %s, want %s", got.result, want)
	}
	if calls := forwarder.callsSnapshot(); len(calls) != 0 {
		t.Fatalf("forwarder called %d times for a local connection, want 0", len(calls))
	}
}

// TestRouterFallsBackToForwarder 确认没有本地连接时把请求发到
// cp.node.<id>.request，并原样返回应答里的 Result。
func TestRouterFallsBackToForwarder(t *testing.T) {
	registry := NewRegistry()
	forwarder := &fakeForwarder{}
	want := json.RawMessage(`{"state":"running"}`)
	forwarder.setHandler(func(_ string, _ []byte) ([]byte, error) {
		return json.Marshal(ForwardResponse{Result: want})
	})

	router := NewRouter(registry, forwarder)
	params := json.RawMessage(`{"directory":"/work"}`)
	result, err := router.Request(context.Background(), "node-1", "opencode.status", params)
	if err != nil {
		t.Fatalf("Request() failed: %v", err)
	}
	if string(result) != string(want) {
		t.Fatalf("result = %s, want %s", result, want)
	}

	calls := forwarder.callsSnapshot()
	if len(calls) != 1 {
		t.Fatalf("forwarder called %d times, want 1", len(calls))
	}
	if calls[0].subject != "cp.node.node-1.request" {
		t.Fatalf("subject = %q, want cp.node.node-1.request", calls[0].subject)
	}
	var forwarded ForwardRequest
	if err := json.Unmarshal(calls[0].request, &forwarded); err != nil {
		t.Fatalf("decode forwarded request %q: %v", string(calls[0].request), err)
	}
	if forwarded.Method != "opencode.status" {
		t.Fatalf("forwarded method = %q, want opencode.status", forwarded.Method)
	}
	if string(forwarded.Params) != string(params) {
		t.Fatalf("forwarded params = %s, want %s", forwarded.Params, params)
	}
}

// TestRouterMapsNoRespondersToOffline 固化「没有实例持有该节点」等于节点离线。
func TestRouterMapsNoRespondersToOffline(t *testing.T) {
	for name, router := range map[string]*Router{
		"no responders": NewRouter(NewRegistry(), &fakeForwarder{}),
		"no forwarder":  NewRouter(NewRegistry(), nil),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := router.Request(context.Background(), "node-1", "opencode.status", nil)
			if !errors.Is(err, ErrNodeOffline) {
				t.Fatalf("Request() error = %v, want ErrNodeOffline", err)
			}
		})
	}
}

// TestRouterKeepsDeadlineError 确认超时不被误报成节点离线。
func TestRouterKeepsDeadlineError(t *testing.T) {
	forwarder := &fakeForwarder{}
	forwarder.setHandler(func(_ string, _ []byte) ([]byte, error) {
		return nil, context.DeadlineExceeded
	})

	router := NewRouter(NewRegistry(), forwarder)
	_, err := router.Request(context.Background(), "node-1", "opencode.status", nil)
	if errors.Is(err, ErrNodeOffline) {
		t.Fatalf("Request() error = %v, want a deadline error rather than ErrNodeOffline", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Request() error = %v, want errors.Is(context.DeadlineExceeded)", err)
	}
}

// TestRouterMapsForwardResponseErrorCodes 证明持有连接的实例把“节点已断开”
// 与“agent 超时”编码进 ForwardError 后，路由层会还原成相同的错误语义，
// 而不是把它们当成普通远端错误透传。
func TestRouterMapsForwardResponseErrorCodes(t *testing.T) {
	tests := map[string]struct {
		code    string
		wantIs  error
		wantOff bool
	}{
		"node offline":  {code: "node_offline", wantIs: ErrNodeOffline, wantOff: true},
		"agent timeout": {code: "agent_timeout", wantIs: context.DeadlineExceeded},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			forwarder := &fakeForwarder{}
			forwarder.setHandler(func(_ string, _ []byte) ([]byte, error) {
				return json.Marshal(ForwardResponse{
					Error: &ForwardError{Code: tt.code, Message: "from the owner instance"},
				})
			})

			_, err := NewRouter(NewRegistry(), forwarder).
				Request(context.Background(), "node-1", "opencode.status", nil)
			if !errors.Is(err, tt.wantIs) {
				t.Fatalf("Request() error = %v, want errors.Is(%v)", err, tt.wantIs)
			}
			if got := errors.Is(err, ErrNodeOffline); got != tt.wantOff {
				t.Fatalf("errors.Is(err, ErrNodeOffline) = %v, want %v", got, tt.wantOff)
			}
		})
	}
}

// TestRouterPassesThroughOtherForwardErrorCodes 证明未知错误码仍作为远端
// 业务错误返回，调用方可以按 Code 分类而不必依赖固定的错误清单。
func TestRouterPassesThroughOtherForwardErrorCodes(t *testing.T) {
	forwarder := &fakeForwarder{}
	forwarder.setHandler(func(_ string, _ []byte) ([]byte, error) {
		return json.Marshal(ForwardResponse{
			Error: &ForwardError{Code: "not_found", Message: "no such session"},
		})
	})

	_, err := NewRouter(NewRegistry(), forwarder).
		Request(context.Background(), "node-1", "session.diff", nil)
	var remote *RemoteError
	if !errors.As(err, &remote) {
		t.Fatalf("Request() error = %v, want *RemoteError", err)
	}
	if remote.Code != "not_found" || remote.Message != "no such session" {
		t.Fatalf("remote error = %+v, want the forwarded code and message", remote)
	}
}
