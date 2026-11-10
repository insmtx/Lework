package opencode

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// TerminalSize is the PTY viewport in character cells.
type TerminalSize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// PersistentTerminal is the session-scoped terminal returned by OpenCode. It
// is persistent across websocket reconnects until closed or the session ends.
type PersistentTerminal struct {
	ID      string       `json:"id"`
	Title   string       `json:"title"`
	Command string       `json:"command"`
	Cwd     string       `json:"cwd"`
	Status  string       `json:"status"`
	PID     int          `json:"pid,omitempty"`
	Size    TerminalSize `json:"size,omitempty"`
}

// CreateTerminalRequest is the body for POST
// /api/experimental/session/{sessionID}/terminal. Args and Env are normalized
// to empty values so the server's required-schema validation succeeds.
type CreateTerminalRequest struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Cwd     string            `json:"cwd"`
	Title   string            `json:"title"`
	Env     map[string]string `json:"env"`
	Size    TerminalSize      `json:"size"`
}

// TerminalConn is one controller attachment to a persistent OpenCode PTY.
// Input and resize frames use the input_protocol=1 five-byte header:
// kind(1) | cols(2, big endian) | rows(2, big endian) | payload.
type TerminalConn struct {
	conn *websocket.Conn

	writeMu sync.Mutex
	size    TerminalSize

	closeOnce sync.Once
	closeErr  error
}

const terminalInputProtocol = 1

// CreatePersistentTerminal starts a terminal bound to sessionID. The caller
// must confine req.Cwd to a workspace root before calling.
func (c *Client) CreatePersistentTerminal(ctx context.Context, sessionID string, req CreateTerminalRequest) (PersistentTerminal, error) {
	if req.Args == nil {
		req.Args = []string{}
	}
	if req.Env == nil {
		req.Env = map[string]string{}
	}
	if req.Size.Cols <= 0 || req.Size.Rows <= 0 {
		req.Size = TerminalSize{Cols: 80, Rows: 24}
	}

	path := "/api/experimental/session/" + url.PathEscape(sessionID) + "/terminal"
	var terminal PersistentTerminal
	if err := c.doData(ctx, http.MethodPost, path, req, &terminal); err != nil {
		return PersistentTerminal{}, err
	}
	return terminal, nil
}

// ConnectPersistentTerminal obtains a one-use ticket and attaches to the PTY as
// the controller. It requests replay from offset 0 so a late attachment still
// sees the terminal's existing output.
func (c *Client) ConnectPersistentTerminal(ctx context.Context, ptyID string) (*TerminalConn, error) {
	ticket, err := c.connectTicket(ctx, ptyID)
	if err != nil {
		return nil, err
	}

	wsURL, err := c.terminalWebSocketURL(ptyID, ticket)
	if err != nil {
		return nil, err
	}

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	headers := http.Header{}
	headers.Set("Origin", c.origin())
	conn, resp, err := dialer.DialContext(ctx, wsURL.String(), headers)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, fmt.Errorf("dial opencode terminal: %w", err)
	}

	return &TerminalConn{
		conn: conn,
		size: TerminalSize{Cols: 80, Rows: 24},
	}, nil
}

// ResizePersistentTerminal changes the stored PTY size over HTTP. The live
// attachment can also resize over the websocket; this method is used when the
// agent needs to restore a size before attaching.
func (c *Client) ResizePersistentTerminal(ctx context.Context, ptyID string, size TerminalSize) error {
	if err := validateTerminalSize(size); err != nil {
		return err
	}
	path := "/api/experimental/persistent-pty/" + url.PathEscape(ptyID)
	body := struct {
		Size TerminalSize `json:"size"`
	}{Size: size}
	return c.doJSON(ctx, http.MethodPut, path, body, nil)
}

// ClosePersistentTerminal removes the terminal and its process. It is safe to
// call when the terminal already exited because the server returns a normal
// 404/204 response.
func (c *Client) ClosePersistentTerminal(ctx context.Context, ptyID string) error {
	path := "/api/experimental/persistent-pty/" + url.PathEscape(ptyID)
	err := c.do(ctx, c.httpClient, http.MethodDelete, path, nil, nil)
	var status *statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		return nil
	}
	return err
}

// WriteInput writes raw terminal bytes using the input_protocol=1 frame shape.
// It is safe for one concurrent reader and multiple writers; writes are
// serialized because gorilla permits only one concurrent writer.
func (t *TerminalConn) WriteInput(ctx context.Context, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	size := t.normalizedSize()
	frame := make([]byte, 5+len(data))
	frame[0] = 1
	binary.BigEndian.PutUint16(frame[1:3], uint16(size.Cols))
	binary.BigEndian.PutUint16(frame[3:5], uint16(size.Rows))
	copy(frame[5:], data)
	return t.writeBinary(ctx, frame)
}

// Resize sends a control frame with no payload. The size is remembered for
// subsequent input frames.
func (t *TerminalConn) Resize(ctx context.Context, size TerminalSize) error {
	if err := validateTerminalSize(size); err != nil {
		return err
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	t.size = size

	frame := make([]byte, 5)
	frame[0] = 0
	binary.BigEndian.PutUint16(frame[1:3], uint16(size.Cols))
	binary.BigEndian.PutUint16(frame[3:5], uint16(size.Rows))
	return t.writeBinary(ctx, frame)
}

// Read returns the next binary output frame. Text control frames (attached,
// replay_complete, resized, controller_changed) are consumed and skipped.
func (t *TerminalConn) Read(ctx context.Context) (int, []byte, error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	stop := interruptReadOnCancel(ctx, t.conn.SetReadDeadline)
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := t.conn.SetReadDeadline(deadline); err != nil {
			return 0, nil, fmt.Errorf("set terminal read deadline: %w", err)
		}
	}

	for {
		kind, data, err := t.conn.ReadMessage()
		if err != nil {
			return 0, nil, fmt.Errorf("read opencode terminal: %w", err)
		}
		if kind == websocket.BinaryMessage {
			return kind, data, nil
		}
		if kind == websocket.TextMessage {
			var control struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(data, &control) == nil {
				continue
			}
		}
	}
}

