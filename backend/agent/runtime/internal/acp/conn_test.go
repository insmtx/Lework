package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

const fakeAgentEnv = "LEROS_ACP_FAKE_AGENT"

// TestFakeACPAgentProcess 是测试用 ACP agent 的入口：父测试用 os.Args[0]
// 重新执行测试二进制并带 -test.run 指向本函数，通过环境变量切换行为。
// 这样失败路径单测不依赖 python/node 等外部解释器。
func TestFakeACPAgentProcess(t *testing.T) {
	if os.Getenv(fakeAgentEnv) != "1" {
		t.Skip("helper process for fake ACP agent")
	}
	serveFakeACP()
	os.Exit(0)
}

func TestStartSpawnFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-opencode")

	_, err := Start(context.Background(), Config{
		Binary:         missing,
		StartupTimeout: 200 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected spawn failure")
	}
	if !errors.Is(err, ErrSpawnFailed) {
		t.Fatalf("error = %v, want ErrSpawnFailed", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error should contain binary path %q: %v", missing, err)
	}
}

func TestStartInitializeTimeoutRetriesOnce(t *testing.T) {
	cfg, countFile := fakeACPConfig(t, "timeout")

	startedAt := time.Now()
	_, err := Start(context.Background(), cfg)
	elapsed := time.Since(startedAt)

	if err == nil {
		t.Fatal("expected initialize timeout")
	}
	if !errors.Is(err, ErrInitializeTimeout) {
		t.Fatalf("error = %v, want ErrInitializeTimeout", err)
	}
	if got := countLines(t, countFile); got != 2 {
		t.Fatalf("spawn count = %d, want 2 (retry once)", got)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("timeout path took %s; SIGTERM cleanup likely skipped", elapsed)
	}
}

func TestStartInitializeErrorRetriesOnce(t *testing.T) {
	cfg, countFile := fakeACPConfig(t, "error")

	_, err := Start(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected initialize error")
	}
	if !errors.Is(err, ErrInitializeFailed) {
		t.Fatalf("error = %v, want ErrInitializeFailed", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want rpc message", err)
	}
	if got := countLines(t, countFile); got != 2 {
		t.Fatalf("spawn count = %d, want 2 (retry once)", got)
	}
}

func TestStartAuthErrorDoesNotRetry(t *testing.T) {
	cfg, countFile := fakeACPConfig(t, "auth")

	_, err := Start(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected auth error")
	}
	var requestErr *acpsdk.RequestError
	if !errors.As(err, &requestErr) || requestErr.Code != -32000 {
		t.Fatalf("error = %v, want RequestError -32000", err)
	}
	if got := countLines(t, countFile); got != 1 {
		t.Fatalf("spawn count = %d, want 1 (-32000 must not retry)", got)
	}
}

func TestConnFailsAfterPeerExit(t *testing.T) {
	cfg, _ := fakeACPConfig(t, "exit-after-init")

	conn, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := conn.NewSession(ctx, t.TempDir(), nil); err == nil {
		t.Fatal("expected session/new to fail after peer exit")
	}
}

// fakeACPConfig 返回以当前测试二进制为 agent 的启动配置，并给出 spawn 计数文件。
func fakeACPConfig(t *testing.T, mode string) (Config, string) {
	t.Helper()
	countFile := filepath.Join(t.TempDir(), "spawns.log")
	return Config{
		Binary: os.Args[0],
		Args:   fakeACPArgs(),
		Env: []string{
			fakeAgentEnv + "=1",
			"FAKE_ACP_MODE=" + mode,
			"FAKE_ACP_COUNT_FILE=" + countFile,
		},
		StartupTimeout: 2 * time.Second,
	}, countFile
}

func fakeACPArgs() []string {
	return []string{"-test.run=^TestFakeACPAgentProcess$"}
}

// serveFakeACP 实现最小的 ACP agent 状态机，行为由 FAKE_ACP_* 环境变量控制。
func serveFakeACP() {
	mode := os.Getenv("FAKE_ACP_MODE")
	resume := os.Getenv("FAKE_ACP_RESUME")
	model := os.Getenv("FAKE_ACP_MODEL")
	promptMode := os.Getenv("FAKE_ACP_PROMPT")
	countFile := os.Getenv("FAKE_ACP_COUNT_FILE")
	cancelFile := os.Getenv("FAKE_ACP_CANCEL_FILE")
	if countFile != "" {
		appendLine(countFile, "spawn")
	}

	out := bufio.NewWriter(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var pendingPrompt json.RawMessage

	reply := func(id json.RawMessage, result any) {
		writeJSON(out, map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	replyErr := func(id json.RawMessage, code int, message string) {
		writeJSON(out, map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error":   map[string]any{"code": code, "message": message},
		})
	}

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(line, &message); err != nil {
			continue
		}

		switch message.Method {
		case "initialize":
			switch mode {
			case "timeout":
				// 永不回复，等父进程超时/SIGTERM；用 Sleep 循环而不是空 select，
				// 避免 Go 运行时判定「所有 goroutine 都在睡觉」后 fatal。
				for {
					time.Sleep(time.Hour)
				}
			case "auth":
				replyErr(message.ID, -32000, "Authentication required")
				continue
			case "error":
				replyErr(message.ID, -32603, "boom")
				continue
			}
			reply(message.ID, map[string]any{
				"protocolVersion": 1,
				"agentCapabilities": map[string]any{
					"sessionCapabilities": map[string]any{},
				},
				"authMethods": []any{},
			})
			if mode == "exit-after-init" {
				os.Exit(0)
			}
		case "session/new":
			reply(message.ID, map[string]any{
				"sessionId":     "s1",
				"configOptions": fakeConfigOptions(model),
			})
		case "session/resume":
			if resume == "fail" {
				replyErr(message.ID, -32602, "session does not belong to cwd")
				continue
			}
			reply(message.ID, map[string]any{"configOptions": fakeConfigOptions(model)})
		case "session/prompt":
			switch promptMode {
			case "auth":
				replyErr(message.ID, -32000, "provider authentication required")
			case "silent", "ignore-cancel":
				pendingPrompt = append(json.RawMessage(nil), message.ID...)
			default:
				reply(message.ID, map[string]any{
					"stopReason": "end_turn",
					"usage":      map[string]any{"inputTokens": 7, "outputTokens": 1, "totalTokens": 8},
				})
			}
		case "session/cancel":
			if cancelFile != "" {
				appendLine(cancelFile, "cancel")
			}
			if promptMode != "ignore-cancel" && pendingPrompt != nil {
				reply(pendingPrompt, map[string]any{"stopReason": "cancelled"})
				pendingPrompt = nil
			}
		}
	}
}

func fakeConfigOptions(model string) []any {
	if model == "" {
		return []any{}
	}
	return []any{map[string]any{
		"id":           "model",
		"name":         "Model",
		"type":         "select",
		"currentValue": model,
		"options":      []any{},
	}}
}

func writeJSON(out *bufio.Writer, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = out.Write(data)
	_ = out.WriteByte('\n')
	_ = out.Flush()
}

func appendLine(path, line string) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(line + "\n")
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read log: %v", err)
	}
	return len(strings.Fields(string(data)))
}
