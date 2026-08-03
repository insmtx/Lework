package agentrun

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/internal/service"
	"github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/ygpkg/yg-go/logs"
)

var connectorEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// MCPServerConfigFromPluginSnapshot decodes a validated MCP snapshot.
func MCPServerConfigFromPluginSnapshot(snapshot domain.PluginSnapshot) (agent.MCPServerConfig, error) {
	if !strings.EqualFold(snapshot.Kind, "mcp") {
		return agent.MCPServerConfig{}, fmt.Errorf("snapshot kind is not mcp")
	}
	code := strings.ToLower(strings.TrimSpace(snapshot.Code))
	if code == "" {
		return agent.MCPServerConfig{}, fmt.Errorf("mcp snapshot code is required")
	}
	if code == "leros" {
		return agent.MCPServerConfig{}, fmt.Errorf("mcp snapshot uses a reserved code")
	}
	definition, err := service.MCPFromDefinition(snapshot.Definition)
	if err != nil {
		return agent.MCPServerConfig{}, err
	}
	if definition == nil {
		return agent.MCPServerConfig{}, fmt.Errorf("connector snapshot has no mcp capability")
	}
	switch definition.Transport {
	case "http", "sse":
		parsed, parseErr := url.Parse(strings.TrimSpace(definition.URL))
		if parseErr != nil || parsed == nil || parsed.Host == "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
			return agent.MCPServerConfig{}, fmt.Errorf("mcp snapshot has an invalid HTTP URL")
		}
	case "stdio":
	default:
		return agent.MCPServerConfig{}, fmt.Errorf("mcp snapshot transport is unsupported")
	}
	return agent.MCPServerConfig{
		Name:        code,
		Transport:   definition.Transport,
		URL:         definition.URL,
		Command:     definition.Command,
		Args:        append([]string(nil), definition.Args...),
		Env:         cloneMCPStringMap(definition.Env),
		Headers:     cloneMCPStringMap(definition.Headers),
		BearerToken: definition.BearerToken,
	}, nil
}

func prepareConnectorRuntimeEnv(
	ctx context.Context,
	snapshots []domain.PluginSnapshot,
) []string {
	values := make(map[string]string)
	for _, snapshot := range sortedPluginSnapshots(snapshots) {
		if !strings.EqualFold(snapshot.Kind, "mcp") {
			continue
		}
		definition, err := service.ConnectorFromDefinition(snapshot.Definition)
		if err != nil || definition == nil || definition.Skill == nil {
			if err != nil {
				logs.WarnContextf(ctx, "skip invalid connector environment: plugin_id=%s error=%v", snapshot.PluginID, err)
			}
			continue
		}
		valid := true
		pending := make(map[string]string)
		for envName, valueKey := range definition.Auth.Bindings.SkillEnv {
			value, ok := definition.Auth.Values[valueKey]
			if !ok || value == "" || !connectorRuntimeEnvAllowed(envName) || strings.ContainsRune(value, '\x00') {
				valid = false
				break
			}
			if existing, exists := values[envName]; exists && existing != value {
				valid = false
				break
			}
			pending[envName] = value
		}
		if !valid {
			logs.WarnContextf(ctx, "skip invalid connector environment: plugin_id=%s code=%s", snapshot.PluginID, snapshot.Code)
			continue
		}
		for name, value := range pending {
			values[name] = value
		}
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name+"="+values[name])
	}
	return result
}

func connectorRuntimeEnvAllowed(name string) bool {
	if !connectorEnvNamePattern.MatchString(name) {
		return false
	}
	switch name {
	case "HOME", "PATH", "SHELL", "USER", "TMPDIR", "OPENCODE_CONFIG_DIR",
		"OPENCODE_DISABLE_PROJECT_CONFIG", "OPENCODE_DISABLE_CLAUDE_CODE",
		"CLAUDE_CONFIG_DIR", "CODEX_HOME", "OPENAI_API_KEY", "OPENAI_API_BASE",
		"OPENAI_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL":
		return false
	}
	return !strings.HasPrefix(name, "LEROS_")
}

func cloneMCPStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
