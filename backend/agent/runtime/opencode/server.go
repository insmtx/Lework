package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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
	"syscall"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/ygpkg/yg-go/logs"
)

const (
	// defaultHealthCheckTimeout 是等待 opencode server 健康检查就绪的默认超时时间。
	// 每个进程拥有独立的启动周期，周期结束后由上层停止进程并重试。
	defaultHealthCheckTimeout = 10 * time.Second

	// fastPollInterval 是健康检查前期的快速轮询间隔。
	fastPollInterval = 200 * time.Millisecond

	// fastPollDuration 是快速轮询阶段的持续时间，之后切换为慢轮询以减少无效请求。
	fastPollDuration = 5 * time.Second

	// slowPollInterval 是健康检查后期的慢速轮询间隔。
	slowPollInterval = 1 * time.Second

	// gracefulShutdownTimeout 是优雅关闭子进程的等待时间。
	// 先发送 SIGTERM 请求子进程自行清理，超时后强制 SIGKILL。
	gracefulShutdownTimeout = 5 * time.Second

	// maxStartAttempts 是 OpenCode 进程的最大启动次数（含重试）。
	maxStartAttempts = 3

	// healthCheckConnectTimeout 是健康检查 TCP 连接超时。
	healthCheckConnectTimeout = 500 * time.Millisecond

	// healthCheckHeaderTimeout 是健康检查响应头超时。
	// 超过此时间无响应头时结束本次请求，并在当前启动周期内继续轮询。
	healthCheckHeaderTimeout = 2 * time.Second

	// healthCheckAttemptTimeout 限制单次健康检查（包括响应体读取）的总耗时。
	healthCheckAttemptTimeout = 2500 * time.Millisecond
)

// healthResult 描述单次健康检查的结果。
type healthResult int

const (
	healthOK          healthResult = iota // 200 + healthy:true
	healthConnRefused                     // TCP 连接被拒绝，进程可能仍在启动
	healthHung                            // 单次请求超时，当前启动周期内继续轮询
	healthFatal                           // 确定性的协议错误（401/403/404/错误 JSON），不应重试
	healthDegraded                        // 5xx 等临时错误，在当前进程窗口内重试
)

// ============================================================================
// OpenCodeServer — opcode serve 子进程的 HTTP 客户端和生命周期管理
// ============================================================================

// OpenCodeServer 管理 opcode serve 子进程并通过 HTTP 与之通信。
type OpenCodeServer struct {
	binary   string
	workDir  string
	addr     string
	password string
	baseURL  string

	cmd    *exec.Cmd
	waitCh chan error // 唯一的 cmd.Wait() 结果通道

	httpClient         *http.Client
	healthClient       *http.Client // 健康检查专用 client（独立配置）
	authHeader         string
	healthCheckTimeout time.Duration

	mu     sync.Mutex
	closed bool
	done   chan struct{}

	// 管道 goroutine 生命周期控制
	pipeCtx    context.Context
	pipeCancel context.CancelFunc

	// 启动阶段的 stderr 收集器，用于健康检查超时时提供诊断信息。
	stderrMu    sync.Mutex
	stderrLines []string
}

// ============================================================================
// 启动入口：带重试的启动逻辑
// ============================================================================

// startOpenCodeServer 启动 opcode serve 子进程，失败时自动重试。
// 最多启动 maxStartAttempts 个全新进程，每个进程拥有独立的健康检查周期。
func startOpenCodeServer(ctx context.Context, binary, workDir string, baseEnv []string, modelCfg agent.ModelConfig, mcpServers []agent.MCPServerConfig, healthCheckTimeout time.Duration, dataDir string) (*OpenCodeServer, error) {
	return startOpenCodeServerWithStarter(
		ctx,
		binary,
		workDir,
		baseEnv,
		modelCfg,
		mcpServers,
		healthCheckTimeout,
		dataDir,
		startSingleOpenCodeServer,
	)
}

type openCodeServerStarter func(
	context.Context,
	string,
	string,
	[]string,
	agent.ModelConfig,
	[]agent.MCPServerConfig,
	time.Duration,
	string,
) (*OpenCodeServer, error)

