package opencode

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	runtimeprocess "github.com/insmtx/Leros/backend/agent/runtime/internal/process"
)

const (
	// providerID 是 OpenCode 配置中使用的 provider 标识符。
	providerID = "leros-provider"
	// providerNpm 使用 @ai-sdk/openai-compatible 通配大多数兼容 API。
	providerNpm = "@ai-sdk/openai-compatible"
	// openCodeBuildAgentPrompt 占位替换 OpenCode build agent 的内置 provider baseline prompt。
	openCodeBuildAgentPrompt = "Follow the system instructions supplied with each task."
	// openCodeDataDirName 是 OpenCode 在 worker 工作目录下的持久化目录。
	openCodeDataDirName = ".opencode"
	// openCodeDBName 是 OpenCode 会话数据库文件名。
	openCodeDBName = "opencode.db"
)

// buildConfigContent 根据 ModelConfig、MCPServerConfig 列表和任务 Skill 目录
// 生成 OPENCODE_CONFIG_CONTENT JSON 字符串。
func buildConfigContent(
	modelCfg agent.ModelConfig,
	mcps []agent.MCPServerConfig,
	skillDir string,
	options ...configOption,
) (string, error) {
	modelID := modelCfg.Model
	if modelID == "" {
		modelID = "default"
	}
	modelName := modelID
	if modelCfg.Provider != "" {
		modelName = modelCfg.Provider + "/" + modelID
	}

	ctxLimit, outLimit := 200000, 16384
	if modelCfg.ContextLimit > 0 {
		ctxLimit = modelCfg.ContextLimit
	}
	if modelCfg.OutputLimit > 0 {
		outLimit = modelCfg.OutputLimit
	}

	modelEntry := modelConfig{
		ID:          modelID,
		Name:        modelName,
		ToolCall:    true,
		Attachment:  true,
		Reasoning:   false,
		Temperature: true,
		Limit: modelLimit{
			Context: ctxLimit,
			Output:  outLimit,
		},
	}

	// 采样参数透传：snake_case 键，兼容 vLLM 等 OpenAI 兼容服务（其只识别 top_p 等蛇形字段）。
	if modelCfg.TopP != nil || modelCfg.FrequencyPenalty != nil || modelCfg.PresencePenalty != nil {
		options := make(map[string]any)
		if modelCfg.TopP != nil {
			options["top_p"] = *modelCfg.TopP
		}
		if modelCfg.FrequencyPenalty != nil {
			options["frequency_penalty"] = *modelCfg.FrequencyPenalty
		}
		if modelCfg.PresencePenalty != nil {
			options["presence_penalty"] = *modelCfg.PresencePenalty
		}
		modelEntry.Options = options
	}

	// 视觉模型声明其真正支持的输入输出模态（图片为主路径，文本输出）。
	// 仅声明已支持的模态，未声明的（PDF/音视频/多模态输出）由 opencode 降级为文本提示，
	// 避免声明过宽导致 AI SDK 层对不支持的 file part 返回硬错误（如视频报
	// "'file part media type video/mp4' functionality not supported"）。
	// 非多模态模型不声明，opencode 据此对全部附件优雅降级。
	if modelCfg.Vision {
		modelEntry.Modalities = &modalityConfig{
			Input:  []string{"text", "image"},
			Output: []string{"text"},
		}
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
					modelID: modelEntry,
				},
			},
		},
		Model: providerID + "/" + modelID,
		Agent: map[string]agentConfig{"build": {Prompt: openCodeBuildAgentPrompt}},
		Agents: map[string]planAgentConfig{
			// v2 原生形状：legacy agent.build.prompt 仍保留兼容，两者一致。
			"build": {Mode: "primary", System: openCodeBuildAgentPrompt},
		},
		Experimental: &experimentalConfig{Todo: true},
		Permission: map[string]any{
			"*": "allow",
			"bash": map[string]string{
				"*":    "allow",
				"rm *": "ask",
			},
		},
	}

	// 构建 MCP 配置（遵循 opencode V1 config schema）
	if mcpCfg := buildMCPConfig(mcps); len(mcpCfg) > 0 {
		cfg.MCP = mcpCfg
	}
	if skillDir = strings.TrimSpace(skillDir); skillDir != "" {
		cfg.Skills = &skillsConfig{Paths: []string{skillDir}}
	}
	for _, option := range options {
		option(&cfg)
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal config content: %w", err)
	}
	return string(data), nil
}

