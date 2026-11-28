package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/agentd/opencode"
	"github.com/insmtx/lework/backend/internal/agentd/supervisor"
)

// fakeOpenCode records every call so the handlers' argument plumbing can be
// asserted without a real HTTP server.
type fakeOpenCode struct {
	prompts int

	promptSessionID string
	promptText      string
	promptReceipt   string
	promptErr       error

	listCalls     int
	listDirectory string
	sessions      []opencode.Session

	childrenCalls     int
	childrenParentID  string
	childrenDirectory string
	children          []opencode.Session
	childrenErr       error

	projectsCalls int
	projects      []opencode.Project
	projectsErr   error

	createdDirectory string
	createCalls      int
	createSession    opencode.Session

	renameCalls     int
	renameSessionID string
	renameTitle     string

	deleteCalls     int
	deleteSessionID string

	messagesCalls     int
	messagesSessionID string
	messagesLimit     int
	messagesCursor    string
	messages          []opencode.Message
	messagesPage      opencode.MessagePage

	replyCalls        int
	replySessionID    string
	replyPermissionID string
	replyDecision     string
	replyMessage      string

	permissionsCalls int
	permissions      []opencode.PermissionRequest
	permissionsErr   error

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
	readFileOffset    int64
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

	listFormsCalls     int
	listFormsSessionID string
	forms              []opencode.FormInfo
	formsErr           error

	locationFormsCalls     int
	locationFormsDirectory string
	locationForms          []opencode.FormInfo
	locationFormsErr       error

	replyFormCalls     int
	replyFormSessionID string
	replyFormID        string
	replyFormDirectory string
	replyFormAnswer    map[string]any
	replyFormErr       error

	cancelFormCalls     int
	cancelFormSessionID string
	cancelFormID        string
	cancelFormDirectory string
	cancelFormMessage   string
	cancelFormErr       error
}

func (f *fakeOpenCode) ListSessions(_ context.Context, directory string) ([]opencode.Session, error) {
	f.listCalls++
	f.listDirectory = directory
	return f.sessions, nil
}

func (f *fakeOpenCode) ListChildSessions(
	_ context.Context,
	parentID, directory string,
) ([]opencode.Session, error) {
	f.childrenCalls++
	f.childrenParentID = parentID
	f.childrenDirectory = directory
	return f.children, f.childrenErr
}

func (f *fakeOpenCode) ListProjects(context.Context) ([]opencode.Project, error) {
	f.projectsCalls++
	return f.projects, f.projectsErr
}

func (f *fakeOpenCode) CreateSession(_ context.Context, directory string) (opencode.Session, error) {
	f.createCalls++
	f.createdDirectory = directory
	return f.createSession, nil
}

func (f *fakeOpenCode) RenameSession(_ context.Context, sessionID, title string) error {
	f.renameCalls++
	f.renameSessionID = sessionID
	f.renameTitle = title
	return nil
}

