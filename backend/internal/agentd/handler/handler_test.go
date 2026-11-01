package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insmtx/Lework/backend/internal/agentd/opencode"
	"github.com/insmtx/Lework/backend/internal/agentd/supervisor"
)

// fakeOpenCode records every call so the handlers' argument plumbing can be
// asserted without a real HTTP server.
type fakeOpenCode struct {
	prompts int

	promptSessionID string
	promptText      string
	promptReceipt   string
	promptErr       error

	listCalls int
	sessions  []opencode.Session

	createdDirectory string
	createCalls      int
	createSession    opencode.Session

	messagesCalls     int
	messagesSessionID string
	messages          []opencode.Message

	replyCalls        int
	replySessionID    string
	replyPermissionID string
	replyDecision     string
	replyMessage      string

	diffCalls     int
	diffSessionID string
	diffDirectory string
	diffFrom      string
	diffTo        string
	diffContext   int
	diffs         []opencode.FileDiff
	diffErr       error

	interruptCalls     int
	interruptSessionID string
	interruptResume    bool
	interruptResult    opencode.InterruptResult
	interruptErr       error

	listFilesCalls     int
	listFilesDirectory string
	listFilesPath      string
	files              []opencode.FileInfo
	listFilesErr       error

	readFileCalls     int
	readFileDirectory string
	readFilePath      string
	readFileMaxBytes  int64
	readFileContent   opencode.FileContent
	readFileErr       error

	createTerminalCalls   int
	createTerminalSession string
	createTerminalReq     opencode.CreateTerminalRequest
	createTerminal        opencode.PersistentTerminal
	createTerminalErr     error

	resizeTerminalCalls int
	resizeTerminalID    string
	resizeTerminalSize  opencode.TerminalSize
	closeTerminalCalls  int
	closeTerminalID     string
}

func (f *fakeOpenCode) ListSessions(context.Context) ([]opencode.Session, error) {
	f.listCalls++
	return f.sessions, nil
}

func (f *fakeOpenCode) CreateSession(_ context.Context, directory string) (opencode.Session, error) {
	f.createCalls++
	f.createdDirectory = directory
	return f.createSession, nil
}

func (f *fakeOpenCode) Prompt(_ context.Context, sessionID, text string) (string, error) {
	f.prompts++
	f.promptSessionID = sessionID
	f.promptText = text
	if f.promptErr != nil {
		return "", f.promptErr
	}
	if f.promptReceipt != "" {
		return f.promptReceipt, nil
	}
	return "receipt-1", nil
}

func (f *fakeOpenCode) Messages(_ context.Context, sessionID string) ([]opencode.Message, error) {
	f.messagesCalls++
	f.messagesSessionID = sessionID
	return f.messages, nil
}

func (f *fakeOpenCode) ReplyPermission(
	_ context.Context,
	sessionID, permissionID, decision, message string,
) error {
	f.replyCalls++
	f.replySessionID = sessionID
	f.replyPermissionID = permissionID
	f.replyDecision = decision
	f.replyMessage = message
	return nil
}

func (f *fakeOpenCode) SessionDiff(_ context.Context, sessionID, from, to string, contextLines int) ([]opencode.FileDiff, error) {
	f.diffCalls++
	f.diffSessionID = sessionID
	f.diffFrom = from
	f.diffTo = to
	f.diffContext = contextLines
	return f.diffs, f.diffErr
}

func (f *fakeOpenCode) Interrupt(_ context.Context, sessionID string, resume bool) (opencode.InterruptResult, error) {
	f.interruptCalls++
	f.interruptSessionID = sessionID
	f.interruptResume = resume
	return f.interruptResult, f.interruptErr
}

func (f *fakeOpenCode) ListFiles(_ context.Context, directory, path string) ([]opencode.FileInfo, error) {
	f.listFilesCalls++
	f.listFilesDirectory = directory
	f.listFilesPath = path
	return f.files, f.listFilesErr
}

