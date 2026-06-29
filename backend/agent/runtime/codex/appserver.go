package codex

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/provider"
	"github.com/ygpkg/yg-go/logs"
)

// AppServer 管理一个 codex app-server 进程。
type AppServer struct {
	binary  string
	workDir string

	cmd     *exec.Cmd
	scanner *bufio.Scanner // stdout 行扫描器
	stdin   io.WriteCloser

	writeMu   sync.Mutex
	nextRPCID int64
	mu        sync.Mutex
	pending   map[int64]chan *rpcResponse
	threadID  string
	turnID    string
	closed    bool
	done      chan struct{}

	pendingApproval *ServerRequest
	onNotification  func(method string, params sonic.NoCopyRawMessage)
	onServerRequest func(req ServerRequest)
	evtChan         chan<- agent.Event
}

// ============================================================================
// 进程启动
// ============================================================================

func startAppServer(ctx context.Context, binary, workDir string, baseEnv []string, modelCfg agent.ModelConfig, mcpServers []provider.MCPServerConfig, taskDir string) (*AppServer, error) {
	codexHome := filepath.Join(taskDir, ".codex")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		return nil, fmt.Errorf("create codex-home dir: %w", err)
	}
	if err := writeCodexConfigToml(codexHome, modelCfg, mcpServers); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "stdio://")
	cmd.Dir = workDir
	cmd.Env = buildAppServerEnv(baseEnv, modelCfg, mcpServers, codexHome)

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

	logs.Infof("Codex app-server started: pid=%d workDir=%s codexHome=%s", cmd.Process.Pid, workDir, codexHome)

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

	// stdout scanner
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
		binary:  binary,
		workDir: workDir,
		cmd:     cmd,
		scanner: initScanner,
		stdin:   stdin,
		pending: make(map[int64]chan *rpcResponse),
		done:    make(chan struct{}),
	}

	// ReadLoop goroutine
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

func (s *AppServer) SetEventChannel(ch chan<- agent.Event) {
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

func (s *AppServer) RespondApproval(ctx context.Context, reqID sonic.NoCopyRawMessage, decision string) error {
	return s.respond(reqID, map[string]any{"decision": decision})
}

// ============================================================================
// config.toml 生成
// ============================================================================

func writeCodexConfigToml(codexHome string, modelCfg agent.ModelConfig, mcpServers []provider.MCPServerConfig) error {
	baseURL := strings.TrimRight(strings.TrimSpace(modelCfg.BaseURL), "/")
	if baseURL != "" && !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}

	var b strings.Builder
	b.WriteString("sandbox_mode = \"danger-full-access\"\n")
	b.WriteString("\n")
	b.WriteString("# BEGIN leros-managed memory-feature (do not edit; regenerated by daemon)\n")
	b.WriteString("features.memories = false\n")
	b.WriteString("# END leros-managed memory-feature\n")
	b.WriteString("\n")
	b.WriteString("# BEGIN leros-managed memory-config (do not edit; regenerated by daemon)\n")
	b.WriteString("memories.generate_memories = false\n")
	b.WriteString("memories.use_memories = false\n")
	b.WriteString("# END leros-managed memory-config\n")
	b.WriteString("\n")
	b.WriteString("# BEGIN leros-managed multi-agent (do not edit; regenerated by daemon)\n")
	b.WriteString("features.multi_agent = false\n")
	b.WriteString("# END leros-managed multi-agent\n")
	b.WriteString("\n")
	b.WriteString("model_provider = \"leros\"\n\n")
	b.WriteString("[model_providers.leros]\n")
	b.WriteString("name = \"leros\"\n")
	b.WriteString(fmt.Sprintf("base_url = %q\n", baseURL))
	b.WriteString("env_key = \"OPENAI_API_KEY\"\n")
	b.WriteString("wire_api = \"responses\"\n")
	b.WriteString("requires_openai_auth = false\n")

	tokenEnvVar := provider.LerosMCPTokenEnvVar()
	for _, m := range mcpServers {
		if m.Name == "" {
			continue
		}
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("[mcp_servers.%s]\n", m.Name))
		if m.Command != "" {
			// Stdio 传输
			b.WriteString(fmt.Sprintf("command = %q\n", m.Command))
			if len(m.Args) > 0 {
				b.WriteString("args = [")
				for i, arg := range m.Args {
					if i > 0 {
						b.WriteString(", ")
					}
					b.WriteString(fmt.Sprintf("%q", arg))
				}
				b.WriteString("]\n")
			}
			if len(m.Env) > 0 {
				b.WriteString(fmt.Sprintf("[mcp_servers.%s.env]\n", m.Name))
				for k, v := range m.Env {
					b.WriteString(fmt.Sprintf("%s = %q\n", k, v))
				}
			}
		} else if m.URL != "" {
			// HTTP 传输，通过 npx mcp-remote 转为 stdio
			b.WriteString("command = \"npx\"\n")
			args := []string{"-y", "mcp-remote", m.URL}
			if m.BearerToken != "" {
				args = append(args, "--header", fmt.Sprintf("Authorization: Bearer ${%s}", tokenEnvVar))
			}
			b.WriteString("args = [")
			for i, arg := range args {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(fmt.Sprintf("%q", arg))
			}
			b.WriteString("]\n")
		}
	}

	configPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(configPath, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	logs.Infof("Codex config.toml written: %s", configPath)
	return nil
}

// ============================================================================
// 环境变量
// ============================================================================

func buildAppServerEnv(baseEnv []string, modelCfg agent.ModelConfig, mcpServers []provider.MCPServerConfig, codexHome string) []string {
	env := provider.BuildBaseEnv(nil)
	env = append(env, "CODEX_QUIET_MODE=1")
	env = append(env, "CODEX_HOME="+codexHome)
	modelEnv := appServerModelEnv(modelCfg)
	for k, v := range modelEnv {
		env = append(env, k+"="+v)
	}
	tokenEnvVar := provider.LerosMCPTokenEnvVar()
	for _, m := range mcpServers {
		if m.BearerToken != "" {
			env = append(env, tokenEnvVar+"="+m.BearerToken)
			break
		}
	}
	logs.Infof("Codex app-server env: CODEX_HOME=%s OPENAI_API_KEY=%s OPENAI_API_BASE=%s OPENAI_BASE_URL=%s",
		codexHome,
		maskKey(modelEnv["OPENAI_API_KEY"]),
		modelEnv["OPENAI_API_BASE"],
		modelEnv["OPENAI_BASE_URL"],
	)
	return env
}

func appServerModelEnv(model agent.ModelConfig) map[string]string {
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
