// gitlab 包提供 GitLab 平台的连接器实现
//
// 该包实现了与 GitLab 平台的集成，包括 Webhook 事件接收、
// OAuth 认证流程等功能。
package gitlab

import (
	"github.com/gin-gonic/gin"
	"github.com/insmtx/Lework/backend/config"
	"github.com/insmtx/Lework/backend/internal/api/connectors"
	eventbus "github.com/insmtx/Lework/backend/internal/infra/mq"
	"github.com/ygpkg/yg-go/logs"
)

// 确保 GitlabConnector 实现了 connectors.Connector 接口
var _ connectors.Connector = (*GitlabConnector)(nil)

// GitlabConnector 是 GitLab 平台的连接器实现
type GitlabConnector struct {
	config    config.GitlabAppConfig // GitLab 应用配置
	publisher eventbus.Publisher     // 事件发布者
}

// ChannelCode 返回 GitLab 渠道的标识符
func (c *GitlabConnector) ChannelCode() string {
	return "gitlab"
}

// RegisterRoutes registers GitLab webhook endpoints.
func (c *GitlabConnector) RegisterRoutes(r gin.IRouter) {
	r.POST("/gitlab/webhook", c.HandleWebhook)
}

// NewConnector 创建一个新的 GitLab 连接器实例
func NewConnector(cfg config.GitlabAppConfig, publisher eventbus.Publisher) *GitlabConnector {
	return &GitlabConnector{
		config:    cfg,
		publisher: publisher,
	}
}

// RegisterGitLabRoutes 注册GitLab路由(便捷函数)
func RegisterGitLabRoutes(r gin.IRouter, cfg config.GitlabAppConfig, publisher eventbus.Publisher) {
	connector := NewConnector(cfg, publisher)
	connector.RegisterRoutes(r)
}

func (c *GitlabConnector) HandleWebhook(ctx *gin.Context) {
	eventType := ctx.GetHeader("X-Gitlab-Event")
	if eventType == "" {
		logs.ErrorContext(ctx, "Missing X-Gitlab-Event header")
		ctx.JSON(400, gin.H{"error": "Missing X-Gitlab-Event header"})
		return
	}

	logs.InfoContextf(ctx, "Received GitLab event: %s", eventType)

	payload, err := ctx.GetRawData()
	if err != nil {
		logs.ErrorContextf(ctx, "Failed to read request body: %v", err)
		ctx.JSON(400, gin.H{"error": "Failed to read request body"})
		return
	}

	if err := c.verifySignature(ctx, payload); err != nil {
		logs.ErrorContextf(ctx, "Signature verification failed: %v", err)
		ctx.JSON(403, gin.H{"error": "Invalid signature"})
		return
	}

	if err := c.processEvent(ctx, eventType, payload); err != nil {
		logs.ErrorContextf(ctx, "Failed to process event: %v", err)
		ctx.JSON(500, gin.H{"error": "Failed to process event"})
		return
	}

	ctx.JSON(200, gin.H{"status": "ok"})
}

func (c *GitlabConnector) verifySignature(ctx *gin.Context, payload []byte) error {
	return nil
}

func (c *GitlabConnector) processEvent(ctx *gin.Context, eventType string, payload []byte) error {
	return nil
}
