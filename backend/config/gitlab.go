// config 包提供 Lework 的配置加载和配置类型定义
//
// 该包负责从配置文件加载各种配置项，包括 GitHub 应用配置、
// GitLab 应用配置、RabbitMQ 消息队列配置和数据库配置等。
package config

// GitlabAppConfig 是 GitLab 应用的配置结构
type GitlabAppConfig struct {
	AppID         int64  `yaml:"app_id,omitempty"`         // GitLab 应用 ID
	PrivateKey    string `yaml:"private_key,omitempty"`    // GitLab 应用私钥
	WebhookSecret string `yaml:"webhook_secret,omitempty"` // Webhook 签名密钥
	BaseURL       string `yaml:"base_url,omitempty"`       // GitLab API 基础地址
}
