// api 包提供 Leros 的 HTTP API 层
//
// 该包负责设置和管理 HTTP 路由，处理外部 API 请求，
// 并注册各种渠道的连接器。
package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/handler"
	"github.com/insmtx/Leros/backend/internal/api/middleware"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
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
	r.Use(middleware.CORS())
	r.Use(middleware.CallerMiddleware(cfg.Server.JWT.Secret, db))
	r.Use(middleware.Logger(".Ping", "metrics"))
	r.Use(ygmiddleware.Recovery())
	v1 := r.Group("/v1")
	{
		websocket.RegisterWebSocketRoutes(v1, eventbus)
		logs.Info("WebSocket connector registered successfully")
	}
	{
		workerScheduler := scheduler.NewProcessScheduler(cfg.Scheduler)

		workerManager := workerserver.NewServer(workerScheduler)
		workerManager.RegisterRoutes(r)
		logs.Info("Worker server routes registered successfully")

		authService := service.NewAuthService(db, cfg.Server.JWT.Secret)
		handler.RegisterAuthRoutes(v1, authService)
		logs.Info("Auth routes registered successfully")

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

		projectFileHandler := handler.NewProjectFileHandler(projectService)
		projectFileHandler.RegisterRoutes(v1)
		logs.Info("Project file routes registered successfully")

		workService := service.NewWorkService(db, eventbus, inferrer)
		handler.RegisterWorkRoutes(v1, workService)
		logs.Info("Work routes registered successfully")

		taskService := service.NewTaskService(db)
		handler.RegisterTaskRoutes(v1, taskService)
		logs.Info("Task routes registered successfully")

		artifactService := service.NewArtifactService(db)
		handler.RegisterArtifactRoutes(v1, artifactService)
		logs.Info("Artifact routes registered successfully")

		fileService := service.NewFileService(db)
		fileHandler := handler.NewFileHandler(fileService)
		fileHandler.RegisterRoutes(v1)
		logs.Info("File routes registered successfully")

		orgService := service.NewOrgService(db)
		handler.RegisterOrgRoutes(v1, orgService)
		logs.Info("Organization routes registered successfully")

		userService := service.NewUserService(db)
		handler.RegisterUserRoutes(v1, userService)
		logs.Info("User routes registered successfully")

		skillMarketplaceService := service.NewSkillMarketplaceService(db, eventbus)
		handler.RegisterSkillMarketplaceRoutes(v1, skillMarketplaceService)
		logs.Info("Skill marketplace routes registered successfully")

		// Start background consumers
		go runnable.StartSessionArtifactDeclared(context.Background(), eventbus, db)
		logs.Info("Session artifact declared runnable started")
		go runnable.StartSessionRunStarted(context.Background(), sessionService, eventbus)
		logs.Info("Session run started runnable started")
		go runnable.StartSessionCompleted(context.Background(), sessionService, eventbus)
		logs.Info("Session completed runnable started")
		go runnable.StartSessionTitleHandler(context.Background(), sessionService, eventbus, db)
		logs.Info("Session title handler runnable started")
	}

	staticGroup := v1.Group("/static", middleware.StaticAuth(
		filestore.StaticAPIKey(),
		cfg.Server.JWT.Secret,
		db,
	))
	handler.RegisterStaticRoutes(staticGroup)
	logs.Info("Static routes registered successfully")

	if filestore.IsLocal() {
		handler.RegisterPresignedRoutes(r)
		logs.Info("Presigned consumption routes registered (local driver)")
	}

	// Swagger UI 路由
	v1.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	return r
}
