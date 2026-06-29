package config

type WorkerConfig struct {
	OrgID    uint `yaml:"org_id" json:"org_id"`
	WorkerID uint `yaml:"worker_id" json:"worker_id"`

	ServerAddr     string `yaml:"server_addr,omitempty" json:"server_addr,omitempty"`
	AuthToken      string `yaml:"auth_token,omitempty" json:"auth_token,omitempty"`
	BootstrapToken string `yaml:"bootstrap_token,omitempty" json:"bootstrap_token,omitempty"`
	WorkspaceRoot  string `yaml:"workspace_root,omitempty" json:"workspace_root,omitempty"`
	AppKey         string `yaml:"app_key,omitempty" json:"app_key,omitempty"`

	Env   string            `yaml:"env,omitempty" json:"env,omitempty"`
	NATS  *NATSConfig       `yaml:"nats,omitempty"`
	CLI   *CLIEnginesConfig `yaml:"cli,omitempty"`
	Gitea *GiteaConfig      `yaml:"gitea,omitempty"`
}

// CLIEnginesConfig is the configuration for external AI coding CLIs.
type CLIEnginesConfig struct {
	Default string     `yaml:"default,omitempty" json:"default,omitempty"`
	MCP     *MCPConfig `yaml:"mcp,omitempty" json:"mcp,omitempty"`
}

// MCPConfig is the configuration for MCP server registration with external CLI tools.
type MCPConfig struct {
	URL         string `yaml:"url,omitempty" json:"url,omitempty"`
	BearerToken string `yaml:"bearer_token,omitempty" json:"bearer_token,omitempty"`
}
