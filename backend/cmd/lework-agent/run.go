package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Lework/backend/internal/agentd/config"
	"github.com/insmtx/Lework/backend/internal/agentd/handler"
	"github.com/insmtx/Lework/backend/internal/agentd/opencode"
	"github.com/insmtx/Lework/backend/internal/agentd/runner"
	"github.com/insmtx/Lework/backend/internal/agentd/supervisor"
	"github.com/insmtx/Lework/backend/internal/agentd/transport"
)

const (
	// initialBackoff and maxBackoff bound the reconnect delay: 1s, 2s, 4s, ...
	// capped at 30s so a control-plane outage is retried promptly at first and
	// cheaply once it lasts.
	initialBackoff = time.Second
	maxBackoff     = 30 * time.Second

	// heartbeatInterval matches the control plane's idle expectation.
	heartbeatInterval = 10 * time.Second

	// shutdownTimeout bounds supervisor.Stop so a wedged child cannot block the
	// agent's exit forever.
	shutdownTimeout = 10 * time.Second
)

// newRunCommand builds the daemon command. The config flag carries a platform
// default so systemd/launchd/Windows Service units can omit it, while operators
// can always point at an alternate file.
func newRunCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the agent and connect to the control plane",
		Long: `Load the agent configuration and keep a control-plane connection alive.

The agent retries with exponential backoff (1s, 2s, 4s, capped at 30s) when the
control plane is unreachable. The local OpenCode service is stopped on shutdown,
but it is kept running across reconnects.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgent(cmd.Context(), configPath)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", defaultConfigPath(), "path to agent.yaml")
	return cmd
}

// runAgent wires the daemon and runs it until ctx ends. Config errors are
// returned before any network or process work happens, which keeps a broken
// configuration from leaving a half-started node behind.
func runAgent(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	// config.Load fills defaults but deliberately does not validate; the daemon
	// must reject an incomplete file before dialing.
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid agent config %s: %w", configPath, err)
	}

	client := opencode.New(fmt.Sprintf("http://127.0.0.1:%d", cfg.OpenCode.Port), http.DefaultClient)
	service := supervisor.New(client, nil, supervisor.ServiceConfig{
		Binary: cfg.OpenCode.Binary,
		Port:   cfg.OpenCode.Port,
		Env:    envSlice(cfg.OpenCode.Env),
		Dir:    "",
	})
	requestHandler := handler.New(client, service)

	// SIGINT/SIGTERM cancel the run loop; the deferred Stop below turns the
	// cancellation into a clean child-process shutdown.
	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	// The shared OpenCode service outlives individual connections but not the
	// agent: stopping it here prevents an orphan when the daemon exits.
	defer stopService(service)

	logs.Infof("lework-agent: starting node %q", cfg.NodeName)

	return reconnect(ctx, cfg, client, requestHandler)
}

// reconnect dials the control plane, runs one connection, and retries with
// exponential backoff until ctx is canceled. A nil error means a clean shutdown.
func reconnect(ctx context.Context, cfg *config.Config, client *opencode.Client, requestHandler *handler.Handler) error {
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
			// A successful handshake resets the delay so the next transient drop
			// is retried quickly.
			backoff = initialBackoff
			logs.Infof("lework-agent: connected to control plane")
			runErr := runner.New(conn, requestHandler, client, runner.RunnerConfig{
				// NodeID is a placeholder in I0: the control plane's welcome
				// frame carries the real node ID, which is not persisted yet.
				NodeID:       cfg.NodeName,
				AgentVersion: version,
				Heartbeat:    heartbeatInterval,
			}).Run(ctx)

			if closeErr := conn.Close(); closeErr != nil {
				logs.Warnf("lework-agent: close connection: %s", redact(closeErr, cfg.EnrollmentToken))
			}
			if ctx.Err() != nil || runErr == nil {
				return nil
			}
			logs.Warnf("lework-agent: connection ended: %s", redact(runErr, cfg.EnrollmentToken))
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
func stopService(service *supervisor.Supervisor) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := service.Stop(ctx); err != nil {
		logs.Warnf("lework-agent: stop opencode service: %s", err)
	}
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

// defaultConfigPath returns the conventional agent.yaml location for the
// running platform. The installation contract is documented in
// docs/design/2026-11-01-opencode-control-plane-technical-architecture.md.
func defaultConfigPath() string {
	return defaultConfigPathFor(runtime.GOOS)
}

// defaultConfigPathFor resolves the platform default. It is split out from
// defaultConfigPath so every platform's path can be tested on any host.
func defaultConfigPathFor(goos string) string {
	switch goos {
	case "linux":
		return "/etc/lework/agent.yaml"
	case "darwin":
		// os.UserConfigDir is ~/Library/Application Support on macOS.
		dir, err := os.UserConfigDir()
		if err != nil {
			return filepath.Join("~", "Library", "Application Support", "Lework", "agent.yaml")
		}
		return filepath.Join(dir, "Lework", "agent.yaml")
	case "windows":
		dir := os.Getenv("ProgramData")
		if dir == "" {
			return `C:\ProgramData\Lework\agent.yaml`
		}
		return filepath.Join(dir, "Lework", "agent.yaml")
	default:
		dir, err := os.UserConfigDir()
		if err != nil {
			return "agent.yaml"
		}
		return filepath.Join(dir, "Lework", "agent.yaml")
	}
}
