//go:build integration

// Package agentd_test contains the live OpenCode verification for the node
// agent. It is gated behind the integration build tag and two environment
// variables so the default `go test ./...` never touches a real model or a
// user's OpenCode data directory.
package agentd_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/insmtx/Lework/backend/internal/agentd/handler"
	"github.com/insmtx/Lework/backend/internal/agentd/opencode"
	"github.com/insmtx/Lework/backend/internal/agentd/protocol"
	"github.com/insmtx/Lework/backend/internal/agentd/runner"
	"github.com/insmtx/Lework/backend/internal/agentd/supervisor"
	"github.com/insmtx/Lework/backend/internal/agentd/transport"
)

const (
	// promptText stays trivial so the run is cheap and the assertion is only
	// "the assistant said something", but the model is free to answer in any
	// language or with extra words.
	promptText = "Reply with the single word OK"

	// runTimeout bounds the whole live test. A cold provider model can take a
	// few seconds; five minutes leaves room without hiding a hang.
	runTimeout = 5 * time.Minute

	// permissionWait bounds a single permission reply so a wedged request
	// cannot stall the event watcher forever.
	permissionWait = 15 * time.Second
)

// TestRealOpenCode drives the real binary end to end: a fake control plane
// completes the WSS hello/welcome handshake, the supervisor starts (and then
// attaches to) one shared service, and a session prompt completes with a
// non-empty assistant message.
//
// Run it with:
//
//	LEWORK_OPENCODE_BIN="$(command -v opencode)" \
//	LEWORK_OPENCODE_TEST_DIR="$(mktemp -d)" \
//	go test -tags integration ./backend/internal/agentd -run TestRealOpenCode -v
func TestRealOpenCode(t *testing.T) {
	binary, testDir := integrationEnv(t)
	port := freeLoopbackPort(t)
	password := randomPassword(t)

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	controlPlane := newFakeControlPlane(t)

	client := opencode.New(
		fmt.Sprintf("http://127.0.0.1:%d", port),
		nil,
		opencode.WithPassword(password),
	)
	spawner := &countingSpawner{inner: supervisor.ExecSpawner{}, password: password}
	service := supervisor.New(client, spawner, supervisor.ServiceConfig{
		Binary: binary,
		Port:   port,
		Dir:    testDir,
	})

	// Every failure between the first Ensure and the explicit Stop must still
	// reap the child. The supervisor starts it with context.WithoutCancel, so
	// the test context alone cannot kill it; Stop is also a no-op when this
	// supervisor did not spawn the process, so an attach-only run stays safe.
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = service.Stop(stopCtx)
	})

	// First Ensure spawns; the second must see the tracked process healthy and
	// attach to it without spawning a duplicate. The mode stays "managed"
	// because the supervisor still owns the process it started.
	status, err := service.Ensure(ctx)
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if !status.Running || status.Mode != "managed" {
		t.Fatalf("first Ensure status = %+v, want running managed", status)
	}
	status, err = service.Ensure(ctx)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if !status.Running {
		t.Fatalf("second Ensure status = %+v, want running", status)
	}
	if got := spawner.Calls(); got != 1 {
		t.Fatalf("spawner calls = %d, want 1 (second Ensure must attach)", got)
	}
	t.Logf("second Ensure attached without respawn (mode=%s, calls=%d)", status.Mode, spawner.Calls())

	// A second supervisor (another agent on the same node) must attach to the
	// healthy service and never spawn.
	foreignSpawner := &failingSpawner{}
	foreign := supervisor.New(client, foreignSpawner, supervisor.ServiceConfig{
		Binary: binary,
		Port:   port,
		Dir:    testDir,
	})
	status, err = foreign.Ensure(ctx)
	if err != nil {
		t.Fatalf("foreign Ensure: %v", err)
	}
	if status.Mode != "attached" {
		t.Fatalf("foreign status.Mode = %q, want attached", status.Mode)
	}
	if got := foreignSpawner.Calls(); got != 0 {
		t.Fatalf("foreign spawner calls = %d, want 0", got)
	}

	// Exercise the control-plane handshake with the real transport and runner.
	handshake := startRunner(t, ctx, controlPlane, client, service)

	// Create the session in the provided directory, then prompt it.
	session, err := client.CreateSession(ctx, testDir)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if session.ID == "" {
		t.Fatal("create session returned an empty id")
	}
	if !sameDirectory(session.Directory, testDir) {
		t.Fatalf("session directory = %q, want %q", session.Directory, testDir)
	}

	// Watch permission requests and terminal events for this session. It must
	// be running before the prompt, otherwise a permission-gated run would
	// block with no one to answer it and the execution event could be missed.
	watch := startEventWatcher(ctx, client, session.ID)

	receipt, err := client.Prompt(ctx, session.ID, promptText)
	if err != nil {
		watch.Stop()
		t.Fatalf("prompt: %v", err)
	}
	if receipt == "" {
		watch.Stop()
		t.Fatal("prompt returned an empty admission id")
	}
	t.Logf("prompt admitted as %s", receipt)

	// Prefer the explicit wait contract; fall back to the event feed when the
	// experimental endpoint is unavailable.
	if err := client.Wait(ctx, session.ID); err != nil {
		if !errors.Is(err, opencode.ErrExperimentalUnavailable) {
			watch.Stop()
			t.Fatalf("wait: %v", err)
		}
		t.Logf("wait endpoint unavailable; falling back to events: %v", err)
		select {
		case outcome := <-watch.outcome:
			t.Logf("terminal event %s", outcome)
		case err := <-watch.errs:
			watch.Stop()
			t.Fatalf("event watcher: %v", err)
		case <-ctx.Done():
			watch.Stop()
			t.Fatalf("timed out waiting for a terminal event: %v", ctx.Err())
		}
	} else {
		t.Log("wait returned after the session became idle")
	}

	// Read the turn back. The assistant message is the authoritative text; the
	// idle turn carries the terminal outcome.
	assistant, idleOutcome, idleError, messages := readTurn(t, ctx, client, session.ID)
	t.Logf("permission replies=%d idle=%q assistant=%q", watch.Replies(), idleOutcome, assistant)
	if assistant == "" {
		watch.Stop()
		t.Fatalf("no non-empty assistant message; messages=%s idle=%q error=%v", describeMessages(messages), idleOutcome, idleError)
	}
	if idleOutcome != "" && idleOutcome != "succeeded" {
		watch.Stop()
		t.Fatalf("session idle outcome = %q, want succeeded (error=%v)", idleOutcome, idleError)
	}
	watch.Stop()

	// Shut the runner down cleanly before stopping the service: the runner's
	// event forwarder treats a closed feed as a gap signal, which is the right
	// production behavior but would mask shutdown ordering here.
	handshake.stop(t)

	// Stop the shared service and prove the child process really exited.
	process := spawner.LastProcess()
	if process == nil {
		t.Fatal("spawner recorded no process")
	}
	if err := service.Stop(context.Background()); err != nil {
		t.Fatalf("stop service: %v", err)
	}
	if service.Status().Running {
		t.Fatal("service status still running after Stop")
	}
	waitFor(t, 10*time.Second, "opencode health to fail", func() bool {
		return client.Health(context.Background()) != nil
	})
	exited := make(chan struct{})
	go func() {
		_ = process.Wait()
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("opencode process did not exit after Stop")
	}
	t.Log("opencode process exited after Stop")
}

