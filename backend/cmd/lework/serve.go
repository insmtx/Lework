package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/insmtx/lework/backend/internal/controlplane"
	controlplaneconfig "github.com/insmtx/lework/backend/internal/controlplane/config"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
	"github.com/insmtx/lework/backend/internal/infra/mq"
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
	configPath := resolveConfigPath()
	natsURL := ""
	databaseURL := ""

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

The control plane stores node data in PostgreSQL and fans events out over
NATS. Configure them with --nats-url / --database-url or the top-level
nats: / database: sections of --config; startup fails without both.

Examples:
  lework serve --enrollment-token <token>
  lework serve token
  lework serve --nats-url nats://127.0.0.1:4222 --database-url postgres://127.0.0.1:5432/lework`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			file, err := loadConfigFile(configPath, cmd.Flags().Changed("config"))
			if err != nil {
				return err
			}
			file.applyControlPlane(&cfg, &tokenFile, cmd.Flags().Changed)
			applyClusterFlags(&cfg, natsURL, databaseURL, cmd.Flags().Changed)
			announceStartup(cmd.OutOrStdout(), cfg)
			return runControlPlane(cmd.Context(), cfg, enrollmentToken, tokenFile)
		},
	}

	flags := cmd.Flags()
	// --config 设为持久 flag，`serve token` 也读同一份配置里的 token_file。
	cmd.PersistentFlags().StringVar(&configPath, "config", configPath,
		"unified config file (controlplane: and agent: sections)")
	flags.StringVar(&cfg.ListenAddr, "listen", controlplaneconfig.DefaultListenAddr,
		"HTTP listen address (non-loopback requires the enrollment token)")
	flags.StringVar(&enrollmentToken, "enrollment-token", enrollmentToken,
		"node enrollment token (defaults to "+envEnrollmentToken+")")
	flags.StringVar(&tokenFile, "token-file", tokenFile,
		"path to the enrollment token file (defaults to "+envTokenFile+")")
	flags.DurationVar(&cfg.RequestTimeout, "request-timeout", controlplaneconfig.DefaultRequestTimeout,
		"timeout for a single node request")
	flags.DurationVar(&cfg.HeartbeatTimeout, "heartbeat-timeout", controlplaneconfig.DefaultHeartbeatTimeout,
		"idle time after which a node is considered offline")
	flags.StringVar(&natsURL, "nats-url", "",
		"NATS URL for cluster event fan-out (defaults to nats.url in the config file)")
	flags.StringVar(&databaseURL, "database-url", "",
		"PostgreSQL URL shared by control plane instances (defaults to database.url in the config file)")
	addDeprecatedPairingFlags(cmd)

	// The token is a secret: keep the value initialized from the environment
	// but clear the printed default so help and completion output never show it.
	if flag := flags.Lookup("enrollment-token"); flag != nil {
		flag.DefValue = ""
	}

	cmd.AddCommand(newServeTokenCommand())

	return cmd
}

// addDeprecatedPairingFlags 保留已废弃的启动配对参数，避免旧部署直接失败。
//
// 这三个 flag 过去用于启动时打印带 enrollment token 的手机配对二维码。设计
// §8/§12 取消该二维码后，启动不再输出任何凭据；这里仍接受这三个参数，但只在
// help 里以 deprecated 说明并隐藏，让已有的 systemd 单元与脚本继续可用。
func addDeprecatedPairingFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.String("public-url", "", "deprecated: ignored; the control plane no longer prints a pairing QR")
	_ = flags.MarkHidden("public-url")
	flags.String("expo-url", "", "deprecated: ignored; the control plane no longer prints a pairing QR")
	_ = flags.MarkHidden("expo-url")
	flags.Bool("no-qr", false, "deprecated: ignored; the control plane no longer prints a pairing QR")
	_ = flags.MarkHidden("no-qr")
}

// announceStartup 是「控制面启动不得打印任何凭据」的不变量接缝。
//
// 设计 §8/§12 要求启动不再打印手机配对二维码：账号体系下手机用账号登录，
// enrollment token 只是管理员 Bearer，出现在终端回滚、截图或共享屏幕里就等于
// 泄漏。这个函数刻意什么都不做，保留它是为了让该不变量有直接的单测（传入带
// token 的配置，断言输出为空且不含 lework://connect / token=），避免以后有人
// 又把二维码加回启动路径。
func announceStartup(_ io.Writer, _ controlplaneconfig.Config) {}

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
			// serve --config 是持久 flag，token 子命令也能读到；配置里写了
			// token_file 时，两边必须指向同一个文件，否则会打印出另一份令牌。
			if path, flagErr := cmd.Flags().GetString("config"); flagErr == nil {
				file, loadErr := loadConfigFile(path, cmd.Flags().Changed("config"))
				if loadErr != nil {
					return loadErr
				}
				file.applyTokenFile(&tokenFile, cmd.Flags().Changed)
			}
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
	bus io.Closer
}

// openControlPlane 校验配置、打开共享存储与集群总线并装配控制面。
//
// 控制面的运行路径只有这一条：PostgreSQL 保证节点数据跨实例共享，NATS
// 负责事件扇出与跨实例请求，缺一不可。任一环节失败时按打开顺序回滚，
// 错误信息绝不拼接 URL（URL 可能内嵌账号密码）。
func openControlPlane(cfg controlplaneconfig.Config) (*controlPlaneRuntime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("control plane config: %w", err)
	}
	if err := cfg.ValidateCluster(); err != nil {
		return nil, fmt.Errorf("control plane cluster config: %w", err)
	}
	if cfg.Database == nil {
		return nil, errors.New("control plane database config is required")
	}
	if cfg.NATS == nil {
		return nil, errors.New("control plane nats config is required")
	}

	st, err := store.OpenPostgres(*cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("open control plane store: %w", err)
	}

	bus, err := mq.NewNATS(cfg.NATS.URL)
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("connect control plane bus: %w", err)
	}

	srv, err := controlplane.New(cfg, st, controlplane.Deps{
		Bus:     bus,
		Core:    bus,
		CoreSub: bus,
		Request: bus,
		Pull:    bus,
	})
	if err != nil {
		_ = bus.Close()
		_ = st.Close()
		return nil, fmt.Errorf("start control plane: %w", err)
	}

	return &controlPlaneRuntime{srv: srv, bus: bus}, nil
}

// Run 启动 HTTP 服务并阻塞到 ctx 取消；信号处理由调用方负责。
func (r *controlPlaneRuntime) Run(ctx context.Context) error {
	if r == nil || r.srv == nil {
		return errors.New("control plane runtime is not initialized")
	}
	return r.srv.Run(ctx)
}

// Close 先释放控制面（含存储），再关闭集群总线；可重复调用。
func (r *controlPlaneRuntime) Close() error {
	if r == nil || r.srv == nil {
		return nil
	}

	var errs []error
	if err := r.srv.Close(); err != nil {
		errs = append(errs, err)
	}
	if r.bus != nil {
		if err := r.bus.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close control plane bus: %w", err))
		}
	}
	return errors.Join(errs...)
}

// runControlPlane 解析注册令牌、装配运行时，并服务到信号触发退出。
//
// 启动路径不再向终端打印任何凭据；令牌只用于节点入网与管理员 Bearer。
func runControlPlane(
	parent context.Context,
	cfg controlplaneconfig.Config,
	explicitToken, tokenPath string,
) error {
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
