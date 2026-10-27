// Package acp 是 ACP（Agent Client Protocol）传输层的雏形。
//
// 当前只覆盖 P1 探针需要的三段：spawn + initialize（conn.go）、
// 新建会话与 configOptions 自检（session.go）、单轮 Prompt（prompt.go）。
// 事件映射、权限/表单桥、背压与空闲超时按设计文档 §13.1 在 W2/W3 拆到
// client.go、translate.go、state.go、elicit.go、errors.go。
package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

const (
	// ClientName 会随 initialize 发给 agent，出现在其日志与 session 元数据里。
	ClientName = "leros"
	// ClientVersion 只是探针标识，不代表 Leros 产品版本。
	ClientVersion = "0.1.0"
	// DefaultStartupTimeout 覆盖「进程启动到 initialize 返回」这一段。
	DefaultStartupTimeout = 15 * time.Second
	// shutdownGrace 是 Close 等待子进程自行退出的上限；与设计文档 §3.3 一致。
	shutdownGrace = 5 * time.Second
	defaultBinary = "opencode"
	// startMaxAttempts 是启动总尝试次数：首次 + 失败后重试 1 次（§3.1）。
	startMaxAttempts = 2
)

// 启动失败分类，调用方可用 errors.Is 区分；重试策略见 Start。
var (
	ErrSpawnFailed       = errors.New("acp spawn failed")
	ErrInitializeFailed  = errors.New("acp initialize failed")
	ErrInitializeTimeout = errors.New("acp initialize timeout")
)

// Config 描述一次 ACP 子进程会话。
type Config struct {
	// Binary 为空时回退到 PATH 上的 opencode。
	Binary string
	// Args 为空时使用 ["acp"]。
	Args []string
	// Dir 是子进程工作目录，空则继承父进程。
	Dir string
	// Env 追加到 os.Environ() 之后，用于 OPENCODE_CONFIG_CONTENT / XDG_* 等。
	Env []string
	// StartupTimeout 为零时使用 DefaultStartupTimeout。
	StartupTimeout time.Duration
	// FrameSink 非空时，把双向原始 JSON-RPC 帧按 ndjson 追加写入，
	// 每行 {"dir":"c2a"|"a2c","line":"..."}，供 W6-2 回放。
	FrameSink io.Writer
	// PermissionPolicy 决定探针对 session/request_permission 的应答：
	// "" / "once" 选 allow_once；"reject" 选 reject_once（权限隔离实测用）。
	PermissionPolicy string
}

// Conn 持有 ACP 子进程与 SDK 连接。
type Conn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	client *Client
	sdk    *acpsdk.ClientSideConnection
	init   acpsdk.InitializeResponse
	stderr *lineBuffer

	closeOnce sync.Once
	waitCh    chan error
}

// Start 启动 `opencode acp` 并完成 initialize。
//
// 失败分类（设计文档 §3.2）：spawn 失败不重试；Initialize 超时/一般错误
// 重试 1 次（新进程）；-32000（凭据缺失）不重试，直接交给调用方映射错误码。
func Start(ctx context.Context, cfg Config) (*Conn, error) {
	cfg = normalizeConfig(cfg)
	var lastErr error
	for attempt := 1; attempt <= startMaxAttempts; attempt++ {
		conn, err := startOnce(ctx, cfg)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if attempt == startMaxAttempts || !retryableStartError(err) {
			break
		}
	}
	return nil, lastErr
}

func normalizeConfig(cfg Config) Config {
	if strings.TrimSpace(cfg.Binary) == "" {
		cfg.Binary = defaultBinary
	}
	if len(cfg.Args) == 0 {
		cfg.Args = []string{"acp"}
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = DefaultStartupTimeout
	}
	return cfg
}

// retryableStartError 只对「重开一个进程可能成功」的失败重试。
func retryableStartError(err error) bool {
	var requestErr *acpsdk.RequestError
	if errors.As(err, &requestErr) {
		return requestErr.Code != -32000
	}
	return !errors.Is(err, ErrSpawnFailed)
}

