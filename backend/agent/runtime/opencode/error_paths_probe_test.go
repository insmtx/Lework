package opencode

import (
	"context"
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

// TestProviderErrorPaths 验证失败路径的可读性：provider 401/500 必须让结果以
// 非空、含上游信息的错误失败，而不是静默空结果。
func TestProviderErrorPaths(t *testing.T) {
	if os.Getenv("LEWORK_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEWORK_ACP_PROBE=1")
	}
	tests := []struct {
		name     string
		status   int
		body     string
		wantSub  string
		wantKind acp.ErrorKind
	}{
		{
			name:   "invalid api key",
			status: http.StatusUnauthorized,
			body:   `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`,
			// opencode 会把 provider 401 归一成 -32000，不保留上游原文。
			wantSub:  "Authentication required",
			wantKind: acp.ErrorKindAuthRequired,
		},
		{
			name:     "upstream 500",
			status:   http.StatusInternalServerError,
			body:     `{"error":{"message":"upstream boom","type":"server_error"}}`,
			wantSub:  "upstream boom",
			wantKind: acp.ErrorKindRuntime,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			root := t.TempDir()
			if err := os.MkdirAll(root+"/workspace", 0o755); err != nil {
				t.Fatal(err)
			}
			invoker := NewACPInvoker("opencode", nil, root+"/data", time.Minute)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			handle, err := invoker.Invoke(ctx, cli.InvocationRequest{
				ExecutionID: "error-probe-" + tt.name,
				WorkDir:     root + "/workspace",
				Prompt:      "只回答两个字：收到",
				Model: agent.ModelConfig{
					Model:   "mock-error",
					APIKey:  "bad-key",
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
			if result.Err == nil {
				t.Fatalf("expected failure, got message %q", result.Message)
			}
			message := result.Err.Error()
			if strings.TrimSpace(message) == "" || message == fallbackReply {
				t.Fatalf("error is not actionable: %q", message)
			}
			if tt.wantSub != "" && !strings.Contains(message, tt.wantSub) {
				t.Fatalf("error %q does not contain %q", message, tt.wantSub)
			}
			if kind := acp.ClassifyError(result.Err); kind != tt.wantKind {
				t.Fatalf("error kind = %q, want %q (err=%v)", kind, tt.wantKind, result.Err)
			}
			t.Logf("error surfaced kind=%s: %s", tt.wantKind, message)
		})
	}
}
