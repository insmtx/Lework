package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/Lework/backend/internal/agentd/protocol"
	"github.com/insmtx/Lework/backend/internal/controlplane/event"
	"github.com/insmtx/Lework/backend/internal/controlplane/node"
	"github.com/insmtx/Lework/backend/internal/controlplane/store"
)

// fakeAgent 是测试用的节点写入端：把控制面发出的请求帧交给测试，
// 由测试通过 conn.Deliver 送回响应。
type fakeAgent struct {
	frames chan protocol.Frame
}

func (f *fakeAgent) Write(ctx context.Context, frame protocol.Frame) error {
	select {
	case f.frames <- frame:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakeAgent) Close() error { return nil }

// testEnv 组装真实的 store、registry、hub 与 API。
type testEnv struct {
	t        *testing.T
	store    *store.Store
	registry *node.Registry
	hub      *event.Hub
	mux      *http.ServeMux
}

func newTestEnv(t *testing.T, timeout time.Duration) *testEnv {
	t.Helper()

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	registry := node.NewRegistry()
	hub := event.NewHub()

	api := New(st, registry, hub, timeout)
	mux := http.NewServeMux()
	api.Register(mux)

	env := &testEnv{t: t, store: st, registry: registry, hub: hub, mux: mux}
	t.Cleanup(func() {
		registry.CloseAll()
		_ = st.Close()
	})
	return env
}

// addNode 写入持久化节点记录并注册一条活跃连接。
func (e *testEnv) addNode(name string) (store.Node, *fakeAgent, *node.Connection) {
	e.t.Helper()

	rec, err := e.store.UpsertNode(context.Background(), store.Node{
		Name:         name,
		OS:           "linux",
		Arch:         "amd64",
		AgentVersion: "0.1.0",
		Capabilities: []string{"session.list", "session.create"},
		LastSeen:     time.Now(),
	})
	if err != nil {
		e.t.Fatalf("upsert node: %v", err)
	}

	agent := &fakeAgent{frames: make(chan protocol.Frame, 8)}
	conn := node.NewConnection(rec.ID, agent)
	e.registry.Register(node.Info{
		ID:           rec.ID,
		Name:         rec.Name,
		OS:           rec.OS,
		Arch:         rec.Arch,
		AgentVersion: rec.AgentVersion,
		Capabilities: rec.Capabilities,
		Connected:    true,
		LastSeen:     time.Now(),
	}, conn)
	return rec, agent, conn
}

func (e *testEnv) do(method, target, body string) *httptest.ResponseRecorder {
	e.t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

// replyOK 读取一条出站请求，记录后向连接投递成功响应。
func replyOK(t *testing.T, agent *fakeAgent, conn *node.Connection, result string) <-chan protocol.Frame {
	t.Helper()

	got := make(chan protocol.Frame, 1)
	go func() {
		select {
		case frame := <-agent.frames:
			got <- frame
			conn.Deliver(protocol.Frame{
				Type:   protocol.FrameResponse,
				ID:     frame.ID,
				Result: json.RawMessage(result),
			})
		case <-time.After(5 * time.Second):
		}
	}()
	return got
}

// replyErr 读取一条出站请求并投递节点侧错误响应。
func replyErr(t *testing.T, agent *fakeAgent, conn *node.Connection, code, message string) <-chan protocol.Frame {
	t.Helper()

	got := make(chan protocol.Frame, 1)
	go func() {
		select {
		case frame := <-agent.frames:
			got <- frame
			conn.Deliver(protocol.Frame{
				Type:  protocol.FrameResponse,
				ID:    frame.ID,
				Error: &protocol.FrameError{Code: code, Message: message},
			})
		case <-time.After(5 * time.Second):
		}
	}()
	return got
}

func waitFrame(t *testing.T, got <-chan protocol.Frame) protocol.Frame {
	t.Helper()

	select {
	case frame := <-got:
		return frame
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not receive a request")
		return protocol.Frame{}
	}
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type nodeResponse struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	AgentVersion string    `json:"agent_version"`
	Capabilities []string  `json:"capabilities"`
	Connected    bool      `json:"connected"`
	LastSeen     time.Time `json:"last_seen"`
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return value
}

func requireStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()

	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, want, rec.Body.String())
	}
}

func requireErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()

	requireStatus(t, rec, wantStatus)
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	body := decodeBody[apiError](t, rec)
	if body.Code != wantCode {
		t.Fatalf("error code = %q, want %q", body.Code, wantCode)
	}
}