func startOnce(ctx context.Context, cfg Config) (*Conn, error) {
	cmd := exec.CommandContext(ctx, cfg.Binary, cfg.Args...)
	cmd.Dir = cfg.Dir
	cmd.Env = append(os.Environ(), cfg.Env...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open acp stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open acp stdout: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open acp stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: %s %s: %v",
			ErrSpawnFailed, cfg.Binary, strings.Join(cfg.Args, " "), err)
	}

	conn := &Conn{
		cmd:    cmd,
		stdin:  stdin,
		client: NewClient(),
		stderr: newLineBuffer(40),
		waitCh: make(chan error, 1),
	}
	conn.client.SetPermissionPolicy(cfg.PermissionPolicy)
	go conn.captureStderr(stderrPipe)
	go func() { conn.waitCh <- cmd.Wait() }()

	var frames *frameRecorder
	if cfg.FrameSink != nil {
		frames = newFrameRecorder(cfg.FrameSink)
	}
	conn.sdk = acpsdk.NewClientSideConnection(
		conn.client,
		&frameWriter{dst: stdin, frames: frames},
		&frameReader{src: stdout, frames: frames},
	)

	initCtx, cancel := context.WithTimeout(ctx, cfg.StartupTimeout)
	defer cancel()
	resp, err := conn.sdk.Initialize(initCtx, acpsdk.InitializeRequest{
		ProtocolVersion: acpsdk.ProtocolVersionNumber,
		ClientCapabilities: acpsdk.ClientCapabilities{
			// 不声明则上游会取消表单并把「问题无法展示」喂给模型（设计文档 §7）。
			Elicitation: &acpsdk.ElicitationCapabilities{
				Form: &acpsdk.ElicitationFormCapabilities{},
			},
		},
		ClientInfo: &acpsdk.Implementation{Name: ClientName, Version: ClientVersion},
	})
	if err != nil {
		_ = conn.Close()
		if initCtx.Err() != nil {
			return nil, fmt.Errorf("%w: %w%s",
				ErrInitializeTimeout, initCtx.Err(), conn.stderrSuffix())
		}
		return nil, fmt.Errorf("%w: %w%s", ErrInitializeFailed, err, conn.stderrSuffix())
	}
	if resp.ProtocolVersion != acpsdk.ProtocolVersionNumber {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: protocol version %d is not supported (want %d)",
			ErrInitializeFailed, resp.ProtocolVersion, acpsdk.ProtocolVersionNumber)
	}
	conn.init = resp
	return conn, nil
}

// Initialize 返回 initialize 的原始结果，供自检读取 agentCapabilities。
func (c *Conn) Initialize() acpsdk.InitializeResponse { return c.init }

// Client 暴露观测侧（收到的 update、反向请求），只读用途。
func (c *Conn) Client() *Client { return c.client }

// PID 返回 ACP 子进程（supervisor）的 pid；私有 serve 是它的子进程。
func (c *Conn) PID() int {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

// StderrTail 返回子进程 stderr 的最后 n 行，失败诊断用。
func (c *Conn) StderrTail(n int) []string { return c.stderr.tail(n) }

// Close 关闭 stdin 让 agent 自行退出，超时后强杀。
// 对应设计文档 §3.3：父进程等 5s，确保私有 serve 的 3s 强杀窗口先走完。
func (c *Conn) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		if c.stdin != nil {
			_ = c.stdin.Close()
		}
		// stdin 关闭后先 SIGTERM，给上游 5s 清理私有 serve 的窗口；
		// 与设计文档 §3.3 的「stdin → SIGTERM → 最多 5s → SIGKILL」一致。
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Signal(syscall.SIGTERM)
		}
		select {
		case <-c.waitCh:
		case <-time.After(shutdownGrace):
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Kill()
			}
			<-c.waitCh
		}
	})
	return nil
}

func (c *Conn) captureStderr(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		c.stderr.add(scanner.Text())
	}
}

