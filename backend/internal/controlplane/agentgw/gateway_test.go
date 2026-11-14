package agentgw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/insmtx/lework/backend/internal/agentd/protocol"
	"github.com/insmtx/lework/backend/internal/agentd/transport"
	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/node"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// testToken 是测试用注册令牌，只出现在客户端请求头里。
const testToken = "test-enrollment-token-9f3a"

// gatewayEnv 是网关测试的真实依赖集合：SQLite、注册表、事件中心和
// 一个用 httptest 启动的 WebSocket 服务端。
type gatewayEnv struct {
	store     *store.Store
	registry  *node.Registry
	hub       *event.Hub
	publisher *hubPublisher
	coreSub   *noopCoreSub
	server    *httptest.Server

	// gateway 暴露网关本身，测试可以核对它持有的注册表与写入端。
	gateway *Gateway

	// handlers 统计仍在运行的 ServeHTTP；httptest 在连接被 hijack 后不再
	// 等待 handler，必须由测试自己等它退出，否则会在存储关闭后继续访问。
	handlers sync.WaitGroup
}

// hubPublisher 把网关事件直接投给本地 Hub，替代真实 NATS。
//
// failPrefix 非空时，类型以它开头的事件返回错误，用来分别验证状态事件与
// PTY 事件发布失败时的两条处理路径。
type hubPublisher struct {
	hub        *event.Hub
	failPrefix string
}

func (p *hubPublisher) PublishEvent(_ context.Context, ev event.Event) error {
	if p.failPrefix != "" && strings.HasPrefix(ev.Type, p.failPrefix) {
		return fmt.Errorf("injected publish failure for %s", ev.Type)
	}
	p.hub.Publish(ev)
	return nil
}

// coreSubscription 记录一次 Core 订阅，测试据此直接驱动转发 handler。
type coreSubscription struct {
	subject string
	queue   string
	handler func(context.Context, []byte) []byte
}

// noopCoreSub 记录订阅参数后阻塞到 ctx 结束，模拟真实 Core NATS 订阅的生命周期。
//
// 记录使用非阻塞发送：不关心转发请求的用例不读通道，订阅协程仍必须能退出。
type noopCoreSub struct {
	made chan coreSubscription
}

func newNoopCoreSub() *noopCoreSub {
	return &noopCoreSub{made: make(chan coreSubscription, 8)}
}

