package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	clauderuntime "github.com/insmtx/Leros/backend/agent/runtime/claude"
	codexruntime "github.com/insmtx/Leros/backend/agent/runtime/codex"
	opencoderuntime "github.com/insmtx/Leros/backend/agent/runtime/opencode"
	"github.com/insmtx/Leros/backend/agent/runtime/provider"
	"github.com/insmtx/Leros/backend/config"
	agentruntime "github.com/insmtx/Leros/backend/internal/assistant/bootstrap"
	builtin "github.com/insmtx/Leros/backend/internal/assistant/bootstrap/builtin"
	skilllinks "github.com/insmtx/Leros/backend/internal/assistant/bootstrap/skilllinks"
	"github.com/insmtx/Leros/backend/internal/infra/mq"
	localmemory "github.com/insmtx/Leros/backend/internal/memory/local"
	modelrouter "github.com/insmtx/Leros/backend/internal/modelrouter"
	"github.com/insmtx/Leros/backend/internal/worker/command"
	"github.com/insmtx/Leros/backend/internal/worker/command/interaction"
	"github.com/insmtx/Leros/backend/internal/worker/command/run"
	"github.com/insmtx/Leros/backend/internal/worker/command/skill"
	"github.com/insmtx/Leros/backend/internal/worker/identity"
	"github.com/insmtx/Leros/backend/internal/worker/router"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/spf13/cobra"
	"github.com/ygpkg/yg-go/lifecycle"
	"github.com/ygpkg/yg-go/logs"
	"gopkg.in/yaml.v2"
)

var (
	workerDefaultRuntime string
	workerListenAddr     string
	workerOrgID          uint
	workerWorkerID       uint
	workerServerAddr     string
	workerAuthToken      string
	workerBootstrapToken string
	workerWorkspaceRoot  string
)

func newWorkerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Start the Leros background worker",
		Long:  `Start the background worker service for processing asynchronous tasks and events.`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runTaskWorker(workerDefaultRuntime)
		},
	}

	cmd.PersistentFlags().StringVar(&workerListenAddr, "listen-addr", ":8081", "Worker HTTP server listen address (MCP + model router)")
	cmd.PersistentFlags().UintVar(&workerOrgID, "org-id", 0, "Organization ID (overrides config file)")
	cmd.PersistentFlags().UintVar(&workerWorkerID, "worker-id", 0, "Worker ID (overrides config file)")
	cmd.PersistentFlags().StringVar(&workerServerAddr, "server-addr", "", "Leros server address (overrides config file)")
	cmd.PersistentFlags().StringVar(&workerAuthToken, "auth-token", "", "Worker auth token for server API calls (overrides config file)")
	cmd.PersistentFlags().StringVar(&workerBootstrapToken, "bootstrap-token", "", "Worker bootstrap token used to request an auth token (overrides config file)")
	cmd.PersistentFlags().StringVar(&workerWorkspaceRoot, "workspace-root", "", "Worker workspace root (overrides config file)")
	cmd.PersistentFlags().StringVar(&workerDefaultRuntime, "default-runtime", "", "Default agent runtime kind, for example leros, claude, codex, or opencode")
	cmd.AddCommand(newCodexWorkerCommand())
	cmd.AddCommand(newClaudeWorkerCommand())
	cmd.AddCommand(newOpenCodeWorkerCommand())
	return cmd
}

func newClaudeWorkerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "claude",
		Short: "Start a standalone task worker backed by the Claude runtime",
		Long:  `Start a standalone Leros worker that subscribes to org.{org_id}.worker.{worker_id}.task and executes agent.run tasks through the Claude agent runtime.`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runTaskWorker(clauderuntime.Kind)
		},
	}
}

func newCodexWorkerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "codex",
		Short: "Start a standalone task worker backed by the Codex runtime",
		Long:  `Start a standalone Leros worker that subscribes to org.{org_id}.worker.{worker_id}.task and executes agent.run tasks through the Codex agent runtime.`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runTaskWorker(codexruntime.Kind)
		},
	}
}

func newOpenCodeWorkerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "opencode",
		Short: "Start a standalone task worker backed by the OpenCode runtime",
		Long:  `Start a standalone Leros worker that subscribes to org.{org_id}.worker.{worker_id}.task and executes agent.run tasks through the OpenCode agent runtime.`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runTaskWorker(opencoderuntime.Kind)
		},
	}
}

func loadWorkerConfig() (*config.WorkerConfig, error) {
	cfg := cliConfig
	if cfg == nil {
		cfg = &config.WorkerConfig{}
	}

	if workerWorkerID != 0 {
		cfg.WorkerID = workerWorkerID
	}
	if workerOrgID != 0 {
		cfg.OrgID = workerOrgID
	}
	if strings.TrimSpace(workerServerAddr) != "" {
		cfg.ServerAddr = workerServerAddr
	}
	if strings.TrimSpace(workerAuthToken) != "" {
		cfg.AuthToken = workerAuthToken
	}
	if strings.TrimSpace(workerBootstrapToken) != "" {
		cfg.BootstrapToken = workerBootstrapToken
	}
	if strings.TrimSpace(workerWorkspaceRoot) != "" {
		cfg.WorkspaceRoot = workerWorkspaceRoot
	}
	return cfg, nil
}

