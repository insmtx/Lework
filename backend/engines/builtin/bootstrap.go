// Package builtin 提供外部 CLI 引擎的编排服务。
// 包含分层架构: Skill 同步 → CLI 发现 → MCP 注册
package builtin

import (
	"context"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/engines"
	"github.com/ygpkg/yg-go/logs"
)

// ============================================================
// Bootstrap Options
// ============================================================

// BootstrapOptions controls host-level CLI bootstrap side effects.
type BootstrapOptions struct {
	SkillsSourceDir string
	MCP             engines.MCPServerConfig
}

// ============================================================
// Layer 1: Bootstrap Service (编排层)
// ============================================================

// BootstrapService 负责协调整个引擎启动流程（native + 外部 CLI）。
type BootstrapService struct {
	cliDiscovery *CLIDiscoveryService
	skillSync    *SkillSyncService
	mcpRegistrar *MCPRegistrarService
}

// NewBootstrapService 创建 BootstrapService 实例。
func NewBootstrapService() *BootstrapService {
	return &BootstrapService{
		cliDiscovery: NewCLIDiscoveryService(),
		skillSync:    NewSkillSyncService(),
		mcpRegistrar: NewMCPRegistrarService(),
	}
}

// GetSkillDirs 返回所有已发现 CLI 的 skill 目录。
func (s *BootstrapService) GetSkillDirs() []string {
	if s == nil || s.cliDiscovery == nil {
		return nil
	}
	return s.cliDiscovery.GetSkillDirs()
}

// Bootstrap 执行完整的引擎启动流程。
// 内置 skill 文件同步已在 server/worker 各自的启动流程中完成；
// Bootstrap 负责 CLI 发现、workspace skills 到外部 CLI 的 symlink 同步、以及 MCP 注册。
func (s *BootstrapService) Bootstrap(ctx context.Context, cfg *config.CLIEnginesConfig, opts BootstrapOptions) (*config.CLIEnginesConfig, error) {
	if cfg == nil {
		cfg = &config.CLIEnginesConfig{}
	}

	var bootstrapErr error

	// === Layer 2: CLI Discovery ===
	logs.Info("Starting CLI discovery...")
	clis := s.cliDiscovery.Discover()

	hasAvailable := false
	for _, c := range clis {
		if c.Installed {
			hasAvailable = true
			logs.Infof("  - %s: %s (v%s) @ %s", c.DisplayName, c.Name, c.Version, c.Path)
		} else {
			logs.Infof("  - %s: not installed (install: %s)", c.DisplayName, c.InstallCmd)
		}
	}

	if !hasAvailable {
		logs.Warn("No CLI engines available")
		return cfg, bootstrapErr
	}

	// 设置默认引擎
	if cfg.Default == "" {
		if defaultName := engines.GetDefaultEngineName(clis); defaultName != "" {
			cfg.Default = defaultName
			logs.Infof("Auto-detected default engine: %s", defaultName)
		}
	}

	// Step 2: 从 workspace skills 同步到各 CLI 目录。
	skillDirs := s.cliDiscovery.GetSkillDirs()
	if len(skillDirs) > 0 {
		logs.Infof("Syncing skills from Leros workspace skills to %d external CLI directories", len(skillDirs))
		if err := s.skillSync.SyncToExternal(skillDirs); err != nil {
			bootstrapErr = appendError(bootstrapErr, err)
			logs.Warnf("Sync skills to external CLI failed: %v", err)
		} else {
			logs.Info("Skills synced to external CLI directories")
		}
	}

	// === Layer 4: MCP Registration ===
	logs.Info("Registering MCP servers for available CLIs...")
	if err := s.mcpRegistrar.RegisterAll(clis, opts.MCP); err != nil {
		bootstrapErr = appendError(bootstrapErr, err)
		logs.Warnf("Register MCP failed: %v", err)
	}

	logs.Info("CLI bootstrap complete")
	return cfg, bootstrapErr
}

