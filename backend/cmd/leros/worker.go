package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/engines/builtin"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/mq"
	agentruntime "github.com/insmtx/Leros/backend/internal/runtime"
	runtimemcp "github.com/insmtx/Leros/backend/internal/runtime/mcp"
	"github.com/insmtx/Leros/backend/internal/worker/identity"
	"github.com/insmtx/Leros/backend/internal/worker/router"
	"github.com/insmtx/Leros/backend/internal/worker/taskconsumer"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/spf13/cobra"
	"github.com/ygpkg/yg-go/lifecycle"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"
)

var (
	workerConfigPath     string
	workerServerAddr     string
	workerDefaultRuntime string
	workerListenAddr     string
	workerWorkerID       uint
	workerWorkspaceRoot  string
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
	workerCmd.PersistentFlags().StringVar(&workerListenAddr, "listen-addr", ":8081", "Worker HTTP server listen address (MCP + model router)")
	workerCmd.PersistentFlags().UintVar(&workerWorkerID, "worker-id", 0, "Worker ID for configuration retrieval")
	workerCmd.PersistentFlags().StringVar(&workerWorkspaceRoot, "workspace-root", "", "Default worker workspace root")
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
	if strings.TrimSpace(workerWorkspaceRoot) != "" {
		cfg.WorkspaceRoot = workerWorkspaceRoot
		logs.Infof("Using workspace root from flag: %s", workerWorkspaceRoot)
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
	if err := applyWorkerWorkspaceRoot(cfg); err != nil {
		logs.Fatalf("Invalid worker workspace config: %v", err)
		return
	}

	var db *gorm.DB
	if cfg.Database != nil && cfg.Database.URL != "" {
		db, err = infradb.InitDB(*cfg.Database, cfg.LLM)
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
	identity.Set(identity.Profile{
		OrgID:    cfg.OrgID,
		WorkerID: cfg.WorkerID,
		// ServerAddr is the control-plane host:port, for example "127.0.0.1:8080".
		ServerAddr: cfg.ServerAddr,
		// WorkerAddr is the worker HTTP service address, for example ":8081" or "127.0.0.1:8081".
		WorkerAddr: workerListenAddr,
	})

	// Setup MCP auth token before starting HTTP server so /v1/mcp uses the configured value.
	if cfg.CLI != nil && cfg.CLI.MCP != nil {
		runtimemcp.SetAuthToken(cfg.CLI.MCP.BearerToken)
	}

	httpServer, err := startWorkerHTTPServer(workerListenAddr)
	if err != nil {
		logs.Fatalf("Failed to start worker HTTP server: %v", err)
		return
	}

	natsURL := "nats://nats:4222"
	if cfg.NATS != nil && strings.TrimSpace(cfg.NATS.URL) != "" {
		natsURL = cfg.NATS.URL
	}

	bus, err := mq.NewNATS(natsURL)
	if err != nil {
		logs.Fatalf("Failed to create NATS client: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
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
			mcpCfg.URL = buildWorkerMCPURL(workerListenAddr)
		}

		// 使用新的分层架构 BootstrapService
		bootstrapSvc := builtin.NewBootstrapService()
		_, err := bootstrapSvc.Bootstrap(ctx, cfg.CLI, builtin.BootstrapOptions{
			MCP: mcpCfg,
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
		return httpServer.Shutdown(shutdownCtx)
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

func applyWorkerWorkspaceRoot(cfg *config.WorkerConfig) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	root := strings.TrimSpace(cfg.WorkspaceRoot)
	if root == "" {
		return nil
	}
	if err := os.Setenv(leros.EnvWorkspaceRoot, root); err != nil {
		return fmt.Errorf("set %s: %w", leros.EnvWorkspaceRoot, err)
	}
	logs.Infof("Using workspace root from config: %s", root)
	return nil
}

func startWorkerHTTPServer(addr string) (*http.Server, error) {
	if strings.TrimSpace(addr) == "" {
		addr = ":8081"
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}

	r := router.SetupRouter()

	server := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		logs.Infof("Worker HTTP server listening on %s", listener.Addr().String())
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logs.Errorf("Worker HTTP server stopped unexpectedly: %v", err)
		}
	}()

	return server, nil
}

func buildWorkerMCPURL(listenAddr string) string {
	addr := strings.TrimSpace(listenAddr)
	if addr == "" {
		addr = ":8081"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/") + "/v1/mcp"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	return "http://" + addr + "/v1/mcp"
}
