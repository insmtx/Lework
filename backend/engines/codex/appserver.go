package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/ygpkg/yg-go/logs"
)

const (
	appServerIdleTimeout = 5 * time.Minute
	appServerGCInterval  = 1 * time.Minute
)

// AppServer 管理一个持久化的 codex app-server 进程。
type AppServer struct {
	binary  string
	workDir string

	cmd     *exec.Cmd
	scanner *bufio.Scanner // stdout 行扫描器（整个生命周期共享）
	stdin   io.WriteCloser

	writeMu   sync.Mutex
	nextRPCID int64
	mu        sync.Mutex
	pending   map[int64]chan *rpcResponse
	threadID  string
	turnID    string
	busy      bool
	lastActive time.Time
	closed    bool
	done      chan struct{}

	pendingApproval *ServerRequest
	onNotification  func(method string, params json.RawMessage)
	onServerRequest func(req ServerRequest)
	evtChan         chan<- events.Event
}

// AppServerPool 以 workDir+baseURL 为 key 管理 app-server 进程池。
type AppServerPool struct {
	mu      sync.Mutex
	servers map[string]*AppServer
	binary  string
	baseEnv []string
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewAppServerPool(binary string, baseEnv []string) *AppServerPool {
	ctx, cancel := context.WithCancel(context.Background())
	pool := &AppServerPool{
		servers: make(map[string]*AppServer),
		binary:  binary,
		baseEnv: baseEnv,
		ctx:     ctx,
		cancel:  cancel,
	}
	go pool.gcLoop()
	return pool
}

func poolKey(workDir, sessionID string) string {
	return workDir + "|" + strings.TrimSpace(sessionID)
}

func (p *AppServerPool) GetOrCreate(ctx context.Context, workDir string, sessionID string, modelCfg engines.ModelConfig) (*AppServer, error) {
	key := poolKey(workDir, sessionID)
	p.mu.Lock()
	if srv, ok := p.servers[key]; ok && !srv.isClosed() {
		p.mu.Unlock()
		return srv, nil
	}
	if srv, ok := p.servers[key]; ok {
		delete(p.servers, key)
		_ = srv.Close()
	}
	p.mu.Unlock()

	srv, err := startAppServer(ctx, p.binary, workDir, p.baseEnv, modelCfg)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	if existing, ok := p.servers[key]; ok && !existing.isClosed() {
		p.mu.Unlock()
		_ = srv.Close()
		return existing, nil
	}
	p.servers[key] = srv
	p.mu.Unlock()
	return srv, nil
}

func (p *AppServerPool) Shutdown() {
	p.cancel()
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, srv := range p.servers {
		_ = srv.Close()
		delete(p.servers, key)
	}
}

func (p *AppServerPool) gcLoop() {
	ticker := time.NewTicker(appServerGCInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.collect()
		}
	}
}

func (p *AppServerPool) collect() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, srv := range p.servers {
		if srv.isClosed() {
			delete(p.servers, key)
			continue
		}
		srv.mu.Lock()
		idle := !srv.busy && time.Since(srv.lastActive) > appServerIdleTimeout
		srv.mu.Unlock()
		if idle {
			logs.Infof("AppServer idle timeout, closing: key=%s", key)
			_ = srv.Close()
			delete(p.servers, key)
		}
	}
}

// ============================================================================
// 进程启动
// ============================================================================