func startOpenCodeServerWithStarter(
	ctx context.Context,
	binary, workDir string,
	baseEnv []string,
	modelCfg agent.ModelConfig,
	mcpServers []agent.MCPServerConfig,
	healthCheckTimeout time.Duration,
	dataDir string,
	starter openCodeServerStarter,
) (*OpenCodeServer, error) {
	if healthCheckTimeout <= 0 {
		healthCheckTimeout = defaultHealthCheckTimeout
	}

	var errs []string

	for attempt := 1; attempt <= maxStartAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled before start attempt %d/%d: %w", attempt, maxStartAttempts, ctx.Err())
		default:
		}

		logs.Infof(
			"OpenCode server start attempt %d/%d: health_timeout=%s workDir=%s",
			attempt,
			maxStartAttempts,
			healthCheckTimeout,
			workDir,
		)

		srv, healthErr := starter(ctx, binary, workDir, baseEnv, modelCfg, mcpServers, healthCheckTimeout, dataDir)
		if healthErr == nil {
			logs.Infof("OpenCode server ready on attempt %d/%d: pid=%d port=%s workDir=%s",
				attempt, maxStartAttempts, srv.PID(), srv.addr, workDir)
			return srv, nil
		}

		errs = append(errs, healthErr.Error())
		logs.Warnf(
			"OpenCode server start attempt %d/%d failed: health_timeout=%s reason=%v",
			attempt,
			maxStartAttempts,
			healthCheckTimeout,
			healthErr,
		)

		// 确定性协议错误不重试
		if errors.Is(healthErr, errHealthFatal) {
			return nil, fmt.Errorf("fatal health check error after %d/%d attempts: %w",
				attempt, maxStartAttempts, healthErr)
		}
	}

	return nil, fmt.Errorf("all %d start attempts failed: %s", maxStartAttempts, strings.Join(errs, "; "))
}

// errHealthFatal 表示确定性协议错误，不应重试进程。
var errHealthFatal = errors.New("fatal health check response")

// ============================================================================
// 单次进程启动
// ============================================================================

