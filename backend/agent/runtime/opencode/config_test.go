package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		"",
		[]string{"OPENCODE_DB=/tmp/inherited.db"},
	)

	assertEnvContains(t, env, "OPENCODE_DB=/workspace/.opencode/opencode.db")
	for _, item := range env {
		if item == "OPENCODE_DB=/tmp/inherited.db" {
			t.Fatalf("inherited OPENCODE_DB was not overridden: %#v", env)
		}
	}
}

func TestEnsureOpenCodeConfigHomeCreatesIsolatedDirectory(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), openCodeDataDirName)

	home, err := ensureOpenCodeConfigHome(dataDir)
	if err != nil {
		t.Fatalf("ensure opencode config home: %v", err)
	}

	want := filepath.Join(dataDir, openCodeConfigHomeName)
	if home != want {
		t.Fatalf("config home = %q, want %q", home, want)
	}
	info, err := os.Stat(home)
	if err != nil {
		t.Fatalf("stat config home: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("config home %q is not a directory", home)
	}
}

func TestEnsureOpenCodeConfigHomeSkipsEmptyDataDir(t *testing.T) {
	home, err := ensureOpenCodeConfigHome("  ")
	if err != nil {
		t.Fatalf("ensure opencode config home: %v", err)
	}
	if home != "" {
		t.Fatalf("empty data dir should skip isolation, got %q", home)
	}
}

// XDG_CONFIG_HOME 是切断宿主机 ~/.config/opencode 全局配置的唯一手段：
// 不设置它时，宿主机的 mcp/plugin 条目会被深度合入并泄漏进每次运行。
func TestBuildServerEnvInjectsIsolatedConfigHome(t *testing.T) {
	env := buildServerEnv(
		"secret",
		"{}",
		"/workspace/.opencode/opencode.db",
		"/workspace/.opencode/config-home",
		[]string{"XDG_CONFIG_HOME=/home/operator/.config"},
	)

	assertEnvContains(t, env, "XDG_CONFIG_HOME=/workspace/.opencode/config-home")
	for _, item := range env {
		if item == "XDG_CONFIG_HOME=/home/operator/.config" {
			t.Fatalf("inherited XDG_CONFIG_HOME was not overridden: %#v", env)
		}
	}
}

func TestBuildServerEnvOmitsConfigHomeWhenIsolationDisabled(t *testing.T) {
	env := buildServerEnv("secret", "{}", "", "", nil)

	for _, item := range env {
		if strings.HasPrefix(item, "XDG_CONFIG_HOME=") {
			t.Fatalf("empty config home should not inject XDG_CONFIG_HOME: %#v", env)
		}
	}
}

// 关闭内置插件与宿主机级技能发现：这些工作同样发生在首个 LLM 请求之前，
// 属于每条消息重复支付的一次性成本。
func TestBuildServerEnvDisablesDefaultPluginsAndExternalSkills(t *testing.T) {
	env := buildServerEnv("secret", "{}", "", "", nil)

	for _, want := range []string{
		"OPENCODE_DISABLE_DEFAULT_PLUGINS=1",
		"OPENCODE_DISABLE_EXTERNAL_SKILLS=1",
		"OPENCODE_DISABLE_CLAUDE_CODE_SKILLS=1",
		"OPENCODE_PURE=1",
		"OPENCODE_DISABLE_PROJECT_CONFIG=1",
	} {
		assertEnvContains(t, env, want)
	}
}

func TestBuildConfigContentSetsBuildAgentPrompt(t *testing.T) {
	content, err := buildConfigContent(agent.ModelConfig{Model: "gpt-test"}, nil, "")
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

func TestBuildConfigContentInjectsTaskSkillDirectory(t *testing.T) {
	for _, skillDir := range []string{
		"/workspace/task-1/skills",
		"/workspace/task-2/skills",
	} {
		content, err := buildConfigContent(agent.ModelConfig{Model: "gpt-test"}, nil, skillDir)
		if err != nil {
			t.Fatalf("build config content for %q: %v", skillDir, err)
		}

		var cfg configContent
		if err := json.Unmarshal([]byte(content), &cfg); err != nil {
			t.Fatalf("unmarshal config content for %q: %v", skillDir, err)
		}
		if cfg.Skills == nil || len(cfg.Skills.Paths) != 1 || cfg.Skills.Paths[0] != skillDir {
			t.Fatalf("skills config = %#v, want path %q", cfg.Skills, skillDir)
		}
	}
}

func TestBuildConfigContentOmitsEmptyTaskSkillDirectory(t *testing.T) {
	content, err := buildConfigContent(agent.ModelConfig{Model: "gpt-test"}, nil, " ")
	if err != nil {
		t.Fatalf("build config content: %v", err)
	}

	var cfg configContent
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		t.Fatalf("unmarshal config content: %v", err)
	}
	if cfg.Skills != nil {
		t.Fatalf("empty SkillDir should omit skills config: %#v", cfg.Skills)
	}
}

