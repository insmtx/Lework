// config 包提供 Lework 的配置加载和配置类型定义
//
// 该包负责从配置文件加载各种配置项，包括 GitHub 应用配置、
// GitLab 应用配置、RabbitMQ 消息队列配置和数据库配置等。
package config

// GithubAppConfig 是 GitHub 应用的配置结构
type GithubAppConfig struct {
	AppID             int64    `yaml:"app_id"`              // GitHub 应用 ID
	PrivateKey        string   `yaml:"private_key"`         // GitHub 应用私钥
	WebhookSecret     string   `yaml:"webhook_secret"`      // Webhook 签名密钥
	BaseURL           string   `yaml:"base_url"`            // GitHub API 基础地址
	ClientID          string   `yaml:"client_id"`           // OAuth 客户端 ID
	ClientSecret      string   `yaml:"client_secret"`       // OAuth 客户端密钥
	RedirectURL       string   `yaml:"redirect_url"`        // OAuth 回调地址
	OAuthScopes       []string `yaml:"oauth_scopes"`        // OAuth scope 列表
	SkipWebhookVerify bool     `yaml:"skip_webhook_verify"` // 跳过webhook签名验证（仅用于开发）
}
