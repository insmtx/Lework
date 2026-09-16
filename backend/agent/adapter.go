package agent

// MCPServerConfig describes one MCP endpoint exposed to an external Runtime.
type MCPServerConfig struct {
	Name        string
	Transport   string
	URL         string
	Command     string
	Args        []string
	Env         map[string]string
	Headers     map[string]string
	BearerToken string
	// TimeoutMS 限制运行时 CLI 连接该 MCP 的超时（毫秒）。0 表示沿用运行时默认值。
	// 该值决定不可达 MCP 的等待上限：连接失败会在首个 LLM 请求之前同步阻塞。
	TimeoutMS int
	// Disabled 为 true 时以 enabled=false 注入，运行时 CLI 直接跳过连接。
	// 用于已知不可达或长期未授权的 MCP，避免每条消息重复付出握手代价。
	Disabled bool
}

// RuntimeAdapterOptions contains host-provided facilities shared by CLI adapters.
type RuntimeAdapterOptions struct {
	InteractionHandler InteractionHandler
	MCPServers         []MCPServerConfig
	// MCPPolicy 收敛最终注入运行时的 MCP 连接行为（统一超时与禁用名单）。
	MCPPolicy MCPInjectPolicy
}

const (
	// PermissionModeBypass skips provider approval requests.
	PermissionModeBypass = "bypass"
	// PermissionModeOnRequest forwards provider approval requests to the user.
	PermissionModeOnRequest = "on-request"
	// PermissionModeAuto automatically approves safe operations.
	PermissionModeAuto = "auto"
)
