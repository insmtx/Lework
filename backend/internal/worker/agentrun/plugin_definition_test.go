package agentrun

import (
	"encoding/json"
	"testing"

	"github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
)

func TestMCPServerConfigFromPluginSnapshot(t *testing.T) {
	config, err := MCPServerConfigFromPluginSnapshot(domain.PluginSnapshot{
		PluginID: "plg_mcp",
		Code:     "docs",
		Kind:     "mcp",
		Definition: json.RawMessage(
			`{"schema":"mcp/v1","transport":"http","name":"ignored","url":"https://example.com/mcp","bearer_token":"runtime-secret","headers":{"X-Tenant":"docs"}}`,
		),
	})
	if err != nil || config.Name != "docs" || config.URL != "https://example.com/mcp" ||
		config.BearerToken != "runtime-secret" || config.Headers["X-Tenant"] != "docs" {
		t.Fatalf("config=%#v err=%v", config, err)
	}
}

func TestMCPServerConfigFromStdioPluginSnapshot(t *testing.T) {
	config, err := MCPServerConfigFromPluginSnapshot(domain.PluginSnapshot{
		PluginID: "plg_stdio",
		Code:     "sqlite",
		Kind:     "mcp",
		Definition: json.RawMessage(
			`{"schema":"mcp/v1","transport":"stdio","name":"sqlite","command":"npx",` +
				`"args":["-y","@example/mcp"],"env":{"LOG_LEVEL":"debug"}}`,
		),
	})
	if err != nil || config.Name != "sqlite" || config.Command != "npx" ||
		len(config.Args) != 2 || config.Env["LOG_LEVEL"] != "debug" {
		t.Fatalf("config=%#v err=%v", config, err)
	}
}

func TestMCPServerConfigFromSSEPluginSnapshot(t *testing.T) {
	config, err := MCPServerConfigFromPluginSnapshot(domain.PluginSnapshot{
		PluginID: "plg_baidu",
		Code:     "baidu-netdisk",
		Kind:     "mcp",
		Definition: json.RawMessage(
			`{"schema":"mcp/v1","transport":"sse","name":"baidu-netdisk",` +
				`"url":"https://example.com/sse?access_token=secret"}`,
		),
	})
	if err != nil || config.Transport != "sse" || config.Name != "baidu-netdisk" {
		t.Fatalf("config=%#v err=%v", config, err)
	}
}

func TestPrepareMCPServersSortsAndSkipsInvalidSnapshots(t *testing.T) {
	configs := prepareMCPServers(t.Context(), []domain.PluginSnapshot{
		{
			PluginID: "plg_z",
			Code:     "zeta",
			Kind:     "mcp",
			Definition: json.RawMessage(
				`{"schema":"mcp/v1","transport":"http","name":"zeta","url":"https://z.example.com/mcp"}`,
			),
		},
		{
			PluginID: "plg_invalid",
			Code:     "invalid",
			Kind:     "mcp",
			Definition: json.RawMessage(
				`{"schema":"mcp/v1","transport":"http","name":"invalid","url":"file:///tmp/mcp"}`,
			),
		},
		{
			PluginID: "plg_invalid_stdio",
			Code:     "invalid-stdio",
			Kind:     "mcp",
			Definition: json.RawMessage(
				`{"schema":"mcp/v1","transport":"stdio","command":"mcp-server",` +
					`"env":{"BAD-NAME":"value"}}`,
			),
		},
		{
			PluginID: "plg_a",
			Code:     "alpha",
			Kind:     "mcp",
			Definition: json.RawMessage(
				`{"schema":"mcp/v1","transport":"http","name":"alpha","url":"https://a.example.com/mcp"}`,
			),
		},
	})
	if len(configs) != 2 || configs[0].Name != "alpha" || configs[1].Name != "zeta" {
		t.Fatalf("configs = %#v", configs)
	}
}

func TestPrepareConnectorRuntimeEnvUsesSkillBindings(t *testing.T) {
	env := prepareConnectorRuntimeEnv(t.Context(), []domain.PluginSnapshot{
		{
			PluginID: "plugin_mail",
			Code:     "netease-mail-user",
			Kind:     "mcp",
			Revision: 1,
			Definition: json.RawMessage(
				`{"schema":"connector/v1","channel":"netease-mail","mode":"skill_only",` +
					`"auth":{"type":"form","values":{"email":"user@example.com","authorization_code":"mail-code"},` +
					`"bindings":{"skill_env":{"NETEASE_EMAIL_PASS":"authorization_code","NETEASE_EMAIL_USER":"email"}}},` +
					`"skill":{"code":"connector-netease-mail","revision":1,"artifact":` +
					`{"file_upload_id":"file_mail","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`,
			),
		},
	})
	if len(env) != 2 ||
		env[0] != "NETEASE_EMAIL_PASS=mail-code" ||
		env[1] != "NETEASE_EMAIL_USER=user@example.com" {
		t.Fatalf("connector env = %#v", env)
	}
}

func TestPrepareConnectorRuntimeEnvRejectsReservedBindings(t *testing.T) {
	env := prepareConnectorRuntimeEnv(t.Context(), []domain.PluginSnapshot{
		{
			PluginID: "plugin_invalid",
			Code:     "invalid",
			Kind:     "mcp",
			Revision: 1,
			Definition: json.RawMessage(
				`{"schema":"connector/v1","channel":"invalid","mode":"skill_only",` +
					`"auth":{"type":"form","values":{"skill_dir":"/tmp/escape"},` +
					`"bindings":{"skill_env":{"LEROS_RUN_SKILLS_DIR":"skill_dir"}}},` +
					`"skill":{"code":"connector-invalid","revision":1,"artifact":` +
					`{"file_upload_id":"file_invalid","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`,
			),
		},
	})
	if len(env) != 0 {
		t.Fatalf("reserved connector env = %#v", env)
	}
}

func TestConnectorRuntimeEnvAllowedRejectsRuntimeAndModelVariables(t *testing.T) {
	for _, name := range []string{
		"LEROS_RUN_SKILLS_DIR",
		"OPENCODE_CONFIG_DIR",
		"CLAUDE_CONFIG_DIR",
		"CODEX_HOME",
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
	} {
		if connectorRuntimeEnvAllowed(name) {
			t.Fatalf("connectorRuntimeEnvAllowed(%q) = true", name)
		}
	}
	if !connectorRuntimeEnvAllowed("NETEASE_EMAIL_USER") {
		t.Fatal("NETEASE_EMAIL_USER must remain available to connector Skills")
	}
}