func (c *Conn) stderrSuffix() string {
	lines := c.stderr.tail(3)
	if len(lines) == 0 {
		return ""
	}
	return ": " + strings.Join(lines, " | ")
}

// ---------------------------------------------------------------------------
// 帧录制：SDK 不暴露原始 JSON-RPC，因此在 io 边界做 tee。
// ---------------------------------------------------------------------------

type frameRecorder struct {
	mu sync.Mutex
	w  io.Writer
}

func newFrameRecorder(w io.Writer) *frameRecorder { return &frameRecorder{w: w} }

func (r *frameRecorder) record(dir, line string) {
	if r == nil || strings.TrimSpace(line) == "" {
		return
	}
	payload, err := json.Marshal(map[string]string{"dir": dir, "line": line})
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.w.Write(append(payload, '\n'))
}

type frameWriter struct {
	dst    io.Writer
	frames *frameRecorder

	mu  sync.Mutex
	buf []byte
}

func (w *frameWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	if n > 0 && w.frames != nil {
		w.mu.Lock()
		w.buf = append(w.buf, p[:n]...)
		for {
			idx := bytes.IndexByte(w.buf, '\n')
			if idx < 0 {
				break
			}
			line := strings.TrimSpace(string(w.buf[:idx]))
			w.buf = w.buf[idx+1:]
			w.frames.record("c2a", line)
		}
		w.mu.Unlock()
	}
	return n, err
}

type frameReader struct {
	src    io.Reader
	frames *frameRecorder

	mu  sync.Mutex
	buf []byte
}

func (r *frameReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if n > 0 && r.frames != nil {
		r.mu.Lock()
		r.buf = append(r.buf, p[:n]...)
		for {
			idx := bytes.IndexByte(r.buf, '\n')
			if idx < 0 {
				break
			}
			line := strings.TrimSpace(string(r.buf[:idx]))
			r.buf = r.buf[idx+1:]
			r.frames.record("a2c", line)
		}
		r.mu.Unlock()
	}
	return n, err
}

// ---------------------------------------------------------------------------
// Client：SDK 回调的最小观测实现。P1 只记录，不改写语义。
// ---------------------------------------------------------------------------

// ToolCallState 是某个 toolCallId 的合并视图：start 帧带 Title/Kind，
// completed 帧不再带 Title，因此必须按 id 回填（设计文档 §13.2 缺口 1）。
type ToolCallState struct {
	ID        string
	Title     string
	Kind      string
	Status    string
	RawInput  any
	RawOutput any
}

// ElicitationRecord 是一次表单请求的观测结果。
type ElicitationRecord struct {
	Message    string
	Properties map[string]any
	Answered   map[string]any
}

// Client 实现 acp.Client 以及实验性的 UnstableCreateElicitation。
type Client struct {
	mu           sync.Mutex
	updates      []acpsdk.SessionNotification
	kinds        map[string]int
	toolCalls    map[string]*ToolCallState
	text         strings.Builder
	elicitations []ElicitationRecord
	permissions  []acpsdk.RequestPermissionRequest

	permissionPolicy string
}

// NewClient 创建观测客户端。
func NewClient() *Client {
	return &Client{kinds: map[string]int{}, toolCalls: map[string]*ToolCallState{}}
}

// SetPermissionPolicy 设置权限应答策略，必须在 Start 之前调用（探针内部使用）。
func (c *Client) SetPermissionPolicy(policy string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.permissionPolicy = strings.ToLower(strings.TrimSpace(policy))
}

// SessionUpdate 必须立即返回：阻塞会让 SDK 水位不推进、Prompt 永不返回
// （设计文档 §3.2.1）。P1 只做内存追加。
func (c *Client) SessionUpdate(_ context.Context, params acpsdk.SessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(c.updates, params)
	c.observe(params.Update)
	return nil
}

