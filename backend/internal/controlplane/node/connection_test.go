package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insmtx/Lework/backend/internal/agentd/protocol"
)

// waitTimeout 限制测试中等待异步结果的时长，避免并发实现出错时挂死整个测试进程。
const waitTimeout = 2 * time.Second

// fakeWriter 记录 Connection 写出的帧，并可以把写入端标记为已关闭。
type fakeWriter struct {
	frames chan protocol.Frame
	closed chan struct{}

	closeOnce  sync.Once
	closeCount atomic.Int32

	mu       sync.Mutex
	writeErr error
}

func newFakeWriter() *fakeWriter {
	return &fakeWriter{
		frames: make(chan protocol.Frame, 16),
		closed: make(chan struct{}),
	}
}

func (w *fakeWriter) Write(ctx context.Context, f protocol.Frame) error {
	w.mu.Lock()
	err := w.writeErr
	w.mu.Unlock()
	if err != nil {
		return err
	}

	select {
	case <-w.closed:
		return errors.New("fake writer is closed")
	default:
	}

	select {
	case w.frames <- f:
		return nil
	case <-w.closed:
		return errors.New("fake writer is closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *fakeWriter) Close() error {
	w.closeCount.Add(1)
	w.closeOnce.Do(func() { close(w.closed) })
	return nil
}

// setWriteErr 让后续写入失败，用于验证写入错误不会带出请求参数。
func (w *fakeWriter) setWriteErr(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeErr = err
}

// nextFrame 取出 Connection 写出的下一帧，超时即判定请求没有发出。
func (w *fakeWriter) nextFrame(t *testing.T) protocol.Frame {
	t.Helper()

	select {
	case f := <-w.frames:
		return f
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for a written frame")
		return protocol.Frame{}
	}
}

// expectNoFrame 断言写入端在短时间内没有再收到帧。
func (w *fakeWriter) expectNoFrame(t *testing.T) {
	t.Helper()

	select {
	case f := <-w.frames:
		t.Fatalf("unexpected frame written: type=%q method=%q", f.Type, f.Method)
	case <-time.After(50 * time.Millisecond):
	}
}

type requestResult struct {
	result json.RawMessage
	err    error
}

// startRequest 在后台发起请求，返回结果通道。
func startRequest(t *testing.T, conn *Connection, ctx context.Context, method string, params json.RawMessage) <-chan requestResult {
	t.Helper()

	done := make(chan requestResult, 1)
	go func() {
		result, err := conn.Request(ctx, method, params)
		done <- requestResult{result: result, err: err}
	}()
	return done
}

// awaitResult 等待后台请求结束，超时即判定请求没有按预期返回。
func awaitResult(t *testing.T, done <-chan requestResult) requestResult {
	t.Helper()

	select {
	case got := <-done:
		return got
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for request result")
		return requestResult{}
	}
}

func TestConnectionRequestRoundTrip(t *testing.T) {
	writer := newFakeWriter()
	conn := NewConnection("node-1", writer)
	params := json.RawMessage(`{"directory":"/work","limit":3}`)

	done := startRequest(t, conn, context.Background(), "session.list", params)

	frame := writer.nextFrame(t)
	if frame.Type != protocol.FrameRequest {
		t.Errorf("frame.Type = %q, want %q", frame.Type, protocol.FrameRequest)
	}
	if frame.ID == "" {
		t.Error("frame.ID is empty, want a generated request ID")
	}
	if !strings.HasPrefix(frame.ID, "req-") {
		t.Errorf("frame.ID = %q, want prefix %q", frame.ID, "req-")
	}
	if frame.Method != "session.list" {
		t.Errorf("frame.Method = %q, want %q", frame.Method, "session.list")
	}
	if !bytes.Equal(frame.Params, params) {
		t.Errorf("frame.Params = %s, want %s", frame.Params, params)
	}

	want := json.RawMessage(`{"sessions":[]}`)
	if ok := conn.Deliver(protocol.Frame{Type: protocol.FrameResponse, ID: frame.ID, Result: want}); !ok {
		t.Fatal("Deliver() = false, want true for a pending request")
	}

	got := awaitResult(t, done)
	if got.err != nil {
		t.Fatalf("Request() failed: %v", got.err)
	}
	if !bytes.Equal(got.result, want) {
		t.Errorf("result = %s, want %s", got.result, want)
	}
}

func TestConnectionRequestRemoteError(t *testing.T) {
	writer := newFakeWriter()
	conn := NewConnection("node-1", writer)

	done := startRequest(t, conn, context.Background(), "opencode.status", nil)
	frame := writer.nextFrame(t)

	ok := conn.Deliver(protocol.Frame{
		Type: protocol.FrameResponse,
		ID:   frame.ID,
		Error: &protocol.FrameError{
			Code:    "agent_error",
			Message: "opencode is not running",
		},
	})
	if !ok {
		t.Fatal("Deliver() = false, want true for a pending request")
	}

	got := awaitResult(t, done)
	if got.err == nil {
		t.Fatal("Request() error = nil, want a remote error")
	}

	var remote *RemoteError
	if !errors.As(got.err, &remote) {
		t.Fatalf("Request() error = %v, want *RemoteError", got.err)
	}
	if remote.Code != "agent_error" {
		t.Errorf("RemoteError.Code = %q, want %q", remote.Code, "agent_error")
	}
	if remote.Message != "opencode is not running" {
		t.Errorf("RemoteError.Message = %q, want %q", remote.Message, "opencode is not running")
	}
	if !strings.Contains(got.err.Error(), "agent_error") {
		t.Errorf("error %q does not contain the code", got.err.Error())
	}
	if !strings.Contains(got.err.Error(), "opencode is not running") {
		t.Errorf("error %q does not contain the message", got.err.Error())
	}
}

func TestConnectionRequestContextCanceled(t *testing.T) {
	writer := newFakeWriter()
	conn := NewConnection("node-1", writer)

	ctx, cancel := context.WithCancel(context.Background())
	done := startRequest(t, conn, ctx, "session.list", nil)
	frame := writer.nextFrame(t)

	cancel()

	got := awaitResult(t, done)
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("Request() error = %v, want context.Canceled", got.err)
	}

	// 取消后待处理项必须被清理，迟到的响应不能再被投递。
	if ok := conn.Deliver(protocol.Frame{Type: protocol.FrameResponse, ID: frame.ID, Result: json.RawMessage(`{}`)}); ok {
		t.Error("Deliver() = true after the request was canceled, want false")
	}
}

func TestConnectionFailUnblocksRequest(t *testing.T) {
	writer := newFakeWriter()
	conn := NewConnection("node-1", writer)

	done := startRequest(t, conn, context.Background(), "session.list", nil)
	frame := writer.nextFrame(t)

	// 传输层原因不应泄漏给等待方：所有未完成请求统一以连接关闭告终。
	conn.Fail(errors.New("websocket read failed"))

	got := awaitResult(t, done)
	if !errors.Is(got.err, ErrConnectionClosed) {
		t.Fatalf("Request() error = %v, want ErrConnectionClosed", got.err)
	}

	if ok := conn.Deliver(protocol.Frame{Type: protocol.FrameResponse, ID: frame.ID, Result: json.RawMessage(`{}`)}); ok {
		t.Error("Deliver() = true after Fail, want false")
	}

	if _, err := conn.Request(context.Background(), "session.list", nil); !errors.Is(err, ErrConnectionClosed) {
		t.Errorf("Request() after Fail error = %v, want ErrConnectionClosed", err)
	}
}

func TestConnectionCloseIsIdempotent(t *testing.T) {
	writer := newFakeWriter()
	conn := NewConnection("node-1", writer)

	if err := conn.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("second Close() failed: %v", err)
	}

	if got := writer.closeCount.Load(); got != 1 {
		t.Errorf("writer closed %d times, want exactly 1", got)
	}
	select {
	case <-writer.closed:
	case <-time.After(waitTimeout):
		t.Fatal("writer was not closed")
	}

	if _, err := conn.Request(context.Background(), "session.list", nil); !errors.Is(err, ErrConnectionClosed) {
		t.Errorf("Request() after Close error = %v, want ErrConnectionClosed", err)
	}
	writer.expectNoFrame(t)
}

