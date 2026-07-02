package opencode

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/provider"
	"github.com/insmtx/Leros/backend/pkg/leros"
)

const (
	// providerID 是 OpenCode 配置中使用的 provider 标识符。
	providerID = "leros-provider"
	// providerNpm 使用 @ai-sdk/openai-compatible 通配大多数兼容 API。
	providerNpm = "@ai-sdk/openai-compatible"
	// openCodeDataDirName 是 OpenCode 在 worker 工作目录下的持久化目录。
	openCodeDataDirName = ".opencode"
	// openCodeDBName 是 OpenCode 会话数据库文件名。
	openCodeDBName = "opencode.db"
)

// buildConfigContent 根据 ModelConfig 和 MCPServerConfig 列表
// 生成 OPENCODE_CONFIG_CONTENT JSON 字符串。
func buildConfigContent(modelCfg agent.ModelConfig, mcps []provider.MCPServerConfig) (string, error) {
	modelID := modelCfg.Model
	if modelID == "" {
		modelID = "default"
	}
	modelName := modelID
	if modelCfg.Provider != "" {
		modelName = modelCfg.Provider + "/" + modelID
	}

	cfg := configContent{
		Provider: map[string]providerConfig{
			providerID: {
				ID:  providerID,
				Npm: providerNpm,
				Options: providerOptions{
					APIKey:  modelCfg.APIKey,
					BaseURL: modelCfg.BaseURL,
				},
				Models: map[string]modelConfig{
					modelID: {
						ID:          modelID,
						Name:        modelName,
						ToolCall:    true,
						Attachment:  true,
						Reasoning:   false,
						Temperature: true,
						Limit: modelLimit{
							Context: 200000,
							Output:  16384,
						},
					},
				},
			},
		},
		Model:      providerID + "/" + modelID,
		Permission: map[string]string{"websearch": "allow"},
	}

	// 构建 MCP 配置（遵循 opencode V1 config schema）
	if mcpCfg := buildMCPConfig(mcps); len(mcpCfg) > 0 {
		cfg.MCP = mcpCfg
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal config content: %w", err)
	}
	return string(data), nil
}

// buildMCPConfig 将 MCPServerConfig 列表转为 opencode V1 MCP schema 格式。
//
// opencode V1 MCP schema:
//
//	Remote (HTTP):  { "type": "remote", "url": "...", "headers": { "Authorization": "Bearer ..." } }
//	Local (stdio):  { "type": "local", "command": ["cmd", ...], "environment": { ... } }
func buildMCPConfig(mcps []provider.MCPServerConfig) map[string]any {
	if len(mcps) == 0 {
		return nil
	}
	mcpServers := make(map[string]any, len(mcps))
	for _, m := range mcps {
		name := m.Name
		if name == "" {
			name = "leros"
		}
		if m.URL != "" {
			// HTTP 传输 — remote type
			entry := map[string]any{
				"type": "remote",
				"url":  m.URL,
			}
			if m.BearerToken != "" {
				entry["headers"] = map[string]string{
					"Authorization": "Bearer " + m.BearerToken,
				}
			}
			mcpServers[name] = entry
		} else if m.Command != "" {
			// Stdio 传输 — local type
			cmdArgs := []string{m.Command}
			cmdArgs = append(cmdArgs, m.Args...)
			entry := map[string]any{
				"type":    "local",
				"command": cmdArgs,
			}
			if len(m.Env) > 0 {
				entry["environment"] = m.Env
			}
			mcpServers[name] = entry
		}
	}
	return mcpServers
}

// ensureOpenCodeDBPath 确保 OpenCode 数据目录存在并返回会话数据库路径。
func ensureOpenCodeDBPath() (string, error) {
	dir, err := leros.JoinWorkspace(openCodeDataDirName)
	if err != nil {
		return "", fmt.Errorf("resolve opencode data directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create opencode data directory %s: %w", dir, err)
	}
	return filepath.Join(dir, openCodeDBName), nil
}

// buildServerEnv 构建 opcode serve 子进程所需的环境变量。
// 返回格式为 "KEY=VALUE" 的字符串切片，附加到 baseEnv 之后。
func buildServerEnv(password, configContent, databasePath string, baseEnv []string) []string {
	env := make([]string, 0, 13)

	// 服务器认证
	env = append(env, "OPENCODE_SERVER_PASSWORD="+password)
	env = append(env, "OPENCODE_SERVER_USERNAME=opencode")

	// 注入完整配置（provider、model、API key、base URL）
	env = append(env, "OPENCODE_CONFIG_CONTENT="+configContent)
	// 将 session 等 SQLite 数据持久化到 worker 工作目录。
	env = append(env, "OPENCODE_DB="+databasePath)

	// 隔离环境变量：确保子进程不读取宿主机的配置文件或插件
	env = append(env, "OPENCODE_DISABLE_PROJECT_CONFIG=1")
	env = append(env, "OPENCODE_PURE=1")
	env = append(env, "OPENCODE_DISABLE_AUTOUPDATE=1")
	env = append(env, "OPENCODE_DISABLE_MODELS_FETCH=1")

	// 启用 plan mode 和 CLI client 模式
	env = append(env, "OPENCODE_EXPERIMENTAL_PLAN_MODE=true")
	env = append(env, "OPENCODE_CLIENT=cli")

	// 启用 EXA web search 功能
	env = append(env, "OPENCODE_ENABLE_EXA=1")

	return provider.BuildRunEnv(baseEnv, env, nil)
}

// generatePassword 生成 32 位随机十六进制密码。
func generatePassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random password: %w", err)
	}
	return hex.EncodeToString(b), nil
}