// startSingleOpenCodeServer 启动一次 opcode serve 子进程并等待其就绪。
// 健康检查失败时返回相应的 sentinel 错误，由上层 retry 逻辑处理。
func startSingleOpenCodeServer(ctx context.Context, binary, workDir string, baseEnv []string, modelCfg agent.ModelConfig, mcpServers []agent.MCPServerConfig, healthCheckTimeout time.Duration, dataDir string) (*OpenCodeServer, error) {
	// 1. 动态端口分配 — 关闭后传递端口，端口冲突交给进程重试处理
	port, err := pickFreePort()
	if err != nil {
		return nil, fmt.Errorf("pick free port: %w", err)
	}

	// 2. 生成随机密码
	password, err := generatePassword()
	if err != nil {
		return nil, err
	}

	// 3. 构建配置和环境变量
	configContent, err := buildConfigContent(modelCfg, mcpServers)
	if err != nil {
		return nil, fmt.Errorf("build config content: %w", err)
	}
	databasePath, err := ensureOpenCodeDBPath(dataDir)
	if err != nil {
		return nil, err
	}

	serverEnv := buildServerEnv(password, configContent, databasePath, baseEnv)

	// 4. 启动子进程
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	baseURL := fmt.Sprintf("http://%s", addr)

	cmd := exec.Command(binary, "serve", "--port", fmt.Sprint(port), "--hostname", "127.0.0.1")
	cmd.Dir = workDir
	cmd.Env = serverEnv

	// 捕获 stderr 和 stdout 用于日志
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	startTime := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start opencode serve: %w", err)
	}

	// 5. 立即建立唯一的 cmd.Wait() goroutine
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	// 6. 构建 auth header
	auth := base64.StdEncoding.EncodeToString([]byte("opencode:" + password))
	authHeader := "Basic " + auth

	// 创建 pipeCtx 用于控制管道读取 goroutine 的生命周期
	pipeCtx, pipeCancel := context.WithCancel(context.Background())

	srv := &OpenCodeServer{
		binary:             binary,
		workDir:            workDir,
		addr:               addr,
		password:           password,
		baseURL:            baseURL,
		cmd:                cmd,
		waitCh:             waitCh,
		httpClient:         &http.Client{Timeout: 30 * time.Second},
		healthClient:       newHealthCheckClient(),
		authHeader:         authHeader,
		healthCheckTimeout: healthCheckTimeout,
		done:               make(chan struct{}),
		pipeCtx:            pipeCtx,
		pipeCancel:         pipeCancel,
	}

	// 7. 监听上层 ctx 取消，触发子进程关闭；正常停止后同步退出监听。
	go func() {
		select {
		case <-ctx.Done():
			_ = srv.Stop()
		case <-srv.done:
		}
	}()

	// 8. 后台读取 stderr
	go func() {
		defer stderrPipe.Close()
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			select {
			case <-pipeCtx.Done():
				return
			default:
			}
			line := scanner.Text()
			logs.Errorf("[opencode stderr] %s", line)
			srv.stderrMu.Lock()
			srv.stderrLines = append(srv.stderrLines, line)
			if len(srv.stderrLines) > 20 {
				srv.stderrLines = srv.stderrLines[len(srv.stderrLines)-20:]
			}
			srv.stderrMu.Unlock()
		}
	}()

	// 9. 后台读取 stdout
	go func() {
		defer stdoutPipe.Close()
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			select {
			case <-pipeCtx.Done():
				return
			default:
			}
			logs.Infof("[opencode stdout] %s", scanner.Text())
		}
	}()

	// 10. 等待 health check 通过
	healthErr := srv.waitHealthy(ctx)

	if healthErr != nil {
		// 收集诊断信息
		elapsed := time.Since(startTime).Truncate(time.Millisecond)
		pid := cmd.Process.Pid

		// 读取进程退出状态（如有）
		var exitInfo string
		select {
		case waitErr := <-waitCh:
			if waitErr != nil {
				var exitErr *exec.ExitError
				if errors.As(waitErr, &exitErr) {
					exitInfo = fmt.Sprintf("exit_code=%d", exitErr.ExitCode())
				} else {
					exitInfo = fmt.Sprintf("wait_err=%v", waitErr)
				}
			} else {
				exitInfo = "exit_code=0"
			}
		default:
			exitInfo = "still_running"
		}

		srv.stderrMu.Lock()
		stderrSnapshot := strings.Join(srv.stderrLines, " | ")
		srv.stderrMu.Unlock()

		// 确定失败阶段
		phase := "health_poll"
		switch {
		case strings.Contains(healthErr.Error(), "health check timeout"):
			phase = "health_poll_timeout"
		case errors.Is(healthErr, errHealthFatal):
			phase = "health_check_fatal"
		case strings.Contains(healthErr.Error(), "exited prematurely"):
			phase = "process_exit"
		}

		// 所有启动失败路径都必须停止并回收仍在运行的子进程。
		if exitInfo == "still_running" {
			_ = srv.Stop()
			exitInfo = "stopped"
		} else {
			pipeCancel()
			select {
			case <-srv.done:
			default:
				close(srv.done)
			}
		}

		diagnostic := fmt.Sprintf("pid=%d port=%d elapsed=%s phase=%s exit=%s", pid, port, elapsed, phase, exitInfo)
		if stderrSnapshot != "" {
			diagnostic += fmt.Sprintf(" stderr=%s", stderrSnapshot)
		}
		// 注意：错误中不包含 password

		// 包装错误，保留原始 sentinel
		return nil, fmt.Errorf("%w: %s: %v", healthErr, diagnostic, healthErr)
	}

	logs.Infof("OpenCode server ready on attempt: pid=%d port=%d baseURL=%s workDir=%s",
		cmd.Process.Pid, port, baseURL, workDir)
	return srv, nil
}

// newHealthCheckClient 创建健康检查专用的 HTTP 客户端。
// 直连 127.0.0.1，禁用代理和连接复用，独立超时配置。
// 不设置 Client.Timeout，由 Transport.ResponseHeaderTimeout 精确控制响应头等待。
func newHealthCheckClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: healthCheckConnectTimeout,
			}).DialContext,
			DisableKeepAlives:     true,
			Proxy:                 nil,
			ResponseHeaderTimeout: healthCheckHeaderTimeout,
		},
	}
}

// pickFreePort 获取一个空闲的 TCP 端口（关闭 listener 后返回端口号）。
// 端口冲突交给进程重试机制处理。
func pickFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

