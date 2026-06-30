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
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/provider"
	"github.com/ygpkg/yg-go/logs"
)

const (
	// defaultHealthCheckTimeout 是等待 opencode server 健康检查就绪的默认超时时间。
	// opencode 启动时需要初始化 SQLite 数据库、配置加载、MCP 连接等 55+ 个服务层，
	// 在 cold start 或磁盘 I/O 繁忙时需要更长的时间。
	defaultHealthCheckTimeout = 30 * time.Second

	// fastPollInterval 是健康检查前期的快速轮询间隔。
	fastPollInterval = 200 * time.Millisecond

	// fastPollDuration 是快速轮询阶段的持续时间，之后切换为慢轮询以减少无效请求。
	fastPollDuration = 5 * time.Second

	// slowPollInterval 是健康检查后期的慢速轮询间隔。
	slowPollInterval = 1 * time.Second
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

	cmd *exec.Cmd

	httpClient         *http.Client
	authHeader         string
	healthCheckTimeout time.Duration

	mu     sync.Mutex
	closed bool
	done   chan struct{}

	// 启动阶段的 stderr 收集器，用于健康检查超时时提供诊断信息。
	stderrMu    sync.Mutex
	stderrLines []string
}

// startOpenCodeServer 启动 opcode serve 子进程并等待其就绪。
// healthCheckTimeout 指定等待健康检查的最长时间；为 0 或负数时使用默认值。
func startOpenCodeServer(ctx context.Context, binary, workDir string, baseEnv []string, modelCfg agent.ModelConfig, mcpServers []provider.MCPServerConfig, healthCheckTimeout time.Duration) (*OpenCodeServer, error) {
	// 1. 动态端口分配
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

	serverEnv := buildServerEnv(password, configContent, baseEnv)

	// 4. 启动子进程
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	baseURL := fmt.Sprintf("http://%s", addr)

	cmd := exec.CommandContext(ctx, binary, "serve", "--port", fmt.Sprint(port), "--hostname", "127.0.0.1")
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

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start opencode serve: %w", err)
	}

	// 5. 构建 auth header
	auth := base64.StdEncoding.EncodeToString([]byte("opencode:" + password))
	authHeader := "Basic " + auth

	srv := &OpenCodeServer{
		binary:             binary,
		workDir:            workDir,
		addr:               addr,
		password:           password,
		baseURL:            baseURL,
		cmd:                cmd,
		httpClient:         &http.Client{Timeout: 30 * time.Second}, // 普通 API 调用默认 30s 超时
		authHeader:         authHeader,
		healthCheckTimeout: healthCheckTimeout,
		done:               make(chan struct{}),
	}

	// 后台读取 stderr（收集最后几行用于健康检查超时诊断）
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			logs.Errorf("[opencode stderr] %s", line)
			srv.stderrMu.Lock()
			srv.stderrLines = append(srv.stderrLines, line)
			// 只保留最近 20 行，避免内存泄漏
			if len(srv.stderrLines) > 20 {
				srv.stderrLines = srv.stderrLines[len(srv.stderrLines)-20:]
			}
			srv.stderrMu.Unlock()
		}
	}()

	// 后台读取 stdout（openCode 可能在 stdout 输出错误信息）
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			logs.Infof("[opencode stdout] %s", scanner.Text())
		}
	}()

	// 6. 等待 health check 通过
	if err := srv.waitHealthy(ctx); err != nil {
		_ = srv.Stop()
		return nil, fmt.Errorf("wait for opencode healthy: %w", err)
	}

	logs.Infof("OpenCode server ready: pid=%d port=%d baseURL=%s workDir=%s", cmd.Process.Pid, port, baseURL, workDir)
	return srv, nil
}

// pickFreePort 获取一个空闲的 TCP 端口。
func pickFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

// waitHealthy 轮询 health check 端点直到服务就绪。
// 使用指数退避策略：前 fastPollDuration 用 200ms 快速轮询，之后切换为 1s 慢轮询。
func (s *OpenCodeServer) waitHealthy(ctx context.Context) error {
	timeout := s.healthCheckTimeout
	if timeout <= 0 {
		timeout = defaultHealthCheckTimeout
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(fastPollInterval)
	defer ticker.Stop()

	var attempts int
	switchToSlow := time.After(fastPollDuration)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-switchToSlow:
			// 5s 后切换到慢轮询，减少无效请求
			ticker.Reset(slowPollInterval)
		case <-ticker.C:
			if time.Now().After(deadline) {
				elapsed := time.Since(deadline.Add(-timeout)).Truncate(time.Millisecond)
				errMsg := fmt.Sprintf("health check timeout after %v (attempts=%d)", elapsed, attempts)
				// 附加 stderr 输出帮助诊断
				s.stderrMu.Lock()
				if len(s.stderrLines) > 0 {
					errMsg += fmt.Sprintf(", stderr=%s", strings.Join(s.stderrLines, " | "))
				}
				s.stderrMu.Unlock()
				return fmt.Errorf("%s", errMsg)
			}
			if s.checkHealth(ctx) {
				logs.Infof("OpenCode health check passed: attempts=%d", attempts+1)
				return nil
			}
			attempts++
		}
	}
}

// checkHealth 调用 GET /global/health 检查服务状态。
func (s *OpenCodeServer) checkHealth(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/global/health", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", s.authHeader)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var hr healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		return false
	}
	return hr.Healthy
}

// ============================================================================
// 进程管理
// ============================================================================

// Stop 终止 opencode 子进程。
func (s *OpenCodeServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	close(s.done)
	return nil
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
// 可能耗时数分钟，因此不使用带超时的 httpClient，而是依赖 context 控制生命周期。
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
