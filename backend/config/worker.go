package config

type WorkerConfig struct {
	OrgID    uint `yaml:"org_id" json:"org_id"`
	WorkerID uint `yaml:"worker_id" json:"worker_id"`

	ServerAddr     string `yaml:"server_addr,omitempty" json:"server_addr,omitempty"`
	AuthToken      string `yaml:"auth_token,omitempty" json:"auth_token,omitempty"`
	BootstrapToken string `yaml:"bootstrap_token,omitempty" json:"bootstrap_token,omitempty"`
	WorkspaceRoot  string `yaml:"workspace_root,omitempty" json:"workspace_root,omitempty"`

	Env    string            `yaml:"env,omitempty" json:"env,omitempty"`
	Log    LogConfig         `yaml:"log,omitempty" json:"log,omitempty"`
	Logger LogsConfig        `yaml:"logger,omitempty" json:"logger,omitempty"`
	NATS   *NATSConfig       `yaml:"nats,omitempty"`
	CLI    *CLIEnginesConfig `yaml:"cli,omitempty"`
	Gitea  *GiteaConfig      `yaml:"gitea,omitempty"`
	Run    *RunConfig        `yaml:"run,omitempty" json:"run,omitempty"`
}

// Effective returns the effective RunConfig with defaults applied AND normalized:
//   - MaxConcurrency < 1   -> 10
//   - MaxInflight    < 1   -> 20
//   - MaxInflight    < MaxConcurrency -> 提升到 MaxConcurrency（准入并发必须不小于运行并发）
//   - MaxInteractionWaits < 1 -> 10
//   - DebounceMS     < 1   -> 1500
//   - InteractionTimeoutSeconds < 1 -> 600
//   - MaxQueuedCommands < 1 -> 1000
//   - QueueRetrySeconds < 1 -> 15
//   - QueueStartTimeoutSeconds < 1 -> 1800
//   - MaxRunDurationSeconds < 1 -> 14400
//
// 归一化只在此处进行；Handler / Coordinator 复用该结果，不再重复定义另一套默认值，
// 保证启动日志打印的就是最终生效值。
func (c *RunConfig) Effective() RunConfig {
	if c == nil {
		c = &RunConfig{}
	}
	eff := *c
	if eff.MaxConcurrency <= 0 {
		eff.MaxConcurrency = 10
	}
	if eff.MaxInflight <= 0 {
		eff.MaxInflight = 20
	}
	// 准入并发必须至少等于运行并发。
	if eff.MaxInflight < eff.MaxConcurrency {
		eff.MaxInflight = eff.MaxConcurrency
	}
	if eff.MaxInteractionWaits <= 0 {
		eff.MaxInteractionWaits = 10
	}
	if eff.DebounceMS <= 0 {
		eff.DebounceMS = 1500
	}
	if eff.InteractionTimeoutSeconds <= 0 {
		eff.InteractionTimeoutSeconds = 600
	}
	if eff.MaxQueuedCommands <= 0 {
		eff.MaxQueuedCommands = 1000
	}
	if eff.QueueRetrySeconds <= 0 {
		eff.QueueRetrySeconds = 15
	}
	if eff.QueueStartTimeoutSeconds <= 0 {
		eff.QueueStartTimeoutSeconds = 1800
	}
	if eff.MaxRunDurationSeconds <= 0 {
		eff.MaxRunDurationSeconds = 14400
	}
	return eff
}

// RunConfig 配置 Worker 的分层调度容量与交互等待生命周期。
type RunConfig struct {
	// MaxConcurrency 实际可占用计算/运行资源的任务数量。
	MaxConcurrency int `yaml:"max_concurrency,omitempty" json:"max_concurrency,omitempty" default:"10"`
	// MaxInflight Worker 准入并发上限（满载时由 NATS 回调持续发送 InProgress）。
	MaxInflight int `yaml:"max_inflight,omitempty" json:"max_inflight,omitempty" default:"20"`
	// MaxInteractionWaits 最大并发交互等待数量。
	MaxInteractionWaits int `yaml:"max_interaction_waits,omitempty" json:"max_interaction_waits,omitempty" default:"10"`
	// DebounceMS trailing debounce 窗口（毫秒）。
	DebounceMS int `yaml:"debounce_ms,omitempty" json:"debounce_ms,omitempty" default:"1500"`
	// InteractionTimeoutSeconds 审批/问题等待的默认硬超时（秒），缺省 600（10 分钟）。
	InteractionTimeoutSeconds int `yaml:"interaction_timeout_seconds,omitempty" json:"interaction_timeout_seconds,omitempty" default:"600"`
	// MaxQueuedCommands Worker 本地 inbox 允许保留的最大非终态命令数量。
	MaxQueuedCommands int `yaml:"max_queued_commands,omitempty" json:"max_queued_commands,omitempty" default:"1000"`
	// QueueRetrySeconds 本地队列满载时请求 JetStream 延迟重投的秒数。
	QueueRetrySeconds int `yaml:"queue_retry_seconds,omitempty" json:"queue_retry_seconds,omitempty" default:"15"`
	// QueueStartTimeoutSeconds 命令从创建到允许开始执行的最长等待时间。
	QueueStartTimeoutSeconds int `yaml:"queue_start_timeout_seconds,omitempty" json:"queue_start_timeout_seconds,omitempty" default:"1800"`
	// MaxRunDurationSeconds Run 从真正开始执行起的硬超时。
	MaxRunDurationSeconds int `yaml:"max_run_duration_seconds,omitempty" json:"max_run_duration_seconds,omitempty" default:"14400"`
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