func (s *noopCoreSub) SubscribeCore(
	ctx context.Context,
	subject, queue string,
	handler func(context.Context, []byte) []byte,
) error {
	select {
	case s.made <- coreSubscription{subject: subject, queue: queue, handler: handler}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

func newGatewayEnv(t *testing.T) *gatewayEnv {
	t.Helper()

	return newGatewayEnvConfig(t, 5*time.Second, nil)
}

// newGatewayEnvConfig 用指定心跳超时与写入端工厂启动网关。
// newWriter 为 nil 时使用网关默认实现。
func newGatewayEnvConfig(t *testing.T, heartbeat time.Duration, newWriter func(*websocket.Conn) node.Writer) *gatewayEnv {
	t.Helper()

	// 使用临时文件而非 ":memory:"：网关的读循环与测试在不同连接上并发
	// 访问存储，纯内存库在 database/sql 连接池下会各自拿到独立的库。
	st, err := store.Open(filepath.Join(t.TempDir(), "controlplane.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	env := &gatewayEnv{
		store:    st,
		registry: node.NewRegistry(),
		hub:      event.NewHub(),
		coreSub:  newNoopCoreSub(),
	}
	env.publisher = &hubPublisher{hub: env.hub}
	gw := New(Config{EnrollmentToken: testToken, HeartbeatTimeout: heartbeat},
		env.store, env.registry, env.publisher, env.coreSub)
	if newWriter != nil {
		gw.newWriter = newWriter
	}
	env.gateway = gw
	env.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.handlers.Add(1)
		defer env.handlers.Done()
		gw.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		// 先关闭节点连接让 handler 退出，再停止接收新请求；httptest 的
		// Close 会等待处理中的 handler，阻塞中的 handler 会拖住它。
		env.registry.CloseAll()
		env.server.Close()
		env.handlers.Wait()
		if err := env.store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return env
}

// dial 用给定的认证头连接网关。header 为空表示完全不带 Authorization。
func (e *gatewayEnv) dial(t *testing.T, header http.Header) (*transport.Client, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return transport.Dial(ctx, transport.DialConfig{
		URL:    "ws" + strings.TrimPrefix(e.server.URL, "http"),
		Header: header,
	})
}

func bearerHeader(token string) http.Header {
	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	return header
}

// connect 建立一条已认证的连接，并在测试结束时关闭。
func (e *gatewayEnv) connect(t *testing.T) *transport.Client {
	t.Helper()

	client, err := e.dial(t, bearerHeader(testToken))
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// testHelloParams 与 runner 发送的 hello 参数保持一致。
type testHelloParams struct {
	NodeName       string   `json:"node_name"`
	AgentVersion   string   `json:"agent_version"`
	OS             string   `json:"os"`
	Arch           string   `json:"arch"`
	Capabilities   []string `json:"capabilities"`
	WorkspaceRoots []string `json:"workspace_roots"`
}

func defaultHello(name string) testHelloParams {
	return testHelloParams{
		NodeName:     name,
		AgentVersion: "dev",
		OS:           "linux",
		Arch:         "amd64",
		Capabilities: []string{"session.list", "session.create", "session.prompt"},
	}
}

func writeHello(t *testing.T, client *transport.Client, params testHelloParams, id string) {
	t.Helper()

	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("encode hello params: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Write(ctx, protocol.Frame{Type: protocol.FrameHello, ID: id, Params: raw}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
}

func readFrame(t *testing.T, client *transport.Client) protocol.Frame {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	frame, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return frame
}

// readUntilError 期望对端关闭连接，并返回读到的错误。
func readUntilError(t *testing.T, client *transport.Client) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := client.Read(ctx)
	return err
}

// handshake 完成注册并返回控制面分配的节点 ID 与 welcome 帧。
func handshake(t *testing.T, client *transport.Client, name string) (string, protocol.Frame) {
	t.Helper()

	writeHello(t, client, defaultHello(name), "hello-1")
	welcome := readFrame(t, client)
	if welcome.Type != protocol.FrameWelcome {
		t.Fatalf("frame type = %q, want welcome", welcome.Type)
	}
	if welcome.ID != "hello-1" {
		t.Fatalf("welcome id = %q, want hello-1", welcome.ID)
	}

	var result struct {
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(welcome.Result, &result); err != nil {
		t.Fatalf("decode welcome result %s: %v", welcome.Result, err)
	}
	if !strings.HasPrefix(result.NodeID, "node_") {
		t.Fatalf("node_id = %q, want node_ prefix", result.NodeID)
	}
	return result.NodeID, welcome
}

// awaitRegistered 等待节点出现在注册表里。
//
// 网关刻意按「先写出 welcome、再登记注册表」的顺序工作，客户端读到 welcome
// 时登记可能尚未完成；直接断言会让用例随调度顺序偶发失败。
func awaitRegistered(t *testing.T, env *gatewayEnv, nodeID string) node.Info {
	t.Helper()

	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if info, ok := env.registry.Get(nodeID); ok {
			return info
		}
		select {
		case <-deadline:
			t.Fatalf("node %q was not registered within 3s", nodeID)
		case <-ticker.C:
		}
	}
}

func listNodes(t *testing.T, st *store.Store) []store.Node {
	t.Helper()

	nodes, err := st.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	return nodes
}

// writeEventFrame 让 agent 侧发出一条事件帧。
func writeEventFrame(t *testing.T, client *transport.Client, ev *protocol.Event) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Write(ctx, protocol.Frame{Type: protocol.FrameEvent, Event: ev}); err != nil {
		t.Fatalf("write event frame: %v", err)
	}
}

// writeRawFrame 让 agent 侧发出一条任意类型的帧。
func writeRawFrame(t *testing.T, client *transport.Client, frame protocol.Frame) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Write(ctx, frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

// awaitEvent 等待第一条满足条件的事件，跳过网关自身发布的生命周期事件。
func awaitEvent(t *testing.T, events <-chan event.Event, match func(event.Event) bool) event.Event {
	t.Helper()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event subscription closed before the expected event")
			}
			if match(ev) {
				return ev
			}
		case <-deadline:
			t.Fatal("timed out waiting for the expected event")
		}
	}
}

// awaitCoreSubscription 等待网关建立转发订阅。
func awaitCoreSubscription(t *testing.T, sub *noopCoreSub) coreSubscription {
	t.Helper()

	select {
	case made := <-sub.made:
		return made
	case <-time.After(3 * time.Second):
		t.Fatal("gateway did not subscribe for forwarded requests")
		return coreSubscription{}
	}
}

func TestServeHTTPRejectsMissingOrWrongToken(t *testing.T) {
	env := newGatewayEnv(t)

	tests := []struct {
		name   string
		header http.Header
	}{
		{"missing", nil},
		{"wrong", bearerHeader("wrong-token")},
		{"wrong scheme", func() http.Header {
			h := http.Header{}
			h.Set("Authorization", "Basic "+testToken)
			return h
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := env.dial(t, tt.header)
			if err == nil {
				_ = client.Close()
				t.Fatal("dial succeeded, want handshake rejection")
			}

			req, err := http.NewRequest(http.MethodGet, env.server.URL+"/api/agent/connect", nil)
			if err != nil {
				t.Fatal(err)
			}
			for key, values := range tt.header {
				req.Header[key] = values
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
			if strings.Contains(string(body), testToken) {
				t.Fatalf("response body leaks the enrollment token: %q", body)
			}
		})
	}
}

func TestHelloRegistersNodeAndSendsWelcome(t *testing.T) {
	env := newGatewayEnv(t)
	client := env.connect(t)

	nodeID, welcome := handshake(t, client, "dev-box")

	nodes := listNodes(t, env.store)
	if len(nodes) != 1 {
		t.Fatalf("stored nodes = %d, want 1", len(nodes))
	}
	if nodes[0].Name != "dev-box" {
		t.Fatalf("node name = %q, want dev-box", nodes[0].Name)
	}
	if nodes[0].ID != nodeID {
		t.Fatalf("stored node id = %q, want %q", nodes[0].ID, nodeID)
	}
	if len(nodes[0].Capabilities) != 3 {
		t.Fatalf("capabilities = %v, want 3 entries", nodes[0].Capabilities)
	}

	info := awaitRegistered(t, env, nodeID)
	if !info.Connected {
		t.Fatal("registered node is not connected")
	}
	if info.Name != "dev-box" || info.AgentVersion != "dev" || info.OS != "linux" || info.Arch != "amd64" {
		t.Fatalf("registry info = %+v, want hello metadata", info)
	}

	encoded, err := json.Marshal(welcome)
	if err != nil {
		t.Fatalf("encode welcome: %v", err)
	}
	if strings.Contains(string(encoded), testToken) {
		t.Fatalf("welcome leaks the enrollment token: %s", encoded)
	}
}

func TestReconnectWithSameNameReusesNodeID(t *testing.T) {
	env := newGatewayEnv(t)

	first := env.connect(t)
	firstID, _ := handshake(t, first, "dev-box")

	second := env.connect(t)
	secondID, _ := handshake(t, second, "dev-box")
	if secondID != firstID {
		t.Fatalf("reconnect node id = %q, want %q", secondID, firstID)
	}

	// 注册表会用新连接替换旧连接并主动关闭旧连接。
	if err := readUntilError(t, first); err == nil {
		t.Fatal("first connection is still open after reconnect")
	}

	if nodes := listNodes(t, env.store); len(nodes) != 1 {
		t.Fatalf("stored nodes = %d, want 1 after reconnect", len(nodes))
	}
	info := awaitRegistered(t, env, firstID)
	if !info.Connected {
		t.Fatalf("registry entry = %+v, want connected", info)
	}
}

func TestHelloWithoutNodeNameClosesWithoutRegistering(t *testing.T) {
	env := newGatewayEnv(t)
	client := env.connect(t)

	writeHello(t, client, testHelloParams{AgentVersion: "dev", OS: "linux", Arch: "amd64"}, "hello-empty")

	if err := readUntilError(t, client); err == nil {
		t.Fatal("connection stayed open, want it closed")
	}
	if nodes := listNodes(t, env.store); len(nodes) != 0 {
		t.Fatalf("stored nodes = %d, want 0", len(nodes))
	}
	if infos := env.registry.List(); len(infos) != 0 {
		t.Fatalf("registry nodes = %+v, want none", infos)
	}
}

func TestEventUsesAssignedNodeIDAndHeartbeatTouchesStore(t *testing.T) {
	env := newGatewayEnv(t)
	client := env.connect(t)
	nodeID, _ := handshake(t, client, "dev-box")

	events, cancel := env.hub.Subscribe(4)
	defer cancel()

	before := time.Now()
	ctx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelWrite()
	if err := client.Write(ctx, protocol.Frame{
		Type: protocol.FrameEvent,
		Event: &protocol.Event{
			NodeID:    "node_spoofed",
			SessionID: "ses_1",
			Type:      "session.text.delta",
			Data:      json.RawMessage(`{"text":"hi"}`),
		},
	}); err != nil {
		t.Fatalf("write event: %v", err)
	}

	// 网关同时会发布 node.connected 等生命周期事件，这里只取被转发的业务事件。
	ev := awaitEvent(t, events, func(ev event.Event) bool { return ev.Type == "session.text.delta" })
	if ev.NodeID != nodeID {
		t.Fatalf("event node_id = %q, want %q", ev.NodeID, nodeID)
	}
	if ev.SessionID != "ses_1" {
		t.Fatalf("event session_id = %q, want ses_1", ev.SessionID)
	}
	if string(ev.Data) != `{"text":"hi"}` {
		t.Fatalf("event data = %s, want original payload", ev.Data)
	}
	if ev.Time.IsZero() {
		t.Fatal("event time is zero, want it filled by the gateway")
	}

	if err := client.Write(ctx, protocol.Frame{Type: protocol.FrameHeartbeat}); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}

	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		nodes := listNodes(t, env.store)
		if len(nodes) == 1 && !nodes[0].LastSeen.Before(before) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("last_seen was not touched by the heartbeat: %+v", nodes)
		case <-ticker.C:
		}
	}
}