func startAppServer(ctx context.Context, binary, workDir string, baseEnv []string, modelCfg engines.ModelConfig) (*AppServer, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(modelCfg.BaseURL), "/")
	if baseURL != "" && !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}

	cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "stdio://",
		"-c", "sandbox_mode=danger-full-access",
		"-c", `model_provider="leros"`,
		"-c", `model_providers.leros.name="leros"`,
		"-c", fmt.Sprintf(`model_providers.leros.base_url=%q`, baseURL),
		"-c", `model_providers.leros.env_key="OPENAI_API_KEY"`,
		"-c", `model_providers.leros.wire_api="responses"`,
		"-c", `model_providers.leros.requires_openai_auth=false`,
	)
	cmd.Dir = workDir
	cmd.Env = buildAppServerEnv(baseEnv, modelCfg)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}

	logs.Infof("Codex app-server started: pid=%d workDir=%s binary=%s", cmd.Process.Pid, workDir, binary)

	// stderr reader
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				logs.Warnf("Codex app-server stderr: pid=%d %s", cmd.Process.Pid, line)
			}
		}
	}()

	// stdout scanner — 整个生命周期共享
	initScanner := bufio.NewScanner(stdout)
	initScanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	// === initialize ===
	initMsg := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"Leros","version":"dev"},"capabilities":null}}`
	logs.Infof("Codex app-server initialize >> %s", initMsg)
	if _, err := fmt.Fprintln(stdin, initMsg); err != nil {
		stdin.Close()
		cmd.Process.Kill()
		return nil, fmt.Errorf("write initialize: %w", err)
	}
	if !initScanner.Scan() {
		err := initScanner.Err()
		if err == nil {
			err = fmt.Errorf("stdout closed")
		}
		stdin.Close()
		cmd.Process.Kill()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	logs.Infof("Codex app-server initialize << %s", initScanner.Text())

	// === initialized ===
	if _, err := fmt.Fprintln(stdin, `{"jsonrpc":"2.0","method":"initialized"}`); err != nil {
		stdin.Close()
		cmd.Process.Kill()
		return nil, fmt.Errorf("write initialized: %w", err)
	}

	srv := &AppServer{
		binary:     binary,
		workDir:    workDir,
		cmd:        cmd,
		scanner:    initScanner,
		stdin:      stdin,
		pending:    make(map[int64]chan *rpcResponse),
		lastActive: time.Now(),
		done:       make(chan struct{}),
	}

	// ReadLoop goroutine — 共享 scanner 持续读取 stdout
	go func() {
		srv.readLoop(ctx)
		srv.markClosed()
		cmd.Wait()
	}()

	return srv, nil
}

// ============================================================================
// 生命周期方法
// ============================================================================

func (s *AppServer) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy = true
	s.lastActive = time.Now()
}

func (s *AppServer) Unlock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy = false
	s.lastActive = time.Now()
	s.turnID = ""
}

func (s *AppServer) ThreadID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threadID
}

func (s *AppServer) TurnID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnID
}

func (s *AppServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return nil
}

func (s *AppServer) Stop() error { return s.Close() }

func (s *AppServer) PID() int {
	if s.cmd == nil || s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

func (s *AppServer) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *AppServer) markClosed() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	close(s.done)
}

// ============================================================================
// 状态存取（供 invoker 使用）
// ============================================================================

func (s *AppServer) SetEventChannel(ch chan<- events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evtChan = ch
}

func (s *AppServer) SetPendingApproval(req *ServerRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req != nil {
		cp := *req
		s.pendingApproval = &cp
	} else {
		s.pendingApproval = nil
	}
}

func (s *AppServer) PendingApproval() *ServerRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingApproval == nil {
		return nil
	}
	cp := *s.pendingApproval
	return &cp
}

func (s *AppServer) RespondApproval(ctx context.Context, reqID json.RawMessage, decision string) error {
	return s.respond(reqID, map[string]any{"decision": decision})
}

// ============================================================================
// 环境变量
// ============================================================================

func buildAppServerEnv(baseEnv []string, modelCfg engines.ModelConfig) []string {
	env := engines.BuildBaseEnv(nil)
	env = append(env, "CODEX_QUIET_MODE=1")
	modelEnv := appServerModelEnv(modelCfg)
	for k, v := range modelEnv {
		env = append(env, k+"="+v)
	}
	logs.Infof("Codex app-server env: OPENAI_API_KEY=%s OPENAI_API_BASE=%s OPENAI_BASE_URL=%s",
		maskKey(modelEnv["OPENAI_API_KEY"]),
		modelEnv["OPENAI_API_BASE"],
		modelEnv["OPENAI_BASE_URL"],
	)
	return env
}

func appServerModelEnv(model engines.ModelConfig) map[string]string {
	baseURL := strings.TrimRight(strings.TrimSpace(model.BaseURL), "/")
	if baseURL != "" && !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	return map[string]string{
		"OPENAI_API_KEY":  model.APIKey,
		"OPENAI_API_BASE": baseURL,
		"OPENAI_BASE_URL": baseURL,
	}
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "***" + key[len(key)-4:]
}
