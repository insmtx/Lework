package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/insmtx/Lework/backend/internal/agentd/opencode"
	"github.com/insmtx/Lework/backend/internal/agentd/protocol"
)

// fakeTerminalConn is a deterministic stand-in for the OpenCode terminal
// websocket. The handler only needs byte output, input, resize, and close.
type fakeTerminalConn struct {
	mu      sync.Mutex
	chunks  chan []byte
	readErr chan error
	written []byte
	size    opencode.TerminalSize
	closed  bool
}

func newFakeTerminalConn() *fakeTerminalConn {
	return &fakeTerminalConn{
		chunks:  make(chan []byte, 8),
		readErr: make(chan error, 1),
	}
}

func (f *fakeTerminalConn) Read(ctx context.Context) (int, []byte, error) {
	select {
	case data := <-f.chunks:
		return websocket.BinaryMessage, data, nil
	case err := <-f.readErr:
		return 0, nil, err
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

func (f *fakeTerminalConn) WriteInput(_ context.Context, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written = append(f.written, data...)
	return nil
}

func (f *fakeTerminalConn) Resize(_ context.Context, size opencode.TerminalSize) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.size = size
	return nil
}

func (f *fakeTerminalConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func TestPTYPumpEmitsOutputAndExit(t *testing.T) {
	manager := newPTYManager()
	conn := newFakeTerminalConn()
	oc := &fakeOpenCode{}
	session := &terminalSession{id: "pty_1", sessionID: "ses_1", conn: conn}
	manager.add(session)

	ctx, cancel := context.WithCancel(context.Background())
	go manager.pump(ctx, session, oc)

	conn.chunks <- []byte("hello")
	event := receivePTYEvent(t, manager.events)
	if event.Type != "pty.output" || event.SessionID != "ses_1" {
		t.Fatalf("event = %#v", event)
	}
	var output struct {
		PTYID string `json:"pty_id"`
		Data  string `json:"data"`
	}
	if err := json.Unmarshal(event.Data, &output); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(output.Data)
	if err != nil || string(decoded) != "hello" || output.PTYID != "pty_1" {
		t.Fatalf("output = %#v err=%v", output, err)
	}

	cancel()
	exit := receivePTYEvent(t, manager.events)
	if exit.Type != "pty.exit" {
		t.Fatalf("exit event = %#v", exit)
	}
	if !conn.closed || oc.closeTerminalCalls != 1 {
		t.Fatalf("closed=%v closeCalls=%d", conn.closed, oc.closeTerminalCalls)
	}
}

func TestPTYEventsBufferFullClosesTerminal(t *testing.T) {
	manager := newPTYManager()
	manager.events = make(chan protocol.Event)
	conn := newFakeTerminalConn()
	oc := &fakeOpenCode{}
	session := &terminalSession{id: "pty_1", sessionID: "ses_1", conn: conn}
	manager.add(session)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go manager.pump(ctx, session, oc)

	conn.chunks <- []byte("hello")
	// A full queue must not block the pump; the terminal is closed instead.
	if !eventually(t, func() bool {
		manager.mu.Lock()
		_, ok := manager.terminals["pty_1"]
		manager.mu.Unlock()
		return !ok
	}) {
		t.Fatal("terminal was not closed after the event buffer filled")
	}
}

func TestPTYCloseAll(t *testing.T) {
	manager := newPTYManager()
	conn := newFakeTerminalConn()
	oc := &fakeOpenCode{}
	manager.add(&terminalSession{id: "pty_1", sessionID: "ses_1", conn: conn})
	manager.add(&terminalSession{id: "pty_2", sessionID: "ses_2", conn: newFakeTerminalConn()})

	manager.closeAll(oc)
	if !conn.closed || oc.closeTerminalCalls != 2 {
		t.Fatalf("closed=%v closeCalls=%d", conn.closed, oc.closeTerminalCalls)
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if len(manager.terminals) != 0 {
		t.Fatalf("terminals = %d", len(manager.terminals))
	}
}

func receivePTYEvent(t *testing.T, events <-chan protocol.Event) protocol.Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for PTY event")
		return protocol.Event{}
	}
}

func eventually(t *testing.T, condition func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
