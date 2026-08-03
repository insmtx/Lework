// Package builtin provides external CLI discovery and default-runtime selection.
package builtin

import (
	"github.com/insmtx/Leros/backend/config"
	runtimecli "github.com/insmtx/Leros/backend/internal/cli"
	"github.com/ygpkg/yg-go/logs"
)

// ============================================================
// Layer 1: Bootstrap Service (编排层)
// ============================================================

// BootstrapService 负责协调整个引擎启动流程（native + 外部 CLI）。
type BootstrapService struct {
	cliDiscovery *CLIDiscoveryService
}

// NewBootstrapService 创建 BootstrapService 实例。
func NewBootstrapService() *BootstrapService {
	return &BootstrapService{
		cliDiscovery: NewCLIDiscoveryService(),
	}
}

// Bootstrap discovers available CLI runtimes and selects a default when needed.
// Skill projection is run-scoped and must not write host-global CLI directories.
func (s *BootstrapService) Bootstrap(cfg *config.CLIEnginesConfig) *config.CLIEnginesConfig {
	if cfg == nil {
		cfg = &config.CLIEnginesConfig{}
	}

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
		logs.Warn("No CLI runtimes available")
		return cfg
	}

	// 设置默认引擎
	if cfg.Default == "" {
		if defaultName := runtimecli.PreferredCLIName(clis); defaultName != "" {
			cfg.Default = defaultName
			logs.Infof("Auto-detected default runtime: %s", defaultName)
		}
	}

	logs.Info("CLI bootstrap complete; Skills are projected per run")
	return cfg
}

// ============================================================
// Layer 2: CLI Discovery Service (发现层)
// ============================================================

// CLIDiscoveryService 负责发现系统中已安装的外部 CLI。
type CLIDiscoveryService struct {
	discovered []runtimecli.CLIToolStatus
}

// NewCLIDiscoveryService 创建 CLIDiscoveryService 实例。
func NewCLIDiscoveryService() *CLIDiscoveryService {
	return &CLIDiscoveryService{}
}

// Discover 发现系统中已安装的外部 CLI。
func (s *CLIDiscoveryService) Discover() []runtimecli.CLIToolStatus {
	s.discovered = runtimecli.DiscoverAvailableCLI()
	return s.discovered
}
