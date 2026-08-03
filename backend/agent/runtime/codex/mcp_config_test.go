package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
)

func TestWriteCodexConfigBridgesSSEAndProtectsConfig(t *testing.T) {
	dir := t.TempDir()
	if err := writeCodexConfigToml(dir, agent.ModelConfig{BaseURL: "https://example.com"}, []agent.MCPServerConfig{
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