func (c *Client) observe(update acpsdk.SessionUpdate) {
	switch {
	case update.AgentMessageChunk != nil:
		c.kinds["agent_message_chunk"]++
		if update.AgentMessageChunk.Content.Text != nil {
			c.text.WriteString(update.AgentMessageChunk.Content.Text.Text)
		}
	case update.AgentThoughtChunk != nil:
		c.kinds["agent_thought_chunk"]++
	case update.ToolCall != nil:
		c.kinds["tool_call"]++
		call := update.ToolCall
		c.toolCalls[string(call.ToolCallId)] = &ToolCallState{
			ID:       string(call.ToolCallId),
			Title:    call.Title,
			Kind:     string(call.Kind),
			Status:   string(call.Status),
			RawInput: call.RawInput,
		}
	case update.ToolCallUpdate != nil:
		c.kinds["tool_call_update"]++
		call := update.ToolCallUpdate
		state := c.toolCalls[string(call.ToolCallId)]
		if state == nil {
			state = &ToolCallState{ID: string(call.ToolCallId)}
			c.toolCalls[string(call.ToolCallId)] = state
		}
		if call.Title != nil {
			state.Title = *call.Title
		}
		if call.Kind != nil {
			state.Kind = string(*call.Kind)
		}
		if call.Status != nil {
			state.Status = string(*call.Status)
		}
		if call.RawInput != nil {
			state.RawInput = call.RawInput
		}
		if call.RawOutput != nil {
			state.RawOutput = call.RawOutput
		}
	case update.UsageUpdate != nil:
		c.kinds["usage_update"]++
	case update.AvailableCommandsUpdate != nil:
		c.kinds["available_commands_update"]++
	case update.CurrentModeUpdate != nil:
		c.kinds["current_mode_update"]++
	case update.ConfigOptionUpdate != nil:
		c.kinds["config_option_update"]++
	case update.SessionInfoUpdate != nil:
		c.kinds["session_info_update"]++
	default:
		c.kinds["unknown"]++
	}
}

// RequestPermission 在探针里一律选 "once"：临时工作区内需要真实执行工具，
// 才能验证 completed 帧的 Name 回填；生产实现见设计文档 §6。
func (c *Client) RequestPermission(ctx context.Context, params acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	c.mu.Lock()
	c.permissions = append(c.permissions, params)
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return acpsdk.RequestPermissionResponse{}, ctx.Err()
	default:
	}

	wantKind := acpsdk.PermissionOptionKindAllowOnce
	if c.permissionPolicy == "reject" {
		wantKind = acpsdk.PermissionOptionKindRejectOnce
	}
	optionID := ""
	for _, opt := range params.Options {
		if opt.Kind == wantKind {
			optionID = string(opt.OptionId)
			break
		}
		if optionID == "" {
			optionID = string(opt.OptionId)
		}
	}
	if optionID == "" {
		return acpsdk.RequestPermissionResponse{}, nil
	}
	return acpsdk.RequestPermissionResponse{
		Outcome: acpsdk.RequestPermissionOutcome{
			Selected: &acpsdk.RequestPermissionOutcomeSelected{OptionId: acpsdk.PermissionOptionId(optionID)},
		},
	}, nil
}

// UnstableCreateElicitation 记录表单并按 schema 回填第一组可选值。
// 探针只关心「能否收到 q0」，不实现设计文档 §7 的完整问答映射。
func (c *Client) UnstableCreateElicitation(
	ctx context.Context,
	req acpsdk.UnstableCreateElicitationRequest,
) (acpsdk.UnstableCreateElicitationResponse, error) {
	select {
	case <-ctx.Done():
		return acpsdk.UnstableCreateElicitationResponse{}, ctx.Err()
	default:
	}
	if req.Form == nil {
		return acpsdk.UnstableCreateElicitationResponse{
			Decline: &acpsdk.UnstableCreateElicitationDecline{},
		}, nil
	}

	answers := map[string]any{}
	for key, prop := range req.Form.RequestedSchema.Properties {
		answers[key] = firstSchemaValue(prop)
	}

	c.mu.Lock()
	c.elicitations = append(c.elicitations, ElicitationRecord{
		Message:    req.Form.Message,
		Properties: req.Form.RequestedSchema.Properties,
		Answered:   answers,
	})
	c.mu.Unlock()

	return acpsdk.UnstableCreateElicitationResponse{
		Accept: &acpsdk.UnstableCreateElicitationAccept{Content: answers},
	}, nil
}

