package claude

import (
	"github.com/bytedance/sonic"
	"os"
	"path/filepath"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/externalcli"
	"github.com/insmtx/Leros/backend/agent/runtime/provider"
)

// ——— 参数构建 ———

func buildArgs(req externalcli.InvocationRequest) []string {
	args := []string{
		"--verbose",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--include-partial-messages",
		"--permission-prompt-tool", "stdio",
		"--disallowedTools", "EnterPlanMode,ExitPlanMode,WebSearch",
	}

	// 权限模式决定是否绕过审批
	switch req.PermissionMode {
	case provider.PermissionModeBypass, "":
		args = append(args, "--dangerously-skip-permissions", "--permission-mode", "bypassPermissions")
	case provider.PermissionModeOnRequest, provider.PermissionModeAuto:
		// on-request 和 auto 均使用 default 模式；auto 由 ApprovalHandler 处理
		args = append(args, "--permission-mode", "default")
	}

	if req.Model.Model != "" {
		args = append(args, "--model", req.Model.Model)
	}
	if systemPrompt := strings.TrimSpace(req.SystemPrompt); systemPrompt != "" {
		args = append(args, "--append-system-prompt", systemPrompt)
	}
	if req.SessionID != "" {
		if req.Resume {
			args = append(args, "--resume", req.SessionID)
		} else {
			args = append(args, "--session-id", req.SessionID)
		}
	}
	return append(args, "--print")
}

// ——— Settings 文件 ———

// lerosSettings 写入的 leros 专用 settings 文件结构。
type lerosSettings struct {
	Model string            `json:"model,omitempty"`
	Env   map[string]string `json:"env"`
}

// buildLerosSettings 根据本次请求构建 leros settings 配置。
func buildLerosSettings(req externalcli.InvocationRequest) *lerosSettings {
	model := strings.TrimSpace(req.Model.Model)
	baseURL := withoutV1Suffix(req.Model.BaseURL)
	apiKey := strings.TrimSpace(req.Model.APIKey)
	return &lerosSettings{
		Model: model,
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":                       baseURL,
			"ANTHROPIC_AUTH_TOKEN":                     apiKey,
			"ANTHROPIC_API_KEY":                        apiKey,
			"ANTHROPIC_DEFAULT_OPUS_MODEL":             model,
			"ANTHROPIC_DEFAULT_SONNET_MODEL":           model,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":            model,
			"API_TIMEOUT_MS":                           "3000000",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"CLAUDE_CODE_ATTRIBUTION_HEADER":           "0",
		},
	}
}

// lerosSettingsPath 返回 ~/.claude/settings.leros.{sessionId}.json 路径，兼容 Windows。
func lerosSettingsPath(sessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	name := "settings.leros.json"
	if sessionID != "" {
		name = "settings.leros." + sessionID + ".json"
	}
	return filepath.Join(home, ".claude", name), nil
}

// writeLerosSettings 将配置写入 leros settings 文件。
func writeLerosSettings(path string, settings *lerosSettings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := sonic.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ——— 模型环境 ———

func claudeModelEnv(_ agent.ModelConfig) map[string]string {
	return nil
}

// ——— MCP 配置写入 ———

// writeMCPConfig 将 MCPServerConfig 列表转为 Claude mcpServers JSON，写入 dir/mcp_config.json。
// 返回文件路径，调用方负责在不再需要时删除。
func writeMCPConfig(dir string, servers []provider.MCPServerConfig) (string, error) {
	mcpServers := make(map[string]any, len(servers))
	for _, s := range servers {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			name = "leros"
		}
		entry := map[string]any{}
		if s.Command != "" {
			entry["command"] = s.Command
			if len(s.Args) > 0 {
				entry["args"] = s.Args
			}
			if len(s.Env) > 0 {
				entry["env"] = s.Env
			}
		} else if s.URL != "" {
			entry["type"] = "http"
			entry["url"] = s.URL
			if s.BearerToken != "" {
				entry["headers"] = map[string]string{
					"Authorization": "Bearer " + s.BearerToken,
				}
			}
		}
		mcpServers[name] = entry
	}

	data, err := sonic.MarshalIndent(map[string]any{"mcpServers": mcpServers}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "mcp_config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// ——— 通用工具 ———

func withoutV1Suffix(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return strings.TrimSuffix(baseURL, "/v1")
}
