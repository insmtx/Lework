package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

// TestOpenCodeACPProbe 是 P1 的 SDK 版探针：initialize → session/new → prompt，
// 覆盖设计文档 §15.2 的 5 条自检。
//
// 需要真实 opencode 2.1.x 与网络（默认用免费模型），因此默认跳过：
//
//	LEWORK_ACP_PROBE=1 go test ./backend/agent/runtime/internal/acp -run TestOpenCodeACPProbe -v
//
// 追加 LEWORK_ACP_RECORD=1 把原始帧写入 testdata/acp/，供 W6-2 回放。
func TestOpenCodeACPProbe(t *testing.T) {
	if os.Getenv("LEWORK_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEWORK_ACP_PROBE=1 (needs opencode 2.1.x and network)")
	}

	binary := envOrDefault("LEWORK_ACP_PROBE_BIN", "opencode")
	model := envOrDefault("LEWORK_ACP_PROBE_MODEL", "opencode/glm-5.3-flash-free")
	configContent, err := probeConfigContent(model)
	if err != nil {
		t.Fatalf("build probe config: %v", err)
	}

	root, workDir := newProbeWorkspace(t)

	frameSink, closeSink := openFrameSink(t)
	if closeSink != nil {
		defer closeSink()
	}

	env := append(probeBaseEnv(root), "OPENCODE_CONFIG_CONTENT="+configContent)

	// 全局库隔离：探针全程只用 OPENCODE_DB/XDG 指向的临时库，
	// 真实 ~/.local/share/opencode/opencode.db 的 mtime/size 必须不变。
	dbPath, dbBefore, hasGlobalDB := fingerprintGlobalDB()
	defer func() {
		if hasGlobalDB {
			assertFileUnchanged(t, dbPath, dbBefore)
		}
	}()

	conn, err := Start(context.Background(), Config{
		Binary:    binary,
		Dir:       workDir,
		Env:       env,
		FrameSink: frameSink,
	})
	if err != nil {
		t.Fatalf("start acp: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("close acp: %v", err)
		}
	}()

	ctx := context.Background()

	// 自检 1：initialize 声明 session/resume。
	t.Run("check1_resume_capability", func(t *testing.T) {
		caps := conn.Initialize().AgentCapabilities.SessionCapabilities
		if caps.Resume == nil {
			t.Fatalf("agentCapabilities.sessionCapabilities.resume is missing: %+v", caps)
		}
		t.Logf("resume capability advertised; agentInfo=%+v", conn.Initialize().AgentInfo)
	})

	// 自检 2：configOptions.model.currentValue 等于注入值。
	sess, err := conn.NewSession(ctx, workDir, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	t.Run("check2_config_injection", func(t *testing.T) {
		if err := sess.RequireModel(model); err != nil {
			t.Fatalf("%v (mode=%v)", err, mustValue(sess.ModeCurrentValue()))
		}
		if got, _ := sess.ModelCurrentValue(); got != model {
			t.Fatalf("model currentValue=%q, want %q", got, model)
		}
		t.Logf("session=%s model=%s", sess.ID, model)
	})

	// 自检 3：一轮正常结束且有 usage。
	t.Run("check3_prompt_usage", func(t *testing.T) {
		resp, err := sess.Prompt(ctx, "只回答两个字：收到")
		if err != nil {
			t.Fatalf("prompt: %v%s", err, stderrSuffix(conn))
		}
		if resp.StopReason != acpsdk.StopReasonEndTurn {
			t.Fatalf("stopReason=%q, want %q", resp.StopReason, acpsdk.StopReasonEndTurn)
		}
		if resp.Usage == nil || resp.Usage.InputTokens <= 0 {
			t.Fatalf("usage=%+v, want inputTokens > 0", resp.Usage)
		}
		t.Logf("stopReason=%s usage=%+v text=%q", resp.StopReason, *resp.Usage, truncate(conn.Client().Snapshot().Text, 120))
	})

	// 自检 4：从 Title/Kind 推导工具名（bash 走 Kind，read 走 Title）。
	t.Run("check4_tool_name_derivation", func(t *testing.T) {
		before := conn.Client().Snapshot().UpdateCount
		_, err := sess.Prompt(ctx,
			"请依次做两件事：1) 用 bash 工具执行命令 `echo hello`；"+
				"2) 用 read 工具读取当前目录的 README.md。最后用一句话总结。")
		if err != nil {
			t.Fatalf("tool prompt: %v%s", err, stderrSuffix(conn))
		}

		calls := toolCallsSince(conn.Client().Snapshot().Updates, before)
		names := map[string]string{}
		for _, call := range calls {
			names[deriveToolName(call.Kind, call.Title)] = call.Title
			t.Logf("toolCall %s kind=%q title=%q status=%q", call.ID, call.Kind, call.Title, call.Status)
		}
		if _, ok := names["shell"]; !ok {
			t.Fatalf("no execute tool observed (names=%v)", names)
		}
		if _, ok := names["read"]; !ok {
			t.Fatalf("read tool title did not resolve to tool name (names=%v)", names)
		}
	})

	// 自检 5：elicitation 能收到含 q0 的表单。
	t.Run("check5_elicitation_form", func(t *testing.T) {
		before := len(conn.Client().Snapshot().Elicitations)
		_, err := sess.Prompt(ctx,
			"请调用 question 工具向我提一个问题「是否继续？」，选项为：是 / 否。"+
				"必须调用工具，不要直接回答。")
		if err != nil {
			t.Fatalf("question prompt: %v%s", err, stderrSuffix(conn))
		}

		snapshot := conn.Client().Snapshot()
		if len(snapshot.Elicitations) <= before {
			t.Fatalf("no elicitation received; toolCalls=%v", snapshotToolTitles(snapshot))
		}
		form := snapshot.Elicitations[before]
		if _, ok := form.Properties["q0"]; !ok {
			t.Fatalf("form properties have no q0: %v", form.Properties)
		}
		t.Logf("elicitation message=%q q0=%v answered=%v",
			form.Message, form.Properties["q0"], form.Answered)
	})
}

