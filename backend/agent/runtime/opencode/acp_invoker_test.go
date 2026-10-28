package opencode

import (
	"context"
	"errors"
	"strings"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/insmtx/Leros/backend/agent"
)

func TestInvocationError(t *testing.T) {
	promptErr := errors.New("prompt failed")
	tests := []struct {
		name       string
		ctxErr     error
		promptErr  error
		stopReason acpsdk.StopReason
		finalText  string
		want       string
	}{
		{"success", nil, nil, acpsdk.StopReasonEndTurn, "hello", ""},
		{"prompt error", nil, promptErr, acpsdk.StopReasonEndTurn, "hello", "prompt failed"},
		{"ctx canceled", context.Canceled, nil, acpsdk.StopReasonEndTurn, "hello", context.Canceled.Error()},
		{"refusal", nil, nil, acpsdk.StopReasonRefusal, "", "拒绝回答"},
		{"empty text", nil, nil, acpsdk.StopReasonEndTurn, "   ", fallbackReply},
		{"echo", nil, nil, acpsdk.StopReasonEndTurn, "【用户问题】重复", fallbackReply},
		{"max tokens is success", nil, nil, acpsdk.StopReasonMaxTokens, "partial", ""},
		// 用户拒绝工具：v2 表现为 cancelled + 空正文，必须是专用文案而不是通用失败文案。
		{"rejected tool", nil, nil, acpsdk.StopReasonCancelled, "", rejectedReply},
		// 用户点停止会让 ctx 取消，此时不能被误标成"拒绝工具"。
		{"user abort is not rejection", context.Canceled, nil, acpsdk.StopReasonCancelled, "", context.Canceled.Error()},
		// 取消但已有正文：仍按已有正文成功处理，避免吞掉部分输出。
		{"cancelled with text", nil, nil, acpsdk.StopReasonCancelled, "部分结果", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := invocationError(tt.ctxErr, tt.promptErr, tt.stopReason, tt.finalText)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want contains %q", err, tt.want)
			}
		})
	}
}

func TestUsageFromACP(t *testing.T) {
	cachedRead := 9
	usage := usageFromACP(&acpsdk.Usage{
		InputTokens:      100,
		OutputTokens:     20,
		CachedReadTokens: &cachedRead,
	})
	if usage.InputTokens != 100 || usage.OutputTokens != 20 || usage.TotalTokens != 120 {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.CacheInputTokens != 9 {
		t.Fatalf("cache input = %d, want 9", usage.CacheInputTokens)
	}

	if zero := usageFromACP(nil); zero == nil || zero.TotalTokens != 0 {
		t.Fatalf("nil usage fallback = %+v", zero)
	}
}

func TestACPMCPServersMapsTransports(t *testing.T) {
	servers := acpMCPServers([]agent.MCPServerConfig{
		{Name: "sse-srv", Transport: "sse", URL: "https://example.com/sse", Headers: map[string]string{"Authorization": "Bearer secret"}},
		{Name: "http-srv", Transport: "http", URL: "https://example.com/mcp"},
		{Name: "stdio-srv", Command: "node", Args: []string{"server.js"}, Env: map[string]string{"TOKEN": "x"}},
	})
	if len(servers) != 3 {
		t.Fatalf("servers = %d, want 3", len(servers))
	}
	sse := servers[0]
	if sse.Stdio == nil || sse.Http != nil {
		t.Fatalf("sse must use stdio bridge: %+v", sse)
	}
	if sse.Stdio.Command != "npx" {
		t.Fatalf("sse bridge command = %q", sse.Stdio.Command)
	}
	joined := strings.Join(sse.Stdio.Args, " ")
	if !strings.Contains(joined, "mcp-remote") || !strings.Contains(joined, "sse-only") {
		t.Fatalf("sse bridge args = %v", sse.Stdio.Args)
	}
	if strings.Contains(joined, "secret") {
		t.Fatalf("sse token leaked into args: %v", sse.Stdio.Args)
	}
	if len(sse.Stdio.Env) == 0 {
		t.Fatal("sse headers must be passed via env")
	}
	if servers[1].Http == nil || servers[1].Http.Type != "http" {
		t.Fatalf("http server mapping = %+v", servers[1])
	}
	if servers[2].Stdio == nil || servers[2].Stdio.Command != "node" {
		t.Fatalf("stdio server mapping = %+v", servers[2])
	}
}
