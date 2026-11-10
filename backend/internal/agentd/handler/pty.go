package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/insmtx/lework/backend/internal/agentd/opencode"
	"github.com/insmtx/lework/backend/internal/agentd/protocol"
)

const (
	// ptyEventBuffer bounds queued terminal output between the terminal pump
	// and the runner's frame writer. A full queue closes the terminal instead
	// of blocking OpenCode's websocket reader.
	ptyEventBuffer = 1024

	// ptyOutputChunk limits one base64 event so it stays under the agent's
	// 1 MiB frame limit.
	ptyOutputChunk = 32 << 10

	// ptyConnectTimeout bounds the OpenCode ticket/websocket handshake.
	ptyConnectTimeout = 15 * time.Second

	// ptyCloseTimeout bounds the best-effort remote close after the browser is
	// already gone.
	ptyCloseTimeout = 5 * time.Second
)

// ptyManager owns the terminals created for one agent process. It deliberately
// lives in the handler rather than the runner so reconnects do not need to
// recreate terminal state; the runner closes active terminals when a
// connection ends.
type ptyManager struct {
	mu        sync.Mutex
	terminals map[string]*terminalSession
	events    chan protocol.Event
}

// terminalSession is one OpenCode persistent terminal and its optional live
// attachment. conn is nil between create and attach.
type terminalConnection interface {
	Read(context.Context) (int, []byte, error)
	WriteInput(context.Context, []byte) error
	Resize(context.Context, opencode.TerminalSize) error
	Close() error
}

type terminalSession struct {
	id        string
	sessionID string
	cols      int
	rows      int

	conn   terminalConnection
	cancel context.CancelFunc

	attached  bool
	closeOnce sync.Once
}

func newPTYManager() *ptyManager {
	return &ptyManager{
		terminals: make(map[string]*terminalSession),
		events:    make(chan protocol.Event, ptyEventBuffer),
	}
}

// add records a newly created terminal. The caller has already validated the
// session and directory.
func (m *ptyManager) add(session *terminalSession) {
	if session == nil || session.id == "" {
		return
	}
	m.mu.Lock()
	m.terminals[session.id] = session
	m.mu.Unlock()
}

// attach connects the OpenCode websocket and starts the output pump exactly
// once. A second attach is a no-op so a browser reconnect can subscribe to the
// same event stream.
func (m *ptyManager) attach(id string, api OpenCodeAPI) error {
	m.mu.Lock()
	session, ok := m.terminals[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("pty.attach: unknown pty %q", id)
	}
	if session.attached {
		m.mu.Unlock()
		return nil
	}
	session.attached = true
	pumpCtx, cancel := context.WithCancel(context.Background())
	session.cancel = cancel
	m.mu.Unlock()

	connectCtx, connectCancel := context.WithTimeout(pumpCtx, ptyConnectTimeout)
	conn, err := api.ConnectPersistentTerminal(connectCtx, id)
	connectCancel()
	if err != nil {
		m.close(id, api)
		return fmt.Errorf("pty.attach: %w", err)
	}

	m.mu.Lock()
	session.conn = conn
	m.mu.Unlock()
	go m.pump(pumpCtx, session, api)
	return nil
}

// pump reads terminal output until the OpenCode socket or the manager context
// ends. A read failure after cancellation is a normal close, not an error.
func (m *ptyManager) pump(ctx context.Context, session *terminalSession, api OpenCodeAPI) {
	for {
		_, data, err := session.conn.Read(ctx)
		if err != nil {
			m.close(session.id, api)
			return
		}
		for len(data) > 0 {
			chunk := data
			if len(chunk) > ptyOutputChunk {
				chunk = chunk[:ptyOutputChunk]
			}
			data = data[len(chunk):]
			if !m.emitOutput(session, chunk) {
				m.close(session.id, api)
				return
			}
		}
	}
}

// write forwards decoded terminal input. It returns an error if the terminal
// has not been attached yet.
func (m *ptyManager) write(ctx context.Context, id string, data []byte) error {
	m.mu.Lock()
	session, ok := m.terminals[id]
	var conn terminalConnection
	if ok {
		conn = session.conn
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("pty.write: unknown pty %q", id)
	}
	if conn == nil {
		return errors.New("pty.write: terminal is not attached")
	}
	return conn.WriteInput(ctx, data)
}

// resize forwards a viewport change over the live terminal websocket.
func (m *ptyManager) resize(ctx context.Context, id string, cols, rows int) error {
	m.mu.Lock()
	session, ok := m.terminals[id]
	var conn terminalConnection
	if ok {
		conn = session.conn
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("pty.resize: unknown pty %q", id)
	}
	if conn == nil {
		return errors.New("pty.resize: terminal is not attached")
	}
	session.cols = cols
	session.rows = rows
	return conn.Resize(ctx, opencode.TerminalSize{Cols: cols, Rows: rows})
}

// close terminates a terminal and removes it from the map. It is idempotent;
// the remote DELETE is best effort because the browser may already be gone.
func (m *ptyManager) close(id string, api OpenCodeAPI) {
	m.mu.Lock()
	session, ok := m.terminals[id]
	if ok {
		delete(m.terminals, id)
	}
	m.mu.Unlock()
	if !ok || session == nil {
		return
	}

	session.closeOnce.Do(func() {
		if session.cancel != nil {
			session.cancel()
		}
		if session.conn != nil {
			_ = session.conn.Close()
		}
		if api != nil {
			ctx, cancel := context.WithTimeout(context.Background(), ptyCloseTimeout)
			defer cancel()
			_ = api.ClosePersistentTerminal(ctx, id)
		}
		m.emitExit(session)
	})
}

// closeAll closes every terminal and leaves the manager reusable for a new
// control-plane connection.
func (m *ptyManager) closeAll(api OpenCodeAPI) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.terminals))
	for id := range m.terminals {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.close(id, api)
	}
}

// emitOutput enqueues one base64 output chunk. A full queue closes the
// terminal rather than blocking the OpenCode reader.
func (m *ptyManager) emitOutput(session *terminalSession, data []byte) bool {
	payload, err := json.Marshal(struct {
		PTYID string `json:"pty_id"`
		Data  string `json:"data"`
	}{
		PTYID: session.id,
		Data:  base64.StdEncoding.EncodeToString(data),
	})
	if err != nil {
		return false
	}
	return m.emit(protocol.Event{
		SessionID: session.sessionID,
		Type:      "pty.output",
		Data:      payload,
		Time:      time.Now().UTC(),
	})
}

// emitExit announces that the terminal is no longer available.
func (m *ptyManager) emitExit(session *terminalSession) {
	payload, err := json.Marshal(struct {
		PTYID string `json:"pty_id"`
	}{PTYID: session.id})
	if err != nil {
		return
	}
	_ = m.emit(protocol.Event{
		SessionID: session.sessionID,
		Type:      "pty.exit",
		Data:      payload,
		Time:      time.Now().UTC(),
	})
}

// emit performs a non-blocking enqueue.
func (m *ptyManager) emit(event protocol.Event) bool {
	select {
	case m.events <- event:
		return true
	default:
		return false
	}
}
