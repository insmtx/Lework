package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/agentd/handler"
	"github.com/insmtx/lework/backend/internal/agentd/opencode"
	"github.com/insmtx/lework/backend/internal/agentd/protocol"
	"github.com/insmtx/lework/backend/internal/agentd/supervisor"
)

// fakeTransport is a two-channel transport: reads carries frames from the fake
// control plane into the runner, writes carries frames the runner emits.
// Buffered channels keep the test from deadlocking while Run is starting up.
type fakeTransport struct {
	reads  chan protocol.Frame
	writes chan protocol.Frame

	closeCalls atomic.Int32
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{
		reads:  make(chan protocol.Frame, 8),
		writes: make(chan protocol.Frame, 64),
	}
}

func (f *fakeTransport) Write(ctx context.Context, frame protocol.Frame) error {
	select {
	case f.writes <- frame:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakeTransport) Read(ctx context.Context) (protocol.Frame, error) {
	select {
	case frame, ok := <-f.reads:
		if !ok {
			return protocol.Frame{}, io.EOF
		}
		return frame, nil
	case <-ctx.Done():
		return protocol.Frame{}, ctx.Err()
	}
}

func (f *fakeTransport) Close() error {
	f.closeCalls.Add(1)
	return nil
}

// fakeEvents hands the runner a caller-owned channel so the test controls when
// events arrive and when the stream ends.
type fakeEvents struct {
	events chan opencode.Event
	err    error
	calls  atomic.Int32
}

func (f *fakeEvents) SubscribeEvents(context.Context) (<-chan opencode.Event, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.events, nil
}

// stubOpenCode and stubSupervisor satisfy handler's dependencies. Only node.info
// is exercised by the runner tests, so the methods stay inert.
type stubOpenCode struct{}

func (stubOpenCode) ListSessions(context.Context, string) ([]opencode.Session, error) {
	return nil, nil
}

func (stubOpenCode) ListChildSessions(context.Context, string, string) ([]opencode.Session, error) {
	return nil, nil
}

func (stubOpenCode) ListProjects(context.Context) ([]opencode.Project, error) {
	return nil, nil
}

func (stubOpenCode) CreateSession(context.Context, string) (opencode.Session, error) {
	return opencode.Session{}, nil
}

func (stubOpenCode) RenameSession(context.Context, string, string) error { return nil }

func (stubOpenCode) DeleteSession(context.Context, string) error { return nil }

func (stubOpenCode) Prompt(context.Context, string, string) (string, error) { return "", nil }

func (stubOpenCode) Messages(context.Context, string, int, string) (opencode.MessagePage, error) {
	return opencode.MessagePage{}, nil
}

func (stubOpenCode) ReplyPermission(context.Context, string, string, string, string) error {
	return nil
}

func (stubOpenCode) ListPermissions(context.Context) ([]opencode.PermissionRequest, error) {
	return nil, nil
}

func (stubOpenCode) SessionDiff(context.Context, string, string, string, int) ([]opencode.FileDiff, error) {
	return nil, nil
}

func (stubOpenCode) Interrupt(context.Context, string, bool) (opencode.InterruptResult, error) {
	return opencode.InterruptResult{}, nil
}

func (stubOpenCode) ListFiles(context.Context, string, string) ([]opencode.FileInfo, error) {
	return nil, nil
}

func (stubOpenCode) ReadFile(context.Context, string, string, int64) (opencode.FileContent, error) {
	return opencode.FileContent{}, nil
}

func (stubOpenCode) CreatePersistentTerminal(context.Context, string, opencode.CreateTerminalRequest) (opencode.PersistentTerminal, error) {
	return opencode.PersistentTerminal{}, nil
}

func (stubOpenCode) ConnectPersistentTerminal(context.Context, string) (*opencode.TerminalConn, error) {
	return nil, nil
}

func (stubOpenCode) ResizePersistentTerminal(context.Context, string, opencode.TerminalSize) error {
	return nil
}

func (stubOpenCode) ClosePersistentTerminal(context.Context, string) error {
	return nil
}

// fakeRequestHandler is a deterministic RequestHandler for PTY forwarding and
// shutdown tests. The production handler is exercised separately.
//
// results, when non-nil, supplies one result per Handle call so tests can drive
// response shapes without touching shared state from another goroutine.
type fakeRequestHandler struct {
	events  chan protocol.Event
	closed  atomic.Bool
	results chan any
}

func (f *fakeRequestHandler) Handle(context.Context, string, json.RawMessage) (any, error) {
	if f.results == nil {
		return nil, nil
	}
	return <-f.results, nil
}

func (f *fakeRequestHandler) PTYEvents() <-chan protocol.Event { return f.events }

func (f *fakeRequestHandler) ClosePTYs() { f.closed.Store(true) }

type stubSupervisor struct{}

func (stubSupervisor) Ensure(context.Context) (supervisor.Status, error) {
	return supervisor.Status{}, nil
}

func (stubSupervisor) Restart(context.Context) error { return nil }
func (stubSupervisor) Stop(context.Context) error    { return nil }

func (stubSupervisor) Status() supervisor.Status { return supervisor.Status{} }

func testRunner(t *testing.T, heartbeat time.Duration) (*Runner, *fakeTransport, *fakeEvents) {
	t.Helper()

	transport := newFakeTransport()
	events := &fakeEvents{events: make(chan opencode.Event, 8)}
	r := New(
		transport,
		handler.New(stubOpenCode{}, stubSupervisor{}),
		events,
		RunnerConfig{NodeID: "node-1", AgentVersion: "0.1.0-test", Heartbeat: heartbeat},
	)
	return r, transport, events
}

// runAsync starts Run and returns its result channel. Cancellation is wired into
// cleanup so a failing assertion cannot leave the goroutine behind.
func runAsync(t *testing.T, r *Runner) (context.CancelFunc, <-chan error) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	t.Cleanup(cancel)
	return cancel, done
}

