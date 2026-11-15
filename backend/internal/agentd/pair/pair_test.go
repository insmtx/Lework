package pair

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/insmtx/lework/backend/internal/agentd/protocol"
)

// scriptedConn is a two-channel pair.Conn: frames are handed to the reader in
// order, sent records everything Run writes. gate, when set, makes every Read
// wait for a token so a test can observe heartbeat writes before releasing the
// next inbound frame.
type scriptedConn struct {
	mu     sync.Mutex
	frames []protocol.Frame
	sent   []protocol.Frame
	gate   chan struct{}
}

func newScriptedConn(frames ...protocol.Frame) *scriptedConn {
	return &scriptedConn{frames: frames}
}

func (c *scriptedConn) Write(_ context.Context, f protocol.Frame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, f)
	return nil
}

func (c *scriptedConn) Read(ctx context.Context) (protocol.Frame, error) {
	if c.gate != nil {
		select {
		case <-c.gate:
		case <-ctx.Done():
			return protocol.Frame{}, ctx.Err()
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.frames) == 0 {
		return protocol.Frame{}, io.EOF
	}
	frame := c.frames[0]
	c.frames = c.frames[1:]
	return frame, nil
}

func (c *scriptedConn) sentFrames() []protocol.Frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]protocol.Frame, len(c.sent))
	copy(out, c.sent)
	return out
}

// waitForFrameType polls sent until a frame of want appears. Polling beats a
// fixed sleep: the heartbeat ticker's phase is not part of the contract.
func waitForFrameType(t *testing.T, conn *scriptedConn, want protocol.FrameType) protocol.Frame {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, frame := range conn.sentFrames() {
			if frame.Type == want {
				return frame
			}
		}
		time.Sleep(time.Millisecond)
	}
	sent := conn.sentFrames()
	types := make([]protocol.FrameType, 0, len(sent))
	for _, frame := range sent {
		types = append(types, frame.Type)
	}
	t.Fatalf("timed out waiting for a %s frame; sent %v", want, types)
	return protocol.Frame{}
}

func pendingFrame(t *testing.T, code string, expiresAt time.Time) protocol.Frame {
	t.Helper()
	raw, err := json.Marshal(struct {
		ClaimID   string    `json:"claim_id"`
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expires_at"`
	}{ClaimID: "claim-1", Code: code, ExpiresAt: expiresAt})
	if err != nil {
		t.Fatalf("encode pair.pending: %v", err)
	}
	return protocol.Frame{Type: protocol.FramePairPending, Result: raw}
}

func resultFrame(t *testing.T, credential string) protocol.Frame {
	t.Helper()
	raw, err := json.Marshal(struct {
		NodeID     string `json:"node_id"`
		Credential string `json:"credential"`
	}{NodeID: "node-1", Credential: credential})
	if err != nil {
		t.Fatalf("encode pair.result: %v", err)
	}
	return protocol.Frame{Type: protocol.FramePairResult, Result: raw}
}