func decodeParams(t *testing.T, frame protocol.Frame) map[string]string {
	t.Helper()

	var params map[string]string
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		t.Fatalf("decode params %q: %v", string(frame.Params), err)
	}
	return params
}

func TestAPIListNodes(t *testing.T) {
	env := newTestEnv(t, time.Second)

	first := env.do(http.MethodGet, "/api/nodes", "")
	requireStatus(t, first, http.StatusOK)
	if got := strings.TrimSpace(first.Body.String()); got != "[]" {
		t.Fatalf("empty list body = %q, want []", got)
	}

	stored, _, _ := env.addNode("dev-box")

	httpRec := env.do(http.MethodGet, "/api/nodes", "")
	requireStatus(t, httpRec, http.StatusOK)
	nodes := decodeBody[[]nodeResponse](t, httpRec)
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	got := nodes[0]
	if got.Name != "dev-box" || got.ID != stored.ID {
		t.Fatalf("node identity = %+v, want name dev-box id %s", got, stored.ID)
	}
	if !got.Connected {
		t.Fatalf("node connected = false, want true")
	}
	if got.LastSeen.IsZero() {
		t.Fatalf("node last_seen is zero")
	}
}

// TestAPIListNodesUsesStoredLastSeen 固化控制器裁决：身份字段与 LastSeen
// 一律取持久化记录，注册表只决定 Connected。
func TestAPIListNodesUsesStoredLastSeen(t *testing.T) {
	env := newTestEnv(t, time.Second)
	stored, _, _ := env.addNode("dev-box")

	// 用明显不同的内存 LastSeen 覆盖注册表项；API 不应采用它。
	conn := node.NewConnection(stored.ID, &fakeAgent{frames: make(chan protocol.Frame, 1)})
	env.registry.Register(node.Info{
		ID:        stored.ID,
		Name:      stored.Name,
		Connected: true,
		LastSeen:  stored.LastSeen.Add(time.Hour),
	}, conn)

	httpRec := env.do(http.MethodGet, "/api/nodes", "")
	requireStatus(t, httpRec, http.StatusOK)
	nodes := decodeBody[[]nodeResponse](t, httpRec)
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	if !nodes[0].Connected {
		t.Fatal("connected = false, want true while registry has a live connection")
	}
	if !nodes[0].LastSeen.Equal(stored.LastSeen) {
		t.Fatalf("last_seen = %s, want stored %s", nodes[0].LastSeen, stored.LastSeen)
	}

	env.registry.CloseAll()
	httpRec = env.do(http.MethodGet, "/api/nodes", "")
	requireStatus(t, httpRec, http.StatusOK)
	nodes = decodeBody[[]nodeResponse](t, httpRec)
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1 after disconnect", len(nodes))
	}
	if nodes[0].Connected {
		t.Fatal("connected = true after disconnect, want false")
	}
	if !nodes[0].LastSeen.Equal(stored.LastSeen) {
		t.Fatalf("last_seen = %s after disconnect, want stored %s", nodes[0].LastSeen, stored.LastSeen)
	}
}

func TestAPIGetNode(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, _, _ := env.addNode("dev-box")

	httpRec := env.do(http.MethodGet, "/api/nodes/"+rec.ID, "")
	requireStatus(t, httpRec, http.StatusOK)
	got := decodeBody[nodeResponse](t, httpRec)
	if got.ID != rec.ID || !got.Connected {
		t.Fatalf("node = %+v, want id %s connected", got, rec.ID)
	}

	missing := env.do(http.MethodGet, "/api/nodes/node_missing", "")
	requireErrorCode(t, missing, http.StatusNotFound, "not_found")
}

func TestAPINodeHealth(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, agent, conn := env.addNode("dev-box")

	got := replyOK(t, agent, conn, `{"state":"running"}`)
	httpRec := env.do(http.MethodGet, "/api/nodes/"+rec.ID+"/health", "")
	requireStatus(t, httpRec, http.StatusOK)

	if ct := httpRec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if body := strings.TrimSpace(httpRec.Body.String()); body != `{"state":"running"}` {
		t.Fatalf("body = %q, want raw agent result", body)
	}

	frame := waitFrame(t, got)
	if frame.Method != "opencode.status" {
		t.Fatalf("method = %q, want opencode.status", frame.Method)
	}
}

// TestAPIEmptyAgentResult 确认空结果写成 JSON null，而不是空响应体。
func TestAPIEmptyAgentResult(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, agent, conn := env.addNode("dev-box")

	got := replyOK(t, agent, conn, "")
	httpRec := env.do(http.MethodGet, "/api/nodes/"+rec.ID+"/health", "")
	requireStatus(t, httpRec, http.StatusOK)
	if body := strings.TrimSpace(httpRec.Body.String()); body != "null" {
		t.Fatalf("body = %q, want null", body)
	}
	waitFrame(t, got)
}