func TestBuildConfigContentDeclaresImageModalityOnlyForVisionModel(t *testing.T) {
	for _, vision := range []bool{true, false} {
		content, err := buildConfigContent(agent.ModelConfig{Model: "gpt-4o", Vision: vision}, nil, "")
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
	}, nil, "")
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
	content, err := buildConfigContent(agent.ModelConfig{Model: "default-model"}, nil, "")
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

// timeout 同时约束 opencode 的连接阶段：单 server 会按 transport 串行尝试两种方式，
// 每次都用该值，默认 30s，因此不可达 MCP 最坏阻塞 60s。enabled=false 则直接跳过连接。
func TestBuildMCPConfigEmitsTimeoutAndDisabled(t *testing.T) {
	config := buildMCPConfig([]agent.MCPServerConfig{
		{Name: "slow", URL: "https://example.com/mcp", TimeoutMS: 1500},
		{Name: "dead", URL: "https://dead.example.com/mcp", Disabled: true},
		{Name: "sse-slow", Transport: "sse", URL: "https://example.com/sse", TimeoutMS: 2000},
		{Name: "stdio-slow", Command: "npx", Args: []string{"-y", "@example/mcp"}, TimeoutMS: 2500, Disabled: true},
	})

	remote, ok := config["slow"].(map[string]any)
	if !ok || remote["timeout"] != 1500 {
		t.Fatalf("remote timeout = %#v, want 1500", config["slow"])
	}
	if _, exists := remote["enabled"]; exists {
		t.Fatalf("non-disabled MCP should not emit enabled: %#v", remote)
	}

	dead, ok := config["dead"].(map[string]any)
	if !ok || dead["enabled"] != false {
		t.Fatalf("disabled MCP enabled flag = %#v, want false", config["dead"])
	}
	if _, exists := dead["timeout"]; exists {
		t.Fatalf("MCP without timeout should not emit it: %#v", dead)
	}

	sse, ok := config["sse-slow"].(map[string]any)
	if !ok || sse["timeout"] != 2000 || sse["type"] != "local" {
		t.Fatalf("sse entry = %#v, want local type with timeout 2000", config["sse-slow"])
	}

	stdio, ok := config["stdio-slow"].(map[string]any)
	if !ok || stdio["timeout"] != 2500 || stdio["enabled"] != false {
		t.Fatalf("stdio entry = %#v, want timeout 2500 and enabled false", config["stdio-slow"])
	}
}

// 未设置策略时不得产生新键，保证与历史注入内容逐字节一致。
func TestBuildMCPConfigOmitsOverridesWhenUnset(t *testing.T) {
	config := buildMCPConfig([]agent.MCPServerConfig{
		{Name: "docs", URL: "https://example.com/mcp"},
	})
	entry, ok := config["docs"].(map[string]any)
	if !ok {
		t.Fatalf("MCP entry = %#v", config["docs"])
	}
	if _, exists := entry["timeout"]; exists {
		t.Fatalf("unexpected timeout key: %#v", entry)
	}
	if _, exists := entry["enabled"]; exists {
		t.Fatalf("unexpected enabled key: %#v", entry)
	}
}

func TestSanitizeConfigContentRemovesAPIKeyAndKeepsBaseURL(t *testing.T) {
	input := `{"provider":{"leros-provider":{"options":{"apiKey":"sk-secret","baseURL":"https://llm.example.com"},
"models":{"gpt-test":{"limit":{"context":82144,"output":42768},"options":{"top_p":0.5,"frequency_penalty":0.1}}}}}}`
	out := sanitizeConfigContent(input)
	if strings.Contains(out, "sk-secret") {
		t.Fatalf("sanitized config still contains apiKey value: %s", out)
	}
	if strings.Contains(out, "apiKey") {
		t.Fatalf("sanitized config still has apiKey key: %s", out)
	}
	if !strings.Contains(out, "https://llm.example.com") {
		t.Fatalf("sanitized config lost baseURL: %s", out)
	}
	for _, want := range []string{"82144", "42768", "0.5", "0.1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sanitized config missing %q: %s", want, out)
		}
	}
}

func TestSanitizeConfigContentKeepsValidJSON(t *testing.T) {
	out := sanitizeConfigContent(`{"provider":{"leros-provider":{"options":{"apiKey":"sk-x","baseURL":"u"}}}}`)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("sanitized config is not valid JSON: %v; out=%s", err, out)
	}
}

func TestSanitizeConfigContentFallsBackOnInvalidInput(t *testing.T) {
	input := "not-json{"
	if out := sanitizeConfigContent(input); out != input {
		t.Fatalf("invalid config should pass through unchanged, got %q", out)
	}
}
