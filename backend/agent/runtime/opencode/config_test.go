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

func TestBuildConfigContentDeclaresImageModalityOnlyForVisionModel(t *testing.T) {
	for _, vision := range []bool{true, false} {
		content, err := buildConfigContent(agent.ModelConfig{Model: "gpt-4o", Vision: vision}, nil)
		if err != nil {
			t.Fatalf("build config content (vision=%v): %v", vision, err)
		}

		var cfg configContent
		if err := json.Unmarshal([]byte(content), &cfg); err != nil {
			t.Fatalf("unmarshal config content: %v", err)
		}

		p, ok := cfg.Provider[providerID]
		if !ok {
			t.Fatalf("provider %q missing: %#v", providerID, cfg.Provider)
		}
		m, ok := p.Models["gpt-4o"]
		if !ok {
			t.Fatalf("model gpt-4o missing: %#v", p.Models)
		}

		if vision {
			if m.Modalities == nil || m.Modalities.Input == nil {
				t.Fatalf("vision model modalities missing: %#v", m.Modalities)
			}
			contains := func(list []string, want string) bool {
				for _, s := range list {
					if s == want {
						return true
					}
				}
				return false
			}
			if !contains(m.Modalities.Input, "image") {
				t.Fatalf("vision model modalities.Input lacks image: %#v", m.Modalities.Input)
			}
			// 仅声明模型真正支持的模态；未声明（video/audio/pdf）由 opencode 降级，
			// 避免声明过宽导致 AI SDK 层对不支持的 file part 返回硬错误。
			for _, unsupported := range []string{"audio", "video", "pdf"} {
				if contains(m.Modalities.Input, unsupported) {
					t.Fatalf("vision model modalities.Input should not declare %q: %#v", unsupported, m.Modalities.Input)
				}
			}
			if m.Modalities.Output == nil || len(m.Modalities.Output) == 0 {
				t.Fatalf("vision model modalities.Output missing: %#v", m.Modalities.Output)
			}
			if !contains(m.Modalities.Output, "text") {
				t.Fatalf("vision model modalities.Output lacks text: %#v", m.Modalities.Output)
			}
			if len(m.Modalities.Output) != 1 {
				t.Fatalf("vision model modalities.Output should declare text only: %#v", m.Modalities.Output)
			}
		} else if m.Modalities != nil {
			t.Fatalf("non-vision model should omit modalities, got %#v", m.Modalities)
		}
	}
}

func TestBuildConfigContentInjectsSamplingParamsAndLimit(t *testing.T) {
	topP := 0.95
	freq := 0.1
	presence := 0.0
	content, err := buildConfigContent(agent.ModelConfig{
		Model:            "Qwen3.6-27B",
		TopP:             &topP,
		FrequencyPenalty: &freq,
		PresencePenalty:  &presence,
		ContextLimit:     82144,
		OutputLimit:      42768,
	}, nil)
	if err != nil {
		t.Fatalf("build config content: %v", err)
	}
	var cfg configContent
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		t.Fatalf("unmarshal config content: %v", err)
	}
	m := cfg.Provider[providerID].Models["Qwen3.6-27B"]
	if m.Limit.Context != 82144 || m.Limit.Output != 42768 {
		t.Fatalf("limit = %#v, want {82144,42768}", m.Limit)
	}
	if m.Options == nil {
		t.Fatalf("options missing: %#v", m)
	}
	if v, ok := m.Options["top_p"].(float64); !ok || v != 0.95 {
		t.Fatalf("top_p = %#v, want 0.95", m.Options["top_p"])
	}
	if v, ok := m.Options["frequency_penalty"].(float64); !ok || v != 0.1 {
		t.Fatalf("frequency_penalty = %#v, want 0.1", m.Options["frequency_penalty"])
	}
	if v, ok := m.Options["presence_penalty"].(float64); !ok || v != 0 {
		t.Fatalf("presence_penalty = %#v, want 0", m.Options["presence_penalty"])
	}
}

func TestBuildConfigContentFallsBackToDefaultLimit(t *testing.T) {
	content, err := buildConfigContent(agent.ModelConfig{Model: "default-model"}, nil)
	if err != nil {
		t.Fatalf("build config content: %v", err)
	}
	var cfg configContent
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		t.Fatalf("unmarshal config content: %v", err)
	}
	m := cfg.Provider[providerID].Models["default-model"]
	if m.Limit.Context != 200000 || m.Limit.Output != 16384 {
		t.Fatalf("limit = %#v, want default {200000,16384}", m.Limit)
	}
	if m.Options != nil {
		t.Fatalf("options should be nil when no sampling params, got %#v", m.Options)
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