func TestRequestFrameClosesConnectionWithProtocolError(t *testing.T) {
	env := newGatewayEnv(t)
	client := env.connect(t)
	nodeID, _ := handshake(t, client, "dev-box")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Write(ctx, protocol.Frame{Type: protocol.FrameRequest, ID: "req-1", Method: "session.list"}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	if err := readUntilError(t, client); err == nil {
		t.Fatal("connection stayed open after a control-plane-bound request frame")
	}

	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, ok := env.registry.Get(nodeID); !ok {
			return
		}
		select {
		case <-deadline:
			t.Fatal("node still registered after protocol error")
		case <-ticker.C:
		}
	}
}

func TestRegistrationDoesNotLeakEnrollmentToken(t *testing.T) {
	env := newGatewayEnv(t)
	client := env.connect(t)
	_, welcome := handshake(t, client, "dev-box")

	nodes := listNodes(t, env.store)
	if len(nodes) != 1 {
		t.Fatalf("stored nodes = %d, want 1", len(nodes))
	}
	encodedNode, err := json.Marshal(nodes[0])
	if err != nil {
		t.Fatalf("encode node: %v", err)
	}
	if strings.Contains(string(encodedNode), testToken) {
		t.Fatalf("stored node leaks the enrollment token: %s", encodedNode)
	}

	encodedWelcome, err := json.Marshal(welcome)
	if err != nil {
		t.Fatalf("encode welcome: %v", err)
	}
	if strings.Contains(string(encodedWelcome), testToken) {
		t.Fatalf("welcome leaks the enrollment token: %s", encodedWelcome)
	}
}

