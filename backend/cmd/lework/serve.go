package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/insmtx/lework/backend/internal/controlplane"
	controlplaneconfig "github.com/insmtx/lework/backend/internal/controlplane/config"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// envEnrollmentToken 是注册令牌的环境变量；命令行参数优先于环境变量。
const envEnrollmentToken = "LEWORK_ENROLLMENT_TOKEN"

// newServeCommand builds the local control-plane command.
//
// It is a first-class lework subcommand and a standalone process: unlike the
// legacy CLI, it does not read the worker config file or let it rewrite the
// workspace environment. Signal handling stays here; the controlplane package
// only knows how to run until its context ends.
func newServeCommand() *cobra.Command {
	cfg := controlplaneconfig.Default()
	enrollmentToken := os.Getenv(envEnrollmentToken)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local Lework control plane",
		Long: `Start the local Lework control plane.

The control plane listens on a loopback address, accepts node agents over an
outbound WebSocket, and serves the browser API. Node agents enroll with the
token passed via --enrollment-token or LEWORK_ENROLLMENT_TOKEN; the token is
never logged.

Examples:
  lework serve --enrollment-token <token>
  lework serve --listen 127.0.0.1:8787 --db ~/.config/Lework/controlplane.db`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runControlPlane(cmd.Context(), cfg, enrollmentToken)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&cfg.ListenAddr, "listen", controlplaneconfig.DefaultListenAddr, "HTTP listen address (loopback only)")
	flags.StringVar(&cfg.DBPath, "db", controlplaneconfig.DefaultDBPath(), "path to the control plane SQLite database")
	flags.StringVar(&enrollmentToken, "enrollment-token", enrollmentToken,
		"node enrollment token (defaults to "+envEnrollmentToken+")")
	flags.DurationVar(&cfg.RequestTimeout, "request-timeout", controlplaneconfig.DefaultRequestTimeout,
		"timeout for a single node request")
	flags.DurationVar(&cfg.HeartbeatTimeout, "heartbeat-timeout", controlplaneconfig.DefaultHeartbeatTimeout,
		"idle time after which a node is considered offline")

	// The token is a secret: keep the value initialized from the environment
	// but clear the printed default so help and completion output never show it.
	if flag := flags.Lookup("enrollment-token"); flag != nil {
		flag.DefValue = ""
	}

	return cmd
}

// runControlPlane validates the configuration, opens the store, and serves
// until the context is canceled by a signal.
func runControlPlane(parent context.Context, cfg controlplaneconfig.Config, enrollmentToken string) error {
	cfg.EnrollmentToken = enrollmentToken
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("control plane config: %w", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open control plane store: %w", err)
	}

	srv, err := controlplane.New(cfg, st)
	if err != nil {
		_ = st.Close()
		return fmt.Errorf("start control plane: %w", err)
	}
	defer func() { _ = srv.Close() }()

	ctx, stopSignals := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	return srv.Run(ctx)
}
