// config 包提供 Leros 的配置加载和配置类型定义
//
// 该包负责从配置文件加载各种配置项，包括 GitHub 应用配置、
// GitLab 应用配置、NATS 消息队列配置和数据库配置等。
package config

// NATSConfig 是 NATS 消息队列的配置结构
type NATSConfig struct {
	URL string `yaml:"url,omitempty" json:"url,omitempty"` // NATS 服务地址
}

// LLMConfig is the configuration structure for LLM providers
type LLMConfig struct {
	Provider    string                `yaml:"provider"`              // LLM Provider (openai, anthropic, etc.)
	APIKey      string                `yaml:"api_key"`               // API Key
	Model       string                `yaml:"model,omitempty"`       // Default model
	BaseURL     string                `yaml:"base_url,omitempty"`    // Custom base URL
	Vision      bool                  `yaml:"vision,omitempty"`      // Default model supports image (multimodal) input
	Translation *LLMTranslationConfig `yaml:"translation,omitempty"` // Built-in translation model

	// TopP 采样温度（核采样），仅 opencode runtime 注入。无需设置可省略。
	TopP *float64 `yaml:"top_p,omitempty"`
	// FrequencyPenalty 频率惩罚，仅 opencode runtime 注入。无需设置可省略。
	FrequencyPenalty *float64 `yaml:"frequency_penalty,omitempty"`
	// PresencePenalty 存在惩罚，仅 opencode runtime 注入。无需设置可省略。
	PresencePenalty *float64 `yaml:"presence_penalty,omitempty"`
	// Limit 上下文/输出 token 上限，仅 opencode runtime 覆盖生效。不设置走默认。
	Limit *LLMLimitConfig `yaml:"limit,omitempty"`
}

// LLMLimitConfig 描述模型上下文窗口与单次输出上限。
type LLMLimitConfig struct {
	Context int `yaml:"context,omitempty"`
	Output  int `yaml:"output,omitempty"`
}

// LLMTranslationConfig configures the built-in fast translation model.
type LLMTranslationConfig struct {
	Provider  string `yaml:"provider,omitempty"`   // LLM Provider for translation
	APIKey    string `yaml:"api_key,omitempty"`    // API Key for translation
	Model     string `yaml:"model,omitempty"`      // Translation model
	BaseURL   string `yaml:"base_url,omitempty"`   // Translation base URL
	IsDefault *bool  `yaml:"is_default,omitempty"` // 是否作为翻译类默认模型，缺省 false
}

// Config 是 Leros 的主配置结构，包含所有子系统的配置
type Config struct {
	Spark  SparkConfig `yaml:"spark,omitempty" json:"spark,omitempty"`
	Server struct {
		Port                  string    `yaml:"port,omitempty"`                    // 服务器端口
		DisableEventConsumers bool      `yaml:"disable_event_consumers,omitempty"` // 是否禁用后台事件消费者
		JWT                   JWTConfig `yaml:"jwt,omitempty"`                     // JWT 认证配置
	} `yaml:"server,omitempty"` // 服务器地址
	Env                 string                                     `yaml:"env,omitempty"`
	WorkspaceRoot       string                                     `yaml:"workspace_root,omitempty" json:"workspace_root,omitempty"`
	Log                 LogConfig                                  `yaml:"log,omitempty" json:"log,omitempty"`
	Logger              LogsConfig                                 `yaml:"logger,omitempty" json:"logger,omitempty"`
	NATS                *NATSConfig                                `yaml:"nats,omitempty"`
	Database            *DatabaseConfig                            `yaml:"database,omitempty"`
	LLM                 *LLMConfig                                 `yaml:"llm,omitempty"`
	Scheduler           *SchedulerConfig                           `yaml:"scheduler,omitempty"`
	Storage             *StorageConfig                             `yaml:"storage,omitempty"`
	Gitea               *GiteaConfig                               `yaml:"gitea,omitempty"`
	WorkerAuth          *WorkerAuthConfig                          `yaml:"worker_auth,omitempty" json:"worker_auth,omitempty"`
	Aliyun              *AliyunConfig                              `yaml:"aliyun,omitempty" json:"aliyun,omitempty"`
	ClientUpdate        *ClientUpdateConfig                        `yaml:"client_update,omitempty" json:"client_update,omitempty"`
	Feishu              *FeishuConfig                              `yaml:"feishu,omitempty" json:"feishu,omitempty"`
	Auth                *IAMConfig                                 `yaml:"auth,omitempty" json:"auth,omitempty"`
	AutomationScheduler *AutomationSchedulerConfig                 `yaml:"automation_scheduler,omitempty" json:"automation_scheduler,omitempty"`
	MCPConnectors       []MCPConnectorConfig                       `yaml:"mcp_connectors,omitempty" json:"mcp_connectors,omitempty"`
	ManagedCredentials  map[string]ManagedCredentialProviderConfig `yaml:"managed_credentials,omitempty" json:"managed_credentials,omitempty"`
	PayrollProxy        PayrollProxyConfig                         `yaml:"payroll_proxy,omitempty" json:"payroll_proxy,omitempty"`
}

