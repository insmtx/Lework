package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/insmtx/Lework/backend/internal/controlplane/event"
)

const (
	// ptyBridgeBuffer bounds one browser bridge. The Hub closes the
	// subscription if the browser is slower than this, which closes the socket
	// instead of growing memory without bound.
	ptyBridgeBuffer = 1024

	// ptyInputLimit is the largest browser binary frame accepted as terminal
	// input. It matches the agent-side limit.
	ptyInputLimit = 64 << 10

	// ptyOutputLimit rejects a malformed or oversized agent event before it is
	// copied to the browser.
	ptyOutputLimit = 1 << 20

	// ptyWriteTimeout bounds one browser-to-agent request so a stopped agent
	// cannot hold the reader goroutine forever.
	ptyWriteTimeout = 10 * time.Second

	// ptyBrowserWriteTimeout bounds a browser write; a tab that stops reading
	// is disconnected rather than blocking shutdown.
	ptyBrowserWriteTimeout = 10 * time.Second
)

// ptyAttachParams is the agent request shape for pty.attach.
type ptyAttachParams struct {
	SessionID string `json:"session_id"`
	PTYID     string `json:"pty_id"`
}

// ptyWriteParams is the agent request shape for pty.write.
type ptyWriteParams struct {
	PTYID string `json:"pty_id"`
	Data  string `json:"data"`
}