func TestBrowserOriginIsRejected(t *testing.T) {
	env := newGatewayEnv(t)

	header := bearerHeader(testToken)
	header.Set("Origin", "http://localhost:5173")
	client, err := env.dial(t, header)
	if err == nil {
		_ = client.Close()
		t.Fatal("dial with a browser Origin succeeded, want rejection")
	}
	if nodes := listNodes(t, env.store); len(nodes) != 0 {
		t.Fatalf("stored nodes = %d, want 0", len(nodes))
	}
}

// blockingWriter 在首次写入时通知测试并阻塞，直到测试显式放行。
// 它用来固化“welcome 写出前节点不可请求”的登记顺序；后续写入不再阻塞，
// 这样登记顺序错误时测试会以请求错误失败而不是死锁。
type blockingWriter struct {
	inner   *wsWriter
	started chan struct{}
	release chan struct{}

	mu      sync.Mutex
	blocked bool
}

func (b *blockingWriter) Write(ctx context.Context, f protocol.Frame) error {
	b.mu.Lock()
	first := !b.blocked
	if first {
		b.blocked = true
	}
	b.mu.Unlock()

	if first {
		close(b.started)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	}
	return b.inner.Write(ctx, f)
}

func (b *blockingWriter) Close() error { return b.inner.Close() }

