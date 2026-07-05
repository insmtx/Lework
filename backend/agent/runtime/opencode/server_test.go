package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/agent"
)

// ============================================================================
// 健康检查单元测试
// ============================================================================

func TestCheckHealthOK(t *testing.T) {
	srv := &OpenCodeServer{
		baseURL:      "http://127.0.0.1:9999",
		authHeader:   "Basic dGVzdDp0ZXN0",
		healthClient: fakeHealthClient(200, `{"healthy":true,"version":"1.0"}`),
	}
	result, status, body := srv.checkHealth(context.Background())
	if result != healthOK {
		t.Fatalf("result=%d, want healthOK", result)
	}
	if status != 200 {
		t.Fatalf("status=%d, want 200", status)
	}
	if body != `{"healthy":true,"version":"1.0"}` {
		t.Fatalf("body=%q", body)
	}
}

func TestCheckHealthNotHealthy(t *testing.T) {
	srv := &OpenCodeServer{
		baseURL:      "http://127.0.0.1:9999",
		authHeader:   "Basic dGVzdDp0ZXN0",
		healthClient: fakeHealthClient(200, `{"healthy":false,"version":"1.0"}`),
	}
	result, _, _ := srv.checkHealth(context.Background())
	if result != healthConnRefused {
		t.Fatalf("result=%d, want healthConnRefused (keep polling)", result)
	}
}

func TestCheckHealthFatal401(t *testing.T) {
	srv := &OpenCodeServer{
		baseURL:      "http://127.0.0.1:9999",
		authHeader:   "Basic dGVzdDp0ZXN0",
		healthClient: fakeHealthClient(401, `{"error":"unauthorized"}`),
	}
	result, status, body := srv.checkHealth(context.Background())
	if result != healthFatal {
		t.Fatalf("result=%d, want healthFatal", result)
	}
	if status != 401 {
		t.Fatalf("status=%d, want 401", status)
	}
	if body != `{"error":"unauthorized"}` {
		t.Fatalf("body=%q", body)
	}
}

func TestCheckHealthFatal403(t *testing.T) {
	srv := &OpenCodeServer{
		baseURL:      "http://127.0.0.1:9999",
		authHeader:   "Basic dGVzdDp0ZXN0",
		healthClient: fakeHealthClient(403, `forbidden`),
	}
	result, _, _ := srv.checkHealth(context.Background())
	if result != healthFatal {
		t.Fatalf("result=%d, want healthFatal", result)
	}
}

func TestCheckHealthFatal404(t *testing.T) {
	srv := &OpenCodeServer{
		baseURL:      "http://127.0.0.1:9999",
		authHeader:   "Basic dGVzdDp0ZXN0",
		healthClient: fakeHealthClient(404, `not found`),
	}
	result, _, _ := srv.checkHealth(context.Background())
	if result != healthFatal {
		t.Fatalf("result=%d, want healthFatal", result)
	}
}

func TestCheckHealthFatalBadJSON(t *testing.T) {
	srv := &OpenCodeServer{
		baseURL:      "http://127.0.0.1:9999",
		authHeader:   "Basic dGVzdDp0ZXN0",
		healthClient: fakeHealthClient(200, `not json`),
	}
	result, _, _ := srv.checkHealth(context.Background())
	if result != healthFatal {
		t.Fatalf("result=%d, want healthFatal (bad JSON)", result)
	}
}

func TestCheckHealthDegraded503(t *testing.T) {
	srv := &OpenCodeServer{
		baseURL:      "http://127.0.0.1:9999",
		authHeader:   "Basic dGVzdDp0ZXN0",
		healthClient: fakeHealthClient(503, `service unavailable`),
	}
	result, _, _ := srv.checkHealth(context.Background())
	if result != healthDegraded {
		t.Fatalf("result=%d, want healthDegraded", result)
	}
}

func TestCheckHealthUsesHealthClient(t *testing.T) {
	// 验证 checkHealth 使用 healthClient 而非 httpClient
	var gotTransport bool
	srv := &OpenCodeServer{
		baseURL:    "http://127.0.0.1:9999",
		authHeader: "Basic dGVzdDp0ZXN0",
		healthClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				gotTransport = true
				return okResponseJSON(r, `{"healthy":true}`)
			}),
		},
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	result, _, _ := srv.checkHealth(context.Background())
	if result != healthOK {
		t.Fatalf("result=%d, want healthOK", result)
	}
	if !gotTransport {
		t.Fatal("health check should use healthClient transport")
	}
}