// firstSchemaValue 从 JSON Schema 属性里挑第一个可选值，避免回填非法枚举。
func firstSchemaValue(prop any) any {
	obj, ok := prop.(map[string]any)
	if !ok {
		return "yes"
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		options, ok := obj[key].([]any)
		if !ok {
			continue
		}
		for _, option := range options {
			entry, ok := option.(map[string]any)
			if !ok {
				continue
			}
			if value, ok := entry["const"]; ok {
				return value
			}
		}
	}
	if enum, ok := obj["enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}
	return "yes"
}

func (c *Client) ReadTextFile(context.Context, acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	return acpsdk.ReadTextFileResponse{}, acpsdk.NewMethodNotFound("fs/read_text_file")
}

func (c *Client) WriteTextFile(context.Context, acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	return acpsdk.WriteTextFileResponse{}, acpsdk.NewMethodNotFound("fs/write_text_file")
}

func (c *Client) CreateTerminal(context.Context, acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	return acpsdk.CreateTerminalResponse{}, acpsdk.NewMethodNotFound("terminal/create")
}

func (c *Client) KillTerminal(context.Context, acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error) {
	return acpsdk.KillTerminalResponse{}, acpsdk.NewMethodNotFound("terminal/kill")
}

func (c *Client) TerminalOutput(context.Context, acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	return acpsdk.TerminalOutputResponse{}, acpsdk.NewMethodNotFound("terminal/output")
}

func (c *Client) ReleaseTerminal(context.Context, acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	return acpsdk.ReleaseTerminalResponse{}, acpsdk.NewMethodNotFound("terminal/release")
}

func (c *Client) WaitForTerminalExit(
	context.Context,
	acpsdk.WaitForTerminalExitRequest,
) (acpsdk.WaitForTerminalExitResponse, error) {
	return acpsdk.WaitForTerminalExitResponse{}, acpsdk.NewMethodNotFound("terminal/wait_for_exit")
}

// Snapshot 返回当前观测结果的拷贝。
func (c *Client) Snapshot() ClientSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	updates := make([]acpsdk.SessionNotification, len(c.updates))
	copy(updates, c.updates)
	calls := make([]ToolCallState, 0, len(c.toolCalls))
	for _, call := range c.toolCalls {
		calls = append(calls, *call)
	}
	kinds := make(map[string]int, len(c.kinds))
	for k, v := range c.kinds {
		kinds[k] = v
	}
	elicitations := make([]ElicitationRecord, len(c.elicitations))
	copy(elicitations, c.elicitations)
	permissions := make([]acpsdk.RequestPermissionRequest, len(c.permissions))
	copy(permissions, c.permissions)
	return ClientSnapshot{
		Updates:      updates,
		UpdateCount:  len(updates),
		Text:         c.text.String(),
		ToolCalls:    calls,
		Kinds:        kinds,
		Elicitations: elicitations,
		Permissions:  permissions,
	}
}

// ClientSnapshot 是某一时刻的只读视图。
type ClientSnapshot struct {
	Updates      []acpsdk.SessionNotification
	UpdateCount  int
	Text         string
	ToolCalls    []ToolCallState
	Kinds        map[string]int
	Elicitations []ElicitationRecord
	Permissions  []acpsdk.RequestPermissionRequest
}

// lineBuffer 保存 stderr 尾部，供启动/运行失败时打印。
type lineBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func newLineBuffer(max int) *lineBuffer { return &lineBuffer{max: max} }

func (b *lineBuffer) add(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, line)
	if len(b.lines) > b.max {
		b.lines = b.lines[len(b.lines)-b.max:]
	}
}

func (b *lineBuffer) tail(n int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n <= 0 || n > len(b.lines) {
		n = len(b.lines)
	}
	out := make([]string, n)
	copy(out, b.lines[len(b.lines)-n:])
	return out
}
