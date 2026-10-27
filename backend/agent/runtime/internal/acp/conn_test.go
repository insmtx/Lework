package acp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

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
	binary, countFile := writeFakeACP(t, "timeout")

	startedAt := time.Now()
	_, err := Start(context.Background(), Config{
		Binary: binary,
		Env: []string{
			"FAKE_ACP_MODE=timeout",
			"FAKE_ACP_COUNT_FILE=" + countFile,
		},
		// python 冷启动本身要几十毫秒，超时窗口留足，避免进程还没执行到脚本就被杀。
		StartupTimeout: 600 * time.Millisecond,
	})
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
	binary, countFile := writeFakeACP(t, "error")

	_, err := Start(context.Background(), Config{
		Binary: binary,
		Env: []string{
			"FAKE_ACP_MODE=error",
			"FAKE_ACP_COUNT_FILE=" + countFile,
		},
		StartupTimeout: time.Second,
	})
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
	binary, countFile := writeFakeACP(t, "auth")

	_, err := Start(context.Background(), Config{
		Binary: binary,
		Env: []string{
			"FAKE_ACP_MODE=auth",
			"FAKE_ACP_COUNT_FILE=" + countFile,
		},
		StartupTimeout: time.Second,
	})
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
	binary, _ := writeFakeACP(t, "exit-after-init")

	conn, err := Start(context.Background(), Config{
		Binary:         binary,
		Env:            []string{"FAKE_ACP_MODE=exit-after-init"},
		StartupTimeout: time.Second,
	})
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

// writeFakeACP 生成一个最小 ACP agent（python），用 FAKE_ACP_MODE 切换行为。
func writeFakeACP(t *testing.T, mode string) (binary, countFile string) {
	t.Helper()
	dir := t.TempDir()
	binary = filepath.Join(dir, "fake-acp-"+mode)
	countFile = filepath.Join(dir, "spawns.log")
	if err := os.WriteFile(binary, []byte(fakeACPScript), 0o755); err != nil {
		t.Fatalf("write fake acp: %v", err)
	}
	return binary, countFile
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read spawn log: %v", err)
	}
	return len(strings.Fields(string(data)))
}

const fakeACPScript = `#!/usr/bin/env python3
import json, os, sys, time

mode = os.environ.get("FAKE_ACP_MODE", "ok")
resume = os.environ.get("FAKE_ACP_RESUME", "ok")
model = os.environ.get("FAKE_ACP_MODEL", "")
prompt_mode = os.environ.get("FAKE_ACP_PROMPT", "ok")
count_file = os.environ.get("FAKE_ACP_COUNT_FILE")
cancel_file = os.environ.get("FAKE_ACP_CANCEL_FILE")
pending_prompt = None

if count_file:
    with open(count_file, "a") as handle:
        handle.write("spawn\n")

def send(payload):
    sys.stdout.write(json.dumps(payload) + "\n")
    sys.stdout.flush()

def config_options():
    if not model:
        return []
    return [{"id": "model", "name": "Model", "type": "select",
             "currentValue": model, "options": []}]

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        message = json.loads(line)
    except Exception:
        continue
    method = message.get("method")
    message_id = message.get("id")

    if method == "initialize":
        if mode == "timeout":
            time.sleep(3600)
            continue
        if mode == "auth":
            send({"jsonrpc": "2.0", "id": message_id,
                  "error": {"code": -32000, "message": "Authentication required"}})
            continue
        if mode == "error":
            send({"jsonrpc": "2.0", "id": message_id,
                  "error": {"code": -32603, "message": "boom"}})
            continue
        send({"jsonrpc": "2.0", "id": message_id, "result": {
            "protocolVersion": 1,
            "agentCapabilities": {"sessionCapabilities": {}},
            "authMethods": []}})
        if mode == "exit-after-init":
            sys.exit(0)
    elif method == "session/new":
        send({"jsonrpc": "2.0", "id": message_id,
              "result": {"sessionId": "s1", "configOptions": config_options()}})
    elif method == "session/resume":
        if resume == "fail":
            send({"jsonrpc": "2.0", "id": message_id,
                  "error": {"code": -32602,
                            "message": "session does not belong to cwd"}})
        else:
            send({"jsonrpc": "2.0", "id": message_id,
                  "result": {"configOptions": config_options()}})
    elif method == "session/prompt":
        if prompt_mode == "auth":
            send({"jsonrpc": "2.0", "id": message_id,
                  "error": {"code": -32000, "message": "provider authentication required"}})
        elif prompt_mode in ("silent", "ignore-cancel"):
            pending_prompt = message_id
        else:
            send({"jsonrpc": "2.0", "id": message_id,
                  "result": {"stopReason": "end_turn",
                             "usage": {"inputTokens": 7, "outputTokens": 1, "totalTokens": 8}}})
    elif method == "session/cancel":
        if cancel_file:
            with open(cancel_file, "a") as handle:
                handle.write("cancel\n")
        if prompt_mode != "ignore-cancel" and pending_prompt is not None:
            send({"jsonrpc": "2.0", "id": pending_prompt,
                  "result": {"stopReason": "cancelled"}})
            pending_prompt = None
`