func (f *fakeOpenCode) ReadFile(_ context.Context, directory, path string, maxBytes int64) (opencode.FileContent, error) {
	f.readFileCalls++
	f.readFileDirectory = directory
	f.readFilePath = path
	f.readFileMaxBytes = maxBytes
	return f.readFileContent, f.readFileErr
}

func (f *fakeOpenCode) CreatePersistentTerminal(_ context.Context, sessionID string, req opencode.CreateTerminalRequest) (opencode.PersistentTerminal, error) {
	f.createTerminalCalls++
	f.createTerminalSession = sessionID
	f.createTerminalReq = req
	return f.createTerminal, f.createTerminalErr
}

func (f *fakeOpenCode) ConnectPersistentTerminal(context.Context, string) (*opencode.TerminalConn, error) {
	return nil, errors.New("fakeOpenCode: terminal connection is not available")
}

func (f *fakeOpenCode) ResizePersistentTerminal(_ context.Context, ptyID string, size opencode.TerminalSize) error {
	f.resizeTerminalCalls++
	f.resizeTerminalID = ptyID
	f.resizeTerminalSize = size
	return nil
}

func (f *fakeOpenCode) ClosePersistentTerminal(_ context.Context, ptyID string) error {
	f.closeTerminalCalls++
	f.closeTerminalID = ptyID
	return nil
}

type fakeSupervisorCalls struct {
	ensure  int
	restart int
	stop    int
}

// fakeSupervisor uses a value receiver so the zero value already satisfies
// SupervisorAPI; counters are optional through calls.
type fakeSupervisor struct {
	status     supervisor.Status
	ensureErr  error
	restartErr error
	stopErr    error
	calls      *fakeSupervisorCalls
}

func (f fakeSupervisor) Ensure(context.Context) (supervisor.Status, error) {
	if f.calls != nil {
		f.calls.ensure++
	}
	return f.status, f.ensureErr
}

func (f fakeSupervisor) Restart(context.Context) error {
	if f.calls != nil {
		f.calls.restart++
	}
	return f.restartErr
}

func (f fakeSupervisor) Stop(context.Context) error {
	if f.calls != nil {
		f.calls.stop++
	}
	return f.stopErr
}

func (f fakeSupervisor) Status() supervisor.Status {
	return f.status
}

func TestPromptIdempotency(t *testing.T) {
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{})
	params := json.RawMessage(`{"session_id":"ses_1","text":"hi","idempotency_key":"k1"}`)

	for i := 0; i < 2; i++ {
		result, err := h.Handle(context.Background(), "session.prompt", params)
		if err != nil {
			t.Fatal(err)
		}
		if result != "receipt-1" {
			t.Fatalf("result = %v, want receipt-1", result)
		}
	}
	if oc.prompts != 1 {
		t.Fatalf("prompts = %d, want 1", oc.prompts)
	}
	if oc.promptSessionID != "ses_1" || oc.promptText != "hi" {
		t.Fatalf("prompt args = %q/%q, want ses_1/hi", oc.promptSessionID, oc.promptText)
	}
}

func TestPromptIdempotencySeparatesSessions(t *testing.T) {
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{})

	for _, sessionID := range []string{"ses_1", "ses_2"} {
		params := json.RawMessage(`{"session_id":"` + sessionID + `","text":"hi","idempotency_key":"k1"}`)
		if _, err := h.Handle(context.Background(), "session.prompt", params); err != nil {
			t.Fatal(err)
		}
	}
	if oc.prompts != 2 {
		t.Fatalf("prompts = %d, want 2", oc.prompts)
	}
}

func TestPromptRequiresIdempotencyKey(t *testing.T) {
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{})

	_, err := h.Handle(
		context.Background(),
		"session.prompt",
		json.RawMessage(`{"session_id":"ses_1","text":"hi"}`),
	)
	if err == nil || err.Error() != "idempotency_key is required" {
		t.Fatalf("err = %v, want idempotency_key is required", err)
	}
	if oc.prompts != 0 {
		t.Fatalf("prompts = %d, want 0", oc.prompts)
	}
}

