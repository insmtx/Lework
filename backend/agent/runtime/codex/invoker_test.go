package codex

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/insmtx/Leros/backend/agent/runtime/externalcli"
)

// TestSayHi 端到端测试：通过 app-server 模式发送 "hi" 并收到真实回复。
func TestSayHi(t *testing.T) {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex CLI not found in PATH")
	}

	workDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	apiKey := os.Getenv("LEROS_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("LEROS_TEST_API_KEY not set")
	}

	adapter := NewAdapter(codexPath, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	handle, err := adapter.Invoke(ctx, externalcli.InvocationRequest{
		WorkDir: workDir,
		Prompt:  "hi",
		Model: agent.ModelConfig{
			Provider: "openai",
			APIKey:   apiKey,
			Model:    "aliyun/deepseek-v4-flash",
			BaseURL:  "http://127.0.0.1:8081",
		},
	})
	if err != nil {
		t.Fatalf("run codex adapter: %v", err)
	}

	t.Logf("codex app-server started, waiting for response...")

	var finalEvent agent.Event
	var responseText string
	for event := range handle.Events {
		t.Logf("event: type=%s content=%q", event.Type, event.Content)
		switch event.Type {
		case events.EventResult:
			responseText = strings.TrimSpace(event.Content)
		case events.EventInvocationCompleted:
			if responseText == "" && event.Content != "" {
				responseText = strings.TrimSpace(event.Content)
			}
		}
		finalEvent = event
	}

	if finalEvent.Type == events.EventInvocationFailed {
		t.Fatalf("codex execution failed: %s", finalEvent.Content)
	}

	if responseText == "" {
		t.Fatal("expected non-empty response for 'hi'")
	}
	t.Logf("codex response: %s", responseText)
}

func TestResolveThread(t *testing.T) {
	tests := []struct {
		sessionID string
		resume    bool
		wantID    string
		wantOK    bool
	}{
		{"", false, "", false},
		{"thread-1", false, "", false},
		{"thread-1", true, "thread-1", true},
		{"  thread-2  ", true, "thread-2", true},
		{"", true, "", false},
	}
	for _, tt := range tests {
		id, ok := resolveThread(tt.sessionID, tt.resume)
		if id != tt.wantID || ok != tt.wantOK {
			t.Errorf("resolveThread(%q, %v) = (%q, %v), want (%q, %v)",
				tt.sessionID, tt.resume, id, ok, tt.wantID, tt.wantOK)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "hello", "world"); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
	if got := firstNonEmpty("", "", "  "); got != "" {
		t.Errorf("expected '', got %q", got)
	}
	if got := firstNonEmpty("  a  "); got != "a" {
		t.Errorf("expected 'a', got %q", got)
	}
}

func TestAppServerModelEnv(t *testing.T) {
	env := appServerModelEnv(agent.ModelConfig{
		APIKey:  "sk-test",
		BaseURL: "http://127.0.0.1:8081",
	})
	if env["OPENAI_API_KEY"] != "sk-test" {
		t.Fatalf("unexpected api key env: %#v", env)
	}
	if env["OPENAI_API_BASE"] != "http://127.0.0.1:8081/v1" {
		t.Fatalf("unexpected api base env: %#v", env)
	}
	if env["OPENAI_BASE_URL"] != "http://127.0.0.1:8081/v1" {
		t.Fatalf("unexpected base url env: %#v", env)
	}
}

func TestAppServerModelEnvWithV1Suffix(t *testing.T) {
	env := appServerModelEnv(agent.ModelConfig{
		APIKey:  "sk-test",
		BaseURL: "http://127.0.0.1:8081/v1/",
	})
	if env["OPENAI_API_BASE"] != "http://127.0.0.1:8081/v1" {
		t.Fatalf("unexpected api base env: %#v", env)
	}
	if env["OPENAI_BASE_URL"] != "http://127.0.0.1:8081/v1" {
		t.Fatalf("unexpected base url env: %#v", env)
	}
}

func TestJSONRPCClientCallAndRespond(t *testing.T) {
	clientToServerR, clientToServerW := io.Pipe()
	serverToClientR, serverToClientW := io.Pipe()

	client := NewClient(serverToClientR, clientToServerW)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		scanner := bufio.NewScanner(clientToServerR)
		if scanner.Scan() {
			line := scanner.Text()
			var req rpcRequest
			if err := sonic.Unmarshal([]byte(line), &req); err != nil {
				t.Logf("server parse error: %v", err)
				return
			}
			resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"greeting":"hello"}}`, req.ID)
			serverToClientW.Write([]byte(resp + "\n"))
		}
	}()

	go func() {
		_ = client.ReadLoop(ctx)
	}()

	time.Sleep(10 * time.Millisecond)

	var result map[string]string
	err := client.Call(ctx, "greet", map[string]string{"name": "world"}, &result)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if result["greeting"] != "hello" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestJSONRPCClientNotification(t *testing.T) {
	serverToClientR, serverToClientW := io.Pipe()
	clientToServerR, clientToServerW := io.Pipe()
	_ = clientToServerR

	client := NewClient(serverToClientR, clientToServerW)
	_ = clientToServerW

	notifCh := make(chan string, 1)
	client.OnNotification = func(method string, params sonic.NoCopyRawMessage) {
		notifCh <- method
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = client.ReadLoop(ctx)
	}()

	serverToClientW.Write([]byte(`{"jsonrpc":"2.0","method":"thread/started","params":{"thread":{"id":"t1"}}}` + "\n"))

	select {
	case method := <-notifCh:
		if method != "thread/started" {
			t.Fatalf("expected thread/started, got %s", method)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for notification")
	}
}
