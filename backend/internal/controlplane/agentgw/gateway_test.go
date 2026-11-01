package agentgw

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/Lework/backend/internal/agentd/protocol"
	"github.com/insmtx/Lework/backend/internal/agentd/transport"
	"github.com/insmtx/Lework/backend/internal/controlplane/event"
	"github.com/insmtx/Lework/backend/internal/controlplane/node"
	"github.com/insmtx/Lework/backend/internal/controlplane/store"
)

// testToken 是测试用注册令牌，只出现在客户端请求头里。
const testToken = "test-enrollment-token-9f3a"

// gatewayEnv 是网关测试的真实依赖集合：SQLite、注册表、事件中心和
// 一个用 httptest 启动的 WebSocket 服务端。
type gatewayEnv struct {
	store    *store.Store
	registry *node.Registry
	hub      *event.Hub
	server   *httptest.Server

	// handlers 统计仍在运行的 ServeHTTP；httptest 在连接被 hijack 后不再
	// 等待 handler，必须由测试自己等它退出，否则会在存储关闭后继续访问。
	handlers sync.WaitGroup
}

func newGatewayEnv(t *testing.T) *gatewayEnv {
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
	}
	gw := New(Config{EnrollmentToken: testToken, HeartbeatTimeout: 5 * time.Second}, env.store, env.registry, env.hub)
	env.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.handlers.Add(1)
		defer env.handlers.Done()
		gw.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		// 先停止接收新请求，再关闭残留连接，最后等所有 handler 退出。
		env.server.Close()
		env.registry.CloseAll()
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
	NodeName     string   `json:"node_name"`
	AgentVersion string   `json:"agent_version"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	Capabilities []string `json:"capabilities"`
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

func listNodes(t *testing.T, st *store.Store) []store.Node {
	t.Helper()

	nodes, err := st.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	return nodes
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

	info, ok := env.registry.Get(nodeID)
	if !ok {
		t.Fatalf("registry has no node %q", nodeID)
	}
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
	info, ok := env.registry.Get(firstID)
	if !ok || !info.Connected {
		t.Fatalf("registry entry = %+v (ok=%v), want connected", info, ok)
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

	select {
	case ev := <-events:
		if ev.NodeID != nodeID {
			t.Fatalf("event node_id = %q, want %q", ev.NodeID, nodeID)
		}
		if ev.Type != "session.text.delta" || ev.SessionID != "ses_1" {
			t.Fatalf("event = %+v, want forwarded payload", ev)
		}
		if string(ev.Data) != `{"text":"hi"}` {
			t.Fatalf("event data = %s, want original payload", ev.Data)
		}
		if ev.Time.IsZero() {
			t.Fatal("event time is zero, want it filled by the gateway")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the published event")
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
