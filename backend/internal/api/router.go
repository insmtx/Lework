// api 包提供 Leros 的 HTTP API 层
//
// 该包负责设置和管理 HTTP 路由，处理外部 API 请求，
// 并注册各种渠道的连接器。
package api

import (
	"context"
	"net/http"
	"time"

	"code.gitea.io/sdk/gitea"
	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter"
	"github.com/insmtx/Leros/backend/internal/api/handler"
	"github.com/insmtx/Leros/backend/internal/api/middleware"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/infra/websocket"
	"github.com/insmtx/Leros/backend/internal/llm"
	"github.com/insmtx/Leros/backend/internal/modelrouter"
	"github.com/insmtx/Leros/backend/internal/runnable"
	"github.com/insmtx/Leros/backend/internal/service"
	"github.com/insmtx/Leros/backend/internal/worker"
	"github.com/insmtx/Leros/backend/internal/worker/scheduler"
	workerserver "github.com/insmtx/Leros/backend/internal/worker/server"
	"github.com/nats-io/nats.go"
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
func SetupRouter(cfg config.Config, edition adapter.Edition, eventbus eventbus.EventBus, db *gorm.DB, modelInvoker modelrouter.Invoker) (*gin.Engine, worker.WorkerScheduler) {
	r := gin.New()

	// ── 全局中间件（必须在 r.Group / RegisterRoutes 之前挂载）────────────────────
	// Gin 的 RouterGroup.Group() 与 handle() 通过 combineHandlers 值拷贝当前
	// Handlers 切片。若先 Group/注册路由再 r.Use，已注册路由的处理链拿不到
	// 后续追加的中间件，会导致 /v1/* 路由响应缺失 CORS 头，浏览器跨域请求失败。
	// 见 gin routergroup.go: Group/handle/combineHandlers。
	var workerScheduler worker.WorkerScheduler
	var workerProvisioningService *service.WorkerProvisioningService
	var giteaClient *gitea.Client
	{
		if cfg.Scheduler != nil {
			var err error
			workerScheduler, err = scheduler.New(cfg.Scheduler)
			if err != nil {
				logs.Errorf("Worker scheduler initialization failed: %v", err)
			}
		}
		if db != nil {
			workerProvisioningService = service.NewWorkerProvisioningService(db, cfg.Scheduler)
		}
		if cfg.Gitea != nil && cfg.Gitea.Enabled {
			var err error
			giteaClient, err = gitea.NewClient(cfg.Gitea.Endpoint,
				gitea.SetToken(cfg.Gitea.AccessToken),
				gitea.SetHTTPClient(&http.Client{Timeout: 30 * time.Second}))
			if err != nil {
				logs.Errorf("create gitea client: %v", err)
				giteaClient = nil
			}
		}
	}

	tokenParser := edition.TokenParser()
	authService := edition.Auth()
	userRepo := edition.User()
	orgRepo := edition.Org()
	_ = edition.Department()

	r.Use(middleware.CORS())
	r.Use(middleware.CallerMiddleware(tokenParser, db))
	r.Use(middleware.ResponseRequestID())
	r.Use(middleware.ClientUpdateMiddleware(cfg.ClientUpdate))
	r.Use(middleware.Logger(".Ping", "metrics", "/plugins/mcp/oauth/:platform_code/callback"))
	r.Use(ygmiddleware.Recovery())

	v1 := r.Group("/v1")
	pluginService := service.NewPluginServiceWithAPIKeyIssuer(
		db,
		edition.APIKeyIssuer(),
	)

	// Worker server routes 注册公开管理端点；放在全局中间件之后以继承 CORS/Logger/Recovery。
	workerManager := workerserver.NewServer(workerScheduler)
	workerManager.RegisterRoutes(r)
	logs.Info("Worker server routes registered successfully")

	// ── 公开路由（无需 org 认证）──────────────────────────────────────────────────
	{
		handler.RegisterPluginOAuthCallbackRoutes(v1, pluginService)
		logs.Info("Plugin OAuth callback routes registered successfully")

		websocket.RegisterWebSocketRoutes(v1, eventbus)
		logs.Info("WebSocket connector registered successfully")

		handler.RegisterAuthRoutes(v1, authService)
		logs.Info("Auth routes registered successfully")

		handler.RegisterWorkerAuthRoutes(v1, tokenParser)
		logs.Info("Worker auth routes registered successfully")

		handler.RegisterClientUpdateRoutes(v1, cfg.ClientUpdate)
		logs.Info("Client update routes registered successfully")

		handler.RegisterFrontendEventRoutes(v1)
		logs.Info("Frontend event routes registered successfully")

		handler.RegisterGlobalRoutes(v1, edition, &cfg)
		logs.Info("Global routes registered successfully")
	}

	// ── 内部路由（worker 上报，不走 RequireCallerOrg）─────────────
	llmUsageRecorder := llm.NewRecorder(db)
	llm.RegisterUsageRoute(v1, llmUsageRecorder)
	logs.Info("LLM usage report route registered successfully")

	// ── 鉴权路由（RequireCallerOrg 统一拦截未认证/未绑定 org 的请求）─────────────
	permSvc := service.NewPermissionService(db, service.NewPermissionCore(db))
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
		sessionService := service.NewSessionService(db, permSvc, eventbus, inferrer, giteaClient, cfg.Gitea, cfg.Env, modelInvoker, userRepo, orgRepo)
		handler.RegisterSessionRoutes(authed, sessionService, permSvc)
		handler.RegisterGlobalEventRoutes(authed, sessionService)
		logs.Info("Session routes registered successfully")

		projectService := service.NewProjectServiceWithInferrerAndPublisher(db, permSvc, inferrer, giteaClient, cfg.Gitea, cfg.Env, eventbus, userRepo)
		handler.RegisterProjectRoutes(authed, projectService, permSvc)
		logs.Info("Project routes registered successfully")

		handler.RegisterPermissionRoutes(authed, NewPermissionBatchChecker(permSvc))
		logs.Info("Permission routes registered successfully")

		projectFileHandler := handler.NewProjectFileHandler(projectService, permSvc)
		projectFileHandler.RegisterRoutes(authed)
		logs.Info("Project file routes registered successfully")

		taskService := service.NewTaskService(db, permSvc)
		handler.RegisterTaskRoutes(authed, taskService, permSvc)
		logs.Info("Task routes registered successfully")

		fileService := service.NewFileService(db)
		fileHandler := handler.NewFileHandler(fileService)
		fileHandler.RegisterRoutes(authed)
		logs.Info("File routes registered successfully")

		orgService := edition.Org()
		handler.RegisterOrgRoutes(authed, orgService)
		logs.Info("Organization routes registered successfully")

		departmentService := edition.Department()
		handler.RegisterDepartmentRoutes(authed, departmentService)
		logs.Info("Department routes registered successfully")

		userService := edition.User()
		handler.RegisterUserRoutes(authed, userService)
		logs.Info("User routes registered successfully")

		handler.RegisterPluginRoutes(authed, pluginService)
		logs.Info("Plugin repository routes registered successfully")
		officialPluginMarketplaceService := service.NewOfficialPluginMarketplaceService(db)
		handler.RegisterOfficialPluginMarketplaceRoutes(authed, officialPluginMarketplaceService)
		logs.Info("Official plugin marketplace routes registered successfully")

		feedbackService := service.NewFeedbackService(db, fileService, cfg.Feishu, modelInvoker, userRepo)
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
			go service.StartSkillPackageUploadedConsumer(context.Background(), db, eventbus)
			logs.Info("Skill package uploaded consumer started")
		} else {
			logs.Info("Session event consumers disabled by config")
		}

		// LLM usage consumer 消费 worker 上报的 org.*.usage.llm 事件并写入 llm_history。
		go llm.StartUsageConsumer(context.Background(), llmUsageRecorder, &llmUsageSubscriberAdapter{eb: eventbus})
		logs.Info("LLM usage consumer started")
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
	return r, workerScheduler
}

// llmUsageSubscriberAdapter 将 eventbus.Subscriber 适配为 llm.UsageSubscriber。
type llmUsageSubscriberAdapter struct {
	eb eventbus.Subscriber
}

func (a *llmUsageSubscriberAdapter) Subscribe(ctx context.Context, topic, consumer string, handler func(data []byte)) error {
	return a.eb.Subscribe(ctx, topic, consumer, func(msg_ *nats.Msg) { handler(msg_.Data) })
}