func TestPairRunReturnsCredentialFromResultFrame(t *testing.T) {
	conn := newScriptedConn(
		pendingFrame(t, "AB234", time.Now().Add(5*time.Minute)),
		resultFrame(t, "cred-1"),
	)
	var gotCode string
	cred, err := Run(context.Background(), conn, Options{
		NodeName:        "work-mac",
		PairKey:         "pk",
		ControlPlaneURL: "ws://cp.example:8787/api/agent/connect",
		OnPending:       func(code, _ string) { gotCode = code },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cred != "cred-1" {
		t.Fatal("credential mismatch")
	}
	if gotCode != "AB234" {
		t.Fatalf("code = %q", gotCode)
	}
	if sent := conn.sentFrames(); len(sent) == 0 || sent[0].Type != protocol.FrameHello {
		t.Fatalf("first frame = %v, want hello", sent)
	}
}

func TestPairRunWithCodeSkipsPendingPrint(t *testing.T) {
	conn := newScriptedConn(resultFrame(t, "cred-2"))

	cred, err := Run(context.Background(), conn, Options{
		NodeName: "work-mac",
		PairKey:  "pk",
		Code:     "AB234",
		OnPending: func(string, string) {
			t.Fatal("pending printed for --code path")
		},
	})
	if err != nil || cred != "cred-2" {
		t.Fatalf("credential mismatch: %v", err)
	}
}

// helloParams mirrors the JSON Run puts in the hello frame. Duplicated here on
// purpose: the test must fail if a field is renamed or dropped.
type helloParams struct {
	Mode           string   `json:"mode"`
	PairKey        string   `json:"pair_key"`
	Code           string   `json:"code"`
	NodeName       string   `json:"node_name"`
	AgentVersion   string   `json:"agent_version"`
	OS             string   `json:"os"`
	Arch           string   `json:"arch"`
	Capabilities   []string `json:"capabilities"`
	WorkspaceRoots []string `json:"workspace_roots"`
}

func decodeHello(t *testing.T, conn *scriptedConn) helloParams {
	t.Helper()

	sent := conn.sentFrames()
	if len(sent) == 0 {
		t.Fatal("Run wrote no frames")
	}
	if sent[0].Type != protocol.FrameHello {
		t.Fatalf("first frame = %q, want hello", sent[0].Type)
	}
	var params helloParams
	if err := json.Unmarshal(sent[0].Params, &params); err != nil {
		t.Fatalf("decode hello params: %v", err)
	}
	return params
}

func TestRunHelloParams(t *testing.T) {
	t.Run("pair mode generates a pair key", func(t *testing.T) {
		conn := newScriptedConn(resultFrame(t, "cred"))
		if _, err := Run(context.Background(), conn, Options{
			NodeName:        "work-mac",
			AgentVersion:    "v1.2.3",
			WorkspaceRoots:  []string{"/home/me/work"},
			ControlPlaneURL: "ws://cp.example:8787/api/agent/connect",
		}); err != nil {
			t.Fatalf("Run: %v", err)
		}

		params := decodeHello(t, conn)
		if params.Mode != "pair" {
			t.Fatalf("mode = %q, want pair", params.Mode)
		}
		if len(params.PairKey) < 32 {
			t.Fatalf("generated pair key = %q, want at least 32 characters", params.PairKey)
		}
		if params.Code != "" {
			t.Fatalf("code = %q, want empty in pair mode", params.Code)
		}
		if params.NodeName != "work-mac" || params.AgentVersion != "v1.2.3" {
			t.Fatalf("hello params = %+v", params)
		}
		if params.OS == "" || params.Arch == "" {
			t.Fatalf("hello params missing os/arch: %+v", params)
		}
		if len(params.WorkspaceRoots) != 1 || params.WorkspaceRoots[0] != "/home/me/work" {
			t.Fatalf("workspace roots = %v", params.WorkspaceRoots)
		}
		if len(params.Capabilities) == 0 {
			t.Fatal("hello params carry no capabilities")
		}
	})

	t.Run("claim mode reports the code", func(t *testing.T) {
		conn := newScriptedConn(resultFrame(t, "cred"))
		if _, err := Run(context.Background(), conn, Options{
			NodeName: "work-mac",
			PairKey:  "pk-claim",
			Code:     "AB234",
		}); err != nil {
			t.Fatalf("Run: %v", err)
		}

		params := decodeHello(t, conn)
		if params.Mode != "claim" {
			t.Fatalf("mode = %q, want claim", params.Mode)
		}
		if params.Code != "AB234" {
			t.Fatalf("code = %q, want AB234", params.Code)
		}
		if params.PairKey != "pk-claim" {
			t.Fatalf("pair key = %q, want pk-claim", params.PairKey)
		}
	})
}

func TestRunWritesClaimFileThenRemovesIt(t *testing.T) {
	dir := t.TempDir()
	claimPath := filepath.Join(dir, "node.claim")
	expires := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Second)

	conn := newScriptedConn(pendingFrame(t, "AB234", expires), resultFrame(t, "cred-3"))
	var gotLink, gotCode string
	cred, err := Run(context.Background(), conn, Options{
		NodeName:        "work-mac",
		PairKey:         "pk-claim-file",
		ControlPlaneURL: "wss://cp.example/api/agent/connect",
		ClaimPath:       claimPath,
		OnPending: func(code, link string) {
			gotCode, gotLink = code, link
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cred != "cred-3" {
		t.Fatal("credential mismatch")
	}
	if gotCode != "AB234" {
		t.Fatalf("pending code = %q", gotCode)
	}
	if !strings.Contains(gotLink, "code=AB234") {
		t.Fatalf("pending link = %q, want the short code", gotLink)
	}
	if strings.Contains(gotLink, cred) || strings.Contains(gotCode, cred) {
		t.Fatal("pair.pending callback leaked the credential")
	}

	if _, err := os.Stat(claimPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("claim file still exists after pair.result (stat err = %v)", err)
	}
}

func TestRunClaimFileContents(t *testing.T) {
	dir := t.TempDir()
	claimPath := filepath.Join(dir, "node.claim")
	expires := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Second)

	// Capture the claim at the moment OnPending runs: Run removes it on success.
	var readBack Claim
	var mode os.FileMode
	conn := newScriptedConn(pendingFrame(t, "XY789", expires))
	conn.frames = append(conn.frames, resultFrame(t, "cred-4"))
	if _, err := Run(context.Background(), conn, Options{
		PairKey:         "pk-file",
		ControlPlaneURL: "http://cp.example:8787",
		ClaimPath:       claimPath,
		OnPending: func(string, string) {
			info, statErr := os.Stat(claimPath)
			if statErr != nil {
				t.Errorf("claim file missing during OnPending: %v", statErr)
				return
			}
			mode = info.Mode().Perm()
			claim, ok, claimErr := ReadClaim(claimPath)
			if claimErr != nil || !ok {
				t.Errorf("ReadClaim during OnPending = %+v, %t, %v", claim, ok, claimErr)
				return
			}
			readBack = claim
		},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if mode != 0o600 {
		t.Fatalf("claim file mode = %o, want 600", mode)
	}
	if readBack.Code != "XY789" || readBack.PairKey != "pk-file" {
		t.Fatalf("claim = %+v", readBack)
	}
	if readBack.ControlPlaneURL != "http://cp.example:8787" {
		t.Fatalf("claim control plane url = %q", readBack.ControlPlaneURL)
	}
	if readBack.Link == "" || !strings.Contains(readBack.Link, "code=XY789") {
		t.Fatalf("claim link = %q", readBack.Link)
	}
	if !readBack.ExpiresAt.Equal(expires) {
		t.Fatalf("claim expires at = %v, want %v", readBack.ExpiresAt, expires)
	}
}

func TestRunIgnoresJunkFrames(t *testing.T) {
	event := protocol.Frame{
		Type:  protocol.FrameEvent,
		Event: &protocol.Event{NodeID: "node-1", Type: "junk"},
	}
	conn := newScriptedConn(
		event,
		protocol.Frame{Type: protocol.FrameHeartbeat},
		pendingFrame(t, "AB234", time.Now().Add(time.Minute)),
		event,
		resultFrame(t, "cred-5"),
	)

	cred, err := Run(context.Background(), conn, Options{
		PairKey:         "pk",
		ControlPlaneURL: "ws://cp.example",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cred != "cred-5" {
		t.Fatal("credential mismatch")
	}
}

func TestRunSendsHeartbeatsWhileWaiting(t *testing.T) {
	previous := heartbeatInterval
	heartbeatInterval = 5 * time.Millisecond
	t.Cleanup(func() { heartbeatInterval = previous })

	gate := make(chan struct{})
	conn := newScriptedConn(
		pendingFrame(t, "AB234", time.Now().Add(time.Minute)),
		resultFrame(t, "cred-6"),
	)
	conn.gate = gate

	type outcome struct {
		cred string
		err  error
	}
	pendingSeen := make(chan string, 1)
	done := make(chan outcome, 1)
	go func() {
		cred, err := Run(context.Background(), conn, Options{
			PairKey:         "pk-heartbeat",
			ControlPlaneURL: "ws://cp.example",
			OnPending:       func(code, _ string) { pendingSeen <- code },
		})
		done <- outcome{cred: cred, err: err}
	}()

	waitForFrameType(t, conn, protocol.FrameHello)
	gate <- struct{}{} // release pair.pending
	select {
	case <-pendingSeen:
	case <-time.After(3 * time.Second):
		t.Fatal("pair.pending was not handled")
	}

	heartbeat := waitForFrameType(t, conn, protocol.FrameHeartbeat)
	if len(heartbeat.Params) != 0 || len(heartbeat.Result) != 0 {
		t.Fatalf("heartbeat carries payload: %+v", heartbeat)
	}

	gate <- struct{}{} // release pair.result
	select {
	case got := <-done:
		if got.err != nil || got.cred != "cred-6" {
			t.Fatalf("Run failed: %v", got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after pair.result")
	}
}

func TestRunRejectsEmptyCredential(t *testing.T) {
	conn := newScriptedConn(resultFrame(t, ""))
	_, err := Run(context.Background(), conn, Options{PairKey: "pk"})
	if err == nil {
		t.Fatal("Run accepted an empty credential")
	}
	if strings.Contains(err.Error(), `""`) {
		t.Fatalf("error text quotes the empty credential: %q", err)
	}
}

func TestClassifyCloseReasons(t *testing.T) {
	closeErr := func(text string) error {
		return &websocket.CloseError{Code: websocket.ClosePolicyViolation, Text: text}
	}
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"pairing expired", closeErr("pairing expired"), ErrPairingExpired},
		{"credential already issued", closeErr("credential already issued"), ErrCredentialAlreadyIssued},
		{"claim code unavailable", closeErr("claim code unavailable"), ErrClaimCodeUnavailable},
		{"claim attempts exceeded", closeErr("claim code attempts exceeded"), ErrClaimAttemptsExceeded},
		{"pair conn replaced", closeErr("pair connection replaced"), ErrPairConnReplaced},
		{"wrapped close", errors.Join(io.EOF, closeErr("pairing expired")), ErrPairingExpired},
		{"unknown reason", closeErr("pairing unavailable"), nil},
		{"empty reason", closeErr(""), nil},
		{"plain error", errors.New("read frame: EOF"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.err)
			if got == nil {
				t.Fatal("classify returned nil")
			}
			if tt.want != nil {
				if !errors.Is(got, tt.want) {
					t.Fatalf("classify = %v, want %v", got, tt.want)
				}
				return
			}
			for _, sentinel := range []error{
				ErrPairingExpired, ErrCredentialAlreadyIssued, ErrClaimCodeUnavailable,
				ErrClaimAttemptsExceeded, ErrPairConnReplaced,
			} {
				if errors.Is(got, sentinel) {
					t.Fatalf("classify mapped an unknown reason to %v", sentinel)
				}
			}
		})
	}
}

// TestRunWrapsCloseReason fixes the contract callers rely on: a close reason
// must stay inspectable with errors.Is after Run wraps it.
func TestRunWrapsCloseReason(t *testing.T) {
	conn := &failingConn{readErr: &websocket.CloseError{Code: websocket.ClosePolicyViolation, Text: "pairing expired"}}
	_, err := Run(context.Background(), conn, Options{PairKey: "pk"})
	if !errors.Is(err, ErrPairingExpired) {
		t.Fatalf("Run error = %v, want ErrPairingExpired", err)
	}
	if !strings.HasPrefix(err.Error(), "agentd: pair: ") {
		t.Fatalf("Run error = %q, want the agentd: pair prefix", err)
	}
}

// failingConn fails every read and accepts writes so Run can reach the read loop.
type failingConn struct{ readErr error }

func (c *failingConn) Write(context.Context, protocol.Frame) error { return nil }
func (c *failingConn) Read(context.Context) (protocol.Frame, error) {
	return protocol.Frame{}, c.readErr
}

func TestLink(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantURL string
	}{
		{"ws to http", "ws://cp.example:8787/api/agent/connect?x=1", "http://cp.example:8787"},
		{"wss to https", "wss://cp.example/api/agent/connect", "https://cp.example"},
		{"http kept", "http://cp.example:9000/", "http://cp.example:9000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link, err := Link(tt.input, "AB234")
			if err != nil {
				t.Fatalf("Link: %v", err)
			}
			parsed, err := url.Parse(link)
			if err != nil {
				t.Fatalf("parse link %q: %v", link, err)
			}
			if parsed.Scheme != "lework" || parsed.Host != "node" || parsed.Path != "/pair" {
				t.Fatalf("link = %q, want lework://node/pair", link)
			}
			if got := parsed.Query().Get("url"); got != tt.wantURL {
				t.Fatalf("url param = %q, want %q", got, tt.wantURL)
			}
			if got := parsed.Query().Get("code"); got != "AB234" {
				t.Fatalf("code param = %q, want AB234", got)
			}
		})
	}

	for _, bad := range []string{"ftp://cp.example", "://bad", "", "ws:///nohost"} {
		if link, err := Link(bad, "AB234"); err == nil {
			t.Fatalf("Link(%q) = %q, want an error", bad, link)
		}
	}
}

func TestClaimPath(t *testing.T) {
	if got, want := ClaimPath("/etc/lework/node.credential"), "/etc/lework/node.claim"; got != want {
		t.Fatalf("ClaimPath = %q, want %q", got, want)
	}
	if got, want := ClaimPath(""), "node.claim"; got != want {
		t.Fatalf("ClaimPath(\"\") = %q, want %q", got, want)
	}
}

func TestReadClaim(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.claim")
	writeClaimFile(t, valid, Claim{
		Link:            "lework://node/pair?code=AB234",
		PairKey:         "pk",
		Code:            "AB234",
		ControlPlaneURL: "ws://cp.example",
		ExpiresAt:       time.Now().Add(time.Minute),
	})
	expired := filepath.Join(dir, "expired.claim")
	writeClaimFile(t, expired, Claim{Code: "ZZ999", ExpiresAt: time.Now().Add(-time.Minute)})
	malformed := filepath.Join(dir, "malformed.claim")
	if err := os.WriteFile(malformed, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write malformed claim: %v", err)
	}

	if claim, ok, err := ReadClaim(valid); err != nil || !ok || claim.Code != "AB234" {
		t.Fatalf("ReadClaim(valid) = %+v, %t, %v", claim, ok, err)
	}
	if claim, ok, err := ReadClaim(expired); err != nil || ok || claim.Code != "ZZ999" {
		t.Fatalf("ReadClaim(expired) = %+v, %t, %v", claim, ok, err)
	}
	if _, ok, err := ReadClaim(filepath.Join(dir, "missing.claim")); err != nil || ok {
		t.Fatalf("ReadClaim(missing) ok = %t, err = %v", ok, err)
	}
	if _, ok, err := ReadClaim(""); err != nil || ok {
		t.Fatalf("ReadClaim(\"\") ok = %t, err = %v", ok, err)
	}
	if _, _, err := ReadClaim(malformed); err == nil {
		t.Fatal("ReadClaim accepted malformed JSON")
	}
}

func writeClaimFile(t *testing.T, path string, claim Claim) {
	t.Helper()
	raw, err := json.Marshal(claim)
	if err != nil {
		t.Fatalf("encode claim: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write claim: %v", err)
	}
}

func TestNewPairKey(t *testing.T) {
	first, err := NewPairKey()
	if err != nil {
		t.Fatalf("NewPairKey: %v", err)
	}
	second, err := NewPairKey()
	if err != nil {
		t.Fatalf("NewPairKey: %v", err)
	}
	if len(first) < 32 {
		t.Fatalf("pair key = %q, want at least 32 characters", first)
	}
	if first == second {
		t.Fatal("NewPairKey returned the same value twice")
	}
	if strings.ContainsAny(first, "+/=") {
		t.Fatalf("pair key = %q, want unpadded base64url", first)
	}
}