// ptyResizeParams is the agent request shape for pty.resize.
type ptyResizeParams struct {
	PTYID string `json:"pty_id"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
}

// ptyExitPayload is the browser-facing and agent-facing exit marker.
type ptyExitPayload struct {
	Type string `json:"type"`
}

// browserSocket serializes writes because gorilla permits only one concurrent
// writer. Both the event writer and the input reader may need to report an
// error, so they share this mutex.
type browserSocket struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (s *browserSocket) writeBinary(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(ptyBrowserWriteTimeout))
	return s.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (s *browserSocket) writeText(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(ptyBrowserWriteTimeout))
	return s.conn.WriteMessage(websocket.TextMessage, payload)
}

// handlePTYWebSocket bridges a browser terminal to one agent-owned PTY.
//
// The route names node, session, and pty explicitly; there is no generic
// OpenCode proxy. Output is binary and input is binary; resize/close are small
// JSON text frames.
func (a *API) handlePTYWebSocket(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeID")
	sessionID := r.PathValue("sessionID")
	ptyID := r.PathValue("ptyID")
	if nodeID == "" || sessionID == "" || ptyID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "node, session, and pty are required")
		return
	}
	if !a.requireNode(w, r, nodeID) {
		return
	}
	if info, ok := a.registry.Get(nodeID); !ok || !info.Connected {
		writeAPIError(w, http.StatusConflict, "node_offline", "node is offline")
		return
	}

	upgrader := websocket.Upgrader{
		HandshakeTimeout: 10 * time.Second,
		ReadBufferSize:   32 << 10,
		WriteBufferSize:  32 << 10,
		CheckOrigin:      sameOriginOrEmpty,
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	socket := &browserSocket{conn: conn}

	// Subscribe before attach so output produced during the handshake is not
	// lost. The Hub may close this channel if the browser falls too far behind.
	events, unsubscribe := a.hub.Subscribe(ptyBridgeBuffer)
	defer unsubscribe()

	attachCtx, cancelAttach := context.WithTimeout(r.Context(), a.requestTimeout)
	_, attachErr := a.callAgent(attachCtx, nodeID, "pty.attach", ptyAttachParams{
		SessionID: sessionID,
		PTYID:     ptyID,
	})
	cancelAttach()
	if attachErr != nil {
		_ = socket.writeText(map[string]string{"type": "error", "message": "terminal is unavailable"})
		a.closePTY(nodeID, ptyID)
		return
	}

	bridgeCtx, cancel := context.WithCancel(r.Context())
	defer cancel()

	errCh := make(chan error, 2)
	go func() { errCh <- a.ptyEventWriter(bridgeCtx, socket, events, nodeID, sessionID, ptyID) }()
	go func() { errCh <- a.ptyInputReader(bridgeCtx, socket, nodeID, ptyID) }()

	// The first side to finish owns shutdown: cancel the other goroutine, close
	// the socket to unblock its read, then close the remote PTY with a fresh
	// context because r.Context is already canceled on a browser disconnect.
	err = <-errCh
	cancel()
	_ = conn.Close()
	a.closePTY(nodeID, ptyID)

	select {
	case <-errCh:
	case <-time.After(time.Second):
	}
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, websocket.ErrCloseSent) {
		// Errors are already reported by the side that observed them when the
		// socket is still writable; no internal detail is copied to the browser.
		return
	}
}

// ptyEventWriter forwards only events for this node, session, and pty. Binary
// output is decoded from base64; malformed or oversized data closes the bridge.
func (a *API) ptyEventWriter(
	ctx context.Context,
	socket *browserSocket,
	events <-chan event.Event,
	nodeID, sessionID, ptyID string,
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, open := <-events:
			if !open {
				_ = socket.writeText(map[string]string{"type": "error", "message": "terminal stream lost"})
				return errors.New("pty event stream closed")
			}
			if ev.NodeID != nodeID || ev.SessionID != sessionID {
				continue
			}
			if ev.Type != "pty.output" && ev.Type != "pty.exit" {
				continue
			}
			var payload struct {
				PTYID string `json:"pty_id"`
				Data  string `json:"data"`
			}
			if err := json.Unmarshal(ev.Data, &payload); err != nil {
				_ = socket.writeText(map[string]string{"type": "error", "message": "invalid terminal event"})
				return fmt.Errorf("decode pty event: %w", err)
			}
			if payload.PTYID != ptyID {
				continue
			}
			if ev.Type == "pty.exit" {
				_ = socket.writeText(ptyExitPayload{Type: "exit"})
				return nil
			}
			data, err := base64.StdEncoding.DecodeString(payload.Data)
			if err != nil {
				_ = socket.writeText(map[string]string{"type": "error", "message": "invalid terminal data"})
				return fmt.Errorf("decode pty output: %w", err)
			}
			if len(data) > ptyOutputLimit {
				_ = socket.writeText(map[string]string{"type": "error", "message": "terminal output is too large"})
				return errors.New("pty output exceeds limit")
			}
			if err := socket.writeBinary(data); err != nil {
				return fmt.Errorf("write pty output: %w", err)
			}
		}
	}
}

// ptyInputReader forwards browser bytes and control frames to the agent.
func (a *API) ptyInputReader(ctx context.Context, socket *browserSocket, nodeID, ptyID string) error {
	for {
		kind, data, err := socket.conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read browser terminal: %w", err)
		}
		switch kind {
		case websocket.BinaryMessage:
			if len(data) > ptyInputLimit {
				_ = socket.writeText(map[string]string{"type": "error", "message": "terminal input is too large"})
				return errors.New("pty input exceeds limit")
			}
			if err := a.sendPTYWrite(ctx, nodeID, ptyID, data); err != nil {
				return err
			}
		case websocket.TextMessage:
			var control struct {
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if err := json.Unmarshal(data, &control); err != nil {
				_ = socket.writeText(map[string]string{"type": "error", "message": "invalid terminal control"})
				return fmt.Errorf("decode browser terminal control: %w", err)
			}
			switch control.Type {
			case "resize":
				if control.Cols < 1 || control.Cols > 1000 || control.Rows < 1 || control.Rows > 1000 {
					_ = socket.writeText(map[string]string{"type": "error", "message": "invalid terminal size"})
					return errors.New("invalid pty resize")
				}
				if err := a.sendPTYResize(ctx, nodeID, ptyID, control.Cols, control.Rows); err != nil {
					return err
				}
			case "close":
				return nil
			default:
				_ = socket.writeText(map[string]string{"type": "error", "message": "unknown terminal control"})
				return fmt.Errorf("unknown terminal control %q", control.Type)
			}
		}
	}
}

// sendPTYWrite encodes browser bytes and waits for the agent acknowledgement.
func (a *API) sendPTYWrite(ctx context.Context, nodeID, ptyID string, data []byte) error {
	callCtx, cancel := context.WithTimeout(ctx, ptyWriteTimeout)
	defer cancel()
	_, err := a.callAgent(callCtx, nodeID, "pty.write", ptyWriteParams{
		PTYID: ptyID,
		Data:  base64.StdEncoding.EncodeToString(data),
	})
	if err != nil {
		return fmt.Errorf("pty.write: %w", err)
	}
	return nil
}

// sendPTYResize forwards a validated viewport change.
func (a *API) sendPTYResize(ctx context.Context, nodeID, ptyID string, cols, rows int) error {
	callCtx, cancel := context.WithTimeout(ctx, ptyWriteTimeout)
	defer cancel()
	_, err := a.callAgent(callCtx, nodeID, "pty.resize", ptyResizeParams{
		PTYID: ptyID,
		Cols:  cols,
		Rows:  rows,
	})
	if err != nil {
		return fmt.Errorf("pty.resize: %w", err)
	}
	return nil
}

// closePTY closes the remote terminal with a fresh timeout. Failure is
// best-effort: the browser is already gone and the agent may be offline.
func (a *API) closePTY(nodeID, ptyID string) {
	if nodeID == "" || ptyID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = a.callAgent(ctx, nodeID, "pty.close", ptyCloseParams{PTYID: ptyID})
}

// sameOriginOrEmpty accepts non-browser clients without an Origin header and
// same-host browser origins. Cross-origin tabs cannot open a terminal.
func sameOriginOrEmpty(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}
