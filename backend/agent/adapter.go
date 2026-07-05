package agent

// MCPServerConfig describes one MCP endpoint exposed to an external Runtime.
type MCPServerConfig struct {
	Name        string
	URL         string
	Command     string
	Args        []string
	Env         map[string]string
	BearerToken string
}

// RuntimeAdapterOptions contains host-provided facilities shared by CLI adapters.
type RuntimeAdapterOptions struct {
	InteractionHandler InteractionHandler
	MCPServers         []MCPServerConfig
}

const (
	// PermissionModeBypass skips provider approval requests.
	PermissionModeBypass = "bypass"
	// PermissionModeOnRequest forwards provider approval requests to the user.
	PermissionModeOnRequest = "on-request"
	// PermissionModeAuto automatically approves safe operations.
	PermissionModeAuto = "auto"
)
