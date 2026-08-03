package claude

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
)

func TestWriteMCPConfigUsesSSETransport(t *testing.T) {
	path, err := writeMCPConfig(t.TempDir(), []agent.MCPServerConfig{
		{Name: "baidu-netdisk", Transport: "sse", URL: "https://example.com/sse?access_token=secret"},
	})
	if err != nil {
		t.Fatalf("writeMCPConfig() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read MCP config: %v", err)
	}
	var config struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("decode MCP config: %v", err)
	}
	if config.MCPServers["baidu-netdisk"]["type"] != "sse" {
		t.Fatalf("MCP config = %s", raw)
	}
}