func TestConnectionDeliverUnknownID(t *testing.T) {
	writer := newFakeWriter()
	conn := NewConnection("node-1", writer)

	if ok := conn.Deliver(protocol.Frame{Type: protocol.FrameResponse, ID: "req-missing", Result: json.RawMessage(`{}`)}); ok {
		t.Error("Deliver() = true for an unknown ID, want false")
	}
	if ok := conn.Deliver(protocol.Frame{Type: protocol.FrameResponse, Result: json.RawMessage(`{}`)}); ok {
		t.Error("Deliver() = true for an empty ID, want false")
	}
}

func TestConnectionDeliverDuplicateResponse(t *testing.T) {
	writer := newFakeWriter()
	conn := NewConnection("node-1", writer)

	done := startRequest(t, conn, context.Background(), "session.list", nil)
	frame := writer.nextFrame(t)

	response := protocol.Frame{Type: protocol.FrameResponse, ID: frame.ID, Result: json.RawMessage(`{}`)}
	if ok := conn.Deliver(response); !ok {
		t.Fatal("first Deliver() = false, want true")
	}
	if ok := conn.Deliver(response); ok {
		t.Error("second Deliver() = true, want false for a duplicate response")
	}
	if got := awaitResult(t, done); got.err != nil {
		t.Fatalf("Request() failed: %v", got.err)
	}
}

func TestConnectionRequestWriteErrorOmitsParams(t *testing.T) {
	writer := newFakeWriter()
	writeErr := errors.New("websocket write failed")
	writer.setWriteErr(writeErr)
	conn := NewConnection("node-1", writer)

	_, err := conn.Request(context.Background(), "session.prompt", json.RawMessage(`{"text":"secret-prompt"}`))
	if !errors.Is(err, writeErr) {
		t.Fatalf("Request() error = %v, want wrapped write error", err)
	}
	if strings.Contains(err.Error(), "secret-prompt") {
		t.Errorf("Request() error %q leaks request params", err.Error())
	}
}

func TestConnectionNodeID(t *testing.T) {
	conn := NewConnection("node-42", newFakeWriter())

	if got := conn.NodeID(); got != "node-42" {
		t.Errorf("NodeID() = %q, want %q", got, "node-42")
	}
}
