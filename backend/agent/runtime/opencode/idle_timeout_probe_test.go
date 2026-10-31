package opencode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Lework/backend/agent"
	"github.com/insmtx/Lework/backend/agent/runtime/internal/acp"
	"github.com/insmtx/Lework/backend/agent/runtime/internal/cli"
)

// TestIdleTimeoutWatchdog 用「挂起不响应」的 mock provider 触发空闲看门狗：
// 2s 无进展后应主动 cancel，并在 5s 排空窗口内返回 v1 文案，而不是永久卡住。
func TestIdleTimeoutWatchdog(t *testing.T) {
	if os.Getenv("LEWORK_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEWORK_ACP_PROBE=1 (needs opencode 2.1.x)")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟 provider 挂死：不写响应，等客户端取消或 10s 兜底。
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer server.Close()

	root := t.TempDir()
	if err := os.MkdirAll(root+"/workspace", 0o755); err != nil {
		t.Fatal(err)
	}
	invoker := NewACPInvoker("opencode", nil, root+"/data", 2*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	started := time.Now()
	handle, err := invoker.Invoke(ctx, cli.InvocationRequest{
		ExecutionID: "idle-timeout-probe",
		WorkDir:     root + "/workspace",
		Prompt:      "只回答两个字：收到",
		Model: agent.ModelConfig{
			Model:   "mock-hang",
			APIKey:  "test-key",
			BaseURL: server.URL,
		},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	for range handle.Events {
	}
	result := <-handle.Result
	handle.Process.Stop()
	elapsed := time.Since(started)

	if !errors.Is(result.Err, acp.ErrIdleTimeout) {
		t.Fatalf("error = %v, want ErrIdleTimeout", result.Err)
	}
	if !strings.Contains(result.Err.Error(), "长时间未操作") {
		t.Fatalf("error = %q, want v1 wording", result.Err)
	}
	if elapsed > 20*time.Second {
		t.Fatalf("idle timeout took %s, want <= 20s", elapsed)
	}
	t.Logf("idle timeout converged in %s: %v", elapsed.Round(time.Millisecond), result.Err)
}
