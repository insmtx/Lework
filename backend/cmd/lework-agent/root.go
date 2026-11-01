package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is the agent build version. Release builds may override it with
// -ldflags "-X main.version=...". It is reported during registration and by the
// version command.
var version = "dev"

// newRootCommand builds the lework-agent command tree. It exists as a
// constructor rather than a package-level variable so tests get a fresh command
// with clean flag state on every call.
func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "lework-agent",
		Short: "Lework node agent",
		Long: `Lework node agent.

The agent dials the control plane over an outbound WebSocket, manages the local
OpenCode service, and relays sessions, events, and permission decisions. It
never listens for inbound connections.

Examples:
  lework-agent run --config /etc/lework/agent.yaml
  lework-agent version`,
		Version: version,
		Args:    cobra.NoArgs,
		// The daemon has no interactive shell; keep the command surface to
		// run/version instead of shipping generated shell completion scripts.
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	root.AddCommand(newRunCommand())
	root.AddCommand(newVersionCommand())
	return root
}

// newVersionCommand reports the build version in a stable, script-friendly
// form. It writes to the command's output stream so tests can capture it.
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the agent version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "lework-agent %s\n", version)
			return err
		},
	}
}
