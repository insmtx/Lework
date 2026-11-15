package main

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/agentd/servicefile"
)

// stubTUIDeps returns deps whose fatal paths fail the test. Each test overrides
// only the behavior it exercises.
func stubTUIDeps(t *testing.T) tuiDeps {
	t.Helper()
	return tuiDeps{
		readInfo: func() (servicefile.Info, error) {
			t.Fatal("readInfo was called")
			return servicefile.Info{}, nil
		},
		health: func(context.Context, servicefile.Info) error {
			t.Fatal("health was called")
			return nil
		},
		lookPath: func(string) (string, error) {
			t.Fatal("lookPath was called")
			return "", nil
		},
		run: func(context.Context, string, []string, []string) error {
			t.Fatal("run was called")
			return nil
		},
	}
}

func TestRunOpenCodeTUIMissingServiceFile(t *testing.T) {
	deps := stubTUIDeps(t)
	deps.readInfo = func() (servicefile.Info, error) {
		return servicefile.Info{}, fs.ErrNotExist
	}

	err := runOpenCodeTUIWith(context.Background(), openCodeTUIOptions{}, deps)
	if err == nil {
		t.Fatal("runOpenCodeTUIWith() error = nil, want missing agent error")
	}
	if !strings.Contains(err.Error(), "lework agent") {
		t.Fatalf("error = %v, want a hint to start lework agent", err)
	}
}

func TestRunOpenCodeTUIUnavailableService(t *testing.T) {
	healthErr := errors.New("connection refused")
	deps := stubTUIDeps(t)
	deps.readInfo = func() (servicefile.Info, error) {
		return servicefile.Info{URL: "http://127.0.0.1:4096", Password: "pw"}, nil
	}
	deps.health = func(context.Context, servicefile.Info) error {
		return healthErr
	}

	err := runOpenCodeTUIWith(context.Background(), openCodeTUIOptions{}, deps)
	if !errors.Is(err, healthErr) {
		t.Fatalf("runOpenCodeTUIWith() error = %v, want the health error", err)
	}
}

func TestRunOpenCodeTUIPassesServerSessionAndDirectory(t *testing.T) {
	t.Setenv("OPENCODE_PASSWORD", "inherited")
	t.Setenv("OPENCODE_SERVER_PASSWORD", "inherited-server")

	var gotPath string
	var gotArgs []string
	var gotEnv []string
	called := false

	deps := stubTUIDeps(t)
	deps.readInfo = func() (servicefile.Info, error) {
		return servicefile.Info{URL: "http://127.0.0.1:4096", Password: "pw"}, nil
	}
	deps.health = func(context.Context, servicefile.Info) error { return nil }
	deps.lookPath = func(name string) (string, error) {
		if name != "opencode" {
			t.Fatalf("lookPath(%q), want opencode", name)
		}
		return "/fake/opencode", nil
	}
	deps.run = func(_ context.Context, path string, args, env []string) error {
		called = true
		gotPath = path
		gotArgs = args
		gotEnv = env
		return nil
	}

	opts := openCodeTUIOptions{Directory: "/work/dir", SessionID: "ses_1"}
	if err := runOpenCodeTUIWith(context.Background(), opts, deps); err != nil {
		t.Fatalf("runOpenCodeTUIWith() error = %v", err)
	}
	if !called {
		t.Fatal("run was not called")
	}
	if gotPath != "/fake/opencode" {
		t.Fatalf("run path = %q, want /fake/opencode", gotPath)
	}
	wantArgs := []string{"--server", "http://127.0.0.1:4096", "--session", "ses_1", "/work/dir"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("run args = %v, want %v", gotArgs, wantArgs)
	}
	if !containsEnv(gotEnv, "OPENCODE_PASSWORD=pw") {
		t.Fatalf("run env = %v, want the published password", gotEnv)
	}
	if containsEnv(gotEnv, "OPENCODE_PASSWORD=inherited") ||
		containsEnv(gotEnv, "OPENCODE_SERVER_PASSWORD=inherited-server") {
		t.Fatalf("run env kept an inherited password: %v", gotEnv)
	}
}

func TestRunOpenCodeTUIContinueFlag(t *testing.T) {
	var gotArgs []string
	deps := stubTUIDeps(t)
	deps.readInfo = func() (servicefile.Info, error) {
		return servicefile.Info{URL: "http://127.0.0.1:4096", Password: "pw"}, nil
	}
	deps.health = func(context.Context, servicefile.Info) error { return nil }
	deps.lookPath = func(string) (string, error) { return "/fake/opencode", nil }
	deps.run = func(_ context.Context, _ string, args, _ []string) error {
		gotArgs = args
		return nil
	}

	if err := runOpenCodeTUIWith(context.Background(), openCodeTUIOptions{Continue: true}, deps); err != nil {
		t.Fatalf("runOpenCodeTUIWith() error = %v", err)
	}
	want := []string{"--server", "http://127.0.0.1:4096", "--continue"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("run args = %v, want %v", gotArgs, want)
	}
}

func TestRunOpenCodeTUIBinaryMissing(t *testing.T) {
	deps := stubTUIDeps(t)
	deps.readInfo = func() (servicefile.Info, error) {
		return servicefile.Info{URL: "http://127.0.0.1:4096", Password: "pw"}, nil
	}
	deps.health = func(context.Context, servicefile.Info) error { return nil }
	deps.lookPath = func(string) (string, error) {
		return "", errors.New("executable file not found")
	}

	err := runOpenCodeTUIWith(context.Background(), openCodeTUIOptions{}, deps)
	if err == nil {
		t.Fatal("runOpenCodeTUIWith() error = nil, want lookup error")
	}
	if !strings.Contains(err.Error(), "find opencode executable") {
		t.Fatalf("error = %v, want a lookup error", err)
	}
}

