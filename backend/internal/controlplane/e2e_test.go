// Package controlplane_test 包含控制面的进程内纵向验收测试。
//
// 该测试使用真实控制面（HTTP API + agent 网关 + SQLite 存储）与真实 agent
// runner，仅把 OpenCode 客户端与 supervisor 替换为桩实现，因此可以在没有
// opencode 二进制、没有模型调用的环境下验证 I1-A 的完整链路。
package controlplane_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	logsconfig "github.com/ygpkg/yg-go/config"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Lework/backend/internal/agentd/handler"
	"github.com/insmtx/Lework/backend/internal/agentd/opencode"
	"github.com/insmtx/Lework/backend/internal/agentd/runner"
	"github.com/insmtx/Lework/backend/internal/agentd/supervisor"
	"github.com/insmtx/Lework/backend/internal/agentd/transport"
	"github.com/insmtx/Lework/backend/internal/controlplane"
	"github.com/insmtx/Lework/backend/internal/controlplane/config"
	"github.com/insmtx/Lework/backend/internal/controlplane/store"
)

const (
	// e2eToken 是注册令牌。测试断言它不会出现在任何 API 响应或日志中。
	e2eToken = "test-token"

	// e2eNodeName 是重连时复用的稳定节点名：控制面按名称登记节点，
	// 因此同名重连必须复用同一个节点 ID。
	e2eNodeName = "e2e-node"

	// e2eSessionID 是桩 OpenCode 返回的会话 ID。
	e2eSessionID = "session-1"

	// e2eSessionTitle 是桩 OpenCode 返回的会话标题。
	e2eSessionTitle = "stub session"

	// e2eReceipt 是桩 OpenCode 返回的提示词回执，prompt 响应必须原样返回它。
	e2eReceipt = "receipt-stub-1"

	// e2eDirectory 是创建会话时使用的绝对工作目录。
	e2eDirectory = "/work"

	// e2eIdempotencyKey 是提示词幂等键，重复请求必须复用同一次投递。
	e2eIdempotencyKey = "k1"

	// e2ePromptText 是提示词内容；只用于桩调用，不进入断言之外的日志。
	e2ePromptText = "hello"

	// e2ePollInterval 是节点列表轮询间隔。
	e2ePollInterval = 25 * time.Millisecond

	// e2eNodeTimeout 是等待节点上线/离线的上限。
	e2eNodeTimeout = 5 * time.Second

	// e2eHTTPTimeout 是单次浏览器 API 调用的超时。
	e2eHTTPTimeout = 5 * time.Second

	// e2eLogSentinel 在日志重定向生效后写入一次，用来证明日志文件确实被捕获；
	// 缺少它说明“日志里没有令牌”的断言是空检查。
	e2eLogSentinel = "e2e-log-capture-sentinel"
)