// TestNodeIsNotRequestableBeforeWelcome 证明登记注册表发生在 welcome 成功
// 写出之后：写入端阻塞时节点已落库，但既不在注册表里，也无法被请求。
func TestNodeIsNotRequestableBeforeWelcome(t *testing.T) {
	blocking := &blockingWriter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	env := newGatewayEnvConfig(t, 5*time.Second, func(ws *websocket.Conn) node.Writer {
		blocking.inner = &wsWriter{conn: ws}
		return blocking
	})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(blocking.release) }) }
	// 先注册放行清理：测试断言失败时也不能让写入端一直阻塞 handler 退出。
	t.Cleanup(release)
	client := env.connect(t)

	writeHello(t, client, defaultHello("dev-box"), "hello-1")

	select {
	case <-blocking.started:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway did not attempt to write the welcome frame")
	}

	// 节点已持久化，但 welcome 未写出：注册表必须仍然为空。
	if infos := env.registry.List(); len(infos) != 0 {
		t.Fatalf("registry nodes = %+v before welcome, want none", infos)
	}

	nodes := listNodes(t, env.store)
	if len(nodes) != 1 {
		t.Fatalf("stored nodes = %d, want 1", len(nodes))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := env.registry.Request(ctx, nodes[0].ID, "session.list", nil); !errors.Is(err, node.ErrNodeOffline) {
		t.Fatalf("request before welcome error = %v, want ErrNodeOffline", err)
	}

	release()
	welcome := readFrame(t, client)
	if welcome.Type != protocol.FrameWelcome {
		t.Fatalf("frame type = %q, want welcome", welcome.Type)
	}

	// welcome 之后网关才登记；读与登记并发，因此用短轮询等待。
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if info, ok := env.registry.Get(nodes[0].ID); ok {
			if !info.Connected {
				t.Fatal("node registered as disconnected")
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("node was never registered after the welcome frame")
		case <-ticker.C:
		}
	}
}

// TestOversizedFrameClosesConnection 证明 1 MiB 帧上限同时约束 hello 与后续帧：
// 超限帧会让对端关闭连接，而不是被无上限地读入内存。
func TestOversizedFrameClosesConnection(t *testing.T) {
	// 上限之上再留出余量，确保服务端读取时确实越过 read limit。
	oversized := strings.Repeat("a", maxFrameBytes+1024)

	writeFrame := func(t *testing.T, client *transport.Client, frame protocol.Frame) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// 服务端可能在写入完成前就关闭连接，写入错误本身即等价于连接已断开。
		_ = client.Write(ctx, frame)
	}

	requireClosed := func(t *testing.T, client *transport.Client) {
		t.Helper()
		err := readUntilError(t, client)
		if err == nil {
			t.Fatal("connection stayed open after an oversized frame")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("connection did not close within the read deadline: %v", err)
		}
	}

	t.Run("hello", func(t *testing.T) {
		env := newGatewayEnv(t)
		client := env.connect(t)

		writeFrame(t, client, protocol.Frame{
			Type:   protocol.FrameHello,
			ID:     "hello-big",
			Params: json.RawMessage(`{"node_name":"` + oversized + `"}`),
		})

		requireClosed(t, client)
		if nodes := listNodes(t, env.store); len(nodes) != 0 {
			t.Fatalf("stored nodes = %d, want 0", len(nodes))
		}
	})

	t.Run("post welcome frame", func(t *testing.T) {
		env := newGatewayEnv(t)
		client := env.connect(t)
		nodeID, _ := handshake(t, client, "dev-box")

		writeFrame(t, client, protocol.Frame{
			Type: protocol.FrameEvent,
			Event: &protocol.Event{
				SessionID: "ses_1",
				Type:      "session.text.delta",
				Data:      json.RawMessage(`{"text":"` + oversized + `"}`),
			},
		})

		requireClosed(t, client)

		deadline := time.After(3 * time.Second)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, ok := env.registry.Get(nodeID); !ok {
				return
			}
			select {
			case <-deadline:
				t.Fatal("node still registered after an oversized frame")
			case <-ticker.C:
			}
		}
	})
}

// TestHeartbeatTimeoutClosesNodeAndFailsPendingRequests 覆盖静默节点的唯一
// 存活检测路径：心跳超时 -> 连接关闭 -> 待处理请求失败 -> 注册表移除。
func TestHeartbeatTimeoutClosesNodeAndFailsPendingRequests(t *testing.T) {
	// 心跳窗口要覆盖握手、请求 goroutine 启动和首帧写出；窗口太紧时，
	// 负载下连接会在请求帧到达前被判定超时，用例随机失败。
	env := newGatewayEnvConfig(t, time.Second, nil)
	client := env.connect(t)
	nodeID, _ := handshake(t, client, "dev-box")

	requestErr := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		_, err := env.registry.Request(ctx, nodeID, "session.list", nil)
		requestErr <- err
	}()

	// 先确认请求确实写到了线上，再静默等待心跳超时。
	frame := readFrame(t, client)
	if frame.Type != protocol.FrameRequest || frame.Method != "session.list" {
		t.Fatalf("frame = %+v, want a session.list request", frame)
	}

	select {
	case err := <-requestErr:
		if !errors.Is(err, node.ErrConnectionClosed) {
			t.Fatalf("pending request error = %v, want ErrConnectionClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pending request did not fail after the heartbeat timeout")
	}

	if err := readUntilError(t, client); err == nil {
		t.Fatal("agent socket stayed open after the heartbeat timeout")
	}

	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, ok := env.registry.Get(nodeID); !ok {
			return
		}
		select {
		case <-deadline:
			t.Fatal("node still registered after the heartbeat timeout")
		case <-ticker.C:
		}
	}
}

