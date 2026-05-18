package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/engines/builtin"
	agentruntime "github.com/insmtx/Leros/backend/internal/agent/runtime"
	runtimemcp "github.com/insmtx/Leros/backend/internal/agent/runtime/mcp"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/worker/identity"
	"github.com/insmtx/Leros/backend/internal/worker/taskconsumer"
	nodetools "github.com/insmtx/Leros/backend/tools/node"
	"github.com/spf13/cobra"
	"github.com/ygpkg/yg-go/lifecycle"
	"github.com/ygpkg/yg-go/logs"
)

var (
	workerConfigPath     string
	workerServerAddr     string
	workerDefaultRuntime string
	workerListenAddr     string
	workerWorkerID       uint
)

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Start the Leros background worker",
	Long:  `Start the background worker service for processing asynchronous tasks and events.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if workerWorkerID == 0 {
			return fmt.Errorf("worker-id is required")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		runTaskWorker(workerDefaultRuntime)
	},
}

func init() {
	workerCmd.PersistentFlags().StringVar(&workerConfigPath, "config", "", "Configuration file path")
	workerCmd.PersistentFlags().StringVar(&workerServerAddr, "server-addr", "127.0.0.1:8080", "Server address for WebSocket connection")
	workerCmd.PersistentFlags().StringVar(&workerListenAddr, "listen-addr", ":8081", "Worker MCP server listen address for runtime bootstrap")
	workerCmd.PersistentFlags().UintVar(&workerWorkerID, "worker-id", 0, "Worker ID for configuration retrieval")
	workerCmd.PersistentFlags().StringVar(&workerDefaultRuntime, "default-runtime", "", "Default agent runtime kind, for example leros, claude, or codex")
	rootCmd.AddCommand(workerCmd)
}

func loadWorkerConfig() (*config.WorkerConfig, error) {
	cfg := &config.WorkerConfig{}
	if workerConfigPath != "" {
		err := LoadYamlLocalFile(workerConfigPath, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to load config from %s: %w", workerConfigPath, err)
		}
	}
	if workerWorkerID != 0 {
		cfg.WorkerID = workerWorkerID
		logs.Infof("Using worker ID from flag: %d", workerWorkerID)
	}
	if strings.TrimSpace(workerServerAddr) != "" {
		cfg.ServerAddr = workerServerAddr
		logs.Infof("Using server address from flag: %s", workerServerAddr)
	}

	return cfg, nil
}

func runTaskWorker(defaultRuntime string) {
	cfg, err := loadWorkerConfig()
	if err != nil {
		logs.Fatalf("Failed to load config: %v", err)
		return
	}
	if err := validateTaskWorkerConfig(cfg); err != nil {
		logs.Fatalf("Invalid worker config: %v", err)
		return
	}

	if cfg.Database != nil && cfg.Database.URL != "" {
		db, err := infradb.InitDB(*cfg.Database, cfg.LLM)
		if err != nil {
			logs.Fatalf("Failed to initialize database: %v", err)
			return
		}
		if sqlDB, err := db.DB(); err == nil {
			lifecycle.Std().AddCloseFunc(sqlDB.Close)
		}
		logs.Info("Worker database initialized successfully")
	} else {
		logs.Warn("No database configuration provided for worker")
	}
	identity.Set(cfg.OrgID, cfg.WorkerID)

	mcpServer, err := startWorkerMCPServer(workerListenAddr)
	if err != nil {
		logs.Fatalf("Failed to start worker MCP server: %v", err)
		return
	}

	natsURL := "nats://nats:4222"
	if cfg.NATS != nil && strings.TrimSpace(cfg.NATS.URL) != "" {
		natsURL = cfg.NATS.URL
	}

	bus, err := mq.NewPublisher(natsURL)
	if err != nil {
		logs.Fatalf("Failed to create NATS client: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	if cfg.WriteSafeRoot != "" {
		nodetools.SetWriteSafeRoot(cfg.WriteSafeRoot)
	}
	runtimeService, err := agentruntime.NewService(ctx, agentruntime.Options{
		CLIConfig:      cfg.CLI,
		ToolsEnabled:   true,
		DefaultRuntime: defaultRuntime,
	})
	if err != nil {
		cancel()
		_ = bus.Close()
		logs.Fatalf("Failed to create agent runtime service: %v", err)
		return
	}

	// Bootstrap external CLI engines: sync skills and register MCP
	if cfg.CLI != nil {
		var mcpCfg engines.MCPServerConfig
		if cfg.CLI.MCP != nil {
			mcpCfg = engines.MCPServerConfig{
				URL:         cfg.CLI.MCP.URL,
				BearerToken: cfg.CLI.MCP.BearerToken,
			}
		}
		// Construct MCP URL from worker listen address if not explicitly configured
		if mcpCfg.URL == "" && workerListenAddr != "" {
			// Use the worker's MCP server address
			mcpCfg.URL = "http://" + workerListenAddr + "/v1/mcp"
		}

		// 使用新的分层架构 BootstrapService
		bootstrapSvc := builtin.NewBootstrapService()
		_, err := bootstrapSvc.Bootstrap(ctx, cfg.CLI, builtin.BootstrapOptions{
			SkillsSourceDir: cfg.SkillsDir,
			MCP:             mcpCfg,
		})
		if err != nil {
			logs.Warnf("Bootstrap CLI engines failed: %v", err)
		}
	}

	consumer, err := taskconsumer.New(taskconsumer.Config{
		OrgID:    cfg.OrgID,
		WorkerID: cfg.WorkerID,
	}, bus, bus, runtimeService)
	if err != nil {
		cancel()
		_ = bus.Close()
		logs.Fatalf("Failed to create worker task consumer: %v", err)
		return
	}
	if err := consumer.Start(ctx); err != nil {
		cancel()
		_ = bus.Close()
		logs.Fatalf("Failed to start worker task consumer: %v", err)
		return
	}

	lifecycle.Std().AddCloseFunc(func() error {
		cancel()
		return nil
	})
	lifecycle.Std().AddCloseFunc(func() error {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		return mcpServer.Shutdown(shutdownCtx)
	})
	lifecycle.Std().AddCloseFunc(bus.Close)

	logs.Infof("Agent worker started: org_id=%d worker_id=%d topic=%s", cfg.OrgID, cfg.WorkerID, consumer.TaskTopic())
	lifecycle.Std().WaitExit()
	logs.Info("Agent worker exited")
}

func validateTaskWorkerConfig(cfg *config.WorkerConfig) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if cfg.WorkerID == 0 {
		return fmt.Errorf("worker.worker_id is required")
	}
	if cfg.OrgID == 0 {
		return fmt.Errorf("worker.org_id is required")
	}
	if cfg.Database == nil || strings.TrimSpace(cfg.Database.URL) == "" {
		return fmt.Errorf("worker.database.url is required")
	}
	return nil
}

func startWorkerMCPServer(addr string) (*http.Server, error) {
	if strings.TrimSpace(addr) == "" {
		addr = ":8081"
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}

	r := gin.New()
	r.GET("/health", workerHealth)
	v1 := r.Group("/v1")
	runtimemcp.RegisterRoutes(v1, runtimemcp.NewServer())

	server := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		logs.Infof("Worker MCP server listening on %s", listener.Addr().String())
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logs.Errorf("Worker MCP server stopped unexpectedly: %v", err)
		}
	}()

	return server, nil
}

type workerHealthResponse struct {
	Status   string `json:"status"`
	Healthy  bool   `json:"healthy"`
	OrgID    uint   `json:"org_id"`
	WorkerID uint   `json:"worker_id"`
}

func workerHealth(c *gin.Context) {
	orgID := identity.OrgID()
	workerID := identity.WorkerID()
	healthy := orgID != 0 && workerID != 0

	status := "healthy"
	code := http.StatusOK
	if !healthy {
		status = "unhealthy"
		code = http.StatusServiceUnavailable
	}

	c.JSON(code, workerHealthResponse{
		Status:   status,
		Healthy:  healthy,
		OrgID:    orgID,
		WorkerID: workerID,
	})
}
