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
	Provider string `yaml:"provider"`           // LLM Provider (openai, anthropic, etc.)
	APIKey   string `yaml:"api_key"`            // API Key
	Model    string `yaml:"model,omitempty"`    // Default model
	BaseURL  string `yaml:"base_url,omitempty"` // Custom base URL
}

// Config 是 Leros 的主配置结构，包含所有子系统的配置
type Config struct {
	Server struct {
		Port string    `yaml:"port,omitempty"` // 服务器端口
		JWT  JWTConfig `yaml:"jwt,omitempty"`  // JWT 认证配置
	} `yaml:"server,omitempty"` // 服务器地址
	WorkspaceRoot string           `yaml:"workspace_root,omitempty" json:"workspace_root,omitempty"`
	NATS          *NATSConfig      `yaml:"nats,omitempty"`
	Database      *DatabaseConfig  `yaml:"database,omitempty"`
	LLM           *LLMConfig       `yaml:"llm,omitempty"`
	Scheduler     *SchedulerConfig `yaml:"scheduler,omitempty"`
}

// JWTConfig JWT 认证配置
type JWTConfig struct {
	Secret string `yaml:"secret,omitempty"` // JWT 签名密钥
}

// DatabaseConfig 是数据库的配置结构
type DatabaseConfig struct {
	URL   string `yaml:"url,omitempty"`   // 数据库连接地址
	Debug bool   `yaml:"debug,omitempty"` // 是否启用调试模式
}
