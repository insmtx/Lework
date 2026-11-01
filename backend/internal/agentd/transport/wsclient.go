// Package transport provides the outbound WebSocket transport used by the
// lework agent to talk to the control plane.
//
// The agent always dials out; there is no listener here. A Client carries
// protocol.Frame values as JSON text messages and is safe for one concurrent
// reader and one concurrent writer, matching gorilla/websocket's contract.
package transport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/insmtx/Lework/backend/internal/agentd/protocol"
)

// handshakeTimeout bounds how long Dial waits for the WebSocket handshake so a
// black-holed control plane cannot stall agent startup indefinitely.
const handshakeTimeout = 10 * time.Second

// DialConfig describes how to reach the control plane.
type DialConfig struct {
	// URL is the control-plane endpoint. Only ws and wss schemes are accepted.
	URL string
	// TLSFingerprint pins the server certificate by SHA-256 of its DER
	// encoding. It is only honoured for wss URLs. Accepted forms are bare hex
	// ("ab12...") or "sha256:ab:12:...". When set, the certificate chain is not
	// validated against system roots; the pin replaces that check entirely.
	TLSFingerprint string
	// Header carries extra handshake headers, typically authentication.
	Header http.Header
}

// Client is a single WebSocket connection to the control plane.
type Client struct {
	conn *websocket.Conn

	closeOnce sync.Once
	closeErr  error
}

// Dial opens a WebSocket connection to the control plane. It returns only once
// the handshake has completed, so a nil error means the peer accepted the
// upgrade and any configured certificate pin matched.
func Dial(ctx context.Context, cfg DialConfig) (*Client, error) {
	endpoint, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse websocket url %q: %w", cfg.URL, err)
	}
	switch endpoint.Scheme {
	case "ws", "wss":
	default:
		return nil, fmt.Errorf("unsupported websocket scheme %q in %q: want ws or wss", endpoint.Scheme, endpoint.Redacted())
	}

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = handshakeTimeout
	if endpoint.Scheme == "wss" {
		tlsConfig, err := tlsConfigFor(cfg.TLSFingerprint)
		if err != nil {
			return nil, err
		}
		dialer.TLSClientConfig = tlsConfig
	}

	conn, resp, err := dialer.DialContext(ctx, endpoint.String(), cfg.Header)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, fmt.Errorf("dial %s: %w", endpoint.Redacted(), err)
	}
	return &Client{conn: conn}, nil
}

// tlsConfigFor builds the TLS settings for a wss connection. An empty
// fingerprint keeps normal chain validation; a non-empty one pins the leaf
// certificate instead, which is the only practical option for self-signed
// control planes without a CA distribution channel.
func tlsConfigFor(fingerprint string) (*tls.Config, error) {
	if fingerprint == "" {
		return nil, nil
	}
	want, err := normalizeFingerprint(fingerprint)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // replaced by the explicit leaf pin below
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("tls: peer presented no certificate")
			}
			sum := sha256.Sum256(rawCerts[0])
			got := hex.EncodeToString(sum[:])
			if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
				return fmt.Errorf("tls: certificate fingerprint mismatch: got sha256:%s", got)
			}
			return nil
		},
	}, nil
}

// normalizeFingerprint reduces a user-supplied fingerprint to lowercase hex
// without a "sha256:" prefix or separators.
func normalizeFingerprint(fingerprint string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(fingerprint))
	normalized = strings.TrimPrefix(normalized, "sha256:")
	normalized = strings.NewReplacer(":", "", " ", "", "-", "").Replace(normalized)
	if normalized == "" {
		return "", errors.New("tls fingerprint is empty")
	}
	if _, err := hex.DecodeString(normalized); err != nil || len(normalized) != sha256.Size*2 {
		return "", fmt.Errorf("invalid sha-256 tls fingerprint %q", fingerprint)
	}
	return normalized, nil
}

// Write sends f as a single JSON text message. The write deadline is taken
// from ctx when it has one; canceling ctx without a deadline also aborts a
// blocked write.
func (c *Client) Write(ctx context.Context, f protocol.Frame) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	stop := interruptOnCancel(ctx, c.conn.SetWriteDeadline)
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return fmt.Errorf("set write deadline: %w", err)
		}
	}
	if err := c.conn.WriteJSON(f); err != nil {
		if ctxErr := contextError(ctx, err); ctxErr != nil {
			return fmt.Errorf("write frame: %w", ctxErr)
		}
		return fmt.Errorf("write frame: %w", err)
	}
	return nil
}

// Read receives the next JSON frame. The read deadline is taken from ctx when
// it has one; canceling ctx without a deadline also aborts a blocked read.
// Context failures are reported as wrapped context.Canceled or
// context.DeadlineExceeded so callers can distinguish them from transport
// errors such as a dropped connection.
func (c *Client) Read(ctx context.Context) (protocol.Frame, error) {
	if err := ctx.Err(); err != nil {
		return protocol.Frame{}, fmt.Errorf("read frame: %w", err)
	}
	stop := interruptOnCancel(ctx, c.conn.SetReadDeadline)
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetReadDeadline(deadline); err != nil {
			return protocol.Frame{}, fmt.Errorf("set read deadline: %w", err)
		}
	}
	var frame protocol.Frame
	if err := c.conn.ReadJSON(&frame); err != nil {
		if ctxErr := contextError(ctx, err); ctxErr != nil {
			return protocol.Frame{}, fmt.Errorf("read frame: %w", ctxErr)
		}
		return protocol.Frame{}, fmt.Errorf("read frame: %w", err)
	}
	return frame, nil
}

// Close shuts the connection down. It is safe to call more than once; later
// calls return the first result.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.conn.Close()
	})
	return c.closeErr
}

// contextError reports which context error, if any, caused the socket error.
// The socket deadline and the context deadline expire at the same instant, so
// the socket timer can win the race by a few nanoseconds and the operation
// fails with a bare timeout while ctx.Err() is still nil. Treating that as
// context.DeadlineExceeded keeps the contract stable for callers.
func contextError(ctx context.Context, opErr error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) && isTimeoutError(opErr) {
		return context.DeadlineExceeded
	}
	return nil
}

// isTimeoutError reports whether err is a socket deadline expiry.
func isTimeoutError(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// interruptOnCancel forces the connection deadline into the past when ctx is
// canceled, so a ReadJSON/WriteJSON issued without a context deadline still
// unblocks promptly. The returned stop function is idempotent and clears the
// deadline; callers must call it before returning to avoid leaving a stale
// deadline on the shared connection.
func interruptOnCancel(ctx context.Context, setDeadline func(time.Time) error) func() {
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
