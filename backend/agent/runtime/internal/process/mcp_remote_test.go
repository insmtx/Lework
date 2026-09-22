package process

import (
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
)

func TestNeedsSSEBridge(t *testing.T) {
	cases := []struct {
		name string
		cfg  agent.MCPServerConfig
		want bool
	}{
		{"sse url", agent.MCPServerConfig{URL: "https://example.com/sse", Transport: "sse"}, true},
		{"sse upper", agent.MCPServerConfig{URL: "https://example.com/sse", Transport: "SSE"}, true},
		{"http url", agent.MCPServerConfig{URL: "https://example.com/mcp", Transport: "http"}, false},
		{"stdio", agent.MCPServerConfig{Command: "npx", Transport: "stdio"}, false},
		{"sse without url", agent.MCPServerConfig{Transport: "sse"}, false},
	}
	for _, tc := range cases {
		if got := NeedsSSEBridge(tc.cfg); got != tc.want {
			t.Fatalf("%s: NeedsSSEBridge() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBuildMCPRemoteHeaderArgsUsesEnvIndirection(t *testing.T) {
	args, env := BuildMCPRemoteHeaderArgs(agent.MCPServerConfig{
		Name:    "baidu-netdisk",
		URL:     "https://mcp-pan.baidu.com/sse",
		Headers: map[string]string{"Authorization": "Bearer access-token"},
	})
	if len(args) != 2 || args[0] != "--header" {
		t.Fatalf("args = %#v", args)
	}
	if !strings.HasPrefix(args[1], "Authorization:${") {
		t.Fatalf("header arg should reference an env var without a space after the colon: %q", args[1])
	}
	if strings.Contains(strings.Join(args, " "), "access-token") {
		t.Fatalf("token leaked into command line: %#v", args)
	}
	key := onlyEnvKey(t, env)
	if !strings.Contains(args[1], "${"+key+"}") {
		t.Fatalf("header arg %q does not reference env key %q", args[1], key)
	}
	if env[key] != "Bearer access-token" {
		t.Fatalf("env[%s] = %q", key, env[key])
	}
}

func TestBuildMCPRemoteHeaderArgsFallsBackToBearerToken(t *testing.T) {
	args, env := BuildMCPRemoteHeaderArgs(agent.MCPServerConfig{
		Name:        "docs",
		URL:         "https://example.com/sse",
		BearerToken: "token-2",
	})
	if len(args) != 2 {
		t.Fatalf("args = %#v", args)
	}
	key := onlyEnvKey(t, env)
	if env[key] != "Bearer token-2" {
		t.Fatalf("env[%s] = %q", key, env[key])
	}
}

func TestBuildMCPRemoteHeaderArgsPrefersExplicitAuthorization(t *testing.T) {
	_, env := BuildMCPRemoteHeaderArgs(agent.MCPServerConfig{
		Name:        "docs",
		URL:         "https://example.com/sse",
		Headers:     map[string]string{"authorization": "Custom token"},
		BearerToken: "builtin-token",
	})
	key := onlyEnvKey(t, env)
	if env[key] != "Custom token" {
		t.Fatalf("explicit header should win, got %q", env[key])
	}
}

func TestBuildMCPRemoteHeaderArgsWithoutHeaders(t *testing.T) {
	args, env := BuildMCPRemoteHeaderArgs(agent.MCPServerConfig{Name: "plain", URL: "https://example.com/sse"})
	if len(args) != 0 || len(env) != 0 {
		t.Fatalf("args = %#v env = %#v", args, env)
	}
}

func TestBuildMCPRemoteHeaderEnvOnlyForSSEAndSorted(t *testing.T) {
	env := BuildMCPRemoteHeaderEnv([]agent.MCPServerConfig{
		{
			Name: "http-server", URL: "https://example.com/mcp", Transport: "http",
			Headers: map[string]string{"Authorization": "Bearer http-token"},
		},
		{
			Name: "baidu-netdisk", URL: "https://example.com/sse", Transport: "sse",
			Headers: map[string]string{"Authorization": "Bearer sse-token"},
		},
	})
	if len(env) != 1 {
		t.Fatalf("HTTP 传输不应注入环境变量: %#v", env)
	}
	if !strings.Contains(env[0], "=Bearer sse-token") {
		t.Fatalf("env = %#v", env)
	}
}

func TestBuildMCPRemoteHeaderEnvForURLIncludesHTTP(t *testing.T) {
	mcps := []agent.MCPServerConfig{
		{
			Name: "http-server", URL: "https://example.com/mcp", Transport: "http",
			Headers: map[string]string{"Authorization": "Bearer http-token"},
		},
		{
			Name: "baidu-netdisk", URL: "https://example.com/sse", Transport: "sse",
			Headers: map[string]string{"Authorization": "Bearer sse-token"},
		},
		{Name: "local", Command: "npx"},
	}
	if got := BuildMCPRemoteHeaderEnv(mcps); len(got) != 1 {
		t.Fatalf("SSE 变体只应包含一个变量: %#v", got)
	}
	got := BuildMCPRemoteHeaderEnvForURL(mcps)
	if len(got) != 2 {
		t.Fatalf("URL 变体应包含 HTTP 与 SSE 两个变量: %#v", got)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"=Bearer http-token", "=Bearer sse-token"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("env 缺少 %q: %#v", want, got)
		}
	}
}

func TestMCPRemoteHeaderEnvNamesAreUniquePerServer(t *testing.T) {
	first, firstEnv := BuildMCPRemoteHeaderArgs(agent.MCPServerConfig{
		Name: "baidu-netdisk-a76f", URL: "https://example.com/sse",
		Headers: map[string]string{"Authorization": "Bearer one"},
	})
	second, secondEnv := BuildMCPRemoteHeaderArgs(agent.MCPServerConfig{
		Name: "baidu-netdisk-b31c", URL: "https://example.com/sse",
		Headers: map[string]string{"Authorization": "Bearer two"},
	})
	if onlyEnvKey(t, firstEnv) == onlyEnvKey(t, secondEnv) {
		t.Fatalf("不同服务端复用了同一个环境变量名: %#v", first)
	}
	if first[1] == second[1] {
		t.Fatalf("不同服务端的 --header 参数应不同: %#v", first)
	}
}

func TestDescribeMCPServersOmitsCredentials(t *testing.T) {
	got := DescribeMCPServers([]agent.MCPServerConfig{{
		Name: "baidu-netdisk", Transport: "sse", URL: "https://mcp-pan.baidu.com/sse?access_token=secret",
		Headers: map[string]string{"Authorization": "Bearer secret-token"},
	}})
	if strings.Contains(got, "secret") {
		t.Fatalf("摘要泄露了凭据: %s", got)
	}
	if !strings.Contains(got, "baidu-netdisk[sse,host=mcp-pan.baidu.com,auth=Authorization]") {
		t.Fatalf("摘要 = %s", got)
	}
}

func onlyEnvKey(t *testing.T, env map[string]string) string {
	t.Helper()
	if len(env) != 1 {
		t.Fatalf("expected exactly one env entry, got %#v", env)
	}
	for key := range env {
		return key
	}
	return ""
}
