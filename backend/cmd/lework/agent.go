package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	qrterminal "github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"

	"github.com/insmtx/lework/backend/internal/agentd/pair"
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
	var opts agentConnectOptions

	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run the Lework node agent",
		Long: `Run the Lework node agent.

The agent dials the control plane over an outbound WebSocket, manages the local
OpenCode service, and relays sessions, events, and permission decisions. It
never listens for inbound connections.

When no node credential exists and no enrollment token is configured, the agent
starts pairing: it prints a QR code and a short code to add this machine to an
account. Use --code to present a code minted in the web UI instead (for
machines you cannot reach with a camera).

Examples:
  lework agent --config /etc/lework/agent.yaml
  lework agent --code ABC12
  lework agent claim
  lework agent version`,
		Version: version,
		Args:    cobra.NoArgs,
		// The daemon has no interactive shell; keep the command surface to
		// agent/version/claim instead of shipping generated shell completion
		// scripts.
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 互斥校验先于读配置：两个来源冲突是命令行错误，不该被一个
			// 恰好缺失的 agent.yaml 掩盖。
			if err := opts.validate(); err != nil {
				return err
			}
			return runAgent(cmd.Context(), configPath, opts)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", resolveConfigPath(), "path to lework.yaml or agent.yaml")
	cmd.Flags().StringVar(&opts.Code, "code", "",
		"pair using a one-time code minted in the web UI (for machines you cannot reach with a camera)")
	cmd.Flags().BoolVar(&opts.NoQR, "no-qr", false, "do not print the pairing QR code; print the link and short code only")
	cmd.Flags().StringVar(&opts.CredentialPath, "credential-file", "",
		"path to the node credential file (0600); defaults to credential_file in the config or next to it")
	cmd.AddCommand(newAgentVersionCommand())
	cmd.AddCommand(newAgentClaimCommand())
	cmd.SetVersionTemplate("lework agent {{.Version}}\n")
	return cmd
}

// newAgentClaimCommand reprints the pending pairing code from the claim file.
//
// 它刻意不拨号、不启动 OpenCode：二维码可能早就被日志刷走，用户要的只是
// 「把终端上那张码再显示一次」。真正维持待定连接的是仍在运行的 agent。
func newAgentClaimCommand() *cobra.Command {
	var configPath string
	var credentialPath string
	var noQR bool

	cmd := &cobra.Command{
		Use:   "claim",
		Short: "Reprint the pending node pairing code",
		Long: `Reprint the pending pairing code written by a running agent.

The claim file lives next to the node credential (node.claim) and expires with
the control plane's pending claim. Run ` + "`lework agent`" + ` again to start a new
pairing if the code has expired.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentClaim(cmd.OutOrStdout(), configPath, credentialPath, noQR)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", resolveConfigPath(), "path to lework.yaml or agent.yaml")
	cmd.Flags().StringVar(&credentialPath, "credential-file", "",
		"path to the node credential file; the claim file lives in the same directory")
	cmd.Flags().BoolVar(&noQR, "no-qr", false, "do not print the QR code; print the link and code only")
	return cmd
}

// runAgentClaim reads node.claim and prints the QR (terminal only), link, short
// code, and expiry. A non-terminal writer still gets the text: claim is an
// explicit terminal command, not a background daemon. noQR suppresses only the
// QR bitmap, so links stay usable from terminals that cannot render it.
func runAgentClaim(out io.Writer, configPath, credentialPath string, noQR bool) error {
	claimPath := pair.ClaimPath(resolveAgentCredentialPath(configPath, credentialPath))
	claim, ok, err := pair.ReadClaim(claimPath)
	if err != nil {
		return err
	}
	if !ok {
		if claim.Code == "" && claim.ExpiresAt.IsZero() {
			return fmt.Errorf("no pending pairing found at %s; run lework agent to start one", claimPath)
		}
		return fmt.Errorf("pairing code at %s expired at %s; run lework agent again to start a new one",
			claimPath, claim.ExpiresAt.Local().Format(time.RFC3339))
	}

	fmt.Fprintln(out, "Pair this node with your account:")
	if claim.Link != "" && !noQR && isTerminal(out) {
		qrterminal.GenerateWithConfig(claim.Link, qrterminal.Config{
			Level:      qrterminal.L,
			Writer:     out,
			HalfBlocks: true,
		})
	}
	if claim.Link != "" {
		fmt.Fprintf(out, "Link: %s\n", claim.Link)
	}
	fmt.Fprintf(out, "Code: %s\n", claim.Code)
	fmt.Fprintf(out, "Expires at: %s\n", claim.ExpiresAt.Local().Format(time.RFC3339))
	return nil
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
