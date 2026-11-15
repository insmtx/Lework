package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ygpkg/yg-go/logs"

	agentdconfig "github.com/insmtx/lework/backend/internal/agentd/config"
	"github.com/insmtx/lework/backend/internal/agentd/handler"
	"github.com/insmtx/lework/backend/internal/agentd/opencode"
	"github.com/insmtx/lework/backend/internal/agentd/runner"
	"github.com/insmtx/lework/backend/internal/agentd/supervisor"
	"github.com/insmtx/lework/backend/internal/agentd/transport"
)

const (
	// initialBackoff and maxBackoff bound the reconnect delay: 1s, 2s, 4s, ...
	// capped at 30s so a control-plane outage is retried promptly at first and
	// cheaply once it lasts.
	initialBackoff = time.Second
	maxBackoff     = 30 * time.Second

	// backoffResetAfter is the stability window a connection must survive before
	// a later drop restarts the retry ladder. Without it, a control plane that
	// accepts then immediately drops would keep the agent at a 1s retry cadence.
	backoffResetAfter = 10 * time.Second

	// heartbeatInterval matches the control plane's idle expectation.
	heartbeatInterval = 10 * time.Second

	// shutdownTimeout bounds supervisor.Stop so a wedged child cannot block the
	// agent's exit forever.
	shutdownTimeout = 10 * time.Second
)

// agentRuntime 是已装配但尚未连接的节点 agent，供独立 agent 与进程内组合
// 启动（bare lework）复用同一套构造/运行/关闭流程。
type agentRuntime struct {
	cfg     *agentdconfig.Config
	client  *opencode.Client
	service *supervisor.Supervisor
	handler *handler.Handler
}

// buildAgent 解析 OpenCode 环境并装配客户端、监管器与请求处理器。
// 它只做构造：不连控制面，也不启动子进程。
func buildAgent(cfg *agentdconfig.Config) (*agentRuntime, error) {
	// The password is shared by the client and the child process. It is derived
	// or generated once here so a generated value reaches opencode through its
	// environment and never has to be read back from the server output.
	env, password, err := openCodeEnv(cfg.OpenCode.Env)
	if err != nil {
		return nil, err
	}

	client := opencode.New(
		fmt.Sprintf("http://127.0.0.1:%d", cfg.OpenCode.Port),
		http.DefaultClient,
		opencode.WithPassword(password),
	)
	service := supervisor.New(client, nil, supervisor.ServiceConfig{
		Binary: cfg.OpenCode.Binary,
		Port:   cfg.OpenCode.Port,
		Env:    envSlice(env),
		Dir:    "",
	})
	requestHandler := handler.New(client, service, handler.WithWorkspaceRoots(cfg.WorkspaceRoots...))

	return &agentRuntime{
		cfg:     cfg,
		client:  client,
		service: service,
		handler: requestHandler,
	}, nil
}

// Run 启动本地 OpenCode 服务并连接控制面，阻塞到 ctx 取消。
// 信号处理由调用方负责，这里不得注册 signal。
func (a *agentRuntime) Run(ctx context.Context) error {
	if a == nil || a.cfg == nil || a.client == nil || a.service == nil || a.handler == nil {
		return errors.New("agent runtime is not initialized")
	}

	// Bootstrap the local service once before dialing. The runner subscribes to
	// OpenCode events immediately after welcome, so without a running service
	// every connection would die at subscribe time and reconnect before the
	// control plane could send opencode.start. A failure here is not fatal: the
	// control plane may still start the service later via opencode.start, and
	// the agent can register and report the failure.
	if _, err := a.service.Ensure(ctx); err != nil {
		if ctx.Err() == nil {
			logs.Warnf("lework-agent: start opencode service: %s", redact(err, a.cfg.EnrollmentToken))
		}
	} else {
		logs.Infof("lework-agent: opencode service ready")
	}

	logs.Infof("lework-agent: starting node %q", a.cfg.NodeName)

	return reconnect(ctx, a.cfg, a.client, a.handler)
}