// ============================================================================
// 健康检查
// ============================================================================

// waitHealthy 轮询 health check 端点直到服务就绪或不可恢复。
func (s *OpenCodeServer) waitHealthy(ctx context.Context) error {
	timeout := s.healthCheckTimeout
	if timeout <= 0 {
		timeout = defaultHealthCheckTimeout
	}

	healthCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startedAt := time.Now()
	timer := time.NewTimer(fastPollInterval)
	defer timer.Stop()

	var attempts int

	for {
		select {
		case <-healthCtx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			elapsed := time.Since(startedAt).Truncate(time.Millisecond)
			return fmt.Errorf("health check timeout after %v (attempts=%d)", elapsed, attempts)

		case waitErr := <-s.waitCh:
			// 子进程提前退出
			if waitErr != nil {
				return fmt.Errorf("opencode process exited prematurely: %w", waitErr)
			}
			return fmt.Errorf("opencode process exited prematurely with code 0")

		case <-timer.C:
			result, statusCode, body := s.checkHealth(healthCtx)
			attempts++
			elapsed := time.Since(startedAt).Truncate(time.Millisecond)
			if healthCtx.Err() != nil {
				continue
			}

			switch result {
			case healthOK:
				logs.Infof(
					"OpenCode health check passed: attempt=%d elapsed=%s pid=%d",
					attempts,
					elapsed,
					s.PID(),
				)
				return nil
			case healthHung:
				logs.Warnf(
					"OpenCode health check request timed out, continuing current startup cycle: attempt=%d elapsed=%s pid=%d",
					attempts,
					elapsed,
					s.PID(),
				)
			case healthFatal:
				errMsg := fmt.Sprintf("fatal health response: status=%d body=%s", statusCode, body)
				return fmt.Errorf("%w: %s", errHealthFatal, errMsg)
			case healthDegraded:
				logs.Warnf(
					"OpenCode health check degraded: status=%d attempt=%d elapsed=%s pid=%d",
					statusCode,
					attempts,
					elapsed,
					s.PID(),
				)
			case healthConnRefused:
				// 继续轮询，进程可能仍在启动
			}

			pollInterval := fastPollInterval
			if time.Since(startedAt) >= fastPollDuration {
				pollInterval = slowPollInterval
			}
			timer.Reset(pollInterval)
		}
	}
}

// checkHealth 调用 GET /global/health，使用独立的健康检查 HTTP 客户端。
// 返回结构化的健康检查结果、HTTP 状态码、有限响应体。
func (s *OpenCodeServer) checkHealth(ctx context.Context) (result healthResult, statusCode int, body string) {
	attemptCtx, cancel := context.WithTimeout(ctx, healthCheckAttemptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, s.baseURL+"/global/health", nil)
	if err != nil {
		return healthConnRefused, 0, ""
	}
	req.Header.Set("Authorization", s.authHeader)

	resp, err := s.healthClient.Do(req)
	if err != nil {
		// 区分连接拒绝和响应头超时
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return healthHung, 0, ""
		}
		// 其他网络错误（连接拒绝等）→ 继续轮询
		return healthConnRefused, 0, ""
	}
	defer resp.Body.Close()

	statusCode = resp.StatusCode

	// 读取有限的响应体用于诊断
	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
	body = string(bodyBytes)
	if readErr != nil {
		if attemptCtx.Err() != nil && ctx.Err() == nil {
			return healthHung, statusCode, body
		}
		var netErr net.Error
		if errors.As(readErr, &netErr) && netErr.Timeout() {
			return healthHung, statusCode, body
		}
		return healthConnRefused, statusCode, body
	}

	// 确定性协议错误：401/403/404 不重试进程
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return healthFatal, statusCode, body
	}

	if statusCode != http.StatusOK {
		// 5xx 等 → 当前进程窗口内重试
		return healthDegraded, statusCode, body
	}

	var hr healthResponse
	if err := json.Unmarshal(bodyBytes, &hr); err != nil {
		// JSON 解析失败 → 协议错误，不重试
		return healthFatal, statusCode, body
	}

	if hr.Healthy {
		return healthOK, statusCode, body
	}

	// 200 但 healthy=false，继续轮询
	return healthConnRefused, statusCode, body
}

