package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ygpkg/yg-go/logs"
	"go.uber.org/zap/zapcore"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/pkg/leros"
)

const (
	envServerURL            = "LEROS_SERVER_URL"
	envServerAddr           = "LEROS_SERVER_ADDR"
	envAuthToken            = "LEROS_AUTH_TOKEN"
	envWorkerBootstrapToken = "LEROS_WORKER_BOOTSTRAP_TOKEN"
	envOrgID                = "LEROS_ORG_ID"
	envWorkerID             = "LEROS_WORKER_ID"
	envDev                  = "LEROS_DEV"

	defaultServerAddr    = "127.0.0.1:8080"
	defaultCLIConfigDir  = ".leros"
	defaultCLIConfigFile = "config.yaml"
)

var (
	cliConfig     *config.WorkerConfig
	cliConfigPath string
)

// cliServerAddr 返回当前有效的服务端地址。
func cliServerAddr() string {
	if cliConfig != nil && cliConfig.ServerAddr != "" {
		return cliConfig.ServerAddr
	}
	return defaultServerAddr
}

// cliAuthToken 返回当前有效的认证 token。
func cliAuthToken() string {
	if cliConfig != nil {
		return cliConfig.AuthToken
	}
	return ""
}

func defaultCLIConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", defaultCLIConfigDir, defaultCLIConfigFile)
	}
	return filepath.Join(home, defaultCLIConfigDir, defaultCLIConfigFile)
}

func loadCLIConfig(path string) *config.WorkerConfig {
	cfg := &config.WorkerConfig{}
	if err := LoadYamlLocalFile(path, cfg); err != nil {
		return cfg
	}
	logs.Debugf("Loaded CLI config from: %s", path)
	return cfg
}

// applyEnvOverrides 用环境变量覆盖配置文件中的值。
func applyEnvOverrides(cfg *config.WorkerConfig) {
	if v := os.Getenv(envOrgID); v != "" {
		if orgID, err := strconv.ParseUint(v, 10, 64); err == nil {
			cfg.OrgID = uint(orgID)
		}
	}
	if v := os.Getenv(envWorkerID); v != "" {
		if workerID, err := strconv.ParseUint(v, 10, 64); err == nil {
			cfg.WorkerID = uint(workerID)
		}
	}
	if v := os.Getenv(envServerAddr); v != "" {
		cfg.ServerAddr = v
	}
	if v := os.Getenv(envServerURL); v != "" {
		cfg.ServerAddr = v
	}
	if v := os.Getenv(envAuthToken); v != "" {
		cfg.AuthToken = v
	}
	if v := os.Getenv(envWorkerBootstrapToken); v != "" {
		cfg.BootstrapToken = v
	}
}

func applyWorkspaceRoot(cfg *config.WorkerConfig) {
	if os.Getenv(leros.EnvWorkspaceRoot) != "" {
		return
	}
	if cfg != nil && strings.TrimSpace(cfg.WorkspaceRoot) != "" {
		os.Setenv(leros.EnvWorkspaceRoot, cfg.WorkspaceRoot)
		return
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		os.Setenv(leros.EnvWorkspaceRoot, filepath.Join(home, ".leros"))
	}
}

func resolveLogLevel(level string) (zapcore.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "":
		if os.Getenv(envDev) == "true" || os.Getenv(envDev) == "1" {
			return zapcore.DebugLevel, true
		}
		return zapcore.InfoLevel, true
	case "debug":
		return zapcore.DebugLevel, true
	case "info":
		return zapcore.InfoLevel, true
	case "warn":
		return zapcore.WarnLevel, true
	case "error":
		return zapcore.ErrorLevel, true
	default:
		return zapcore.InfoLevel, false
	}
}

func applyLogLevel(level string) {
	parsedLevel, valid := resolveLogLevel(level)
	logs.SetLevel(parsedLevel)
	if !valid {
		logs.Warnf("Invalid log.level %q; using default level info", level)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "leros",
		Short: "Leros command line interface",
		Long: `Leros CLI — manage the Leros agent platform from the command line.

Core commands:
  server     Start the HTTP API server
  worker     Start a background task worker
  chat       Start an interactive chat session with a running server
  session    List and inspect sessions
  project    Manage projects
  task       Manage tasks

Examples:
  leros server --config config.yaml
  leros server --dev                     # development mode (auto-config)
  leros worker --worker-id 1 --default-runtime leros
  leros worker claude --worker-id 1
  leros chat                             # interactive chat (server must be running)
  leros chat "Hello, what can you do?"   # one-shot message
  leros session ls --status active --limit 10
  leros session get <session-id>
  leros project ls
  leros task ls --project-id <id>

Authentication:
  leros login
  leros logout

Use "leros [command] --help" for more information about a command.`,
		Args: cobra.ArbitraryArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			path := cliConfigPath
			if path == "" {
				path = defaultCLIConfigPath()
			}
			cliConfig = loadCLIConfig(path)
			applyEnvOverrides(cliConfig)
			applyWorkspaceRoot(cliConfig)
			applyLogLevel(cliConfig.Log.Level)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unknown command %q for %q", args[0], cmd.Name())
			}
			return cmd.Help()
		},
		SilenceUsage:  false,
		SilenceErrors: false,
	}

	root.Flags().Var(&configPathValue{&cliConfigPath}, "config", "CLI config file path")

	root.SetHelpCommand(&cobra.Command{
		Use:    "__help_disabled",
		Hidden: true,
	})
	root.SetUsageTemplate(`Usage:{{if .Runnable}}
  {{.CommandPath}} [options]{{if .HasAvailableSubCommands}} [command]{{end}}{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Commands) 1}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Options:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Options:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`)
	registerCommands(root)
	return root
}

// configPathValue 将 --config 参数显示为 <path> 而非 string。
type configPathValue struct {
	s *string
}

func (v *configPathValue) String() string {
	if v.s == nil {
		return ""
	}
	return *v.s
}

func (v *configPathValue) Set(s string) error {
	*v.s = s
	return nil
}

func (v *configPathValue) Type() string {
	return "<file>"
}