// Close 停止 agent 托管的 OpenCode 子进程，避免 daemon 退出后留下孤儿进程。
// 可重复调用：supervisor.Stop 在没有受管子进程时是空操作。
func (a *agentRuntime) Close() error {
	if a == nil || a.cfg == nil || a.service == nil {
		return nil
	}
	stopService(a.service, a.cfg.EnrollmentToken)
	return nil
}

// runAgent wires the daemon and runs it until ctx ends. Config errors are
// returned before any network or process work happens, which keeps a broken
// configuration from leaving a half-started node behind.
func runAgent(ctx context.Context, configPath string) error {
	cfg, err := loadAgentConfig(configPath)
	if err != nil {
		return err
	}
	// agent.yaml 没写 workspace_roots 时沿用组合模式的默认目录；
	// 显式配置的目录不受影响。
	if err := applyDefaultWorkspaceRoots(cfg); err != nil {
		return fmt.Errorf("%w (set workspace_roots in %s)", err, configPath)
	}
	// agentdconfig.Load fills defaults but deliberately does not validate; the
	// daemon must reject an incomplete file before dialing.
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid agent config %s: %w", configPath, err)
	}

	runtime, err := buildAgent(cfg)
	if err != nil {
		return err
	}

	// SIGINT/SIGTERM cancel the run loop; the deferred Close below turns the
	// cancellation into a clean child-process shutdown.
	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	// The shared OpenCode service outlives individual connections but not the
	// agent: stopping it here prevents an orphan when the daemon exits.
	defer func() { _ = runtime.Close() }()

	return runtime.Run(ctx)
}

// reconnect dials the control plane, runs one connection, and retries with
// exponential backoff until ctx is canceled. A nil error means a clean shutdown.
func reconnect(
	ctx context.Context,
	cfg *agentdconfig.Config,
	client *opencode.Client,
	requestHandler *handler.Handler,
) error {
	backoff := initialBackoff

	for {
		if ctx.Err() != nil {
			return nil
		}

		conn, err := transport.Dial(ctx, transport.DialConfig{
			URL:            cfg.ControlPlaneURL,
			TLSFingerprint: cfg.TLSFingerprint,
			Header:         controlPlaneHeader(cfg.EnrollmentToken),
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logs.Warnf("lework-agent: connect control plane: %s", redact(err, cfg.EnrollmentToken))
		} else {
			logs.Infof("lework-agent: connected to control plane")
			started := time.Now()
			runErr := runner.New(conn, requestHandler, client, runner.RunnerConfig{
				// NodeID is a placeholder: the welcome frame carries the ID the
				// control plane assigned (node_...) and the runner switches to
				// it before forwarding events. NodeName is the stable identity
				// the control plane keys node records by.
				NodeID:         cfg.NodeName,
				NodeName:       cfg.NodeName,
				AgentVersion:   version,
				Heartbeat:      heartbeatInterval,
				WorkspaceRoots: cfg.WorkspaceRoots,
			}).Run(ctx)
			connectedFor := time.Since(started)

			if closeErr := conn.Close(); closeErr != nil {
				logs.Warnf("lework-agent: close connection: %s", redact(closeErr, cfg.EnrollmentToken))
			}
			if ctx.Err() != nil || runErr == nil {
				return nil
			}
			logs.Warnf("lework-agent: connection ended: %s", redact(runErr, cfg.EnrollmentToken))

			// Only a connection that stayed up long enough earns a fresh
			// ladder. A connect-then-drop loop keeps doubling the delay.
			if shouldResetBackoff(connectedFor) {
				backoff = initialBackoff
			}
		}

		if err := waitBackoff(ctx, backoff); err != nil {
			return nil
		}
		backoff = nextBackoff(backoff)
	}
}

// stopService terminates a managed OpenCode process within shutdownTimeout.
// It uses a fresh context because the run context is already canceled by the
// time this deferred call executes.
func stopService(service *supervisor.Supervisor, enrollmentToken string) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := service.Stop(ctx); err != nil {
		logs.Warnf("lework-agent: stop opencode service: %s", redact(err, enrollmentToken))
	}
}

