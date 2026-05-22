// api 包提供 Leros 的 HTTP API 层
//
// 该包负责设置和管理 HTTP 路由，处理外部 API 请求，
// 并注册各种渠道的连接器。
package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/config"
	auth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/connectors/github"
	"github.com/insmtx/Leros/backend/internal/api/connectors/gitlab"
	"github.com/insmtx/Leros/backend/internal/api/handler"
	"github.com/insmtx/Leros/backend/internal/api/middleware"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	githubprovider "github.com/insmtx/Leros/backend/internal/infra/providers/github"
	"github.com/insmtx/Leros/backend/internal/infra/websocket"
	"github.com/insmtx/Leros/backend/internal/runnable"
	"github.com/insmtx/Leros/backend/internal/service"
	"github.com/insmtx/Leros/backend/internal/worker/scheduler"
	workerserver "github.com/insmtx/Leros/backend/internal/worker/server"
	ygmiddleware "github.com/ygpkg/yg-go/apis/runtime/middleware"
	"github.com/ygpkg/yg-go/logs"

	"gorm.io/gorm"

	_ "github.com/insmtx/Leros/docs/swagger" // Swagger 文档生成的导入
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// SetupRouter 设置事件网关的路由，注册所有连接器
//
// 根据配置初始化并注册 GitHub、GitLab 等渠道连接器，
// 同时设置客户端 WebSocket 连接器，并将所有连接器的路由注册到 HTTP 服务器。
func SetupRouter(cfg config.Config, eventbus eventbus.EventBus, db *gorm.DB) *gin.Engine {
	r := gin.New()
	r.Use(ygmiddleware.CORS())
	r.Use(middleware.CallerMiddleware(cfg.Server.JWT.Secret, db))
	r.Use(middleware.Logger(".Ping", "metrics"))
	r.Use(ygmiddleware.Recovery())
	v1 := r.Group("/v1")
	{
		if cfg.Github != nil {
			logs.Info("Setting up GitHub connector")
			authService := initThirdPartyAuthService(&cfg)
			github.RegisterGitHubRoutes(v1, *cfg.Github, eventbus, db, authService)
			logs.Info("GitHub connector registered successfully")
		} else {
			logs.Debug("No GitHub configuration provided, skipping GitHub connector setup")
		}

		if cfg.Gitlab != nil {
			logs.Info("Setting up GitLab connector")
			gitlab.RegisterGitLabRoutes(v1, *cfg.Gitlab, eventbus)
			logs.Info("GitLab connector registered successfully")
		} else {
			logs.Debug("No GitLab configuration provided, skipping GitLab connector setup")
		}
	}
	{
		websocket.RegisterWebSocketRoutes(v1, eventbus)
		logs.Info("WebSocket connector registered successfully")
	}
	{
		workerScheduler := scheduler.NewProcessScheduler(cfg.Scheduler)

		workerManager := workerserver.NewServer(workerScheduler, db)
		workerManager.RegisterRoutes(r)
		logs.Info("Worker server routes registered successfully")

		digitalAssistantService := service.NewDigitalAssistantService(db, workerScheduler)
		handler.RegisterDigitalAssistantRoutes(v1, digitalAssistantService)
		logs.Info("Digital assistant routes registered successfully")

		llmModelService := service.NewLLMModelService(db)
		handler.RegisterLLMModelRoutes(v1, llmModelService)
		logs.Info("LLM model routes registered successfully")

		inferrer := service.NewDefaultAssistantInferrer(1)
		sessionService := service.NewSessionService(db, eventbus, inferrer)
		handler.RegisterSessionRoutes(v1, sessionService)
		logs.Info("Session routes registered successfully")

		projectService := service.NewProjectService(db)
		handler.RegisterProjectRoutes(v1, projectService)
		logs.Info("Project routes registered successfully")

		// Start background consumers
		go runnable.StartSessionCompleted(context.Background(), sessionService, eventbus)
		logs.Info("Session completed runnable started")
		go runnable.StartSessionTitleHandler(context.Background(), sessionService, eventbus, db)
		logs.Info("Session title handler runnable started")
	}

	// Swagger UI 路由
	v1.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	return r
}

// initThirdPartyAuthService 初始化第三方平台授权服务并注册 provider
func initThirdPartyAuthService(cfg *config.Config) *auth.ThirdPartyAuthService {
	accountStore := auth.NewInMemoryStore()
	accountResolver := auth.NewAccountResolver(accountStore)
	authService := auth.NewThirdPartyAuthService(accountStore, accountResolver)

	if cfg != nil && cfg.Github != nil {
		authService.RegisterProvider(githubprovider.NewOAuthProvider(*cfg.Github))
	}

	return authService
}
