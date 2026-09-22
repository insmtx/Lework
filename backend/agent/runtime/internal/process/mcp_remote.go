package process

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
)

// mcpRemoteHeaderEnvPrefix 是 SSE 桥接请求头环境变量的统一前缀。
const mcpRemoteHeaderEnvPrefix = "LEROS_MCP_HEADER_"

// NeedsSSEBridge 判断该 MCP 服务端是否需要经 npx mcp-remote 桥接为 SSE。
//
// opencode 与 codex 的远端 MCP 客户端只讲 HTTP；SSE 传输必须由 mcp-remote 转换，
// 转换后请求头不会自动携带，调用方需要显式传 --header。
func NeedsSSEBridge(cfg agent.MCPServerConfig) bool {
	return strings.TrimSpace(cfg.URL) != "" &&
		strings.EqualFold(strings.TrimSpace(cfg.Transport), "sse")
}

// BuildMCPRemoteHeaderArgs 生成 mcp-remote 的 --header 参数，并返回配套的环境变量。
//
// 为什么不直接把请求头值拼进参数：mcp-remote 支持在 --header 中展开 ${VAR}，凭据走
// 环境变量可以避免 access token 出现在进程命令行（ps 可见）以及 opencode/codex 的
// MCP 配置里（opencode 的配置内容会被写入日志）。
//
// 返回的 args 形如 ["--header", "Authorization:${LEROS_MCP_HEADER_AUTHORIZATION_1A2B3C4D}"]，
// 冒号后不留空格：mcp-remote 在请求头值包含空格时依赖 "${VAR}" 整体替换。
// env 为需要注入 CLI 子进程的键值对，调用方必须保证它与 args 一同生效。
func BuildMCPRemoteHeaderArgs(cfg agent.MCPServerConfig) ([]string, map[string]string) {
	headers := effectiveMCPHeaders(cfg)
	if len(headers) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)

	args := make([]string, 0, len(names)*2)
	env := make(map[string]string, len(names))
	for _, name := range names {
		envName := mcpRemoteHeaderEnvName(cfg.Name, name)
		args = append(args, "--header", fmt.Sprintf("%s:${%s}", name, envName))
		env[envName] = headers[name]
	}
	return args, env
}

// BuildMCPRemoteHeaderEnv 汇总 SSE 桥接（opencode）所需的请求头环境变量。
//
// opencode 的 HTTP 传输由自身携带请求头，只有 SSE 才经 mcp-remote 转换，
// 因此这里只处理 NeedsSSEBridge 的服务端。输出按变量名排序，保证子进程环境稳定。
func BuildMCPRemoteHeaderEnv(mcps []agent.MCPServerConfig) []string {
	return buildMCPRemoteHeaderEnv(mcps, NeedsSSEBridge)
}

// BuildMCPRemoteHeaderEnvForURL 汇总所有 URL 型 MCP 服务端的请求头环境变量。
//
// codex 的远端 MCP 无论 HTTP 还是 SSE 都统一经 mcp-remote 转换，因此凡是带 URL
// 的服务端其 --header 都会引用环境变量，注入范围必须与之对齐。
func BuildMCPRemoteHeaderEnvForURL(mcps []agent.MCPServerConfig) []string {
	return buildMCPRemoteHeaderEnv(mcps, func(cfg agent.MCPServerConfig) bool {
		return strings.TrimSpace(cfg.URL) != ""
	})
}

// buildMCPRemoteHeaderEnv 汇总 keep 选中的服务端所需的请求头环境变量。
func buildMCPRemoteHeaderEnv(mcps []agent.MCPServerConfig, keep func(agent.MCPServerConfig) bool) []string {
	values := make(map[string]string)
	for _, cfg := range mcps {
		if keep != nil && !keep(cfg) {
			continue
		}
		_, env := BuildMCPRemoteHeaderArgs(cfg)
		for key, value := range env {
			values[key] = value
		}
	}
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

// DescribeMCPServers 返回适合写入日志的 MCP 服务端摘要：名称、传输方式、主机名与
// 携带了哪些认证头名，绝不包含凭据值。
func DescribeMCPServers(mcps []agent.MCPServerConfig) string {
	parts := make([]string, 0, len(mcps))
	for _, cfg := range mcps {
		transport := strings.TrimSpace(cfg.Transport)
		if transport == "" {
			transport = "stdio"
		}
		item := fmt.Sprintf("%s[%s", cfg.Name, transport)
		if parsed, err := url.Parse(strings.TrimSpace(cfg.URL)); err == nil && parsed.Host != "" {
			item += ",host=" + parsed.Host
		}
		item += ",auth=" + strings.Join(mcpHeaderNames(cfg), ",")
		parts = append(parts, item+"]")
	}
	return strings.Join(parts, " ")
}

// effectiveMCPHeaders 合并显式请求头与 BearerToken，显式 Authorization 优先。
func effectiveMCPHeaders(cfg agent.MCPServerConfig) map[string]string {
	headers := make(map[string]string, len(cfg.Headers)+1)
	for key, value := range cfg.Headers {
		headers[key] = value
	}
	if token := strings.TrimSpace(cfg.BearerToken); token != "" && !hasHeaderName(headers, "authorization") {
		headers["Authorization"] = "Bearer " + token
	}
	return headers
}

// mcpHeaderNames 返回排序后的请求头名，用于日志摘要（只有名字，没有值）。
func mcpHeaderNames(cfg agent.MCPServerConfig) []string {
	headers := effectiveMCPHeaders(cfg)
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return []string{"none"}
	}
	return names
}

// hasHeaderName 以大小写不敏感的方式判断请求头是否已存在。
func hasHeaderName(headers map[string]string, target string) bool {
	for name := range headers {
		if strings.EqualFold(name, target) {
			return true
		}
	}
	return false
}

// mcpRemoteHeaderEnvName 由服务名与请求头名生成稳定且唯一的环境变量名。
//
// 服务名（连接器 code）较长且可能包含 '-'，这里截断到可读长度并附加服务名哈希，
// 既避免环境变量名过长，也避免不同服务端的同名请求头互相覆盖。
func mcpRemoteHeaderEnvName(serverName, headerName string) string {
	server := sanitizeEnvSegment(serverName)
	if len(server) > 24 {
		server = server[:24]
	}
	sum := sha256.Sum256([]byte(serverName))
	return fmt.Sprintf("%s%s_%s_%s",
		mcpRemoteHeaderEnvPrefix,
		sanitizeEnvSegment(headerName),
		server,
		strings.ToUpper(hex.EncodeToString(sum[:4])),
	)
}

// sanitizeEnvSegment 把任意标识符转换为可用于环境变量名的片段。
func sanitizeEnvSegment(value string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(value)) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
