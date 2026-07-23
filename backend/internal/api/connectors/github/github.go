// github 包提供 GitHub 平台的连接器实现
//
// 该包实现了与 GitHub 平台的集成，包括 Webhook 事件接收、
// OAuth 认证流程、用户信息同步等功能。
package github

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v78/github"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/connectors"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
)

const (
	githubAPIBaseURL = "https://api.github.com"
)

// 确保 Connector 实现了 connectors.Connector 接口
var _ connectors.Connector = (*Connector)(nil)

// Connector implements the GitHub connector interface.
type Connector struct {
	cfg       config.GithubAppConfig
	client    *github.Client
	publisher eventbus.Publisher
	userRepo  account.UserRepository
	authSvc   *auth.ThirdPartyAuthService
}

// ChannelCode returns the channel identifier for GitHub.
func (Connector) ChannelCode() string {
	return "github"
}

// RegisterRoutes registers GitHub webhook and auth endpoints.
func (c *Connector) RegisterRoutes(r gin.IRouter) {
	r.POST("/github/webhook", c.handleWebhook)
	r.GET("/github/auth", c.oAuthRedirect)
	r.GET("/github/callback", c.oAuthCallback)
}

// RegisterGitHubRoutes 注册GitHub路由(便捷函数)
func RegisterGitHubRoutes(r gin.IRouter, cfg config.GithubAppConfig, publisher eventbus.Publisher, userRepo account.UserRepository, authSvc *auth.ThirdPartyAuthService) {
	connector := NewConnector(cfg, publisher, userRepo, authSvc)
	connector.RegisterRoutes(r)
}

// NewConnector creates a new GitHub connector instance.
func NewConnector(cfg config.GithubAppConfig, publisher eventbus.Publisher, userRepo account.UserRepository, authSvc *auth.ThirdPartyAuthService) *Connector {
	logs.Infof("Creating new GitHub connector for app ID: %d", cfg.AppID)

	var githubClient *github.Client
	if cfg.AppID != 0 && cfg.PrivateKey != "" {
		logs.Debugf("GitHub connector initialized with app ID: %d", cfg.AppID)
	} else {
		logs.Warnf("GitHub connector initialized without authentication - limited functionality")
	}

	return &Connector{
		cfg:       cfg,
		client:    githubClient,
		publisher: publisher,
		userRepo:  userRepo,
		authSvc:   authSvc,
	}
}

// oAuthRedirect initiates the GitHub OAuth flow.
func (c *Connector) oAuthRedirect(ctx *gin.Context) {
	if c.authSvc == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "authorization service unavailable"})
		return
	}
	userID := ctx.Query("user_id")
	if userID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "user_id parameter missing"})
		return
	}

	redirectURL, err := c.authSvc.StartAuthorization(ctx.Request.Context(), &auth.StartAuthorizationRequest{
		UserID:      userID,
		Provider:    auth.ProviderGitHub,
		RedirectURI: ctx.Query("redirect_uri"),
	})
	if err != nil {
		logs.ErrorContextf(ctx, "Failed to start GitHub authorization: %v", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

// oAuthCallback handles the GitHub OAuth callback.
func (c *Connector) oAuthCallback(ctx *gin.Context) {
	if c.authSvc == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "authorization service unavailable"})
		return
	}

	code := ctx.Query("code")
	if code == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "code parameter missing"})
		return
	}

	state := ctx.Query("state")
	if state == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "state parameter missing"})
		return
	}

	result, err := c.authSvc.HandleAuthorizationCallback(ctx.Request.Context(), &auth.AuthorizationCallbackRequest{
		Provider: auth.ProviderGitHub,
		State:    state,
		Code:     code,
	})
	if err != nil {
		logs.ErrorContextf(ctx, "Failed to complete GitHub authorization: %v", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response := c.buildOAuthResponse(result.Account)
	if err := c.saveUserIfNeeded(ctx, result.Account, response); err != nil {
		logs.ErrorContextf(ctx, "Failed to save user: %v", err)
	}

	ctx.JSON(http.StatusOK, response)
}

// buildOAuthResponse constructs the OAuth response.
func (c *Connector) buildOAuthResponse(account *auth.AuthorizedAccount) gin.H {
	user := gin.H{
		"github_id":    account.ExternalAccountID,
		"github_login": account.Metadata["github_login"],
		"name":         account.Metadata["name"],
		"email":        account.Metadata["email"],
	}

	return gin.H{
		"user":    user,
		"account": account,
	}
}

// saveUserIfNeeded saves user to database if available.
func (c *Connector) saveUserIfNeeded(ctx context.Context, authAccount *auth.AuthorizedAccount, response gin.H) error {
	if c.userRepo == nil {
		logs.WarnContext(ctx, "User repository not available, user info will not be saved")
		return nil
	}

	email := authAccount.Metadata["email"]
	name := authAccount.Metadata["name"]

	existingUser, err := c.userRepo.GetUser(ctx, "", email)
	if err != nil {
		return err
	}
	if existingUser != nil {
		response["user"].(gin.H)["id"] = existingUser.ID
		return nil
	}

	result, err := c.userRepo.CreateUser(ctx, &account.CreateUserInput{
		Name:  name,
		Email: email,
	})
	if err != nil {
		return err
	}

	response["user"].(gin.H)["id"] = result.UserID
	return nil
}

func parseGithubID(externalAccountID string) (int64, error) {
	var githubID int64
	_, err := fmt.Sscanf(externalAccountID, "%d", &githubID)
	if err != nil {
		return 0, fmt.Errorf("parse github id: %w", err)
	}
	return githubID, nil
}
