package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/insmtx/lework/backend/internal/agentd/opencode"
	"github.com/insmtx/lework/backend/internal/agentd/servicefile"
)

const (
	// defaultOpenCodeBinary is the executable name resolved through PATH when
	// the user does not pass --opencode or LEWORK_OPENCODE_BIN.
	defaultOpenCodeBinary = "opencode"
	// envOpenCodeBinary lets an installation outside PATH point the TUI
	// command at its own build without repeating a flag.
	envOpenCodeBinary = "LEWORK_OPENCODE_BIN"
)

// Health polling. They are variables so tests can shorten the wait without
// sleeping for the production timeout.
var (
	serviceHealthTimeout = 5 * time.Second
	serviceHealthPoll    = 250 * time.Millisecond
	serviceHealthRequest = 2 * time.Second
)

// openCodeTUIOptions is the command-line surface of `lework opencode`.
type openCodeTUIOptions struct {
	Binary    string
	Directory string
	SessionID string
	Continue  bool
}

// tuiDeps isolates the command from the process and network so tests can
// assert the arguments, environment, and error paths without a real TUI.
type tuiDeps struct {
	readInfo func() (servicefile.Info, error)
	health   func(context.Context, servicefile.Info) error
	lookPath func(string) (string, error)
	run      func(ctx context.Context, path string, args, env []string) error
}

func defaultTUIDeps() tuiDeps {
	return tuiDeps{
		readInfo: servicefile.Read,
		health:   waitServiceReady,
		lookPath: exec.LookPath,
		run:      runOpenCodeExec,
	}
}

// runOpenCodeTUICommand is a seam so the command wiring can be tested without
// starting an interactive child process.
var runOpenCodeTUICommand = runOpenCodeTUI

// runOpenCodeTUI launches an OpenCode TUI that attaches to the service owned by
// the local lework agent.
func runOpenCodeTUI(ctx context.Context, opts openCodeTUIOptions) error {
	return runOpenCodeTUIWith(ctx, opts, defaultTUIDeps())
}

// runOpenCodeTUIWith reads the published endpoint, verifies the service, and
// starts `opencode --server <url>`. The published password reaches the child
// only through OPENCODE_PASSWORD; it is never included in an error.
func runOpenCodeTUIWith(ctx context.Context, opts openCodeTUIOptions, deps tuiDeps) error {
	info, err := deps.readInfo()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return errors.New("no running lework agent found for this user; start `lework agent` first")
		}
		return fmt.Errorf("read lework agent service info: %w", err)
	}

	if err := deps.health(ctx, info); err != nil {
		return err
	}

	binary := opts.Binary
	if binary == "" {
		binary = defaultOpenCodeBinary
	}
	path, err := deps.lookPath(binary)
	if err != nil {
		return fmt.Errorf("find opencode executable %q: %w", binary, err)
	}

	args := []string{"--server", info.URL}
	if opts.Continue {
		args = append(args, "--continue")
	}
	if opts.SessionID != "" {
		args = append(args, "--session", opts.SessionID)
	}
	if opts.Directory != "" {
		args = append(args, opts.Directory)
	}

	if err := deps.run(ctx, path, args, tuiEnv(os.Environ(), info.Password)); err != nil {
		return fmt.Errorf("run opencode: %w", err)
	}
	return nil
}

// waitServiceReady polls the agent-owned service until it answers. A rejected
// credential fails immediately: retrying cannot heal a password mismatch, and
// the published endpoint is only useful when both sides agree.
func waitServiceReady(ctx context.Context, info servicefile.Info) error {
	timeout := serviceHealthTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	poll := serviceHealthPoll
	if poll <= 0 {
		poll = 250 * time.Millisecond
	}
	requestTimeout := serviceHealthRequest
	if requestTimeout <= 0 {
		requestTimeout = 2 * time.Second
	}

	client := opencode.New(
		info.URL,
		&http.Client{Timeout: requestTimeout},
		opencode.WithPassword(info.Password),
	)
	deadline := time.Now().Add(timeout)

	var lastErr error
	for {
		probeCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		lastErr = client.Health(probeCtx)
		cancel()

		if lastErr == nil {
			return nil
		}
		if errors.Is(lastErr, opencode.ErrUnauthorized) {
			return fmt.Errorf("shared OpenCode service rejected credentials at %s: %w", info.URL, lastErr)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("shared OpenCode service is not available at %s: %w", info.URL, lastErr)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for shared OpenCode service: %w", ctx.Err())
		case <-time.After(poll):
		}
	}
}

// tuiEnv removes inherited OpenCode credentials before injecting the published
// password. Leaving an inherited value would make the TUI authenticate with the
// wrong password and report a misleading 401.
func tuiEnv(base []string, password string) []string {
	env := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, "OPENCODE_PASSWORD=") ||
			strings.HasPrefix(kv, "OPENCODE_SERVER_PASSWORD=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "OPENCODE_PASSWORD="+password)
}

// runOpenCodeExec replaces the current terminal's foreground process with the
// TUI. It deliberately does not use exec.CommandContext: the child shares the
// terminal's process group, so Ctrl-C reaches the TUI directly instead of being
// converted into an immediate SIGKILL by the parent.
func runOpenCodeExec(_ context.Context, path string, args, env []string) error {
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	return cmd.Run()
}

// openCodeBinaryFromEnv resolves the executable once at command construction so
// the help text shows the effective default.
func openCodeBinaryFromEnv() string {
	if value := strings.TrimSpace(os.Getenv(envOpenCodeBinary)); value != "" {
		return value
	}
	return defaultOpenCodeBinary
}

// newOpenCodeCommand builds `lework opencode`. It is a standalone command: it
// does not load the legacy CLI/worker config, and it talks only to the local
// agent-owned service.
func newOpenCodeCommand() *cobra.Command {
	opts := openCodeTUIOptions{}

	cmd := &cobra.Command{
		Use:   "opencode [directory]",
		Short: "Start an OpenCode TUI on the agent-managed service",
		Long: `Start an OpenCode TUI attached to the local lework agent's OpenCode service.

The agent must already be running on this node and enrolled with the control
plane. The command reads the endpoint and credential the agent published under
the current user's state directory, verifies the service, and starts
` + "`opencode --server <url>`" + ` in the current terminal. The TUI and the Lework
control plane then see the same sessions, messages, and permission requests.

This works only for the agent's own service. Running ` + "`opencode`" + ` directly still
connects to OpenCode's background service, and ` + "`opencode --standalone`" + ` uses a
private server; neither is visible to Lework.

Examples:
  lework opencode
  lework opencode ~/src/project --session ses_123
  lework opencode --continue --opencode /opt/opencode/bin/opencode`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				opts.Directory = args[0]
			}
			return runOpenCodeTUICommand(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVar(&opts.Binary, "opencode", openCodeBinaryFromEnv(),
		"opencode executable path (defaults to $"+envOpenCodeBinary+" or opencode)")
	cmd.Flags().StringVarP(&opts.SessionID, "session", "s", "",
		"OpenCode session ID to open")
	cmd.Flags().BoolVarP(&opts.Continue, "continue", "c", false,
		"continue the most recent OpenCode session")
	return cmd
}
