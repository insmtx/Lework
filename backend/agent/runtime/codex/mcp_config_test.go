package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
)

func TestWriteCodexConfigBridgesSSEAndProtectsConfig(t *testing.T) {
	dir := t.TempDir()
	if err := writeCodexConfigToml(context.Background(), dir, agent.ModelConfig{BaseURL: "https://example.com"}, []agent.MCPServerConfig{
		{Name: "baidu-netdisk", Transport: "sse", URL: "https://example.com/sse?access_token=secret"},
	}); err != nil {
		t.Fatalf("writeCodexConfigToml() error = %v", err)
	}
	path := filepath.Join(dir, "config.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	if !strings.Contains(string(raw), `"--transport", "sse-only"`) {
		t.Fatalf("config.toml = %s", raw)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config.toml: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config.toml mode = %o, want 600", info.Mode().Perm())
	}
}

func TestWriteCodexConfigTomlPassesConnectorHeadersViaEnv(t *testing.T) {
	dir := t.TempDir()
	if err := writeCodexConfigToml(context.Background(), dir, agent.ModelConfig{BaseURL: "https://example.com"},
		[]agent.MCPServerConfig{{
			Name:      "baidu-netdisk",
			Transport: "sse",
			URL:       "https://mcp-pan.baidu.com/sse",
			Headers:   map[string]string{"Authorization": "Bearer access-token"},
		}}); err != nil {
		t.Fatalf("writeCodexConfigToml() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(raw)
	if strings.Contains(content, "access-token") {
		t.Fatalf("token leaked into config.toml: %s", content)
	}
	if !strings.Contains(content, `"--header"`) || !strings.Contains(content, "Authorization:${") {
		t.Fatalf("config.toml 缺少认证头参数: %s", content)
	}
}