func TestRunOpenCodeTUIWrapsRunError(t *testing.T) {
	deps := stubTUIDeps(t)
	deps.readInfo = func() (servicefile.Info, error) {
		return servicefile.Info{URL: "http://127.0.0.1:4096", Password: "pw"}, nil
	}
	deps.health = func(context.Context, servicefile.Info) error { return nil }
	deps.lookPath = func(string) (string, error) { return "/fake/opencode", nil }
	deps.run = func(context.Context, string, []string, []string) error {
		return errors.New("boom")
	}

	err := runOpenCodeTUIWith(context.Background(), openCodeTUIOptions{}, deps)
	if err == nil {
		t.Fatal("runOpenCodeTUIWith() error = nil, want run error")
	}
	if !strings.Contains(err.Error(), "run opencode") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want a wrapped run error", err)
	}
}

func TestTUIEnvReplacesInheritedPasswords(t *testing.T) {
	got := tuiEnv([]string{
		"HOME=/home/user",
		"OPENCODE_PASSWORD=old",
		"OPENCODE_SERVER_PASSWORD=old-server",
	}, "new")

	want := []string{"HOME=/home/user", "OPENCODE_PASSWORD=new"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tuiEnv() = %v, want %v", got, want)
	}
}

func TestWaitServiceReadyRejectsUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := waitServiceReady(context.Background(), servicefile.Info{URL: srv.URL, Password: "pw"})
	if err == nil {
		t.Fatal("waitServiceReady() error = nil, want unauthorized error")
	}
	if !strings.Contains(err.Error(), "rejected credentials") {
		t.Fatalf("error = %v, want a rejected-credentials error", err)
	}
}

func TestWaitServiceReadyRetriesUntilHealthy(t *testing.T) {
	restore := shortenHealthWait(t)
	defer restore()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "opencode" || pass != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"test"}`))
	}))
	defer srv.Close()

	if err := waitServiceReady(context.Background(), servicefile.Info{URL: srv.URL, Password: "pw"}); err != nil {
		t.Fatalf("waitServiceReady() error = %v", err)
	}
	if got := atomic.LoadInt32(&calls); got < 3 {
		t.Fatalf("health probes = %d, want at least 3", got)
	}
}

func TestWaitServiceReadyTimesOut(t *testing.T) {
	restore := shortenHealthWait(t)
	defer restore()
	serviceHealthTimeout = 40 * time.Millisecond
	serviceHealthPoll = 5 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := waitServiceReady(context.Background(), servicefile.Info{URL: srv.URL, Password: "pw"})
	if err == nil {
		t.Fatal("waitServiceReady() error = nil, want timeout error")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Fatalf("error = %v, want an unavailable-service error", err)
	}
}

func TestNewOpenCodeCommandParsesArgs(t *testing.T) {
	original := runOpenCodeTUICommand
	defer func() { runOpenCodeTUICommand = original }()

	var got openCodeTUIOptions
	runOpenCodeTUICommand = func(_ context.Context, opts openCodeTUIOptions) error {
		got = opts
		return nil
	}

	cmd := newOpenCodeCommand()
	cmd.SetArgs([]string{
		"/work/dir",
		"--session", "ses_1",
		"--continue",
		"--opencode", "/opt/opencode",
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	want := openCodeTUIOptions{
		Binary:    "/opt/opencode",
		Directory: "/work/dir",
		SessionID: "ses_1",
		Continue:  true,
	}
	if got != want {
		t.Fatalf("options = %+v, want %+v", got, want)
	}
}

func TestNewOpenCodeCommandRejectsExtraArgs(t *testing.T) {
	cmd := newOpenCodeCommand()
	cmd.SetArgs([]string{"one", "two"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("ExecuteContext() error = nil, want an argument-count error")
	}
}

// shortenHealthWait makes the retry loop fast enough for unit tests.
func shortenHealthWait(t *testing.T) func() {
	t.Helper()
	oldTimeout := serviceHealthTimeout
	oldPoll := serviceHealthPoll
	oldRequest := serviceHealthRequest
	serviceHealthTimeout = time.Second
	serviceHealthPoll = 5 * time.Millisecond
	serviceHealthRequest = time.Second
	return func() {
		serviceHealthTimeout = oldTimeout
		serviceHealthPoll = oldPoll
		serviceHealthRequest = oldRequest
	}
}

func containsEnv(env []string, want string) bool {
	for _, kv := range env {
		if kv == want {
			return true
		}
	}
	return false
}

func TestRootRegistersOpenCodeCommand(t *testing.T) {
	root := newRootCommand()
	cmd, _, err := root.Find([]string{"opencode"})
	if err != nil {
		t.Fatalf("find opencode command: %v", err)
	}
	if cmd == nil || cmd.Name() != "opencode" {
		t.Fatalf("opencode command = %v, want the registered subcommand", cmd)
	}
	if cmd.Flags().Lookup("config") != nil {
		t.Fatal("legacy --config flag was added to lework opencode")
	}
	if !isStandaloneCommand(cmd) {
		t.Fatal("lework opencode must skip the legacy CLI config setup")
	}
}

func TestRootHelpMentionsOpenCode(t *testing.T) {
	output, err := executeRootCommand("--help")
	if err != nil {
		t.Fatalf("root help error = %v", err)
	}
	if !strings.Contains(output, "opencode") {
		t.Fatalf("root help does not mention opencode:\n%s", output)
	}
}