// Close closes the websocket once. A later call returns the first error.
func (t *TerminalConn) Close() error {
	if t == nil {
		return nil
	}
	t.closeOnce.Do(func() {
		t.closeErr = t.conn.Close()
	})
	return t.closeErr
}

// connectTicket asks OpenCode for a short-lived PTY connect ticket. The
// request needs the x-opencode-ticket marker and a matching Origin; the ticket
// itself is then placed in the websocket query, not in Basic Auth.
func (c *Client) connectTicket(ctx context.Context, ptyID string) (string, error) {
	path := "/api/experimental/persistent-pty/" + url.PathEscape(ptyID) + "/connect-token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return "", fmt.Errorf("build opencode terminal ticket request: %w", err)
	}
	req.Header.Set("x-opencode-ticket", "1")
	req.Header.Set("Origin", c.origin())
	c.authorize(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("opencode terminal ticket: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", c.responseError(http.MethodPost, path, resp)
	}

	var envelope struct {
		Data struct {
			Ticket string `json:"ticket"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return "", fmt.Errorf("decode opencode terminal ticket: %w", err)
	}
	if envelope.Data.Ticket == "" {
		return "", errors.New("opencode terminal ticket: response missing ticket")
	}
	return envelope.Data.Ticket, nil
}

// terminalWebSocketURL builds the controller attachment URL. It uses a fresh
// attachment ID per connection so OpenCode can track handoff/controller state.
func (c *Client) terminalWebSocketURL(ptyID, ticket string) (*url.URL, error) {
	endpoint, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse opencode base url: %w", err)
	}
	switch endpoint.Scheme {
	case "http":
		endpoint.Scheme = "ws"
	case "https":
		endpoint.Scheme = "wss"
	default:
		return nil, fmt.Errorf("unsupported opencode url scheme %q", endpoint.Scheme)
	}
	endpoint.Path = "/api/experimental/persistent-pty/" + url.PathEscape(ptyID) + "/connect"
	query := endpoint.Query()
	query.Set("ticket", ticket)
	query.Set("cursor", "0")
	query.Set("role", "controller")
	query.Set("attachment_id", randomAttachmentID())
	query.Set("takeover", "true")
	query.Set("input_protocol", fmt.Sprintf("%d", terminalInputProtocol))
	endpoint.RawQuery = query.Encode()
	return endpoint, nil
}

// origin returns the HTTP origin used for OpenCode's Origin check. The PTY
// ticket and websocket routes reject requests without a matching origin.
func (c *Client) origin() string {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return c.baseURL
	}
	return u.Scheme + "://" + u.Host
}

// writeBinary writes one binary frame with a context-derived deadline. The
// cancellation hook forces a blocked write to unblock promptly.
func (t *TerminalConn) writeBinary(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := interruptWriteOnCancel(ctx, t.conn.SetWriteDeadline)
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := t.conn.SetWriteDeadline(deadline); err != nil {
			return fmt.Errorf("set terminal write deadline: %w", err)
		}
	}
	if err := t.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return fmt.Errorf("write opencode terminal: %w", err)
	}
	return nil
}

// normalizedSize returns a valid size for input frames.
func (t *TerminalConn) normalizedSize() TerminalSize {
	if t.size.Cols <= 0 || t.size.Rows <= 0 {
		return TerminalSize{Cols: 80, Rows: 24}
	}
	return t.size
}

// validateTerminalSize rejects zero and out-of-range cell counts before they
// are encoded into a two-byte header.
func validateTerminalSize(size TerminalSize) error {
	if size.Cols < 1 || size.Cols > 65535 || size.Rows < 1 || size.Rows > 65535 {
		return fmt.Errorf("invalid terminal size %dx%d", size.Cols, size.Rows)
	}
	return nil
}

// randomAttachmentID creates a stable-looking unique attachment identifier.
// It is not a capability; the ticket is the capability.
func randomAttachmentID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("lework-%d", time.Now().UnixNano())
	}
	return strings.Join([]string{
		hex.EncodeToString(buf[0:4]),
		hex.EncodeToString(buf[4:6]),
		hex.EncodeToString(buf[6:8]),
		hex.EncodeToString(buf[8:10]),
		hex.EncodeToString(buf[10:16]),
	}, "-")
}

// interruptReadOnCancel forces a read deadline into the past when ctx is
// canceled. The stop function clears the deadline before returning.
func interruptReadOnCancel(ctx context.Context, setDeadline func(time.Time) error) func() {
	return interruptDeadlineOnCancel(ctx, setDeadline)
}

func interruptWriteOnCancel(ctx context.Context, setDeadline func(time.Time) error) func() {
	return interruptDeadlineOnCancel(ctx, setDeadline)
}

func interruptDeadlineOnCancel(ctx context.Context, setDeadline func(time.Time) error) func() {
	if ctx.Done() == nil {
		return func() {}
	}

	var (
		mu       sync.Mutex
		finished bool
	)
	stop := context.AfterFunc(ctx, func() {
		mu.Lock()
		defer mu.Unlock()
		if finished {
			return
		}
		_ = setDeadline(time.Now())
	})

	var once sync.Once
	return func() {
		once.Do(func() {
			stop()
			mu.Lock()
			defer mu.Unlock()
			finished = true
			_ = setDeadline(time.Time{})
		})
	}
}