// deriveToolName 复刻设计文档 §5.5 的推导规则：
// execute 用 Kind 判定，其余（根会话）Title 即工具名；子会话前缀已压平。
func deriveToolName(kind, title string) string {
	if kind == string(acpsdk.ToolKindExecute) {
		return "shell"
	}
	title = strings.TrimSpace(title)
	if idx := strings.LastIndex(title, ": "); idx >= 0 {
		title = title[idx+2:]
	}
	return title
}

func toolCallsSince(updates []acpsdk.SessionNotification, from int) []ToolCallState {
	calls := map[string]*ToolCallState{}
	for _, update := range updates[min(from, len(updates)):] {
		switch {
		case update.Update.ToolCall != nil:
			call := update.Update.ToolCall
			state := &ToolCallState{ID: string(call.ToolCallId)}
			state.Title = call.Title
			state.Kind = string(call.Kind)
			state.Status = string(call.Status)
			calls[state.ID] = state
		case update.Update.ToolCallUpdate != nil:
			call := update.Update.ToolCallUpdate
			state := calls[string(call.ToolCallId)]
			if state == nil {
				state = &ToolCallState{ID: string(call.ToolCallId)}
				calls[state.ID] = state
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
		}
	}
	out := make([]ToolCallState, 0, len(calls))
	for _, call := range calls {
		out = append(out, *call)
	}
	return out
}

func snapshotToolTitles(snapshot ClientSnapshot) []string {
	titles := make([]string, 0, len(snapshot.ToolCalls))
	for _, call := range snapshot.ToolCalls {
		titles = append(titles, fmt.Sprintf("%s(%s)", call.Kind, call.Title))
	}
	return titles
}

func openFrameSink(t *testing.T) (io.Writer, func()) {
	t.Helper()
	return openFrameSinkNamed(t, "opencode-2.1.4-probe.ndjson")
}

func openFrameSinkNamed(t *testing.T, name string) (io.Writer, func()) {
	t.Helper()
	if os.Getenv("LEWORK_ACP_RECORD") != "1" {
		return nil, nil
	}
	path := filepath.Join("testdata", "acp", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create frame dir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create frame sink: %v", err)
	}
	t.Logf("recording frames to %s", path)
	return f, func() { _ = f.Close() }
}

// newProbeWorkspace 准备隔离的 workspace 与 XDG 目录。
func newProbeWorkspace(t *testing.T) (root, workDir string) {
	t.Helper()
	root = t.TempDir()
	workDir = filepath.Join(root, "workspace")
	for _, dir := range []string{
		workDir,
		filepath.Join(root, "xdg", "data"),
		filepath.Join(root, "xdg", "config"),
		filepath.Join(root, "xdg", "state"),
		filepath.Join(root, "xdg", "cache"),
		filepath.Join(root, "data"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create probe dir %s: %v", dir, err)
		}
	}
	// macOS 的 /tmp 是 /private/tmp 的软链；权限规则按真实路径匹配，
	// 这里统一解析，避免 allow 规则因路径前缀不同而失效。
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}
	if err := os.WriteFile(
		filepath.Join(workDir, "README.md"),
		[]byte("# probe workspace\nhello from readme\n"),
		0o644,
	); err != nil {
		t.Fatalf("write probe README: %v", err)
	}
	return root, workDir
}