func TestPromptDoesNotCacheFailure(t *testing.T) {
	oc := &fakeOpenCode{promptErr: errors.New("boom")}
	h := New(oc, fakeSupervisor{})
	params := json.RawMessage(`{"session_id":"ses_1","text":"hi","idempotency_key":"k1"}`)

	if _, err := h.Handle(context.Background(), "session.prompt", params); err == nil {
		t.Fatal("expected error")
	}

	oc.promptErr = nil
	result, err := h.Handle(context.Background(), "session.prompt", params)
	if err != nil {
		t.Fatal(err)
	}
	if result != "receipt-1" {
		t.Fatalf("result = %v, want receipt-1", result)
	}
	if oc.prompts != 2 {
		t.Fatalf("prompts = %d, want 2", oc.prompts)
	}
}

// blockingOpenCode keeps the first Prompt call inside the handler until the
// test has admitted a second caller, so the single-flight path is exercised
// deterministically instead of depending on scheduler timing.
type blockingOpenCode struct {
	fakeOpenCode
	calls   int32
	entered chan struct{}
	release chan struct{}
	err     error
}

func (f *blockingOpenCode) Prompt(_ context.Context, _, _ string) (string, error) {
	atomic.AddInt32(&f.calls, 1)
	f.entered <- struct{}{}
	<-f.release
	return "receipt-concurrent", f.err
}

// waitSignalContext reports when Done is first observed. idempotencyStore.do
// evaluates Done only after it joins an in-flight admission, which lets the
// test prove the second caller reached the wait path before releasing the
// leader.
type waitSignalContext struct {
	context.Context
	once    sync.Once
	entered chan struct{}
}

func (c *waitSignalContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

type promptOutcome struct {
	receipt string
	err     error
}

func handlePrompt(h *Handler, ctx context.Context, params json.RawMessage) promptOutcome {
	result, err := h.Handle(ctx, "session.prompt", params)
	receipt, _ := result.(string)
	return promptOutcome{receipt: receipt, err: err}
}

// runConcurrentPrompt starts one prompt, waits until it is blocked inside
// Prompt, then starts a second caller with the same key and waits until that
// caller is parked on the in-flight admission. Only then is the leader
// released, so both outcomes belong to the same admission.
func runConcurrentPrompt(t *testing.T, h *Handler, oc *blockingOpenCode, params json.RawMessage) []promptOutcome {
	t.Helper()

	release := sync.OnceFunc(func() { close(oc.release) })
	defer release()

	results := make(chan promptOutcome, 2)
	go func() { results <- handlePrompt(h, context.Background(), params) }()

	select {
	case <-oc.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first caller did not enter Prompt")
	}

	waiterCtx := &waitSignalContext{Context: context.Background(), entered: make(chan struct{})}
	go func() { results <- handlePrompt(h, waiterCtx, params) }()

	// The healthy path signals from the in-flight wait. A second Prompt entry
	// is also accepted so a regression is reported as a duplicate call rather
	// than as a timeout.
	select {
	case <-waiterCtx.entered:
	case <-oc.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("second caller did not enter the handler")
	}

	release()

	outcomes := make([]promptOutcome, 0, 2)
	for i := 0; i < 2; i++ {
		select {
		case outcome := <-results:
			outcomes = append(outcomes, outcome)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for a caller result")
		}
	}
	return outcomes
}

func TestPromptIdempotencyConcurrent(t *testing.T) {
	oc := &blockingOpenCode{
		entered: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	h := New(oc, fakeSupervisor{})
	params := json.RawMessage(`{"session_id":"ses_1","text":"hi","idempotency_key":"k1"}`)

	outcomes := runConcurrentPrompt(t, h, oc, params)
	for i, outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("caller %d err = %v", i, outcome.err)
		}
		if outcome.receipt != "receipt-concurrent" {
			t.Fatalf("caller %d receipt = %q, want receipt-concurrent", i, outcome.receipt)
		}
	}
	if got := atomic.LoadInt32(&oc.calls); got != 1 {
		t.Fatalf("prompt calls = %d, want 1", got)
	}
}

