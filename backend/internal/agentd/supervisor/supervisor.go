// Package supervisor owns the local opencode service lifecycle. A node runs at
// most one shared service per database, so the supervisor first attaches to an
// already-healthy instance and only spawns a process when none answers. It
// never starts a second process while one is tracked.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Defaults applied by New when a ServiceConfig leaves them empty.
const (
	defaultBinary = "opencode"
	defaultPort   = 4096
	serviceHost   = "127.0.0.1"
)

// Health and startup polling. They are struct fields on Supervisor so tests can
// shorten them without sleeping for the production timeout.
const (
	healthPollInterval = 200 * time.Millisecond
	healthTimeout      = 30 * time.Second
)

// Status.Mode values. "attached" means an instance the supervisor does not own
// answered the health check; "managed" means the tracked child is serving.
const (
	modeAttached = "attached"
	modeManaged  = "managed"
)

// HealthChecker reports whether the service accepts requests. opencode.Client
// satisfies it.
type HealthChecker interface {
	Health(ctx context.Context) error
}

// ServiceConfig describes the shared service the supervisor may launch.
type ServiceConfig struct {
	// Binary is the opencode executable name or absolute path.
	Binary string
	// Port is the local HTTP port the service listens on.
	Port int
	// Env holds extra environment variables for the child, as KEY=VALUE.
	Env []string
	// Dir is the child's working directory; empty inherits the agent's.
	Dir string
}

// Status is a snapshot of the supervisor's view of the service.
type Status struct {
	Running bool
	Mode    string
	BaseURL string
}

// Supervisor serializes service startup. All operations take the same lock, so
// concurrent Ensure calls can never race into two spawns.
type Supervisor struct {
	mu sync.Mutex

	client  HealthChecker
	spawner Spawner
	cfg     ServiceConfig
	baseURL string

	proc   Process
	status Status

	pollInterval   time.Duration
	startupTimeout time.Duration
}

// New builds a supervisor for the shared service at cfg.Port. A nil spawner
// selects ExecSpawner, the production default.
func New(client HealthChecker, spawner Spawner, cfg ServiceConfig) *Supervisor {
	if cfg.Binary == "" {
		cfg.Binary = defaultBinary
	}
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	if len(cfg.Env) > 0 {
		cfg.Env = append([]string(nil), cfg.Env...)
	}
	if spawner == nil {
		spawner = ExecSpawner{}
	}

	baseURL := fmt.Sprintf("http://%s:%d", serviceHost, cfg.Port)
	return &Supervisor{
		client:         client,
		spawner:        spawner,
		cfg:            cfg,
		baseURL:        baseURL,
		status:         Status{BaseURL: baseURL},
		pollInterval:   healthPollInterval,
		startupTimeout: healthTimeout,
	}
}

// Ensure makes the shared service available and returns its status. It attaches
// when the port already answers, otherwise it spawns and waits for startup.
// Calling it repeatedly is safe: it never starts a second process while one is
// tracked.
func (s *Supervisor) Ensure(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureLocked(ctx)
}

// Restart stops the tracked process, if any, and ensures the service again.
func (s *Supervisor) Restart(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.stopLocked(ctx); err != nil {
		return err
	}
	_, err := s.ensureLocked(ctx)
	return err
}

// Stop terminates the tracked process and clears the running status. It is a
// no-op when the supervisor only attached to a foreign instance.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked(ctx)
}

// Status returns the last observed state without touching the service.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// ensureLocked implements Ensure while the caller holds s.mu.
func (s *Supervisor) ensureLocked(ctx context.Context) (Status, error) {
	if s.client == nil {
		return Status{}, errors.New("agentd: supervisor requires a health checker")
	}

	// An already-healthy service may belong to another agent on this node, so
	// attach instead of binding the same database with a second server. When
	// the answering service is our own child, report it as managed: calling
	// Ensure again must not downgrade a process the supervisor owns.
	if err := s.client.Health(ctx); err == nil {
		if s.proc != nil {
			return s.setStatus(true, modeManaged), nil
		}
		return s.setStatus(true, modeAttached), nil
	}

	// A tracked process exists but is not answering yet (still booting, or it
	// died). Wait for it rather than spawning a duplicate on the same port.
	if s.proc != nil {
		if err := s.waitHealthy(ctx); err != nil {
			return Status{}, s.abandon(ctx, s.proc, err)
		}
		return s.setStatus(true, modeManaged), nil
	}

	proc, err := s.spawner.Spawn(ctx, s.spec())
	if err != nil {
		return Status{}, fmt.Errorf("agentd: spawn opencode service: %w", err)
	}
	if proc == nil {
		return Status{}, errors.New("agentd: spawner returned no process")
	}
	if err := proc.Start(ctx); err != nil {
		return Status{}, fmt.Errorf("agentd: start opencode service: %w", err)
	}
	s.proc = proc

	if err := s.waitHealthy(ctx); err != nil {
		return Status{}, s.abandon(ctx, proc, err)
	}
	return s.setStatus(true, modeManaged), nil
}

// abandon stops a process that failed to become healthy, clearing the tracked
// state so a later Ensure may spawn cleanly. The stop error, if any, is joined
// with cause so the operator sees both failures.
func (s *Supervisor) abandon(ctx context.Context, proc Process, cause error) error {
	s.proc = nil
	s.setStatus(false, "")

	stopErr := proc.Stop(ctx)
	if stopErr != nil {
		cause = errors.Join(cause, fmt.Errorf("stop opencode service: %w", stopErr))
	}
	return fmt.Errorf("agentd: opencode service did not become healthy: %w", cause)
}

// stopLocked clears the tracked process before stopping it so a failed stop
// cannot leave a half-dead process recorded as running.
func (s *Supervisor) stopLocked(ctx context.Context) error {
	proc := s.proc
	s.proc = nil
	s.setStatus(false, "")
	if proc == nil {
		return nil
	}
	if err := proc.Stop(ctx); err != nil {
		return fmt.Errorf("agentd: stop opencode service: %w", err)
	}
	return nil
}

// waitHealthy polls until the service answers, ctx ends, or the startup
// deadline passes. The first probe runs immediately so a fast start costs no
// extra latency.
func (s *Supervisor) waitHealthy(ctx context.Context) error {
	timeout := s.startupTimeout
	if timeout <= 0 {
		timeout = healthTimeout
	}
	interval := s.pollInterval
	if interval <= 0 {
		interval = healthPollInterval
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastErr error
	for {
		lastErr = s.client.Health(ctx)
		if lastErr == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for opencode health: %w", ctx.Err())
		case <-deadline.C:
			return fmt.Errorf("opencode not healthy after %s: %w", timeout, lastErr)
		case <-ticker.C:
		}
	}
}

// setStatus records and returns a fresh snapshot. BaseURL is retained when the
// service stops so callers can still report where it used to listen.
func (s *Supervisor) setStatus(running bool, mode string) Status {
	s.status = Status{Running: running, Mode: mode, BaseURL: s.baseURL}
	return s.status
}

// spec builds the shared-service command. The env slice is copied so a caller
// mutating cfg.Env cannot change a process that is already starting.
func (s *Supervisor) spec() Spec {
	return Spec{
		Binary: s.cfg.Binary,
		Args: []string{
			"serve",
			"--service",
			"--hostname", serviceHost,
			"--port", fmt.Sprintf("%d", s.cfg.Port),
		},
		Env: append([]string(nil), s.cfg.Env...),
		Dir: s.cfg.Dir,
	}
}