// integrationEnv reads and validates the two required environment variables.
// A missing variable skips, because this test needs a real binary and an
// isolated working directory; an invalid value fails loudly instead of
// silently skipping a broken harness.
func integrationEnv(t *testing.T) (binary, testDir string) {
	t.Helper()

	binary = os.Getenv("LEWORK_OPENCODE_BIN")
	testDir = os.Getenv("LEWORK_OPENCODE_TEST_DIR")
	if binary == "" || testDir == "" {
		t.Skip("set LEWORK_OPENCODE_BIN and LEWORK_OPENCODE_TEST_DIR to run the live OpenCode test")
	}

	info, err := os.Stat(binary)
	if err != nil {
		t.Fatalf("LEWORK_OPENCODE_BIN %q: %v", binary, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		t.Fatalf("LEWORK_OPENCODE_BIN %q is not an executable file", binary)
	}

	if !filepath.IsAbs(testDir) {
		t.Fatalf("LEWORK_OPENCODE_TEST_DIR %q must be absolute", testDir)
	}
	entries, err := os.ReadDir(testDir)
	if err != nil {
		t.Fatalf("LEWORK_OPENCODE_TEST_DIR %q: %v", testDir, err)
	}
	if len(entries) != 0 {
		t.Fatalf("LEWORK_OPENCODE_TEST_DIR %q must be empty, found %d entries", testDir, len(entries))
	}
	return binary, testDir
}

// freeLoopbackPort reserves and releases a port so opencode can bind it. The
// gap between close and bind is unavoidable without passing a listener, which
// opencode does not support.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release loopback port: %v", err)
	}
	return port
}

// randomPassword creates the per-process basic-auth secret.
func randomPassword(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate password: %v", err)
	}
	return hex.EncodeToString(buf)
}

// sameDirectory compares two paths after resolving symlinks, because macOS
// reports /private/var/... for a /var/... input.
func sameDirectory(got, want string) bool {
	if got == want {
		return true
	}
	gotReal, gotErr := filepath.EvalSymlinks(got)
	wantReal, wantErr := filepath.EvalSymlinks(want)
	return gotErr == nil && wantErr == nil && gotReal == wantReal
}