// ============================================================================
// waitHealthy 测试（使用 mock channel）
// ============================================================================

func TestWaitHealthyExitsEarlyOnPrematureProcessExit(t *testing.T) {
	waitCh := make(chan error, 1)
	close(waitCh) // 模拟进程立即退出（零值 error = 成功退出）

	srv := &OpenCodeServer{
		baseURL:            "http://127.0.0.1:9999",
		healthClient:       fakeHealthClient(200, `{"healthy":true}`),
		healthCheckTimeout: 10 * time.Second,
		waitCh:             waitCh,
		done:               make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err := srv.waitHealthy(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error on premature exit")
	}
	if !strings.Contains(err.Error(), "exited prematurely") {
		t.Fatalf("error = %v, want 'exited prematurely'", err)
	}
	if elapsed > 1*time.Second {
		t.Fatalf("elapsed=%v, should return immediately on process exit", elapsed)
	}
}

func TestWaitHealthyExitsEarlyOnProcessError(t *testing.T) {
	// 进程异常退出（如 crash），waitCh 包含非 nil 错误
	waitCh := make(chan error, 1)
	waitCh <- &exec.ExitError{}

	srv := &OpenCodeServer{
		baseURL:            "http://127.0.0.1:9999",
		healthClient:       fakeHealthClient(200, `{"healthy":true}`),
		healthCheckTimeout: 10 * time.Second,
		waitCh:             waitCh,
		done:               make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err := srv.waitHealthy(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error on exit error")
	}
	if !strings.Contains(err.Error(), "exited prematurely") {
		t.Fatalf("error = %v, want 'exited prematurely'", err)
	}
	if elapsed > 1*time.Second {
		t.Fatalf("elapsed=%v, should return immediately", elapsed)
	}
}

func TestStartOpenCodeServerUsesIndependentTimeoutForEveryAttempt(t *testing.T) {
	const attemptTimeout = 25 * time.Millisecond

	var gotTimeouts []time.Duration
	starter := func(
		_ context.Context,
		_, _ string,
		_ []string,
		_ agent.ModelConfig,
		_ []agent.MCPServerConfig,
		timeout time.Duration,
		_ string,
	) (*OpenCodeServer, error) {
		gotTimeouts = append(gotTimeouts, timeout)
		return nil, errors.New("startup timeout")
	}

	_, err := startOpenCodeServerWithStarter(
		context.Background(),
		"opencode",
		"/workspace",
		nil,
		agent.ModelConfig{},
		nil,
		attemptTimeout,
		"/workspace/.opencode",
		starter,
	)
	if err == nil {
		t.Fatal("expected all attempts to fail")
	}
	if len(gotTimeouts) != maxStartAttempts {
		t.Fatalf("attempts=%d, want %d", len(gotTimeouts), maxStartAttempts)
	}
	for attempt, timeout := range gotTimeouts {
		if timeout != attemptTimeout {
			t.Fatalf("attempt %d timeout=%s, want %s", attempt+1, timeout, attemptTimeout)
		}
	}
}

func TestDefaultHealthCheckTimeoutIsTenSeconds(t *testing.T) {
	if defaultHealthCheckTimeout != 10*time.Second {
		t.Fatalf("defaultHealthCheckTimeout=%s, want 10s", defaultHealthCheckTimeout)
	}
}

// ============================================================================
// Stop 并发安全
// ============================================================================

func TestStopIdempotentConcurrently(t *testing.T) {
	waitCh := make(chan error, 1)
	close(waitCh) // process already exited

	// 使用 os/exec 创建一个空 cmd（Process 为 nil，Stop 应该直接返回）
	cmd := &exec.Cmd{}
	srv := &OpenCodeServer{
		cmd:    cmd,
		waitCh: waitCh,
		done:   make(chan struct{}),
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = srv.Stop()
		}()
	}
	wg.Wait()

	// 二次调用也应幂等
	if err := srv.Stop(); err != nil {
		t.Fatalf("second Stop() returned error: %v", err)
	}
}

func TestStopConsumesWaitCh(t *testing.T) {
	// 模拟进程正常运行后退出
	waitCh := make(chan error, 1)

	cmd := &exec.Cmd{}
	// 模拟进程正在运行（Process 非 nil）
	cmd.Process = &os.Process{Pid: 99999}

	srv := &OpenCodeServer{
		cmd:    cmd,
		waitCh: waitCh,
		done:   make(chan struct{}),
	}

	// 在 goroutine 中发送信号让 Stop 中的 SIGTERM 和 select 能执行
	go func() {
		time.Sleep(50 * time.Millisecond)
		// 模拟进程在收到信号后退出
		select {
		case waitCh <- nil:
		default:
		}
	}()

	err := srv.Stop()
	// Signal 对一个不存在的 PID 会失败，返回 error
	if err == nil {
		// waitCh 应该已被消费
		select {
		case <-waitCh:
			t.Fatal("waitCh should be consumed by Stop")
		default:
			// OK
		}
	}
}

func TestStopNilProcess(t *testing.T) {
	srv := &OpenCodeServer{
		done:   make(chan struct{}),
		waitCh: make(chan error),
	}
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop() on nil process = %v", err)
	}

	cmd := &exec.Cmd{} // Process == nil, 未启动
	srv = &OpenCodeServer{
		cmd:    cmd,
		done:   make(chan struct{}),
		waitCh: make(chan error),
	}
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop() with nil cmd.Process = %v", err)
	}
}

