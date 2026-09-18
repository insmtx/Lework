package agent

import "time"

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
}

// RuntimeAdapterOptions contains host-provided facilities shared by CLI adapters.
type RuntimeAdapterOptions struct {
	InteractionHandler InteractionHandler
	MCPServers         []MCPServerConfig
	// ProgressIdleTimeout 限制单次 Run 内无任何进度输出的最长等待时间。
	// Runtime 自身决定是否使用该值；零值表示沿用 Runtime 内置缺省值。
	ProgressIdleTimeout time.Duration
}

const (
	// PermissionModeBypass skips provider approval requests.
	PermissionModeBypass = "bypass"
	// PermissionModeOnRequest forwards provider approval requests to the user.
	PermissionModeOnRequest = "on-request"
	// PermissionModeAuto automatically approves safe operations.
	PermissionModeAuto = "auto"
)