func TestHelloRecordsWorkspaceRoots(t *testing.T) {
	env := newGatewayEnv(t)
	client := env.connect(t)

	params := defaultHello("roots-box")
	params.WorkspaceRoots = []string{"/work/a", "/work/b"}
	writeHello(t, client, params, "hello-roots")
	if frame := readFrame(t, client); frame.Type != protocol.FrameWelcome {
		t.Fatalf("frame type = %q, want welcome", frame.Type)
	}

	nodes := listNodes(t, env.store)
	if len(nodes) != 1 {
		t.Fatalf("stored nodes = %d, want 1", len(nodes))
	}
	if len(nodes[0].WorkspaceRoots) != 2 || nodes[0].WorkspaceRoots[0] != "/work/a" {
		t.Fatalf("workspace roots = %#v", nodes[0].WorkspaceRoots)
	}
}

// TestGatewayPublishesEventsThroughPublisher 证明读循环把节点事件交给注入的
// 发布器，而不是只写本地 Hub：生产环境靠发布器把事件送进集群总线。
func TestGatewayPublishesEventsThroughPublisher(t *testing.T) {
	env := newGatewayEnv(t)
	client := env.connect(t)
	nodeID, _ := handshake(t, client, "dev-box")

	events, cancel := env.hub.Subscribe(8)
	defer cancel()

	writeEventFrame(t, client, &protocol.Event{
		NodeID:    "node_spoofed",
		SessionID: "ses_1",
		Type:      "session.text.delta",
		Data:      json.RawMessage(`{"text":"hi"}`),
	})

	ev := awaitEvent(t, events, func(ev event.Event) bool { return ev.Type == "session.text.delta" })
	if ev.NodeID != nodeID {
		t.Fatalf("event node_id = %q, want %q", ev.NodeID, nodeID)
	}
	if ev.SessionID != "ses_1" || string(ev.Data) != `{"text":"hi"}` {
		t.Fatalf("event = %+v, want forwarded payload", ev)
	}
}

// TestGatewayPublishesLifecycleEvents 覆盖连接生命周期事件的发布：
// 订阅建立后发 node.connected，断开清理时发 node.offline。
func TestGatewayPublishesLifecycleEvents(t *testing.T) {
	env := newGatewayEnv(t)
	events, cancel := env.hub.Subscribe(8)
	defer cancel()

	client := env.connect(t)
	nodeID, _ := handshake(t, client, "dev-box")

	connected := awaitEvent(t, events, func(ev event.Event) bool { return ev.Type == "node.connected" })
	if connected.NodeID != nodeID {
		t.Fatalf("connected event node_id = %q, want %q", connected.NodeID, nodeID)
	}

	// 主动关闭 agent 侧连接，网关清理路径应当发布离线事件。
	if err := client.Close(); err != nil {
		t.Fatalf("close agent connection: %v", err)
	}

	offline := awaitEvent(t, events, func(ev event.Event) bool { return ev.Type == "node.offline" })
	if offline.NodeID != nodeID {
		t.Fatalf("offline event node_id = %q, want %q", offline.NodeID, nodeID)
	}
	if offline.Time.IsZero() {
		t.Fatal("offline event time is zero, want it filled by the gateway")
	}
}