func saveEffectiveConfig(cfg *config.WorkerConfig) {
	if cfg == nil {
		return
	}
	targetPath := defaultCLIConfigPath()

	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logs.Warnf("Cannot create CLI config dir %s: %v", dir, err)
		return
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		logs.Warnf("Failed to marshal effective config: %v", err)
		return
	}

	tmpPath := targetPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		logs.Warnf("Failed to write effective config to %s: %v", tmpPath, err)
		return
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		logs.Warnf("Failed to rename %s -> %s: %v", tmpPath, targetPath, err)
		return
	}
	logs.Infof("Effective config persisted to %s", targetPath)
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
	if err := ensureWorkerAuthToken(context.Background(), cfg); err != nil {
		logs.Fatalf("Failed to prepare worker auth token: %v", err)
		return
	}
	go saveEffectiveConfig(cfg)
	// root.go PersistentPreRunE 已从配置文件设置了 LEROS_WORKSPACE_ROOT。
	// 这里仅在 CLI flag --workspace-root 显式覆盖时重新设置，确保子进程（如 leros skill list）继承正确的值。
	if strings.TrimSpace(workerWorkspaceRoot) != "" {
		os.Setenv(leros.EnvWorkspaceRoot, workerWorkspaceRoot)
	}
	if strings.TrimSpace(cfg.ServerAddr) != "" {
		os.Setenv(envServerAddr, cfg.ServerAddr)
	}
	if strings.TrimSpace(cfg.AuthToken) != "" {
		os.Setenv(envAuthToken, cfg.AuthToken)
	}
	if _, err := leros.EnsureStateDir(); err != nil {
		logs.Fatalf("Failed to ensure state dir: %v", err)
		return
	}
	if err := skilllinks.SyncToLerosDir(""); err != nil {
		logs.Warnf("Sync worker built-in skills failed: %v", err)
	}
	identity.Set(identity.Profile{
		OrgID:    cfg.OrgID,
		WorkerID: cfg.WorkerID,
		// ServerAddr is the control-plane host:port, for example "127.0.0.1:8080".
		ServerAddr: cfg.ServerAddr,
		// WorkerAddr is the worker HTTP service address, for example ":8081" or "127.0.0.1:8081".
		WorkerAddr: workerListenAddr,
		AppKey:     cfg.AppKey,
	})
	var mcpToken string
	if cfg.CLI != nil && cfg.CLI.MCP != nil {
		mcpToken = cfg.CLI.MCP.BearerToken
	}
	modelStore := modelrouter.NewModelStore()
	httpServer, err := startWorkerHTTPServer(workerListenAddr, modelStore, mcpToken)
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
	var cliSkillDirs []string
	// Bootstrap engines: always sync built-in skills to .leros/skills (serves native engine).
	// If CLI engines are configured, also sync symlinks.
	{
		var cliCfg *config.CLIEnginesConfig
		if cfg.CLI != nil {
			cliCfg = cfg.CLI
		}
		bootstrapSvc := builtin.NewBootstrapService()
		updatedCLICfg, err := bootstrapSvc.Bootstrap(ctx, cliCfg, builtin.BootstrapOptions{})
		if err != nil {
			logs.Warnf("Bootstrap engines failed: %v", err)
		}
		if updatedCLICfg != nil {
			cfg.CLI = updatedCLICfg
		}
		// 默认注入 Leros MCP，确保引擎启动时始终携带业务 MCP 工具（per-run 注入路径）。
		if cfg.CLI != nil && cfg.CLI.MCP == nil && workerListenAddr != "" {
			cfg.CLI.MCP = &config.MCPConfig{
				URL: buildWorkerMCPURL(workerListenAddr),
			}
		}
		cliSkillDirs = bootstrapSvc.GetSkillDirs()
	}
	interactionRouter := provider.NewInteractionRouter()
	memoryStore, err := localmemory.NewStore(localmemory.Options{})
	if err != nil {
		cancel()
		_ = bus.Close()
		logs.Fatalf("Failed to create memory store: %v", err)
		return
	}
	runtimeService, err := agentruntime.NewService(ctx, agentruntime.Options{
		CLIConfig:         cfg.CLI,
		DefaultRuntime:    defaultRuntime,
		CLISkillDirs:      cliSkillDirs,
		GiteaCfg:          cfg.Gitea,
		Env:               cfg.Env,
		InteractionRouter: interactionRouter,
		ModelStore:        modelStore,
		MemoryStore:       memoryStore,
	})
	if err != nil {
		cancel()
		_ = bus.Close()
		logs.Fatalf("Failed to create agent runtime service: %v", err)
		return
	}
	// Use shared leros.db for seq tracking (coexists with provider_session_bindings table).
	seqTrackerPath, err := leros.StateDBPath()
	if err != nil {
		cancel()
		_ = bus.Close()
		logs.Fatalf("Failed to resolve state db path: %v", err)
		return
	}

	runHandler, err := run.New(run.Config{
		OrgID:          cfg.OrgID,
		WorkerID:       cfg.WorkerID,
		Env:            cfg.Env,
		SeqTrackerPath: seqTrackerPath,
	}, bus, runtimeService.AssistantService())
	if err != nil {
		cancel()
		_ = bus.Close()
		logs.Fatalf("Failed to create run handler: %v", err)
		return
	}

	interactionHandler := interaction.New(interactionRouter)

	skillHandler, err := skill.New(bus.Conn())
	if err != nil {
		cancel()
		_ = bus.Close()
		logs.Fatalf("Failed to create skill handler: %v", err)
		return
	}

	dispatcher, err := command.New(command.Config{
		OrgID:    cfg.OrgID,
		WorkerID: cfg.WorkerID,
	}, bus, command.Handlers{
		Run:         runHandler,
		Control:     runHandler,
		Interaction: interactionHandler,
		Skill:       skillHandler,
	})
	if err != nil {
		cancel()
		_ = bus.Close()
		logs.Fatalf("Failed to create command dispatcher: %v", err)
		return
	}

	go func() {
		if err := dispatcher.Run(ctx); err != nil {
			logs.Errorf("Command dispatcher exited with error: %v", err)
			lifecycle.Std().Exit()
		}
	}()

	lifecycle.Std().AddCloseFunc(func() error {
		cancel()
		return nil
	})
	lifecycle.Std().AddCloseFunc(func() error {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		return httpServer.Shutdown(shutdownCtx)
	})
	lifecycle.Std().AddCloseFunc(func() error {
		logs.Info("Shutting down task consumer...")
		if err := runHandler.Close(); err != nil {
			logs.Errorf("Failed to close task consumer: %v", err)
		}
		return nil
	})
	lifecycle.Std().AddCloseFunc(bus.Close)
	logs.Infof("Agent worker started: org_id=%d worker_id=%d topic=%s", cfg.OrgID, cfg.WorkerID, runHandler.RunSubject())
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
	return nil
}

