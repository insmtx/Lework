package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/config"
	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/overview"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// testToken 是测试用注册令牌；只参与校验，不写入日志。
const testToken = "test-enrollment-token"

// testConfig 返回可用的控制面配置，数据库放在内存里。
func testConfig() config.Config {
	cfg := config.Default()
	cfg.EnrollmentToken = testToken
	cfg.DBPath = ":memory:"
	cfg.ListenAddr = "127.0.0.1:0"
	return cfg
}

// newTestServer 用内存存储组装一台控制面，测试结束时关闭。
func newTestServer(t *testing.T) *Server {
	t.Helper()

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	srv, err := New(testConfig(), st)
	if err != nil {
		_ = st.Close()
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Errorf("close server: %v", err)
		}
	})
	return srv
}

// TestServerNewRejectsInvalidConfig 覆盖启动前的配置校验。
func TestServerNewRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Config)
		want   string
	}{
		{
			name:   "empty token",
			mutate: func(cfg *config.Config) { cfg.EnrollmentToken = " " },
			want:   "enrollment_token",
		},
		{
			// 非回环地址是合法绑定，校验只拒绝畸形 host:port。
			name:   "malformed listen",
			mutate: func(cfg *config.Config) { cfg.ListenAddr = "8787" },
			want:   "host:port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })

			cfg := testConfig()
			tt.mutate(&cfg)

			srv, err := New(cfg, st)
			if err == nil {
				_ = srv.Close()
				t.Fatal("New accepted an invalid config")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("New error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

// TestServerHealthz 校验健康检查端点。
func TestServerHealthz(t *testing.T) {
	srv := newTestServer(t)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode healthz body %q: %v", rec.Body.String(), err)
	}
	if body.Status != "ok" {
		t.Fatalf("status field = %q, want %q", body.Status, "ok")
	}
}

// loopbackRequest 构造来自本机回环的请求；鉴权中间件对无转发头的本机请求放行。
func loopbackRequest(method, target string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = "127.0.0.1:1234"
	return req
}

// TestServerListNodesEmpty 校验空库返回空 JSON 数组。
func TestServerListNodesEmpty(t *testing.T) {
	srv := newTestServer(t)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/nodes"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var views []json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatalf("decode nodes body %q: %v", rec.Body.String(), err)
	}
	if len(views) != 0 {
		t.Fatalf("nodes = %d, want 0", len(views))
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("nodes body = %q, want %q", got, "[]")
	}
}

// TestServerHandlerWithHTTPTestServer 校验 Handler 可直接交给标准库服务器。
func TestServerHandlerWithHTTPTestServer(t *testing.T) {
	srv := newTestServer(t)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("get healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestServerCloseIsIdempotent 校验 Close 关闭存储且可重复调用。
func TestServerCloseIsIdempotent(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	srv, err := New(testConfig(), st)
	if err != nil {
		_ = st.Close()
		t.Fatalf("new server: %v", err)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if _, err := st.ListNodes(context.Background()); err == nil {
		t.Fatal("store is still usable after Close")
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

// TestServerRunStopsOnContextCancel 校验 Run 在上下文取消后正常退出。
func TestServerRunStopsOnContextCancel(t *testing.T) {
	cfg := testConfig()
	cfg.ListenAddr = freeLoopbackAddr(t)

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	srv, err := New(cfg, st)
	if err != nil {
		_ = st.Close()
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()

	waitForHealthz(t, cfg.ListenAddr)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned %v, want nil after context cancellation", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

// TestServerRunShutsDownOpenEventStream 校验持有 SSE 长连接时取消上下文仍能及时退出。
//
// 浏览器界面会常驻 /api/events，http.Server.Shutdown 本身不会取消请求上下文；
// 如果服务端不主动取消，优雅退出会一直等到超时并最终报错。
func TestServerRunShutsDownOpenEventStream(t *testing.T) {
	cfg := testConfig()
	cfg.ListenAddr = freeLoopbackAddr(t)

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	srv, err := New(cfg, st)
	if err != nil {
		_ = st.Close()
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	runCtx, cancelRun := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(runCtx) }()

	waitForHealthz(t, cfg.ListenAddr)

	// 模拟浏览器订阅事件流：客户端连接独立于 Run 的上下文保持打开。
	streamCtx, cancelStream := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStream()

	url := fmt.Sprintf("http://%s/api/events", cfg.ListenAddr)
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new events request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("event stream status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("event stream content type = %q, want text/event-stream", got)
	}

	cancelRun()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned %v, want nil while an event stream was open", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s while an event stream was open")
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
}

// freeLoopbackAddr 预占一个回环端口后立即释放，供 Run 绑定。
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release loopback port: %v", err)
	}
	return addr
}

// waitForHealthz 轮询健康检查，直到服务器开始监听或超时。
func waitForHealthz(t *testing.T, addr string) {
	t.Helper()

	client := &http.Client{Timeout: 500 * time.Millisecond}
	url := fmt.Sprintf("http://%s/healthz", addr)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s never became healthy", addr)
}

// TestServerServesUIOrBuildPlaceholder covers both checkout states: a release
// build embeds a real index, while a clean checkout has only dist/.gitkeep.
func TestServerServesUIOrBuildPlaceholder(t *testing.T) {
	srv := newTestServer(t)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/"))
	if rec.Code != http.StatusOK {
		t.Fatalf("UI status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := strings.TrimSpace(rec.Body.String()); body == "" {
		t.Fatal("UI body is empty")
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("UI content type = %q, want text/html", got)
	}
}

// TestServerUnknownAPIPathIsNotSPA prevents a mistyped API route from returning
// the browser shell with a 200.
func TestServerUnknownAPIPathIsNotSPA(t *testing.T) {
	srv := newTestServer(t)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/does-not-exist"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type = %q, want application/json", got)
	}
}

func assertOverviewState(t *testing.T, idx *overview.Index, nodeID, sessionID string, want overview.State) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, n := range idx.Snapshot() {
			if n.NodeID != nodeID {
				continue
			}
			for _, s := range n.Sessions {
				if s.SessionID == sessionID && s.State == want {
					return
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("overview never reached %q for %s/%s: %+v", want, nodeID, sessionID, idx.Snapshot())
}

func TestMonitorFeedsOverview(t *testing.T) {
	srv := newTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.startMonitor(ctx)

	srv.hub.Publish(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "session.created"})
	assertOverviewState(t, srv.index, "node_1", "ses_1", overview.StateRunning)

	srv.index.Apply(event.Event{NodeID: "node_1", Type: "agent.resync_required"})
	if got := srv.index.Snapshot(); len(got) != 0 {
		t.Fatalf("snapshot = %+v, want empty after resync", got)
	}
}

// closingSubscriber 第一次订阅立刻关闭，之后投递一条事件再关闭，
// 用来证明索引订阅被 Hub 淘汰后会自动重订。
type closingSubscriber struct {
	mu    sync.Mutex
	calls int
}

func (c *closingSubscriber) Subscribe(int) (<-chan event.Event, func()) {
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.mu.Unlock()

	ch := make(chan event.Event, 4)
	if n > 1 {
		ch <- event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "session.created"}
	}
	close(ch)
	return ch, func() {}
}

func (c *closingSubscriber) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestMonitorResubscribesAfterEviction(t *testing.T) {
	srv := newTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := &closingSubscriber{}
	srv.startMonitorWith(ctx, sub)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(srv.index.Snapshot()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(srv.index.Snapshot()) == 0 {
		t.Fatal("overview never recovered after the subscription was closed")
	}
	if sub.count() < 2 {
		t.Fatalf("Subscribe calls = %d, want at least 2", sub.count())
	}
}

// TestServerAuthGate 校验非本机请求被令牌中间件拦截，健康检查保持豁免。
func TestServerAuthGate(t *testing.T) {
	srv := newTestServer(t)

	tests := []struct {
		name   string
		target string
		want   int
	}{
		{name: "remote api denied", target: "/api/nodes", want: http.StatusUnauthorized},
		{name: "healthz exempt", target: "/healthz", want: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			req.RemoteAddr = "203.0.113.5:1"
			rec := httptest.NewRecorder()

			srv.Handler().ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tt.want, rec.Body)
			}
			if strings.Contains(rec.Body.String(), testToken) {
				t.Fatalf("response leaked the enrollment token: %s", rec.Body)
			}
		})
	}
}
