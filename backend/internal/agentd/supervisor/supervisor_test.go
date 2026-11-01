package supervisor

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

type fakeHealth struct{ err error }

func (f *fakeHealth) Health(context.Context) error { return f.err }

type fakeProcess struct{ stopped bool }

func (p *fakeProcess) Start(context.Context) error { return nil }
func (p *fakeProcess) Stop(context.Context) error  { p.stopped = true; return nil }
func (p *fakeProcess) Wait() error                 { return nil }

type fakeSpawner struct {
	calls  int
	health *fakeHealth
	proc   *fakeProcess
}

func (s *fakeSpawner) Spawn(context.Context, Spec) (Process, error) {
	s.calls++
	s.health.err = nil
	return s.proc, nil
}

// recordingSpawner keeps the last spec so tests can assert the service command.
type recordingSpawner struct {
	health *fakeHealth
	proc   *fakeProcess
	spec   Spec
	calls  int
}

func (s *recordingSpawner) Spawn(_ context.Context, spec Spec) (Process, error) {
	s.calls++
	s.spec = spec
	s.health.err = nil
	return s.proc, nil
}

// stuckSpawner never makes health succeed, which drives the startup timeout.
type stuckSpawner struct {
	proc  *fakeProcess
	calls int
}

func (s *stuckSpawner) Spawn(context.Context, Spec) (Process, error) {
	s.calls++
	return s.proc, nil
}

// restartProcess marks the service unhealthy when stopped, so a restart has to
// spawn again instead of attaching to the process it just killed.
type restartProcess struct {
	health  *fakeHealth
	stopped bool
}

func (p *restartProcess) Start(context.Context) error { return nil }
func (p *restartProcess) Wait() error                 { return nil }

func (p *restartProcess) Stop(context.Context) error {
	p.stopped = true
	p.health.err = errors.New("process stopped")
	return nil
}

type restartSpawner struct {
	health *fakeHealth
	procs  []*restartProcess
}

func (s *restartSpawner) Spawn(context.Context, Spec) (Process, error) {
	s.health.err = nil
	p := &restartProcess{health: s.health}
	s.procs = append(s.procs, p)
	return p, nil
}

func TestEnsureAttachesWhenHealthy(t *testing.T) {
	health := &fakeHealth{}
	spawner := &fakeSpawner{health: health, proc: &fakeProcess{}}
	sup := New(health, spawner, ServiceConfig{Port: 5000})

	status, err := sup.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() error = %v, want nil", err)
	}
	if spawner.calls != 0 {
		t.Fatalf("spawner calls = %d, want 0", spawner.calls)
	}
	want := Status{Running: true, Mode: "attached", BaseURL: "http://127.0.0.1:5000"}
	if status != want {
		t.Fatalf("status = %+v, want %+v", status, want)
	}
	if got := sup.Status(); got != want {
		t.Fatalf("Status() = %+v, want %+v", got, want)
	}
}

func TestEnsureStartsWhenUnhealthy(t *testing.T) {
	health := &fakeHealth{err: errors.New("connection refused")}
	proc := &fakeProcess{}
	spawner := &fakeSpawner{health: health, proc: proc}
	sup := New(health, spawner, ServiceConfig{Port: 5000})

	status, err := sup.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() error = %v, want nil", err)
	}
	if spawner.calls != 1 {
		t.Fatalf("spawner calls = %d, want 1", spawner.calls)
	}
	if !status.Running {
		t.Fatalf("status.Running = false, want true")
	}
	if status.Mode != "managed" {
		t.Fatalf("status.Mode = %q, want managed", status.Mode)
	}
	if status.BaseURL != "http://127.0.0.1:5000" {
		t.Fatalf("status.BaseURL = %q, want http://127.0.0.1:5000", status.BaseURL)
	}
}

func TestEnsureSendsServiceArgs(t *testing.T) {
	health := &fakeHealth{err: errors.New("connection refused")}
	spawner := &recordingSpawner{health: health, proc: &fakeProcess{}}
	cfg := ServiceConfig{
		Binary: "/usr/local/bin/opencode",
		Port:   5000,
		Env:    []string{"OPENCODE_LOG=debug"},
		Dir:    "/srv/workspace",
	}
	sup := New(health, spawner, cfg)

	if _, err := sup.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v, want nil", err)
	}

	wantArgs := []string{"serve", "--service", "--hostname", "127.0.0.1", "--port", "5000"}
	if got := spawner.spec.Binary; got != cfg.Binary {
		t.Fatalf("spec.Binary = %q, want %q", got, cfg.Binary)
	}
	if len(spawner.spec.Args) != len(wantArgs) {
		t.Fatalf("spec.Args = %v, want %v", spawner.spec.Args, wantArgs)
	}
	for i := range wantArgs {
		if spawner.spec.Args[i] != wantArgs[i] {
			t.Fatalf("spec.Args = %v, want %v", spawner.spec.Args, wantArgs)
		}
	}
	if len(spawner.spec.Env) != 1 || spawner.spec.Env[0] != cfg.Env[0] {
		t.Fatalf("spec.Env = %v, want %v", spawner.spec.Env, cfg.Env)
	}
	if spawner.spec.Dir != cfg.Dir {
		t.Fatalf("spec.Dir = %q, want %q", spawner.spec.Dir, cfg.Dir)
	}
}