// ============================================================================
// pickFreePort 测试
// ============================================================================

func TestPickFreePort(t *testing.T) {
	port, err := pickFreePort()
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("port=%d out of range", port)
	}
}

// ============================================================================
// 已有测试
// ============================================================================

func TestSendPermissionDecisionUsesLatestReplyEndpoint(t *testing.T) {
	var gotPath string
	var gotBody permissionDecision
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		return okResponse(r), nil
	})}

	srv := &OpenCodeServer{
		baseURL:    "http://opencode.test",
		httpClient: client,
	}

	if err := srv.SendPermissionDecision(context.Background(), "per_123", "once"); err != nil {
		t.Fatalf("send permission decision: %v", err)
	}
	if gotPath != "/permission/per_123/reply" {
		t.Fatalf("path = %q, want %q", gotPath, "/permission/per_123/reply")
	}
	if gotBody.Reply != "once" {
		t.Fatalf("reply = %q, want %q", gotBody.Reply, "once")
	}
	if gotBody.Message != "" {
		t.Fatalf("message = %q, want empty", gotBody.Message)
	}
}

func TestSendQuestionAnswerUsesLatestReplyEndpoint(t *testing.T) {
	var gotPath string
	var gotBody questionAnswerReq
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		return okResponse(r), nil
	})}

	srv := &OpenCodeServer{
		baseURL:    "http://opencode.test",
		httpClient: client,
	}

	answers := [][]string{{"Use latest endpoint"}}
	if err := srv.SendQuestionAnswer(context.Background(), "que_123", answers); err != nil {
		t.Fatalf("send question answer: %v", err)
	}
	if gotPath != "/question/que_123/reply" {
		t.Fatalf("path = %q, want %q", gotPath, "/question/que_123/reply")
	}
	if len(gotBody.Answers) != 1 || len(gotBody.Answers[0]) != 1 || gotBody.Answers[0][0] != answers[0][0] {
		t.Fatalf("answers = %#v, want %#v", gotBody.Answers, answers)
	}
}

// ============================================================================
// 辅助类型和函数
// ============================================================================

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func okResponse(request *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    request,
	}
}

func okResponseJSON(request *http.Request, body string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

// fakeHealthClient 创建一个返回预设 response 的 HTTP client，用于测试 checkHealth。
func fakeHealthClient(statusCode int, body string) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/global/health" {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("wrong path")),
					Request:    r,
				}, nil
			}
			return &http.Response{
				StatusCode: statusCode,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    r,
			}, nil
		}),
	}
}

// ============================================================================
// waitHealthy 集成测试（使用真实 TCP 服务器）
// ============================================================================