func TestPromptIdempotencyConcurrentErrorNotCached(t *testing.T) {
	wantErr := errors.New("admission failed")
	oc := &blockingOpenCode{
		entered: make(chan struct{}, 2),
		release: make(chan struct{}),
		err:     wantErr,
	}
	h := New(oc, fakeSupervisor{})
	params := json.RawMessage(`{"session_id":"ses_1","text":"hi","idempotency_key":"k1"}`)

	outcomes := runConcurrentPrompt(t, h, oc, params)
	for i, outcome := range outcomes {
		if !errors.Is(outcome.err, wantErr) {
			t.Fatalf("caller %d err = %v, want %v", i, outcome.err, wantErr)
		}
	}
	if got := atomic.LoadInt32(&oc.calls); got != 1 {
		t.Fatalf("prompt calls = %d, want 1", got)
	}

	// The shared failure must not be cached: a later retry admits again.
	if _, err := h.Handle(context.Background(), "session.prompt", params); !errors.Is(err, wantErr) {
		t.Fatalf("retry err = %v, want %v", err, wantErr)
	}
	if got := atomic.LoadInt32(&oc.calls); got != 2 {
		t.Fatalf("prompt calls after retry = %d, want 2", got)
	}
}

func TestPromptRejectsMalformedParams(t *testing.T) {
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{})

	_, err := h.Handle(context.Background(), "session.prompt", json.RawMessage(`{"session_id":`))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "session_id") {
		t.Fatalf("error should not echo params: %v", err)
	}
	if oc.prompts != 0 {
		t.Fatalf("prompts = %d, want 0", oc.prompts)
	}
}

func TestIdempotencyStoreExpires(t *testing.T) {
	now := time.Unix(0, 0)
	store := newIdempotencyStore(10 * time.Minute)
	store.now = func() time.Time { return now }

	store.put("ses_1\x00k1", "receipt-1")
	if receipt, ok := store.get("ses_1\x00k1"); !ok || receipt != "receipt-1" {
		t.Fatalf("get = %q/%v, want receipt-1/true", receipt, ok)
	}

	now = now.Add(10*time.Minute + time.Second)
	if _, ok := store.get("ses_1\x00k1"); ok {
		t.Fatal("expected expired entry to be treated as missing")
	}
}

func TestIdempotencyStoreCleansUpOnWrite(t *testing.T) {
	now := time.Unix(0, 0)
	store := newIdempotencyStore(10 * time.Minute)
	store.now = func() time.Time { return now }

	store.put("old", "receipt-old")
	now = now.Add(11 * time.Minute)
	store.put("fresh", "receipt-fresh")

	if _, ok := store.entries["old"]; ok {
		t.Fatal("expected expired entry to be removed on write")
	}
	if _, ok := store.entries["fresh"]; !ok {
		t.Fatal("expected fresh entry to be kept")
	}
}

func TestUnknownMethod(t *testing.T) {
	_, err := New(&fakeOpenCode{}, fakeSupervisor{}).Handle(context.Background(), "nope", nil)
	if !errors.Is(err, ErrUnknownMethod) {
		t.Fatalf("err = %v, want ErrUnknownMethod", err)
	}
	if !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("err = %v, want method name", err)
	}
}

