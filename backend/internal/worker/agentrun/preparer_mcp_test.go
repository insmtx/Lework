package agentrun

import (
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
)

func TestRuntimeSupportsMCPConnectors(t *testing.T) {
	cases := map[string]bool{
		"opencode": true,
		"claude":   true,
		"codex":    true,
		"CODEX":    true,
		"leros":    false,
		"":         false,
	}
	for kind, want := range cases {
		if got := runtimeSupportsMCPConnectors(kind); got != want {
			t.Fatalf("runtimeSupportsMCPConnectors(%q) = %v, want %v", kind, got, want)
		}
	}
}

func TestCountMCPPluginSnapshots(t *testing.T) {
	snapshots := []agentrundomain.PluginSnapshot{
		{Kind: "mcp"},
		{Kind: "skill"},
		{Kind: "MCP"},
	}
	if got := countMCPPluginSnapshots(snapshots); got != 2 {
		t.Fatalf("countMCPPluginSnapshots() = %d, want 2", got)
	}
	if got := countMCPPluginSnapshots(nil); got != 0 {
		t.Fatalf("countMCPPluginSnapshots(nil) = %d, want 0", got)
	}
}

func TestDescribeConnectorMCPOmitsCredentials(t *testing.T) {
	got := describeConnectorMCP([]agent.MCPServerConfig{{
		Name:      "baidu-netdisk",
		Transport: "sse",
		URL:       "https://mcp-pan.baidu.com/sse?access_token=secret",
		Headers:   map[string]string{"Authorization": "Bearer secret-token"},
	}})
	if strings.Contains(got, "secret") {
		t.Fatalf("日志摘要泄露了凭据: %s", got)
	}
	if !strings.Contains(got, "baidu-netdisk[sse,auth=present,host=mcp-pan.baidu.com]") {
		t.Fatalf("日志摘要 = %s", got)
	}
	if got := describeConnectorMCP([]agent.MCPServerConfig{{Name: "local", Command: "npx"}}); got != "local[stdio,auth=none]" {
		t.Fatalf("日志摘要 = %s", got)
	}
}