// hungServer 启动一个 TCP server：接受连接但从不发送 HTTP 响应头。
// 用于模拟进程端口已监听但 HTTP 层卡死的场景。
func hungServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	addr = fmt.Sprintf("127.0.0.1:%d", port)

	done := make(chan struct{})
	go func() {
		defer ln.Close()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
				}
				return
			}
			// 读取 HTTP 请求以让 client 认为连接建立，但强制拖延不发送响应头
			// 用 SetReadDeadline 守候，避免 client 断开后 goroutine 泄漏
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			buf := make([]byte, 4096)
			_, _ = conn.Read(buf)
			// 不写任何数据 — client 将收到 response header timeout
			_ = conn.SetReadDeadline(time.Time{})
			select {
			case <-done:
				conn.Close()
				return
			case <-time.After(10 * time.Second):
				conn.Close()
			}
		}
	}()

	return addr, func() { close(done); ln.Close() }
}

func TestWaitHealthyRetriesHungUntilCycleTimeout(t *testing.T) {
	var attempts int
	srv := &OpenCodeServer{
		baseURL:    "http://opencode.test",
		authHeader: "Basic dGVzdDp0ZXN0",
		healthClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			return nil, &net.DNSError{IsTimeout: true}
		})},
		healthCheckTimeout: 750 * time.Millisecond,
		waitCh:             make(chan error),
		done:               make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	err := srv.waitHealthy(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected cycle timeout, got nil")
	}
	if !strings.Contains(err.Error(), "health check timeout") {
		t.Fatalf("err=%v, want health check timeout", err)
	}
	if elapsed < 700*time.Millisecond {
		t.Fatalf("elapsed=%v, request timeout must not end the startup cycle early", elapsed)
	}
	if attempts < 2 {
		t.Fatalf("attempts=%d, want repeated health checks", attempts)
	}
}

func TestWaitHealthyRetriesTimedOutRequestsSeriallyThenSucceeds(t *testing.T) {
	var mu sync.Mutex
	active := 0
	maxActive := 0
	attempts := 0

	srv := &OpenCodeServer{
		baseURL:    "http://opencode.test",
		authHeader: "Basic dGVzdDp0ZXN0",
		healthClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			attempts++
			attempt := attempts
			mu.Unlock()

			time.Sleep(25 * time.Millisecond)

			mu.Lock()
			active--
			mu.Unlock()

			if attempt <= 2 {
				return nil, &net.DNSError{IsTimeout: true}
			}
			return okResponseJSON(r, `{"healthy":true}`)
		})},
		healthCheckTimeout: 2 * time.Second,
		waitCh:             make(chan error),
		done:               make(chan struct{}),
	}

	if err := srv.waitHealthy(context.Background()); err != nil {
		t.Fatalf("waitHealthy() error=%v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if attempts != 3 {
		t.Fatalf("attempts=%d, want 3", attempts)
	}
	if maxActive != 1 {
		t.Fatalf("max concurrent health checks=%d, want 1", maxActive)
	}
}

