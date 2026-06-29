// api 包提供 Leros 的 HTTP API 层
//
// 该包负责设置和管理 HTTP 路由，处理外部 API 请求，
// 并注册各种渠道的连接器。
package api

import (
	"context"
	"strings"

	"code.gitea.io/sdk/gitea"
	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/handler"
	"github.com/insmtx/Leros/backend/internal/api/middleware"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/infra/websocket"
	"github.com/insmtx/Leros/backend/internal/runnable"
	"github.com/insmtx/Leros/backend/internal/service"
	"github.com/insmtx/Leros/backend/internal/worker"
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
	r.Use(middleware.ClientUpdateMiddleware(cfg.ClientUpdate))
	r.Use(middleware.Logger(".Ping", "metrics"))
	r.Use(ygmiddleware.Recovery())

	var giteaClient *gitea.Client
	if cfg.Gitea != nil && cfg.Gitea.Enabled {
		var err error
		giteaClient, err = gitea.NewClient(cfg.Gitea.Endpoint, gitea.SetToken(cfg.Gitea.AccessToken))
		if err != nil {
			logs.Errorf("create gitea client: %v", err)
			giteaClient = nil
		}
	}

	v1 := r.Group("/v1")
	{
		websocket.RegisterWebSocketRoutes(v1, eventbus)
		logs.Info("WebSocket connector registered successfully")
	}
	{
		var workerScheduler worker.WorkerScheduler
		if cfg.Scheduler != nil && strings.TrimSpace(cfg.Scheduler.Mode) != "" {
			var err error
			workerScheduler, err = scheduler.New(cfg.Scheduler)
			if err != nil {
				logs.Errorf("Worker scheduler initialization failed: %v", err)
			}
		}

		workerManager := workerserver.NewServer(workerScheduler)
		workerManager.RegisterRoutes(r)
		logs.Info("Worker server routes registered successfully")

		authService := service.NewAuthService(db, cfg.Server.JWT.Secret, cfg.Aliyun)
		handler.RegisterAuthRoutes(v1, authService)
		logs.Info("Auth routes registered successfully")

		handler.RegisterWorkerAuthRoutes(v1, cfg.WorkerAuth, cfg.Server.JWT.Secret, db)
		logs.Info("Worker auth routes registered successfully")

		handler.RegisterClientUpdateRoutes(v1, cfg.ClientUpdate)
		logs.Info("Client update routes registered successfully")

		var workerProvisioningService *service.WorkerProvisioningService
		if db != nil {
			workerProvisioningService = service.NewWorkerProvisioningService(db, cfg.Scheduler)
		}
		digitalAssistantService := service.NewDigitalAssistantServiceWithProvisioning(db, workerScheduler, workerProvisioningService)
		handler.RegisterDigitalAssistantRoutes(v1, digitalAssistantService)
		logs.Info("Digital assistant routes registered successfully")

		llmModelService := service.NewLLMModelService(db)
		handler.RegisterLLMModelRoutes(v1, llmModelService)
		logs.Info("LLM model routes registered successfully")

		inferrer := service.NewDefaultAssistantInferrer(1)
		sessionService := service.NewSessionService(db, eventbus, inferrer, giteaClient, cfg.Gitea, cfg.Env)
		handler.RegisterSessionRoutes(v1, sessionService)
		logs.Info("Session routes registered successfully")

		// projectService := service.NewProjectService(db, giteaClient, cfg.Gitea, cfg.Env)
		projectService := service.NewProjectServiceWithInferrer(db, inferrer, giteaClient, cfg.Gitea, cfg.Env)
		handler.RegisterProjectRoutes(v1, projectService)
		logs.Info("Project routes registered successfully")

		projectFileHandler := handler.NewProjectFileHandler(projectService)
		projectFileHandler.RegisterRoutes(v1)
		logs.Info("Project file routes registered successfully")

		workService := service.NewWorkService(db, eventbus, inferrer, giteaClient, cfg.Gitea, cfg.Env, sessionService)
		handler.RegisterWorkRoutes(v1, workService)
		logs.Info("Work routes registered successfully")

		taskService := service.NewTaskService(db)
		handler.RegisterTaskRoutes(v1, taskService)
		logs.Info("Task routes registered successfully")

		artifactService := service.NewArtifactService(db, nil)
		handler.RegisterArtifactRoutes(v1, artifactService)
		logs.Info("Artifact routes registered successfully")

		fileService := service.NewFileService(db)
		fileHandler := handler.NewFileHandler(fileService)
		fileHandler.RegisterRoutes(v1)
		logs.Info("File routes registered successfully")

		orgService := service.NewOrgServiceWithProvisioning(db, workerProvisioningService)
		handler.RegisterOrgRoutes(v1, orgService)
		logs.Info("Organization routes registered successfully")

		userService := service.NewUserService(db)
		handler.RegisterUserRoutes(v1, userService)
		logs.Info("User routes registered successfully")

		skillMarketplaceService := service.NewSkillMarketplaceServiceWithTranslator(db, eventbus, inferrer, service.NewDefaultSkillDescriptionTranslator(db), filestore.GetStorage(), filestore.DefaultBucket())
		handler.RegisterSkillMarketplaceRoutes(v1, skillMarketplaceService)
		logs.Info("Skill marketplace routes registered successfully")

		skillService := service.NewSkillService(db, eventbus, inferrer)
		handler.RegisterSkillRoutes(v1, skillService)
		logs.Info("Skill management routes registered successfully")

		// Start background consumers
		if !cfg.Server.DisableEventConsumers {
			// 统一的 run state projector，消费 org.*.session.*.run.state
			// 替代旧分散的 StartSessionRunStarted + StartSessionArtifactDeclared + StartSessionCompleted
			go runnable.StartSessionRunStateProjector(context.Background(), sessionService, eventbus, db)
			logs.Info("Session run state projector started")
			// Stream projector records the stream lane start seq for SSE replay.
			go runnable.StartSessionRunStreamProjector(context.Background(), sessionService, eventbus)
			logs.Info("Session run stream projector started")
		} else {
			logs.Info("Session event consumers disabled by config")
		}
		if workerScheduler != nil {
			go service.StartWorkerDeploymentReconciler(context.Background(), db, workerScheduler, cfg.Scheduler)
			logs.Info("Worker deployment reconciler started")
		}
	}

	staticGroup := v1.Group("/static")
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