// TestGatewayForwardsCoreRequest 直接驱动 Core 订阅的 handler，证明跨实例
// 转发把方法调用写到节点连接，并把结果/远端错误编码成 ForwardResponse。
func TestGatewayForwardsCoreRequest(t *testing.T) {
	env := newGatewayEnv(t)
	client := env.connect(t)
	nodeID, _ := handshake(t, client, "dev-box")

	sub := awaitCoreSubscription(t, env.coreSub)
	wantSubject := "cp.node." + nodeID + ".request"
	if sub.subject != wantSubject {
		t.Fatalf("forward subject = %q, want %q", sub.subject, wantSubject)
	}
	if sub.queue != nodeID {
		t.Fatalf("forward queue = %q, want node id %q", sub.queue, nodeID)
	}

	// 先起读协程再接请求：agent 侧必须及时把响应回给等待中的 Connection。
	replyErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		frame, err := client.Read(ctx)
		if err != nil {
			replyErr <- fmt.Errorf("read forwarded request: %w", err)
			return
		}
		if frame.Type != protocol.FrameRequest || frame.Method != "session.list" {
			replyErr <- fmt.Errorf("forwarded frame = %+v, want a session.list request", frame)
			return
		}
		replyErr <- client.Write(ctx, protocol.Frame{
			Type:   protocol.FrameResponse,
			ID:     frame.ID,
			Result: json.RawMessage(`{"ok":true}`),
		})
	}()

	payload, err := json.Marshal(node.ForwardRequest{Method: "session.list"})
	if err != nil {
		t.Fatalf("encode forward request: %v", err)
	}
	raw := sub.handler(context.Background(), payload)
	if err := <-replyErr; err != nil {
		t.Fatalf("agent reply: %v", err)
	}

	var resp node.ForwardResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode forward response %s: %v", raw, err)
	}
	if resp.Error != nil {
		t.Fatalf("forward response error = %+v, want result", resp.Error)
	}
	if string(resp.Result) != `{"ok":true}` {
		t.Fatalf("forward result = %s, want agent result", resp.Result)
	}

	// 第二轮验证远端错误映射：agent 的错误帧必须变成 ForwardError。
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		frame, err := client.Read(ctx)
		if err != nil {
			replyErr <- fmt.Errorf("read forwarded request: %w", err)
			return
		}
		replyErr <- client.Write(ctx, protocol.Frame{
			Type:  protocol.FrameResponse,
			ID:    frame.ID,
			Error: &protocol.FrameError{Code: "not_found", Message: "no such session"},
		})
	}()

	raw = sub.handler(context.Background(), payload)
	if err := <-replyErr; err != nil {
		t.Fatalf("agent reply: %v", err)
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode forward response %s: %v", raw, err)
	}
	if resp.Error == nil {
		t.Fatalf("forward response = %s, want an error", raw)
	}
	if resp.Error.Code != "not_found" || resp.Error.Message != "no such session" {
		t.Fatalf("forward error = %+v, want remote error passthrough", resp.Error)
	}
}

// TestGatewayClosesConnectionOnStatePublishFailure 证明状态事件发布失败必须
// 断开节点：漏掉状态事件会让总览与浏览器永久落后，重连重试比继续更安全。
func TestGatewayClosesConnectionOnStatePublishFailure(t *testing.T) {
	env := newGatewayEnv(t)
	env.gateway.publisher = &hubPublisher{hub: env.hub, failPrefix: "session."}

	client := env.connect(t)
	nodeID, _ := handshake(t, client, "dev-box")

	writeEventFrame(t, client, &protocol.Event{
		SessionID: "ses_1",
		Type:      "session.text.delta",
		Data:      json.RawMessage(`{"text":"hi"}`),
	})

	err := readUntilError(t, client)
	if err == nil {
		t.Fatal("connection stayed open after a state event publish failure")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("connection did not close after the publish failure: %v", err)
	}

	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, ok := env.registry.Get(nodeID); !ok {
			return
		}
		select {
		case <-deadline:
			t.Fatal("node still registered after a state event publish failure")
		case <-ticker.C:
		}
	}
}

// TestGatewayForwardMapsClosedConnectionToOffline 证明路由检查之后连接才断开
// 时仍返回 node_offline，而不是把内部连接错误当成 agent 业务错误。
func TestGatewayForwardMapsClosedConnectionToOffline(t *testing.T) {
	env := newGatewayEnv(t)
	client := env.connect(t)
	nodeID, _ := handshake(t, client, "dev-box")
	sub := awaitCoreSubscription(t, env.coreSub)

	if err := client.Close(); err != nil {
		t.Fatalf("close agent connection: %v", err)
	}

	// 等清理路径注销节点，确保连接自身已经关闭而不仅是底层套接字收到 FIN。
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, ok := env.registry.Get(nodeID); !ok {
			break
		}
		select {
		case <-deadline:
			t.Fatal("node stayed registered after the agent connection closed")
		case <-ticker.C:
		}
	}

	payload, err := json.Marshal(node.ForwardRequest{Method: "session.list"})
	if err != nil {
		t.Fatalf("encode forward request: %v", err)
	}
	raw := sub.handler(context.Background(), payload)

	var resp node.ForwardResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode forward response %s: %v", raw, err)
	}
	if resp.Error == nil {
		t.Fatalf("forward response = %s, want an error after the connection closed", raw)
	}
	if resp.Error.Code != "node_offline" {
		t.Fatalf("forward error code = %q, want node_offline (message %q)", resp.Error.Code, resp.Error.Message)
	}
}

