package opencode

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	// openCodeConfigHomeName 是 opencode 数据目录下用作 XDG_CONFIG_HOME 的子目录。
	//
	// 为什么需要它：opencode 的全局配置目录固定为 $XDG_CONFIG_HOME/opencode，
	// 只设置 OPENCODE_CONFIG_DIR 不会改变该路径，因此宿主机的
	// ~/.config/opencode/opencode.json 仍会被读取并深度合入最终配置
	// （其中的 mcp/plugin 条目会泄漏进每一次运行）。把 XDG_CONFIG_HOME 指向
	// 该隔离目录后，全局配置不再存在，注入的 OPENCODE_CONFIG_CONTENT 即最终配置。
	//
	// 注意：该目录必须持久存在。它是配置目录而非缓存目录，不能与
	// XDG_CACHE_HOME 混用——清空缓存目录会使 models.json 缺失。
	openCodeConfigHomeName = "config-home"
)

// buildConfigContent 根据 ModelConfig、MCPServerConfig 列表和任务 Skill 目录
// 生成 OPENCODE_CONFIG_CONTENT JSON 字符串。
func buildConfigContent(modelCfg agent.ModelConfig, mcps []agent.MCPServerConfig, skillDir string) (string, error) {
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

	data, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal config content: %w", err)
	}
	return string(data), nil
}

// sanitizeConfigContent 返回适合写入日志的 config JSON 字符串。
// 剔除每个 provider options 下的 apiKey 字段以避免密钥落入日志；
// baseURL 及采样参数等其余字段原样保留。config 无法解析时原样返回，
// 不阻塞日志输出（调用方不应因脱敏失败而中断启动流程）。
func sanitizeConfigContent(configContent string) string {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(configContent), &cfg); err != nil {
		return configContent
	}
	providers, ok := cfg["provider"].(map[string]any)
	if ok {
		for _, v := range providers {
			opts, ok := v.(map[string]any)["options"].(map[string]any)
			if !ok {
				continue
			}
			delete(opts, "apiKey")
		}
	}
	cleaned, err := json.Marshal(cfg)
	if err != nil {
		return configContent
	}
	return string(cleaned)
}

// buildMCPConfig 将 MCPServerConfig 列表转为 opencode V1 MCP schema 格式。
//
// opencode V1 MCP schema:
//
//	Remote (HTTP):  { "type": "remote", "url": "...", "headers": { "Authorization": "Bearer ..." } }
//	Local (stdio):  { "type": "local", "command": ["cmd", ...], "environment": { ... } }
//
// 两种形态都支持 timeout(毫秒) 与 enabled。timeout 同时作用于连接阶段：
// opencode 对单个 server 会按连接方式串行尝试（streamable-http → sse），
// 每次都用该 timeout，默认值 30s，因此一个不可达的 MCP 最坏阻塞 60s。
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
			if strings.EqualFold(m.Transport, "sse") {
				command := []string{"npx", "-y", "mcp-remote", m.URL, "--transport", "sse-only"}
				mcpServers[name] = applyMCPOverrides(map[string]any{"type": "local", "command": command}, m)
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
			mcpServers[name] = applyMCPOverrides(entry, m)
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
			mcpServers[name] = applyMCPOverrides(entry, m)
		}
	}
	return mcpServers
}

// applyMCPOverrides 按 MCP 策略写入 timeout 与 enabled。
// 仅在显式设置时写入，未设置时不产生额外键，保持与历史注入内容一致。
func applyMCPOverrides(entry map[string]any, m agent.MCPServerConfig) map[string]any {
	if m.TimeoutMS > 0 {
		entry["timeout"] = m.TimeoutMS
	}
	if m.Disabled {
		entry["enabled"] = false
	}
	return entry
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

// ensureOpenCodeConfigHome 创建并返回隔离用的 XDG_CONFIG_HOME 目录。
// 返回空字符串表示未启用隔离（dataDir 为空），调用方应跳过该环境变量。
func ensureOpenCodeConfigHome(dataDir string) (string, error) {
	dir := strings.TrimSpace(dataDir)
	if dir == "" {
		return "", nil
	}
	home := filepath.Join(dir, openCodeConfigHomeName)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", fmt.Errorf("create opencode config home %s: %w", home, err)
	}
	return home, nil
}

// buildServerEnv 构建 opcode serve 子进程所需的环境变量。
// 返回格式为 "KEY=VALUE" 的字符串切片，附加到 baseEnv 之后。
//
// configHome 为隔离用的 XDG_CONFIG_HOME；为空表示不隔离（保持历史行为）。
func buildServerEnv(password, configContent, databasePath, configHome string, baseEnv []string) []string {
	env := make([]string, 0, 18)

	// 服务器认证
	env = append(env, "OPENCODE_SERVER_PASSWORD="+password)
	env = append(env, "OPENCODE_SERVER_USERNAME=opencode")

	// 注入完整配置（provider、model、API key、base URL）
	env = append(env, "OPENCODE_CONFIG_CONTENT="+configContent)
	// 将 session 等 SQLite 数据持久化到 worker 工作目录。
	if databasePath != "" {
		env = append(env, "OPENCODE_DB="+databasePath)
	}

	// 隔离环境变量：确保子进程不读取宿主机的配置文件或插件
	//
	// XDG_CONFIG_HOME 是唯一能切断宿主机 ~/.config/opencode 全局配置的手段：
	// opencode 的全局配置路径固定为 $XDG_CONFIG_HOME/opencode，而
	// OPENCODE_CONFIG_DIR 只影响附加的配置目录扫描，不改变该路径。
	// 宿主机全局配置中的 mcp/plugin 条目会被深度合入并泄漏进每次运行。
	if configHome != "" {
		env = append(env, "XDG_CONFIG_HOME="+configHome)
	}
	env = append(env, "OPENCODE_DISABLE_PROJECT_CONFIG=1")
	env = append(env, "OPENCODE_PURE=1")
	// 内置插件是 Codex/Copilot/Modal/GitLab/Poe/Cloudflare/Azure/DigitalOcean/
	// Snowflake/Xai 等第三方 provider 的鉴权插件；Leros 注入自建的 OpenAI 兼容
	// provider 并自带 API Key，不经过这些鉴权流程，禁用它们不影响任何既有能力。
	env = append(env, "OPENCODE_DISABLE_DEFAULT_PLUGINS=1")
	env = append(env, "OPENCODE_DISABLE_AUTOUPDATE=1")
	env = append(env, "OPENCODE_DISABLE_MODELS_FETCH=1")
	// 关闭宿主机级技能发现：~/.claude 与 ~/.agents 的目录扫描同样发生在
	// 首个 LLM 请求之前，且这些技能并不属于当前会话。
	env = append(env, "OPENCODE_DISABLE_EXTERNAL_SKILLS=1")
	env = append(env, "OPENCODE_DISABLE_CLAUDE_CODE_SKILLS=1")

	// 启用 plan mode 和 CLI client 模式
	env = append(env, "OPENCODE_EXPERIMENTAL_PLAN_MODE=true")
	env = append(env, "OPENCODE_CLIENT=cli")

	// 启用 EXA web search 功能
	env = append(env, "OPENCODE_ENABLE_EXA=1")

	return runtimeprocess.BuildRunEnv(baseEnv, env, nil)
}

// generatePassword 生成 32 位随机十六进制密码。
func generatePassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random password: %w", err)
	}
	return hex.EncodeToString(b), nil
}