// PayrollProxyConfig contains the trusted Go Payroll HTTP API base URL.
type PayrollProxyConfig struct {
	Endpoint string `yaml:"endpoint" json:"endpoint"`
}

// ManagedCredentialProviderConfig points to an internal service that resolves credentials for a managed connector.
type ManagedCredentialProviderConfig struct {
	Endpoint     string `yaml:"endpoint" json:"endpoint"`
	ServiceToken string `yaml:"service_token,omitempty" json:"service_token,omitempty"`
}

// MCPConnectorConfig describes one system MCP connector declared by the server configuration.
// It defines only the reusable channel template; user credentials are collected when a user connects it.
type MCPConnectorConfig struct {
	Channel     string                 `yaml:"channel" json:"channel"`
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description,omitempty" json:"description,omitempty"`
	Status      string                 `yaml:"status,omitempty" json:"status,omitempty"`
	SkillCode   string                 `yaml:"skill_code,omitempty" json:"skill_code,omitempty"`
	Transport   string                 `yaml:"transport,omitempty" json:"transport,omitempty"`
	URL         string                 `yaml:"url,omitempty" json:"url,omitempty"`
	Headers     map[string]string      `yaml:"headers,omitempty" json:"headers,omitempty"`
	Bindings    MCPConnectorBindings   `yaml:"bindings,omitempty" json:"bindings,omitempty"`
	Auth        MCPConnectorAuthConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// MCPConnectorAuthConfig declares the authorization schema for one system connector.
type MCPConnectorAuthConfig struct {
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
	// Description is user-facing guidance shown above the connector authorization form.
	Description string                   `yaml:"description,omitempty" json:"description,omitempty"`
	Fields      []MCPConnectorAuthField  `yaml:"fields,omitempty" json:"fields,omitempty"`
	Handler     string                   `yaml:"handler,omitempty" json:"handler,omitempty"`
	OAuth       *MCPConnectorOAuthConfig `yaml:"oauth,omitempty" json:"oauth,omitempty"`
}

// MCPConnectorAuthField describes a value collected from a user when connecting a platform.
type MCPConnectorAuthField struct {
	Key         string `yaml:"key" json:"key"`
	Label       string `yaml:"label" json:"label"`
	Type        string `yaml:"type" json:"type"`
	Required    bool   `yaml:"required" json:"required"`
	Placeholder string `yaml:"placeholder,omitempty" json:"placeholder,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// MCPConnectorBindings maps connected credentials to runtime destinations.
// A value may be a credential key or a template such as "Bearer {{api_key}}".
type MCPConnectorBindings struct {
	SkillEnv   map[string]string `yaml:"skill_env,omitempty" json:"skill_env,omitempty"`
	MCPHeaders map[string]string `yaml:"mcp_headers,omitempty" json:"mcp_headers,omitempty"`
	MCPEnv     map[string]string `yaml:"mcp_env,omitempty" json:"mcp_env,omitempty"`
	MCPQuery   map[string]string `yaml:"mcp_query,omitempty" json:"mcp_query,omitempty"`
}

// MCPConnectorOAuthConfig stores operations-managed OAuth application settings.
type MCPConnectorOAuthConfig struct {
	AppKey      string   `yaml:"app_key,omitempty" json:"app_key,omitempty"`
	SecretKey   string   `yaml:"secret_key,omitempty" json:"secret_key,omitempty"`
	RedirectURI string   `yaml:"redirect_uri,omitempty" json:"redirect_uri,omitempty"`
	Scopes      []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
}

// AutomationSchedulerConfig 配置自动化定时任务的 Planner/Dispatcher 后台调度。
type AutomationSchedulerConfig struct {
	// Enabled 是否启动 Planner/Dispatcher（阶段二功能开关）。
	// nil 或未配置时默认开启；显式 false 表示关闭。
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// PlannerInterval Planner 扫描间隔（秒），缺省 30
	PlannerInterval int `yaml:"planner_interval,omitempty" json:"planner_interval,omitempty"`
}

// FeishuConfig configures Feishu Open Platform integration for feedback Bitable sync.
type FeishuConfig struct {
	Enabled   bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	AppID     string `yaml:"app_id,omitempty" json:"app_id,omitempty"`
	AppSecret string `yaml:"app_secret,omitempty" json:"app_secret,omitempty"`
	AppToken  string `yaml:"app_token,omitempty" json:"app_token,omitempty"`
	TableID   string `yaml:"table_id,omitempty" json:"table_id,omitempty"`
}

// JWTConfig JWT 认证配置
type JWTConfig struct {
	Secret string `yaml:"secret,omitempty"` // JWT 签名密钥
}

// WorkerAuthConfig configures worker bootstrap-token based authentication.
type WorkerAuthConfig struct {
	BootstrapTokens []WorkerBootstrapToken `yaml:"bootstrap_tokens,omitempty" json:"bootstrap_tokens,omitempty"`
	TokenTTLSeconds int                    `yaml:"token_ttl_seconds,omitempty" json:"token_ttl_seconds,omitempty"`
}

// WorkerBootstrapToken binds a bootstrap token to one worker/AI teammate identity.
type WorkerBootstrapToken struct {
	OrgID    uint   `yaml:"org_id" json:"org_id"`
	WorkerID uint   `yaml:"worker_id" json:"worker_id"`
	Token    string `yaml:"token" json:"token"`
}

// ClientUpdateConfig configures client version compatibility policies.
type ClientUpdateConfig struct {
	Desktop ClientUpdatePolicy `yaml:"desktop,omitempty" json:"desktop,omitempty"`
	Web     ClientUpdatePolicy `yaml:"web,omitempty" json:"web,omitempty"`
}

// ClientUpdatePolicy describes update requirements for one client app.
type ClientUpdatePolicy struct {
	MinSupportedVersion string `yaml:"min_supported_version,omitempty" json:"min_supported_version,omitempty"`
	LatestVersion       string `yaml:"latest_version,omitempty" json:"latest_version,omitempty"`
	UpdateURL           string `yaml:"update_url,omitempty" json:"update_url,omitempty"`
	ForceMessage        string `yaml:"force_message,omitempty" json:"force_message,omitempty"`
}

// DatabaseConfig 是数据库的配置结构
type DatabaseConfig struct {
	URL   string `yaml:"url,omitempty"`   // 数据库连接地址
	Debug bool   `yaml:"debug,omitempty"` // 是否启用调试模式
}

// AliyunConfig configures Aliyun SMS verification code delivery.
type AliyunConfig struct {
	AccessKeyID     string `yaml:"access_key_id,omitempty" json:"access_key_id,omitempty"`
	AccessKeySecret string `yaml:"access_key_secret,omitempty" json:"access_key_secret,omitempty"`
	SignName        string `yaml:"sign_name,omitempty" json:"sign_name,omitempty"`
	TemplateCode    string `yaml:"template_code,omitempty" json:"template_code,omitempty"`
	RegionID        string `yaml:"region_id,omitempty" json:"region_id,omitempty"`
	DefaultCode     string `yaml:"default_code,omitempty" json:"default_code,omitempty"`
}

// StorageConfig 是存储的配置结构
type StorageConfig struct {
	Driver     string `yaml:"driver"`
	Endpoint   string `yaml:"endpoint,omitempty"`
	AccessKey  string `yaml:"access_key,omitempty"`
	SecretKey  string `yaml:"secret_key,omitempty"`
	UseSSL     bool   `yaml:"use_ssl,omitempty"`
	Bucket     string `yaml:"bucket,omitempty"`
	BaseURL    string `yaml:"base_url,omitempty"`
	URLStyle   string `yaml:"url_style,omitempty"`
	LocalDir   string `yaml:"local_dir,omitempty"`
	SignSecret string `yaml:"sign_secret,omitempty"`
}

// GiteaConfig 外部 gitea 实例连接配置
type GiteaConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Endpoint    string `yaml:"endpoint"`
	AccessToken string `yaml:"access_token"`
	Owner       string `yaml:"owner"`
}

// IAMConfig configures the connection to the IAM (identity and access
// management) service. Only required when building with the enterprise
// build tag (-tags enterprise).
type IAMConfig struct {
	BaseURL               string `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	DomainName            string `yaml:"domain_name,omitempty" json:"domain_name,omitempty"`
	PhoneCodeLoginEnabled *bool  `yaml:"phone_code_login_enabled,omitempty" json:"phone_code_login_enabled,omitempty"`
}

// SparkConfig binds optional host observations to one privately managed organization.
type SparkConfig struct {
	Maintenance *SparkMaintenanceConfig `yaml:"maintenance,omitempty" json:"maintenance,omitempty"`
	SocketPath  string                  `yaml:"socket_path,omitempty" json:"socket_path,omitempty"`
	HostID      string                  `yaml:"host_id,omitempty" json:"host_id,omitempty"`
	OrgID       uint                    `yaml:"org_id,omitempty" json:"org_id,omitempty"`
	WorkerIDs   []uint                  `yaml:"worker_ids,omitempty" json:"worker_ids,omitempty"`
}

// SparkMaintenanceConfig binds read-only admission control to an operations-owned host state directory.
type SparkMaintenanceConfig struct {
	StatePath string `yaml:"state_path" json:"state_path"`
	HostID    string `yaml:"host_id" json:"host_id"`
}
