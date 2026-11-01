package handler

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
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
	createSession    opencode.Session

	messagesCalls     int
	messagesSessionID string
	messages          []opencode.Message

	replyCalls        int
	replySessionID    string
	replyPermissionID string
	replyDecision     string
	replyMessage      string
}

func (f *fakeOpenCode) ListSessions(context.Context) ([]opencode.Session, error) {
	f.listCalls++
	return f.sessions, nil
}

func (f *fakeOpenCode) CreateSession(_ context.Context, directory string) (opencode.Session, error) {
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
	if err == nil {
		t.Fatal("expected error")
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