func waitRun(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func readFrame(t *testing.T, transport *fakeTransport) protocol.Frame {
	t.Helper()

	select {
	case frame := <-transport.writes:
		return frame
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an outbound frame")
		return protocol.Frame{}
	}
}

func awaitHello(t *testing.T, transport *fakeTransport) protocol.Frame {
	t.Helper()

	hello := readFrame(t, transport)
	if hello.Type != protocol.FrameHello {
		t.Fatalf("first frame type = %q, want hello", hello.Type)
	}
	return hello
}

func completeHandshake(t *testing.T, transport *fakeTransport) protocol.Frame {
	t.Helper()

	hello := awaitHello(t, transport)
	transport.reads <- protocol.Frame{Type: protocol.FrameWelcome, ID: hello.ID}
	return hello
}

func TestRunnerSendsHelloAndHandlesRequest(t *testing.T) {
	r, transport, _ := testRunner(t, 0)
	cancel, done := runAsync(t, r)

	hello := awaitHello(t, transport)
	if !strings.HasPrefix(hello.ID, "hello-") {
		t.Fatalf("hello id = %q, want hello- prefix", hello.ID)
	}

	var params struct {
		AgentVersion string   `json:"agent_version"`
		OS           string   `json:"os"`
		Arch         string   `json:"arch"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(hello.Params, &params); err != nil {
		t.Fatalf("decode hello params: %v", err)
	}
	if params.AgentVersion != "0.1.0-test" {
		t.Fatalf("agent_version = %q, want 0.1.0-test", params.AgentVersion)
	}
	if params.OS != runtime.GOOS || params.Arch != runtime.GOARCH {
		t.Fatalf("os/arch = %q/%q, want %q/%q", params.OS, params.Arch, runtime.GOOS, runtime.GOARCH)
	}
	if len(params.Capabilities) == 0 {
		t.Fatal("capabilities are empty")
	}

	transport.reads <- protocol.Frame{Type: protocol.FrameWelcome, ID: hello.ID}
	transport.reads <- protocol.Frame{Type: protocol.FrameRequest, ID: "r1", Method: "node.info"}

	response := readFrame(t, transport)
	if response.Type != protocol.FrameResponse {
		t.Fatalf("frame type = %q, want response", response.Type)
	}
	if response.ID != "r1" {
		t.Fatalf("response id = %q, want r1", response.ID)
	}
	if response.Error != nil {
		t.Fatalf("response error = %+v, want nil", response.Error)
	}
	var info handler.NodeInfo
	if err := json.Unmarshal(response.Result, &info); err != nil {
		t.Fatalf("decode response result: %v", err)
	}
	if info.NodeName == "" || info.AgentVersion == "" {
		t.Fatalf("node.info result = %+v, want populated fields", info)
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run() = %v, want nil after cancel", err)
	}
}

func TestRunnerUnknownMethodReturnsErrorResponseAndKeepsConnection(t *testing.T) {
	r, transport, _ := testRunner(t, 0)
	cancel, done := runAsync(t, r)

	completeHandshake(t, transport)
	transport.reads <- protocol.Frame{Type: protocol.FrameRequest, ID: "r2", Method: "does.not.exist"}

	response := readFrame(t, transport)
	if response.Type != protocol.FrameResponse || response.ID != "r2" {
		t.Fatalf("frame = %+v, want response r2", response)
	}
	if response.Error == nil || response.Error.Code != "method_not_found" {
		t.Fatalf("error = %+v, want code method_not_found", response.Error)
	}

	// The connection must stay usable after an unknown method.
	transport.reads <- protocol.Frame{Type: protocol.FrameRequest, ID: "r3", Method: "node.info"}
	next := readFrame(t, transport)
	if next.ID != "r3" || next.Error != nil {
		t.Fatalf("follow-up response = %+v, want successful r3", next)
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run() = %v, want nil after cancel", err)
	}
}

// TestRunnerBoundsResponseFrames 固化 2026-11-15 的 node_offline 回归：
// 200 条消息页实测约 1.34 MiB，超过旧的 1 MiB 帧上限后连接被关闭，调用方
// 收到误导性的 node_offline。合法响应必须完整发回；真正超过协议上限的响应
// 只能降级为明确错误，且不能连坐断开连接。
func TestRunnerBoundsResponseFrames(t *testing.T) {
	transport := newFakeTransport()
	requestHandler := &fakeRequestHandler{
		events:  make(chan protocol.Event, 4),
		results: make(chan any, 4),
	}
	r := New(transport, requestHandler, nil, RunnerConfig{
		NodeID:       "node-1",
		AgentVersion: "0.1.0-test",
	})
	cancel, done := runAsync(t, r)
	completeHandshake(t, transport)

	// 1.3 MiB 的页面代表长会话：必须原样返回，不能被上限误伤。
	page := strings.Repeat("a", 13<<17)
	requestHandler.results <- page
	transport.reads <- protocol.Frame{Type: protocol.FrameRequest, ID: "r1", Method: "session.messages"}

	response := readFrame(t, transport)
	if response.ID != "r1" || response.Error != nil {
		t.Fatalf("response = %+v, want the large result", response)
	}
	var got string
	if err := json.Unmarshal(response.Result, &got); err != nil {
		t.Fatalf("decode large result: %v", err)
	}
	if len(got) != len(page) {
		t.Fatalf("result bytes = %d, want %d", len(got), len(page))
	}

	// HTML 转义会把每个 < 放大到 6 字节，最终帧远超上限，必须变成错误帧。
	oversized := strings.Repeat("<", protocol.MaxFrameBytes/4)
	requestHandler.results <- oversized
	transport.reads <- protocol.Frame{Type: protocol.FrameRequest, ID: "r2", Method: "session.messages"}

	response = readFrame(t, transport)
	if response.Error == nil || response.Error.Code != "response_too_large" {
		t.Fatalf("error = %+v, want code response_too_large", response.Error)
	}

	// 连接必须保持可用：后续请求仍能得到正常响应，而不是被断开重连。
	requestHandler.results <- "ok"
	transport.reads <- protocol.Frame{Type: protocol.FrameRequest, ID: "r3", Method: "session.messages"}
	response = readFrame(t, transport)
	if response.ID != "r3" || response.Error != nil {
		t.Fatalf("follow-up response = %+v, want successful r3", response)
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run() = %v, want nil after cancel", err)
	}
}

func TestRunnerForwardsEvents(t *testing.T) {
	r, transport, events := testRunner(t, 0)
	cancel, done := runAsync(t, r)

	completeHandshake(t, transport)
	events.events <- opencode.Event{
		Type:      "session.text.delta",
		SessionID: "ses_9",
		Data:      json.RawMessage(`{"text":"hi"}`),
	}

	frame := readFrame(t, transport)
	if frame.Type != protocol.FrameEvent {
		t.Fatalf("frame type = %q, want event", frame.Type)
	}
	if frame.Event == nil {
		t.Fatal("event frame has no event payload")
	}
	if frame.Event.Type != "session.text.delta" {
		t.Fatalf("event type = %q, want session.text.delta", frame.Event.Type)
	}
	if frame.Event.NodeID != "node-1" {
		t.Fatalf("node_id = %q, want node-1", frame.Event.NodeID)
	}
	if frame.Event.SessionID != "ses_9" {
		t.Fatalf("session_id = %q, want ses_9", frame.Event.SessionID)
	}
	if frame.Event.Seq != 0 {
		t.Fatalf("seq = %d, want 0 for I0", frame.Event.Seq)
	}
	if string(frame.Event.Data) != `{"text":"hi"}` {
		t.Fatalf("event data = %s, want payload", frame.Event.Data)
	}
	if frame.Event.Time.IsZero() {
		t.Fatal("event time is zero")
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run() = %v, want nil after cancel", err)
	}
}

func TestRunnerRequestsResyncWhenEventStreamCloses(t *testing.T) {
	r, transport, events := testRunner(t, 0)
	cancel, done := runAsync(t, r)

	completeHandshake(t, transport)
	close(events.events)

	frame := readFrame(t, transport)
	if frame.Type != protocol.FrameEvent || frame.Event == nil {
		t.Fatalf("frame = %+v, want event frame", frame)
	}
	if frame.Event.Type != "agent.resync_required" {
		t.Fatalf("event type = %q, want agent.resync_required", frame.Event.Type)
	}

	err := waitRun(t, done)
	if err == nil || !strings.Contains(err.Error(), "event stream closed") {
		t.Fatalf("Run() = %v, want wrapped event stream closed error", err)
	}
	cancel()
}

func TestRunnerHeartbeat(t *testing.T) {
	r, transport, _ := testRunner(t, 5*time.Millisecond)
	cancel, done := runAsync(t, r)

	completeHandshake(t, transport)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case frame := <-transport.writes:
			if frame.Type == protocol.FrameHeartbeat {
				cancel()
				if err := waitRun(t, done); err != nil {
					t.Fatalf("Run() = %v, want nil after cancel", err)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for a heartbeat")
		}
	}
}

func TestRunnerStopsOnContextCancelDuringWelcome(t *testing.T) {
	r, transport, _ := testRunner(t, 0)
	cancel, done := runAsync(t, r)

	awaitHello(t, transport)
	cancel()

	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run() = %v, want nil after cancel", err)
	}
}

func TestRunnerReturnsNilWhenContextAlreadyCanceled(t *testing.T) {
	r, transport, _ := testRunner(t, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := r.Run(ctx); err != nil {
		t.Fatalf("Run() = %v, want nil for a canceled context", err)
	}
	select {
	case frame := <-transport.writes:
		t.Fatalf("wrote %+v, want no frames for a canceled context", frame)
	default:
	}
}

func TestRunnerReturnsWrappedReadError(t *testing.T) {
	r, transport, _ := testRunner(t, 0)
	cancel, done := runAsync(t, r)

	completeHandshake(t, transport)
	close(transport.reads)

	err := waitRun(t, done)
	if err == nil || !strings.Contains(err.Error(), "read frame") {
		t.Fatalf("Run() = %v, want wrapped read error", err)
	}
	cancel()
}

func TestRunnerRejectsInvalidFrameBeforeWelcome(t *testing.T) {
	r, transport, _ := testRunner(t, 0)
	cancel, done := runAsync(t, r)

	awaitHello(t, transport)
	// A request frame without id and method is malformed; the handshake must
	// fail instead of ignoring it and waiting for welcome forever.
	transport.reads <- protocol.Frame{Type: protocol.FrameRequest}

	err := waitRun(t, done)
	if err == nil || !strings.Contains(err.Error(), "invalid frame") {
		t.Fatalf("Run() = %v, want invalid frame error", err)
	}
	cancel()
}

func TestRunnerRejectsInvalidFrameAfterWelcome(t *testing.T) {
	r, transport, _ := testRunner(t, 0)
	cancel, done := runAsync(t, r)

	completeHandshake(t, transport)
	transport.reads <- protocol.Frame{Type: protocol.FrameRequest}

	err := waitRun(t, done)
	if err == nil || !strings.Contains(err.Error(), "invalid frame") {
		t.Fatalf("Run() = %v, want invalid frame error", err)
	}
	select {
	case frame := <-transport.writes:
		t.Fatalf("wrote %+v for an invalid inbound frame, want no response", frame)
	default:
	}
	cancel()
}

func TestWriteRejectsInvalidFrame(t *testing.T) {
	r, transport, _ := testRunner(t, 0)

	err := r.write(context.Background(), protocol.Frame{Type: protocol.FrameRequest})
	if err == nil || !strings.Contains(err.Error(), "invalid frame") {
		t.Fatalf("write() = %v, want invalid frame error", err)
	}
	select {
	case frame := <-transport.writes:
		t.Fatalf("transport received %+v, want no write", frame)
	default:
	}
}

func TestRunnerReturnsSubscribeError(t *testing.T) {
	r, transport, events := testRunner(t, 0)
	events.err = errors.New("event endpoint unavailable")
	cancel, done := runAsync(t, r)

	completeHandshake(t, transport)

	err := waitRun(t, done)
	if err == nil || !strings.Contains(err.Error(), "subscribe opencode events") {
		t.Fatalf("Run() = %v, want wrapped subscribe error", err)
	}
	cancel()
}

func TestRunnerReturnsInternalErrorForHandlerFailure(t *testing.T) {
	r, transport, _ := testRunner(t, 0)
	cancel, done := runAsync(t, r)

	completeHandshake(t, transport)
	transport.reads <- protocol.Frame{
		Type:   protocol.FrameRequest,
		ID:     "r4",
		Method: "session.prompt",
		Params: json.RawMessage(`{"session_id":"ses_1","text":"hi"}`),
	}

	response := readFrame(t, transport)
	if response.ID != "r4" || response.Error == nil {
		t.Fatalf("response = %+v, want error for r4", response)
	}
	if response.Error.Code != "internal_error" {
		t.Fatalf("error code = %q, want internal_error", response.Error.Code)
	}
	if strings.Contains(response.Error.Message, `"session_id"`) {
		t.Fatalf("error message echoes params: %q", response.Error.Message)
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run() = %v, want nil after cancel", err)
	}
}

func TestRunnerWithoutEventSourceStaysAlive(t *testing.T) {
	transport := newFakeTransport()
	r := New(
		transport,
		handler.New(stubOpenCode{}, stubSupervisor{}),
		nil,
		RunnerConfig{NodeID: "node-1", AgentVersion: "0.1.0-test"},
	)
	cancel, done := runAsync(t, r)

	completeHandshake(t, transport)

	// A nil event source must only disable event forwarding; it must not tear
	// down an otherwise healthy connection after the handshake.
	select {
	case err := <-done:
		t.Fatalf("Run returned before cancellation: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	transport.reads <- protocol.Frame{Type: protocol.FrameRequest, ID: "r5", Method: "node.info"}
	response := readFrame(t, transport)
	if response.ID != "r5" || response.Error != nil {
		t.Fatalf("response = %+v, want successful r5", response)
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run() = %v, want nil after cancel", err)
	}
}

func TestNewDefaultsHeartbeat(t *testing.T) {
	r, _, _ := testRunner(t, 0)
	if r.cfg.Heartbeat != 10*time.Second {
		t.Fatalf("heartbeat = %v, want 10s", r.cfg.Heartbeat)
	}
}

// testRunnerWithConfig builds a runner with an explicit identity so identity
// tests can vary NodeID/NodeName without disturbing the shared helper.
func testRunnerWithConfig(cfg RunnerConfig) (*Runner, *fakeTransport, *fakeEvents) {
	transport := newFakeTransport()
	events := &fakeEvents{events: make(chan opencode.Event, 8)}
	return New(
		transport,
		handler.New(stubOpenCode{}, stubSupervisor{}),
		events,
		cfg,
	), transport, events
}

func TestSendHelloIncludesNodeName(t *testing.T) {
	r, transport, _ := testRunnerWithConfig(RunnerConfig{
		NodeID:       "node-1",
		NodeName:     "dev-box",
		AgentVersion: "0.1.0-test",
	})

	if err := r.sendHello(context.Background()); err != nil {
		t.Fatalf("sendHello() = %v, want nil", err)
	}

	hello := awaitHello(t, transport)
	if !strings.Contains(string(hello.Params), `"node_name":"dev-box"`) {
		t.Fatalf("hello params = %s, want node_name dev-box", hello.Params)
	}
}

func TestAwaitWelcomeUsesAssignedNodeID(t *testing.T) {
	r, transport, events := testRunnerWithConfig(RunnerConfig{
		NodeID:       "node-1",
		NodeName:     "dev-box",
		AgentVersion: "0.1.0-test",
	})
	cancel, done := runAsync(t, r)

	hello := awaitHello(t, transport)
	transport.reads <- protocol.Frame{
		Type:   protocol.FrameWelcome,
		ID:     hello.ID,
		Result: json.RawMessage(`{"node_id":"node_assigned"}`),
	}
	events.events <- opencode.Event{Type: "session.text.delta", SessionID: "ses_1"}

	frame := readFrame(t, transport)
	if frame.Type != protocol.FrameEvent || frame.Event == nil {
		t.Fatalf("frame = %+v, want event frame", frame)
	}
	if frame.Event.NodeID != "node_assigned" {
		t.Fatalf("event node_id = %q, want node_assigned", frame.Event.NodeID)
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run() = %v, want nil after cancel", err)
	}
}

func TestAwaitWelcomeFallsBackWithoutNodeID(t *testing.T) {
	r, transport, events := testRunnerWithConfig(RunnerConfig{
		NodeID:       "node-1",
		NodeName:     "dev-box",
		AgentVersion: "0.1.0-test",
	})
	cancel, done := runAsync(t, r)

	completeHandshake(t, transport)
	events.events <- opencode.Event{Type: "session.text.delta", SessionID: "ses_1"}

	frame := readFrame(t, transport)
	if frame.Type != protocol.FrameEvent || frame.Event == nil {
		t.Fatalf("frame = %+v, want event frame", frame)
	}
	if frame.Event.NodeID != "node-1" {
		t.Fatalf("event node_id = %q, want configured node-1", frame.Event.NodeID)
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run() = %v, want nil after cancel", err)
	}
}

func TestAwaitWelcomeRejectsMalformedNodeID(t *testing.T) {
	r, transport, _ := testRunnerWithConfig(RunnerConfig{
		NodeID:       "node-1",
		NodeName:     "dev-box",
		AgentVersion: "0.1.0-test",
	})
	cancel, done := runAsync(t, r)

	hello := awaitHello(t, transport)
	transport.reads <- protocol.Frame{
		Type:   protocol.FrameWelcome,
		ID:     hello.ID,
		Result: json.RawMessage(`{"node_id":`),
	}

	err := waitRun(t, done)
	if err == nil || !strings.Contains(err.Error(), "welcome") {
		t.Fatalf("Run() = %v, want wrapped welcome decode error", err)
	}
	cancel()
}

func TestHelloCapabilitiesIncludeI1BMethods(t *testing.T) {
	r, transport, _ := testRunner(t, 0)
	if err := r.sendHello(context.Background()); err != nil {
		t.Fatal(err)
	}
	hello := awaitHello(t, transport)
	var params struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(hello.Params, &params); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"project.list",
		"session.diff", "session.interrupt",
		"session.children",
		"file.list", "file.read",
		"pty.create", "pty.attach", "pty.write", "pty.resize", "pty.close",
	}
	for _, method := range want {
		if !contains(params.Capabilities, method) {
			t.Fatalf("capabilities missing %q: %v", method, params.Capabilities)
		}
	}
}

func TestRunnerForwardsPTYEventsAndClosesOnExit(t *testing.T) {
	transport := newFakeTransport()
	requestHandler := &fakeRequestHandler{events: make(chan protocol.Event, 4)}
	r := New(transport, requestHandler, nil, RunnerConfig{
		NodeID:       "node-1",
		AgentVersion: "0.1.0-test",
	})
	cancel, done := runAsync(t, r)
	completeHandshake(t, transport)

	requestHandler.events <- protocol.Event{
		NodeID:    "",
		SessionID: "ses_1",
		Type:      "pty.output",
		Data:      json.RawMessage(`{"pty_id":"pty_1","data":"aGk="}`),
	}
	frame := readFrame(t, transport)
	if frame.Type != protocol.FrameEvent || frame.Event == nil {
		t.Fatalf("frame = %+v, want event", frame)
	}
	if frame.Event.NodeID != "node-1" || frame.Event.SessionID != "ses_1" || frame.Event.Type != "pty.output" {
		t.Fatalf("event = %#v", frame.Event)
	}
	if frame.Event.Time.IsZero() {
		t.Fatal("event time was not filled")
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if !requestHandler.closed.Load() {
		t.Fatal("handler.ClosePTYs was not called")
	}
}

// welcomeCredentialSink 记录 welcome 下发的凭证。got 只存在于内存中；
// 失败消息打印长度，避免把明文写进测试日志。
type welcomeCredentialSink struct {
	got string
	err error
}

func (s *welcomeCredentialSink) Set(raw string) error {
	s.got = raw
	return s.err
}

func TestWelcomeCredentialIsPersisted(t *testing.T) {
	sink := &welcomeCredentialSink{}
	r, transport, _ := testRunnerWithConfig(RunnerConfig{
		NodeID:         "node-1",
		NodeName:       "dev-box",
		AgentVersion:   "0.1.0-test",
		CredentialSink: sink.Set,
	})
	cancel, done := runAsync(t, r)

	hello := awaitHello(t, transport)
	transport.reads <- protocol.Frame{
		Type:   protocol.FrameWelcome,
		ID:     hello.ID,
		Result: json.RawMessage(`{"node_id":"node-1","credential":"cred-9"}`),
	}
	// 一条成功响应证明 welcome 已被处理：sink 在握手完成前就被调用。
	transport.reads <- protocol.Frame{Type: protocol.FrameRequest, ID: "r-cred", Method: "node.info"}
	if response := readFrame(t, transport); response.ID != "r-cred" || response.Error != nil {
		t.Fatalf("response = %+v, want a successful handshake follow-up", response)
	}

	if want := "cred-9"; sink.got != want {
		t.Fatalf("credential sink received %d bytes, want %d", len(sink.got), len(want))
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run() = %v, want nil after cancel", err)
	}
}

// TestWelcomeCredentialSinkFailureFailsHandshake 固化「没写下的凭证就是没人
// 持有的凭证」：控制面已经存了哈希，agent 落盘失败必须断开而不是继续用一个
// 只存在于内存里的身份。
func TestWelcomeCredentialSinkFailureFailsHandshake(t *testing.T) {
	transport := newFakeTransport()
	r := New(transport, nil, nil, RunnerConfig{
		NodeID:       "node-1",
		AgentVersion: "0.1.0-test",
		CredentialSink: func(string) error {
			return errors.New("disk full")
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-transport.writes // hello
		transport.reads <- protocol.Frame{
			Type:   protocol.FrameWelcome,
			Result: json.RawMessage(`{"node_id":"node-1","credential":"cred-secret"}`),
		}
	}()

	err := r.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "persist node credential") {
		t.Fatalf("Run() = %v, want a wrapped persist error", err)
	}
	// 调用方靠这个哨兵判断「控制面已登记凭证、本地没写下」，从而停止重试，
	// 否则每轮都会用旧令牌新建一个节点。
	if !errors.Is(err, ErrCredentialPersist) {
		t.Fatalf("Run() = %v, want it to wrap ErrCredentialPersist", err)
	}
	if strings.Contains(err.Error(), "cred-secret") {
		t.Fatal("persist error leaked the credential")
	}
}

func TestWelcomeWithoutCredentialSkipsSink(t *testing.T) {
	sink := &welcomeCredentialSink{}
	r, transport, _ := testRunnerWithConfig(RunnerConfig{
		NodeID:         "node-1",
		AgentVersion:   "0.1.0-test",
		CredentialSink: sink.Set,
	})
	cancel, done := runAsync(t, r)

	completeHandshake(t, transport)
	transport.reads <- protocol.Frame{Type: protocol.FrameRequest, ID: "r-nocred", Method: "node.info"}
	if response := readFrame(t, transport); response.ID != "r-nocred" || response.Error != nil {
		t.Fatalf("response = %+v, want a successful handshake follow-up", response)
	}
	if sink.got != "" {
		t.Fatalf("credential sink received %d bytes, want no call", len(sink.got))
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run() = %v, want nil after cancel", err)
	}
}

// TestWelcomeCredentialIgnoredWithoutSink 覆盖测试与嵌入式调用方：没有 sink
// 时一次性凭证被忽略，握手仍然成功。
func TestWelcomeCredentialIgnoredWithoutSink(t *testing.T) {
	r, transport, _ := testRunnerWithConfig(RunnerConfig{
		NodeID:       "node-1",
		AgentVersion: "0.1.0-test",
	})
	cancel, done := runAsync(t, r)

	hello := awaitHello(t, transport)
	transport.reads <- protocol.Frame{
		Type:   protocol.FrameWelcome,
		ID:     hello.ID,
		Result: json.RawMessage(`{"node_id":"node-1","credential":"cred-9"}`),
	}
	transport.reads <- protocol.Frame{Type: protocol.FrameRequest, ID: "r-nil-sink", Method: "node.info"}
	if response := readFrame(t, transport); response.ID != "r-nil-sink" || response.Error != nil {
		t.Fatalf("response = %+v, want a successful handshake follow-up", response)
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run() = %v, want nil after cancel", err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestHelloCarriesWorkspaceRoots(t *testing.T) {
	tr := newFakeTransport()
	events := &fakeEvents{events: make(chan opencode.Event)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := New(tr, handler.New(stubOpenCode{}, stubSupervisor{}), events, RunnerConfig{
		NodeName:       "roots",
		AgentVersion:   "test",
		WorkspaceRoots: []string{"/work/a", "/work/b"},
	})
	go func() { _ = r.Run(ctx) }()

	frame := <-tr.writes
	if frame.Type != protocol.FrameHello {
		t.Fatalf("frame type = %q, want hello", frame.Type)
	}
	var hello struct {
		WorkspaceRoots []string `json:"workspace_roots"`
	}
	if err := json.Unmarshal(frame.Params, &hello); err != nil {
		t.Fatal(err)
	}
	if len(hello.WorkspaceRoots) != 2 || hello.WorkspaceRoots[1] != "/work/b" {
		t.Fatalf("workspace roots = %#v", hello.WorkspaceRoots)
	}
}