// sanitizeConfigContent 返回适合写入日志的 config JSON 字符串。
// 同时覆盖两种 provider 形状：
//   - v1：provider.<id>.options.{apiKey,headers}
//   - v2：providers.<id>.{settings.{apiKey,headers}, headers}
//
// 密钥删除、请求头值置为 ***；baseURL、采样参数等其余字段原样保留。
// config 无法解析时原样返回，不阻塞日志输出（调用方不应因脱敏失败而中断流程）。
func sanitizeConfigContent(configContent string) string {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(configContent), &cfg); err != nil {
		return configContent
	}
	sanitizeProviderMap(cfg["provider"])
	sanitizeProviderMap(cfg["providers"])
	redactMCPCredentials(cfg)
	cleaned, err := json.Marshal(cfg)
	if err != nil {
		return configContent
	}
	return string(cleaned)
}

// sanitizeProviderMap 处理 v1 与 v2 的 provider 容器（值形状不同，按字段探测）。
func sanitizeProviderMap(raw any) {
	providers, ok := raw.(map[string]any)
	if !ok {
		return
	}
	for _, entry := range providers {
		provider, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		for _, field := range []string{"options", "settings"} {
			settings, ok := provider[field].(map[string]any)
			if !ok {
				continue
			}
			for _, key := range []string{"apiKey", "api_key", "api-key"} {
				delete(settings, key)
			}
			redactHeaderValues(settings["headers"])
		}
		redactHeaderValues(provider["headers"])
	}
}

// redactHeaderValues 保留请求头键名、把值替换为 ***。
func redactHeaderValues(raw any) {
	headers, ok := raw.(map[string]any)
	if !ok {
		return
	}
	for key := range headers {
		headers[key] = "***"
	}
}

// redactMCPCredentials 抹掉 MCP 服务端配置中的凭据：请求头与环境变量的值，以及
// URL 查询串中形如 access_token/api_key 的参数。键名保留，便于排查「哪个连接器
// 带了认证头」而不泄露凭据本身。
func redactMCPCredentials(cfg map[string]any) {
	servers, ok := cfg["mcp"].(map[string]any)
	if !ok {
		return
	}
	for _, raw := range servers {
		server, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for _, field := range []string{"headers", "environment"} {
			redactHeaderValues(server[field])
		}
		if rawURL, ok := server["url"].(string); ok {
			server["url"] = redactURLCredentials(rawURL)
		}
	}
}

// redactURLCredentials 将 URL 查询串中的敏感参数值替换为 ***。
func redactURLCredentials(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.RawQuery == "" {
		return rawURL
	}
	query := parsed.Query()
	changed := false
	for key := range query {
		if isSensitiveQueryKey(key) {
			query.Set(key, "***")
			changed = true
		}
	}
	if !changed {
		return rawURL
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// isSensitiveQueryKey 判断查询参数名是否可能承载凭据。
func isSensitiveQueryKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	switch normalized {
	case "access_token", "token", "api_key", "apikey", "key", "secret", "password":
		return true
	}
	return strings.Contains(normalized, "token") || strings.Contains(normalized, "secret")
}

// buildMCPConfig 将 MCPServerConfig 列表转为 opencode V1 MCP schema 格式。
//
// opencode V1 MCP schema:
//
//	Remote (HTTP):  { "type": "remote", "url": "...", "headers": { "Authorization": "Bearer ..." } }
//	Local (stdio):  { "type": "local", "command": ["cmd", ...], "environment": { ... } }
func buildMCPConfig(mcps []agent.MCPServerConfig) map[string]any {
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
			if runtimeprocess.NeedsSSEBridge(m) {
				command := []string{"npx", "-y", "mcp-remote", m.URL, "--transport", "sse-only"}
				// 中文注释：SSE 桥接必须显式传递认证头，否则远端 MCP 以未认证身份握手失败；
				// 凭据经 ${ENV} 间接引用，避免 token 落进进程命令行与配置文件（后者会写日志）。
				headerArgs, _ := runtimeprocess.BuildMCPRemoteHeaderArgs(m)
				command = append(command, headerArgs...)
				mcpServers[name] = map[string]any{"type": "local", "command": command}
				continue
			}
			// HTTP 传输 — remote type
			entry := map[string]any{
				"type": "remote",
				"url":  m.URL,
			}
			headers := cloneHeaders(m.Headers)
			if m.BearerToken != "" && !hasHeader(headers, "authorization") {
				if headers == nil {
					headers = make(map[string]string)
				}
				headers["Authorization"] = "Bearer " + m.BearerToken
			}
			if len(headers) > 0 {
				entry["headers"] = headers
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

func cloneHeaders(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func hasHeader(headers map[string]string, name string) bool {
	for key := range headers {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return true
		}
	}
	return false
}

// ensureOpenCodeDBPath ensures the OpenCode data directory exists and returns the session database path.
// Returns an empty string if dataDir is empty — the caller should skip the OPENCODE_DB env var
// so that OpenCode falls back to its own default location.
func ensureOpenCodeDBPath(dataDir string) (string, error) {
	dir := strings.TrimSpace(dataDir)
	if dir == "" {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create opencode data directory %s: %w", dir, err)
	}
	return filepath.Join(dir, openCodeDBName), nil
}