func TestCheckHealthTimesOutWhileReadingBody(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"healthy":`))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-r.Context().Done()
		}),
	}
	go func() {
		_ = server.Serve(ln)
	}()
	defer func() {
		_ = server.Close()
		_ = ln.Close()
	}()

	srv := &OpenCodeServer{
		baseURL:      "http://" + ln.Addr().String(),
		authHeader:   "Basic dGVzdDp0ZXN0",
		healthClient: newHealthCheckClient(),
	}
	start := time.Now()
	result, _, _ := srv.checkHealth(context.Background())
	elapsed := time.Since(start)

	if result != healthHung {
		t.Fatalf("result=%d, want healthHung", result)
	}
	if elapsed > healthCheckAttemptTimeout+time.Second {
		t.Fatalf("elapsed=%v, body read should honor attempt timeout", elapsed)
	}
}

// fatalServer 返回一个启动在随机端口上、对 /global/health 固定返回 401 的 HTTP server。
func fatalServer(t *testing.T, statusCode int, body string) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	addr = fmt.Sprintf("127.0.0.1:%d", port)

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(statusCode)
			w.Write([]byte(body)) //nolint:errcheck
		}),
	}
	go srv.Serve(ln) //nolint:errcheck
	return addr, func() { srv.Close(); ln.Close() }
}

func TestWaitHealthyFatalResponseNoRetry(t *testing.T) {
	addr, cleanup := fatalServer(t, 401, `{"error":"unauthorized"}`)
	defer cleanup()

	waitCh := make(chan error)
	srv := &OpenCodeServer{
		baseURL:            fmt.Sprintf("http://%s", addr),
		authHeader:         "Basic dGVzdDp0ZXN0",
		healthClient:       newHealthCheckClient(),
		healthCheckTimeout: 5 * time.Second,
		waitCh:             waitCh,
		done:               make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	err := srv.waitHealthy(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected fatal error, got nil")
	}
	if !errors.Is(err, errHealthFatal) {
		t.Fatalf("err = %v, want errHealthFatal", err)
	}
	if elapsed > 1*time.Second {
		t.Fatalf("elapsed=%v, should fail fast on 401", elapsed)
	}
}

func TestWaitHealthyDegradedRetriesThenOK(t *testing.T) {
	// 前两次返回 503，第三次返回 200 healthy
	var attempts int
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts <= 2 {
				w.WriteHeader(503)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"healthy":true}`)) //nolint:errcheck
		}),
	}
	go srv.Serve(ln) //nolint:errcheck
	defer func() { srv.Close(); ln.Close() }()

	waitCh := make(chan error)
	hcSrv := &OpenCodeServer{
		baseURL:            fmt.Sprintf("http://%s", addr),
		authHeader:         "Basic dGVzdDp0ZXN0",
		healthClient:       newHealthCheckClient(),
		healthCheckTimeout: 5 * time.Second,
		waitCh:             waitCh,
		done:               make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = hcSrv.waitHealthy(ctx)
	if err != nil {
		t.Fatalf("expected success after 503 retries, got: %v", err)
	}
	if attempts < 3 {
		t.Fatalf("attempts=%d, want at least 3", attempts)
	}
	t.Logf("degraded→OK after %d attempts", attempts)
}

func TestWaitHealthySucceedsAfterConnRefused(t *testing.T) {
	// 先让健康检查遇到连接拒绝（港口无监听），再启动服务器
	port, _ := pickFreePort()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	waitCh := make(chan error)
	srv := &OpenCodeServer{
		baseURL:            fmt.Sprintf("http://%s", addr),
		authHeader:         "Basic dGVzdDp0ZXN0",
		healthClient:       newHealthCheckClient(),
		healthCheckTimeout: 5 * time.Second,
		waitCh:             waitCh,
		done:               make(chan struct{}),
	}

	// 500ms 后启动真实服务器
	var started bool
	var startMu sync.Mutex
	go func() {
		time.Sleep(500 * time.Millisecond)
		ln, listenErr := net.Listen("tcp", addr)
		if listenErr != nil {
			return
		}
		hs := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"healthy":true}`)) //nolint:errcheck
			}),
		}
		startMu.Lock()
		started = true
		startMu.Unlock()
		go hs.Serve(ln) //nolint:errcheck
		defer hs.Close()
		defer ln.Close()
		<-srv.done // 测试结束后关闭
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err := srv.waitHealthy(ctx)
	elapsed := time.Since(start)

	// 通知后台 goroutine 退出，回收 listener
	close(srv.done)

	if err != nil {
		t.Fatalf("expected success after server starts, got: %v", err)
	}
	if elapsed < 500*time.Millisecond {
		t.Fatalf("elapsed=%v, should wait for server to start", elapsed)
	}

	startMu.Lock()
	if !started {
		t.Fatal("server was not started")
	}
	startMu.Unlock()
	t.Logf("succeeded after %v (conn refused → server started)", elapsed)
}

func TestWaitHealthyContextCancelNoRetry(t *testing.T) {
	// context 取消后 waitHealthy 应立即返回
	addr, cleanup := hungServer(t)
	defer cleanup()

	waitCh := make(chan error)
	srv := &OpenCodeServer{
		baseURL:            fmt.Sprintf("http://%s", addr),
		authHeader:         "Basic dGVzdDp0ZXN0",
		healthClient:       newHealthCheckClient(),
		healthCheckTimeout: 30 * time.Second,
		waitCh:             waitCh,
		done:               make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := srv.waitHealthy(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context canceled error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("elapsed=%v, should return quickly on cancel", elapsed)
	}
}