// countingSpawner adapts the production spec to a stateless `opencode serve`
// launch and records how many processes were spawned.
//
// Why the adaptation: opencode v2.0.32 in --service mode ignores
// OPENCODE_SERVER_PASSWORD, reads its password from a per-user
// $XDG_CONFIG_HOME/opencode/service.json, and replaces (and therefore stops)
// any other service registered in that directory. Spawning it from this test
// would hijack the user's TUI service. Plain `serve` with an explicit
// OPENCODE_SERVER_PASSWORD is isolated and still exercises the supervisor's
// real spawn/attach/stop lifecycle. The production supervisor still passes
// --service; that gap is recorded in the I0 findings.
type countingSpawner struct {
	inner    supervisor.Spawner
	password string

	mu   sync.Mutex
	call int
	last supervisor.Process
}

// Spawn strips --service, injects the password, and delegates to ExecSpawner.
func (s *countingSpawner) Spawn(ctx context.Context, spec supervisor.Spec) (supervisor.Process, error) {
	s.mu.Lock()
	s.call++
	s.mu.Unlock()

	spec.Args = withoutServiceFlag(spec.Args)
	spec.Env = append(spec.Env, "OPENCODE_SERVER_PASSWORD="+s.password)

	process, err := s.inner.Spawn(ctx, spec)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.last = process
	s.mu.Unlock()
	return process, nil
}

// Calls reports how many times Spawn was invoked.
func (s *countingSpawner) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.call
}

// LastProcess returns the process from the most recent Spawn.
func (s *countingSpawner) LastProcess() supervisor.Process {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// withoutServiceFlag removes --service while preserving the hostname/port
// arguments the supervisor built.
func withoutServiceFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--service" {
			continue
		}
		out = append(out, arg)
	}
	return out
}

// failingSpawner proves the foreign-attach path never spawns.
type failingSpawner struct{ calls atomic.Int32 }

func (s *failingSpawner) Spawn(context.Context, supervisor.Spec) (supervisor.Process, error) {
	s.calls.Add(1)
	return nil, errors.New("integration: foreign supervisor must not spawn")
}

func (s *failingSpawner) Calls() int { return int(s.calls.Load()) }

// fakeControlPlane accepts one WebSocket, validates hello, and answers welcome.
type fakeControlPlane struct {
	srv    *httptest.Server
	hello  chan protocol.Frame
	frames chan protocol.Frame
}

func newFakeControlPlane(t *testing.T) *fakeControlPlane {
	t.Helper()

	cp := &fakeControlPlane{
		hello:  make(chan protocol.Frame, 1),
		frames: make(chan protocol.Frame, 64),
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	cp.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		var hello protocol.Frame
		if err := conn.ReadJSON(&hello); err != nil {
			return
		}
		select {
		case cp.hello <- hello:
		default:
		}
		if err := conn.WriteJSON(protocol.Frame{Type: protocol.FrameWelcome, ID: hello.ID}); err != nil {
			return
		}

		// Drain frames (heartbeats, forwarded events) so the runner never
		// blocks on a full socket.
		for {
			var frame protocol.Frame
			if err := conn.ReadJSON(&frame); err != nil {
				return
			}
			select {
			case cp.frames <- frame:
			default:
			}
		}
	}))
	t.Cleanup(cp.srv.Close)
	return cp
}

// fingerprint returns the SHA-256 pin the transport needs for httptest's
// self-signed certificate.
func (cp *fakeControlPlane) fingerprint(t *testing.T) string {
	t.Helper()

	sum := sha256.Sum256(cp.srv.Certificate().Raw)
	return hex.EncodeToString(sum[:])
}

// url converts the httptest HTTPS URL into a WSS endpoint.
func (cp *fakeControlPlane) url() string {
	return "wss" + strings.TrimPrefix(cp.srv.URL, "https")
}

// handshake tracks the live runner connection.
type handshake struct {
	conn   *transport.Client
	cancel context.CancelFunc
	done   chan error
}

