package agent

import "strings"

// MCPInjectPolicy 收敛注入外部 Runtime 的 MCP 连接行为。
//
// 背景：运行时 CLI（如 opencode）会在首个 LLM 请求之前同步连接全部 MCP，
// 默认连接超时为 30s，且单个 server 会串行尝试两种 transport，因此一个宿主机
// 残留或长期未授权的 MCP 足以把首 token 延迟抬到分钟级。TimeoutMS 给出统一
// 上限，Disabled 让运维显式摘掉已知不可达的 server。
type MCPInjectPolicy struct {
	// TimeoutMS 为未单独指定超时的 MCP 设置连接超时（毫秒）。<=0 表示不下发。
	TimeoutMS int
	// Disabled 中的名称（大小写不敏感）会被标记为禁用。
	Disabled []string
}

// Apply 返回套用策略后的新切片，不修改入参切片及其元素。
// 单个 MCP 已显式声明的 TimeoutMS 优先于策略默认值。
func (p MCPInjectPolicy) Apply(configs []MCPServerConfig) []MCPServerConfig {
	if len(configs) == 0 {
		return nil
	}
	disabled := make(map[string]struct{}, len(p.Disabled))
	for _, name := range p.Disabled {
		if key := strings.ToLower(strings.TrimSpace(name)); key != "" {
			disabled[key] = struct{}{}
		}
	}
	result := make([]MCPServerConfig, 0, len(configs))
	for _, config := range configs {
		if config.TimeoutMS <= 0 && p.TimeoutMS > 0 {
			config.TimeoutMS = p.TimeoutMS
		}
		if !config.Disabled {
			if _, hit := disabled[strings.ToLower(strings.TrimSpace(config.Name))]; hit {
				config.Disabled = true
			}
		}
		result = append(result, config)
	}
	return result
}