// appendError 辅助函数，用于追加错误。
func appendError(errs, newErr error) error {
	if errs == nil {
		return newErr
	}
	if newErr == nil {
		return errs
	}
	return &multiError{errors: []error{errs, newErr}}
}

type multiError struct {
	errors []error
}

func (e *multiError) Error() string {
	return "multiple errors occurred"
}

// ============================================================
// Layer 2: CLI Discovery Service (发现层)
// ============================================================

// CLIDiscoveryService 负责发现系统中已安装的外部 CLI。
type CLIDiscoveryService struct {
	discovered []engines.CLIToolStatus
	engines    map[string]engines.Engine
}

// NewCLIDiscoveryService 创建 CLIDiscoveryService 实例。
func NewCLIDiscoveryService() *CLIDiscoveryService {
	return &CLIDiscoveryService{
		engines: make(map[string]engines.Engine),
	}
}

// Discover 发现系统中已安装的外部 CLI。
func (s *CLIDiscoveryService) Discover() []engines.CLIToolStatus {
	s.discovered = engines.DiscoverAvailableCLI()

	// 为已安装的 CLI 创建引擎实例
	for _, status := range s.discovered {
		if !status.Installed {
			continue
		}
		engine, err := newEngine(status.Name, status.Path)
		if err != nil {
			logs.Warnf("Failed to create engine for %s: %v", status.Name, err)
			continue
		}
		s.engines[status.Name] = engine
	}

	return s.discovered
}

// GetSkillDirs 获取所有已安装 CLI 的 skill 目录。
func (s *CLIDiscoveryService) GetSkillDirs() []string {
	var dirs []string
	for _, engine := range s.engines {
		dir := engine.GetSkillDir()
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// GetEngine 获取指定名称的引擎。
func (s *CLIDiscoveryService) GetEngine(name string) (engines.Engine, bool) {
	engine, ok := s.engines[name]
	return engine, ok
}

// GetEngines 返回所有已创建的引擎映射。
func (s *CLIDiscoveryService) GetEngines() map[string]engines.Engine {
	return s.engines
}

// ============================================================
// Layer 3: Skill Sync Service (同步层)
// ============================================================

// SkillSyncService 负责技能目录的同步。
type SkillSyncService struct{}

// NewSkillSyncService 创建 SkillSyncService 实例。
func NewSkillSyncService() *SkillSyncService {
	return &SkillSyncService{}
}

// SyncToExternal 将 workspace skills 同步到外部 CLI 目录。
func (s *SkillSyncService) SyncToExternal(dirs []string) error {
	return engines.ReconcileExternalSkillLinks(dirs)
}

// ============================================================
// Layer 4: MCP Registrar Service (注册层)
// ============================================================

// MCPRegistrarService 负责 MCP 服务器的注册。
type MCPRegistrarService struct{}

// NewMCPRegistrarService 创建 MCPRegistrarService 实例。
func NewMCPRegistrarService() *MCPRegistrarService {
	return &MCPRegistrarService{}
}

// RegisterAll 为所有已安装的 CLI 注册 MCP 服务器。
func (s *MCPRegistrarService) RegisterAll(clis []engines.CLIToolStatus, cfg engines.MCPServerConfig) error {
	if cfg.URL == "" {
		logs.Debug("No MCP URL provided, skipping registration")
		return nil
	}

	cfg = engines.NormalizeMCPServerConfig(cfg)
	var errs error

	for _, cli := range clis {
		if !cli.Installed {
			continue
		}

		engine, err := newEngine(cli.Name, cli.Path)
		if err != nil {
			errs = appendError(errs, err)
			continue
		}

		if err := engine.RegisterMCP(context.Background(), cfg); err != nil {
			errs = appendError(errs, err)
			logs.Warnf("Failed to register MCP for %s: %v", cli.Name, err)
			continue
		}
		logs.Infof("Registered MCP server for %s", cli.Name)
	}

	return errs
}