// startRunner dials the fake control plane and runs one agent connection. It
// waits for hello, asserts the advertised capabilities, and returns a handle
// the test uses to shut the connection down.
func startRunner(
	t *testing.T,
	ctx context.Context,
	controlPlane *fakeControlPlane,
	client *opencode.Client,
	service *supervisor.Supervisor,
) *handshake {
	t.Helper()

	conn, err := transport.Dial(ctx, transport.DialConfig{
		URL:            controlPlane.url(),
		TLSFingerprint: controlPlane.fingerprint(t),
	})
	if err != nil {
		t.Fatalf("dial fake control plane: %v", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	requestHandler := handler.New(client, service)
	go func() {
		done <- runner.New(conn, requestHandler, client, runner.RunnerConfig{
			NodeID:       "i0-integration-node",
			AgentVersion: "i0-integration",
			Heartbeat:    2 * time.Second,
		}).Run(runCtx)
	}()

	select {
	case hello := <-controlPlane.hello:
		if hello.Type != protocol.FrameHello {
			cancel()
			t.Fatalf("control plane received %q, want hello", hello.Type)
		}
		var params struct {
			Capabilities []string `json:"capabilities"`
		}
		if err := json.Unmarshal(hello.Params, &params); err != nil {
			cancel()
			t.Fatalf("decode hello params: %v", err)
		}
		if !contains(params.Capabilities, "session.prompt") || !contains(params.Capabilities, "permission.reply") {
			cancel()
			t.Fatalf("hello capabilities = %v, want session.prompt and permission.reply", params.Capabilities)
		}
		t.Logf("control plane accepted hello with %d capabilities", len(params.Capabilities))
	case <-time.After(15 * time.Second):
		cancel()
		t.Fatal("timed out waiting for the agent hello frame")
	}

	return &handshake{conn: conn, cancel: cancel, done: done}
}

// stop cancels the runner and asserts the connection returns cleanly.
func (h *handshake) stop(t *testing.T) {
	t.Helper()

	h.cancel()
	_ = h.conn.Close()
	select {
	case err := <-h.done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("runner returned %v, want a clean shutdown", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runner did not stop after cancellation")
	}
}

// eventWatcher replies "once" to permission requests and reports the terminal
// execution event. It is also the fallback path when Wait is unavailable.
type eventWatcher struct {
	client    *opencode.Client
	sessionID string
	outcome   chan string
	errs      chan error
	replies   atomic.Int32

	cancel context.CancelFunc
	done   chan struct{}
}

func startEventWatcher(parent context.Context, client *opencode.Client, sessionID string) *eventWatcher {
	ctx, cancel := context.WithCancel(parent)
	watcher := &eventWatcher{
		client:    client,
		sessionID: sessionID,
		outcome:   make(chan string, 1),
		errs:      make(chan error, 1),
		cancel:    cancel,
		done:      make(chan struct{}),
	}

	events, err := client.SubscribeEvents(ctx)
	if err != nil {
		watcher.errs <- err
		close(watcher.done)
		return watcher
	}

	go func() {
		defer close(watcher.done)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				watcher.handle(ctx, event)
			}
		}
	}()
	return watcher
}

func (w *eventWatcher) handle(ctx context.Context, event opencode.Event) {
	if event.SessionID != w.sessionID {
		return
	}

	switch event.Type {
	case "permission.asked":
		var request struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(event.Data, &request); err != nil || request.ID == "" {
			return
		}
		replyCtx, cancel := context.WithTimeout(ctx, permissionWait)
		defer cancel()
		if err := w.client.ReplyPermission(replyCtx, w.sessionID, request.ID, "once", ""); err != nil {
			select {
			case w.errs <- fmt.Errorf("reply to permission %s: %w", request.ID, err):
			default:
			}
			return
		}
		w.replies.Add(1)

	case "session.execution.succeeded", "session.execution.failed", "session.execution.interrupted":
		select {
		case w.outcome <- event.Type:
		default:
		}
	}
}

// Replies reports how many permission requests were answered.
func (w *eventWatcher) Replies() int { return int(w.replies.Load()) }

// Stop unsubscribes and waits for the watcher goroutine to finish.
func (w *eventWatcher) Stop() {
	w.cancel()
	<-w.done
}

// readTurn polls the message list until an assistant reply appears or the
// context ends. Polling absorbs the small gap between the terminal event and
// the committed message row.
func readTurn(t *testing.T, ctx context.Context, client *opencode.Client, sessionID string) (assistant, idleOutcome string, idleError *opencode.MessageError, messages []opencode.Message) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for {
		var err error
		messages, err = client.Messages(ctx, sessionID)
		if err != nil {
			t.Fatalf("read messages: %v", err)
		}

		for _, message := range messages {
			switch message.Role {
			case "assistant":
				if strings.TrimSpace(message.Text) != "" {
					assistant = message.Text
				}
			case "idle":
				idleOutcome = message.Outcome
				idleError = message.Error
			}
		}

		if assistant != "" || idleOutcome == "failed" || time.Now().After(deadline) {
			return assistant, idleOutcome, idleError, messages
		}
		select {
		case <-ctx.Done():
			return assistant, idleOutcome, idleError, messages
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// describeMessages renders just enough of the turn to diagnose a failure
// without dumping provider payloads.
func describeMessages(messages []opencode.Message) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		parts = append(parts, fmt.Sprintf("%s(%q)", message.Role, message.Text))
	}
	return strings.Join(parts, ", ")
}

// waitFor polls condition until it is true or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, description string, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if condition() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s after %s", description, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