type issueWorkerTokenRequest struct {
	OrgID    uint `json:"org_id"`
	WorkerID uint `json:"worker_id"`
}

type issueWorkerTokenAPIResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		AuthToken string `json:"auth_token"`
		ExpiredAt int64  `json:"expired_at"`
		TokenType string `json:"token_type"`
	} `json:"data"`
}

func ensureWorkerAuthToken(ctx context.Context, cfg *config.WorkerConfig) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if strings.TrimSpace(cfg.AuthToken) != "" {
		return nil
	}
	bootstrapToken := strings.TrimSpace(cfg.BootstrapToken)
	if bootstrapToken == "" {
		return nil
	}
	if strings.TrimSpace(cfg.ServerAddr) == "" {
		return fmt.Errorf("worker.server_addr is required to request auth token")
	}

	token, err := requestWorkerAuthToken(ctx, cfg.ServerAddr, bootstrapToken, cfg.OrgID, cfg.WorkerID)
	if err != nil {
		return err
	}
	cfg.AuthToken = token
	return nil
}

func requestWorkerAuthToken(ctx context.Context, serverAddr, bootstrapToken string, orgID, workerID uint) (string, error) {
	body, err := json.Marshal(issueWorkerTokenRequest{
		OrgID:    orgID,
		WorkerID: workerID,
	})
	if err != nil {
		return "", fmt.Errorf("marshal worker token request: %w", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := workerTokenEndpoint(serverAddr)
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create worker token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Worker-Bootstrap-Token", bootstrapToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request worker token: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read worker token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("worker token request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp issueWorkerTokenAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("decode worker token response: %w", err)
	}
	if apiResp.Code != 0 {
		return "", fmt.Errorf("worker token request failed: %s", apiResp.Message)
	}
	if strings.TrimSpace(apiResp.Data.AuthToken) == "" {
		return "", fmt.Errorf("worker token response missing auth_token")
	}
	return apiResp.Data.AuthToken, nil
}

func workerTokenEndpoint(serverAddr string) string {
	serverAddr = strings.TrimRight(strings.TrimSpace(serverAddr), "/")
	if strings.HasPrefix(serverAddr, "http://") || strings.HasPrefix(serverAddr, "https://") {
		return serverAddr + "/v1/workers/token"
	}
	return fmt.Sprintf("http://%s/v1/workers/token", serverAddr)
}

func startWorkerHTTPServer(
	addr string,
	modelStore *modelrouter.ModelStore,
	mcpToken string,
) (*http.Server, error) {
	if strings.TrimSpace(addr) == "" {
		addr = ":8081"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	r := router.SetupRouter(modelStore, mcpToken)
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
