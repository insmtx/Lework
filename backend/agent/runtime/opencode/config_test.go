package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
)

func TestEnsureOpenCodeDBPathUsesConfiguredDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), openCodeDataDirName)

	path, err := ensureOpenCodeDBPath(dataDir)
	if err != nil {
		t.Fatalf("ensure opencode database path: %v", err)
	}

	want := filepath.Join(dataDir, openCodeDBName)
	if path != want {
		t.Fatalf("database path = %q, want %q", path, want)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat opencode data directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("opencode data path %q is not a directory", info.Name())
	}
}

func TestBuildServerEnvOverridesInheritedOpenCodeDB(t *testing.T) {
	env := buildServerEnv(
		"secret",
		"{}",
		"/workspace/.opencode/opencode.db",
		[]string{"OPENCODE_DB=/tmp/inherited.db"},
	)

	assertEnvContains(t, env, "OPENCODE_DB=/workspace/.opencode/opencode.db")
	for _, item := range env {
		if item == "OPENCODE_DB=/tmp/inherited.db" {
			t.Fatalf("inherited OPENCODE_DB was not overridden: %#v", env)
		}
	}
}

func TestBuildConfigContentSetsBuildAgentPrompt(t *testing.T) {
	content, err := buildConfigContent(agent.ModelConfig{Model: "gpt-test"}, nil)
	if err != nil {
		t.Fatalf("build config content: %v", err)
	}

	var cfg configContent
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		t.Fatalf("unmarshal config content: %v", err)
	}

	buildAgent, ok := cfg.Agent["build"]
	if !ok {
		t.Fatalf("build agent config missing: %#v", cfg.Agent)
	}
	if buildAgent.Prompt != openCodeBuildAgentPrompt {
		t.Fatalf("build agent prompt = %q, want %q", buildAgent.Prompt, openCodeBuildAgentPrompt)
	}
}

func TestBuildMCPConfigIncludesProjectHeadersAndPreservesExplicitAuthorization(t *testing.T) {
	sourceHeaders := map[string]string{"Authorization": "Custom token", "X-Tenant": "one"}
	config := buildMCPConfig([]agent.MCPServerConfig{
		{
			Name:        "docs",
			URL:         "https://example.com/mcp",
			Headers:     sourceHeaders,
			BearerToken: "builtin-token",
		},
	})
	entry, ok := config["docs"].(map[string]any)
	if !ok {
		t.Fatalf("MCP entry = %#v", config["docs"])
	}
	headers, ok := entry["headers"].(map[string]string)
	if !ok {
		t.Fatalf("MCP headers = %#v", entry["headers"])
	}
	if headers["Authorization"] != "Custom token" || headers["X-Tenant"] != "one" {
		t.Fatalf("MCP headers = %#v", headers)
	}
	headers["X-Tenant"] = "changed"
	if sourceHeaders["X-Tenant"] != "one" {
		t.Fatalf("buildMCPConfig mutated request headers: %#v", sourceHeaders)
	}
}

func TestBuildMCPConfigIncludesStdioEnvironment(t *testing.T) {
	config := buildMCPConfig([]agent.MCPServerConfig{
		{
			Name:    "sqlite",
			Command: "npx",
			Args:    []string{"-y", "@example/mcp"},
			Env:     map[string]string{"LOG_LEVEL": "debug"},
		},
	})
	entry, ok := config["sqlite"].(map[string]any)
	if !ok {
		t.Fatalf("MCP entry = %#v", config["sqlite"])
	}
	command, ok := entry["command"].([]string)
	if !ok || len(command) != 3 || command[0] != "npx" {
		t.Fatalf("MCP command = %#v", entry["command"])
	}
	environment, ok := entry["environment"].(map[string]string)
	if !ok || environment["LOG_LEVEL"] != "debug" {
		t.Fatalf("MCP environment = %#v", entry["environment"])
	}
}

func TestBuildMCPConfigBridgesSSEThroughMCPRemote(t *testing.T) {
	config := buildMCPConfig([]agent.MCPServerConfig{
		{Name: "baidu-netdisk", Transport: "sse", URL: "https://example.com/sse?access_token=secret"},
	})
	entry, ok := config["baidu-netdisk"].(map[string]any)
	if !ok || entry["type"] != "local" {
		t.Fatalf("MCP entry = %#v", config["baidu-netdisk"])
	}
	command, ok := entry["command"].([]string)
	if !ok || len(command) != 6 || command[0] != "npx" || command[5] != "sse-only" {
		t.Fatalf("MCP command = %#v", entry["command"])
	}
}
