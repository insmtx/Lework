package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

// version is the unified Lework build version. Release builds may override it
// with -ldflags "-X main.version=...". It is reported during agent registration
// and by the agent version command.
var version = "dev"

// newAgentCommand builds the node-agent command. The agent is a first-class
// subcommand of the single lework binary: invoking it starts the daemon, while
// `lework agent version` reports the build without starting the process.
func newAgentCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run the Lework node agent",
		Long: `Run the Lework node agent.

The agent dials the control plane over an outbound WebSocket, manages the local
OpenCode service, and relays sessions, events, and permission decisions. It
never listens for inbound connections.

Examples:
  lework agent --config /etc/lework/agent.yaml
  lework agent version`,
		Version: version,
		Args:    cobra.NoArgs,
		// The daemon has no interactive shell; keep the command surface to
		// agent/version instead of shipping generated shell completion scripts.
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgent(cmd.Context(), configPath)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", resolveConfigPath(), "path to lework.yaml or agent.yaml")
	cmd.AddCommand(newAgentVersionCommand())
	cmd.SetVersionTemplate("lework agent {{.Version}}\n")
	return cmd
}

// newAgentVersionCommand reports the build version in a stable, script-friendly
// form. It writes to the command's output stream so tests can capture it.
func newAgentVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the agent version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "lework agent %s\n", version)
			return err
		},
	}
}

// defaultConfigPath returns the conventional agent.yaml location for the
// running platform. The installation contract is documented in
// docs/design/2026-11-01-opencode-control-plane-technical-architecture.md.
func defaultConfigPath() string {
	return defaultConfigPathFor(runtime.GOOS)
}

// userConfigDir is a seam so the no-home fallback of defaultConfigPathFor can be
// tested on every platform. Production always uses os.UserConfigDir.
var userConfigDir = os.UserConfigDir

// defaultConfigPathFor resolves the platform default. It is split out from
// defaultConfigPath so every platform's path can be tested on any host.
func defaultConfigPathFor(goos string) string {
	switch goos {
	case "linux":
		return "/etc/lework/agent.yaml"
	case "darwin":
		// os.UserConfigDir is ~/Library/Application Support on macOS.
		dir, err := userConfigDir()
		if err != nil {
			return "agent.yaml"
		}
		return filepath.Join(dir, "Lework", "agent.yaml")
	case "windows":
		dir := os.Getenv("ProgramData")
		if dir == "" {
			return `C:\ProgramData\Lework\agent.yaml`
		}
		return filepath.Join(dir, "Lework", "agent.yaml")
	default:
		dir, err := userConfigDir()
		if err != nil {
			return "agent.yaml"
		}
		return filepath.Join(dir, "Lework", "agent.yaml")
	}
}