// ============================================================================
// 进程管理
// ============================================================================

// Stop 优雅地终止 opencode 子进程。
//
// 关闭流程：
//  1. 取消管道读取 goroutine（pipeCancel）
//  2. 关闭 done channel（通知外部观察者）
//  3. 发送 SIGTERM 请求子进程自行清理
//  4. 等待 gracefulShutdownTimeout
//  5. 超时则强制 SIGKILL
//  6. 从唯一的 waitCh 等待进程回收
//
// Stop 是幂等的，多次调用安全。
func (s *OpenCodeServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	pipeCancel := s.pipeCancel
	cmd := s.cmd
	done := s.done

	// 1. 通知管道 goroutine 退出
	if pipeCancel != nil {
		pipeCancel()
	}

	// 2. 关闭 done channel
	select {
	case <-done:
	default:
		close(done)
	}

	// 3. 无子进程直接返回
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// 4. 优雅关闭：先 SIGTERM，超时后 SIGKILL
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		_ = cmd.Process.Kill()
	}

	// 5. 从唯一的 waitCh 等待进程退出
	select {
	case <-s.waitCh:
		return nil
	case <-time.After(gracefulShutdownTimeout):
		logs.Warnf("OpenCode server pid=%d did not exit after SIGTERM, sending SIGKILL", cmd.Process.Pid)
		_ = cmd.Process.Kill()
		<-s.waitCh
		return nil
	}
}

// PID 返回子进程的 PID。
func (s *OpenCodeServer) PID() int {
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// ============================================================================
// Session API
// ============================================================================

// CreateSession 创建新的 OpenCode 会话。
func (s *OpenCodeServer) CreateSession(ctx context.Context, title, providerID, modelID, systemPrompt string) (*sessionResponse, error) {
	reqBody := sessionCreateRequest{
		Title: title,
	}
	if providerID != "" && modelID != "" {
		reqBody.Model = &sessionModelRef{
			ProviderID: providerID,
			ID:         modelID,
		}
	}
	if systemPrompt != "" {
		// system prompt 不直接在创建时传入；通过后续 message 的 system 字段传递
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal create session request: %w", err)
	}

	logs.Debugf("CreateSession request body: %s", string(body))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/session", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create session request: %w", err)
	}
	req.Header.Set("Authorization", s.authHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		logs.Errorf("CreateSession failed: status=%d body=%s", resp.StatusCode, string(respBody))
		return nil, fmt.Errorf("create session returned %d: %s", resp.StatusCode, string(respBody))
	}

	var session sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("decode session response: %w", err)
	}

	logs.Infof("OpenCode session created: id=%s title=%s", session.ID, session.Title)
	return &session, nil
}

// GetSession retrieves session metadata required to resume plan handoff handling.
func (s *OpenCodeServer) GetSession(ctx context.Context, sessionID string) (*sessionResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/session/"+sessionID, nil)
	if err != nil {
		return nil, fmt.Errorf("get session request: %w", err)
	}
	req.Header.Set("Authorization", s.authHeader)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("get session returned %d: %s", resp.StatusCode, string(respBody))
	}

	var session sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("decode session response: %w", err)
	}
	return &session, nil
}

// SendMessage 向指定会话发送消息并同步等待完整响应。
// 注意：openCode 的 /session/:id/message 是同步端点，会等待模型完整生成，
// 可能耗时数分钟甚至更长，因此不使用带超时的 httpClient，而是完全依赖 context 控制生命周期。
func (s *OpenCodeServer) SendMessage(ctx context.Context, sessionID string, req messageRequest) (*messageResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal message request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/session/"+sessionID+"/message", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("send message request: %w", err)
	}
	httpReq.Header.Set("Authorization", s.authHeader)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.longPollClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		logs.Errorf("SendMessage failed: status=%d session=%s body=%s", resp.StatusCode, sessionID, string(respBody))
		return nil, fmt.Errorf("send message returned %d: %s", resp.StatusCode, string(respBody))
	}

	var msgResp messageResponse
	if err := json.NewDecoder(resp.Body).Decode(&msgResp); err != nil {
		return nil, fmt.Errorf("decode message response: %w", err)
	}

	return &msgResp, nil
}