func TestNodeInfo(t *testing.T) {
	result, err := New(&fakeOpenCode{}, fakeSupervisor{}).Handle(context.Background(), "node.info", nil)
	if err != nil {
		t.Fatal(err)
	}
	info, ok := result.(NodeInfo)
	if !ok {
		t.Fatalf("result type = %T, want NodeInfo", result)
	}
	if info.OS != runtime.GOOS {
		t.Fatalf("os = %q, want %q", info.OS, runtime.GOOS)
	}
	if info.Arch != runtime.GOARCH {
		t.Fatalf("arch = %q, want %q", info.Arch, runtime.GOARCH)
	}
	if info.NodeName == "" {
		t.Fatal("node_name is empty")
	}
	if info.AgentVersion == "" {
		t.Fatal("agent_version is empty")
	}

	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"node_name"`, `"os"`, `"arch"`, `"agent_version"`} {
		if !strings.Contains(string(encoded), key) {
			t.Fatalf("node.info JSON %s missing %s", encoded, key)
		}
	}
}

func TestSessionListPassesThrough(t *testing.T) {
	oc := &fakeOpenCode{sessions: []opencode.Session{{ID: "ses_1"}}}
	h := New(oc, fakeSupervisor{})

	result, err := h.Handle(
		context.Background(),
		"session.list",
		json.RawMessage(`{"directory":"/work/project"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	sessions, ok := result.([]opencode.Session)
	if !ok {
		t.Fatalf("result type = %T, want []opencode.Session", result)
	}
	if oc.listCalls != 1 {
		t.Fatalf("list calls = %d, want 1", oc.listCalls)
	}
	if len(sessions) != 1 || sessions[0].ID != "ses_1" {
		t.Fatalf("sessions = %#v, want one ses_1", sessions)
	}
}

func TestSessionCreatePassesDirectory(t *testing.T) {
	oc := &fakeOpenCode{createSession: opencode.Session{ID: "ses_new"}}
	h := New(oc, fakeSupervisor{})

	result, err := h.Handle(
		context.Background(),
		"session.create",
		json.RawMessage(`{"directory":"/work/project"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if oc.createdDirectory != "/work/project" {
		t.Fatalf("directory = %q, want /work/project", oc.createdDirectory)
	}
	session, ok := result.(opencode.Session)
	if !ok || session.ID != "ses_new" {
		t.Fatalf("result = %#v, want session ses_new", result)
	}
}

func TestSessionCreateAllowsDirectoryInsideWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{root, filepath.Join(root, "project")} {
		t.Run(filepath.Base(directory), func(t *testing.T) {
			oc := &fakeOpenCode{createSession: opencode.Session{ID: "ses_new"}}
			h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

			params, err := json.Marshal(SessionCreateParams{Directory: directory})
			if err != nil {
				t.Fatal(err)
			}
			result, err := h.Handle(context.Background(), "session.create", params)
			if err != nil {
				t.Fatalf("Handle() error = %v, want nil", err)
			}
			if oc.createCalls != 1 || oc.createdDirectory != directory {
				t.Fatalf("create = %d/%q, want 1/%q", oc.createCalls, oc.createdDirectory, directory)
			}
			session, ok := result.(opencode.Session)
			if !ok || session.ID != "ses_new" {
				t.Fatalf("result = %#v, want session ses_new", result)
			}
		})
	}
}

func TestSessionCreateRejectsOutsideWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, err := json.Marshal(SessionCreateParams{Directory: outside})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.Handle(context.Background(), "session.create", params)
	if err == nil || !strings.Contains(err.Error(), "outside workspace_roots") {
		t.Fatalf("Handle() error = %v, want outside workspace_roots", err)
	}
	if oc.createCalls != 0 {
		t.Fatalf("create calls = %d, want 0 for a rejected directory", oc.createCalls)
	}
}

func TestSessionCreateRejectsEmptyDirectoryWithWorkspaceRoots(t *testing.T) {
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(t.TempDir()))

	_, err := h.Handle(context.Background(), "session.create", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "directory is required") {
		t.Fatalf("Handle() error = %v, want directory is required", err)
	}
	if oc.createCalls != 0 {
		t.Fatalf("create calls = %d, want 0 for an empty directory", oc.createCalls)
	}
}

func TestSessionMessagesPassesSessionID(t *testing.T) {
	oc := &fakeOpenCode{messages: []opencode.Message{{ID: "msg_1"}}}
	h := New(oc, fakeSupervisor{})

	result, err := h.Handle(
		context.Background(),
		"session.messages",
		json.RawMessage(`{"session_id":"ses_1"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if oc.messagesSessionID != "ses_1" {
		t.Fatalf("session_id = %q, want ses_1", oc.messagesSessionID)
	}
	messages, ok := result.([]opencode.Message)
	if !ok || len(messages) != 1 || messages[0].ID != "msg_1" {
		t.Fatalf("result = %#v, want one msg_1", result)
	}
}

func TestPermissionReplyPassesFields(t *testing.T) {
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{})

	_, err := h.Handle(
		context.Background(),
		"permission.reply",
		json.RawMessage(`{"session_id":"ses_1","permission_id":"per_1","decision":"once","message":"ok"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if oc.replyCalls != 1 {
		t.Fatalf("reply calls = %d, want 1", oc.replyCalls)
	}
	if oc.replySessionID != "ses_1" || oc.replyPermissionID != "per_1" ||
		oc.replyDecision != "once" || oc.replyMessage != "ok" {
		t.Fatalf("reply args = %#v", oc)
	}
}

func TestOpenCodeLifecycleDispatch(t *testing.T) {
	calls := &fakeSupervisorCalls{}
	status := supervisor.Status{Running: true, Mode: "managed", BaseURL: "http://127.0.0.1:4096"}
	h := New(&fakeOpenCode{}, fakeSupervisor{status: status, calls: calls})

	result, err := h.Handle(context.Background(), "opencode.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := result.(supervisor.Status); !ok || got != status {
		t.Fatalf("status result = %#v, want %#v", result, status)
	}

	result, err = h.Handle(context.Background(), "opencode.start", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := result.(supervisor.Status); !ok || got != status {
		t.Fatalf("start result = %#v, want %#v", result, status)
	}
	if calls.ensure != 1 {
		t.Fatalf("ensure calls = %d, want 1", calls.ensure)
	}

	if _, err := h.Handle(context.Background(), "opencode.restart", nil); err != nil {
		t.Fatal(err)
	}
	if calls.restart != 1 {
		t.Fatalf("restart calls = %d, want 1", calls.restart)
	}

	if _, err := h.Handle(context.Background(), "opencode.stop", nil); err != nil {
		t.Fatal(err)
	}
	if calls.stop != 1 {
		t.Fatalf("stop calls = %d, want 1", calls.stop)
	}
}

func TestOpenCodeLifecyclePropagatesErrors(t *testing.T) {
	want := errors.New("service down")
	h := New(&fakeOpenCode{}, fakeSupervisor{ensureErr: want, restartErr: want, stopErr: want})

	for _, method := range []string{"opencode.start", "opencode.restart", "opencode.stop"} {
		if _, err := h.Handle(context.Background(), method, nil); !errors.Is(err, want) {
			t.Fatalf("%s err = %v, want %v", method, err, want)
		}
	}
}

func TestSessionDiffConfinesAndPassesParams(t *testing.T) {
	root := t.TempDir()
	oc := &fakeOpenCode{diffs: []opencode.FileDiff{{File: "a.txt", Status: "modified"}}}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(SessionDiffParams{
		SessionID: "ses_1",
		Directory: root,
		From:      "msg_a",
		To:        "msg_b",
		Context:   3,
	})
	result, err := h.Handle(context.Background(), "session.diff", params)
	if err != nil {
		t.Fatal(err)
	}
	if oc.diffCalls != 1 || oc.diffSessionID != "ses_1" || oc.diffFrom != "msg_a" || oc.diffTo != "msg_b" || oc.diffContext != 3 {
		t.Fatalf("diff call = %d/%q/%q/%q/%d", oc.diffCalls, oc.diffSessionID, oc.diffFrom, oc.diffTo, oc.diffContext)
	}
	diffs, ok := result.([]opencode.FileDiff)
	if !ok || len(diffs) != 1 || diffs[0].File != "a.txt" {
		t.Fatalf("result = %#v", result)
	}
}

func TestSessionDiffRejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(SessionDiffParams{SessionID: "ses_1", Directory: outside})
	if _, err := h.Handle(context.Background(), "session.diff", params); err == nil || !strings.Contains(err.Error(), "outside workspace_roots") {
		t.Fatalf("error = %v", err)
	}
	if oc.diffCalls != 0 {
		t.Fatalf("diff calls = %d", oc.diffCalls)
	}
}

func TestSessionInterruptPassesSessionID(t *testing.T) {
	oc := &fakeOpenCode{interruptResult: opencode.InterruptResult{Interrupted: true}}
	h := New(oc, fakeSupervisor{})

	params, _ := json.Marshal(SessionInterruptParams{SessionID: "ses_1"})
	result, err := h.Handle(context.Background(), "session.interrupt", params)
	if err != nil {
		t.Fatal(err)
	}
	if oc.interruptCalls != 1 || oc.interruptSessionID != "ses_1" || oc.interruptResume {
		t.Fatalf("interrupt = %d/%q/%v", oc.interruptCalls, oc.interruptSessionID, oc.interruptResume)
	}
	got, ok := result.(opencode.InterruptResult)
	if !ok || !got.Interrupted {
		t.Fatalf("result = %#v", result)
	}
}

func TestFileListConfinesRelativePath(t *testing.T) {
	root := t.TempDir()
	oc := &fakeOpenCode{files: []opencode.FileInfo{{Path: "a.txt", Type: "file"}}}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(FileListParams{Directory: root, Path: "sub"})
	result, err := h.Handle(context.Background(), "file.list", params)
	if err != nil {
		t.Fatal(err)
	}
	if oc.listFilesCalls != 1 || oc.listFilesPath != "sub" || oc.listFilesDirectory != root {
		t.Fatalf("list = %d/%q/%q", oc.listFilesCalls, oc.listFilesDirectory, oc.listFilesPath)
	}
	files, ok := result.([]opencode.FileInfo)
	if !ok || len(files) != 1 || files[0].Path != "a.txt" {
		t.Fatalf("result = %#v", result)
	}
}

func TestFileListRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(FileListParams{Directory: root, Path: "../secret"})
	if _, err := h.Handle(context.Background(), "file.list", params); err == nil {
		t.Fatal("expected traversal error")
	}
	if oc.listFilesCalls != 0 {
		t.Fatalf("list calls = %d", oc.listFilesCalls)
	}
}

func TestFileListRejectsSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(FileListParams{Directory: root, Path: "link"})
	if _, err := h.Handle(context.Background(), "file.list", params); err == nil {
		t.Fatal("expected symlink containment error")
	}
	if oc.listFilesCalls != 0 {
		t.Fatalf("list calls = %d", oc.listFilesCalls)
	}
}

func TestFileReadReturnsUTF8AndCapsSize(t *testing.T) {
	root := t.TempDir()
	oc := &fakeOpenCode{readFileContent: opencode.FileContent{Data: []byte("hello"), Size: 5}}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(FileReadParams{Directory: root, Path: "a.txt", MaxBytes: 10 << 20})
	result, err := h.Handle(context.Background(), "file.read", params)
	if err != nil {
		t.Fatal(err)
	}
	if oc.readFileMaxBytes != 512<<10 {
		t.Fatalf("max bytes = %d, want 512 KiB cap", oc.readFileMaxBytes)
	}
	got, ok := result.(FileReadResult)
	if !ok || got.Encoding != "utf-8" || got.Content != "hello" || got.Path != "a.txt" || got.Size != 5 {
		t.Fatalf("result = %#v", result)
	}
}

func TestFileReadReturnsBase64ForBinary(t *testing.T) {
	root := t.TempDir()
	oc := &fakeOpenCode{readFileContent: opencode.FileContent{Data: []byte{0, 1, 2}, Size: 3}}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(FileReadParams{Directory: root, Path: "bin.dat"})
	result, err := h.Handle(context.Background(), "file.read", params)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := result.(FileReadResult)
	if !ok || got.Encoding != "base64" {
		t.Fatalf("result = %#v", result)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Content)
	if err != nil || string(decoded) != string([]byte{0, 1, 2}) {
		t.Fatalf("content = %q err=%v", got.Content, err)
	}
}

func TestPTYCreateConfinesAndDefaults(t *testing.T) {
	root := t.TempDir()
	oc := &fakeOpenCode{createTerminal: opencode.PersistentTerminal{ID: "pty_1", Cwd: root, Status: "running"}}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(PTYCreateParams{SessionID: "ses_1", Directory: root})
	result, err := h.Handle(context.Background(), "pty.create", params)
	if err != nil {
		t.Fatal(err)
	}
	if oc.createTerminalCalls != 1 || oc.createTerminalSession != "ses_1" {
		t.Fatalf("create = %d/%q", oc.createTerminalCalls, oc.createTerminalSession)
	}
	if oc.createTerminalReq.Size.Cols != 80 || oc.createTerminalReq.Size.Rows != 24 {
		t.Fatalf("size = %#v", oc.createTerminalReq.Size)
	}
	if oc.createTerminalReq.Args == nil || oc.createTerminalReq.Env == nil {
		t.Fatalf("args/env must be non-nil: %#v", oc.createTerminalReq)
	}
	got, ok := result.(opencode.PersistentTerminal)
	if !ok || got.ID != "pty_1" {
		t.Fatalf("result = %#v", result)
	}
	h.ClosePTYs()
}

func TestPTYCreateRejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(PTYCreateParams{SessionID: "ses_1", Directory: outside})
	if _, err := h.Handle(context.Background(), "pty.create", params); err == nil || !strings.Contains(err.Error(), "outside workspace_roots") {
		t.Fatalf("error = %v", err)
	}
	if oc.createTerminalCalls != 0 {
		t.Fatalf("create calls = %d", oc.createTerminalCalls)
	}
}

func TestPTYWriteResizeAndClose(t *testing.T) {
	root := t.TempDir()
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))
	conn := &fakeTerminalConn{}
	h.pty.add(&terminalSession{id: "pty_1", sessionID: "ses_1", cols: 80, rows: 24, conn: conn})

	writeParams, _ := json.Marshal(PTYWriteParams{PTYID: "pty_1", Data: base64.StdEncoding.EncodeToString([]byte("hi"))})
	if _, err := h.Handle(context.Background(), "pty.write", writeParams); err != nil {
		t.Fatal(err)
	}
	if string(conn.written) != "hi" {
		t.Fatalf("written = %q", conn.written)
	}

	resizeParams, _ := json.Marshal(PTYResizeParams{PTYID: "pty_1", Cols: 100, Rows: 40})
	if _, err := h.Handle(context.Background(), "pty.resize", resizeParams); err != nil {
		t.Fatal(err)
	}
	if conn.size.Cols != 100 || conn.size.Rows != 40 {
		t.Fatalf("size = %#v", conn.size)
	}

	closeParams, _ := json.Marshal(PTYIDParams{PTYID: "pty_1"})
	if _, err := h.Handle(context.Background(), "pty.close", closeParams); err != nil {
		t.Fatal(err)
	}
	if !conn.closed || oc.closeTerminalCalls != 1 {
		t.Fatalf("closed=%v closeCalls=%d", conn.closed, oc.closeTerminalCalls)
	}
	if _, err := h.Handle(context.Background(), "pty.close", closeParams); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestConfinePath(t *testing.T) {
	root := t.TempDir()
	h := New(&fakeOpenCode{}, fakeSupervisor{}, WithWorkspaceRoots(root))

	if _, err := h.confinePath(root, "../x"); err == nil {
		t.Fatal("expected traversal rejection")
	}
	if _, err := h.confinePath(root, "/etc/passwd"); err == nil {
		t.Fatal("expected absolute outside rejection")
	}
	got, err := h.confinePath(root, "sub/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "sub", "file.txt") {
		t.Fatalf("target = %q", got)
	}
}