func (f *fakeOpenCode) DeleteSession(_ context.Context, sessionID string) error {
	f.deleteCalls++
	f.deleteSessionID = sessionID
	return nil
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

func (f *fakeOpenCode) Messages(_ context.Context, sessionID string, limit int, cursor string) (opencode.MessagePage, error) {
	f.messagesCalls++
	f.messagesSessionID = sessionID
	f.messagesLimit = limit
	f.messagesCursor = cursor
	if len(f.messagesPage.Messages) > 0 || f.messagesPage.Next != "" {
		return f.messagesPage, nil
	}
	return opencode.MessagePage{Messages: f.messages}, nil
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

func (f *fakeOpenCode) ListForms(_ context.Context, sessionID string) ([]opencode.FormInfo, error) {
	f.listFormsCalls++
	f.listFormsSessionID = sessionID
	return f.forms, f.formsErr
}

func (f *fakeOpenCode) ListLocationForms(_ context.Context, directory string) ([]opencode.FormInfo, error) {
	f.locationFormsCalls++
	f.locationFormsDirectory = directory
	return f.locationForms, f.locationFormsErr
}

func (f *fakeOpenCode) ReplyForm(
	_ context.Context,
	sessionID, formID, directory string,
	answer map[string]any,
) error {
	f.replyFormCalls++
	f.replyFormSessionID, f.replyFormID = sessionID, formID
	f.replyFormDirectory, f.replyFormAnswer = directory, answer
	return f.replyFormErr
}

func (f *fakeOpenCode) CancelForm(_ context.Context, sessionID, formID, directory, message string) error {
	f.cancelFormCalls++
	f.cancelFormSessionID, f.cancelFormID = sessionID, formID
	f.cancelFormDirectory, f.cancelFormMessage = directory, message
	return f.cancelFormErr
}

func (f *fakeOpenCode) ListPermissions(context.Context) ([]opencode.PermissionRequest, error) {
	f.permissionsCalls++
	return f.permissions, f.permissionsErr
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

func (f *fakeOpenCode) ReadFile(_ context.Context, directory, path string, offset, maxBytes int64) (opencode.FileContent, error) {
	f.readFileCalls++
	f.readFileDirectory = directory
	f.readFilePath = path
	f.readFileOffset = offset
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
	if oc.listDirectory != "/work/project" {
		t.Fatalf("directory = %q, want /work/project", oc.listDirectory)
	}
	if len(sessions) != 1 || sessions[0].ID != "ses_1" {
		t.Fatalf("sessions = %#v, want one ses_1", sessions)
	}
}

func TestSessionChildrenPassesThrough(t *testing.T) {
	oc := &fakeOpenCode{
		children: []opencode.Session{{ID: "ses_child", ParentID: "ses_parent"}},
	}
	h := New(oc, fakeSupervisor{})

	result, err := h.Handle(
		context.Background(),
		"session.children",
		json.RawMessage(`{"session_id":"ses_parent","directory":"/work/project"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	sessions, ok := result.([]opencode.Session)
	if !ok {
		t.Fatalf("result type = %T, want []opencode.Session", result)
	}
	if oc.childrenCalls != 1 {
		t.Fatalf("children calls = %d, want 1", oc.childrenCalls)
	}
	if oc.childrenParentID != "ses_parent" {
		t.Fatalf("parent id = %q, want ses_parent", oc.childrenParentID)
	}
	if oc.childrenDirectory != "/work/project" {
		t.Fatalf("directory = %q, want /work/project", oc.childrenDirectory)
	}
	if len(sessions) != 1 || sessions[0].ID != "ses_child" {
		t.Fatalf("sessions = %#v, want one ses_child", sessions)
	}
}

func TestSessionChildrenRequiresParent(t *testing.T) {
	// 空 parent 会被服务端当成「不过滤」，必须本地拒绝。
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{})

	if _, err := h.Handle(
		context.Background(),
		"session.children",
		json.RawMessage(`{"session_id":"  "}`),
	); err == nil {
		t.Fatal("err = nil, want error for missing session_id")
	}
	if oc.childrenCalls != 0 {
		t.Fatalf("children calls = %d, want 0", oc.childrenCalls)
	}
}

func TestSessionRenamePassesTitle(t *testing.T) {
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{})

	if _, err := h.Handle(
		context.Background(),
		"session.rename",
		json.RawMessage(`{"session_id":"ses_1","title":"  new title  "}`),
	); err != nil {
		t.Fatal(err)
	}
	if oc.renameCalls != 1 || oc.renameSessionID != "ses_1" || oc.renameTitle != "new title" {
		t.Fatalf("rename args = %#v", oc)
	}

	if _, err := h.Handle(
		context.Background(),
		"session.rename",
		json.RawMessage(`{"session_id":"ses_1","title":"   "}`),
	); err == nil {
		t.Fatal("expected empty title to be rejected")
	}
}

func TestSessionDeletePassesSessionID(t *testing.T) {
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{})

	if _, err := h.Handle(
		context.Background(),
		"session.delete",
		json.RawMessage(`{"session_id":"ses_1"}`),
	); err != nil {
		t.Fatal(err)
	}
	if oc.deleteCalls != 1 || oc.deleteSessionID != "ses_1" {
		t.Fatalf("delete args = %#v", oc)
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

func TestSessionCreateAllowsExistingProjectOutsideWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	project := t.TempDir()
	oc := &fakeOpenCode{
		projects:      []opencode.Project{{ID: "abc", Canonical: project}},
		createSession: opencode.Session{ID: "ses_new"},
	}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	// The project cache starts empty, so this also covers the lazy refresh.
	params, err := json.Marshal(SessionCreateParams{Directory: project})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(context.Background(), "session.create", params); err != nil {
		t.Fatalf("Handle() error = %v, want nil for an existing project", err)
	}
	if oc.createdDirectory != project {
		t.Fatalf("directory = %q, want %q", oc.createdDirectory, project)
	}
	if oc.projectsCalls != 1 {
		t.Fatalf("project.list calls = %d, want 1", oc.projectsCalls)
	}

	// The refreshed cache must be reused instead of listing projects again.
	if _, err := h.Handle(context.Background(), "session.create", params); err != nil {
		t.Fatalf("second Handle() error = %v", err)
	}
	if oc.projectsCalls != 1 {
		t.Fatalf("project.list calls = %d, want the cached set reused", oc.projectsCalls)
	}

	// Only the exact project root is allowed; a child directory stays outside.
	nested, err := json.Marshal(SessionCreateParams{Directory: filepath.Join(project, "nested")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(context.Background(), "session.create", nested); err == nil {
		t.Fatal("expected a project child directory to stay confined")
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
		json.RawMessage(`{"session_id":"ses_1","limit":500,"cursor":"cursor-1"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if oc.messagesSessionID != "ses_1" {
		t.Fatalf("session_id = %q, want ses_1", oc.messagesSessionID)
	}
	if oc.messagesLimit != 200 {
		t.Fatalf("limit = %d, want clamp to 200", oc.messagesLimit)
	}
	if oc.messagesCursor != "cursor-1" {
		t.Fatalf("cursor = %q, want cursor-1", oc.messagesCursor)
	}
	page, ok := result.(opencode.MessagePage)
	if !ok || len(page.Messages) != 1 || page.Messages[0].ID != "msg_1" {
		t.Fatalf("result = %#v, want one msg_1", result)
	}

	result, err = h.Handle(
		context.Background(),
		"session.messages",
		json.RawMessage(`{"session_id":"ses_1"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if oc.messagesLimit != 200 {
		t.Fatalf("default limit = %d, want 200", oc.messagesLimit)
	}
	if page, ok := result.(opencode.MessagePage); !ok || len(page.Messages) != 1 {
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

func TestPermissionListFiltersBySession(t *testing.T) {
	oc := &fakeOpenCode{permissions: []opencode.PermissionRequest{
		{ID: "per_1", SessionID: "ses_1", Action: "shell"},
		{ID: "per_2", SessionID: "ses_2", Action: "edit"},
	}}
	h := New(oc, fakeSupervisor{})

	result, err := h.Handle(
		context.Background(),
		"permission.list",
		json.RawMessage(`{"session_id":"ses_1"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if oc.permissionsCalls != 1 {
		t.Fatalf("list calls = %d, want 1", oc.permissionsCalls)
	}
	got, ok := result.([]opencode.PermissionRequest)
	if !ok || len(got) != 1 || got[0].ID != "per_1" {
		t.Fatalf("result = %#v, want only per_1", result)
	}

	result, err = h.Handle(context.Background(), "permission.list", nil)
	if err != nil {
		t.Fatal(err)
	}
	all, ok := result.([]opencode.PermissionRequest)
	if !ok || len(all) != 2 {
		t.Fatalf("result = %#v, want both permissions", result)
	}
}

func TestPermissionReplyRejectsUnknownDecision(t *testing.T) {
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{})

	_, err := h.Handle(
		context.Background(),
		"permission.reply",
		json.RawMessage(`{"session_id":"ses_1","permission_id":"per_1","decision":"maybe"}`),
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if oc.replyCalls != 0 {
		t.Fatalf("reply calls = %d, want 0", oc.replyCalls)
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
	if oc.readFileMaxBytes != 1<<20 {
		t.Fatalf("max bytes = %d, want 1 MiB cap", oc.readFileMaxBytes)
	}
	if oc.readFileOffset != 0 {
		t.Fatalf("offset = %d, want 0", oc.readFileOffset)
	}
	got, ok := result.(FileReadResult)
	if !ok || got.Encoding != "utf-8" || got.Content != "hello" || got.Path != "a.txt" || got.Size != 5 {
		t.Fatalf("result = %#v", result)
	}
}

func TestFileReadPassesOffset(t *testing.T) {
	root := t.TempDir()
	oc := &fakeOpenCode{readFileContent: opencode.FileContent{Data: []byte("tail"), Size: 8}}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(FileReadParams{Directory: root, Path: "a.txt", Offset: 4})
	if _, err := h.Handle(context.Background(), "file.read", params); err != nil {
		t.Fatal(err)
	}
	if oc.readFileOffset != 4 {
		t.Fatalf("offset = %d, want 4", oc.readFileOffset)
	}
}

func TestFileReadTrimsPartialRuneAtTruncation(t *testing.T) {
	root := t.TempDir()
	// 512 KiB 的截断点可能落在多字节字符中间：末尾两个字节是「中」的前半段。
	oc := &fakeOpenCode{readFileContent: opencode.FileContent{
		Data:      []byte("你好\xe4\xb8"),
		Size:      600 << 10,
		Truncated: true,
	}}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(FileReadParams{Directory: root, Path: "a.md"})
	result, err := h.Handle(context.Background(), "file.read", params)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := result.(FileReadResult)
	if !ok || got.Encoding != "utf-8" || got.Content != "你好" {
		t.Fatalf("result = %#v", result)
	}
}

func TestFileReadKeepsBase64WithoutTruncation(t *testing.T) {
	root := t.TempDir()
	oc := &fakeOpenCode{readFileContent: opencode.FileContent{
		Data: []byte("你好\xe4\xb8"),
		Size: 8,
	}}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(FileReadParams{Directory: root, Path: "a.bin"})
	result, err := h.Handle(context.Background(), "file.read", params)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := result.(FileReadResult)
	if !ok || got.Encoding != "base64" {
		t.Fatalf("result = %#v", result)
	}
}

func TestFileReadRejectsNegativeOffset(t *testing.T) {
	root := t.TempDir()
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(FileReadParams{Directory: root, Path: "a.txt", Offset: -1})
	if _, err := h.Handle(context.Background(), "file.read", params); err == nil {
		t.Fatal("expected negative offset error")
	}
	if oc.readFileCalls != 0 {
		t.Fatalf("read calls = %d, want 0", oc.readFileCalls)
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

	if _, err := h.confinePath(context.Background(), root, "../x"); err == nil {
		t.Fatal("expected traversal rejection")
	}
	if _, err := h.confinePath(context.Background(), root, "/etc/passwd"); err == nil {
		t.Fatal("expected absolute outside rejection")
	}
	got, err := h.confinePath(context.Background(), root, "sub/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "sub", "file.txt") {
		t.Fatalf("target = %q", got)
	}
}

func TestHandleProjectList(t *testing.T) {
	oc := &fakeOpenCode{projects: []opencode.Project{
		{ID: "abc", Canonical: "/work/repo"},
		{ID: "global", Canonical: "/work/plain"},
	}}
	h := New(oc, fakeSupervisor{})

	result, err := h.Handle(context.Background(), "project.list", nil)
	if err != nil {
		t.Fatal(err)
	}
	projects, ok := result.([]opencode.Project)
	if !ok {
		t.Fatalf("result type = %T, want []opencode.Project", result)
	}
	if len(projects) != 2 || projects[0].ID != "abc" || projects[1].Canonical != "/work/plain" {
		t.Fatalf("projects = %#v", projects)
	}
	if oc.projectsCalls != 1 {
		t.Fatalf("projects calls = %d, want 1", oc.projectsCalls)
	}
}

func TestHandleProjectListPropagatesError(t *testing.T) {
	oc := &fakeOpenCode{projectsErr: errors.New("boom")}
	if _, err := New(oc, fakeSupervisor{}).Handle(context.Background(), "project.list", nil); err == nil {
		t.Fatal("expected the client error to surface")
	}
}

func TestHandleProjectCreate(t *testing.T) {
	root := t.TempDir()
	h := New(&fakeOpenCode{}, fakeSupervisor{}, WithWorkspaceRoots(root))

	result, err := h.Handle(context.Background(), "project.create",
		json.RawMessage(`{"name":"  新项目  "}`))
	if err != nil {
		t.Fatal(err)
	}
	created, ok := result.(ProjectCreateResult)
	if !ok {
		t.Fatalf("result type = %T, want ProjectCreateResult", result)
	}
	want := filepath.Join(root, "新项目")
	if created.Canonical != want {
		t.Fatalf("canonical = %q, want %q", created.Canonical, want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", want)
	}
}

func TestHandleProjectCreateUsesFirstWorkspaceRoot(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	h := New(&fakeOpenCode{}, fakeSupervisor{}, WithWorkspaceRoots(first, second))

	if _, err := h.Handle(context.Background(), "project.create", json.RawMessage(`{"name":"demo"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(first, "demo")); err != nil {
		t.Fatalf("project was not created under the first root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(second, "demo")); !os.IsNotExist(err) {
		t.Fatalf("project leaked into the second root: %v", err)
	}
}

func TestHandleProjectCreateRejectsInvalidNames(t *testing.T) {
	root := t.TempDir()
	h := New(&fakeOpenCode{}, fakeSupervisor{}, WithWorkspaceRoots(root))

	for _, name := range []string{"", "   ", ".", "..", ".git", "a/b", `a\b`, "a\x00b", strings.Repeat("a", 65)} {
		_, err := h.Handle(context.Background(), "project.create",
			json.RawMessage(`{"name":`+strconv.Quote(name)+`}`))
		if err == nil {
			t.Fatalf("name %q was accepted", name)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid names created entries: %#v", entries)
	}
}

func TestHandleProjectCreateRequiresWorkspaceRoot(t *testing.T) {
	h := New(&fakeOpenCode{}, fakeSupervisor{})
	if _, err := h.Handle(context.Background(), "project.create",
		json.RawMessage(`{"name":"demo"}`)); err == nil {
		t.Fatal("expected a missing workspace root error")
	}
}

func TestHandleProjectCreateRejectsExistingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := New(&fakeOpenCode{}, fakeSupervisor{}, WithWorkspaceRoots(root))
	if _, err := h.Handle(context.Background(), "project.create",
		json.RawMessage(`{"name":"demo"}`)); err == nil {
		t.Fatal("expected an existing directory error")
	}
}

func TestNodeInfoReportsWorkspaceRoots(t *testing.T) {
	h := New(&fakeOpenCode{}, fakeSupervisor{}, WithWorkspaceRoots("/w/a", "/w/b"))

	result, err := h.Handle(context.Background(), "node.info", nil)
	if err != nil {
		t.Fatal(err)
	}
	info, ok := result.(NodeInfo)
	if !ok {
		t.Fatalf("result type = %T, want NodeInfo", result)
	}
	if len(info.WorkspaceRoots) != 2 || info.WorkspaceRoots[0] != "/w/a" || info.WorkspaceRoots[1] != "/w/b" {
		t.Fatalf("workspace roots = %#v", info.WorkspaceRoots)
	}
}

func TestNodeInfoWorkspaceRootsNeverNil(t *testing.T) {
	result, err := New(&fakeOpenCode{}, fakeSupervisor{}).Handle(context.Background(), "node.info", nil)
	if err != nil {
		t.Fatal(err)
	}
	info := result.(NodeInfo)
	if info.WorkspaceRoots == nil {
		t.Fatal("workspace roots must serialize as [] rather than null")
	}
}

func TestFormListPassesSession(t *testing.T) {
	oc := &fakeOpenCode{forms: []opencode.FormInfo{{ID: "frm_1", SessionID: "ses_1"}}}
	h := New(oc, fakeSupervisor{})

	result, err := h.Handle(context.Background(), "form.list",
		json.RawMessage(`{"session_id":"ses_1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if oc.listFormsCalls != 1 || oc.listFormsSessionID != "ses_1" {
		t.Fatalf("calls = %#v", oc)
	}
	forms, ok := result.([]opencode.FormInfo)
	if !ok || len(forms) != 1 || forms[0].ID != "frm_1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestFormListRequiresSession(t *testing.T) {
	oc := &fakeOpenCode{}
	if _, err := New(oc, fakeSupervisor{}).Handle(context.Background(), "form.list", nil); err == nil {
		t.Fatal("expected error")
	}
	if oc.listFormsCalls != 0 {
		t.Fatalf("calls = %d", oc.listFormsCalls)
	}
}

func TestFormGlobalListFilteringAssignsDirectoryAndDedupes(t *testing.T) {
	oc := &fakeOpenCode{locationForms: []opencode.FormInfo{
		{ID: "frm_global", SessionID: "global"},
		{ID: "frm_session", SessionID: "ses_1"},
	}}
	h := New(oc, fakeSupervisor{})

	result, err := h.Handle(context.Background(), "form.global_list",
		json.RawMessage(`{"directory":"/work/foo"}`))
	if err != nil {
		t.Fatal(err)
	}
	if oc.locationFormsDirectory != "/work/foo" {
		t.Fatalf("directory = %q", oc.locationFormsDirectory)
	}
	forms, ok := result.([]opencode.FormInfo)
	if !ok || len(forms) != 1 || forms[0].ID != "frm_global" {
		t.Fatalf("result = %#v", result)
	}
	if forms[0].Directory != "/work/foo" {
		t.Fatalf("directory = %q", forms[0].Directory)
	}
}

func TestFormGlobalListEnumeratesRootsAndProjects(t *testing.T) {
	root := t.TempDir()
	project := t.TempDir()
	oc := &fakeOpenCode{locationForms: []opencode.FormInfo{{ID: "frm_1", SessionID: "global"}}}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))
	h.rememberProjects([]opencode.Project{{ID: "p1", Canonical: project}})

	if _, err := h.Handle(context.Background(), "form.global_list", nil); err != nil {
		t.Fatal(err)
	}
	if oc.locationFormsCalls != 2 {
		t.Fatalf("calls = %d, want 2 (root + project)", oc.locationFormsCalls)
	}
}

func TestFormGlobalListSkipsFailingDirectory(t *testing.T) {
	root := t.TempDir()
	oc := &fakeOpenCode{locationFormsErr: errors.New("gone")}
	h := New(oc, fakeSupervisor{}, WithWorkspaceRoots(root))

	result, err := h.Handle(context.Background(), "form.global_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	forms, ok := result.([]opencode.FormInfo)
	if !ok || len(forms) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestFormReplyPassesAnswerAndDirectory(t *testing.T) {
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{})

	_, err := h.Handle(context.Background(), "form.reply",
		json.RawMessage(`{"session_id":"global","form_id":"frm_1","directory":"/work/foo",`+
			`"answer":{"q0":["a","b"],"q1":true,"q2":2.5}}`))
	if err != nil {
		t.Fatal(err)
	}
	if oc.replyFormSessionID != "global" || oc.replyFormID != "frm_1" ||
		oc.replyFormDirectory != "/work/foo" {
		t.Fatalf("args = %#v", oc)
	}
	if len(oc.replyFormAnswer) != 3 {
		t.Fatalf("answer = %#v", oc.replyFormAnswer)
	}
}

func TestFormReplyRejectsMalformedAnswer(t *testing.T) {
	cases := []string{
		`{}`,
		`{"q0":{"nested":true}}`,
		`{"q0":["a",1]}`,
		`{"q0":null}`,
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			oc := &fakeOpenCode{}
			raw := `{"session_id":"ses_1","form_id":"frm_1","answer":` + body + `}`
			if _, err := New(oc, fakeSupervisor{}).Handle(context.Background(), "form.reply",
				json.RawMessage(raw)); err == nil {
				t.Fatal("expected error")
			}
			if oc.replyFormCalls != 0 {
				t.Fatalf("calls = %d", oc.replyFormCalls)
			}
		})
	}
}

func TestFormCancelPassesOptionalMessage(t *testing.T) {
	oc := &fakeOpenCode{}
	h := New(oc, fakeSupervisor{})

	if _, err := h.Handle(context.Background(), "form.cancel",
		json.RawMessage(`{"session_id":"ses_1","form_id":"frm_1"}`)); err != nil {
		t.Fatal(err)
	}
	if oc.cancelFormMessage != "" {
		t.Fatalf("message = %q", oc.cancelFormMessage)
	}
	if _, err := h.Handle(context.Background(), "form.cancel",
		json.RawMessage(`{"session_id":"ses_1","form_id":"frm_1","message":"skip"}`)); err != nil {
		t.Fatal(err)
	}
	if oc.cancelFormMessage != "skip" {
		t.Fatalf("message = %q", oc.cancelFormMessage)
	}
}

func TestFileWriteCreatesAndReturnsPath(t *testing.T) {
	root := t.TempDir()
	h := New(&fakeOpenCode{}, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(FileWriteParams{
		Directory: root, Path: "a.txt", Data: base64.StdEncoding.EncodeToString([]byte("hello")),
	})
	result, err := h.Handle(context.Background(), "file.write", params)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := result.(FileWriteResult)
	if !ok || got.Path != "a.txt" || got.Size != 5 {
		t.Fatalf("result = %#v", result)
	}
	data, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("file = %q err=%v", data, err)
	}
}

func TestFileWriteRenamesConflict(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(&fakeOpenCode{}, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(FileWriteParams{
		Directory: root, Path: "a.txt", Data: base64.StdEncoding.EncodeToString([]byte("new")),
	})
	result, err := h.Handle(context.Background(), "file.write", params)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(FileWriteResult)
	if got.Path != "a (1).txt" {
		t.Fatalf("path = %q, want a (1).txt", got.Path)
	}
	old, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	renamed, _ := os.ReadFile(filepath.Join(root, "a (1).txt"))
	if string(old) != "old" || string(renamed) != "new" {
		t.Fatalf("old=%q renamed=%q", old, renamed)
	}
}

func TestFileWriteAppendsChunkAndTruncates(t *testing.T) {
	root := t.TempDir()
	h := New(&fakeOpenCode{}, fakeSupervisor{}, WithWorkspaceRoots(root))

	write := func(offset int64, data string) FileWriteResult {
		params, _ := json.Marshal(FileWriteParams{
			Directory: root, Path: "a.txt", Offset: offset,
			Data: base64.StdEncoding.EncodeToString([]byte(data)),
		})
		result, err := h.Handle(context.Background(), "file.write", params)
		if err != nil {
			t.Fatal(err)
		}
		return result.(FileWriteResult)
	}
	write(0, "abcdef")
	write(3, "XY")

	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(data) != "abcXY" {
		t.Fatalf("file = %q, want abcXY", data)
	}
}

func TestFileWriteRejectsBadPaths(t *testing.T) {
	root := t.TempDir()
	h := New(&fakeOpenCode{}, fakeSupervisor{}, WithWorkspaceRoots(root))

	for _, path := range []string{"../a.txt", "sub/a.txt", `sub\a.txt`, ".", "..", ""} {
		params, _ := json.Marshal(FileWriteParams{
			Directory: root, Path: path, Data: base64.StdEncoding.EncodeToString([]byte("x")),
		})
		if _, err := h.Handle(context.Background(), "file.write", params); err == nil {
			t.Fatalf("path %q: expected error", path)
		}
	}
}

func TestFileWriteRejectsSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "a.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	h := New(&fakeOpenCode{}, fakeSupervisor{}, WithWorkspaceRoots(root))

	params, _ := json.Marshal(FileWriteParams{
		Directory: root, Path: "a.txt", Offset: 1,
		Data: base64.StdEncoding.EncodeToString([]byte("x")),
	})
	if _, err := h.Handle(context.Background(), "file.write", params); err == nil {
		t.Fatal("expected symlink rejection")
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "secret" {
		t.Fatalf("outside file changed: %q", data)
	}
}

func TestFileWriteRejectsOversizeChunk(t *testing.T) {
	root := t.TempDir()
	h := New(&fakeOpenCode{}, fakeSupervisor{}, WithWorkspaceRoots(root))

	data := base64.StdEncoding.EncodeToString(make([]byte, (1<<20)+1))
	params, _ := json.Marshal(FileWriteParams{Directory: root, Path: "a.bin", Data: data})
	if _, err := h.Handle(context.Background(), "file.write", params); err == nil {
		t.Fatal("expected oversize chunk error")
	}
}