func probeBaseEnv(root string) []string {
	return []string{
		"XDG_DATA_HOME=" + filepath.Join(root, "xdg", "data"),
		"XDG_CONFIG_HOME=" + filepath.Join(root, "xdg", "config"),
		"XDG_STATE_HOME=" + filepath.Join(root, "xdg", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "xdg", "cache"),
		"OPENCODE_DB=" + filepath.Join(root, "data", "opencode.db"),
	}
}

// probeConfigMap 读取现有 opencode 配置并裁剪成最小注入配置。
// 默认只注入模型；LEWORK_ACP_PROBE_CONFIG_FILE 指向一份已有 opencode 配置时，
// 取该配置里对应 provider 的条目（含 baseURL/凭据）并裁掉 mcp 等无关段。
func probeConfigMap(model string) (map[string]any, error) {
	path := strings.TrimSpace(os.Getenv("LEWORK_ACP_PROBE_CONFIG_FILE"))
	cfg := map[string]any{}
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, err
		}
	}
	cfg["model"] = model
	delete(cfg, "mcp")
	providerID := strings.SplitN(model, "/", 2)[0]
	if providers, ok := cfg["provider"].(map[string]any); ok {
		if entry, ok := providers[providerID]; ok {
			cfg["provider"] = map[string]any{providerID: entry}
		}
	}
	return cfg, nil
}

func probeConfigContent(model string) (string, error) {
	cfg, err := probeConfigMap(model)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type fileFingerprint struct {
	Size    int64
	ModTime time.Time
}

// fingerprintGlobalDB 记录真实全局库的指纹；文件不存在时返回 false（跳过断言）。
func fingerprintGlobalDB() (string, fileFingerprint, bool) {
	path := strings.TrimSpace(os.Getenv("LEWORK_ACP_PROBE_GLOBAL_DB"))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fileFingerprint{}, false
		}
		path = filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	}
	info, err := os.Stat(path)
	if err != nil {
		return path, fileFingerprint{}, false
	}
	return path, fileFingerprint{Size: info.Size(), ModTime: info.ModTime()}, true
}

func assertFileUnchanged(t *testing.T, path string, want fileFingerprint) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("global opencode db disappeared: %v", err)
		return
	}
	if info.Size() != want.Size || !info.ModTime().Equal(want.ModTime) {
		t.Errorf("global opencode db changed: size %d->%d, mtime %s->%s",
			want.Size, info.Size(), want.ModTime, info.ModTime())
	}
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func mustValue(value string, ok bool) string {
	if !ok {
		return "<missing>"
	}
	return value
}

func stderrSuffix(conn *Conn) string {
	lines := conn.StderrTail(3)
	if len(lines) == 0 {
		return ""
	}
	return "; stderr: " + strings.Join(lines, " | ")
}

func truncate(text string, limit int) string {
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