func TestAPIListSessions(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, agent, conn := env.addNode("dev-box")

	got := replyOK(t, agent, conn, `[{"id":"ses_1"}]`)
	httpRec := env.do(http.MethodGet, "/api/sessions?node="+rec.ID+"&directory=%2Fwork", "")
	requireStatus(t, httpRec, http.StatusOK)
	if body := strings.TrimSpace(httpRec.Body.String()); body != `[{"id":"ses_1"}]` {
		t.Fatalf("body = %q, want raw agent result", body)
	}

	frame := waitFrame(t, got)
	if frame.Method != "session.list" {
		t.Fatalf("method = %q, want session.list", frame.Method)
	}
	params := decodeParams(t, frame)
	if params["directory"] != "/work" {
		t.Fatalf("params = %v, want directory /work", params)
	}
}

func TestAPICreateSession(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, agent, conn := env.addNode("dev-box")

	got := replyOK(t, agent, conn, `{"id":"ses_new"}`)
	httpRec := env.do(http.MethodPost, "/api/sessions",
		`{"node_id":"`+rec.ID+`","directory":"/work"}`)
	requireStatus(t, httpRec, http.StatusOK)
	if body := strings.TrimSpace(httpRec.Body.String()); body != `{"id":"ses_new"}` {
		t.Fatalf("body = %q, want raw agent result", body)
	}

	frame := waitFrame(t, got)
	if frame.Method != "session.create" {
		t.Fatalf("method = %q, want session.create", frame.Method)
	}
	params := decodeParams(t, frame)
	if params["directory"] != "/work" {
		t.Fatalf("params = %v, want directory /work", params)
	}

	t.Run("missing node", func(t *testing.T) {
		httpRec := env.do(http.MethodPost, "/api/sessions", `{"directory":"/work"}`)
		requireErrorCode(t, httpRec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("missing directory", func(t *testing.T) {
		httpRec := env.do(http.MethodPost, "/api/sessions", `{"node_id":"`+rec.ID+`"}`)
		requireErrorCode(t, httpRec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("relative directory", func(t *testing.T) {
		httpRec := env.do(http.MethodPost, "/api/sessions",
			`{"node_id":"`+rec.ID+`","directory":"relative/path"}`)
		requireErrorCode(t, httpRec, http.StatusBadRequest, "invalid_request")
	})
}

func TestAPISessionMessages(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, agent, conn := env.addNode("dev-box")

	got := replyOK(t, agent, conn, `[{"role":"user"}]`)
	httpRec := env.do(http.MethodGet, "/api/sessions/"+rec.ID+"/ses_1/messages", "")
	requireStatus(t, httpRec, http.StatusOK)
	if body := strings.TrimSpace(httpRec.Body.String()); body != `[{"role":"user"}]` {
		t.Fatalf("body = %q, want raw agent result", body)
	}

	frame := waitFrame(t, got)
	if frame.Method != "session.messages" {
		t.Fatalf("method = %q, want session.messages", frame.Method)
	}
	params := decodeParams(t, frame)
	if params["session_id"] != "ses_1" {
		t.Fatalf("params = %v, want session_id ses_1", params)
	}
}

func TestAPISessionPrompt(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, agent, conn := env.addNode("dev-box")

	got := replyOK(t, agent, conn, `{"accepted":true}`)
	httpRec := env.do(http.MethodPost, "/api/sessions/"+rec.ID+"/ses_1/prompt",
		`{"text":"hello\nworld","idempotency_key":"key-1"}`)
	requireStatus(t, httpRec, http.StatusOK)
	if body := strings.TrimSpace(httpRec.Body.String()); body != `{"accepted":true}` {
		t.Fatalf("body = %q, want raw agent result", body)
	}

	frame := waitFrame(t, got)
	if frame.Method != "session.prompt" {
		t.Fatalf("method = %q, want session.prompt", frame.Method)
	}
	params := decodeParams(t, frame)
	if params["session_id"] != "ses_1" {
		t.Fatalf("params = %v, want session_id ses_1", params)
	}
	if params["text"] != "hello\nworld" {
		t.Fatalf("text = %q, want preserved prompt text", params["text"])
	}
	if params["idempotency_key"] != "key-1" {
		t.Fatalf("idempotency_key = %q, want key-1", params["idempotency_key"])
	}

	t.Run("missing text", func(t *testing.T) {
		httpRec := env.do(http.MethodPost, "/api/sessions/"+rec.ID+"/ses_1/prompt",
			`{"idempotency_key":"key-1"}`)
		requireErrorCode(t, httpRec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("missing idempotency key", func(t *testing.T) {
		httpRec := env.do(http.MethodPost, "/api/sessions/"+rec.ID+"/ses_1/prompt",
			`{"text":"hello"}`)
		requireErrorCode(t, httpRec, http.StatusBadRequest, "invalid_request")
	})
}

func TestAPIPermissionReply(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, agent, conn := env.addNode("dev-box")

	got := replyOK(t, agent, conn, `null`)
	httpRec := env.do(http.MethodPost, "/api/sessions/"+rec.ID+"/ses_1/permissions/perm_1/reply",
		`{"decision":"once","message":"ok"}`)
	requireStatus(t, httpRec, http.StatusOK)
	if body := strings.TrimSpace(httpRec.Body.String()); body != `null` {
		t.Fatalf("body = %q, want null", body)
	}

	frame := waitFrame(t, got)
	if frame.Method != "permission.reply" {
		t.Fatalf("method = %q, want permission.reply", frame.Method)
	}
	params := decodeParams(t, frame)
	if params["session_id"] != "ses_1" || params["permission_id"] != "perm_1" {
		t.Fatalf("params = %v, want session_id ses_1 permission_id perm_1", params)
	}
	if params["decision"] != "once" || params["message"] != "ok" {
		t.Fatalf("params = %v, want decision once message ok", params)
	}
}

func TestAPIErrorMapping(t *testing.T) {
	t.Run("offline node", func(t *testing.T) {
		env := newTestEnv(t, time.Second)
		rec, _, _ := env.addNode("dev-box")
		// 连接断开：注册表中已无活跃连接。
		env.registry.CloseAll()

		httpRec := env.do(http.MethodGet, "/api/nodes/"+rec.ID+"/health", "")
		requireErrorCode(t, httpRec, http.StatusConflict, "node_offline")
		body := decodeBody[apiError](t, httpRec)
		if body.Message != "node is offline" {
			t.Fatalf("message = %q, want node is offline", body.Message)
		}
	})

	t.Run("connection closed", func(t *testing.T) {
		env := newTestEnv(t, time.Second)
		rec, _, conn := env.addNode("dev-box")
		conn.Fail(nil)

		httpRec := env.do(http.MethodGet, "/api/nodes/"+rec.ID+"/health", "")
		requireErrorCode(t, httpRec, http.StatusConflict, "node_offline")
	})

	t.Run("agent timeout", func(t *testing.T) {
		// 不投递响应，请求应在短超时后映射为 504。
		env := newTestEnv(t, 20*time.Millisecond)
		rec, _, _ := env.addNode("dev-box")

		httpRec := env.do(http.MethodGet, "/api/nodes/"+rec.ID+"/health", "")
		requireErrorCode(t, httpRec, http.StatusGatewayTimeout, "agent_timeout")
	})

	t.Run("remote error", func(t *testing.T) {
		env := newTestEnv(t, time.Second)
		rec, agent, conn := env.addNode("dev-box")

		got := replyErr(t, agent, conn, "session_not_found", "session not found")
		httpRec := env.do(http.MethodGet, "/api/sessions/"+rec.ID+"/ses_missing/messages", "")
		requireErrorCode(t, httpRec, http.StatusBadGateway, "agent_error")
		waitFrame(t, got)
	})
}

// streamRecorder 是并发安全的 SSE 响应写入端；可选地阻塞首次正文写入，
// 以便确定性地制造慢消费者被 Hub 淘汰。
type streamRecorder struct {
	mu        sync.Mutex
	header    http.Header
	status    int
	body      bytes.Buffer
	flushed   chan struct{}
	flushOnce sync.Once
	blockOnce sync.Once
	block     chan struct{}
}

func newStreamRecorder() *streamRecorder {
	rec := &streamRecorder{
		header:  make(http.Header),
		flushed: make(chan struct{}),
		block:   make(chan struct{}),
	}
	// 默认不阻塞：构造后立即放行首次写入。
	close(rec.block)
	return rec
}

func newBlockingStreamRecorder() *streamRecorder {
	return &streamRecorder{
		header:  make(http.Header),
		flushed: make(chan struct{}),
		block:   make(chan struct{}),
	}
}

func (s *streamRecorder) Header() http.Header { return s.header }

func (s *streamRecorder) WriteHeader(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == 0 {
		s.status = status
	}
}

func (s *streamRecorder) Write(p []byte) (int, error) {
	s.blockOnce.Do(func() { <-s.block })

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.Write(p)
}

func (s *streamRecorder) Flush() {
	s.flushOnce.Do(func() { close(s.flushed) })
}

func (s *streamRecorder) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.String()
}

func (s *streamRecorder) statusCode() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// eventStream 启动 SSE handler，并等待其完成首次 flush。
type eventStream struct {
	rec    *streamRecorder
	cancel context.CancelFunc
	done   chan struct{}
}

func startEvents(t *testing.T, env *testEnv, target string) *eventStream {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
	rec := newStreamRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		env.mux.ServeHTTP(rec, req)
	}()

	select {
	case <-rec.flushed:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("SSE handler did not flush headers")
	}
	return &eventStream{rec: rec, cancel: cancel, done: done}
}

func waitStreamDone(t *testing.T, stream *eventStream) {
	t.Helper()

	select {
	case <-stream.done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not return")
	}
}

func waitBody(t *testing.T, rec *streamRecorder, marker string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.text(), marker) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("stream body %q does not contain %q", rec.text(), marker)
}

func TestEventsStream(t *testing.T) {
	env := newTestEnv(t, time.Second)
	stream := startEvents(t, env, "/api/events")
	defer stream.cancel()

	if got := stream.rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if got := stream.rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
	if got := stream.rec.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no", got)
	}
	if status := stream.rec.statusCode(); status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	env.hub.Publish(event.Event{
		NodeID:    "node_1",
		SessionID: "ses_1",
		Type:      "session.message",
		Data:      json.RawMessage(`{"text":"hi"}`),
		Time:      time.Now(),
	})

	waitBody(t, stream.rec, `"type":"session.message"`)
	body := stream.rec.text()
	if !strings.HasPrefix(body, "data: ") {
		t.Fatalf("stream body %q does not start with data frame", body)
	}

	stream.cancel()
	waitStreamDone(t, stream)
}

func TestEventsFilters(t *testing.T) {
	env := newTestEnv(t, time.Second)
	stream := startEvents(t, env, "/api/events?node=node_1&session=ses_1")
	defer stream.cancel()

	env.hub.Publish(event.Event{NodeID: "node_2", SessionID: "ses_1", Type: "wrong-node", Time: time.Now()})
	env.hub.Publish(event.Event{NodeID: "node_1", SessionID: "ses_2", Type: "wrong-session", Time: time.Now()})
	env.hub.Publish(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "match", Time: time.Now()})

	waitBody(t, stream.rec, `"type":"match"`)
	body := stream.rec.text()
	if strings.Contains(body, "wrong-node") {
		t.Fatalf("stream body %q contains event for another node", body)
	}
	if strings.Contains(body, "wrong-session") {
		t.Fatalf("stream body %q contains event for another session", body)
	}

	stream.cancel()
	waitStreamDone(t, stream)
}

func TestEventsClientCancel(t *testing.T) {
	env := newTestEnv(t, time.Second)
	stream := startEvents(t, env, "/api/events")

	stream.cancel()
	waitStreamDone(t, stream)

	// 取消之后发布事件不应再写入响应。
	before := stream.rec.text()
	env.hub.Publish(event.Event{NodeID: "node_1", Type: "late", Time: time.Now()})
	if after := stream.rec.text(); after != before {
		t.Fatalf("body changed after cancel: %q -> %q", before, after)
	}
}

func TestEventsResyncOnClosedSubscription(t *testing.T) {
	env := newTestEnv(t, time.Second)

	// 阻塞首次正文写入，让订阅者来不及消费，从而被 Hub 淘汰。
	blocking := newBlockingStreamRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		env.mux.ServeHTTP(blocking, req)
	}()

	select {
	case <-blocking.flushed:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not flush headers")
	}

	// 超过订阅缓冲（256）的事件会让 Hub 关闭该订阅通道。
	for i := 0; i < 300; i++ {
		env.hub.Publish(event.Event{NodeID: "node_1", Type: "burst", Time: time.Now()})
	}

	close(blocking.block)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not return after subscription closed")
	}

	body := blocking.text()
	const resync = "event: resync\ndata: {\"reason\":\"slow_consumer\"}\n\n"
	if !strings.HasSuffix(body, resync) {
		t.Fatalf("stream body does not end with resync frame: %q", body)
	}
	if strings.Count(body, "event: resync") != 1 {
		t.Fatalf("resync frame written %d times, want 1", strings.Count(body, "event: resync"))
	}
}
