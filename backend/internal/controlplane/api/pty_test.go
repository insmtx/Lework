package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/insmtx/lework/backend/internal/agentd/protocol"
	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/node"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// startPTYAgentResponses auto-acknowledges every agent request so the bridge
// reader/writer can be exercised without a real node process.
func startPTYAgentResponses(t *testing.T, agent *fakeAgent, conn *node.Connection) (<-chan protocol.Frame, func()) {
	t.Helper()
	frames := make(chan protocol.Frame, 32)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case frame := <-agent.frames:
				select {
				case frames <- frame:
				default:
				}
				_ = conn.Deliver(protocol.Frame{
					Type:   protocol.FrameResponse,
					ID:     frame.ID,
					Result: json.RawMessage("null"),
				})
			case <-stop:
				return
			}
		}
	}()
	return frames, func() { close(stop) }
}

func openPTYBridge(t *testing.T, env *testEnv, rec store.Node) (*httptest.Server, *websocket.Conn, <-chan protocol.Frame) {
	t.Helper()
	agent := &fakeAgent{frames: make(chan protocol.Frame, 16)}
	conn := node.NewConnection(rec.ID, agent)
	env.registry.Register(node.Info{ID: rec.ID, Name: rec.Name, Connected: true}, conn)
	frames, stop := startPTYAgentResponses(t, agent, conn)

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	t.Cleanup(stop)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/pty/" + rec.ID + "/ses_1/pty_1/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial pty websocket: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return srv, ws, frames
}

func TestPTYWebSocketRejectsUnknownAndOfflineNode(t *testing.T) {
	env := newTestEnv(t, time.Second)

	unknown := env.do(http.MethodGet, "/api/pty/node_missing/ses_1/pty_1/ws", "")
	requireErrorCode(t, unknown, http.StatusNotFound, "not_found")

	rec, _, _ := env.addNode("dev-box")
	env.registry.CloseAll()
	offline := env.do(http.MethodGet, "/api/pty/"+rec.ID+"/ses_1/pty_1/ws", "")
	requireErrorCode(t, offline, http.StatusConflict, "node_offline")
}

func TestPTYWebSocketAttachesAndForwardsOutput(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, _, _ := env.addNode("dev-box")
	_, ws, frames := openPTYBridge(t, env, rec)

	attach := waitFrame(t, frames)
	if attach.Method != "pty.attach" {
		t.Fatalf("first method = %q, want pty.attach", attach.Method)
	}
	params := decodeParamsInto[ptyAttachParams](t, attach)
	if params.SessionID != "ses_1" || params.PTYID != "pty_1" {
		t.Fatalf("attach params = %+v", params)
	}

	env.hub.Publish(event.Event{
		NodeID:    rec.ID,
		SessionID: "ses_1",
		Type:      "pty.output",
		Data:      json.RawMessage(`{"pty_id":"pty_1","data":"aGVsbG8="}`),
	})
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	kind, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if kind != websocket.BinaryMessage || string(data) != "hello" {
		t.Fatalf("message = kind %d data %q", kind, data)
	}

	// An event for another pty is ignored, not written to this browser.
	env.hub.Publish(event.Event{
		NodeID:    rec.ID,
		SessionID: "ses_1",
		Type:      "pty.output",
		Data:      json.RawMessage(`{"pty_id":"pty_other","data":"bm8="}`),
	})
}

func TestPTYWebSocketForwardsInputResizeAndClose(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, _, _ := env.addNode("dev-box")
	_, ws, frames := openPTYBridge(t, env, rec)
	_ = waitFrame(t, frames)

	if err := ws.WriteMessage(websocket.BinaryMessage, []byte("echo hi")); err != nil {
		t.Fatal(err)
	}
	write := waitFrame(t, frames)
	if write.Method != "pty.write" {
		t.Fatalf("method = %q, want pty.write", write.Method)
	}
	writeParams := decodeParamsInto[ptyWriteParams](t, write)
	decoded, err := base64.StdEncoding.DecodeString(writeParams.Data)
	if err != nil || string(decoded) != "echo hi" {
		t.Fatalf("write params = %+v err=%v", writeParams, err)
	}

	if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":100,"rows":40}`)); err != nil {
		t.Fatal(err)
	}
	resize := waitFrame(t, frames)
	if resize.Method != "pty.resize" {
		t.Fatalf("method = %q, want pty.resize", resize.Method)
	}
	resizeParams := decodeParamsInto[ptyResizeParams](t, resize)
	if resizeParams.PTYID != "pty_1" || resizeParams.Cols != 100 || resizeParams.Rows != 40 {
		t.Fatalf("resize params = %+v", resizeParams)
	}

	if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"close"}`)); err != nil {
		t.Fatal(err)
	}
	closeFrame := waitFrame(t, frames)
	if closeFrame.Method != "pty.close" {
		t.Fatalf("method = %q, want pty.close", closeFrame.Method)
	}
	closeParams := decodeParamsInto[ptyCloseParams](t, closeFrame)
	if closeParams.PTYID != "pty_1" {
		t.Fatalf("close params = %+v", closeParams)
	}
}

func TestPTYWebSocketForwardsExit(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, _, _ := env.addNode("dev-box")
	_, ws, frames := openPTYBridge(t, env, rec)
	_ = waitFrame(t, frames)

	env.hub.Publish(event.Event{
		NodeID:    rec.ID,
		SessionID: "ses_1",
		Type:      "pty.exit",
		Data:      json.RawMessage(`{"pty_id":"pty_1"}`),
	})
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	kind, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if kind != websocket.TextMessage || strings.TrimSpace(string(data)) != `{"type":"exit"}` {
		t.Fatalf("message = kind %d data %q", kind, data)
	}
}

func TestPTYWebSocketCloseSendsAgentClose(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec, _, _ := env.addNode("dev-box")
	_, ws, frames := openPTYBridge(t, env, rec)
	_ = waitFrame(t, frames)

	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	// The server's reader observes the close and calls pty.close with a fresh
	// context. The auto-responder acknowledges it.
	closeFrame := waitFrame(t, frames)
	if closeFrame.Method != "pty.close" {
		t.Fatalf("method = %q, want pty.close", closeFrame.Method)
	}
}