// Abort 中断正在运行的会话。
func (s *OpenCodeServer) Abort(ctx context.Context, sessionID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/session/"+sessionID+"/abort", nil)
	if err != nil {
		return fmt.Errorf("abort session request: %w", err)
	}
	req.Header.Set("Authorization", s.authHeader)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("abort session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("abort session returned %d: %s", resp.StatusCode, string(respBody))
	}

	logs.Infof("OpenCode session aborted: id=%s", sessionID)
	return nil
}

// SendPermissionDecision 响应权限请求。
func (s *OpenCodeServer) SendPermissionDecision(ctx context.Context, permissionID, decision string) error {
	reqBody := permissionDecision{Reply: decision}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal permission decision: %w", err)
	}

	url := fmt.Sprintf("%s/permission/%s/reply", s.baseURL, permissionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("permission decision request: %w", err)
	}
	req.Header.Set("Authorization", s.authHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send permission decision: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("permission decision returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SendQuestionAnswer 响应 question 请求，发送用户选择的答案。
func (s *OpenCodeServer) SendQuestionAnswer(ctx context.Context, questionID string, answers [][]string) error {
	reqBody := questionAnswerReq{Answers: answers}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal question answer: %w", err)
	}

	url := fmt.Sprintf("%s/question/%s/reply", s.baseURL, questionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("question answer request: %w", err)
	}
	req.Header.Set("Authorization", s.authHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send question answer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("question answer returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ============================================================================
// SSE 事件流
// ============================================================================

// ConnectSSE 连接到 /event SSE 端点，返回解析后的事件通道。
// 事件按 directory 过滤以确保只接收当前工作区的事件。
func (s *OpenCodeServer) ConnectSSE(ctx context.Context, workDir string) (<-chan sseEvent, error) {
	url := s.baseURL + "/event"
	if workDir != "" {
		url += "?directory=" + workDir
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create SSE request: %w", err)
	}
	req.Header.Set("Authorization", s.authHeader)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	// SSE 需要长连接，使用独立的无超时 client
	sseClient := &http.Client{Timeout: 0}
	resp, err := sseClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect SSE: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("SSE connect returned %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan sseEvent, 128)

	go func() {
		defer resp.Body.Close()
		defer close(ch)

		// 监听 context 取消，主动关闭 resp.Body 以中断阻塞的 scanner.Scan()
		go func() {
			<-ctx.Done()
			logs.Debugf("SSE resp.Body closing (ctx cancelled)")
			resp.Body.Close()
		}()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line

		var dataLines []string

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()

			// SSE 空行表示事件结束
			if line == "" {
				if len(dataLines) > 0 {
					event := parseSSEData(dataLines)
					if event != nil {
						select {
						case ch <- *event:
						case <-ctx.Done():
							return
						}
					}
					dataLines = dataLines[:0]
				}
				continue
			}

			// 跳过注释行
			if strings.HasPrefix(line, ":") {
				continue
			}

			// 收集 data: 行
			if data, found := strings.CutPrefix(line, "data:"); found {
				data = strings.TrimSpace(data)
				if data != "" {
					dataLines = append(dataLines, data)
				}
			}
			// 也接受 event: 和 id: 行（当前忽略，按需使用 data 中的 type 字段）
		}

		if err := scanner.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				logs.Debugf("SSE scanner stopped (ctx cancelled): %v", err)
			} else {
				logs.Warnf("SSE scanner error: %v", err)
			}
		}
	}()

	return ch, nil
}

// parseSSEData 从 SSE data 行解析事件。
func parseSSEData(lines []string) *sseEvent {
	if len(lines) == 0 {
		return nil
	}

	// 合并多行 data（SSE 规范允许同一事件的多个 data: 行）
	data := strings.Join(lines, "\n")

	var event sseEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		logs.Warnf("Failed to parse SSE event: %v, data=%s", err, data)
		return nil
	}

	if event.Type == "" {
		return nil
	}

	return &event
}

// longPollClient 返回一个无超时的 HTTP client，用于长轮询请求（如 SendMessage）。
// 这些请求可能耗时数分钟等待模型生成，生命周期由 context 控制而非 client timeout。
func (s *OpenCodeServer) longPollClient() *http.Client {
	return &http.Client{Timeout: 0}
}