// shouldResetBackoff reports whether a connection lived long enough to count as
// healthy. Short-lived connections keep climbing the ladder so a control plane
// that accepts and immediately drops cannot pin the agent at 1s retries.
func shouldResetBackoff(connectedFor time.Duration) bool {
	return connectedFor >= backoffResetAfter
}

// nextBackoff doubles current up to maxBackoff. A non-positive current starts
// the ladder at initialBackoff.
func nextBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return initialBackoff
	}
	next := current * 2
	if next <= 0 || next > maxBackoff {
		return maxBackoff
	}
	return next
}

// waitBackoff sleeps for d or until ctx is canceled, whichever comes first.
func waitBackoff(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// openCodeEnv copies the configured environment, resolves the shared service
// password, and records a generated value so the client and the child process
// agree on it. The input map is never modified: it belongs to the loaded
// configuration, and mutating it would leak the password into a value other
// callers can read.
func openCodeEnv(cfgEnv map[string]string) (map[string]string, string, error) {
	env := make(map[string]string, len(cfgEnv)+1)
	for key, value := range cfgEnv {
		env[key] = value
	}
	// An empty yaml value means "unset", not "clear": the child inherits the
	// daemon's environment, and mergeEnv would let a copied empty key override
	// a non-empty inherited password with "". Dropping the empty entries keeps
	// the effective inherited value and stops an empty key reaching the child.
	for _, key := range []string{"OPENCODE_PASSWORD", "OPENCODE_SERVER_PASSWORD"} {
		if env[key] == "" {
			delete(env, key)
		}
	}

	password, generated, err := resolveOpenCodePassword(env)
	if err != nil {
		return nil, "", err
	}
	if generated {
		env["OPENCODE_SERVER_PASSWORD"] = password
	}
	return env, password, nil
}

// resolveOpenCodePassword returns the password the local server and client
// must share, and whether the agent generated it.
//
// The child does not run with only the yaml env: supervisor.mergeEnv starts
// from os.Environ() and lets spec.Env override individual keys, so a value
// inherited from systemd Environment= or a shell export is still visible to
// OpenCode. OpenCode itself checks OPENCODE_PASSWORD before
// OPENCODE_SERVER_PASSWORD. The effective value for each key is therefore a
// non-empty yaml override when present, otherwise the inherited process value.
// Looking at the yaml map alone would generate a fresh password that the child
// never uses (because it still sees an inherited OPENCODE_PASSWORD), producing
// a permanent 401 and a 30s health timeout. Generated passwords are 32 random
// bytes in unpadded base64url, which is safe inside an environment value and a
// Basic Auth header.
func resolveOpenCodePassword(env map[string]string) (password string, generated bool, err error) {
	effectivePassword := env["OPENCODE_PASSWORD"]
	if effectivePassword == "" {
		effectivePassword = os.Getenv("OPENCODE_PASSWORD")
	}
	effectiveServerPassword := env["OPENCODE_SERVER_PASSWORD"]
	if effectiveServerPassword == "" {
		effectiveServerPassword = os.Getenv("OPENCODE_SERVER_PASSWORD")
	}

	if effectivePassword != "" {
		return effectivePassword, false, nil
	}
	if effectiveServerPassword != "" {
		return effectiveServerPassword, false, nil
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", false, fmt.Errorf("generate opencode password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), true, nil
}

// envSlice converts the configured OpenCode environment into the KEY=VALUE form
// the supervisor passes to the child. Sorting makes the result deterministic so
// repeated runs launch an identical process specification.
func envSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

// controlPlaneHeader carries the enrollment token in the WebSocket handshake.
// The frame protocol has no token field, and keeping the token out of frames
// also keeps it out of frame logs.
func controlPlaneHeader(token string) http.Header {
	if token == "" {
		return nil
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	return header
}

// redact removes a known secret from an error message before it is logged.
// Transport errors already drop userinfo, but a proxy or a mistyped config can
// copy the token into a URL, so this is defense in depth.
func redact(err error, secret string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if secret != "" {
		message = strings.ReplaceAll(message, secret, "[redacted]")
	}
	return message
}
