// api 包提供 Leros 的 HTTP API 层
//
// 该包负责设置和管理 HTTP 路由，处理外部 API 请求，
// 并注册各种渠道的连接器。
package api

import (
	"context"

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

	var workerScheduler worker.WorkerScheduler
	var workerProvisioningService *service.WorkerProvisioningService
	{
		if cfg.Scheduler != nil {
			var err error
			workerScheduler, err = scheduler.New(cfg.Scheduler)
			if err != nil {
				logs.Errorf("Worker scheduler initialization failed: %v", err)
			}
		}

		workerManager := workerserver.NewServer(workerScheduler)
		workerManager.RegisterRoutes(r)
		logs.Info("Worker server routes registered successfully")

		if db != nil {
			workerProvisioningService = service.NewWorkerProvisioningService(db, cfg.Scheduler)
		}
	}

	// ── 公开路由（无需 org 认证）──────────────────────────────────────────────────
	{
		websocket.RegisterWebSocketRoutes(v1, eventbus)
		logs.Info("WebSocket connector registered successfully")

		authService := service.NewAuthServiceWithProvisioning(db, cfg.Server.JWT.Secret, cfg.Aliyun, workerProvisioningService)
		handler.RegisterAuthRoutes(v1, authService)
		logs.Info("Auth routes registered successfully")

		handler.RegisterWorkerAuthRoutes(v1, cfg.WorkerAuth, cfg.Server.JWT.Secret, db)
		logs.Info("Worker auth routes registered successfully")

		handler.RegisterClientUpdateRoutes(v1, cfg.ClientUpdate)
		logs.Info("Client update routes registered successfully")
	}

	// ── 鉴权路由（RequireCallerOrg 统一拦截未认证/未绑定 org 的请求）─────────────
	permSvc := service.NewPermissionService(db)
	authed := v1.Group("/", middleware.RequireCallerOrg())
	{
		digitalAssistantService := service.NewDigitalAssistantServiceWithProvisioning(db, workerScheduler, workerProvisioningService)
		handler.RegisterDigitalAssistantRoutes(authed, digitalAssistantService)
		logs.Info("Digital assistant routes registered successfully")

		aiTeammateTemplateService := service.NewAITeammateTemplateService(db)
		handler.RegisterAITeammateTemplateRoutes(authed, aiTeammateTemplateService)
		logs.Info("AI teammate template routes registered successfully")

		llmModelService := service.NewLLMModelService(db)
		handler.RegisterLLMModelRoutes(authed, llmModelService)
		logs.Info("LLM model routes registered successfully")

		inferrer := service.NewDefaultAssistantInferrer(1)
		sessionService := service.NewSessionService(db, eventbus, inferrer, giteaClient, cfg.Gitea, cfg.Env)
		handler.RegisterSessionRoutes(authed, sessionService, permSvc)
		handler.RegisterGlobalEventRoutes(authed, sessionService)
		logs.Info("Session routes registered successfully")

		projectService := service.NewProjectServiceWithInferrerAndPublisher(db, inferrer, giteaClient, cfg.Gitea, cfg.Env, eventbus)
		handler.RegisterProjectRoutes(authed, projectService, permSvc)
		logs.Info("Project routes registered successfully")

		handler.RegisterPermissionRoutes(authed, NewPermissionBatchChecker(permSvc))
		logs.Info("Permission routes registered successfully")

		projectFileHandler := handler.NewProjectFileHandler(projectService, permSvc)
		projectFileHandler.RegisterRoutes(authed)
		logs.Info("Project file routes registered successfully")

		taskService := service.NewTaskService(db)
		handler.RegisterTaskRoutes(authed, taskService, permSvc)
		logs.Info("Task routes registered successfully")

		fileService := service.NewFileService(db)
		fileHandler := handler.NewFileHandler(fileService)
		fileHandler.RegisterRoutes(authed)
		logs.Info("File routes registered successfully")

		orgService := service.NewOrgServiceWithProvisioning(db, workerProvisioningService)
		handler.RegisterOrgRoutes(authed, orgService)
		logs.Info("Organization routes registered successfully")

		departmentService := service.NewDepartmentService(db)
		handler.RegisterDepartmentRoutes(authed, departmentService)
		logs.Info("Department routes registered successfully")

		userService := service.NewUserService(db)
		handler.RegisterUserRoutes(authed, userService)
		logs.Info("User routes registered successfully")

		skillMarketplaceService := service.NewSkillMarketplaceServiceWithTranslator(db, eventbus, inferrer, service.NewDefaultSkillDescriptionTranslator(db), filestore.GetStorage(), filestore.DefaultBucket())
		handler.RegisterSkillMarketplaceRoutes(authed, skillMarketplaceService)
		logs.Info("Skill marketplace routes registered successfully")

		skillService := service.NewSkillService(db, eventbus, inferrer)
		handler.RegisterSkillRoutes(authed, skillService)
		logs.Info("Skill management routes registered successfully")

		feedbackService := service.NewFeedbackService(db, fileService, cfg.Feishu)
		handler.RegisterFeedbackRoutes(v1, feedbackService)
		logs.Info("Feedback routes registered successfully")

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
			go service.StartWorkerDeploymentReconciler(context.Background(), db, workerScheduler, cfg.Scheduler, eventbus)
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