// TestEndToEnd 是 I1-A 的验收测试：真实控制面 + 真实 agent runner + 桩 OpenCode。
func TestEndToEnd(t *testing.T) {
	logPath := captureLogs(t)

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	srv, err := controlplane.New(config.Config{
		ListenAddr:       "127.0.0.1:0",
		DBPath:           ":memory:",
		EnrollmentToken:  e2eToken,
		RequestTimeout:   e2eHTTPTimeout,
		HeartbeatTimeout: 5 * time.Second,
	}, st)
	if err != nil {
		_ = st.Close()
		t.Fatalf("new control plane: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Errorf("close control plane: %v", err)
		}
	})

	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/api/agent/connect"
	api := &testAPI{t: t, baseURL: httpServer.URL, client: &http.Client{Timeout: e2eHTTPTimeout}}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stubOpenCode := newCountingOpenCode()
	stubService := &stubSupervisor{status: supervisor.Status{
		Running: true,
		Mode:    "managed",
		BaseURL: "http://127.0.0.1:4096",
	}}

	agent := startAgent(t, ctx, wsURL, stubOpenCode, stubService)
	t.Cleanup(func() { agent.stop(t) })

	// 1. 节点注册：真实 gateway 读取 hello、登记存储并写入注册表。
	nodes := api.waitForNodes(ctx, e2eNodeTimeout, "one connected e2e-node", func(nodes []nodeView) bool {
		return len(nodes) == 1 && nodes[0].Name == e2eNodeName && nodes[0].Connected
	})
	node := nodes[0]
	if !strings.HasPrefix(node.ID, "node_") {
		t.Fatalf("node id = %q, want a node_ prefix", node.ID)
	}
	if node.AgentVersion != "test" {
		t.Fatalf("node agent_version = %q, want %q", node.AgentVersion, "test")
	}
	t.Logf("node registered as %s", node.ID)

	// 2. 创建会话：浏览器 API 把请求转发给 agent，桩返回会话 JSON。
	status, body := api.doJSON(ctx, http.MethodPost, "/api/sessions",
		fmt.Sprintf(`{"node_id":%q,"directory":%q}`, node.ID, e2eDirectory))
	if status != http.StatusOK {
		t.Fatalf("create session status = %d, want 200 (body=%s)", status, body)
	}
	var session struct {
		ID        string `json:"id"`
		Directory string `json:"directory"`
		Title     string `json:"title"`
	}
	if err := json.Unmarshal(body, &session); err != nil {
		t.Fatalf("decode session %s: %v", body, err)
	}
	if session.ID != e2eSessionID || session.Directory != e2eDirectory || session.Title != e2eSessionTitle {
		t.Fatalf("session = %+v, want id %q in %q titled %q", session, e2eSessionID, e2eDirectory, e2eSessionTitle)
	}

	promptPath := fmt.Sprintf("/api/sessions/%s/%s/prompt", node.ID, e2eSessionID)
	promptBody := fmt.Sprintf(`{"text":%q,"idempotency_key":%q}`, e2ePromptText, e2eIdempotencyKey)

	// 3. 首次提示词：返回桩回执，且只投递一次。
	status, body = api.doJSON(ctx, http.MethodPost, promptPath, promptBody)
	if status != http.StatusOK {
		t.Fatalf("first prompt status = %d, want 200 (body=%s)", status, body)
	}
	if got := decodeReceipt(t, body); got != e2eReceipt {
		t.Fatalf("first prompt receipt = %q, want %q", got, e2eReceipt)
	}
	if got := stubOpenCode.PromptCount(); got != 1 {
		t.Fatalf("opencode prompt calls = %d, want 1", got)
	}

	// 4. 同一幂等键重试：回执相同，OpenCode 仍然只被调用一次。
	status, body = api.doJSON(ctx, http.MethodPost, promptPath, promptBody)
	if status != http.StatusOK {
		t.Fatalf("repeated prompt status = %d, want 200 (body=%s)", status, body)
	}
	if got := decodeReceipt(t, body); got != e2eReceipt {
		t.Fatalf("repeated prompt receipt = %q, want %q", got, e2eReceipt)
	}
	if got := stubOpenCode.PromptCount(); got != 1 {
		t.Fatalf("opencode prompt calls after retry = %d, want 1 (idempotency key must be replayed)", got)
	}

	// 5. 读取消息：浏览器 API 原样透传桩返回的消息数组。
	messagesPath := fmt.Sprintf("/api/sessions/%s/%s/messages", node.ID, e2eSessionID)
	status, body = api.doJSON(ctx, http.MethodGet, messagesPath, "")
	if status != http.StatusOK {
		t.Fatalf("messages status = %d, want 200 (body=%s)", status, body)
	}
	var messages []struct {
		ID   string `json:"id"`
		Role string `json:"role"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &messages); err != nil {
		t.Fatalf("decode messages %s: %v", body, err)
	}
	wantMessages := stubOpenCode.messageList()
	if len(messages) != len(wantMessages) {
		t.Fatalf("messages = %+v, want %d entries", messages, len(wantMessages))
	}
	for i, want := range wantMessages {
		if messages[i].ID != want.ID || messages[i].Role != want.Role || messages[i].Text != want.Text {
			t.Fatalf("messages[%d] = %+v, want %+v", i, messages[i], want)
		}
	}

	// 6. 断开并重连：同名节点必须复用同一个控制面节点 ID。
	agent.stop(t)
	api.waitForNodes(ctx, e2eNodeTimeout, "the node to disconnect", func(nodes []nodeView) bool {
		return len(nodes) == 0 || (len(nodes) == 1 && !nodes[0].Connected)
	})

	reconnected := startAgent(t, ctx, wsURL, stubOpenCode, stubService)
	t.Cleanup(func() { reconnected.stop(t) })

	nodes = api.waitForNodes(ctx, e2eNodeTimeout, "the reconnected node", func(nodes []nodeView) bool {
		return len(nodes) == 1 && nodes[0].Connected && nodes[0].ID == node.ID
	})
	if nodes[0].Name != e2eNodeName {
		t.Fatalf("reconnected node name = %q, want %q", nodes[0].Name, e2eNodeName)
	}
	t.Logf("node %s reconnected with the same id", nodes[0].ID)

	// 7. 机密性：注册令牌绝不能出现在任何 API 响应或日志中。
	api.assertNoSecret(t, e2eToken)
	assertLogCapture(t, logPath, e2eLogSentinel, e2eToken)
}

// countingOpenCode 是 handler.OpenCodeAPI 与 runner.EventSource 的桩实现。
// 它记录 Prompt 调用次数，用来验证幂等键只投递一次。
type countingOpenCode struct {
	prompts atomic.Int32
}

// newCountingOpenCode 创建一个只有固定返回值的桩 OpenCode。
func newCountingOpenCode() *countingOpenCode {
	return &countingOpenCode{}
}

// ListSessions 返回空会话列表。
func (c *countingOpenCode) ListSessions(context.Context) ([]opencode.Session, error) {
	return []opencode.Session{}, nil
}

// CreateSession 返回固定会话；目录沿用调用方请求，便于校验透传。
func (c *countingOpenCode) CreateSession(_ context.Context, directory string) (opencode.Session, error) {
	return opencode.Session{ID: e2eSessionID, Directory: directory, Title: e2eSessionTitle}, nil
}

// Prompt 记录调用次数并返回固定回执。
func (c *countingOpenCode) Prompt(context.Context, string, string) (string, error) {
	c.prompts.Add(1)
	return e2eReceipt, nil
}

// Messages 返回固定的用户/助手消息。
func (c *countingOpenCode) Messages(context.Context, string) ([]opencode.Message, error) {
	return c.messageList(), nil
}

// ReplyPermission 是空实现：本测试不产生权限请求。
func (c *countingOpenCode) ReplyPermission(context.Context, string, string, string, string) error {
	return nil
}

// SessionDiff 返回空 diff。
func (c *countingOpenCode) SessionDiff(context.Context, string, string, string, int) ([]opencode.FileDiff, error) {
	return []opencode.FileDiff{}, nil
}

// Interrupt 返回未中断状态。
func (c *countingOpenCode) Interrupt(context.Context, string, bool) (opencode.InterruptResult, error) {
	return opencode.InterruptResult{}, nil
}

// ListFiles 返回空列表。
func (c *countingOpenCode) ListFiles(context.Context, string, string) ([]opencode.FileInfo, error) {
	return []opencode.FileInfo{}, nil
}

// ReadFile 返回空内容。
func (c *countingOpenCode) ReadFile(context.Context, string, string, int64) (opencode.FileContent, error) {
	return opencode.FileContent{}, nil
}

// CreatePersistentTerminal 返回固定终端。
func (c *countingOpenCode) CreatePersistentTerminal(context.Context, string, opencode.CreateTerminalRequest) (opencode.PersistentTerminal, error) {
	return opencode.PersistentTerminal{ID: "pty_e2e"}, nil
}

// ConnectPersistentTerminal 不支持真实连接。
func (c *countingOpenCode) ConnectPersistentTerminal(context.Context, string) (*opencode.TerminalConn, error) {
	return nil, errors.New("e2e: no terminal connection")
}

// ResizePersistentTerminal 是空实现。
func (c *countingOpenCode) ResizePersistentTerminal(context.Context, string, opencode.TerminalSize) error {
	return nil
}

// ClosePersistentTerminal 是空实现。
func (c *countingOpenCode) ClosePersistentTerminal(context.Context, string) error {
	return nil
}

// SubscribeEvents 返回一个保持打开的事件通道，直到测试上下文结束。
func (c *countingOpenCode) SubscribeEvents(ctx context.Context) (<-chan opencode.Event, error) {
	events := make(chan opencode.Event)
	go func() {
		<-ctx.Done()
		close(events)
	}()
	return events, nil
}

// PromptCount 返回 Prompt 被调用的次数。
func (c *countingOpenCode) PromptCount() int {
	return int(c.prompts.Load())
}

// messageList 返回桩消息副本，避免调用方修改共享切片。
func (c *countingOpenCode) messageList() []opencode.Message {
	return []opencode.Message{
		{ID: "msg-1", Role: "user", Text: e2ePromptText},
		{ID: "msg-2", Role: "assistant", Text: "hi from the stub"},
	}
}

// stubSupervisor 是 handler.SupervisorAPI 的空实现。
type stubSupervisor struct {
	status supervisor.Status
}

// Ensure 返回固定状态。
func (s *stubSupervisor) Ensure(context.Context) (supervisor.Status, error) {
	return s.status, nil
}

// Restart 始终成功。
func (s *stubSupervisor) Restart(context.Context) error { return nil }

// Stop 始终成功。
func (s *stubSupervisor) Stop(context.Context) error { return nil }

// Status 返回固定状态快照。
func (s *stubSupervisor) Status() supervisor.Status { return s.status }

// agentConn 跟踪一条真实 agent runner 连接。
//
// stop 是幂等的：正常路径显式停止，失败路径由 t.Cleanup 兜底，二者不会
// 重复等待同一个 done 通道。
type agentConn struct {
	conn   *transport.Client
	cancel context.CancelFunc
	done   chan error

	stopOnce sync.Once
	stopErr  error
}

// startAgent 建立 WebSocket、启动真实 runner，并在测试结束时由调用方停止。
func startAgent(
	t *testing.T,
	parent context.Context,
	wsURL string,
	openCode *countingOpenCode,
	service *stubSupervisor,
) *agentConn {
	t.Helper()

	conn, err := transport.Dial(parent, transport.DialConfig{
		URL:    wsURL,
		Header: http.Header{"Authorization": {"Bearer " + e2eToken}},
	})
	if err != nil {
		t.Fatalf("dial control plane: %v", err)
	}

	runCtx, cancel := context.WithCancel(parent)
	done := make(chan error, 1)
	requestHandler := handler.New(openCode, service)
	go func() {
		done <- runner.New(conn, requestHandler, openCode, runner.RunnerConfig{
			NodeName:     e2eNodeName,
			AgentVersion: "test",
			Heartbeat:    50 * time.Millisecond,
		}).Run(runCtx)
	}()

	return &agentConn{conn: conn, cancel: cancel, done: done}
}

// stop 取消并关闭连接，等待 runner 干净退出；重复调用直接复用首次结果。
func (a *agentConn) stop(t *testing.T) {
	t.Helper()

	a.stopOnce.Do(func() {
		a.cancel()
		_ = a.conn.Close()

		select {
		case a.stopErr = <-a.done:
		case <-time.After(e2eNodeTimeout):
			a.stopErr = fmt.Errorf("runner did not stop within %s", e2eNodeTimeout)
		}
	})

	if a.stopErr != nil && !errors.Is(a.stopErr, context.Canceled) {
		t.Errorf("runner returned %v, want a clean shutdown", a.stopErr)
	}
}

// nodeView 是 GET /api/nodes 的响应元素。
type nodeView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	AgentVersion string `json:"agent_version"`
	Connected    bool   `json:"connected"`
}

// testAPI 是带超时与响应记录的浏览器 API 客户端。
type testAPI struct {
	t       *testing.T
	baseURL string
	client  *http.Client

	// bodies 收集全部响应体，用于证明注册令牌不会泄漏到 API 输出。
	bodies [][]byte
}

// doJSON 发送一次带超时的请求并返回状态码与响应体。
func (c *testAPI) doJSON(ctx context.Context, method, path, body string) (int, []byte) {
	c.t.Helper()

	callCtx, cancel := context.WithTimeout(ctx, e2eHTTPTimeout)
	defer cancel()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(callCtx, method, c.baseURL+path, reader)
	if err != nil {
		c.t.Fatalf("new %s %s request: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read %s %s response: %v", method, path, err)
	}
	c.bodies = append(c.bodies, data)
	return resp.StatusCode, data
}

// listNodes 读取节点列表；非 200 视为错误，便于轮询重试。
func (c *testAPI) listNodes(ctx context.Context) ([]nodeView, error) {
	status, body := c.doJSON(ctx, http.MethodGet, "/api/nodes", "")
	if status != http.StatusOK {
		return nil, fmt.Errorf("GET /api/nodes status %d: %s", status, body)
	}
	var nodes []nodeView
	if err := json.Unmarshal(body, &nodes); err != nil {
		return nil, fmt.Errorf("decode nodes %s: %w", body, err)
	}
	return nodes, nil
}

// waitForNodes 轮询节点列表直到条件满足或超时。
func (c *testAPI) waitForNodes(
	ctx context.Context,
	timeout time.Duration,
	description string,
	condition func([]nodeView) bool,
) []nodeView {
	c.t.Helper()

	deadline := time.Now().Add(timeout)
	var (
		last    []nodeView
		lastErr error
	)
	for {
		last, lastErr = c.listNodes(ctx)
		if lastErr == nil && condition(last) {
			return last
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("timed out after %s waiting for %s: nodes=%+v err=%v", timeout, description, last, lastErr)
		}
		select {
		case <-ctx.Done():
			c.t.Fatalf("context ended while waiting for %s: %v", description, ctx.Err())
		case <-time.After(e2ePollInterval):
		}
	}
}

// assertNoSecret 确认注册令牌不在任何已记录的响应体中。
func (c *testAPI) assertNoSecret(t *testing.T, secret string) {
	t.Helper()

	for i, body := range c.bodies {
		if bytes.Contains(body, []byte(secret)) {
			t.Fatalf("response %d leaked the enrollment token: %s", i, body)
		}
	}
}

// decodeReceipt 解码 prompt 响应中的 JSON 字符串回执。
func decodeReceipt(t *testing.T, body []byte) string {
	t.Helper()

	var receipt string
	if err := json.Unmarshal(body, &receipt); err != nil {
		t.Fatalf("decode receipt %s: %v", body, err)
	}
	return receipt
}

// captureLogs 把默认日志重定向到临时文件并返回其路径。重载后立即通过同一个
// yg-go 日志实例写入固定哨兵并校验文件，确认捕获确实生效；测试结束时恢复
// 控制台输出。
func captureLogs(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "controlplane.log")
	reloadLogs(t, logsconfig.LogsConfig{
		"default": {{Writer: "file", Encoder: "default", Level: zapcore.InfoLevel, Logger: &lumberjack.Logger{Filename: path}}},
	})

	// 没有这次写入，日志文件可能从未创建，后续“不含令牌”的断言将永远通过。
	logs.Infof("%s", e2eLogSentinel)
	_ = logs.Sync()
	assertLogCapture(t, path, e2eLogSentinel, "")

	t.Cleanup(func() {
		// 先刷新文件写入端，再恢复控制台输出，保证断言看到完整日志。
		_ = logs.Sync()
		reloadLogs(t, logsconfig.LogsConfig{
			"default": {{Writer: "console", Encoder: "default", Level: zapcore.InfoLevel}},
		})
	})
	return path
}

// reloadLogs 用给定配置重载默认日志实例。
func reloadLogs(t *testing.T, cfg logsconfig.LogsConfig) {
	t.Helper()

	if err := logs.ReloadConfig("-", cfg); err != nil {
		t.Fatalf("reload logs: %v", err)
	}
}

// checkLogCapture 校验日志文件真实存在、包含哨兵且不包含机密。secret 为空时
// 只校验哨兵，供 captureLogs 在测试早期确认捕获生效。
func checkLogCapture(path, sentinel, secret string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read log file %q: %w", path, err)
	}
	if !bytes.Contains(data, []byte(sentinel)) {
		return fmt.Errorf("log file %q does not contain sentinel %q", path, sentinel)
	}
	if secret != "" && bytes.Contains(data, []byte(secret)) {
		return fmt.Errorf("logs leaked the enrollment token: %s", data)
	}
	return nil
}

// assertLogCapture 要求日志捕获真实生效（文件存在且含哨兵）并且不含机密。
func assertLogCapture(t *testing.T, path, sentinel, secret string) {
	t.Helper()

	// 文件写入端可能还有缓冲，读取前先刷新。
	_ = logs.Sync()
	if err := checkLogCapture(path, sentinel, secret); err != nil {
		t.Fatal(err)
	}
}

// TestLogCaptureRequiresSentinel 证明日志断言不是空检查：文件缺失或缺少哨兵
// 都必须失败，只有捕获生效且不含机密时才算通过。
func TestLogCaptureRequiresSentinel(t *testing.T) {
	dir := t.TempDir()
	sentinel := e2eLogSentinel
	secret := e2eToken

	withSentinel := filepath.Join(dir, "with-sentinel.log")
	if err := os.WriteFile(withSentinel, []byte("info "+sentinel+"\n"), 0o600); err != nil {
		t.Fatalf("write log fixture: %v", err)
	}
	withoutSentinel := filepath.Join(dir, "without-sentinel.log")
	if err := os.WriteFile(withoutSentinel, []byte("info other\n"), 0o600); err != nil {
		t.Fatalf("write log fixture: %v", err)
	}
	leaking := filepath.Join(dir, "leaking.log")
	if err := os.WriteFile(leaking, []byte("info "+sentinel+" token="+secret+"\n"), 0o600); err != nil {
		t.Fatalf("write log fixture: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "missing file", path: filepath.Join(dir, "missing.log"), wantErr: true},
		{name: "missing sentinel", path: withoutSentinel, wantErr: true},
		{name: "token leaked", path: leaking, wantErr: true},
		{name: "captured without secret", path: withSentinel, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkLogCapture(tt.path, sentinel, secret)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkLogCapture(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}
