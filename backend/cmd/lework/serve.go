package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/insmtx/lework/backend/internal/controlplane"
	controlplaneconfig "github.com/insmtx/lework/backend/internal/controlplane/config"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

const (
	// envEnrollmentToken 是注册令牌的环境变量；命令行参数优先于环境变量。
	envEnrollmentToken = "LEWORK_ENROLLMENT_TOKEN"

	// envTokenFile 覆盖默认的令牌文件路径；命令行 --token-file 优先于它。
	envTokenFile = "LEWORK_TOKEN_FILE"
)

// defaultTokenFile 返回生效的默认令牌文件路径：环境变量 > 平台默认。
func defaultTokenFile() string {
	if path := os.Getenv(envTokenFile); path != "" {
		return path
	}
	return controlplaneconfig.DefaultTokenPath()
}

// newServeCommand builds the local control-plane command.
//
// It is a first-class lework subcommand and a standalone process: unlike the
// legacy CLI, it does not read the worker config file or let it rewrite the
// workspace environment. Signal handling stays here; the controlplane package
// only knows how to run until its context ends.
func newServeCommand() *cobra.Command {
	cfg := controlplaneconfig.Default()
	enrollmentToken := os.Getenv(envEnrollmentToken)
	tokenFile := defaultTokenFile()

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local Lework control plane",
		Long: `Start the local Lework control plane.

The control plane listens on 127.0.0.1:8787 by default, accepts node agents
over an outbound WebSocket, and serves the browser API. Binding a non-loopback
address exposes the browser API, which then requires the enrollment token.

Node agents enroll with the token passed via --enrollment-token or
LEWORK_ENROLLMENT_TOKEN. When neither is set, the token is read from
--token-file (or LEWORK_TOKEN_FILE) and generated there if missing. The token
is never logged.

Examples:
  lework serve --enrollment-token <token>
  lework serve token
  lework serve --listen 127.0.0.1:8787 --db ~/.config/Lework/controlplane.db`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runControlPlane(cmd.Context(), cfg, enrollmentToken, tokenFile)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&cfg.ListenAddr, "listen", controlplaneconfig.DefaultListenAddr,
		"HTTP listen address (non-loopback requires the enrollment token)")
	flags.StringVar(&cfg.DBPath, "db", controlplaneconfig.DefaultDBPath(), "path to the control plane SQLite database")
	flags.StringVar(&enrollmentToken, "enrollment-token", enrollmentToken,
		"node enrollment token (defaults to "+envEnrollmentToken+")")
	flags.StringVar(&tokenFile, "token-file", tokenFile,
		"path to the enrollment token file (defaults to "+envTokenFile+")")
	flags.DurationVar(&cfg.RequestTimeout, "request-timeout", controlplaneconfig.DefaultRequestTimeout,
		"timeout for a single node request")
	flags.DurationVar(&cfg.HeartbeatTimeout, "heartbeat-timeout", controlplaneconfig.DefaultHeartbeatTimeout,
		"idle time after which a node is considered offline")

	// The token is a secret: keep the value initialized from the environment
	// but clear the printed default so help and completion output never show it.
	if flag := flags.Lookup("enrollment-token"); flag != nil {
		flag.DefValue = ""
	}

	cmd.AddCommand(newServeTokenCommand())

	return cmd
}

// newServeTokenCommand 打印当前生效的注册令牌，文件缺失时生成并持久化。
//
// 令牌只写入 cmd.OutOrStdout()，绝不经过日志，避免泄漏到日志文件；
// 该命令不打开存储、不启动服务，可以安全地重复执行。
func newServeTokenCommand() *cobra.Command {
	tokenFile := defaultTokenFile()

	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print the control plane enrollment token",
		Long: `Print the enrollment token that node agents must present.

The token is read from the token file; if the file does not exist, a new token
is generated and written there so later runs print the same value.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := controlplaneconfig.ResolveToken("", tokenFile)
			if err != nil {
				return fmt.Errorf("resolve enrollment token: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), token)
			return err
		},
	}
	cmd.Flags().StringVar(&tokenFile, "token-file", tokenFile,
		"path to the enrollment token file (defaults to "+envTokenFile+")")

	return cmd
}

// controlPlaneRuntime 是已装配但尚未启动的控制面，供独立 serve 与进程内
// 组合启动（bare lework）复用同一套构造/运行/关闭流程。
type controlPlaneRuntime struct {
	srv *controlplane.Server
}

// openControlPlane 校验配置、打开存储并装配控制面；返回的运行时可多次 Close。
func openControlPlane(cfg controlplaneconfig.Config) (*controlPlaneRuntime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("control plane config: %w", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open control plane store: %w", err)
	}

	srv, err := controlplane.New(cfg, st)
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("start control plane: %w", err)
	}

	return &controlPlaneRuntime{srv: srv}, nil
}

// Run 启动 HTTP 服务并阻塞到 ctx 取消；信号处理由调用方负责。
func (r *controlPlaneRuntime) Run(ctx context.Context) error {
	if r == nil || r.srv == nil {
		return errors.New("control plane runtime is not initialized")
	}
	return r.srv.Run(ctx)
}

// Close 释放控制面资源（含存储），可重复调用。
func (r *controlPlaneRuntime) Close() error {
	if r == nil || r.srv == nil {
		return nil
	}
	return r.srv.Close()
}

// runControlPlane 解析注册令牌、装配运行时，并服务到信号触发退出。
func runControlPlane(parent context.Context, cfg controlplaneconfig.Config, explicitToken, tokenPath string) error {
	token, err := controlplaneconfig.ResolveToken(explicitToken, tokenPath)
	if err != nil {
		return fmt.Errorf("resolve enrollment token: %w", err)
	}
	cfg.EnrollmentToken = token

	runtime, err := openControlPlane(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = runtime.Close() }()

	ctx, stopSignals := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	return runtime.Run(ctx)
}