// TestGatewayDropsAgentLifecycleEvents 证明 agent 不能伪造控制面的 node.*
// 命名空间；事件被丢弃，但连接保持可用，不会因一个有 bug 的节点完全失联。
func TestGatewayDropsAgentLifecycleEvents(t *testing.T) {
	env := newGatewayEnv(t)
	events, cancel := env.hub.Subscribe(16)
	defer cancel()

	client := env.connect(t)
	nodeID, _ := handshake(t, client, "dev-box")

	// 先消费网关自己的 node.connected，后续队列里只应有 agent 事件。
	connected := awaitEvent(t, events, func(ev event.Event) bool { return ev.Type == "node.connected" })
	if connected.NodeID != nodeID {
		t.Fatalf("connected event node_id = %q, want %q", connected.NodeID, nodeID)
	}

	for _, typ := range []string{"node.offline", "node.snapshot", "node.connected"} {
		writeEventFrame(t, client, &protocol.Event{
			NodeID: "node_spoofed",
			Type:   typ,
			Data:   json.RawMessage(`{"sessions":[]}`),
		})
	}
	writeEventFrame(t, client, &protocol.Event{
		SessionID: "ses_1",
		Type:      "session.created",
		Data:      json.RawMessage(`{"title":"still alive"}`),
	})

	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-events:
			if strings.HasPrefix(ev.Type, "node.") {
				t.Fatalf("agent-originated reserved event %q was published", ev.Type)
			}
			if ev.Type != "session.created" {
				continue
			}
			if ev.NodeID != nodeID {
				t.Fatalf("event node_id = %q, want %q", ev.NodeID, nodeID)
			}
			// 普通事件仍能通过，说明丢弃保留事件没有断开连接。
			if _, ok := env.registry.Get(nodeID); !ok {
				t.Fatal("node was unregistered after sending reserved events")
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for a normal event after reserved events")
		}
	}
}

// TestGatewayKeepsConnectionOnPTYPublishFailure 证明 PTY 事件是易失数据：
// 发布失败只记日志，连接继续处理心跳、响应与后续事件。
func TestGatewayKeepsConnectionOnPTYPublishFailure(t *testing.T) {
	env := newGatewayEnv(t)
	env.gateway.publisher = &hubPublisher{hub: env.hub, failPrefix: "pty."}

	client := env.connect(t)
	nodeID, _ := handshake(t, client, "dev-box")

	events, cancel := env.hub.Subscribe(8)
	defer cancel()

	writeEventFrame(t, client, &protocol.Event{
		Type: "pty.output",
		Data: json.RawMessage(`{"pty_id":"pty-1","data":"hello"}`),
	})
	writeRawFrame(t, client, protocol.Frame{Type: protocol.FrameHeartbeat})

	// 心跳之后仍然能应答控制面请求，说明读循环没有被 PTY 发布失败打断。
	requestErr := make(chan error, 1)
	ctx, cancelRequest := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRequest()
	go func() {
		_, err := env.registry.Request(ctx, nodeID, "session.list", nil)
		requestErr <- err
	}()

	frame := readFrame(t, client)
	if frame.Type != protocol.FrameRequest || frame.Method != "session.list" {
		t.Fatalf("frame = %+v, want a session.list request", frame)
	}
	writeRawFrame(t, client, protocol.Frame{
		Type:   protocol.FrameResponse,
		ID:     frame.ID,
		Result: json.RawMessage(`{"ok":true}`),
	})
	if err := <-requestErr; err != nil {
		t.Fatalf("request after pty publish failure: %v", err)
	}

	writeEventFrame(t, client, &protocol.Event{
		SessionID: "ses_1",
		Type:      "session.text.delta",
		Data:      json.RawMessage(`{"text":"still here"}`),
	})
	ev := awaitEvent(t, events, func(ev event.Event) bool { return ev.Type == "session.text.delta" })
	if ev.NodeID != nodeID {
		t.Fatalf("event node_id = %q, want %q", ev.NodeID, nodeID)
	}
}