func TestEnsureDefaultsBinaryAndPort(t *testing.T) {
	health := &fakeHealth{err: errors.New("connection refused")}
	spawner := &recordingSpawner{health: health, proc: &fakeProcess{}}
	sup := New(health, spawner, ServiceConfig{})

	status, err := sup.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() error = %v, want nil", err)
	}
	if spawner.spec.Binary != "opencode" {
		t.Fatalf("spec.Binary = %q, want opencode", spawner.spec.Binary)
	}
	if got := spawner.spec.Args[len(spawner.spec.Args)-1]; got != "4096" {
		t.Fatalf("spec port arg = %q, want 4096", got)
	}
	if status.BaseURL != "http://127.0.0.1:4096" {
		t.Fatalf("status.BaseURL = %q, want http://127.0.0.1:4096", status.BaseURL)
	}
}

func TestEnsureDoesNotSpawnTwice(t *testing.T) {
	health := &fakeHealth{err: errors.New("connection refused")}
	spawner := &fakeSpawner{health: health, proc: &fakeProcess{}}
	sup := New(health, spawner, ServiceConfig{Port: 5000})

	if _, err := sup.Ensure(context.Background()); err != nil {
		t.Fatalf("first Ensure() error = %v, want nil", err)
	}
	if _, err := sup.Ensure(context.Background()); err != nil {
		t.Fatalf("second Ensure() error = %v, want nil", err)
	}
	if spawner.calls != 1 {
		t.Fatalf("spawner calls = %d, want 1", spawner.calls)
	}
	if got := sup.Status(); got.Mode != "managed" {
		t.Fatalf("Status().Mode = %q, want managed", got.Mode)
	}
}

func TestEnsureTimesOutAndStopsProcess(t *testing.T) {
	health := &fakeHealth{err: errors.New("connection refused")}
	proc := &fakeProcess{}
	spawner := &stuckSpawner{proc: proc}
	sup := New(health, spawner, ServiceConfig{Port: 5000})
	sup.startupTimeout = 20 * time.Millisecond
	sup.pollInterval = time.Millisecond

	if _, err := sup.Ensure(context.Background()); err == nil {
		t.Fatal("Ensure() error = nil, want timeout error")
	}
	if spawner.calls != 1 {
		t.Fatalf("spawner calls = %d, want 1", spawner.calls)
	}
	if !proc.stopped {
		t.Fatal("process was not stopped after a startup timeout")
	}
	if got := sup.Status(); got.Running {
		t.Fatalf("Status().Running = true, want false")
	}
}

func TestStop(t *testing.T) {
	health := &fakeHealth{err: errors.New("connection refused")}
	proc := &fakeProcess{}
	spawner := &fakeSpawner{health: health, proc: proc}
	sup := New(health, spawner, ServiceConfig{Port: 5000})

	if _, err := sup.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v, want nil", err)
	}
	if err := sup.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v, want nil", err)
	}
	if !proc.stopped {
		t.Fatal("process was not stopped")
	}
	if got := sup.Status(); got.Running {
		t.Fatalf("Status().Running = true, want false")
	}
}

func TestStopWithoutProcess(t *testing.T) {
	health := &fakeHealth{}
	sup := New(health, &fakeSpawner{health: health, proc: &fakeProcess{}}, ServiceConfig{Port: 5000})

	if err := sup.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v, want nil", err)
	}
}

func TestRestartSpawnsFreshProcess(t *testing.T) {
	health := &fakeHealth{err: errors.New("connection refused")}
	spawner := &restartSpawner{health: health}
	sup := New(health, spawner, ServiceConfig{Port: 5000})

	if _, err := sup.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v, want nil", err)
	}
	if err := sup.Restart(context.Background()); err != nil {
		t.Fatalf("Restart() error = %v, want nil", err)
	}
	if len(spawner.procs) != 2 {
		t.Fatalf("spawned %d processes, want 2", len(spawner.procs))
	}
	if !spawner.procs[0].stopped {
		t.Fatal("first process was not stopped by Restart")
	}
	if spawner.procs[1].stopped {
		t.Fatal("second process was stopped by Restart")
	}
	if got := sup.Status(); !got.Running || got.Mode != "managed" {
		t.Fatalf("Status() = %+v, want running managed", got)
	}
}

func TestMergeEnvOverridesInheritedValue(t *testing.T) {
	base := []string{"PATH=/bin", "HOME=/root", "KEEP=1"}
	extra := []string{"HOME=/srv", "ADDED=2"}

	got := mergeEnv(base, extra)
	want := []string{"PATH=/bin", "KEEP=1", "HOME=/srv", "ADDED=2"}
	if len(got) != len(want) {
		t.Fatalf("mergeEnv() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mergeEnv() = %v, want %v", got, want)
		}
	}
}

func TestExecSpawnerRequiresBinary(t *testing.T) {
	if _, err := (ExecSpawner{}).Spawn(context.Background(), Spec{}); err == nil {
		t.Fatal("Spawn() error = nil, want missing binary error")
	}
}

func TestExecSpawnerStopsRealProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process supervision is not implemented on windows")
	}

	proc, err := ExecSpawner{}.Spawn(context.Background(), Spec{
		Binary: "sleep",
		Args:   []string{"60"},
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waited := make(chan error, 1)
	go func() { waited <- proc.Wait() }()

	if err := proc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait() did not return after Stop()")
	}
}
